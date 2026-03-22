// Package wsdecoder provides a Decoder for WebSocket connections.
//
// WebSocket is negotiated via an HTTP/1.1 Upgrade handshake. Once upgraded,
// the connection carries WebSocket frames rather than HTTP messages.
//
// This decoder:
//  1. Detects the Upgrade: websocket header in the HTTP handshake.
//  2. Forwards the handshake transparently (it is already captured by the
//     HTTP/1.1 decoder as a regular exchange).
//  3. Reads WS frames from both directions and emits one Exchange per frame
//     with Protocol=ProtoWS. This lets the GUI show a WebSocket message log
//     beneath the upgrade exchange.
package wsdecoder

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"mitm/internal/proxy"
)

// Decoder implements decoder.Decoder for WebSocket traffic.
type Decoder struct{}

func (Decoder) Name() string  { return "WebSocket" }
func (Decoder) Priority() int { return 5 }

// CanHandle returns true if the first line looks like an HTTP Upgrade request
// with "websocket" in the peek bytes.
func (Decoder) CanHandle(peek []byte, _ string) bool {
	return bytes.Contains(bytes.ToLower(peek), []byte("upgrade"))
}

// Decode processes one WebSocket conversation. It first reads and forwards the
// HTTP Upgrade handshake, then enters frame-relay mode.
func (d Decoder) Decode(
	ctx context.Context,
	clientConn net.Conn,
	serverConn net.Conn,
	tlsInfo *proxy.TLSInfo,
	out func(*proxy.Exchange),
) error {
	// ── Phase 1: HTTP Upgrade handshake ───────────────────────────────────────
	clientBuf := bufio.NewReader(clientConn)

	req, err := http.ReadRequest(clientBuf)
	if err != nil {
		return fmt.Errorf("ws: read upgrade request: %w", err)
	}

	// Forward request to server.
	if err := req.Write(serverConn); err != nil {
		return fmt.Errorf("ws: forward upgrade request: %w", err)
	}

	// Read server response.
	serverBuf := bufio.NewReader(serverConn)
	resp, err := http.ReadResponse(serverBuf, req)
	if err != nil {
		return fmt.Errorf("ws: read upgrade response: %w", err)
	}

	// Forward response to client.
	if err := resp.Write(clientConn); err != nil {
		return fmt.Errorf("ws: forward upgrade response: %w", err)
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		// Server declined — not a WebSocket connection.
		return nil
	}

	// Emit the upgrade exchange.
	upgradeEx := &proxy.Exchange{
		Protocol: proxy.ProtoWS,
		TLS:      tlsInfo,
		Request:  req,
		Response: resp,
		Timing:   proxy.Timing{Start: time.Now(), Done: time.Now()},
	}
	upgradeEx.Tag("ws-upgrade")
	out(upgradeEx)

	// ── Phase 2: WebSocket frame relay ────────────────────────────────────────
	return relayFrames(ctx, clientConn, serverConn, clientBuf, serverBuf, tlsInfo, out)
}

// relayFrames copies WebSocket frames bidirectionally, emitting an Exchange
// for each complete message (a message may span multiple continuation frames).
func relayFrames(
	ctx context.Context,
	clientConn, serverConn net.Conn,
	clientBuf, serverBuf *bufio.Reader,
	tlsInfo *proxy.TLSInfo,
	out func(*proxy.Exchange),
) error {
	var wg sync.WaitGroup
	errc := make(chan error, 2)

	relay := func(src net.Conn, srcBuf *bufio.Reader, dst net.Conn, fromClient bool) {
		defer wg.Done()
		var msgBuf bytes.Buffer
		var msgOpcode byte

		for {
			select {
			case <-ctx.Done():
				errc <- ctx.Err()
				return
			default:
			}

			src.SetReadDeadline(time.Now().Add(60 * time.Second))
			fin, opcode, payload, err := readFrame(srcBuf)
			if err != nil {
				errc <- err
				return
			}

			// Forward the raw frame to the other side.
			masked := fromClient // client→server frames are masked per RFC 6455
			if err := writeFrame(dst, fin, opcode, payload, masked); err != nil {
				errc <- err
				return
			}

			// Accumulate fragmented messages.
			if opcode != 0x00 { // non-continuation frame starts a new message
				msgBuf.Reset()
				msgOpcode = opcode
			}
			msgBuf.Write(payload)

			if fin {
				// Complete message ready — emit an exchange.
				body := make([]byte, msgBuf.Len())
				copy(body, msgBuf.Bytes())

				direction := "client→server"
				if !fromClient {
					direction = "server→client"
				}

				ex := &proxy.Exchange{
					Protocol: proxy.ProtoWS,
					TLS:      tlsInfo,
					RespBody: body,
					Timing:   proxy.Timing{Start: time.Now(), Done: time.Now()},
				}
				ex.Tag(fmt.Sprintf("ws-msg opcode=%02x dir=%s", msgOpcode, direction))
				out(ex)
			}
		}
	}

	wg.Add(2)
	go relay(clientConn, clientBuf, serverConn, true)
	go relay(serverConn, serverBuf, clientConn, false)

	wg.Wait()
	return nil
}

// ════════════════════════════════════════════════════════════════════════════
// Minimal WebSocket frame reader / writer (RFC 6455)
// ════════════════════════════════════════════════════════════════════════════

// readFrame reads one WebSocket frame from r.
// Returns (fin, opcode, payload, error).
func readFrame(r io.Reader) (fin bool, opcode byte, payload []byte, err error) {
	header := make([]byte, 2)
	if _, err = io.ReadFull(r, header); err != nil {
		return
	}

	fin = header[0]&0x80 != 0
	opcode = header[0] & 0x0f
	masked := header[1]&0x80 != 0
	payloadLen := int64(header[1] & 0x7f)

	switch payloadLen {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return
		}
		payloadLen = int64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return
		}
		payloadLen = int64(binary.BigEndian.Uint64(ext[:]))
	}

	var maskKey [4]byte
	if masked {
		if _, err = io.ReadFull(r, maskKey[:]); err != nil {
			return
		}
	}

	payload = make([]byte, payloadLen)
	if _, err = io.ReadFull(r, payload); err != nil {
		return
	}

	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}
	return
}

// writeFrame writes one WebSocket frame to w. The payload is masked if masked
// is true (required for client→server frames per RFC 6455).
func writeFrame(w io.Writer, fin bool, opcode byte, payload []byte, masked bool) error {
	var buf bytes.Buffer

	b0 := opcode
	if fin {
		b0 |= 0x80
	}
	buf.WriteByte(b0)

	length := len(payload)
	var maskBit byte
	if masked {
		maskBit = 0x80
	}

	switch {
	case length < 126:
		buf.WriteByte(maskBit | byte(length))
	case length < 65536:
		buf.WriteByte(maskBit | 126)
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(length))
		buf.Write(ext[:])
	default:
		buf.WriteByte(maskBit | 127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(length))
		buf.Write(ext[:])
	}

	if masked {
		// Use a fixed mask for forwarded frames — the original mask was
		// already stripped during readFrame.
		maskKey := [4]byte{0xAB, 0xCD, 0xEF, 0x12}
		buf.Write(maskKey[:])
		maskedPayload := make([]byte, length)
		for i, b := range payload {
			maskedPayload[i] = b ^ maskKey[i%4]
		}
		buf.Write(maskedPayload)
	} else {
		buf.Write(payload)
	}

	_, err := w.Write(buf.Bytes())
	return err
}
