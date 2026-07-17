// Package grpcx provides a managed gRPC server with the same operational
// surface as httpx: a default interceptor stack (recovery, request-id, logging,
// errorx-to-status), OpenTelemetry tracing that is a no-op until otelx is
// installed, a grpc_health_v1 health service wired to the app's readiness
// checks, and server reflection in debug mode. It also ships a Dial helper that
// installs the matching client interceptors.
package grpcx

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hatami57/microjet/core/configx"
	"github.com/hatami57/microjet/core/errorx"
	"github.com/hatami57/microjet/grpcx/interceptors"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

// ServerConfig is the [grpc] configuration section. A named server reads
// [grpc.<name>] instead so several servers can bind different ports.
type ServerConfig struct {
	Host  string `mapstructure:"host"`
	Port  int    `mapstructure:"port"`
	Debug bool   `mapstructure:"debug"`
}

// healthPollInterval is how often the health service re-runs the readiness
// checks to refresh the served status.
const healthPollInterval = 5 * time.Second

// ReadinessFunc reports whether a dependency is ready to serve traffic, returning
// nil when healthy and an error otherwise. It mirrors httpx.ReadinessFunc.
type ReadinessFunc func(ctx context.Context) error

type namedReadinessCheck struct {
	name string
	fn   ReadinessFunc
}

// Server is a managed gRPC server. Register generated services on the underlying
// *grpc.Server (via Server()) from a Setup hook before it begins serving.
type Server struct {
	config     ServerConfig
	section    string
	logger     *slog.Logger
	grpcServer *grpc.Server
	healthSrv  *health.Server
	listener   net.Listener

	readinessMu     sync.RWMutex
	readinessChecks []namedReadinessCheck

	// notReady forces the health service to NOT_SERVING once shutdown begins; see
	// SetReady and core.ReadinessToggler.
	notReady atomic.Bool

	healthStop chan struct{}
	errCh      chan error
}

// AddReadinessCheck registers a named dependency probe consulted by the health
// service. Safe for concurrent use.
func (s *Server) AddReadinessCheck(name string, fn ReadinessFunc) {
	s.readinessMu.Lock()
	defer s.readinessMu.Unlock()
	s.readinessChecks = append(s.readinessChecks, namedReadinessCheck{name: name, fn: fn})
}

// Server returns the underlying *grpc.Server so application code can register
// generated service implementations, e.g. from a Setup hook:
//
//	pb.RegisterGreeterServer(grpcx.Of(app).Server(), &greeter{})
func (s *Server) Server() *grpc.Server { return s.grpcServer }

// SetReady toggles readiness, implementing core.ReadinessToggler. When set to
// false the health service reports NOT_SERVING immediately (so a gRPC load
// balancer stops routing) while the process keeps draining in-flight RPCs. The
// host flips this off at the start of graceful shutdown.
func (s *Server) SetReady(ready bool) {
	s.notReady.Store(!ready)
	if !ready && s.healthSrv != nil {
		s.healthSrv.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	}
}

func (s *Server) runReadinessChecks(ctx context.Context) bool {
	s.readinessMu.RLock()
	checks := make([]namedReadinessCheck, len(s.readinessChecks))
	copy(checks, s.readinessChecks)
	s.readinessMu.RUnlock()

	for _, ch := range checks {
		if err := ch.fn(ctx); err != nil {
			s.logger.Warn("gRPC readiness check failed", "check", ch.name, "error", err)
			return false
		}
	}
	return true
}

// NewServer builds a Server with the default interceptor stack, the otelgrpc
// stats handler (a no-op until otelx installs a tracer provider), a health
// service, and — in debug mode — server reflection. It does not listen until
// Init and Start run.
func NewServer(cfg ServerConfig, logger *slog.Logger) *Server {
	return &Server{
		config:     cfg,
		logger:     logger,
		healthStop: make(chan struct{}),
		errCh:      make(chan error, 1),
	}
}

// SetConfigSection overrides the config section the server reads (default
// "grpc"). Named servers use it to read their own [grpc.<name>] section.
func (s *Server) SetConfigSection(section string) { s.section = section }

// ReadConfig implements configx.Configurable, reading the server's config
// section (default "grpc") with the standard defaults.
func (s *Server) ReadConfig(l configx.Reader) error {
	section := s.section
	if section == "" {
		section = "grpc"
	}
	l.SetDefault(section+".host", "0.0.0.0")
	l.SetDefault(section+".port", 9090)
	return l.Read(section, &s.config)
}

// Init implements core.Initer. It builds the *grpc.Server with the interceptor
// chain, registers the health service, and — when debug — enables reflection. It
// does not begin listening; register services and call Start to serve.
func (s *Server) Init() error {
	s.grpcServer = grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(
			interceptors.RecoveryUnary(s.logger),
			interceptors.RequestIDUnary(),
			interceptors.LoggingUnary(s.logger),
			interceptors.ErrorUnary(s.config.Debug),
		),
		grpc.ChainStreamInterceptor(
			interceptors.RecoveryStream(s.logger),
			interceptors.RequestIDStream(),
			interceptors.LoggingStream(s.logger),
			interceptors.ErrorStream(s.config.Debug),
		),
	)

	s.healthSrv = health.NewServer()
	grpc_health_v1.RegisterHealthServer(s.grpcServer, s.healthSrv)

	// Reflection lets grpcurl and similar tools discover services without the
	// .proto; gate it behind debug like httpx does Swagger/pprof.
	if s.config.Debug {
		reflection.Register(s.grpcServer)
	}
	return nil
}

// Start implements core.Starter. It binds the listener and serves in a
// background goroutine, whose exit outcome is available via ErrCh. It also starts
// the health updater that keeps the served status in sync with the readiness
// checks. Init must have run first.
func (s *Server) Start() error {
	if s.grpcServer == nil {
		return errorx.NewInternalError("grpc", "gRPC server not initialized; call Init before Start")
	}
	lis, err := net.Listen("tcp", s.Addr())
	if err != nil {
		return errorx.NewInternalError("grpc", "gRPC listen failed", "addr", s.Addr()).WithInner(err)
	}
	s.listener = lis
	s.logger.Info("starting gRPC server", "addr", lis.Addr().String())

	s.updateHealth() // publish an initial status before the first tick
	go s.runHealthUpdater()
	go func() {
		s.errCh <- s.grpcServer.Serve(lis)
	}()
	return nil
}

// runHealthUpdater periodically refreshes the health service's served status
// from the readiness checks until the server closes.
func (s *Server) runHealthUpdater() {
	ticker := time.NewTicker(healthPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.healthStop:
			return
		case <-ticker.C:
			s.updateHealth()
		}
	}
}

// updateHealth runs the readiness checks and sets the overall served status. Once
// readiness has been toggled off (shutdown), it stays NOT_SERVING.
func (s *Server) updateHealth() {
	if s.healthSrv == nil {
		return
	}
	status := grpc_health_v1.HealthCheckResponse_SERVING
	if s.notReady.Load() {
		status = grpc_health_v1.HealthCheckResponse_NOT_SERVING
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if !s.runReadinessChecks(ctx) {
			status = grpc_health_v1.HealthCheckResponse_NOT_SERVING
		}
		cancel()
	}
	s.healthSrv.SetServingStatus("", status)
}

// Close implements core.Closer, gracefully draining in-flight RPCs before
// stopping, bounded by a timeout after which it stops forcefully.
func (s *Server) Close() error {
	if s.grpcServer == nil {
		return nil
	}
	close(s.healthStop)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return s.Stop(ctx)
}

// Stop gracefully stops the server, falling back to a forceful stop if the
// context deadline is reached first (GracefulStop itself takes no context).
func (s *Server) Stop(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.grpcServer.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		s.grpcServer.Stop()
		<-done
		return ctx.Err()
	}
}

// ErrCh returns the channel that receives the server's exit error (nil on a
// clean stop). The host reads this to react to unexpected failures.
func (s *Server) ErrCh() <-chan error { return s.errCh }

// Addr returns the listener address. Before Start it is derived from config;
// after Start it reflects the bound address (so port 0 resolves to the chosen
// port).
func (s *Server) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
}
