package jetstream

import (
	"context"

	"github.com/hatami57/microjet/core/errorx"
	"github.com/nats-io/nats.go/jetstream"
)

// ensureStreams creates or updates every stream declared in config. It is
// idempotent (CreateOrUpdateStream), so restarts and rolling deploys converge on
// the declared shape without erroring on already-existing streams.
func (c *Client) ensureStreams(ctx context.Context) error {
	for _, s := range c.Config.JetStream.Streams {
		cfg := jetstream.StreamConfig{
			Name:      s.Name,
			Subjects:  s.Subjects,
			Retention: retentionPolicy(s.Retention),
			Storage:   storageType(s.Storage),
			MaxAge:    s.MaxAge,
		}
		if _, err := c.js.CreateOrUpdateStream(ctx, cfg); err != nil {
			return errorx.NewInternalError("jetstream", "ensure stream failed", "stream", s.Name).WithInner(err)
		}
		c.logger.Info("ensured JetStream stream", "stream", s.Name, "subjects", s.Subjects)
	}
	return nil
}

// retentionPolicy maps the config string to a jetstream.RetentionPolicy,
// defaulting to Limits for empty/unknown values.
func retentionPolicy(s string) jetstream.RetentionPolicy {
	switch s {
	case "interest":
		return jetstream.InterestPolicy
	case "workqueue":
		return jetstream.WorkQueuePolicy
	default:
		return jetstream.LimitsPolicy
	}
}

// storageType maps the config string to a jetstream.StorageType, defaulting to
// File for empty/unknown values.
func storageType(s string) jetstream.StorageType {
	if s == "memory" {
		return jetstream.MemoryStorage
	}
	return jetstream.FileStorage
}
