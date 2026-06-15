package cache

import (
	"context"
	"sync"
	"time"

	"github.com/hatami57/microjet/core"
)

// MemoryCache is an in-process Cache with TTL expiry, safe for concurrent use.
// Suitable for single-instance services and tests; use RedisCache when entries
// must be shared across replicas.
//
// Get/Set store arbitrary values in the in-process map without serialization,
// so the exact value (including its concrete type) is returned. GetBytes/SetBytes
// share the same map; a value stored with one pair is not visible to the other.
type MemoryCache struct {
	mu           sync.RWMutex
	entries      map[string]memoryEntry
	timeProvider core.TimeProvider // injectable for deterministic tests
}

type memoryEntry struct {
	value     any
	expiresAt time.Time // zero = no expiry
}

// NewMemoryCache returns an empty in-memory cache.
func NewMemoryCache(timeProvider core.TimeProvider) *MemoryCache {
	return &MemoryCache{entries: map[string]memoryEntry{}, timeProvider: timeProvider}
}

// get returns the live entry value for key, handling expiry. found is false for
// a missing or expired key.
func (m *MemoryCache) get(key string) (any, bool) {
	m.mu.RLock()
	entry, ok := m.entries[key]
	m.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if !entry.expiresAt.IsZero() && m.timeProvider.Now().After(entry.expiresAt) {
		m.mu.Lock()
		// Re-check under the write lock in case it was refreshed concurrently.
		if e, ok := m.entries[key]; ok && !e.expiresAt.IsZero() && m.timeProvider.Now().After(e.expiresAt) {
			delete(m.entries, key)
		}
		m.mu.Unlock()
		return nil, false
	}
	return entry.value, true
}

func (m *MemoryCache) set(key string, value any, ttl time.Duration) {
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = m.timeProvider.Now().Add(ttl)
	}
	m.mu.Lock()
	m.entries[key] = memoryEntry{value: value, expiresAt: expiresAt}
	m.mu.Unlock()
}

func (m *MemoryCache) GetBytes(_ context.Context, key string) ([]byte, bool, error) {
	v, ok := m.get(key)
	if !ok {
		return nil, false, nil
	}
	b, ok := v.([]byte)
	if !ok {
		return nil, false, nil
	}
	return b, true, nil
}

func (m *MemoryCache) SetBytes(_ context.Context, key string, value []byte, ttl time.Duration) error {
	m.set(key, value, ttl)
	return nil
}

// Get returns the value stored for key. The value is returned as-is, with its
// original concrete type, so callers type-assert it back.
func (m *MemoryCache) Get(_ context.Context, key string) (any, bool, error) {
	v, ok := m.get(key)
	return v, ok, nil
}

// Set stores value for key. The value is held directly in the in-process map,
// not copied or serialized, so callers must not mutate it after storing.
func (m *MemoryCache) Set(_ context.Context, key string, value any, ttl time.Duration) error {
	m.set(key, value, ttl)
	return nil
}

func (m *MemoryCache) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.entries, key)
	m.mu.Unlock()
	return nil
}

func (m *MemoryCache) Close() error { return nil }
