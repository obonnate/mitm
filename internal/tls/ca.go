package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CA manages the root certificate authority and the per-host leaf certificate
// cache. It is the heart of the MITM interception layer.
type CA struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	tlsCert tls.Certificate
	certDir string

	mu    sync.RWMutex
	cache map[string]*tls.Certificate // host → forged leaf cert
}

// LoadOrCreate loads a CA from certDir/ca.crt + certDir/ca.key, or generates a
// fresh one if those files don't exist yet.
func LoadOrCreate(certDir string) (*CA, error) {
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return nil, fmt.Errorf("tls: mkdir %s: %w", certDir, err)
	}

	crtPath := filepath.Join(certDir, "ca.crt")
	keyPath := filepath.Join(certDir, "ca.key")

	_, crtErr := os.Stat(crtPath)
	_, keyErr := os.Stat(keyPath)

	if os.IsNotExist(crtErr) || os.IsNotExist(keyErr) {
		return generateCA(certDir, crtPath, keyPath)
	}

	return loadCA(certDir, crtPath, keyPath)
}

func generateCA(certDir, crtPath, keyPath string) (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("tls: generate CA key: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "Mitm",
			Organization: []string{"Mitm"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("tls: self-sign CA: %w", err)
	}

	// Persist
	if err := writePEM(crtPath, "CERTIFICATE", derBytes, 0644); err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("tls: marshal CA key: %w", err)
	}
	if err := writePEM(keyPath, "EC PRIVATE KEY", keyDER, 0600); err != nil {
		return nil, err
	}

	cert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return nil, err
	}

	tlsCert := tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  key,
		Leaf:        cert,
	}

	fmt.Printf("[CA] Generated new root CA → %s\n", crtPath)
	fmt.Printf("[CA] Install %s in your OS/browser trust store to avoid certificate warnings.\n", crtPath)

	return &CA{
		cert:    cert,
		key:     key,
		tlsCert: tlsCert,
		certDir: certDir,
		cache:   make(map[string]*tls.Certificate),
	}, nil
}

func loadCA(certDir, crtPath, keyPath string) (*CA, error) {
	tlsCert, err := tls.LoadX509KeyPair(crtPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("tls: load CA keypair: %w", err)
	}
	tlsCert.Leaf, err = x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("tls: parse CA cert: %w", err)
	}

	key, ok := tlsCert.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("tls: CA key is not ECDSA")
	}

	return &CA{
		cert:    tlsCert.Leaf,
		key:     key,
		tlsCert: tlsCert,
		certDir: certDir,
		cache:   make(map[string]*tls.Certificate),
	}, nil
}

// CertFor returns a TLS certificate for host, forging one on-the-fly if it is
// not already cached. The forged cert is signed by the CA and valid for 24 h.
//
// host may be a bare hostname or host:port; port is stripped automatically.
func (ca *CA) CertFor(host string) (*tls.Certificate, error) {
	// Strip port if present
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	ca.mu.RLock()
	if c, ok := ca.cache[host]; ok {
		ca.mu.RUnlock()
		return c, nil
	}
	ca.mu.RUnlock()

	// Forge under write lock to avoid duplicate work
	ca.mu.Lock()
	defer ca.mu.Unlock()
	if c, ok := ca.cache[host]; ok {
		return c, nil
	}

	c, err := ca.forge(host)
	if err != nil {
		return nil, err
	}
	ca.cache[host] = c
	return c, nil
}

func (ca *CA) forge(host string) (*tls.Certificate, error) {
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("tls: forge key for %s: %w", host, err)
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   host,
			Organization: []string{"Mitm (intercepted)"},
		},
		NotBefore:   time.Now().Add(-time.Minute),
		NotAfter:    time.Now().Add(24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	// Populate SANs
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{host}
		// Also add wildcard for sub-domains so *.acme.io also works
		tmpl.DNSNames = append(tmpl.DNSNames, "*."+host)
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &leafKey.PublicKey, ca.key)
	if err != nil {
		return nil, fmt.Errorf("tls: sign leaf for %s: %w", host, err)
	}

	leaf, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return nil, err
	}

	c := &tls.Certificate{
		Certificate: [][]byte{derBytes, ca.cert.Raw},
		PrivateKey:  leafKey,
		Leaf:        leaf,
	}
	return c, nil
}

// CACertPEM returns the CA certificate in PEM format (for installation helpers).
func (ca *CA) CACertPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: ca.cert.Raw,
	})
}

// CACertDER returns the raw DER bytes of the CA certificate.
func (ca *CA) CACertDER() []byte {
	return ca.cert.Raw
}

// ServerTLSConfig returns a *tls.Config suitable for the proxy→client connection.
// GetCertificate is called per-handshake so each host gets its forged cert.
func (ca *CA) ServerTLSConfig() *tls.Config {
	return &tls.Config{
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			return ca.CertFor(hello.ServerName)
		},
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2", "http/1.1"},
	}
}

// UpstreamTLSConfig returns a *tls.Config for the proxy→server connection.
// It captures the upstream server certificate for inclusion in the Exchange.
func UpstreamTLSConfig(serverName string, capture *[]byte) *tls.Config {
	return &tls.Config{
		ServerName: serverName,
		MinVersion: tls.VersionTLS12,
		// We verify normally — we just also want to see the cert.
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) > 0 && capture != nil {
				*capture = rawCerts[0]
			}
			return nil
		},
	}
}

// — helpers —

func randomSerial() (*big.Int, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("tls: random serial: %w", err)
	}
	return new(big.Int).SetBytes(b), nil
}

func writePEM(path, pemType string, derBytes []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("tls: write %s: %w", path, err)
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: pemType, Bytes: derBytes})
}
