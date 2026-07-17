// Package jetstream provides a NATS JetStream implementation of the
// messaging.Client broker interface. Unlike the core NATS driver — which is
// fire-and-forget and drops messages while a consumer is down — JetStream
// persists published messages to a stream and delivers them to durable
// consumers with explicit acks, redelivery, and dead-lettering, giving the
// outbox at-least-once delivery end to end (broker and consumer).
//
// It is a drop-in messaging.Client: install it with messaging.Module and the
// existing messaging.Subscribe helper and outbox relay work unchanged.
package jetstream

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/core/configx"
	"github.com/hatami57/microjet/core/errorx"
	"github.com/hatami57/microjet/messaging"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// tracerName identifies this instrumentation in exported spans. Spans are
// no-ops until a global tracer provider is installed (e.g. via otelx).
const tracerName = "github.com/hatami57/microjet/messaging/jetstream"

var (
	_ messaging.Client     = (*Client)(nil)
	_ configx.Configurable = (*Client)(nil)
	_ core.Initer          = (*Client)(nil)
	_ core.Closer          = (*Client)(nil)
)

// Client is the JetStream-backed implementation of messaging.Client. Publish
// persists to a stream (waiting for the server ack); Subscribe binds a durable
// consumer with explicit acks. Request/Respond use core NATS over the same
// connection, since JetStream has no request-reply.
type Client struct {
	Config Config
	logger *slog.Logger
	conn   *nats.Conn
	js     jetstream.JetStream
	opts   []nats.Option

	mu   sync.Mutex
	subs []*jsSubscription
}

// New returns a Client ready to be handed to messaging.Module, which wires the
// host logger via SetLogger and drives the lifecycle. Production defaults
// (connect timeout, infinite reconnect, error logging) are applied first; any
// opts are appended afterwards and therefore override them.
func New(opts ...nats.Option) *Client {
	c := &Client{logger: slog.Default()}
	c.opts = append(c.defaultOptions(), opts...)
	return c
}

// SetLogger sets the logger used for connection events and async errors; a nil
// logger is ignored so the default set in New is kept.
func (c *Client) SetLogger(logger *slog.Logger) {
	if logger != nil {
		c.logger = logger
	}
}

func (c *Client) defaultOptions() []nats.Option {
	return []nats.Option{
		nats.Timeout(10 * time.Second),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(-1),
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
			c.logger.Error("jetstream client error", "error", err)
		}),
	}
}

// ReadConfig implements configx.Configurable, reading [messaging] and its
// [messaging.jetstream] sub-table with the standard defaults.
func (c *Client) ReadConfig(l configx.Reader) error {
	l.SetDefault("messaging.jetstream.ackWait", "30s")
	l.SetDefault("messaging.jetstream.maxDeliver", 5)
	l.SetDefault("messaging.jetstream.maxAckPending", 1000)
	return l.Read("messaging", &c.Config)
}

// Connect dials NATS, opens the JetStream context, and ensures the configured
// streams. Call after config is loaded.
func (c *Client) Connect() error {
	conn, err := nats.Connect(c.Config.URL, c.opts...)
	if err != nil {
		return errorx.NewInternalError("jetstream", "failed to connect to NATS", "url", c.Config.URL).WithInner(err)
	}
	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return errorx.NewInternalError("jetstream", "opening JetStream context failed").WithInner(err)
	}
	c.conn = conn
	c.js = js

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.ensureStreams(ctx); err != nil {
		conn.Close()
		return err
	}
	c.logger.Info("connected to NATS JetStream", "url", c.Config.URL)
	return nil
}

// Init implements core.Initer.
func (c *Client) Init() error { return c.Connect() }

// Close implements core.Closer: it stops every consumer, then drains and closes
// the connection.
func (c *Client) Close() error { return c.Disconnect() }

// Disconnect stops consumers and drains the connection. Drain flushes pending
// messages and unsubscribes before closing.
func (c *Client) Disconnect() error {
	c.mu.Lock()
	subs := c.subs
	c.subs = nil
	c.mu.Unlock()
	for _, s := range subs {
		_ = s.Unsubscribe()
	}

	if c.conn == nil {
		return nil
	}
	if err := c.conn.Drain(); err != nil {
		c.conn.Close()
		return errorx.NewInternalError("jetstream", "drain failed").WithInner(err)
	}
	return nil
}

// IsConnected reports whether the client currently has a live NATS connection.
func (c *Client) IsConnected() bool {
	return c.conn != nil && c.conn.IsConnected()
}

// Healthy implements core.HealthChecker.
func (c *Client) Healthy(_ context.Context) error {
	if !c.IsConnected() {
		return errorx.NewInternalError("jetstream", "not connected")
	}
	return nil
}

func spanAttrs(subject string) trace.SpanStartOption {
	return trace.WithAttributes(
		attribute.String("messaging.system", "nats-jetstream"),
		attribute.String("messaging.destination.name", subject),
	)
}

// Publish persists msg to its stream, waiting for the server's PubAck so a nil
// return means the message is durably stored. The trace context and correlation
// id on ctx are injected into the headers so consumers continue the trace.
func (c *Client) Publish(ctx context.Context, msg messaging.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, span := otel.Tracer(tracerName).Start(ctx, msg.Subject+" publish",
		trace.WithSpanKind(trace.SpanKindProducer), spanAttrs(msg.Subject))
	defer span.End()
	msg.Headers = messaging.InjectContext(ctx, msg.Headers)

	_, err := c.js.PublishMsg(ctx, &nats.Msg{
		Subject: msg.Subject,
		Data:    msg.Data,
		Header:  toNATSHeader(msg.Headers),
	})
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return errorx.NewInternalError("jetstream", "publish failed", "subject", msg.Subject).WithInner(err)
	}
	return nil
}

// Request sends req and waits for a single reply over core NATS (JetStream has
// no request-reply), honouring ctx for cancellation and deadlines.
func (c *Client) Request(ctx context.Context, req messaging.Request) (*messaging.Response, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, req.Subject+" request",
		trace.WithSpanKind(trace.SpanKindClient), spanAttrs(req.Subject))
	defer span.End()
	req.Headers = messaging.InjectContext(ctx, req.Headers)

	reply, err := c.conn.RequestMsgWithContext(ctx, &nats.Msg{
		Subject: req.Subject,
		Data:    req.Data,
		Header:  toNATSHeader(req.Headers),
	})
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		if errors.Is(err, nats.ErrTimeout) || errors.Is(err, nats.ErrNoResponders) {
			return nil, fmt.Errorf("%w: %w", messaging.ErrTimeout, err)
		}
		return nil, err
	}
	return &messaging.Response{
		Subject: reply.Subject,
		Data:    reply.Data,
		Reply:   reply.Reply,
		Headers: fromNATSHeader(reply.Header),
	}, nil
}

// Respond registers handler for requests on command over core NATS, publishing
// the returned response to the request's reply subject.
func (c *Client) Respond(command string, handler messaging.RequestHandler) (messaging.Subscription, error) {
	sub, err := c.conn.Subscribe(command, c.requestCallback(handler))
	if err != nil {
		return nil, err
	}
	return &natsSubscription{sub: sub}, nil
}

// QueueRespond registers handler for requests on command within a queue group.
func (c *Client) QueueRespond(command, queue string, handler messaging.RequestHandler) (messaging.Subscription, error) {
	sub, err := c.conn.QueueSubscribe(command, queue, c.requestCallback(handler))
	if err != nil {
		return nil, err
	}
	return &natsSubscription{sub: sub}, nil
}

func (c *Client) requestCallback(handler messaging.RequestHandler) nats.MsgHandler {
	return func(m *nats.Msg) {
		req := &messaging.Request{Subject: m.Subject, Data: m.Data, Headers: fromNATSHeader(m.Header)}
		hctx := messaging.ExtractContext(context.Background(), req.Headers)
		hctx, span := otel.Tracer(tracerName).Start(hctx, m.Subject+" respond",
			trace.WithSpanKind(trace.SpanKindServer), spanAttrs(m.Subject))
		defer span.End()
		resp, err := handler(hctx, req)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			c.logger.Error("request handler failed", "subject", m.Subject, "error", err)
			return
		}
		if resp == nil || m.Reply == "" {
			return
		}
		if err := c.conn.PublishMsg(&nats.Msg{Subject: m.Reply, Data: resp.Data, Header: toNATSHeader(resp.Headers)}); err != nil {
			c.logger.Error("failed to publish reply", "subject", m.Reply, "error", err)
		}
	}
}

// toNATSHeader converts the messaging header representation into a nats.Header,
// returning nil for empty input so no header frame is sent.
func toNATSHeader(h map[string][]string) nats.Header {
	if len(h) == 0 {
		return nil
	}
	out := make(nats.Header, len(h))
	for k, vs := range h {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

// fromNATSHeader converts a nats.Header into the messaging header representation.
func fromNATSHeader(h nats.Header) map[string][]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string][]string, len(h))
	for k, vs := range h {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

// natsSubscription wraps a *nats.Subscription for the core-NATS request-reply
// paths, satisfying messaging.Subscription.
type natsSubscription struct {
	sub *nats.Subscription
}

func (s *natsSubscription) Unsubscribe() error { return s.sub.Unsubscribe() }
func (s *natsSubscription) Subject() string    { return s.sub.Subject }
