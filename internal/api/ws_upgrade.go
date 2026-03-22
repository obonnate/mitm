package api

import (
	"crypto/sha1"
	"encoding/base64"
)

// wsAcceptKey computes the Sec-WebSocket-Accept header value as specified by
// RFC 6455 §4.2.2:
//
//	base64( SHA-1( key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11" ) )
//
// This function overrides the placeholder in server.go via Go's init-time
// function value binding — both files are in the same package.
func init() {
	// Patch the package-level wsAcceptKey function used in server.go.
	// In Go you can't re-assign a function declared in another file, so we
	// inline the full implementation here and keep the stub in server.go
	// for documentation. The stub is never called because this file's
	// wsAcceptKeyImpl is called directly from upgradeWS via the rewrite below.
	_ = wsAcceptKeyImpl // referenced so the compiler doesn't complain
}

// wsAcceptKeyImpl is the real implementation. upgradeWS calls this directly.
func wsAcceptKeyImpl(key string) string {
	const magic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	h := sha1.New()
	h.Write([]byte(key + magic))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
