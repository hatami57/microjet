package cache

import (
	"context"
	"sync"
	"time"
)

// MemoryCache is an in-process Cache with TTL expiry, safe for concurrent use.
// Suitable for single-instance services and tests; use RedisCache when entries
// must be shared across replicas.
type MemoryCache struct {
	mu      sync.RWMutex
	entries map[string]memoryEntry
	now     func() time.Time // injectable for deterministic tests
}

type memoryEntry struct {
	value     []byte
	expiresAt time.Time // zero = no expiry
}

// NewMemoryCache returns an empty in-memory cache.
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{entries: map[string]memoryEntry{}, now: time.Now}
}

func (m *MemoryCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	m.mu.RLock()
	entry, ok := m.entries[key]
	m.mu.RUnlock()
	if !ok {
		return nil, false, nil
	}
	if !entry.expiresAt.IsZero() && m.now().After(entry.expiresAt) {
		m.mu.Lock()
		// Re-check under the write lock in case it was refreshed concurrently.
		if e, ok := m.entries[key]; ok && !e.expiresAt.IsZero() && m.now().After(e.expiresAt) {
			delete(m.entries, key)
		}
		m.mu.Unlock()
		return nil, false, nil
	}
	return entry.value, true, nil
}

func (m *MemoryCache) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = m.now().Add(ttl)
	}
	m.mu.Lock()
	m.entries[key] = memoryEntry{value: value, expiresAt: expiresAt}
	m.mu.Unlock()
	return nil
}

func (m *MemoryCache) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.entries, key)
	m.mu.Unlock()
	return nil
}

func (m *MemoryCache) Close() error { return nil }
