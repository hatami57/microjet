package host

import (
	"context"
	"log/slog"
	"strings"

	"github.com/hatami57/microjet/cache"
	"github.com/hatami57/microjet/core"
)

// cacheService implements core.Configurable, core.Initer, and core.Closer, and
// reports readiness via Healthy. A pre-injected client (WithCacheClient) skips
// config loading and Init.
type cacheService struct {
	config     cache.Config
	logger     *slog.Logger
	cache      cache.Cache
	configured bool // true = client/config already set, skip LoadConfig
}

func (s *cacheService) LoadConfig(l *core.ConfigLoader) error {
	if s.configured {
		return nil
	}
	return l.UnmarshalKey("cache", &s.config)
}

func (s *cacheService) Init() error {
	if s.cache != nil {
		return nil
	}
	c, err := cache.New(context.Background(), s.config)
	if err != nil {
		return err
	}
	s.cache = c
	driver := strings.ToLower(strings.TrimSpace(s.config.Driver))
	if driver == "" {
		driver = "memory"
	}
	s.logger.Info("cache ready", "driver", driver)
	return nil
}

func (s *cacheService) Close() error {
	if s.cache == nil {
		return nil
	}
	return s.cache.Close()
}

// Healthy implements core.HealthChecker, delegating to the underlying cache when
// it supports a health check (RedisCache does; MemoryCache is always healthy).
func (s *cacheService) Healthy(ctx context.Context) error {
	if s.cache == nil {
		return nil
	}
	if hc, ok := s.cache.(interface {
		Healthy(context.Context) error
	}); ok {
		return hc.Healthy(ctx)
	}
	return nil
}

// Cache returns the configured cache, or nil if WithCache/WithCacheClient was
// not called. It is available after services are initialized (Run, StartHTTP, or
// InitServices); during the fluent chain it returns nil for the deferred path.
func (a *App) Cache() cache.Cache {
	if svc, ok := ResolveService[*cacheService](a); ok {
		return svc.cache
	}
	return nil
}

// WithCache registers a cache service that loads its config from [cache] and
// connects at Init time. Driver "memory" (default) or "redis". The connection is
// closed automatically on shutdown. Errors are deferred to Run/MustRun/Err.
func (a *App) WithCache() *App {
	if a.err != nil {
		return a
	}
	ProvideService(a, &cacheService{logger: a.Logger})
	return a
}

// WithCacheClient injects a pre-built cache.Cache (e.g. a MemoryCache in tests or
// a custom implementation), bypassing config loading and Init. It is still closed
// by the lifecycle on shutdown.
func (a *App) WithCacheClient(c cache.Cache) *App {
	if a.err != nil {
		return a
	}
	ProvideService(a, &cacheService{logger: a.Logger, cache: c, configured: true})
	return a
}
