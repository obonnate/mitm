// Package api exposes the proxy internals over HTTP:
//
//	GET  /api/exchanges          — paginated list (JSON)
//	GET  /api/exchanges/:uuid    — single exchange detail (JSON)
//	POST /api/exchanges/:uuid/replay — replay with optional body overrides
//	GET  /api/ca.crt             — download the CA certificate
//	GET  /api/stats              — store + bus stats (JSON)
//	GET  /ws                     — WebSocket live stream of new exchanges
//	GET  /                       — Angular SPA (embed.FS)
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"mitm/internal/bus"
	"mitm/internal/proxy"
	"mitm/internal/store"
	gotls "mitm/internal/tls"
)

// Server is the API + GUI HTTP server.
type Server struct {
	store   *store.Store
	bus     *bus.Bus
	ca      *gotls.CA
	mux     *http.ServeMux
	httpSrv *http.Server

	wsMu      sync.RWMutex
	wsClients map[uint64]*wsClient
	wsSeq     uint64
}

// New creates an API Server. uiFS may be nil if the Angular build is not
// embedded (e.g. during development when ng serve handles the frontend).
func New(s *store.Store, b *bus.Bus, ca *gotls.CA, addr string) *Server {
	srv := &Server{
		store:     s,
		bus:       b,
		ca:        ca,
		mux:       http.NewServeMux(),
		wsClients: make(map[uint64]*wsClient),
	}
	srv.httpSrv = &http.Server{
		Addr:    addr,
		Handler: corsMiddleware(srv.mux),
	}
	srv.routes()
	return srv
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/exchanges", s.handleExchangeList)
	s.mux.HandleFunc("/api/exchanges/", s.handleExchangeDetail) // /uuid and /uuid/replay
	s.mux.HandleFunc("/api/ca.crt", s.handleCACert)
	s.mux.HandleFunc("/api/stats", s.handleStats)
	s.mux.HandleFunc("/ws", s.handleWebSocket)
	// Angular SPA — registered last so /api/* routes take priority.
	s.mux.Handle("/", staticHandler())
}

// ServeHTTP implements http.Handler so the API server can be embedded.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.httpSrv.Handler.ServeHTTP(w, r)
}

// Start begins listening. It also subscribes to the bus and fans events out
// to all connected WebSocket clients.
func (s *Server) Start() error {
	// Subscribe to the event bus and broadcast to WebSocket clients.
	sub := s.bus.Subscribe(512)
	go func() {
		for ev := range sub.C() {
			s.broadcast(ev.Exchange)
		}
	}()

	log.Printf("[api] Listening on %s", s.httpSrv.Addr)
	return s.httpSrv.ListenAndServe()
}

// ════════════════════════════════════════════════════════════════════════════
// REST handlers
// ════════════════════════════════════════════════════════════════════════════

// GET /api/exchanges?limit=50&offset=0&host=...&method=GET&search=...&status_min=200&status_max=299
func (s *Server) handleExchangeList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	f := store.Filter{
		Host:      q.Get("host"),
		Method:    q.Get("method"),
		Search:    q.Get("search"),
		Limit:     queryInt(q.Get("limit"), 50),
		Offset:    queryInt(q.Get("offset"), 0),
		StatusMin: queryInt(q.Get("status_min"), 0),
		StatusMax: queryInt(q.Get("status_max"), 0),
	}

	exchanges := s.store.List(f)
	summaries := make([]exchangeSummary, 0, len(exchanges))
	for _, ex := range exchanges {
		summaries = append(summaries, toSummary(ex))
	}

	writeJSON(w, map[string]any{
		"total":     s.store.Count(),
		"exchanges": summaries,
	})
}

// GET  /api/exchanges/:uuid
// POST /api/exchanges/:uuid/replay
func (s *Server) handleExchangeDetail(w http.ResponseWriter, r *http.Request) {
	// Strip /api/exchanges/ prefix.
	rest := strings.TrimPrefix(r.URL.Path, "/api/exchanges/")
	parts := strings.SplitN(rest, "/", 2)
	uuid := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}

	if uuid == "" {
		http.NotFound(w, r)
		return
	}

	ex := s.store.GetByUUID(uuid)
	if ex == nil {
		http.Error(w, "exchange not found", http.StatusNotFound)
		return
	}

	switch action {
	case "":
		writeJSON(w, toDetail(ex))
	case "replay":
		s.handleReplay(w, r, ex)
	default:
		http.NotFound(w, r)
	}
}

// POST /api/exchanges/:uuid/replay
// Body (JSON, all fields optional):
//
//	{ "method": "POST", "url": "...", "headers": {...}, "body": "..." }
func (s *Server) handleReplay(w http.ResponseWriter, r *http.Request, original *proxy.Exchange) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if original.Request == nil {
		http.Error(w, "original request is nil", http.StatusBadRequest)
		return
	}

	var overrides struct {
		Method  string            `json:"method"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&overrides); err != nil && err != io.EOF {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Build the replay request from the original, applying overrides.
	method := original.Request.Method
	if overrides.Method != "" {
		method = overrides.Method
	}
	targetURL := original.Request.URL.String()
	if overrides.URL != "" {
		targetURL = overrides.URL
	}

	var bodyReader io.Reader = strings.NewReader(string(original.ReqBody))
	if overrides.Body != "" {
		bodyReader = strings.NewReader(overrides.Body)
	}

	req, err := http.NewRequestWithContext(r.Context(), method, targetURL, bodyReader)
	if err != nil {
		http.Error(w, fmt.Sprintf("build request: %v", err), http.StatusBadRequest)
		return
	}

	// Copy original headers, then apply overrides.
	for k, vv := range original.Request.Header {
		req.Header[k] = vv
	}
	for k, v := range overrides.Headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	writeJSON(w, map[string]any{
		"status":  resp.StatusCode,
		"headers": resp.Header,
		"body":    string(respBody),
	})
}

// GET /api/ca.crt — lets the browser/OS download the CA cert for installation.
func (s *Server) handleCACert(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-x509-ca-cert")
	w.Header().Set("Content-Disposition", `attachment; filename="mitm-ca.crt"`)
	w.Write(s.ca.CACertPEM())
}

// GET /api/stats
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	s.wsMu.RLock()
	wsCount := len(s.wsClients)
	s.wsMu.RUnlock()

	writeJSON(w, map[string]any{
		"store":      s.store.Stats(),
		"ws_clients": wsCount,
	})
}

// ════════════════════════════════════════════════════════════════════════════
// WebSocket live stream
// ════════════════════════════════════════════════════════════════════════════

// wsClient represents one connected browser tab.
type wsClient struct {
	id   uint64
	conn *wsConn
}

// handleWebSocket upgrades an HTTP connection to a WebSocket and streams
// exchange summaries as they arrive from the bus.
//
// Protocol: the server sends JSON text frames with this shape:
//
//	{ "type": "exchange", "data": <ExchangeSummary> }
//	{ "type": "ping" }
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgradeWS(w, r)
	if err != nil {
		log.Printf("[ws] upgrade: %v", err)
		return
	}

	s.wsMu.Lock()
	s.wsSeq++
	id := s.wsSeq
	client := &wsClient{id: id, conn: conn}
	s.wsClients[id] = client
	s.wsMu.Unlock()

	defer func() {
		s.wsMu.Lock()
		delete(s.wsClients, id)
		s.wsMu.Unlock()
		conn.Close()
	}()

	// Send a ping every 30 s to keep the connection alive through proxies.
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	pingMsg, _ := json.Marshal(map[string]string{"type": "ping"})

	for {
		select {
		case <-ticker.C:
			if err := conn.WriteMessage(pingMsg); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

// broadcast sends an exchange summary to all connected WebSocket clients.
func (s *Server) broadcast(ex *proxy.Exchange) {
	msg, err := json.Marshal(map[string]any{
		"type": "exchange",
		"data": toSummary(ex),
	})
	if err != nil {
		return
	}

	s.wsMu.RLock()
	clients := make([]*wsClient, 0, len(s.wsClients))
	for _, c := range s.wsClients {
		clients = append(clients, c)
	}
	s.wsMu.RUnlock()

	for _, c := range clients {
		c.conn.WriteMessage(msg) // best-effort; closed conns return an error
	}
}

// ════════════════════════════════════════════════════════════════════════════
// JSON view models
// ════════════════════════════════════════════════════════════════════════════

type exchangeSummary struct {
	ID         uint64   `json:"id"`
	UUID       string   `json:"uuid"`
	Protocol   string   `json:"protocol"`
	Method     string   `json:"method"`
	Host       string   `json:"host"`
	Path       string   `json:"path"`
	Status     int      `json:"status"`
	DurationMs float64  `json:"duration_ms"`
	ReqSize    int      `json:"req_size"`
	RespSize   int      `json:"resp_size"`
	TLS        bool     `json:"tls"`
	Tags       []string `json:"tags"`
	Error      string   `json:"error,omitempty"`
	StartedAt  string   `json:"started_at"`
}

type exchangeDetail struct {
	exchangeSummary
	ReqHeaders  map[string][]string `json:"req_headers"`
	RespHeaders map[string][]string `json:"resp_headers"`
	ReqBody     string              `json:"req_body"`
	RespBody    string              `json:"resp_body"`
	TLSInfo     *tlsDetail          `json:"tls_info,omitempty"`
	Timing      timingDetail        `json:"timing"`
}

type tlsDetail struct {
	Version      string `json:"version"`
	CipherSuite  string `json:"cipher_suite"`
	ServerName   string `json:"server_name"`
	ALPN         string `json:"alpn"`
	ForgedCert   []byte `json:"forged_cert_der,omitempty"`
	UpstreamCert []byte `json:"upstream_cert_der,omitempty"`
}

type timingDetail struct {
	StartedAt  string  `json:"started_at"`
	DurationMs float64 `json:"duration_ms"`
	DNSMs      float64 `json:"dns_ms,omitempty"`
	TCPMs      float64 `json:"tcp_ms,omitempty"`
	TLSMs      float64 `json:"tls_ms,omitempty"`
	TTFBMs     float64 `json:"ttfb_ms,omitempty"`
	DownloadMs float64 `json:"download_ms,omitempty"`
}

func toSummary(ex *proxy.Exchange) exchangeSummary {
	s := exchangeSummary{
		ID:        ex.ID,
		UUID:      ex.UUID,
		Protocol:  string(ex.Protocol),
		TLS:       ex.TLS != nil,
		ReqSize:   len(ex.ReqBody),
		RespSize:  len(ex.RespBody),
		Tags:      ex.Tags(),
		StartedAt: ex.Timing.Start.Format(time.RFC3339Nano),
	}

	if ex.Request != nil {
		s.Method = ex.Request.Method
		s.Host = ex.Request.Host
		if ex.Request.URL != nil {
			s.Path = ex.Request.URL.RequestURI()
		}
	}
	if ex.Response != nil {
		s.Status = ex.Response.StatusCode
	}
	if ex.Error != nil {
		s.Error = ex.Error.Error()
	}

	d := ex.Timing.Duration()
	s.DurationMs = float64(d.Microseconds()) / 1000.0

	return s
}

func toDetail(ex *proxy.Exchange) exchangeDetail {
	d := exchangeDetail{
		exchangeSummary: toSummary(ex),
		ReqBody:         string(ex.ReqBody),
		RespBody:        string(ex.RespBody),
	}

	if ex.Request != nil {
		d.ReqHeaders = map[string][]string(ex.Request.Header)
	}
	if ex.Response != nil {
		d.RespHeaders = map[string][]string(ex.Response.Header)
	}

	if ex.TLS != nil {
		d.TLSInfo = &tlsDetail{
			Version:      proxy.TLSVersionString(ex.TLS.Version),
			CipherSuite:  fmt.Sprintf("0x%04x", ex.TLS.CipherSuite),
			ServerName:   ex.TLS.ServerName,
			ALPN:         ex.TLS.ALPN,
			ForgedCert:   ex.TLS.ForgedCert,
			UpstreamCert: ex.TLS.UpstreamCert,
		}
	}

	t := ex.Timing
	d.Timing = timingDetail{
		StartedAt:  t.Start.Format(time.RFC3339Nano),
		DurationMs: float64(t.Duration().Microseconds()) / 1000.0,
	}
	if !t.TCPConnected.IsZero() {
		d.Timing.TCPMs = float64(t.TCPConnected.Sub(t.Start).Microseconds()) / 1000.0
	}
	if !t.TLSHandshook.IsZero() && !t.TCPConnected.IsZero() {
		d.Timing.TLSMs = float64(t.TLSHandshook.Sub(t.TCPConnected).Microseconds()) / 1000.0
	}
	if !t.FirstByte.IsZero() {
		d.Timing.TTFBMs = float64(t.FirstByte.Sub(t.Start).Microseconds()) / 1000.0
	}
	if !t.Done.IsZero() && !t.FirstByte.IsZero() {
		d.Timing.DownloadMs = float64(t.Done.Sub(t.FirstByte).Microseconds()) / 1000.0
	}

	return d
}

// ════════════════════════════════════════════════════════════════════════════
// Minimal WebSocket server-side upgrade (no dependency on gorilla/ws)
// ════════════════════════════════════════════════════════════════════════════

// wsConn is a minimal WebSocket connection that can only write text frames.
// For a production build, replace this with nhooyr.io/websocket or
// github.com/gorilla/websocket.
type wsConn struct {
	mu  sync.Mutex
	raw io.ReadWriteCloser
}

func upgradeWS(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, fmt.Errorf("ws: hijack not supported")
	}

	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "missing Sec-WebSocket-Key", http.StatusBadRequest)
		return nil, fmt.Errorf("ws: missing key")
	}

	accept := wsAcceptKey(key)

	// Write the handshake response.
	conn, brw, err := hj.Hijack()
	if err != nil {
		return nil, fmt.Errorf("ws: hijack: %w", err)
	}

	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	brw.WriteString(resp)
	brw.Flush()

	return &wsConn{raw: conn}, nil
}

// WriteMessage sends a text WebSocket frame.
func (c *wsConn) WriteMessage(msg []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return wsSendTextFrame(c.raw, msg)
}

// Close closes the underlying connection.
func (c *wsConn) Close() { c.raw.Close() }

// wsSendTextFrame writes a single unmasked text frame (server→client frames
// must not be masked per RFC 6455 §5.1).
func wsSendTextFrame(w io.Writer, payload []byte) error {
	length := len(payload)
	header := make([]byte, 2, 10)
	header[0] = 0x81 // FIN=1, opcode=1 (text)

	switch {
	case length < 126:
		header[1] = byte(length)
	case length < 65536:
		header[1] = 126
		header = append(header, byte(length>>8), byte(length))
	default:
		header[1] = 127
		for i := 7; i >= 0; i-- {
			header = append(header, byte(length>>(uint(i)*8)))
		}
	}

	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// wsAcceptKey computes the Sec-WebSocket-Accept value per RFC 6455 §4.2.2.
// The real implementation lives in ws_upgrade.go (wsAcceptKeyImpl).
func wsAcceptKey(key string) string {
	return wsAcceptKeyImpl(key)
}

// ════════════════════════════════════════════════════════════════════════════
// Helpers
// ════════════════════════════════════════════════════════════════════════════

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func queryInt(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
