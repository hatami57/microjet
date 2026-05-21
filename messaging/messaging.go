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

func (c *natsClient) Disconnect() error {
	c.conn.Drain()
	c.conn.Close()
	return nil
}
