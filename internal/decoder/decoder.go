// Package decoder defines the Decoder interface that all protocol decoders
// must implement, plus a registry for looking up decoders by protocol.
//
// The proxy core calls Decoder.CanHandle after the TLS handshake to
// determine which decoder should process the connection. The first decoder
// that returns true from CanHandle is used; decoders are checked in
// registration order.
//
// Built-in decoders (HTTP/1.1) are pre-registered. Additional decoders
// (HTTP/2, gRPC, WebSocket, raw TCP) can be registered before the proxy
// starts.
package decoder

import (
	"context"
	"net"

	"mitm/internal/proxy"
)

// Decoder decodes a pair of clear-text connections into a stream of exchanges.
//
// Implementations must be safe for concurrent use across goroutines since each
// intercepted TLS connection runs in its own goroutine.
type Decoder interface {
	// Name returns the protocol name, e.g. "HTTP/1.1", "HTTP/2", "gRPC".
	Name() string

	// Priority controls decoder selection order. Higher priority wins.
	// Built-in decoders use priority 0; override with a positive value.
	Priority() int

	// CanHandle inspects the first few bytes of the clear-text stream and
	// returns true if this decoder can handle it. peek is at most 8 bytes
	// and must not be consumed.
	CanHandle(peek []byte, alpn string) bool

	// Decode processes the full clear-text conversation, emitting one
	// Exchange per request/response cycle by calling out. It must return
	// when either connection closes or ctx is cancelled.
	Decode(
		ctx context.Context,
		clientConn net.Conn,
		serverConn net.Conn,
		tlsInfo *proxy.TLSInfo,
		out func(*proxy.Exchange),
	) error
}

// Registry holds registered decoders in priority order.
type Registry struct {
	decoders []Decoder
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a decoder. Decoders are sorted by Priority (descending) on
// each registration so the highest priority decoder is checked first.
func (r *Registry) Register(d Decoder) {
	r.decoders = append(r.decoders, d)
	// Insertion sort by priority descending — list is tiny so O(n²) is fine.
	for i := len(r.decoders) - 1; i > 0; i-- {
		if r.decoders[i].Priority() > r.decoders[i-1].Priority() {
			r.decoders[i], r.decoders[i-1] = r.decoders[i-1], r.decoders[i]
		} else {
			break
		}
	}
}

// Select returns the first decoder that can handle the given peek bytes and
// ALPN string. Returns nil if no registered decoder matches.
func (r *Registry) Select(peek []byte, alpn string) Decoder {
	for _, d := range r.decoders {
		if d.CanHandle(peek, alpn) {
			return d
		}
	}
	return nil
}

// All returns all registered decoders in priority order.
func (r *Registry) All() []Decoder {
	out := make([]Decoder, len(r.decoders))
	copy(out, r.decoders)
	return out
}
