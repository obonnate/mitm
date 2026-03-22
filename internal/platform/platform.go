// Package platform handles OS-specific integration:
//   - Setting / clearing the system HTTP/HTTPS proxy
//   - Trusting the GoProxy root CA in the OS certificate store
//   - Opening the default browser
package platform

import (
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// ProxyConfig describes the proxy to activate.
type ProxyConfig struct {
	// Host is the proxy host, e.g. "127.0.0.1".
	Host string
	// Port is the proxy port, e.g. "8080".
	Port string
}

// Server returns "host:port".
func (c ProxyConfig) Server() string {
	return fmt.Sprintf("%s:%s", c.Host, c.Port)
}

// SetSystemProxy enables the OS-level HTTP/HTTPS proxy.
// On Windows it writes to the registry and notifies WinINet.
// On macOS it calls networksetup.
// On Linux it sets environment variables (best-effort for the current session).
func SetSystemProxy(cfg ProxyConfig) error {
	return setSystemProxy(cfg)
}

// ClearSystemProxy disables the OS-level proxy and restores the previous state.
func ClearSystemProxy() error {
	return clearSystemProxy()
}

// TrustCA installs the certificate at caCertPath into the OS trust store.
// On Windows: certutil -addstore Root <path>
// On macOS:   security add-trusted-cert -d -r trustRoot -k System.keychain <path>
// On Linux:   copies to /usr/local/share/ca-certificates/ and runs update-ca-certificates
func TrustCA(caCertPath string) error {
	return trustCA(caCertPath)
}

// IsTrusted returns true if a certificate with the given CN is already present
// in the OS trust store. Used to skip re-installation on repeated starts.
func IsTrusted(commonName string) (bool, error) {
	return isTrusted(commonName)
}

// OpenBrowser opens url in the default system browser after a short delay
// so the HTTP server has time to start.
func OpenBrowser(url string) {
	time.Sleep(400 * time.Millisecond)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
