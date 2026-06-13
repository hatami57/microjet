package outbox

import (
	"context"
	"errors"
	"testing"

	glebarez "github.com/glebarez/sqlite"
	"github.com/hatami57/microjet/messaging"
	"gorm.io/gorm"
)

func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(glebarez.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// fakePublisher records published messages and can be told to fail.
type fakePublisher struct {
	published []messaging.Message
	failOn    map[string]bool // subjects that should fail
}

func (p *fakePublisher) Publish(_ context.Context, msg messaging.Message) error {
	if p.failOn[msg.Subject] {
		return errors.New("broker down")
	}
	p.published = append(p.published, msg)
	return nil
}

func countPending(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&Message{}).Where("published_at IS NULL").Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestEnqueueAndRelay(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)

	err := db.Transaction(func(tx *gorm.DB) error {
		return EnqueueJSON(tx, "orders.created", map[string]any{"id": 1})
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if got := countPending(t, db); got != 1 {
		t.Fatalf("pending = %d, want 1", got)
	}

	pub := &fakePublisher{}
	relay := NewRelay(db, pub)
	n, err := relay.PublishPending(ctx)
	if err != nil {
		t.Fatalf("PublishPending: %v", err)
	}
	if n != 1 {
		t.Errorf("published count = %d, want 1", n)
	}
	if len(pub.published) != 1 || pub.published[0].Subject != "orders.created" {
		t.Errorf("published = %+v", pub.published)
	}
	if got := countPending(t, db); got != 0 {
		t.Errorf("pending after relay = %d, want 0", got)
	}
}

func TestEnqueuePreservesHeaders(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)

	headers := messaging.SetCorrelationID(nil, "corr-9")
	if err := db.Transaction(func(tx *gorm.DB) error {
		return Enqueue(tx, messaging.Message{Subject: "evt", Data: []byte(`{}`), Headers: headers})
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	pub := &fakePublisher{}
	if _, err := NewRelay(db, pub).PublishPending(ctx); err != nil {
		t.Fatalf("PublishPending: %v", err)
	}
	if len(pub.published) != 1 {
		t.Fatalf("published = %d, want 1", len(pub.published))
	}
	if got := messaging.CorrelationID(pub.published[0].Headers); got != "corr-9" {
		t.Errorf("correlation header = %q, want corr-9", got)
	}
}

func TestRelayRecordsFailureAndRetries(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)

	if err := db.Transaction(func(tx *gorm.DB) error {
		return EnqueueJSON(tx, "flaky", map[string]any{"n": 1})
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// First pass: publisher fails, message stays pending with an attempt recorded.
	pub := &fakePublisher{failOn: map[string]bool{"flaky": true}}
	relay := NewRelay(db, pub)
	if n, err := relay.PublishPending(ctx); err != nil || n != 0 {
		t.Fatalf("first pass n=%d err=%v, want 0/nil", n, err)
	}
	if got := countPending(t, db); got != 1 {
		t.Errorf("pending after failure = %d, want 1", got)
	}
	var row Message
	if err := db.First(&row).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if row.Attempts != 1 || row.LastError == "" {
		t.Errorf("attempts=%d lastError=%q, want 1/non-empty", row.Attempts, row.LastError)
	}

	// Second pass: broker recovers, message is delivered.
	pub.failOn = nil
	if n, err := relay.PublishPending(ctx); err != nil || n != 1 {
		t.Fatalf("second pass n=%d err=%v, want 1/nil", n, err)
	}
	if got := countPending(t, db); got != 0 {
		t.Errorf("pending after recovery = %d, want 0", got)
	}
}

func TestRelayBatchSizeLimitsPass(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	for i := range 5 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			return EnqueueJSON(tx, "bulk", map[string]any{"i": i})
		}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	pub := &fakePublisher{}
	relay := NewRelay(db, pub, WithBatchSize(2))
	n, err := relay.PublishPending(ctx)
	if err != nil {
		t.Fatalf("PublishPending: %v", err)
	}
	if n != 2 {
		t.Errorf("published = %d, want 2 (batch limit)", n)
	}
	if got := countPending(t, db); got != 3 {
		t.Errorf("pending = %d, want 3", got)
	}
}
