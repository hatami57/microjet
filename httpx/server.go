package httpx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hatami57/microjet/httpx/middleware"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

type ServerConfig struct {
	Host  string
	Port  int
	Debug bool
}

// ReadinessFunc reports whether a dependency is ready to serve traffic. It
// should return nil when healthy and an error describing the problem otherwise.
type ReadinessFunc func(ctx context.Context) error

type namedReadinessCheck struct {
	name string
	fn   ReadinessFunc
}

type Server struct {
	Router     *gin.Engine
	httpServer *http.Server

	readinessMu     sync.RWMutex
	readinessChecks []namedReadinessCheck
}

// AddReadinessCheck registers a named dependency probe consulted by GET /readyz.
// Checks are evaluated on each request, so registration order and timing do not
// matter. Safe for concurrent use.
func (s *Server) AddReadinessCheck(name string, fn ReadinessFunc) {
	s.readinessMu.Lock()
	defer s.readinessMu.Unlock()
	s.readinessChecks = append(s.readinessChecks, namedReadinessCheck{name: name, fn: fn})
}

func (s *Server) runReadinessChecks(ctx context.Context) (bool, map[string]string) {
	s.readinessMu.RLock()
	checks := make([]namedReadinessCheck, len(s.readinessChecks))
	copy(checks, s.readinessChecks)
	s.readinessMu.RUnlock()

	ready := true
	results := make(map[string]string, len(checks))
	for _, ch := range checks {
		if err := ch.fn(ctx); err != nil {
			results[ch.name] = "error: " + err.Error()
			ready = false
		} else {
			results[ch.name] = "ok"
		}
	}
	return ready, results
}

func NewServer(cfg ServerConfig, logger *slog.Logger) *Server {
	if cfg.Debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	server := &Server{
		Router: router,
		httpServer: &http.Server{
			Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
			Handler:      router,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
	}

	metrics := middleware.NewMetrics()

	router.Use(middleware.RequestID())
	router.Use(metrics.Middleware())
	router.Use(middleware.Logger(logger))
	router.Use(middleware.Error(cfg.Debug))
	router.Use(gin.Recovery())

	// Liveness: the process is up. Readiness: dependencies are usable.
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Prometheus metrics in text exposition format.
	router.GET("/metrics", gin.WrapH(metrics.Handler()))

	router.GET("/readyz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		ready, results := server.runReadinessChecks(ctx)
		status := http.StatusOK
		statusText := "ok"
		if !ready {
			status = http.StatusServiceUnavailable
			statusText = "unavailable"
		}
		c.JSON(status, gin.H{"status": statusText, "checks": results})
	})

	if cfg.Debug {
		router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}

	return server
}

func (s *Server) Start() error {
	if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) Addr() string {
	return s.httpServer.Addr
}
