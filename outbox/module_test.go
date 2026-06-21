package outbox

import (
	"context"
	"testing"

	"github.com/hatami57/microjet/gormx"
	"github.com/hatami57/microjet/host"
)

func newApp(t *testing.T) *host.App {
	t.Helper()
	app, err := host.New()
	if err != nil {
		t.Fatalf("host.New: %v", err)
	}
	return app
}

func TestMigrateTableCreatesTable(t *testing.T) {
	db := openDB(t)
	app := newApp(t)
	app.WithModule(gormx.Inject(db))
	if err := app.Err(); err != nil {
		t.Fatalf("inject db: %v", err)
	}
	if err := migrateTable(app, gormx.DefaultDatabase); err != nil {
		t.Fatalf("migrateTable: %v", err)
	}
	if !db.Migrator().HasTable("outbox_messages") {
		t.Fatal("outbox table was not migrated")
	}
}

func TestMigrateTableWithoutDatabaseFails(t *testing.T) {
	app := newApp(t)
	if err := migrateTable(app, gormx.DefaultDatabase); err == nil {
		t.Error("expected migrateTable to fail without a database installed")
	}
}

func TestRelayOnceWithoutMessagingFails(t *testing.T) {
	db := openDB(t)
	app := newApp(t)
	app.WithModule(gormx.Inject(db))
	err := relayOnce(context.Background(), app, config{dbName: gormx.DefaultDatabase, batchSize: DefaultBatchSize})
	if err == nil {
		t.Error("expected relayOnce to fail without a messaging client")
	}
}
