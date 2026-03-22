// Package http2decoder provides full HTTP/2 interception.
//
// Design
// ──────
// An HTTP/2 connection is a single TLS pipe carrying multiplexed streams.
// After the TLS MITM handshake we have two clear-text net.Conn objects:
//
//	clientConn  — the browser/app talking to the proxy
//	serverConn  — the proxy talking to the origin
//
// We run two goroutines:
//
//	clientToServer  reads frames from clientConn, forwards them to serverConn,
//	                and records request headers/data per stream.
//
//	serverToClient  reads frames from serverConn, forwards them to clientConn,
//	                and records response headers/data per stream.
//
// The critical detail: golang.org/x/net/http2.Framer has SEPARATE read and
// write sides.  We create one Framer per direction:
//
//	readFramer  reads from the source connection (ReadFrame)
//	writeFramer writes to the destination connection (WriteRawFrame)
//
// Using a single Framer for both directions corrupts the HPACK state because
// the framer's internal encoder/decoder state gets crossed.
//
// Stream lifecycle
// ────────────────
// Client-initiated streams always have odd IDs (1, 3, 5, …).
// A stream is "request-complete" when we see END_STREAM on a HEADERS or DATA
// frame from the client side, and "response-complete" when we see END_STREAM
// from the server side.  Once both flags are set we emit one Exchange.
//
// CONTINUATION frames
// ───────────────────
// HTTP/2 allows a HEADERS frame to be followed by CONTINUATION frames before
// any other frame type on that stream.  We accumulate all header block
// fragments before decoding with hpack.
package http2decoder

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"

	"mitm/internal/proxy"
)

// ════════════════════════════════════════════════════════════════════════════
// Public Decoder — implements decoder.Decoder
// ════════════════════════════════════════════════════════════════════════════

// Decoder is the HTTP/2 protocol decoder.
type Decoder struct{}

func (Decoder) Name() string  { return "HTTP/2" }
func (Decoder) Priority() int { return 10 }

// CanHandle returns true when the negotiated ALPN is "h2" or the first bytes
// of the stream match the HTTP/2 client connection preface.
func (Decoder) CanHandle(peek []byte, alpn string) bool {
	if alpn == "h2" {
		return true
	}
	const preface = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"
	n := len(peek)
	if n > len(preface) {
		n = len(preface)
	}
	return bytes.HasPrefix(peek, []byte(preface[:n]))
}

// Decode runs the full HTTP/2 session.  It blocks until both legs close or ctx
// is cancelled.
func (Decoder) Decode(
	ctx context.Context,
	clientConn net.Conn,
	serverConn net.Conn,
	tlsInfo *proxy.TLSInfo,
	emit func(*proxy.Exchange),
) error {
	s := newSession(clientConn, serverConn, tlsInfo, emit)
	return s.run(ctx)
}

// ════════════════════════════════════════════════════════════════════════════
// session
// ════════════════════════════════════════════════════════════════════════════

type session struct {
	clientConn net.Conn
	serverConn net.Conn
	tlsInfo    *proxy.TLSInfo
	emit       func(*proxy.Exchange)

	mu      sync.Mutex
	streams map[uint32]*h2stream

	// Per-direction HPACK decoders.  Must not be shared across goroutines.
	clientHPACK *hpack.Decoder // decodes client→server header blocks
	serverHPACK *hpack.Decoder // decodes server→client header blocks

	seqGen atomic.Uint64
}

func newSession(client, server net.Conn, tls *proxy.TLSInfo, emit func(*proxy.Exchange)) *session {
	return &session{
		clientConn:  client,
		serverConn:  server,
		tlsInfo:     tls,
		emit:        emit,
		streams:     make(map[uint32]*h2stream),
		clientHPACK: hpack.NewDecoder(4096, nil),
		serverHPACK: hpack.NewDecoder(4096, nil),
	}
}

func (s *session) run(ctx context.Context) error {
	// ── Forward the 24-byte client connection preface verbatim ───────────────
	// (PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n)
	// The server must receive this before any frames or it will send a GOAWAY.
	preface := make([]byte, 24)
	if _, err := io.ReadFull(s.clientConn, preface); err != nil {
		return fmt.Errorf("h2: read preface: %w", err)
	}
	if _, err := s.serverConn.Write(preface); err != nil {
		return fmt.Errorf("h2: forward preface: %w", err)
	}

	errc := make(chan error, 2)
	go func() { errc <- s.relay(ctx, s.clientConn, s.serverConn, s.clientHPACK, dirClient) }()
	go func() { errc <- s.relay(ctx, s.serverConn, s.clientConn, s.serverHPACK, dirServer) }()

	select {
	case err := <-errc:
		// One direction closed — signal the other to stop.
		s.clientConn.Close()
		s.serverConn.Close()
		<-errc // drain
		return err
	case <-ctx.Done():
		s.clientConn.Close()
		s.serverConn.Close()
		<-errc
		<-errc
		return ctx.Err()
	}
}

type direction uint8

const (
	dirClient direction = iota // frames flowing client → server
	dirServer                  // frames flowing server → client
)

// relay reads every HTTP/2 frame from src, immediately forwards the raw bytes
// to dst, then inspects the frame to update stream state and emit exchanges.
//
// Two separate Framers:
//   - readFramer  has src as its reader — used only for ReadFrame.
//   - writeFramer has dst as its writer — used only for WriteRawFrame.
//
// We never use a single Framer for both ends because its internal HPACK
// encoder/decoder tables would get corrupted.
func (s *session) relay(
	ctx context.Context,
	src, dst net.Conn,
	hpackDec *hpack.Decoder,
	dir direction,
) error {
	srcBuf := bufio.NewReaderSize(src, 32*1024)

	// readFramer reads from src; we pass a io.Discard writer because we forward
	// manually using WriteRawFrame on a separate writeFramer.
	readFramer := http2.NewFramer(io.Discard, srcBuf)
	readFramer.AllowIllegalReads = true
	readFramer.ReadMetaHeaders = nil // we decode HPACK ourselves

	// writeFramer writes to dst; its reader is never used.
	writeFramer := http2.NewFramer(dst, bytes.NewReader(nil))
	writeFramer.AllowIllegalReads = true

	// mu guards writeFramer so that if we ever need to write from multiple
	// goroutines (not currently the case) we don't interleave partial frames.
	var writeMu sync.Mutex

	writeRaw := func(raw []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_, err := dst.Write(raw)
		return err
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		src.SetReadDeadline(time.Now().Add(90 * time.Second))

		// Peek the 9-byte frame header to know how many bytes to read.
		header, err := srcBuf.Peek(9)
		if err != nil {
			return err
		}
		// Length field is the first 3 bytes, big-endian.
		payloadLen := int(header[0])<<16 | int(header[1])<<8 | int(header[2])
		frameLen := 9 + payloadLen

		// Read the full frame into a buffer so we can forward it raw.
		raw := make([]byte, frameLen)
		if _, err := io.ReadFull(srcBuf, raw); err != nil {
			return err
		}

		// Forward immediately — the observed application sees the traffic with
		// minimum added latency.
		if err := writeRaw(raw); err != nil {
			return err
		}

		// Parse the frame for observation (best-effort: errors are non-fatal).
		frameReader := bytes.NewReader(raw)
		inspFramer := http2.NewFramer(io.Discard, frameReader)
		inspFramer.AllowIllegalReads = true
		frame, err := inspFramer.ReadFrame()
		if err != nil {
			// Unrecognised or malformed frame — already forwarded, just skip.
			continue
		}

		s.inspect(frame, hpackDec, dir)
	}
}

// ════════════════════════════════════════════════════════════════════════════
// Frame inspection — purely observational, never modifies traffic
// ════════════════════════════════════════════════════════════════════════════

func (s *session) inspect(frame http2.Frame, dec *hpack.Decoder, dir direction) {
	switch f := frame.(type) {

	case *http2.MetaHeadersFrame:
		// ReadMetaHeaders=nil so this won't fire, but handle it defensively.
		s.onMetaHeaders(f, dir)

	case *http2.HeadersFrame:
		s.onHeaders(f, dec, dir)

	case *http2.ContinuationFrame:
		s.onContinuation(f, dec, dir)

	case *http2.DataFrame:
		s.onData(f, dir)

	case *http2.RSTStreamFrame:
		s.mu.Lock()
		delete(s.streams, f.StreamID)
		s.mu.Unlock()

	case *http2.GoAwayFrame:
		// Connection is closing — no action needed, relay will EOF soon.

	case *http2.WindowUpdateFrame:
		// Flow control — no exchange data.

	case *http2.PingFrame:
		// Keep-alive — no exchange data.

	case *http2.SettingsFrame:
		// Connection-level settings — update HPACK table size if present.
		f.ForeachSetting(func(s http2.Setting) error {
			if s.ID == http2.SettingHeaderTableSize {
				dec.SetMaxDynamicTableSize(s.Val)
			}
			return nil
		})
	}
}

func (s *session) onMetaHeaders(f *http2.MetaHeadersFrame, dir direction) {
	fields := make([]hpack.HeaderField, len(f.Fields))
	for i, hf := range f.Fields {
		fields[i] = hpack.HeaderField{Name: hf.Name, Value: hf.Value}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	str := s.getOrCreate(f.StreamID)
	if dir == dirClient {
		str.reqHeaderFields = append(str.reqHeaderFields, fields...)
		if f.StreamEnded() {
			str.reqDone = true
			s.tryComplete(f.StreamID, str)
		}
	} else {
		str.respHeaderFields = append(str.respHeaderFields, fields...)
		if f.StreamEnded() {
			str.respDone = true
			s.tryComplete(f.StreamID, str)
		}
	}
}

func (s *session) onHeaders(f *http2.HeadersFrame, dec *hpack.Decoder, dir direction) {
	// Buffer the raw header block fragment; decode it once END_HEADERS is set.
	s.mu.Lock()
	defer s.mu.Unlock()
	str := s.getOrCreate(f.StreamID)

	if dir == dirClient {
		str.reqHeaderBuf = append(str.reqHeaderBuf, f.HeaderBlockFragment()...)
		str.reqHeadersDone = f.HeadersEnded()
		if str.reqHeadersDone {
			str.reqHeaderFields, _ = dec.DecodeFull(str.reqHeaderBuf)
			str.reqHeaderBuf = nil
		}
		if f.StreamEnded() {
			str.reqDone = true
			s.tryComplete(f.StreamID, str)
		}
	} else {
		str.respHeaderBuf = append(str.respHeaderBuf, f.HeaderBlockFragment()...)
		str.respHeadersDone = f.HeadersEnded()
		if str.respHeadersDone {
			str.respHeaderFields, _ = dec.DecodeFull(str.respHeaderBuf)
			str.respHeaderBuf = nil
		}
		if f.StreamEnded() {
			str.respDone = true
			s.tryComplete(f.StreamID, str)
		}
	}
}

func (s *session) onContinuation(f *http2.ContinuationFrame, dec *hpack.Decoder, dir direction) {
	s.mu.Lock()
	defer s.mu.Unlock()
	str, ok := s.streams[f.StreamID]
	if !ok {
		return
	}
	if dir == dirClient {
		str.reqHeaderBuf = append(str.reqHeaderBuf, f.HeaderBlockFragment()...)
		if f.HeadersEnded() {
			str.reqHeaderFields, _ = dec.DecodeFull(str.reqHeaderBuf)
			str.reqHeaderBuf = nil
			str.reqHeadersDone = true
		}
	} else {
		str.respHeaderBuf = append(str.respHeaderBuf, f.HeaderBlockFragment()...)
		if f.HeadersEnded() {
			str.respHeaderFields, _ = dec.DecodeFull(str.respHeaderBuf)
			str.respHeaderBuf = nil
			str.respHeadersDone = true
		}
	}
}

func (s *session) onData(f *http2.DataFrame, dir direction) {
	s.mu.Lock()
	defer s.mu.Unlock()
	str, ok := s.streams[f.StreamID]
	if !ok {
		return
	}
	if dir == dirClient {
		str.reqBody = append(str.reqBody, f.Data()...)
		if f.StreamEnded() {
			str.reqDone = true
			s.tryComplete(f.StreamID, str)
		}
	} else {
		str.respBody = append(str.respBody, f.Data()...)
		if f.StreamEnded() {
			str.respDone = true
			s.tryComplete(f.StreamID, str)
		}
	}
}

// getOrCreate returns the stream for sid, creating it if necessary.
// Must be called with s.mu held.
func (s *session) getOrCreate(sid uint32) *h2stream {
	str, ok := s.streams[sid]
	if !ok {
		str = &h2stream{id: sid, start: time.Now()}
		s.streams[sid] = str
	}
	return str
}

// tryComplete emits an Exchange once both request and response are finished.
// Must be called with s.mu held.
func (s *session) tryComplete(sid uint32, str *h2stream) {
	if !str.reqDone || !str.respDone {
		return
	}
	delete(s.streams, sid)

	ex := &proxy.Exchange{
		ID:       s.seqGen.Add(1),
		UUID:     newUUID(),
		Protocol: proxy.ProtoHTTP2,
		TLS:      s.tlsInfo,
		ReqBody:  str.reqBody,
		RespBody: str.respBody,
		Timing: proxy.Timing{
			Start: str.start,
			Done:  time.Now(),
		},
	}
	ex.Request = buildRequest(str.reqHeaderFields, str.reqBody)
	ex.Response = buildResponse(str.respHeaderFields, str.respBody)

	go s.emit(ex) // non-blocking hand-off
}

// ════════════════════════════════════════════════════════════════════════════
// h2stream — per-stream state
// ════════════════════════════════════════════════════════════════════════════

type h2stream struct {
	id    uint32
	start time.Time

	// Request side
	reqHeaderBuf    []byte
	reqHeadersDone  bool
	reqHeaderFields []hpack.HeaderField
	reqBody         []byte
	reqDone         bool

	// Response side
	respHeaderBuf    []byte
	respHeadersDone  bool
	respHeaderFields []hpack.HeaderField
	respBody         []byte
	respDone         bool
}

// ════════════════════════════════════════════════════════════════════════════
// Build net/http types from decoded HPACK pseudo-headers + regular headers
// ════════════════════════════════════════════════════════════════════════════

func buildRequest(fields []hpack.HeaderField, body []byte) *http.Request {
	req := &http.Request{
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
		ProtoMajor: 2,
		ProtoMinor: 0,
		Proto:      "HTTP/2.0",
	}
	for _, f := range fields {
		switch f.Name {
		case ":method":
			req.Method = f.Value
		case ":path":
			req.URL, _ = url.ParseRequestURI(f.Value)
		case ":authority":
			req.Host = f.Value
		case ":scheme":
			if req.URL == nil {
				req.URL = &url.URL{}
			}
			req.URL.Scheme = f.Value
		default:
			if !strings.HasPrefix(f.Name, ":") {
				req.Header.Add(http.CanonicalHeaderKey(f.Name), f.Value)
			}
		}
	}
	if req.URL != nil && req.Host != "" && req.URL.Host == "" {
		req.URL.Host = req.Host
	}
	if req.Method == "" {
		req.Method = http.MethodGet
	}
	req.ContentLength = int64(len(body))
	return req
}

func buildResponse(fields []hpack.HeaderField, body []byte) *http.Response {
	resp := &http.Response{
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Proto:      "HTTP/2.0",
		ProtoMajor: 2,
	}
	for _, f := range fields {
		switch f.Name {
		case ":status":
			fmt.Sscanf(f.Value, "%d", &resp.StatusCode)
			resp.Status = f.Value + " " + http.StatusText(resp.StatusCode)
		default:
			if !strings.HasPrefix(f.Name, ":") {
				resp.Header.Add(http.CanonicalHeaderKey(f.Name), f.Value)
			}
		}
	}
	resp.ContentLength = int64(len(body))
	return resp
}

// ════════════════════════════════════════════════════════════════════════════
// Helpers
// ════════════════════════════════════════════════════════════════════════════

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
