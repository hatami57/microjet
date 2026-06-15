// Package cache provides a small key/value cache abstraction with a Redis-backed
// implementation (for cross-replica sharing) and an in-memory implementation
// (for single-instance or tests). It lives in its own module so the Redis
// dependency is not forced on services that do not need it.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hatami57/microjet/core"
)

// Cache is a byte-oriented key/value store with per-entry expiry. A zero ttl
// means no expiry. Get reports found=false for a missing or expired key.
type Cache interface {
	GetBytes(ctx context.Context, key string) (value []byte, found bool, err error)
	SetBytes(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Get(ctx context.Context, key string) (value any, found bool, err error)
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Close() error
}

// Config selects and configures a Cache. Driver "memory" (the default) needs no
// other fields; "redis" uses Addr/Password/DB. Prefix is prepended to every key.
type Config struct {
	Driver   string `mapstructure:"driver"`
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
	Prefix   string `mapstructure:"prefix"`
}

// New builds a Cache from cfg. An empty or "memory" driver returns an in-process
// MemoryCache; "redis" connects to Redis (verifying the connection with a PING).
func New(ctx context.Context, cfg Config, timeProvider core.TimeProvider) (Cache, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Driver)) {
	case "", "memory":
		return NewMemoryCache(timeProvider), nil
	case "redis":
		return NewRedis(ctx, RedisOptions{
			Addr:     cfg.Addr,
			Password: cfg.Password,
			DB:       cfg.DB,
			Prefix:   cfg.Prefix,
		})
	default:
		return nil, fmt.Errorf("cache: unsupported driver %q", cfg.Driver)
	}
}

// GetJSON fetches key and JSON-decodes it into T. found is false (with a zero T
// and nil error) when the key is absent.
func GetJSON[T any](ctx context.Context, c Cache, key string) (T, bool, error) {
	var zero T
	data, found, err := c.GetBytes(ctx, key)
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
	return c.SetBytes(ctx, key, data, ttl)
}
