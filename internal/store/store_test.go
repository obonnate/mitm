package store_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"mitm/internal/proxy"
	"mitm/internal/store"
)

func makeExchange(id uint64, method, rawURL string, status int) *proxy.Exchange {
	u, _ := url.Parse(rawURL)
	ex := &proxy.Exchange{
		ID:       id,
		UUID:     fmt.Sprintf("uuid-%d", id),
		Protocol: proxy.ProtoHTTP1,
		Request: &http.Request{
			Method: method,
			URL:    u,
			Host:   u.Host,
		},
	}
	if status > 0 {
		ex.Response = &http.Response{StatusCode: status}
	}
	return ex
}

func TestStore_AddAndGetByID(t *testing.T) {
	s := store.New(100)
	ex := makeExchange(1, "GET", "https://example.com/api", 200)
	s.Add(ex)

	got := s.GetByID(1)
	if got == nil {
		t.Fatal("expected exchange, got nil")
	}
	if got.UUID != ex.UUID {
		t.Errorf("UUID mismatch: %s != %s", got.UUID, ex.UUID)
	}
}

func TestStore_RingEviction(t *testing.T) {
	s := store.New(3)
	for i := uint64(1); i <= 5; i++ {
		s.Add(makeExchange(i, "GET", fmt.Sprintf("https://ex%d.com/", i), 200))
	}

	if s.Count() != 3 {
		t.Errorf("expected count=3, got %d", s.Count())
	}

	// IDs 1 and 2 should have been evicted.
	if s.GetByID(1) != nil {
		t.Error("ID 1 should have been evicted")
	}
	if s.GetByID(3) == nil {
		t.Error("ID 3 should still be present")
	}
	if s.GetByID(5) == nil {
		t.Error("ID 5 should be present")
	}
}

func TestStore_ListFilter(t *testing.T) {
	s := store.New(100)
	s.Add(makeExchange(1, "GET", "https://api.example.com/users", 200))
	s.Add(makeExchange(2, "POST", "https://api.example.com/orders", 201))
	s.Add(makeExchange(3, "GET", "https://cdn.example.com/img.png", 304))
	s.Add(makeExchange(4, "DELETE", "https://api.example.com/users/1", 404))

	// Filter by method.
	results := s.List(store.Filter{Method: "GET"})
	if len(results) != 2 {
		t.Errorf("expected 2 GET exchanges, got %d", len(results))
	}

	// Filter by host substring.
	results = s.List(store.Filter{Host: "cdn"})
	if len(results) != 1 {
		t.Errorf("expected 1 cdn exchange, got %d", len(results))
	}

	// Filter by status range.
	results = s.List(store.Filter{StatusMin: 400, StatusMax: 499})
	if len(results) != 1 {
		t.Errorf("expected 1 4xx exchange, got %d", len(results))
	}

	// Pagination.
	all := s.List(store.Filter{})
	page1 := s.List(store.Filter{Limit: 2, Offset: 0})
	page2 := s.List(store.Filter{Limit: 2, Offset: 2})
	if len(page1) != 2 || len(page2) != 2 {
		t.Errorf("pagination: page1=%d page2=%d (want 2, 2)", len(page1), len(page2))
	}
	if page1[0].ID == page2[0].ID {
		t.Error("pages should not overlap")
	}
	_ = all
}

func TestStore_Clear(t *testing.T) {
	s := store.New(100)
	for i := uint64(1); i <= 5; i++ {
		s.Add(makeExchange(i, "GET", "https://x.com/", 200))
	}
	s.Clear()
	if s.Count() != 0 {
		t.Errorf("expected count=0 after clear, got %d", s.Count())
	}
}
