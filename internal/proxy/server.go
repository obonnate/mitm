package proxy

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gotls "mitm/internal/tls"
)

// H2Decoder is the interface the proxy calls to handle a decoded HTTP/2
// connection.  Implemented by plugins/http2decoder.Decoder.
type H2Decoder interface {
	Decode(ctx context.Context, clientConn, serverConn net.Conn, tlsInfo *TLSInfo, emit func(*Exchange)) error
}

// passthroughH2 is the no-op fallback used when no H2Decoder is registered.
type passthroughH2 struct{}

func (passthroughH2) Decode(_ context.Context, c, s net.Conn, _ *TLSInfo, _ func(*Exchange)) error {
	tunnel2(c, s)
	return nil
}

// Handler is the hook the proxy calls after every completed exchange.
// Implementations should be non-blocking; heavy work should be done in a
// goroutine. The exchange must not be modified after returning.
type Handler func(ex *Exchange)

// Config holds all knobs for the proxy server.
type Config struct {
	// Addr is the listen address, e.g. ":8080".
	Addr string

	// CA is the certificate authority used for TLS interception.
	CA *gotls.CA

	// OnExchange is called once per completed exchange (request + response).
	OnExchange Handler

	// H2Decoder handles decoded HTTP/2 connections.
	// If nil, HTTP/2 traffic is tunnelled transparently.
	H2Decoder H2Decoder

	// PassthroughHosts is a list of host globs that should not be intercepted.
	PassthroughHosts []string

	// UpstreamProxy, if set, forwards traffic through another proxy.
	UpstreamProxy string
}

// Server is the main proxy HTTP server.
type Server struct {
	cfg     Config
	seq     atomic.Uint64
	httpSrv *http.Server
	h2dec   H2Decoder
}

// New creates a new proxy Server with the given Config.
func New(cfg Config) *Server {
	h2dec := cfg.H2Decoder
	if h2dec == nil {
		h2dec = passthroughH2{}
	}
	s := &Server{cfg: cfg, h2dec: h2dec}
	s.httpSrv = &http.Server{
		Addr:              cfg.Addr,
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

// ListenAndServe starts the proxy. It blocks until the context is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("proxy: listen %s: %w", s.cfg.Addr, err)
	}
	fmt.Printf("[proxy] Listening on %s\n", ln.Addr())

	go func() {
		<-ctx.Done()
		s.httpSrv.Shutdown(context.Background())
	}()

	return s.httpSrv.Serve(ln)
}

// ServeHTTP is the single entry point for all proxy traffic.
//
//   - CONNECT requests → TLS intercept pipeline
//   - Everything else → plain HTTP forward pipeline
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		s.handleCONNECT(w, r)
		return
	}
	s.handleHTTP(w, r)
}

// ════════════════════════════════════════════════════════════════════════════
// Plain HTTP pipeline
// ════════════════════════════════════════════════════════════════════════════

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request) {
	ex := s.newExchange(ProtoHTTP1, nil)
	ex.Timing.Start = time.Now()

	// Drain and capture the request body.
	reqBody, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		http.Error(w, "proxy: read request body", http.StatusBadGateway)
		return
	}
	ex.ReqBody = reqBody
	r.Body = io.NopCloser(bytes.NewReader(reqBody))

	// Store a snapshot of the raw request for replay / hex view.
	rawReq, _ := httputil.DumpRequest(r, false)
	ex.RawReqBytes = append(rawReq, reqBody...)
	ex.Request = r

	// Sanitise the outgoing request (strip hop-by-hop headers, fix URL).
	outReq := r.Clone(r.Context())
	outReq.RequestURI = ""
	removeHopByHop(outReq.Header)

	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: 30 * time.Second,
	}

	ex.Timing.RequestSent = time.Now()
	resp, err := transport.RoundTrip(outReq)
	if err != nil {
		ex.Error = err
		s.emit(ex)
		http.Error(w, fmt.Sprintf("proxy: upstream: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	ex.Timing.FirstByte = time.Now()

	// Drain and capture the response body (decompressing if needed).
	respBody, err := drainBody(resp)
	if err != nil {
		ex.Error = err
		s.emit(ex)
		http.Error(w, "proxy: read response body", http.StatusBadGateway)
		return
	}
	ex.RespBody = respBody
	ex.Response = resp
	ex.Timing.Done = time.Now()

	// Write response back to the client.
	copyHeader(w.Header(), resp.Header)
	// Remove content-encoding since we already decompressed.
	w.Header().Del("Content-Encoding")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(respBody)))
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)

	s.emit(ex)
}

// ════════════════════════════════════════════════════════════════════════════
// CONNECT / TLS intercept pipeline
// ════════════════════════════════════════════════════════════════════════════

// handleCONNECT responds 200 Connection Established, then hijacks the raw TCP
// connection. If the target host is in PassthroughHosts it blindly tunnels the
// bytes; otherwise it performs a MITM TLS handshake on both legs.
func (s *Server) handleCONNECT(w http.ResponseWriter, r *http.Request) {
	host := r.Host // host:port

	// Tell the client the tunnel is ready.
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "proxy: hijack not supported", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)

	clientConn, clientBuf, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer clientConn.Close()

	if s.passthrough(host) {
		s.tunnel(clientConn, host)
		return
	}

	s.interceptTLS(clientConn, clientBuf, host)
}

// interceptTLS performs the MITM handshake:
//
//  1. TLS-terminate the client connection using a forged cert for the target host.
//  2. Open a real TLS connection to the upstream server, capturing its cert.
//  3. Decode HTTP/1.1 or HTTP/2 traffic on the resulting pair of clear-text pipes.
func (s *Server) interceptTLS(clientConn net.Conn, clientBuf *bufio.ReadWriter, host string) {
	// ── Step 1: Forge a cert and terminate TLS toward the client ──────────────

	leafCert, err := s.cfg.CA.CertFor(host)
	if err != nil {
		fmt.Printf("[tls] cert forge failed for %s: %v\n", host, err)
		return
	}

	clientTLSCfg := s.cfg.CA.ServerTLSConfig()
	// Override GetCertificate with the pre-fetched cert so we don't re-lock.
	clientTLSCfg.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		return leafCert, nil
	}

	// Wrap the already-hijacked buffered reader so buffered bytes aren't lost.
	clientStream := &bufConn{Conn: clientConn, r: clientBuf}
	clientTLS := tls.Server(clientStream, clientTLSCfg)

	if err := clientTLS.Handshake(); err != nil {
		fmt.Printf("[tls] client handshake %s: %v\n", host, err)
		return
	}
	clientState := clientTLS.ConnectionState()

	// ── Step 2: Connect to the real upstream server ───────────────────────────

	hostname := host
	if h, _, e := net.SplitHostPort(host); e == nil {
		hostname = h
	}

	var upstreamCertDER []byte
	upstreamTLSCfg := gotls.UpstreamTLSConfig(hostname, &upstreamCertDER)
	// Mirror the ALPN offer from the client so we negotiate the same protocol.
	if clientState.NegotiatedProtocol != "" {
		upstreamTLSCfg.NextProtos = []string{clientState.NegotiatedProtocol}
	} else {
		upstreamTLSCfg.NextProtos = []string{"http/1.1"}
	}

	tcpStart := time.Now()
	rawUpstream, err := net.DialTimeout("tcp", host, 15*time.Second)
	if err != nil {
		fmt.Printf("[tls] dial upstream %s: %v\n", host, err)
		return
	}
	tcpDone := time.Now()

	upstreamTLS := tls.Client(rawUpstream, upstreamTLSCfg)
	if err := upstreamTLS.Handshake(); err != nil {
		fmt.Printf("[tls] upstream handshake %s: %v\n", host, err)
		rawUpstream.Close()
		return
	}
	tlsDone := time.Now()
	upstreamState := upstreamTLS.ConnectionState()

	// ── Step 3: Build TLSInfo for all exchanges on this connection ────────────

	tlsInfo := &TLSInfo{
		Version:      clientState.Version,
		CipherSuite:  clientState.CipherSuite,
		ServerName:   clientState.ServerName,
		ALPN:         upstreamState.NegotiatedProtocol,
		ForgedCert:   leafCert.Certificate[0],
		UpstreamCert: upstreamCertDER,
	}

	baseTiming := Timing{
		TCPConnected: tcpDone,
		TLSHandshook: tlsDone,
	}
	_ = tcpStart

	// ── Step 4: Hand off to HTTP decoder ──────────────────────────────────────

	proto := ProtoHTTP1
	if upstreamState.NegotiatedProtocol == "h2" {
		proto = ProtoHTTP2
	}

	d := &httpDecoder{
		server:       s,
		clientConn:   clientTLS,
		upstreamConn: upstreamTLS,
		host:         host,
		tlsInfo:      tlsInfo,
		baseTiming:   baseTiming,
		proto:        proto,
	}
	d.run()
}

// ════════════════════════════════════════════════════════════════════════════
// HTTP decoder — runs on top of an already-clear-text pair of connections
// ════════════════════════════════════════════════════════════════════════════

type httpDecoder struct {
	server       *Server
	clientConn   net.Conn
	upstreamConn net.Conn
	host         string
	tlsInfo      *TLSInfo
	baseTiming   Timing
	proto        Protocol
}

func (d *httpDecoder) run() {
	defer d.clientConn.Close()
	defer d.upstreamConn.Close()

	if d.proto == ProtoHTTP2 {
		if err := d.server.h2dec.Decode(
			context.Background(),
			d.clientConn,
			d.upstreamConn,
			d.tlsInfo,
			d.server.emit,
		); err != nil && !isClosedErr(err) {
			fmt.Printf("[h2] %s: %v\n", d.host, err)
		}
		return
	}

	d.runHTTP1()
}

func (d *httpDecoder) runHTTP1() {
	clientReader := bufio.NewReader(d.clientConn)
	transport := &http.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return d.upstreamConn, nil
		},
		// Disable connection pooling — we own this connection.
		DisableKeepAlives: true,
	}

	for {
		// Read the next request from the (already decrypted) client stream.
		req, err := http.ReadRequest(clientReader)
		if err != nil {
			return // EOF or client gone
		}

		ex := d.server.newExchange(ProtoHTTP1, d.tlsInfo)
		ex.Timing = d.baseTiming
		ex.Timing.Start = time.Now()
		ex.Request = req

		// Capture request body.
		reqBody, _ := io.ReadAll(req.Body)
		req.Body.Close()
		ex.ReqBody = reqBody
		req.Body = io.NopCloser(bytes.NewReader(reqBody))

		rawReq, _ := httputil.DumpRequest(req, false)
		ex.RawReqBytes = append(rawReq, reqBody...)

		// Fix the request for forwarding.
		outReq := req.Clone(req.Context())
		outReq.URL.Scheme = "https"
		outReq.URL.Host = d.host
		outReq.RequestURI = ""
		removeHopByHop(outReq.Header)

		ex.Timing.RequestSent = time.Now()
		resp, err := transport.RoundTrip(outReq)
		if err != nil {
			ex.Error = err
			d.server.emit(ex)
			return
		}

		ex.Timing.FirstByte = time.Now()

		respBody, err := drainBody(resp)
		if err != nil {
			ex.Error = err
			resp.Body.Close()
			d.server.emit(ex)
			return
		}
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
		ex.RespBody = respBody
		ex.Response = resp
		ex.Timing.Done = time.Now()

		// Write the response back to the client over TLS.
		removeHopByHop(resp.Header)
		resp.Header.Del("Content-Encoding")
		resp.ContentLength = int64(len(respBody))
		resp.Body = io.NopCloser(bytes.NewReader(respBody))

		if err := resp.Write(d.clientConn); err != nil {
			ex.Error = err
		}

		d.server.emit(ex)

		// Check Connection: close
		if req.Close || resp.Close {
			return
		}
	}
}

// ════════════════════════════════════════════════════════════════════════════
// Transparent tunnel (passthrough / HTTP/2 fallback)
// ════════════════════════════════════════════════════════════════════════════

func (s *Server) tunnel(clientConn net.Conn, host string) {
	upstreamConn, err := net.DialTimeout("tcp", host, 15*time.Second)
	if err != nil {
		fmt.Printf("[proxy] tunnel dial %s: %v\n", host, err)
		return
	}
	tunnel2(clientConn, upstreamConn)
}

// tunnel2 copies bytes bidirectionally between two connections until either
// side closes or errors.
func tunnel2(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); io.Copy(a, b); tryCloseWrite(a) }()
	go func() { defer wg.Done(); io.Copy(b, a); tryCloseWrite(b) }()
	wg.Wait()
}

// tryCloseWrite performs a half-close on connections that support it
// (e.g. *tls.Conn, *net.TCPConn), so the other side sees EOF without
// closing the connection for writing in the other direction.
func tryCloseWrite(c net.Conn) {
	type closeWriter interface{ CloseWrite() error }
	if cw, ok := c.(closeWriter); ok {
		cw.CloseWrite()
	}
}

// isClosedErr reports whether err is a routine connection-closed error
// that should not be logged.
func isClosedErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "use of closed network connection") ||
		strings.Contains(s, "connection reset by peer") ||
		strings.Contains(s, "broken pipe") ||
		err == io.EOF ||
		err == io.ErrUnexpectedEOF
}

// ════════════════════════════════════════════════════════════════════════════
// Helpers
// ════════════════════════════════════════════════════════════════════════════

func (s *Server) newExchange(proto Protocol, tlsInfo *TLSInfo) *Exchange {
	id := s.seq.Add(1)
	return &Exchange{
		ID:       id,
		UUID:     newUUID(),
		Protocol: proto,
		TLS:      tlsInfo,
	}
}

func (s *Server) emit(ex *Exchange) {
	if s.cfg.OnExchange != nil {
		s.cfg.OnExchange(ex)
	}
}

func (s *Server) passthrough(host string) bool {
	if s.cfg.CA == nil {
		return true
	}
	h, _, _ := net.SplitHostPort(host)
	for _, pattern := range s.cfg.PassthroughHosts {
		if matchGlob(pattern, h) {
			return true
		}
	}
	return false
}

// drainBody reads the full response body, transparently decompressing gzip.
// It restores resp.Body to a fresh reader over the raw (compressed) bytes
// so callers that need the wire format can re-read it.
func drainBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()

	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = gz
	}

	return io.ReadAll(reader)
}

var hopByHop = []string{
	"Connection", "Proxy-Connection", "Keep-Alive", "Transfer-Encoding",
	"TE", "Trailer", "Upgrade", "Proxy-Authorization", "Proxy-Authenticate",
}

func removeHopByHop(h http.Header) {
	// First remove anything listed in Connection: header
	for _, v := range h["Connection"] {
		h.Del(v)
	}
	for _, k := range hopByHop {
		h.Del(k)
	}
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

// bufConn wraps a net.Conn with a bufio.ReadWriter so that bytes already
// buffered before the hijack are not lost.
type bufConn struct {
	net.Conn
	r *bufio.ReadWriter
}

func (b *bufConn) Read(p []byte) (int, error) {
	if b.r != nil && b.r.Reader.Buffered() > 0 {
		return b.r.Read(p)
	}
	return b.Conn.Read(p)
}

// matchGlob matches a simple glob pattern (* = any sequence of non-dot chars)
// against host. e.g. "*.internal" matches "foo.internal".
func matchGlob(pattern, host string) bool {
	if pattern == host {
		return true
	}
	if len(pattern) > 2 && pattern[:2] == "*." {
		suffix := pattern[1:] // ".internal"
		return len(host) > len(suffix) && host[len(host)-len(suffix):] == suffix
	}
	return false
}

// newUUID returns a random UUID v4 string.
func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
