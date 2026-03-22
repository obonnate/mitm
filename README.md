# Mitm

A Fiddler/Wireshark-grade MITM proxy written in Go, with an embedded Angular dashboard.

## Features

- **HTTP/1.1 & HTTP/2** interception and decoding
- **TLS MITM** — dynamic per-host certificate forgery, custom root CA
- **WebSocket** frame capture
- **Exchange store** — bounded ring buffer (10 000 entries by default)
- **Real-time GUI** — Angular dashboard over WebSocket
- **REST API** — query, filter, replay and diff exchanges
- **Plugin interface** — add decoders for gRPC, raw TCP, etc.
- **Replay & modify** — resend any request with header/body overrides

---

## Quick start

```bash
# 1. Clone and build
git clone https://github.com/obonnate/mitm
cd mitm
go mod tidy          # downloads golang.org/x/net
make build           # → bin/mitm

# 2. Trust the CA (run once per machine)
./bin/mitm --install-ca
# Follow the printed instructions for your OS/browser

# 3. Start the proxy
./bin/mitm

# 4. Point your OS/browser proxy settings to:
#    HTTP  → 127.0.0.1:8080
#    HTTPS → 127.0.0.1:8080

# 5. Open the dashboard
open http://localhost:9000
```

---

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--addr` | `:8080` | Proxy listen address |
| `--api-addr` | `:9000` | Dashboard + API listen address |
| `--ca-dir` | `~/.config/mitm` | CA cert/key directory |
| `--store-cap` | `10000` | Max in-memory exchanges |
| `--passthrough` | `` | Comma-separated host globs to tunnel unmodified |
| `--install-ca` | — | Print CA trust instructions and exit |
| `-v` | — | Verbose exchange logging |

---

## Architecture

```
CLI (cmd/mitm)
    │
    ├── proxy.Server          — HTTP listener, CONNECT hijack
    │       │
    │       ├── tls.CA        — root CA + per-host cert cache
    │       ├── httpDecoder   — HTTP/1.1 request/response decoder
    │       └── OnExchange()  — fan-out via MultiHandler
    │                │
    │                ├── store.Store   — ring buffer
    │                └── bus.Bus       — pub/sub fan-out
    │                          │
    └── api.Server ────────────┘
            ├── GET  /api/exchanges          (list + filter)
            ├── GET  /api/exchanges/:uuid    (detail)
            ├── POST /api/exchanges/:uuid/replay
            ├── GET  /api/ca.crt
            ├── GET  /ws                     (live WebSocket stream)
            └── GET  /                       (Angular SPA)
```

---

## TLS interception flow

```
Client ──CONNECT api.example.com:443──► Proxy
                                          │
                               forge cert for api.example.com
                               (signed by mitm CA)
                                          │
Client ◄──101 Tunnel established──────── Proxy
  │                                       │
  └──TLS handshake (forged cert)──────► Proxy ──TLS handshake (real cert)──► api.example.com
           plain HTTP/1.1 ↕                        plain HTTP/1.1 ↕
         (Proxy reads all frames)            (Proxy forwards + records)
```

---

## Adding a protocol decoder

Implement the `decoder.Decoder` interface and register it before the proxy starts:

```go
type MyDecoder struct{}

func (MyDecoder) Name() string { return "MyProto" }
func (MyDecoder) Priority() int { return 20 }
func (MyDecoder) CanHandle(peek []byte, alpn string) bool {
    return bytes.HasPrefix(peek, []byte("MYPROTO/1.0"))
}
func (MyDecoder) Decode(ctx context.Context, client, server net.Conn,
    tlsInfo *proxy.TLSInfo, out func(*proxy.Exchange)) error {
    // decode frames, call out() for each exchange
    return nil
}

// In main():
registry.Register(MyDecoder{})
```

---

## Development (hot-reload)

```bash
# Terminal 1 — Go backend
go run ./cmd/mitm --v

# Terminal 2 — Angular dev server (proxies /api and /ws to :9000)
cd ui && ng serve --proxy-config proxy.conf.json
# Dashboard at http://localhost:4200
```

---

## Roadmap

- [ ] HTTP/2 full frame decoder (h2 plugin)
- [ ] gRPC decoder (protobuf reflection)
- [ ] SQLite persistence for exchanges
- [ ] Breakpoint / rewrite rules engine
- [ ] Script hooks (Tengo or starlark)
- [ ] Export to HAR format
- [ ] Certificate pinning bypass helpers
