package migrate

import (
	"context"
	"embed"
	"testing"

	glebarez "github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

//go:embed testdata/migrations/*.sql
var migrationsFS embed.FS

func openSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(glebarez.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

func TestUpAppliesAllMigrations(t *testing.T) {
	ctx := context.Background()
	db := openSQLite(t)

	if err := Up(ctx, db, migrationsFS, WithDir("testdata/migrations")); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if !db.Migrator().HasTable("widgets") {
		t.Fatal("widgets table was not created")
	}
	if !db.Migrator().HasColumn("widgets", "color") {
		t.Error("second migration (color column) was not applied")
	}

	m, err := New(db, migrationsFS, WithDir("testdata/migrations"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	v, err := m.Version(ctx)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != 2 {
		t.Errorf("version = %d, want 2", v)
	}
}

func TestUpIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := openSQLite(t)
	for i := 0; i < 2; i++ {
		if err := Up(ctx, db, migrationsFS, WithDir("testdata/migrations")); err != nil {
			t.Fatalf("Up pass %d: %v", i, err)
		}
	}
}

func TestDownRollsBackOne(t *testing.T) {
	ctx := context.Background()
	db := openSQLite(t)
	m, err := New(db, migrationsFS, WithDir("testdata/migrations"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := m.Down(ctx); err != nil {
		t.Fatalf("Down: %v", err)
	}
	v, err := m.Version(ctx)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != 1 {
		t.Errorf("version after one rollback = %d, want 1", v)
	}
	if db.Migrator().HasColumn("widgets", "color") {
		t.Error("color column should have been rolled back")
	}
}

func TestUnsupportedDialect(t *testing.T) {
	// A gorm DB whose dialector reports an unknown name should be rejected.
	db := openSQLite(t)
	db.Config.Dialector = fakeDialector{glebarez.Open(":memory:")}
	if _, err := New(db, migrationsFS, WithDir("testdata/migrations")); err == nil {
		t.Error("expected an error for an unsupported dialect")
	}
}

// fakeDialector wraps a real dialector but reports an unsupported name.
type fakeDialector struct{ gorm.Dialector }

func (fakeDialector) Name() string { return "oracle" }
