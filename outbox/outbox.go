// Package outbox implements the transactional outbox pattern on top of a
// gormx-managed database and a messaging.Publisher. Writing an event to the
// outbox in the same transaction that persists a domain change guarantees the
// event is recorded atomically with the change; a background relay then
// publishes recorded events to the broker with at-least-once delivery, so a
// crash between commit and publish never loses (or fabricates) an event.
//
// Enqueue is context-threaded: called inside a gormx transaction
// (BaseRepository.RunTx), it joins that transaction, so the message and the
// domain write commit atomically. With outbox.Module installed, resolve the
// shared Enqueuer with outbox.Of(app):
//
//	enq := outbox.Of(app)
//	err := repo.RunTx(ctx, func(ctx context.Context) error {
//	    if err := orders.Create(ctx, &order); err != nil {
//	        return err
//	    }
//	    return enq.EnqueueJSON(ctx, "orders.created", order)
//	})
//
// The relay installed by outbox.Module then drains the table to the broker.
package outbox

import (
	"context"
	"time"

	"github.com/hatami57/microjet/core/errorx"
	"github.com/hatami57/microjet/core/jsonx"
	"github.com/hatami57/microjet/gormx"
	"github.com/hatami57/microjet/messaging"
	"gorm.io/gorm"
)

// Message is a single outbox row: a broker message recorded for later delivery.
type Message struct {
	ID          uint64     `gorm:"primaryKey;autoIncrement"`
	Subject     string     `gorm:"not null;index"`
	Payload     []byte     `gorm:"not null"`
	Headers     []byte     // JSON-encoded map[string][]string; nil when none
	CreatedAt   time.Time  `gorm:"not null;index"`
	PublishedAt *time.Time `gorm:"index"` // nil until the relay publishes it
	FailedAt    *time.Time `gorm:"index"` // set when the relay quarantines it after WithMaxAttempts
	Attempts    int        `gorm:"not null;default:0"`
	LastError   string
}

// TableName sets the table name used by gorm for outbox rows.
func (Message) TableName() string { return "outbox_messages" }

// Migrate creates or updates the outbox table. Call it once at startup (the host
// integration does this automatically) before enqueuing or relaying.
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(&Message{}); err != nil {
		return errorx.NewInternalError("outbox", "migrating outbox table failed").WithInner(err)
	}
	return nil
}

// Enqueuer records messages into the outbox through gormx. Because its writes are
// context-threaded, calling Enqueue/EnqueueJSON inside a gormx transaction
// (BaseRepository.RunTx) joins that transaction, so the message and the domain
// change commit atomically. Resolve the shared one with outbox.Of(app), or build
// a standalone one with NewEnqueuer; either way reuse it.
type Enqueuer struct {
	table  *gormx.Table[Message]
	notify chan<- struct{} // best-effort wake-up to the relay; nil for standalone enqueuers
}

// NewEnqueuer builds an Enqueuer over the given database connection (e.g.
// gormx.Of(app)). Prefer outbox.Of(app) when outbox.Module is installed.
func NewEnqueuer(db *gorm.DB) *Enqueuer {
	return &Enqueuer{table: gormx.NewTable[Message](db)}
}

// Enqueue records msg in the outbox. Call it with a context carrying a gormx
// transaction (inside BaseRepository.RunTx) so the message and the domain change
// the message announces commit atomically. The caller's trace context and
// correlation id are captured into the stored headers, so the event keeps its
// lineage when the relay publishes it later.
func (e *Enqueuer) Enqueue(ctx context.Context, msg messaging.Message) error {
	if e.table == nil {
		return errorx.NewInternalError("outbox", "enqueuer is not initialized; build it with NewEnqueuer or resolve outbox.Of after the app is set up")
	}
	row := Message{Subject: msg.Subject, Payload: msg.Data}
	// Capture the caller's trace context and correlation id now, while the request
	// context is live, so the event keeps its lineage when the relay publishes it
	// later from a background context (mirrors the live messaging.Publisher).
	headers := messaging.InjectContext(ctx, msg.Headers)
	if len(headers) > 0 {
		h, err := jsonx.ToJSON(headers)
		if err != nil {
			return errorx.NewInternalError("outbox", "encoding message headers failed").
				WithParams("subject", msg.Subject).WithInner(err)
		}
		row.Headers = []byte(h)
	}
	if err := e.table.Create(ctx, &row); err != nil {
		return errorx.NewInternalError("outbox", "enqueuing message failed").
			WithParams("subject", msg.Subject).WithInner(err)
	}
	e.signal()
	return nil
}

// signal nudges the relay to run a pass promptly after an enqueue, instead of
// waiting out its interval. It is a best-effort hint (the interval remains the
// correctness backstop, and the wake may fire just before the enclosing
// transaction commits) and never blocks: a full buffer already carries a pending
// wake-up, and a standalone enqueuer has no relay to notify.
func (e *Enqueuer) signal() {
	if e.notify == nil {
		return
	}
	select {
	case e.notify <- struct{}{}:
	default:
	}
}

// EnqueueJSON marshals payload to JSON and records it under subject. It is the
// typed companion to Enqueue for the common JSON-body case.
func (e *Enqueuer) EnqueueJSON(ctx context.Context, subject string, payload any) error {
	data, err := jsonx.ToJSON(payload)
	if err != nil {
		return errorx.NewInternalError("outbox", "encoding message payload failed").
			WithParams("subject", subject).WithInner(err)
	}
	return e.Enqueue(ctx, messaging.Message{Subject: subject, Data: []byte(data)})
}
