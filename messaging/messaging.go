package messaging

import (
	"fmt"
	"log/slog"

	"github.com/hatami57/microjet/core"
	"github.com/nats-io/nats.go"
)

// Client is the messaging abstraction used by the host.
type Client interface {
	Publish(subject string, data []byte) error
	Subscribe(subject string, handler func(msg []byte)) (Subscription, error)
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

func New(cfg *core.MessagingConfig, logger *slog.Logger) (Client, error) {
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

func (c *natsClient) Publish(subject string, data []byte) error {
	return c.conn.Publish(subject, data)
}

func (c *natsClient) Subscribe(subject string, handler func(msg []byte)) (Subscription, error) {
	sub, err := c.conn.Subscribe(subject, func(m *nats.Msg) {
		handler(m.Data)
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
