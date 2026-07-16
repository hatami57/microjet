package outbox

// This file holds the Postgres-backed proof that the relay is replica-safe: two
// relays draining one outbox table must publish every message exactly once. It
// needs a real Postgres (SKIP LOCKED is a no-op on SQLite), so it is gated behind
// the MICROJET_TEST_POSTGRES_DSN environment variable and skips when unset — the
// SQLite tests in outbox_test.go cover the lock-free path in CI.
//
// Run it locally against a throwaway database, e.g.:
//
//	docker run --rm -e POSTGRES_PASSWORD=postgres -p 5432:5432 postgres:16
//	MICROJET_TEST_POSTGRES_DSN='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable' \
//	    go test -race -run TestRelayConcurrentPostgres ./outbox/
//
// The test drops and recreates the outbox_messages table, so point it at a
// database you do not mind clobbering.

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/hatami57/microjet/messaging"
	gormpg "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// recordingPublisher records every published payload under a mutex so two relays
// can publish through it concurrently; the counts expose any double-publish.
type recordingPublisher struct {
	mu   sync.Mutex
	seen map[string]int
}

func (p *recordingPublisher) Publish(_ context.Context, msg messaging.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seen[string(msg.Data)]++
	return nil
}

func openPostgres(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(gormpg.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	return db
}

func TestRelayConcurrentPostgres(t *testing.T) {
	dsn := os.Getenv("MICROJET_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("MICROJET_TEST_POSTGRES_DSN not set; skipping Postgres concurrency test")
	}

	ctx := context.Background()

	// Two independent connections mirror two replicas each running their own relay.
	dbA := openPostgres(t, dsn)
	dbB := openPostgres(t, dsn)

	// Start from a clean table so the run is deterministic.
	if err := dbA.Migrator().DropTable(&Message{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	if err := Migrate(dbA); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	const total = 300
	for i := range total {
		m := Message{Subject: "evt", Payload: []byte(fmt.Sprintf("msg-%d", i))}
		if err := dbA.WithContext(ctx).Create(&m).Error; err != nil {
			t.Fatalf("seed message %d: %v", i, err)
		}
	}

	pub := &recordingPublisher{seen: make(map[string]int)}
	// A small batch size forces the two relays to interleave many passes rather
	// than one relay claiming everything in a single sweep.
	relayA := NewRelay(dbA, pub, WithBatchSize(7))
	relayB := NewRelay(dbB, pub, WithBatchSize(7))
	if !relayA.locking || !relayB.locking {
		t.Fatalf("expected relays over postgres to use the locking path")
	}

	// drainAll runs passes until the whole table is drained, returning how many
	// messages this relay published. A zero-count pass means the peer holds the
	// remaining rows (SKIP LOCKED skipped them), so it loops until nothing is left.
	drainAll := func(relay *Relay) (int, error) {
		published := 0
		for {
			n, err := relay.PublishPending(ctx)
			if err != nil {
				return published, err
			}
			published += n
			var pending int64
			if err := dbA.WithContext(ctx).Model(&Message{}).
				Where("published_at IS NULL").Count(&pending).Error; err != nil {
				return published, err
			}
			if pending == 0 {
				return published, nil
			}
		}
	}

	var (
		wg         sync.WaitGroup
		countA     int
		countB     int
		errA, errB error
	)
	wg.Add(2)
	go func() { defer wg.Done(); countA, errA = drainAll(relayA) }()
	go func() { defer wg.Done(); countB, errB = drainAll(relayB) }()
	wg.Wait()

	if errA != nil || errB != nil {
		t.Fatalf("drain errors: A=%v B=%v", errA, errB)
	}

	// Exactly-once: every seeded payload was published, and none was published twice.
	pub.mu.Lock()
	defer pub.mu.Unlock()
	if len(pub.seen) != total {
		t.Errorf("distinct published = %d, want %d", len(pub.seen), total)
	}
	for i := range total {
		key := fmt.Sprintf("msg-%d", i)
		switch pub.seen[key] {
		case 1:
			// exactly once, as required
		case 0:
			t.Errorf("message %q was never published", key)
		default:
			t.Errorf("message %q was published %d times (double-publish)", key, pub.seen[key])
		}
	}
	if countA+countB != total {
		t.Errorf("relay published counts sum to %d, want %d", countA+countB, total)
	}
	// Both relays must have partitioned the work, not one starving the other.
	if countA == 0 || countB == 0 {
		t.Errorf("expected both relays to make progress, got A=%d B=%d", countA, countB)
	}
}
