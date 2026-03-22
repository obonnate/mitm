//go:build !windows && !darwin

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ── System proxy ─────────────────────────────────────────────────────────────
// Linux has no single system-wide proxy API. We set the standard environment
// variables for the current process — child processes (curl, git, etc.) inherit
// them. For a persistent per-session proxy the user needs to add these to their
// shell profile; we print instructions.

func setSystemProxy(cfg ProxyConfig) error {
	proxyURL := "http://" + cfg.Server()
	os.Setenv("http_proxy", proxyURL)
	os.Setenv("HTTP_PROXY", proxyURL)
	os.Setenv("https_proxy", proxyURL)
	os.Setenv("HTTPS_PROXY", proxyURL)
	os.Setenv("no_proxy", "localhost,127.0.0.1")
	os.Setenv("NO_PROXY", "localhost,127.0.0.1")

	fmt.Printf("[proxy] Environment proxy set to %s (current process only)\n", cfg.Server())
	fmt.Println("[proxy] To set system-wide, add to your shell profile:")
	fmt.Printf("  export http_proxy=%s https_proxy=%s\n", proxyURL, proxyURL)

	// Best-effort: try gsettings (GNOME) and KDE kwriteconfig.
	_ = exec.Command("gsettings", "set", "org.gnome.system.proxy", "mode", "manual").Run()
	_ = exec.Command("gsettings", "set", "org.gnome.system.proxy.http", "host", cfg.Host).Run()
	_ = exec.Command("gsettings", "set", "org.gnome.system.proxy.http", "port", cfg.Port).Run()
	_ = exec.Command("gsettings", "set", "org.gnome.system.proxy.https", "host", cfg.Host).Run()
	_ = exec.Command("gsettings", "set", "org.gnome.system.proxy.https", "port", cfg.Port).Run()

	return nil
}

func clearSystemProxy() error {
	for _, v := range []string{"http_proxy", "HTTP_PROXY", "https_proxy", "HTTPS_PROXY", "no_proxy", "NO_PROXY"} {
		os.Unsetenv(v)
	}
	_ = exec.Command("gsettings", "set", "org.gnome.system.proxy", "mode", "none").Run()
	fmt.Println("[proxy] Environment proxy cleared")
	return nil
}

// ── CA trust ─────────────────────────────────────────────────────────────────

func trustCA(caCertPath string) error {
	// Try Debian/Ubuntu first, then Fedora/RHEL.
	if _, err := exec.LookPath("update-ca-certificates"); err == nil {
		return trustDebian(caCertPath)
	}
	if _, err := exec.LookPath("update-ca-trust"); err == nil {
		return trustRHEL(caCertPath)
	}
	fmt.Println("[ca] Could not find update-ca-certificates or update-ca-trust.")
	fmt.Printf("[ca] Install manually: copy %s to /usr/local/share/ca-certificates/mitm.crt\n", caCertPath)
	return nil
}

func trustDebian(caCertPath string) error {
	dest := "/usr/local/share/ca-certificates/mitm-proxy.crt"
	if out, err := exec.Command("sudo", "cp", caCertPath, dest).CombinedOutput(); err != nil {
		return fmt.Errorf("platform: copy CA cert: %w\n%s", err, out)
	}
	if out, err := exec.Command("sudo", "update-ca-certificates").CombinedOutput(); err != nil {
		return fmt.Errorf("platform: update-ca-certificates: %w\n%s", err, out)
	}
	fmt.Println("[ca] Root certificate trusted (Debian/Ubuntu)")
	return nil
}

func trustRHEL(caCertPath string) error {
	dest := "/etc/pki/ca-trust/source/anchors/mitm-proxy.crt"
	if out, err := exec.Command("sudo", "cp", caCertPath, dest).CombinedOutput(); err != nil {
		return fmt.Errorf("platform: copy CA cert: %w\n%s", err, out)
	}
	if out, err := exec.Command("sudo", "update-ca-trust").CombinedOutput(); err != nil {
		return fmt.Errorf("platform: update-ca-trust: %w\n%s", err, out)
	}
	fmt.Println("[ca] Root certificate trusted (RHEL/Fedora)")
	return nil
}

func isTrusted(commonName string) (bool, error) {
	// Check NSS db (used by Chrome on Linux).
	out, err := exec.Command("certutil", "-d", "sql:"+os.Getenv("HOME")+"/.pki/nssdb",
		"-L", "-n", commonName).Output()
	if err != nil {
		return false, nil
	}
	return strings.Contains(string(out), commonName), nil
}
