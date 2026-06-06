package messaging

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hatami57/microjet/core"
	"github.com/nats-io/nats.go"
)

// Config is the messaging broker configuration, read from the [messaging]
// section of the application config (with APP_MESSAGING_* env overrides).
type Config struct {
	URL     string `mapstructure:"url"`
	Source  string `mapstructure:"source"`
	Version int    `mapstructure:"version"`
}

// Message is a published or received message. Headers carry metadata such as a
// correlation id (e.g. "X-Request-ID") so it can be propagated across services.
type Message struct {
	Subject string
	Data    []byte
	Headers map[string]string
}

// Handler processes a received message. The context is derived from the
// subscription context and is cancelled when the subscription ends.
type Handler func(ctx context.Context, msg Message)

// Client is the messaging abstraction used by the host. Publish and Subscribe
// take a context for cancellation/deadlines, and messages carry headers so
// metadata (correlation ids, trace context) survives across the broker.
type Client interface {
	Publish(ctx context.Context, msg Message) error
	Subscribe(ctx context.Context, subject string, handler Handler) (Subscription, error)
	Disconnect() error
}

// Subscription represents an active subscription that can be cancelled.
type Subscription interface {
	Unsubscribe() error
}

// NATSClient is the NATS-backed implementation of Client. It implements
// core.Configurable so it can participate in a core.LoadAll call — LoadConfig
// reads the [messaging] section. Call Connect after LoadAll to dial the broker.
type NATSClient struct {
	Config Config
	logger *slog.Logger
	conn   *nats.Conn
}

// NewNATSClient returns a NATSClient ready to be passed to core.LoadAll.
func NewNATSClient(logger *slog.Logger) *NATSClient {
	return &NATSClient{logger: logger}
}

// LoadConfig implements core.Configurable, reading the [messaging] section.
func (c *NATSClient) LoadConfig(l *core.ConfigLoader) error {
	return l.UnmarshalKey("messaging", &c.Config)
}

// Connect dials the NATS broker using the loaded config. Call this after
// core.LoadAll has populated Config.
func (c *NATSClient) Connect() error {
	conn, err := nats.Connect(c.Config.URL)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS at %s: %w", c.Config.URL, err)
	}
	c.logger.Info("connected to NATS", "url", c.Config.URL)
	c.conn = conn
	return nil
}

// Init implements core.Initer, connecting to the NATS broker after config is loaded.
func (c *NATSClient) Init() error { return c.Connect() }

// Close implements core.Closer, draining and closing the NATS connection.
func (c *NATSClient) Close() error { return c.Disconnect() }

func (c *NATSClient) Publish(ctx context.Context, msg Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m := &nats.Msg{Subject: msg.Subject, Data: msg.Data}
	if len(msg.Headers) > 0 {
		m.Header = make(nats.Header, len(msg.Headers))
		for k, v := range msg.Headers {
			m.Header.Set(k, v)
		}
	}
	return c.conn.PublishMsg(m)
}

func (c *NATSClient) Subscribe(ctx context.Context, subject string, handler Handler) (Subscription, error) {
	sub, err := c.conn.Subscribe(subject, func(m *nats.Msg) {
		var headers map[string]string
		if len(m.Header) > 0 {
			headers = make(map[string]string, len(m.Header))
			for k := range m.Header {
				headers[k] = m.Header.Get(k)
			}
		}
		handler(ctx, Message{Subject: m.Subject, Data: m.Data, Headers: headers})
	})
	if err != nil {
		return nil, err
	}
	return sub, nil
}

// Healthy implements core.HealthChecker, reporting an error when the NATS
// connection is not currently connected. The context is accepted for interface
// uniformity; the check is local and fast.
func (c *NATSClient) Healthy(_ context.Context) error {
	if c.conn == nil || !c.conn.IsConnected() {
		return fmt.Errorf("nats: not connected")
	}
	return nil
}

func (c *NATSClient) Disconnect() error {
	// Drain flushes pending messages and unsubscribes before closing; surface
	// its error rather than silently dropping in-flight work.
	if err := c.conn.Drain(); err != nil {
		c.conn.Close()
		return fmt.Errorf("nats: drain failed: %w", err)
	}
	return nil
}