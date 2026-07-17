// Package httpx provides a Gin-based HTTP server with built-in middleware, a
// health/readiness/metrics surface, typed request helpers, a JSON client with
// retries and a circuit breaker, and web/callback helpers.
package httpx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hatami57/microjet/core/configx"
	"github.com/hatami57/microjet/core/errorx"
	"github.com/hatami57/microjet/httpx/middleware"
	"github.com/prometheus/client_golang/prometheus"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

type ServerConfig struct {
	Host  string `mapstructure:"host"`
	Port  int    `mapstructure:"port"`
	Debug bool   `mapstructure:"debug"`

	// ReadTimeout bounds reading the entire request (headers + body); 0 disables.
	ReadTimeout time.Duration `mapstructure:"readTimeout"`
	// ReadHeaderTimeout bounds reading just the request headers, capping slowloris
	// exposure even if ReadTimeout is set to 0.
	ReadHeaderTimeout time.Duration `mapstructure:"readHeaderTimeout"`
	// WriteTimeout bounds writing the response; 0 disables.
	WriteTimeout time.Duration `mapstructure:"writeTimeout"`
	// IdleTimeout bounds how long a keep-alive connection stays open between
	// requests; 0 disables.
	IdleTimeout time.Duration `mapstructure:"idleTimeout"`
	// MaxHeaderBytes caps the size of request headers (default 1 MiB when 0).
	MaxHeaderBytes int `mapstructure:"maxHeaderBytes"`

	// CertFile and KeyFile enable TLS. When both are set the server serves HTTPS
	// via ListenAndServeTLS; otherwise it serves plain HTTP.
	CertFile string `mapstructure:"certFile"`
	KeyFile  string `mapstructure:"keyFile"`
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
	section    string
	logger     *slog.Logger
	metrics    *middleware.Metrics
	httpServer *http.Server

	readinessMu     sync.RWMutex
	readinessChecks []namedReadinessCheck

	// notReady short-circuits /readyz to 503 without running checks once shutdown
	// begins; see SetReady and core.ReadinessToggler.
	notReady atomic.Bool

	errCh chan error
}

// SetReady toggles the server's readiness, implementing core.ReadinessToggler.
// When set to false, /readyz returns 503 {"status":"shutting-down"} without
// running the dependency checks, so a load balancer stops routing new traffic
// while /health (liveness) stays 200. The host flips this off at the start of
// graceful shutdown. Safe for concurrent use.
func (s *Server) SetReady(ready bool) { s.notReady.Store(!ready) }

// AddReadinessCheck registers a named dependency probe consulted by GET /readyz.
// Safe for concurrent use.
func (s *Server) AddReadinessCheck(name string, fn ReadinessFunc) {
	s.readinessMu.Lock()
	defer s.readinessMu.Unlock()
	s.readinessChecks = append(s.readinessChecks, namedReadinessCheck{name: name, fn: fn})
}

// MetricsRegistry returns the live Prometheus registry backing the server's
// /metrics endpoint, so application code can register custom collectors that are
// then exposed alongside the built-in HTTP, Go runtime, and process metrics. Call
// it from a Setup hook (before the server starts serving):
//
//	counter := prometheus.NewCounter(prometheus.CounterOpts{Name: "widgets_total"})
//	httpx.Of(app).MetricsRegistry().MustRegister(counter)
func (s *Server) MetricsRegistry() *prometheus.Registry {
	return s.metrics.Registry()
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
// (/health, /metrics, /readyz, and /swagger + /debug/pprof if debug). The
// server does not start listening until Init is called.
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
		if s.notReady.Load() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "shutting-down"})
			return
		}
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
		registerPprof(router)
	}

	return s
}

// registerPprof mounts the net/http/pprof handlers under /debug/pprof. Only
// wired in debug mode: the profile/trace endpoints run for tens of seconds and
// expose internals, so they must not be reachable on a production port.
func registerPprof(router *gin.Engine) {
	dbg := router.Group("/debug/pprof")
	dbg.GET("/", gin.WrapF(pprof.Index))
	dbg.GET("/cmdline", gin.WrapF(pprof.Cmdline))
	dbg.GET("/profile", gin.WrapF(pprof.Profile))
	dbg.GET("/symbol", gin.WrapF(pprof.Symbol))
	dbg.POST("/symbol", gin.WrapF(pprof.Symbol))
	dbg.GET("/trace", gin.WrapF(pprof.Trace))
	for _, p := range []string{"allocs", "block", "goroutine", "heap", "mutex", "threadcreate"} {
		dbg.GET("/"+p, gin.WrapH(pprof.Handler(p)))
	}
}

// SetConfigSection overrides the config section the server reads (default
// "http"). Named servers use it to read their own [http.<name>] section so
// several servers can bind different ports.
func (s *Server) SetConfigSection(section string) { s.section = section }

// ReadConfig implements configx.Configurable, reading the server's config section
// (default "http") so the server owns its configuration independently of
// host.Config.
func (s *Server) ReadConfig(l configx.Reader) error {
	section := s.section
	if section == "" {
		section = "http"
	}
	l.SetDefault(section+".host", "localhost")
	l.SetDefault(section+".port", 8080)
	l.SetDefault(section+".readTimeout", "10s")
	l.SetDefault(section+".readHeaderTimeout", "5s")
	l.SetDefault(section+".writeTimeout", "10s")
	l.SetDefault(section+".idleTimeout", "60s")
	l.SetDefault(section+".maxHeaderBytes", 1<<20) // 1 MiB
	return l.Read(section, &s.config)
}

// Init implements core.Initer. It builds the underlying http.Server but does not
// begin listening, leaving a window for setup work (route registration) before
// the server serves. Call Start (the host does this in its start phase) to serve.
func (s *Server) Init() error {
	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", s.config.Host, s.config.Port),
		Handler: s.Router,
		// Timeouts and MaxHeaderBytes are populated from [http] config, whose
		// defaults ReadConfig sets (10s/5s/10s/60s, 1 MiB). A zero value disables
		// the corresponding timeout; net/http applies its own 1 MiB default when
		// MaxHeaderBytes is 0.
		ReadTimeout:       s.config.ReadTimeout,
		ReadHeaderTimeout: s.config.ReadHeaderTimeout,
		WriteTimeout:      s.config.WriteTimeout,
		IdleTimeout:       s.config.IdleTimeout,
		MaxHeaderBytes:    s.config.MaxHeaderBytes,
	}
	return nil
}

// Start implements core.Starter. It begins serving in a background goroutine; the
// outcome is available via ErrCh once the server stops (nil on clean shutdown,
// error otherwise). Init must have run first.
func (s *Server) Start() error {
	if s.httpServer == nil {
		return errorx.NewInternalError("http", "http server not initialized; call Init before Start")
	}
	s.logger.Info("starting HTTP server", "addr", s.Addr(), "tls", s.config.CertFile != "" && s.config.KeyFile != "")
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
	var err error
	if s.config.CertFile != "" && s.config.KeyFile != "" {
		err = s.httpServer.ListenAndServeTLS(s.config.CertFile, s.config.KeyFile)
	} else {
		err = s.httpServer.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
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
