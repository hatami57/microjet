package jetstream

import (
	"context"
	"strings"
	"sync"

	"github.com/hatami57/microjet/core/errorx"
	"github.com/hatami57/microjet/messaging"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Subscribe binds a durable consumer to subject and invokes handler for each
// message. The durable name is derived from the subject (prefixed by
// DurablePrefix), so a restart resumes where it left off. A nil handler return
// acks the message; an error naks it for redelivery, and after MaxDeliver
// attempts the message is terminated (and copied to DeadLetterSubject if set).
func (c *Client) Subscribe(ctx context.Context, subject string, handler messaging.Handler) (messaging.Subscription, error) {
	return c.consume(ctx, subject, c.durableName(subject), handler)
}

// QueueSubscribe binds a shared durable consumer named after queue, so replicas
// using the same queue load-balance the subject between them — the durable
// equivalent of a core-NATS queue group.
func (c *Client) QueueSubscribe(ctx context.Context, subject, queue string, handler messaging.Handler) (messaging.Subscription, error) {
	return c.consume(ctx, subject, c.durableName(queue), handler)
}

func (c *Client) consume(ctx context.Context, subject, durable string, handler messaging.Handler) (messaging.Subscription, error) {
	stream, err := c.js.StreamNameBySubject(ctx, subject)
	if err != nil {
		return nil, errorx.NewInternalError("jetstream", "no stream bound to subject; declare it under [messaging.jetstream.streams]", "subject", subject).WithInner(err)
	}

	cons, err := c.js.CreateOrUpdateConsumer(ctx, stream, jetstream.ConsumerConfig{
		Durable:       durable,
		FilterSubject: subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       c.Config.JetStream.AckWait,
		MaxDeliver:    c.Config.JetStream.MaxDeliver,
		MaxAckPending: c.Config.JetStream.MaxAckPending,
	})
	if err != nil {
		return nil, errorx.NewInternalError("jetstream", "create consumer failed", "durable", durable, "stream", stream).WithInner(err)
	}

	cc, err := cons.Consume(func(m jetstream.Msg) { c.handleMessage(ctx, m, handler) })
	if err != nil {
		return nil, errorx.NewInternalError("jetstream", "consume failed", "durable", durable).WithInner(err)
	}

	sub := &jsSubscription{subject: subject, cc: cc}
	c.mu.Lock()
	c.subs = append(c.subs, sub)
	c.mu.Unlock()
	c.logger.Info("subscribed", "subject", subject, "durable", durable, "stream", stream)
	return sub, nil
}

// handleMessage runs the handler and acknowledges the message according to the
// outcome: Ack on success, Nak (redeliver) on error, or Term (+ dead-letter) once
// MaxDeliver attempts are exhausted.
func (c *Client) handleMessage(ctx context.Context, m jetstream.Msg, handler messaging.Handler) {
	msg := &messaging.Message{
		Subject: m.Subject(),
		Data:    m.Data(),
		Headers: fromNATSHeader(m.Headers()),
	}
	hctx := messaging.ExtractContext(ctx, msg.Headers)
	hctx, span := otel.Tracer(tracerName).Start(hctx, m.Subject()+" receive",
		trace.WithSpanKind(trace.SpanKindConsumer), spanAttrs(m.Subject()))
	defer span.End()

	if err := handler(hctx, msg); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		c.nakOrTerminate(hctx, m, msg, err)
		return
	}
	if err := m.Ack(); err != nil {
		c.logger.Error("jetstream: ack failed", "subject", m.Subject(), "error", err)
	}
}

// nakOrTerminate naks a failed message for redelivery, or terminates it once
// MaxDeliver is reached, copying it to the dead-letter subject when configured.
func (c *Client) nakOrTerminate(ctx context.Context, m jetstream.Msg, msg *messaging.Message, cause error) {
	max := c.Config.JetStream.MaxDeliver
	if max > 0 && deliveryCount(m) >= max {
		c.logger.Error("jetstream: message exhausted retries, terminating",
			"subject", m.Subject(), "attempts", max, "error", cause)
		c.deadLetter(ctx, msg)
		if err := m.Term(); err != nil {
			c.logger.Error("jetstream: term failed", "subject", m.Subject(), "error", err)
		}
		return
	}
	c.logger.Warn("jetstream: handler failed, will redeliver", "subject", m.Subject(), "error", cause)
	if err := m.Nak(); err != nil {
		c.logger.Error("jetstream: nak failed", "subject", m.Subject(), "error", err)
	}
}

// deadLetter publishes msg to the configured dead-letter subject (a no-op when
// none is set), tagging it with the original subject. It is a core-NATS publish,
// so a JetStream stream listening on the dead-letter subject captures it durably.
func (c *Client) deadLetter(_ context.Context, msg *messaging.Message) {
	dlq := c.Config.JetStream.DeadLetterSubject
	if dlq == "" {
		return
	}
	header := toNATSHeader(msg.Headers)
	if header == nil {
		header = nats.Header{}
	}
	header.Set("X-Original-Subject", msg.Subject)
	if err := c.conn.PublishMsg(&nats.Msg{Subject: dlq, Data: msg.Data, Header: header}); err != nil {
		c.logger.Error("jetstream: dead-letter publish failed", "subject", dlq, "error", err)
	}
}

// deliveryCount returns how many times m has been delivered, or 0 when the
// metadata is unavailable.
func deliveryCount(m jetstream.Msg) int {
	meta, err := m.Metadata()
	if err != nil {
		return 0
	}
	return int(meta.NumDelivered)
}

// durableName builds a JetStream-safe durable consumer name from base, applying
// DurablePrefix. JetStream forbids '.', '*', '>', whitespace, and path
// separators in durable names, so they are replaced with '_'.
func (c *Client) durableName(base string) string {
	name := sanitizeDurable(base)
	if p := c.Config.JetStream.DurablePrefix; p != "" {
		name = sanitizeDurable(p) + "_" + name
	}
	return name
}

var durableReplacer = strings.NewReplacer(".", "_", "*", "_", ">", "_", "/", "_", "\\", "_", " ", "_", "\t", "_")

func sanitizeDurable(s string) string { return durableReplacer.Replace(s) }

// jsSubscription wraps a JetStream ConsumeContext to satisfy
// messaging.Subscription; Unsubscribe stops delivery and is idempotent.
type jsSubscription struct {
	subject string
	cc      jetstream.ConsumeContext
	once    sync.Once
}

func (s *jsSubscription) Unsubscribe() error {
	s.once.Do(s.cc.Stop)
	return nil
}

func (s *jsSubscription) Subject() string { return s.subject }
