// Package cache provides a small key/value cache abstraction with a Redis-backed
// implementation (for cross-replica sharing) and an in-memory implementation
// (for single-instance or tests). It lives in its own module so the Redis
// dependency is not forced on services that do not need it.
package cache

import (
	"context"
	"encoding/json"
	"time"
)

// Cache is a byte-oriented key/value store with per-entry expiry. A zero ttl
// means no expiry. Get reports found=false for a missing or expired key.
type Cache interface {
	Get(ctx context.Context, key string) (value []byte, found bool, err error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Close() error
}

// GetJSON fetches key and JSON-decodes it into T. found is false (with a zero T
// and nil error) when the key is absent.
func GetJSON[T any](ctx context.Context, c Cache, key string) (T, bool, error) {
	var zero T
	data, found, err := c.Get(ctx, key)
	if err != nil || !found {
		return zero, found, err
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return zero, false, err
	}
	return v, true, nil
}

// SetJSON JSON-encodes v and stores it under key with the given ttl.
func SetJSON[T any](ctx context.Context, c Cache, key string, v T, ttl time.Duration) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.Set(ctx, key, data, ttl)
}
