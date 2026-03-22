package bus_test

import (
	"testing"
	"time"

	"mitm/internal/bus"
	"mitm/internal/proxy"
)

func TestBus_PublishReceive(t *testing.T) {
	b := bus.New()
	sub := b.Subscribe(8)
	defer b.Unsubscribe(sub)

	ex := &proxy.Exchange{ID: 42, UUID: "test-uuid"}
	b.Publish(ex)

	select {
	case ev := <-sub.C():
		if ev.Exchange.ID != 42 {
			t.Errorf("expected ID=42, got %d", ev.Exchange.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestBus_MultipleSubscribers(t *testing.T) {
	b := bus.New()
	s1 := b.Subscribe(4)
	s2 := b.Subscribe(4)
	defer b.Unsubscribe(s1)
	defer b.Unsubscribe(s2)

	b.Publish(&proxy.Exchange{ID: 1})

	recv := func(sub *bus.Subscriber) {
		select {
		case <-sub.C():
		case <-time.After(time.Second):
			t.Error("subscriber did not receive event")
		}
	}
	recv(s1)
	recv(s2)
}

func TestBus_SlowSubscriberDrops(t *testing.T) {
	b := bus.New()
	// Buffer of 1 — will drop if we publish faster than the subscriber reads.
	sub := b.Subscribe(1)
	defer b.Unsubscribe(sub)

	for i := 0; i < 10; i++ {
		b.Publish(&proxy.Exchange{ID: uint64(i)})
	}

	if sub.Dropped() == 0 {
		t.Error("expected some drops for a slow subscriber with buf=1")
	}
}

func TestBus_UnsubscribeClosesChan(t *testing.T) {
	b := bus.New()
	sub := b.Subscribe(4)
	b.Unsubscribe(sub)

	// Channel should be closed.
	select {
	case _, ok := <-sub.C():
		if ok {
			t.Error("channel should be closed after Unsubscribe")
		}
	default:
		t.Error("channel should be closed, not just empty")
	}
}
