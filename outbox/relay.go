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
// so a crash in between simply re-delivers on the next pass.
//
// Concurrency is dialect-dependent. On Postgres and MySQL the relay claims each
// batch with SELECT … FOR UPDATE SKIP LOCKED inside a transaction, so any number
// of relays (one per replica, say) partition the pending set instead of racing
// over it and never publish a message twice — the standard multi-replica outbox.
// On SQLite, and any other dialect without SKIP LOCKED, it keeps the lock-free
// path, which requires a single relay instance: run only one, or coordinate with
// leader election.
//
// On the transactional path the whole batch's published_at marks commit
// atomically and the publish side effects happen while the rows are locked, so
// keep BatchSize modest for slow brokers to bound lock-hold time. A crash after a
// publish but before commit rolls the marks back, so those messages re-deliver on
// the next pass — the same at-least-once guarantee, reached by rollback rather
// than by an un-updated row. (If lock-hold time ever becomes a problem, the
// escape hatch is a claim-then-publish design with a claimed_at/claimed_by lease
// column, which this deliberately avoids at the current scale.)
type Relay struct {
	db          *gorm.DB
	table       *gormx.Table[Message]
	publisher   messaging.Publisher
	batchSize   int
	maxAttempts int
	retention   time.Duration
	logger      *slog.Logger
	clock       core.TimeProvider
	locking     bool // dialect supports FOR UPDATE SKIP LOCKED
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

// NewRelay builds a Relay over db and publisher. The replica-safety strategy is
// chosen from the database dialect: Postgres and MySQL get the transactional
// SKIP LOCKED claim path, everything else the single-instance lock-free path.
func NewRelay(db *gorm.DB, publisher messaging.Publisher, opts ...RelayOption) *Relay {
	r := &Relay{
		db:        db,
		table:     gormx.NewTable[Message](db),
		publisher: publisher,
		batchSize: DefaultBatchSize,
		logger:    slog.Default(),
		clock:     core.UTC,
		locking:   dialectSupportsLocking(db.Dialector.Name()),
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// dialectSupportsLocking reports whether the SQL dialect can claim rows with
// SELECT … FOR UPDATE SKIP LOCKED, the primitive that lets several relays drain
// one outbox table without double-publishing. Postgres and MySQL 8+ do; SQLite
// and others do not, and fall back to the single-instance path.
func dialectSupportsLocking(dialect string) bool {
	return dialect == "postgres" || dialect == "mysql"
}

// PublishPending publishes up to the configured batch size of unpublished,
// non-quarantined messages in insertion order and returns how many were
// published. A publish failure is recorded (attempts incremented, last error
// stored) and the message is left for a later pass — or quarantined once it hits
// WithMaxAttempts; either way the relay continues with the rest of the batch
// rather than blocking on one bad message, so strict ordering is not preserved
// across failures.
//
// On a dialect that supports it the whole pass runs in one transaction that
// claims its batch with FOR UPDATE SKIP LOCKED, so concurrent relays partition
// the pending set; on other dialects it runs the lock-free path and must be the
// only relay (see the Relay doc comment).
func (r *Relay) PublishPending(ctx context.Context) (int, error) {
	if r.locking {
		return r.publishPendingLocked(ctx)
	}
	rows, err := r.pendingRows(ctx, r.table)
	if err != nil {
		return 0, err
	}
	published, err := r.drain(ctx, r.table, rows, false)
	if published > 0 {
		r.logger.Debug("outbox: relayed messages", "count", published)
	}
	return published, err
}

// publishPendingLocked runs one pass inside a transaction, claiming the batch
// with FOR UPDATE SKIP LOCKED so a second relay on the same database skips the
// rows this one holds. Publishing and marking happen inside the transaction, so
// the batch's published_at marks commit atomically. A write error rolls the pass
// back; anything already handed to the broker carries no committed published_at
// and re-delivers next pass, so at-least-once still holds.
func (r *Relay) publishPendingLocked(ctx context.Context) (int, error) {
	published := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txTable := gormx.NewTable[Message](tx)
		rows, err := r.pendingRows(ctx, txTable.LockForUpdateSkipLocked())
		if err != nil {
			return err
		}
		published, err = r.drain(ctx, txTable, rows, true)
		return err
	})
	if err != nil {
		// The transaction rolled back: no published_at mark committed, so report
		// zero even if some rows reached the broker — they re-publish next pass.
		return 0, err
	}
	if published > 0 {
		r.logger.Debug("outbox: relayed messages", "count", published)
	}
	return published, nil
}

// pendingRows loads a batch of unpublished, non-quarantined messages in insertion
// order through table. Pass a table carrying LockForUpdateSkipLocked to claim the
// rows under the caller's transaction.
func (r *Relay) pendingRows(ctx context.Context, table *gormx.Table[Message]) ([]*Message, error) {
	rows, err := table.
		Where("published_at IS NULL AND failed_at IS NULL").
		Order("id ASC").
		Limit(r.batchSize).
		ListAll(ctx)
	if err != nil {
		return nil, errorx.NewInternalError("outbox", "loading pending messages failed").WithInner(err)
	}
	return rows, nil
}

// drain publishes rows through table and returns how many were published-and-
// marked. When stopOnWriteErr is set (the transactional path) a database write
// failure returns immediately so the caller rolls the pass back; otherwise the
// failure has already been logged and the loop moves on, leaving the row pending
// for the next pass (the lock-free at-least-once path). A cancelled context stops
// the loop on either path.
func (r *Relay) drain(ctx context.Context, table *gormx.Table[Message], rows []*Message, stopOnWriteErr bool) (int, error) {
	published := 0
	for _, m := range rows {
		if err := ctx.Err(); err != nil {
			return published, err
		}
		ok, err := r.publishOne(ctx, table, m)
		if err != nil {
			if stopOnWriteErr {
				return published, err
			}
			continue
		}
		if ok {
			published++
		}
	}
	return published, nil
}

// publishOne publishes one message through table and marks it published. It
// returns whether the message was published-and-marked, plus any database write
// error. A publish or bad-header failure is recorded via recordFailure and
// reported as not published; the returned error is non-nil only when a database
// write (the failure record or the published_at mark) itself fails, which the
// caller uses to decide whether to abort the pass.
func (r *Relay) publishOne(ctx context.Context, table *gormx.Table[Message], m *Message) (bool, error) {
	msg := messaging.Message{Subject: m.Subject, Data: m.Payload}
	if len(m.Headers) > 0 {
		if err := jsonx.FromJSON(string(m.Headers), &msg.Headers); err != nil {
			r.logger.Warn("outbox: skipping message with bad headers", "id", m.ID, "error", err)
			return false, r.recordFailure(ctx, table, m, err)
		}
	}
	if err := r.publisher.Publish(ctx, msg); err != nil {
		r.logger.Warn("outbox: publish failed", "id", m.ID, "subject", m.Subject, "error", err)
		return false, r.recordFailure(ctx, table, m, err)
	}
	now := r.clock.Now()
	if _, err := table.UpdateMap(ctx, map[string]any{"published_at": now}, "id = ?", m.ID); err != nil {
		// Published but not marked: it will be re-delivered next pass
		// (at-least-once). Surface so the operator notices a persistent issue.
		r.logger.Error("outbox: marking message published failed", "id", m.ID, "error", err)
		return false, err
	}
	return true, nil
}

// recordFailure increments the attempt counter and stores the error message,
// leaving published_at NULL so the message is retried. Once the attempt count
// reaches WithMaxAttempts it also stamps failed_at, quarantining the message so it
// is no longer picked up. It writes through table so it participates in the
// caller's transaction on the locked path. The returned error is non-nil only if
// the write itself fails, which the transactional path propagates to roll back.
func (r *Relay) recordFailure(ctx context.Context, table *gormx.Table[Message], m *Message, cause error) error {
	attempts := m.Attempts + 1
	values := map[string]any{"attempts": attempts, "last_error": cause.Error()}
	if r.maxAttempts > 0 && attempts >= r.maxAttempts {
		values["failed_at"] = r.clock.Now()
		r.logger.Error("outbox: message quarantined after repeated failures",
			"id", m.ID, "subject", m.Subject, "attempts", attempts, "error", cause.Error())
	}
	if _, err := table.UpdateMap(ctx, values, "id = ?", m.ID); err != nil {
		r.logger.Error("outbox: recording failure failed", "id", m.ID, "error", err)
		return err
	}
	return nil
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
	n, err := r.table.Delete(ctx, cond, cutoff)
	if err != nil {
		return 0, errorx.NewInternalError("outbox", "pruning published messages failed").WithInner(err)
	}
	return n, nil
}
