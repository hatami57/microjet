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
	"github.com/hatami57/microjet/core/config"
	"github.com/hatami57/microjet/httpx/middleware"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

type ServerConfig struct {
	Host  string `mapstructure:"host"`
	Port  int    `mapstructure:"port"`
	Debug bool   `mapstructure:"debug"`
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
	config     ServerConfig
	logger     *slog.Logger
	metrics    *middleware.Metrics
	httpServer *http.Server

	readinessMu     sync.RWMutex
	readinessChecks []namedReadinessCheck

	errCh chan error
}

// AddReadinessCheck registers a named dependency probe consulted by GET /readyz.
// Safe for concurrent use.
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

// NewServer creates the router with its middleware stack and standard routes
// (/health, /metrics, /readyz, and /swagger if debug). The server does not
// start listening until Init is called.
func NewServer(cfg ServerConfig, logger *slog.Logger) *Server {
	if cfg.Debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	metrics := middleware.NewMetrics()

	s := &Server{
		Router:  router,
		config:  cfg,
		logger:  logger,
		metrics: metrics,
		errCh:   make(chan error, 1),
	}

	router.Use(middleware.RequestID())
	router.Use(middleware.Tracing())
	router.Use(metrics.Middleware())
	router.Use(middleware.Logger(logger))
	router.Use(middleware.Error(cfg.Debug))
	router.Use(gin.Recovery())

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/metrics", gin.WrapH(metrics.Handler()))
	router.GET("/readyz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		ready, results := s.runReadinessChecks(ctx)
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

	return s
}

// ReadConfig implements config.Configurable, reading the [server] section so the
// server owns its configuration independently of host.Config.
func (s *Server) ReadConfig(l config.Reader) error {
	l.SetDefault("server.host", "localhost")
	l.SetDefault("server.port", 8080)
	return l.Read("server", &s.config)
}

// Init implements core.Initer. It builds the underlying http.Server but does not
// begin listening, leaving a window for setup work (route registration) before
// the server serves. Call Start (the host does this in its start phase) to serve.
func (s *Server) Init() error {
	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", s.config.Host, s.config.Port),
		Handler:      s.Router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return nil
}

// Start implements core.Starter. It begins serving in a background goroutine; the
// outcome is available via ErrCh once the server stops (nil on clean shutdown,
// error otherwise). Init must have run first.
func (s *Server) Start() error {
	if s.httpServer == nil {
		return fmt.Errorf("http server not initialized; call Init before Start")
	}
	s.logger.Info("starting HTTP server", "addr", s.Addr())
	go func() {
		s.errCh <- s.serve()
	}()
	return nil
}

// Close implements core.Closer, gracefully draining and stopping the server.
func (s *Server) Close() error {
	if s.httpServer == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return s.Stop(ctx)
}

// ErrCh returns the channel that receives the server's exit error (or nil on
// clean shutdown via Stop). The host reads this to react to unexpected failures.
func (s *Server) ErrCh() <-chan error {
	return s.errCh
}

func (s *Server) serve() error {
	if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// Addr returns the listener address. Before Init it is derived from the config;
// after Init it reflects the bound address.
func (s *Server) Addr() string {
	if s.httpServer != nil {
		return s.httpServer.Addr
	}
	return fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
}
