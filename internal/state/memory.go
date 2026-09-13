package state

import (
	"sync"
	"time"
)

// ResponseAffinity pins previous_response_id to its origin provider/model.
type ResponseAffinity struct {
	ResponseID    string
	ProviderID    string
	LocalModelID  string
	UpstreamModel string
	CreatedAt     time.Time
}

// Memory is a TTL-bounded in-memory affinity store.
type Memory struct {
	mu   sync.RWMutex
	data map[string]ResponseAffinity
	ttl  time.Duration
}

func NewMemory(ttl time.Duration) *Memory {
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}
	return &Memory{data: map[string]ResponseAffinity{}, ttl: ttl}
}

func (m *Memory) Set(a ResponseAffinity) {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	m.mu.Lock()
	m.data[a.ResponseID] = a
	m.mu.Unlock()
}

func (m *Memory) Get(id string) (ResponseAffinity, bool) {
	m.mu.RLock()
	a, ok := m.data[id]
	m.mu.RUnlock()
	if !ok {
		return ResponseAffinity{}, false
	}
	if time.Since(a.CreatedAt) > m.ttl {
		m.mu.Lock()
		delete(m.data, id)
		m.mu.Unlock()
		return ResponseAffinity{}, false
	}
	return a, true
}
