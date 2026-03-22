package tls_test

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"testing"

	gotls "mitm/internal/tls"
)

func TestLoadOrCreate_GeneratesCA(t *testing.T) {
	dir := t.TempDir()
	ca, err := gotls.LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}

	// Files should exist.
	for _, name := range []string{"ca.crt", "ca.key"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected %s to exist: %v", name, err)
		}
	}

	// PEM should be non-empty and parseable.
	pemBytes := ca.CACertPEM()
	if len(pemBytes) == 0 {
		t.Fatal("CACertPEM returned empty bytes")
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		t.Fatal("CACertPEM is not a valid PEM certificate")
	}
}

func TestLoadOrCreate_LoadsExisting(t *testing.T) {
	dir := t.TempDir()

	// Generate once.
	ca1, err := gotls.LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("first LoadOrCreate: %v", err)
	}

	// Load again — should return the same CA.
	ca2, err := gotls.LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("second LoadOrCreate: %v", err)
	}

	if string(ca1.CACertDER()) != string(ca2.CACertDER()) {
		t.Error("CA DER changed between loads — should be identical")
	}
}

func TestCertFor_ForgesLeaf(t *testing.T) {
	dir := t.TempDir()
	ca, err := gotls.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}

	leaf, err := ca.CertFor("example.com")
	if err != nil {
		t.Fatalf("CertFor: %v", err)
	}

	if leaf.Leaf == nil {
		t.Fatal("Leaf field should be populated")
	}

	if leaf.Leaf.Subject.CommonName != "example.com" {
		t.Errorf("expected CN=example.com, got %s", leaf.Leaf.Subject.CommonName)
	}

	// Verify the leaf is signed by the CA.
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca.CACertPEM())

	opts := x509.VerifyOptions{
		DNSName: "example.com",
		Roots:   pool,
	}
	if _, err := leaf.Leaf.Verify(opts); err != nil {
		t.Errorf("leaf cert does not verify against CA: %v", err)
	}
}

func TestCertFor_Cached(t *testing.T) {
	dir := t.TempDir()
	ca, err := gotls.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}

	c1, _ := ca.CertFor("cache-test.example.com")
	c2, _ := ca.CertFor("cache-test.example.com")

	if c1 != c2 {
		t.Error("expected same pointer from cache; got two distinct certs")
	}
}

func TestCertFor_StripPort(t *testing.T) {
	dir := t.TempDir()
	ca, _ := gotls.LoadOrCreate(dir)

	withPort, _ := ca.CertFor("example.com:443")
	withoutPort, _ := ca.CertFor("example.com")

	// Both should resolve to the same cached entry.
	if withPort != withoutPort {
		t.Error("host:port and bare host should share the same cached cert")
	}
}

func TestServerTLSConfig_HandshakesWithForgedCert(t *testing.T) {
	dir := t.TempDir()
	ca, err := gotls.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Build a client-trusted root pool from the CA.
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca.CACertPEM())

	// Stand up a TLS server using the CA's ServerTLSConfig.
	ln, err := tls.Listen("tcp", "127.0.0.1:0", ca.ServerTLSConfig())
	if err != nil {
		t.Fatalf("tls.Listen: %v", err)
	}
	defer ln.Close()

	serverErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		// Complete the handshake.
		if err := conn.(*tls.Conn).Handshake(); err != nil {
			serverErr <- err
			return
		}
		conn.Close()
		serverErr <- nil
	}()

	// Connect as a client that trusts our CA.
	clientCfg := &tls.Config{
		ServerName: "example.com",
		RootCAs:    pool,
	}
	conn, err := tls.Dial("tcp", ln.Addr().String(), clientCfg)
	if err != nil {
		t.Fatalf("client TLS dial: %v", err)
	}
	conn.Close()

	if err := <-serverErr; err != nil {
		t.Fatalf("server TLS handshake: %v", err)
	}
}

func TestUpstreamTLSConfig_CapturesCert(t *testing.T) {
	dir := t.TempDir()
	serverCA, _ := gotls.LoadOrCreate(dir)

	// Stand up a real TLS server (using a different CA so we can distinguish
	// it from the proxy CA).
	serverCert, err := serverCA.CertFor("localhost")
	if err != nil {
		t.Fatal(err)
	}
	serverCfg := &tls.Config{Certificates: []tls.Certificate{*serverCert}}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", serverCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, _ := ln.Accept()
		if conn != nil {
			conn.(*tls.Conn).Handshake()
			conn.Close()
		}
	}()

	var captured []byte
	clientCfg := gotls.UpstreamTLSConfig("localhost", &captured)
	clientCfg.InsecureSkipVerify = true // skip chain validation in this unit test

	rawConn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	tlsConn := tls.Client(rawConn, clientCfg)
	tlsConn.Handshake()
	tlsConn.Close()

	if len(captured) == 0 {
		t.Error("UpstreamTLSConfig: upstream cert was not captured")
	}
}
