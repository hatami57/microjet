package outbox

import (
	"context"
	"log/slog"
	"time"

	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/core/errorx"
	"github.com/hatami57/microjet/core/jsonx"
	"github.com/hatami57/microjet/gormx"
	"github.com/hatami57/microjet/messaging"
	"gorm.io/gorm"
)

// DefaultBatchSize bounds how many pending messages a single relay pass drains.
const DefaultBatchSize = 100

// Relay drains the outbox table to a messaging.Publisher. Delivery is
// at-least-once: a message is marked published only after a successful publish,
// so a crash in between simply re-delivers on the next pass. Run a single relay
// (or coordinate with leader election) — concurrent relays may double-publish.
type Relay struct {
	table       *gormx.Table[Message]
	publisher   messaging.Publisher
	batchSize   int
	maxAttempts int
	retention   time.Duration
	logger      *slog.Logger
	clock       core.TimeProvider
}

// RelayOption configures a Relay.
type RelayOption func(*Relay)

// WithBatchSize overrides how many pending messages are drained per pass.
func WithBatchSize(n int) RelayOption {
	return func(r *Relay) {
		if n > 0 {
			r.batchSize = n
		}
	}
}

// WithLogger sets the logger used for relay progress and per-message failures.
func WithLogger(l *slog.Logger) RelayOption {
	return func(r *Relay) {
		if l != nil {
			r.logger = l
		}
	}
}

// WithClock sets the time source used to stamp published_at, so the relay can be
// made deterministic in tests. Defaults to core.UTC.
func WithClock(c core.TimeProvider) RelayOption {
	return func(r *Relay) {
		if c != nil {
			r.clock = c
		}
	}
}

// WithMaxAttempts quarantines a message once it has failed to publish n times, so
// a permanently-failing ("poison") message stops being retried on every pass and
// can be found (FailedAt set, with LastError). n <= 0 (the default) retries
// indefinitely.
func WithMaxAttempts(n int) RelayOption {
	return func(r *Relay) {
		if n > 0 {
			r.maxAttempts = n
		}
	}
}

// WithRetention deletes messages published longer than d ago at the end of each
// pass, keeping the table bounded. d <= 0 (the default) keeps published rows
// forever.
func WithRetention(d time.Duration) RelayOption {
	return func(r *Relay) {
		if d > 0 {
			r.retention = d
		}
	}
}

// NewRelay builds a Relay over db and publisher.
func NewRelay(db *gorm.DB, publisher messaging.Publisher, opts ...RelayOption) *Relay {
	r := &Relay{table: gormx.NewTable[Message](db), publisher: publisher, batchSize: DefaultBatchSize, logger: slog.Default(), clock: core.UTC}
	for _, o := range opts {
		o(r)
	}
	return r
}

// PublishPending publishes up to the configured batch size of unpublished,
// non-quarantined messages in insertion order and returns how many were
// published. A publish failure is recorded (attempts incremented, last error
// stored) and the message is left for a later pass — or quarantined once it hits
// WithMaxAttempts; either way the relay continues with the rest of the batch
// rather than blocking on one bad message, so strict ordering is not preserved
// across failures.
func (r *Relay) PublishPending(ctx context.Context) (int, error) {
	rows, err := r.table.
		Where("published_at IS NULL AND failed_at IS NULL").
		Order("id ASC").
		Limit(r.batchSize).
		ListAll(ctx)
	if err != nil {
		return 0, errorx.NewInternalError("outbox", "loading pending messages failed").WithInner(err)
	}

	published := 0
	for _, m := range rows {
		if err := ctx.Err(); err != nil {
			return published, err
		}
		msg := messaging.Message{Subject: m.Subject, Data: m.Payload}
		if len(m.Headers) > 0 {
			if err := jsonx.FromJSON(string(m.Headers), &msg.Headers); err != nil {
				r.logger.Warn("outbox: skipping message with bad headers", "id", m.ID, "error", err)
				r.recordFailure(ctx, m, err)
				continue
			}
		}
		if err := r.publisher.Publish(ctx, msg); err != nil {
			r.logger.Warn("outbox: publish failed", "id", m.ID, "subject", m.Subject, "error", err)
			r.recordFailure(ctx, m, err)
			continue
		}
		now := r.clock.Now()
		if _, err := r.table.UpdateMap(ctx, map[string]any{"published_at": now}, "id = ?", m.ID); err != nil {
			// Published but not marked: it will be re-delivered next pass
			// (at-least-once). Surface so the operator notices a persistent issue.
			r.logger.Error("outbox: marking message published failed", "id", m.ID, "error", err)
			continue
		}
		published++
	}
	if published > 0 {
		r.logger.Debug("outbox: relayed messages", "count", published)
	}
	return published, nil
}

// recordFailure increments the attempt counter and stores the error message,
// leaving published_at NULL so the message is retried. Once the attempt count
// reaches WithMaxAttempts it also stamps failed_at, quarantining the message so it
// is no longer picked up.
func (r *Relay) recordFailure(ctx context.Context, m *Message, cause error) {
	attempts := m.Attempts + 1
	values := map[string]any{"attempts": attempts, "last_error": cause.Error()}
	if r.maxAttempts > 0 && attempts >= r.maxAttempts {
		values["failed_at"] = r.clock.Now()
		r.logger.Error("outbox: message quarantined after repeated failures",
			"id", m.ID, "subject", m.Subject, "attempts", attempts, "error", cause.Error())
	}
	if _, err := r.table.UpdateMap(ctx, values, "id = ?", m.ID); err != nil {
		r.logger.Error("outbox: recording failure failed", "id", m.ID, "error", err)
	}
}

// PrunePublished deletes messages published longer than the configured retention
// ago, keeping the table bounded, and returns how many rows were removed. It is a
// no-op when retention is unset. Quarantined (failed_at) rows are kept for
// inspection.
func (r *Relay) PrunePublished(ctx context.Context) (int64, error) {
	if r.retention <= 0 {
		return 0, nil
	}
	cutoff := r.clock.Now().Add(-r.retention)
	const cond = "published_at IS NOT NULL AND published_at < ?"
	n, err := r.table.Count(ctx, cond, cutoff)
	if err != nil {
		return 0, errorx.NewInternalError("outbox", "counting prunable messages failed").WithInner(err)
	}
	if n == 0 {
		return 0, nil
	}
	if err := r.table.Delete(ctx, cond, cutoff); err != nil {
		return 0, errorx.NewInternalError("outbox", "pruning published messages failed").WithInner(err)
	}
	return n, nil
}
