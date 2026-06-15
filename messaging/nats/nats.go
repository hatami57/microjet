package nats

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/core/config"
	"github.com/hatami57/microjet/messaging"
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// tracerName identifies this instrumentation in exported spans. Spans are
// no-ops until a global tracer provider is installed (e.g. via otelx).
const tracerName = "github.com/hatami57/microjet/messaging/nats"

// spanAttrs returns the standard messaging attributes for a span on subject.
func spanAttrs(subject string) trace.SpanStartOption {
	return trace.WithAttributes(
		attribute.String("messaging.system", "nats"),
		attribute.String("messaging.destination.name", subject),
	)
}

var (
	_ messaging.Client    = (*Client)(nil)
	_ config.Configurable = (*Client)(nil)
	_ core.Initer         = (*Client)(nil)
	_ core.Closer         = (*Client)(nil)
)

// Client is the NATS-backed implementation of messaging.Client. It implements
// config.Configurable so it can participate in a core.LoadAll call — LoadConfig
// reads the [messaging] section. Call Connect after LoadAll to dial the broker.
type Client struct {
	Config Config
	logger *slog.Logger
	conn   *nats.Conn
	opts   []nats.Option
}

// New returns a Client ready to be handed to host.WithMessaging, which wires
// the host logger via SetLogger and drives the lifecycle. Sane production defaults
// (connect timeout, infinite reconnect, error logging) are applied first; any
// opts are appended afterwards and therefore override them.
func New(opts ...nats.Option) *Client {
	c := &Client{logger: slog.Default()}
	c.opts = append(c.defaultOptions(), opts...)
	return c
}

// SetLogger sets the logger used for connection events and async errors. The
// host calls it during WithMessaging; a nil logger is ignored so the default
// set in New is kept.
func (c *Client) SetLogger(logger *slog.Logger) {
	if logger != nil {
		c.logger = logger
	}
}

// defaultOptions are the baseline nats.Connect options applied to every
// Client: a bounded connect timeout, unlimited reconnects with a fixed
// backoff, and async errors routed to the client logger. The error handler reads
// c.logger at call time so it honors a logger set after construction.
func (c *Client) defaultOptions() []nats.Option {
	return []nats.Option{
		nats.Timeout(10 * time.Second),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(-1),
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
			c.logger.Error("nats client error", "error", err)
		}),
	}
}

// ReadConfig implements config.Configurable, reading the [messaging] section.
func (c *Client) ReadConfig(l config.Reader) error {
	return l.Read("messaging", &c.Config)
}

// Connect dials the NATS broker using the loaded config. Call this after
// core.LoadAll has populated Config.
func (c *Client) Connect() error {
	conn, err := nats.Connect(c.Config.URL, c.opts...)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS at %s: %w", c.Config.URL, err)
	}
	c.logger.Info("connected to NATS", "url", c.Config.URL)
	c.conn = conn
	return nil
}

// Init implements core.Initer, connecting to the NATS broker after config is loaded.
func (c *Client) Init() error { return c.Connect() }

// Close implements core.Closer, draining and closing the NATS connection.
func (c *Client) Close() error { return c.Disconnect() }

// Disconnect drains and closes the NATS connection. Drain flushes pending
// messages and unsubscribes before closing; surface its error rather than
// silently dropping in-flight work.
func (c *Client) Disconnect() error {
	if c.conn == nil {
		return nil
	}
	if err := c.conn.Drain(); err != nil {
		c.conn.Close()
		return fmt.Errorf("nats: drain failed: %w", err)
	}
	return nil
}

// IsConnected reports whether the client currently has a live NATS connection.
func (c *Client) IsConnected() bool {
	return c.conn != nil && c.conn.IsConnected()
}

// Healthy implements core.HealthChecker, reporting an error when the NATS
// connection is not currently connected. The context is accepted for interface
// uniformity; the check is local and fast.
func (c *Client) Healthy(_ context.Context) error {
	if !c.IsConnected() {
		return fmt.Errorf("nats: not connected")
	}
	return nil
}

// Publish sends a message to the subject carried on msg. The trace context and
// correlation id carried by ctx are injected into the message headers so
// consumers continue the trace.
func (c *Client) Publish(ctx context.Context, msg messaging.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, span := otel.Tracer(tracerName).Start(ctx, msg.Subject+" publish",
		trace.WithSpanKind(trace.SpanKindProducer), spanAttrs(msg.Subject))
	defer span.End()
	msg.Headers = messaging.InjectContext(ctx, msg.Headers)

	err := c.conn.PublishMsg(&nats.Msg{
		Subject: msg.Subject,
		Data:    msg.Data,
		Header:  toNATSHeader(msg.Headers),
	})
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

// Subscribe registers handler for messages on subject. The handler is invoked
// with a context derived from ctx.
func (c *Client) Subscribe(ctx context.Context, subject string, handler messaging.Handler) (messaging.Subscription, error) {
	sub, err := c.conn.Subscribe(subject, c.messageCallback(ctx, handler))
	if err != nil {
		return nil, err
	}
	return &subscription{sub: sub}, nil
}

// QueueSubscribe registers handler for messages on subject within a queue group,
// so each message is delivered to only one member of the group.
func (c *Client) QueueSubscribe(ctx context.Context, subject, queue string, handler messaging.Handler) (messaging.Subscription, error) {
	sub, err := c.conn.QueueSubscribe(subject, queue, c.messageCallback(ctx, handler))
	if err != nil {
		return nil, err
	}
	return &subscription{sub: sub}, nil
}

// Request sends req and waits for a single reply, honouring ctx for
// cancellation and deadlines. The trace context and correlation id carried by
// ctx are injected into the request headers so the responder continues the trace.
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
		// Normalise NATS' timeout/no-responder errors into the transport-agnostic
		// sentinel so callers can detect them without importing nats.
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

// Respond registers handler for requests on command, publishing the returned
// response to the request's reply subject.
func (c *Client) Respond(command string, handler messaging.RequestHandler) (messaging.Subscription, error) {
	sub, err := c.conn.Subscribe(command, c.requestCallback(handler))
	if err != nil {
		return nil, err
	}
	return &subscription{sub: sub}, nil
}

// QueueRespond registers handler for requests on command within a queue group,
// so each request is handled by only one member of the group.
func (c *Client) QueueRespond(command, queue string, handler messaging.RequestHandler) (messaging.Subscription, error) {
	sub, err := c.conn.QueueSubscribe(command, queue, c.requestCallback(handler))
	if err != nil {
		return nil, err
	}
	return &subscription{sub: sub}, nil
}

// messageCallback adapts a messaging.Handler to a nats.MsgHandler, propagating
// ctx and logging handler errors (NATS has no delivery channel for them). The
// handler context carries the trace context and correlation id extracted from
// the message headers, wrapped in a consumer span.
func (c *Client) messageCallback(ctx context.Context, handler messaging.Handler) nats.MsgHandler {
	return func(m *nats.Msg) {
		msg := &messaging.Message{
			Subject: m.Subject,
			Data:    m.Data,
			Headers: fromNATSHeader(m.Header),
		}
		hctx := messaging.ExtractContext(ctx, msg.Headers)
		hctx, span := otel.Tracer(tracerName).Start(hctx, m.Subject+" receive",
			trace.WithSpanKind(trace.SpanKindConsumer), spanAttrs(m.Subject))
		defer span.End()
		if err := handler(hctx, msg); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			c.logger.Error("message handler failed", "subject", m.Subject, "error", err)
		}
	}
}

// requestCallback adapts a messaging.RequestHandler to a nats.MsgHandler,
// publishing the handler's response to the request's reply subject. The handler
// context carries the trace context and correlation id extracted from the
// request headers, wrapped in a server span.
func (c *Client) requestCallback(handler messaging.RequestHandler) nats.MsgHandler {
	return func(m *nats.Msg) {
		req := &messaging.Request{
			Subject: m.Subject,
			Data:    m.Data,
			Headers: fromNATSHeader(m.Header),
		}
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
		reply := &nats.Msg{
			Subject: m.Reply,
			Data:    resp.Data,
			Header:  toNATSHeader(resp.Headers),
		}
		if err := c.conn.PublishMsg(reply); err != nil {
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

// fromNATSHeader converts a nats.Header into the messaging header
// representation, returning nil for empty input.
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

// subscription wraps a *nats.Subscription to satisfy messaging.Subscription,
// which exposes Subject as a method rather than a field.
type subscription struct {
	sub *nats.Subscription
}

func (s *subscription) Unsubscribe() error { return s.sub.Unsubscribe() }
func (s *subscription) Subject() string    { return s.sub.Subject }
