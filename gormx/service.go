package gormx

import (
	"context"
	"log/slog"

	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/core/configx"
	"github.com/hatami57/microjet/core/errorx"
	"gorm.io/gorm"
)

var (
	_ configx.Configurable = (*Service)(nil)
	_ core.Initer          = (*Service)(nil)
	_ core.Closer          = (*Service)(nil)
	_ core.HealthChecker   = (*Service)(nil)
)

// Service manages the lifecycle of a single *gorm.DB connection. It implements
// configx.Configurable, core.Initer, core.Closer, and core.HealthChecker, so the
// host drives config loading, dialing, and shutdown automatically. When a
// connection is provided up front (NewServiceFromDB), config loading and Init are
// no-ops, but Close and health checks still run.
type Service struct {
	name    string // display name used in health-check error messages
	section string // config key, e.g. "database" or "database.secondary"
	driver  Driver
	config  Config
	logger  *slog.Logger
	clock   core.TimeProvider
	db      *gorm.DB
}

// NewService creates a Service that opens a connection via driver during Init.
// name is used in health-check error messages; section is the config key
// (e.g. "database" or "database.secondary").
func NewService(name, section string, driver Driver) *Service {
	return &Service{
		name:    name,
		section: section,
		driver:  driver,
		logger:  slog.Default(),
	}
}

// NewServiceFromDB creates a Service around an already-open *gorm.DB. The
// Service participates in Close and health checks but skips config loading and
// Init.
func NewServiceFromDB(name string, db *gorm.DB) *Service {
	return &Service{name: name, db: db, logger: slog.Default()}
}

// SetLogger replaces the logger. A nil logger is ignored.
func (s *Service) SetLogger(l *slog.Logger) {
	if l != nil {
		s.logger = l
	}
}

// SetClock sets the time source GORM uses to stamp CreatedAt/UpdatedAt, wiring it
// to db.Config.NowFunc. For driver-opened connections it takes effect during
// Init; for an already-open (injected) connection it applies immediately. A nil
// clock is ignored, leaving GORM's default (time.Now).
func (s *Service) SetClock(c core.TimeProvider) {
	if c == nil {
		return
	}
	s.clock = c
	if s.db != nil {
		s.db.Config.NowFunc = c.Now
	}
}

// DB returns the underlying *gorm.DB, nil until Init is called.
func (s *Service) DB() *gorm.DB { return s.db }

// ReadConfig implements configx.Configurable. It is a no-op for injected connections.
func (s *Service) ReadConfig(l configx.Reader) error {
	if s.db != nil {
		return nil
	}
	return l.Read(s.section, &s.config)
}

// Init implements core.Initer. It is a no-op for injected connections.
func (s *Service) Init() error {
	if s.db != nil {
		return nil
	}
	db, err := s.driver.Open(s.config, s.logger)
	if err != nil {
		return err
	}
	// Tracing callbacks are no-ops without a tracer provider (see otelx), so
	// every driver-opened connection is instrumented unconditionally. Injected
	// connections (NewServiceFromDB) are left untouched — their owner decides.
	if err := UseTracing(db); err != nil {
		return err
	}
	// Route GORM's CreatedAt/UpdatedAt stamping through the host's clock so
	// timestamps honor the injected TimeProvider (core.UTC by default) and can be
	// frozen with a FixedClock in tests. NowFunc is read per operation, so setting
	// it after Open takes effect immediately.
	if s.clock != nil {
		db.Config.NowFunc = s.clock.Now
	}
	s.db = db
	return nil
}

// Close implements core.Closer, closing the underlying SQL connection.
func (s *Service) Close() error {
	if s.db == nil {
		return nil
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Healthy implements core.HealthChecker, pinging the underlying connection so
// the host's /readyz can report the database. A self-describing error names the
// database for multi-database setups.
func (s *Service) Healthy(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	name := s.name
	if name == "" {
		name = "default"
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		return errorx.NewInternalError("gormx", "database handle unavailable", "db", name).WithInner(err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return errorx.NewInternalError("gormx", "database ping failed", "db", name).WithInner(err)
	}
	return nil
}
