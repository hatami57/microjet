package cache

import (
	"context"
	"log/slog"
	"strings"

	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/core/configx"
	"github.com/hatami57/microjet/host"
)

// service implements configx.Configurable, core.Initer, and core.Closer, and
// reports readiness via Healthy. A pre-injected client (ModuleWithClient) skips
// config loading and Init.
type service struct {
	config     Config
	section    string
	logger     *slog.Logger
	clock      core.TimeProvider
	cache      Cache
	configured bool // true = client/config already set, skip ReadConfig
}

func (s *service) ReadConfig(l configx.Reader) error {
	if s.configured {
		return nil
	}
	section := s.section
	if section == "" {
		section = "cache"
	}
	return l.Read(section, &s.config)
}

func (s *service) Init() error {
	if s.cache != nil {
		return nil
	}
	c, err := New(context.Background(), s.config, s.clock)
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

func (s *service) Close() error {
	if s.cache == nil {
		return nil
	}
	return s.cache.Close()
}

// Healthy implements core.HealthChecker, delegating to the underlying cache when
// it supports a health check (RedisCache does; MemoryCache is always healthy).
func (s *service) Healthy(ctx context.Context) error {
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

// Module registers a cache service that loads its config from [cache] and
// connects at Init time. Driver "memory" (default) or "redis". The connection is
// closed automatically on shutdown.
//
//	host.MustNew().WithModule(cache.Module()).MustRun()
//
// Pass an optional name to install several caches side by side; a named cache
// reads its own [cache.<name>] config section and is retrieved with
// cache.Of(app, name). Reach the default cache with cache.Of(app).
func Module(name ...string) host.Module {
	return host.ModuleFunc(func(app *host.App) error {
		section := "cache"
		if n := first(name); n != "" {
			section = "cache." + n
		}
		host.ProvideService(app, &service{logger: app.Logger, clock: app.Clock, section: section}, name...)
		return nil
	})
}

// ModuleWithClient registers a pre-built Cache (e.g. a MemoryCache in tests or a
// custom implementation), bypassing config loading and Init. It is still closed
// by the lifecycle on shutdown. Pass an optional name to register it as a named
// instance.
func ModuleWithClient(c Cache, name ...string) host.Module {
	return host.ModuleFunc(func(app *host.App) error {
		host.ProvideService(app, &service{logger: app.Logger, cache: c, configured: true}, name...)
		return nil
	})
}

// CloseOrder closes the cache late (as a backend), after the edges that use it
// have drained.
func (s *service) CloseOrder() int { return host.CloseBackend }

// Of returns the cache installed by Module/ModuleWithClient under the optional
// name, or nil if none was installed. It is available after services are
// initialized.
func Of(app *host.App, name ...string) Cache {
	if svc, ok := host.ResolveService[*service](app, name...); ok {
		return svc.cache
	}
	return nil
}

// first returns the first name or "" — the default instance.
func first(name []string) string {
	if len(name) > 0 {
		return name[0]
	}
	return ""
}
