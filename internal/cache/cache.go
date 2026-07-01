// Package cache provides a small TTL cache abstraction.
//
// The Cache interface lets you start with the zero-dependency in-memory
// implementation and later swap in Redis (or anything else) without
// touching the service layer.
package cache

import (
	"sync"
	"time"
)

// Cache is the minimal contract the service layer depends on.
type Cache interface {
	// Get returns the cached bytes and true if present and not expired.
	Get(key string) ([]byte, bool)
	// Set stores bytes under key for the given TTL.
	Set(key string, value []byte, ttl time.Duration)
}

type entry struct {
	value     []byte
	expiresAt time.Time
}

// Memory is a concurrency-safe in-memory TTL cache with a background janitor
// that evicts expired entries.
type Memory struct {
	mu    sync.RWMutex
	items map[string]entry
	stop  chan struct{}
}

// NewMemory creates an in-memory cache and starts a janitor that sweeps
// expired entries on the given interval. Call Close to stop it.
func NewMemory(sweepEvery time.Duration) *Memory {
	m := &Memory{
		items: make(map[string]entry),
		stop:  make(chan struct{}),
	}
	if sweepEvery <= 0 {
		sweepEvery = time.Minute
	}
	go m.janitor(sweepEvery)
	return m
}

// Get implements Cache.
func (m *Memory) Get(key string) ([]byte, bool) {
	m.mu.RLock()
	e, ok := m.items[key]
	m.mu.RUnlock()
	if !ok || time.Now().After(e.expiresAt) {
		return nil, false
	}
	return e.value, true
}

// Set implements Cache.
func (m *Memory) Set(key string, value []byte, ttl time.Duration) {
	m.mu.Lock()
	m.items[key] = entry{value: value, expiresAt: time.Now().Add(ttl)}
	m.mu.Unlock()
}

// Close stops the background janitor.
func (m *Memory) Close() { close(m.stop) }

func (m *Memory) janitor(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-t.C:
			now := time.Now()
			m.mu.Lock()
			for k, e := range m.items {
				if now.After(e.expiresAt) {
					delete(m.items, k)
				}
			}
			m.mu.Unlock()
		}
	}
}
