package outbox

import (
	"context"
	"log/slog"
	"time"

	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/jsonx"
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
	db        *gorm.DB
	publisher messaging.Publisher
	batchSize int
	logger    *slog.Logger
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

// NewRelay builds a Relay over db and publisher.
func NewRelay(db *gorm.DB, publisher messaging.Publisher, opts ...RelayOption) *Relay {
	r := &Relay{db: db, publisher: publisher, batchSize: DefaultBatchSize, logger: slog.Default()}
	for _, o := range opts {
		o(r)
	}
	return r
}

// PublishPending publishes up to the configured batch size of unpublished
// messages in insertion order and returns how many were published. A publish
// failure is recorded (attempts incremented, last error stored) and the message
// is left for a later pass; the relay continues with the rest of the batch
// rather than blocking on one bad message, so strict ordering is not preserved
// across failures.
func (r *Relay) PublishPending(ctx context.Context) (int, error) {
	var rows []Message
	if err := r.db.WithContext(ctx).
		Where("published_at IS NULL").
		Order("id ASC").
		Limit(r.batchSize).
		Find(&rows).Error; err != nil {
		return 0, core.NewInternalError("outbox", "loading pending messages failed").WithInner(err)
	}

	published := 0
	for i := range rows {
		if err := ctx.Err(); err != nil {
			return published, err
		}
		m := &rows[i]
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
		now := time.Now().UTC()
		if err := r.db.WithContext(ctx).Model(m).Update("published_at", now).Error; err != nil {
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
// leaving published_at NULL so the message is retried.
func (r *Relay) recordFailure(ctx context.Context, m *Message, cause error) {
	if err := r.db.WithContext(ctx).Model(m).
		Updates(map[string]any{"attempts": m.Attempts + 1, "last_error": cause.Error()}).Error; err != nil {
		r.logger.Error("outbox: recording failure failed", "id", m.ID, "error", err)
	}
}
