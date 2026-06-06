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

// LoadConfig loads the [messaging] config section using the shared viper setup.
func LoadConfig(envPrefix string) (*Config, error) {
	return core.LoadSection[Config]("messaging", envPrefix)
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

// HealthChecker is an optional interface a Client may implement to report
// whether its connection is currently usable (consulted by the host's /readyz).
type HealthChecker interface {
	Healthy() error
}

// natsClient is the NATS-backed implementation of Client.
type natsClient struct {
	conn *nats.Conn
}

func New(cfg *Config, logger *slog.Logger) (Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("messaging config is required")
	}
	conn, err := nats.Connect(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS at %s: %w", cfg.URL, err)
	}
	logger.Info("connected to NATS", "url", cfg.URL)
	return &natsClient{conn: conn}, nil
}

func (c *natsClient) Publish(ctx context.Context, msg Message) error {
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

func (c *natsClient) Subscribe(ctx context.Context, subject string, handler Handler) (Subscription, error) {
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

// Healthy reports an error when the NATS connection is not currently connected.
func (c *natsClient) Healthy() error {
	if !c.conn.IsConnected() {
		return fmt.Errorf("nats: not connected (status %s)", c.conn.Status())
	}
	return nil
}

func (c *natsClient) Disconnect() error {
	// Drain flushes pending messages and unsubscribes before closing; surface
	// its error rather than silently dropping in-flight work.
	if err := c.conn.Drain(); err != nil {
		c.conn.Close()
		return fmt.Errorf("nats: drain failed: %w", err)
	}
	return nil
}
