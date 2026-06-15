// Package outbox implements the transactional outbox pattern on top of a
// gorm-managed database and a messaging.Publisher. Writing an event to the
// outbox in the same transaction that persists a domain change guarantees the
// event is recorded atomically with the change; a background relay then
// publishes recorded events to the broker with at-least-once delivery, so a
// crash between commit and publish never loses (or fabricates) an event.
//
//	err := db.Transaction(func(tx *gorm.DB) error {
//	    if err := tx.Create(&order).Error; err != nil {
//	        return err
//	    }
//	    return outbox.EnqueueJSON(tx, "orders.created", order)
//	})
//
// Run a relay (see Relay, or host.WithOutbox) to drain the table to the broker.
package outbox

import (
	"time"

	"github.com/hatami57/microjet/core/errorx"
	"github.com/hatami57/microjet/core/jsonx"
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

// Enqueue records msg in the outbox using tx. Call it inside the same
// transaction that persists the domain change the message announces, so the two
// commit atomically.
func Enqueue(tx *gorm.DB, msg messaging.Message) error {
	row := Message{Subject: msg.Subject, Payload: msg.Data}
	if len(msg.Headers) > 0 {
		h, err := jsonx.ToJSON(msg.Headers)
		if err != nil {
			return errorx.NewInternalError("outbox", "encoding message headers failed").
				WithParams("subject", msg.Subject).WithInner(err)
		}
		row.Headers = []byte(h)
	}
	if err := tx.Create(&row).Error; err != nil {
		return errorx.NewInternalError("outbox", "enqueuing message failed").
			WithParams("subject", msg.Subject).WithInner(err)
	}
	return nil
}

// EnqueueJSON marshals payload to JSON and records it under subject. It is the
// typed companion to Enqueue for the common JSON-body case.
func EnqueueJSON(tx *gorm.DB, subject string, payload any) error {
	data, err := jsonx.ToJSON(payload)
	if err != nil {
		return errorx.NewInternalError("outbox", "encoding message payload failed").
			WithParams("subject", subject).WithInner(err)
	}
	return Enqueue(tx, messaging.Message{Subject: subject, Data: []byte(data)})
}
