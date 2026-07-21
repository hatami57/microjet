package gormx

import (
	"log/slog"
	"testing"
	"time"

	glebarez "github.com/glebarez/sqlite"
	"github.com/hatami57/microjet/core"
	"gorm.io/gorm"
)

// memDriver is a test Driver that opens a fresh in-memory SQLite, ignoring the
// resolved config. It lets the Service lifecycle (Init, NowFunc wiring) be
// exercised without a real database.
type memDriver struct{}

func (memDriver) Open(_ Config, _ *slog.Logger) (*gorm.DB, error) {
	return gorm.Open(glebarez.Open(":memory:"), &gorm.Config{})
}

// clockRow carries the timestamp columns GORM auto-populates from NowFunc.
type clockRow struct {
	ID        int `gorm:"primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TestInitWiresClockIntoNowFunc verifies that a clock set via SetClock drives
// GORM's CreatedAt/UpdatedAt stamping through the injected TimeProvider.
func TestInitWiresClockIntoNowFunc(t *testing.T) {
	fixed := core.NewFixedClock(time.Date(2026, 7, 21, 10, 30, 0, 0, time.UTC))

	svc := NewService("test", "database", memDriver{})
	svc.SetClock(fixed)
	if err := svc.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	db := svc.DB()
	if err := db.AutoMigrate(&clockRow{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	row := &clockRow{}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if !row.CreatedAt.Equal(fixed.Now()) {
		t.Errorf("CreatedAt = %v, want %v", row.CreatedAt, fixed.Now())
	}
	if !row.UpdatedAt.Equal(fixed.Now()) {
		t.Errorf("UpdatedAt = %v, want %v", row.UpdatedAt, fixed.Now())
	}

	// Advancing the clock is reflected on the next write, proving NowFunc is read
	// per operation rather than captured once.
	fixed.Advance(time.Hour)
	next := &clockRow{}
	if err := db.Create(next).Error; err != nil {
		t.Fatalf("create after advance: %v", err)
	}
	if !next.CreatedAt.Equal(fixed.Now()) {
		t.Errorf("CreatedAt after advance = %v, want %v", next.CreatedAt, fixed.Now())
	}
}

// TestSetClockOnInjectedDB verifies that SetClock takes effect immediately for an
// already-open (injected) connection, whose Init is a no-op.
func TestSetClockOnInjectedDB(t *testing.T) {
	fixed := core.NewFixedClock(time.Date(2026, 7, 21, 10, 30, 0, 0, time.UTC))

	db, err := gorm.Open(glebarez.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	svc := NewServiceFromDB("test", db)
	svc.SetClock(fixed)
	if err := svc.Init(); err != nil { // no-op for injected, but must not clear NowFunc
		t.Fatalf("init: %v", err)
	}
	if err := db.AutoMigrate(&clockRow{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	row := &clockRow{}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if !row.CreatedAt.Equal(fixed.Now()) {
		t.Errorf("CreatedAt = %v, want %v", row.CreatedAt, fixed.Now())
	}
}

// TestInitWithoutClockUsesGormDefault verifies that omitting a clock leaves
// GORM's default time source in place (timestamps track wall-clock, not a frozen
// value).
func TestInitWithoutClockUsesGormDefault(t *testing.T) {
	svc := NewService("test", "database", memDriver{})
	if err := svc.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	db := svc.DB()
	if err := db.AutoMigrate(&clockRow{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	before := time.Now().Add(-time.Minute)
	row := &clockRow{}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if row.CreatedAt.Before(before) {
		t.Errorf("CreatedAt = %v, expected roughly now", row.CreatedAt)
	}
}
