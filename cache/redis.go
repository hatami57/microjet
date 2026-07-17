package cache

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"time"

	"github.com/hatami57/microjet/core/errorx"
	"github.com/redis/go-redis/v9"
)

// RedisCache is a Redis-backed Cache, suitable for sharing entries across
// replicas. Keys are optionally namespaced with a prefix.
type RedisCache struct {
	client *redis.Client
	prefix string
}

// RedisOptions configures a RedisCache.
type RedisOptions struct {
	// Addr is the Redis address, e.g. "localhost:6379".
	Addr string
	// Password and DB are optional.
	Password string
	DB       int
	// Prefix is prepended to every key (e.g. "myservice:").
	Prefix string
}

// NewRedis connects to Redis and verifies the connection with a PING.
func NewRedis(ctx context.Context, opts RedisOptions) (*RedisCache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     opts.Addr,
		Password: opts.Password,
		DB:       opts.DB,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, errorx.NewInternalError("cache", "redis ping failed", "addr", opts.Addr).WithInner(err)
	}
	return &RedisCache{client: client, prefix: opts.Prefix}, nil
}

// NewRedisWithClient wraps an existing *redis.Client (for shared clients or
// custom configuration).
func NewRedisWithClient(client *redis.Client, prefix string) *RedisCache {
	return &RedisCache{client: client, prefix: prefix}
}

func (r *RedisCache) key(k string) string { return r.prefix + k }

func (r *RedisCache) GetBytes(ctx context.Context, key string) ([]byte, bool, error) {
	data, err := r.client.Get(ctx, r.key(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, errorx.NewInternalError("cache", "redis get failed").WithInner(err)
	}
	return data, true, nil
}

func (r *RedisCache) SetBytes(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := r.client.Set(ctx, r.key(key), value, ttl).Err(); err != nil {
		return errorx.NewInternalError("cache", "redis set failed").WithInner(err)
	}
	return nil
}

// Get fetches key and gob-decodes the stored value. Because Redis cannot hold
// live Go values, Set serializes with encoding/gob; the dynamic type of the
// value must be registered with gob.Register for the decode to reconstruct it.
func (r *RedisCache) Get(ctx context.Context, key string) (any, bool, error) {
	data, found, err := r.GetBytes(ctx, key)
	if err != nil || !found {
		return nil, found, err
	}
	var value any
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&value); err != nil {
		return nil, false, errorx.NewInternalError("cache", "redis get: gob decode failed").WithInner(err)
	}
	return value, true, nil
}

// Set gob-encodes value and stores it under key with the given ttl. The
// dynamic type of value must be registered with gob.Register so Get can decode
// it back into the same type.
func (r *RedisCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(&value); err != nil {
		return errorx.NewInternalError("cache", "redis set: gob encode failed").WithInner(err)
	}
	return r.SetBytes(ctx, key, buf.Bytes(), ttl)
}

func (r *RedisCache) Delete(ctx context.Context, key string) error {
	if err := r.client.Del(ctx, r.key(key)).Err(); err != nil {
		return errorx.NewInternalError("cache", "redis del failed").WithInner(err)
	}
	return nil
}

// Healthy reports an error when Redis does not respond to a PING. It lets a
// RedisCache double as a host readiness probe.
func (r *RedisCache) Healthy(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

func (r *RedisCache) Close() error { return r.client.Close() }
