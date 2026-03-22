// Package bus provides a lightweight publish/subscribe event bus that
// connects the proxy core to downstream consumers (store, API, GUI).
//
// The bus is safe for concurrent use. Publishers never block: if a
// subscriber's channel is full the event is dropped and a drop counter is
// incremented. This keeps the hot proxy path free of backpressure.
package bus

import (
	"sync"
	"sync/atomic"

	"mitm/internal/proxy"
)

// Event wraps a completed exchange emitted by the proxy.
type Event struct {
	Exchange *proxy.Exchange
}

// Subscriber receives events from the bus.
type Subscriber struct {
	ch      chan Event
	dropped atomic.Uint64
	id      uint64
}

// C returns the read-only channel the subscriber should range over.
func (s *Subscriber) C() <-chan Event { return s.ch }

// Dropped returns the number of events that were dropped because the channel
// was full.
func (s *Subscriber) Dropped() uint64 { return s.dropped.Load() }

// Bus is the central exchange event dispatcher.
type Bus struct {
	mu   sync.RWMutex
	subs map[uint64]*Subscriber
	seq  atomic.Uint64
}

// New creates a new Bus.
func New() *Bus {
	return &Bus{subs: make(map[uint64]*Subscriber)}
}

// Subscribe registers a new subscriber with a buffered channel of size buf.
// Call Unsubscribe when done to avoid leaking goroutines.
func (b *Bus) Subscribe(buf int) *Subscriber {
	id := b.seq.Add(1)
	s := &Subscriber{
		ch: make(chan Event, buf),
		id: id,
	}
	b.mu.Lock()
	b.subs[id] = s
	b.mu.Unlock()
	return s
}

// Unsubscribe removes the subscriber and closes its channel.
func (b *Bus) Unsubscribe(s *Subscriber) {
	b.mu.Lock()
	delete(b.subs, s.id)
	b.mu.Unlock()
	close(s.ch)
}

// Publish sends an event to all registered subscribers.
// It never blocks: slow subscribers receive a dropped-event count increment.
func (b *Bus) Publish(ex *proxy.Exchange) {
	ev := Event{Exchange: ex}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, s := range b.subs {
		select {
		case s.ch <- ev:
		default:
			s.dropped.Add(1)
		}
	}
}

// Handler returns a proxy.Handler that publishes every exchange onto the bus.
func (b *Bus) Handler() proxy.Handler {
	return func(ex *proxy.Exchange) {
		b.Publish(ex)
	}
}
