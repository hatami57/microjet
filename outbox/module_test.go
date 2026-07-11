package outbox

import (
	"context"
	"testing"

	"github.com/hatami57/microjet/gormx"
	"github.com/hatami57/microjet/host"
	"github.com/hatami57/microjet/messaging"
)

func newApp(t *testing.T) *host.App {
	t.Helper()
	app, err := host.New()
	if err != nil {
		t.Fatalf("host.New: %v", err)
	}
	return app
}

func newRelayService() *relayService {
	return &relayService{cfg: config{dbName: gormx.DefaultDatabase, batchSize: DefaultBatchSize}, enq: &Enqueuer{}}
}

func TestWireMigratesTableAndBuildsRelay(t *testing.T) {
	db := openDB(t)
	app := newApp(t)
	svc := newRelayService()
	if err := svc.wire(app, db, &fakePublisher{}); err != nil {
		t.Fatalf("wire: %v", err)
	}
	if !db.Migrator().HasTable("outbox_messages") {
		t.Fatal("outbox table was not migrated")
	}
	if svc.enq.table == nil {
		t.Fatal("wire did not connect the enqueuer table")
	}
	if svc.relay == nil {
		t.Fatal("wire did not build the relay")
	}
}

func TestSetupWithoutDatabaseFails(t *testing.T) {
	app := newApp(t)
	if err := newRelayService().Setup(app); err == nil {
		t.Error("expected Setup to fail without a database installed")
	}
}

func TestSetupWithoutMessagingFails(t *testing.T) {
	db := openDB(t)
	app := newApp(t)
	app.WithModule(gormx.Inject(db))
	if err := app.Err(); err != nil {
		t.Fatalf("inject db: %v", err)
	}
	if err := newRelayService().Setup(app); err == nil {
		t.Error("expected Setup to fail without a messaging client installed")
	}
}

func TestEnqueueBeforeSetupFails(t *testing.T) {
	if err := (&Enqueuer{}).Enqueue(context.Background(), messaging.Message{Subject: "x"}); err == nil {
		t.Error("expected Enqueue on an unwired enqueuer to fail")
	}
}
