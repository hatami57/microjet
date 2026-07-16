package outbox

import (
	"context"
	"errors"
	"testing"
	"time"

	glebarez "github.com/glebarez/sqlite"
	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/gormx"
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

// enqueueTx records a message through the gormx-native Enqueuer inside a gormx
// transaction, exercising the same context-threaded join callers use.
func enqueueTx(t *testing.T, db *gorm.DB, msg messaging.Message) {
	t.Helper()
	base := gormx.NewBaseRepository(db)
	if err := base.RunTx(context.Background(), func(ctx context.Context) error {
		return NewEnqueuer(db).Enqueue(ctx, msg)
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
}

// enqueueJSONTx is the JSON companion to enqueueTx.
func enqueueJSONTx(t *testing.T, db *gorm.DB, subject string, payload any) {
	t.Helper()
	base := gormx.NewBaseRepository(db)
	if err := base.RunTx(context.Background(), func(ctx context.Context) error {
		return NewEnqueuer(db).EnqueueJSON(ctx, subject, payload)
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
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

func TestDialectSupportsLocking(t *testing.T) {
	cases := map[string]bool{
		"postgres":  true,
		"mysql":     true,
		"sqlite":    false,
		"sqlserver": false,
		"":          false,
	}
	for dialect, want := range cases {
		if got := dialectSupportsLocking(dialect); got != want {
			t.Errorf("dialectSupportsLocking(%q) = %v, want %v", dialect, got, want)
		}
	}
}

// TestNewRelayDialectGating verifies the relay picks its concurrency strategy
// from the connection dialect: the sqlite test DB must take the lock-free path.
func TestNewRelayDialectGating(t *testing.T) {
	db := openDB(t)
	relay := NewRelay(db, &fakePublisher{})
	if relay.locking {
		t.Errorf("relay over sqlite has locking=true, want false (no SKIP LOCKED)")
	}
}

func TestEnqueueAndRelay(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)

	enqueueJSONTx(t, db, "orders.created", map[string]any{"id": 1})
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
	enqueueTx(t, db, messaging.Message{Subject: "evt", Data: []byte(`{}`), Headers: headers})

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

	enqueueJSONTx(t, db, "flaky", map[string]any{"n": 1})

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
		enqueueJSONTx(t, db, "bulk", map[string]any{"i": i})
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

func TestEnqueuePropagatesCorrelationContext(t *testing.T) {
	db := openDB(t)
	ctx := core.ContextWithCorrelationID(context.Background(), "corr-42")

	// The correlation id lives only on the enqueue-time context; the relay later
	// publishes from a bare background context, so the id survives only if the
	// enqueuer captured it.
	base := gormx.NewBaseRepository(db)
	if err := base.RunTx(ctx, func(ctx context.Context) error {
		return NewEnqueuer(db).EnqueueJSON(ctx, "orders.created", map[string]any{"id": 1})
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	pub := &fakePublisher{}
	if _, err := NewRelay(db, pub).PublishPending(context.Background()); err != nil {
		t.Fatalf("PublishPending: %v", err)
	}
	if len(pub.published) != 1 {
		t.Fatalf("published = %d, want 1", len(pub.published))
	}
	if got := messaging.CorrelationID(pub.published[0].Headers); got != "corr-42" {
		t.Errorf("correlation header = %q, want corr-42", got)
	}
}

func TestEnqueueSignalsRelay(t *testing.T) {
	db := openDB(t)
	notify := make(chan struct{}, 1)
	enq := &Enqueuer{table: gormx.NewTable[Message](db), notify: notify}

	base := gormx.NewBaseRepository(db)
	if err := base.RunTx(context.Background(), func(ctx context.Context) error {
		return enq.EnqueueJSON(ctx, "evt", map[string]any{"n": 1})
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	select {
	case <-notify:
	default:
		t.Error("expected enqueue to signal the relay")
	}
}

func TestRelayQuarantinesAfterMaxAttempts(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	enqueueJSONTx(t, db, "poison", map[string]any{"n": 1})

	pub := &fakePublisher{failOn: map[string]bool{"poison": true}}
	relay := NewRelay(db, pub, WithMaxAttempts(2))

	// Attempt 1 fails: still pending, one attempt recorded.
	if n, err := relay.PublishPending(ctx); err != nil || n != 0 {
		t.Fatalf("pass 1 n=%d err=%v, want 0/nil", n, err)
	}
	if got := countPending(t, db); got != 1 {
		t.Errorf("pending after pass 1 = %d, want 1", got)
	}

	// Attempt 2 reaches the limit: the message is quarantined (FailedAt set).
	if n, err := relay.PublishPending(ctx); err != nil || n != 0 {
		t.Fatalf("pass 2 n=%d err=%v, want 0/nil", n, err)
	}
	var row Message
	if err := db.First(&row).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if row.FailedAt == nil {
		t.Error("expected message to be quarantined (FailedAt set)")
	}
	if row.Attempts != 2 {
		t.Errorf("attempts = %d, want 2", row.Attempts)
	}

	// Even after the broker recovers, a quarantined message is never retried.
	pub.failOn = nil
	if n, err := relay.PublishPending(ctx); err != nil || n != 0 {
		t.Fatalf("pass 3 n=%d err=%v, want 0 (quarantined)", n, err)
	}
	if len(pub.published) != 0 {
		t.Errorf("published %d, want 0 (quarantined stays put)", len(pub.published))
	}
}

func TestRelayPrunesPublished(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	enqueueJSONTx(t, db, "keep", map[string]any{"n": 1})

	clock := core.NewFixedClock(time.Now())
	relay := NewRelay(db, &fakePublisher{}, WithRetention(time.Hour), WithClock(clock))

	if n, err := relay.PublishPending(ctx); err != nil || n != 1 {
		t.Fatalf("publish n=%d err=%v, want 1/nil", n, err)
	}
	// Freshly published: inside the retention window, so nothing is pruned.
	if n, err := relay.PrunePublished(ctx); err != nil || n != 0 {
		t.Fatalf("early prune n=%d err=%v, want 0/nil", n, err)
	}
	// Advance past the window: the published row is removed.
	clock.Advance(2 * time.Hour)
	if n, err := relay.PrunePublished(ctx); err != nil || n != 1 {
		t.Fatalf("prune n=%d err=%v, want 1/nil", n, err)
	}
	var total int64
	if err := db.Model(&Message{}).Count(&total).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if total != 0 {
		t.Errorf("rows after prune = %d, want 0", total)
	}
}
