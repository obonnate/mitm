// Package store provides an in-memory ring buffer for completed exchanges.
//
// The ring buffer is bounded so that the proxy can run indefinitely without
// unbounded memory growth. When the buffer is full the oldest exchange is
// evicted. All operations are safe for concurrent use.
package store

import (
	"fmt"
	"strings"
	"sync"

	"mitm/internal/proxy"
)

const defaultCap = 10_000

// Store holds a bounded, ordered collection of exchanges.
type Store struct {
	mu     sync.RWMutex
	cap    int
	ring   []*proxy.Exchange
	head   int // next write position (oldest entry after wrap)
	count  int // number of valid entries
	byID   map[uint64]*proxy.Exchange
	byUUID map[string]*proxy.Exchange
}

// New creates a Store with the given capacity. If cap ≤ 0, defaultCap is used.
func New(cap int) *Store {
	if cap <= 0 {
		cap = defaultCap
	}
	return &Store{
		cap:    cap,
		ring:   make([]*proxy.Exchange, cap),
		byID:   make(map[uint64]*proxy.Exchange, cap),
		byUUID: make(map[string]*proxy.Exchange, cap),
	}
}

// Add inserts an exchange. If the ring is full the oldest entry is evicted.
func (s *Store) Add(ex *proxy.Exchange) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Evict the entry being overwritten.
	if old := s.ring[s.head]; old != nil {
		delete(s.byID, old.ID)
		delete(s.byUUID, old.UUID)
	}

	s.ring[s.head] = ex
	s.byID[ex.ID] = ex
	s.byUUID[ex.UUID] = ex

	s.head = (s.head + 1) % s.cap
	if s.count < s.cap {
		s.count++
	}
}

// GetByID returns the exchange with the given sequence ID, or nil.
func (s *Store) GetByID(id uint64) *proxy.Exchange {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byID[id]
}

// GetByUUID returns the exchange with the given UUID, or nil.
func (s *Store) GetByUUID(uuid string) *proxy.Exchange {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byUUID[uuid]
}

// Filter holds optional filter criteria for List.
type Filter struct {
	// Host filters by the request host (substring match).
	Host string
	// Method filters by HTTP method (exact, upper-case).
	Method string
	// StatusMin / StatusMax filter by response status code range (0 = unset).
	StatusMin, StatusMax int
	// Search is a substring matched against the full URL.
	Search string
	// Limit caps the number of results (0 = no limit).
	Limit int
	// Offset skips the first N matches (for pagination).
	Offset int
}

// List returns exchanges in insertion order (oldest first), filtered by f.
func (s *Store) List(f Filter) []*proxy.Exchange {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*proxy.Exchange, 0, min(s.count, 256))
	skipped := 0

	// Iterate from oldest to newest.
	start := (s.head - s.count + s.cap) % s.cap
	for i := 0; i < s.count; i++ {
		ex := s.ring[(start+i)%s.cap]
		if ex == nil {
			continue
		}
		if !matches(ex, f) {
			continue
		}
		if skipped < f.Offset {
			skipped++
			continue
		}
		out = append(out, ex)
		if f.Limit > 0 && len(out) >= f.Limit {
			break
		}
	}
	return out
}

// Count returns the total number of stored exchanges (not filtered).
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.count
}

// Clear removes all exchanges.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ring = make([]*proxy.Exchange, s.cap)
	s.byID = make(map[uint64]*proxy.Exchange, s.cap)
	s.byUUID = make(map[string]*proxy.Exchange, s.cap)
	s.head = 0
	s.count = 0
}

// Handler returns a proxy.Handler that adds every exchange to the store.
func (s *Store) Handler() proxy.Handler {
	return func(ex *proxy.Exchange) {
		s.Add(ex)
	}
}

// Stats returns a human-readable summary (useful for the status bar).
func (s *Store) Stats() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fmt.Sprintf("%d exchanges (cap %d)", s.count, s.cap)
}

func matches(ex *proxy.Exchange, f Filter) bool {
	if f.Host != "" && ex.Request != nil {
		if !strings.Contains(ex.Request.Host, f.Host) {
			return false
		}
	}
	if f.Method != "" && ex.Request != nil {
		if ex.Request.Method != f.Method {
			return false
		}
	}
	if (f.StatusMin > 0 || f.StatusMax > 0) && ex.Response != nil {
		code := ex.Response.StatusCode
		if f.StatusMin > 0 && code < f.StatusMin {
			return false
		}
		if f.StatusMax > 0 && code > f.StatusMax {
			return false
		}
	}
	if f.Search != "" && ex.Request != nil {
		url := ex.Request.URL.String()
		if !strings.Contains(strings.ToLower(url), strings.ToLower(f.Search)) {
			return false
		}
	}
	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
