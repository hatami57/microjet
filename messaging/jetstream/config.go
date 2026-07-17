package jetstream

import "time"

// Config is the JetStream driver configuration, read from the [messaging]
// section (URL) plus the [messaging.jetstream] sub-table. It mirrors how the
// core NATS driver reads [messaging], so switching drivers keeps the same URL.
type Config struct {
	URL       string          `mapstructure:"url"`
	JetStream JetStreamConfig `mapstructure:"jetstream"`
}

// JetStreamConfig is the [messaging.jetstream] sub-table: the durable-consumer
// and redelivery policy plus the declarative streams to ensure on connect.
type JetStreamConfig struct {
	// DurablePrefix is prepended to every derived durable consumer name, so two
	// services consuming the same subject get distinct durables. Empty means no
	// prefix.
	DurablePrefix string `mapstructure:"durablePrefix"`
	// AckWait is how long the server waits for an ack before redelivering.
	AckWait time.Duration `mapstructure:"ackWait"`
	// MaxDeliver is the number of delivery attempts before a message is
	// terminated (and sent to DeadLetterSubject when set). 0 means unlimited.
	MaxDeliver int `mapstructure:"maxDeliver"`
	// MaxAckPending caps the number of un-acked messages in flight per consumer.
	MaxAckPending int `mapstructure:"maxAckPending"`
	// DeadLetterSubject, when set, receives a copy of any message that exhausts
	// MaxDeliver. A JetStream stream listening on this subject makes the DLQ
	// durable; otherwise it is a best-effort core-NATS publish.
	DeadLetterSubject string `mapstructure:"deadLetterSubject"`
	// Streams are ensured (created or updated) on connect. Leave empty when
	// streams are provisioned out of band.
	Streams []StreamSpec `mapstructure:"streams"`
}

// StreamSpec is one declarative stream definition under
// [[messaging.jetstream.streams]].
type StreamSpec struct {
	Name     string   `mapstructure:"name"`
	Subjects []string `mapstructure:"subjects"`
	// Retention is "limits" (default), "interest", or "workqueue".
	Retention string `mapstructure:"retention"`
	// Storage is "file" (default) or "memory".
	Storage string        `mapstructure:"storage"`
	MaxAge  time.Duration `mapstructure:"maxAge"`
}
