//go:build windows

package platform

import (
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	inetKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	// WinINet broadcast message — tells IE/Edge/Chrome to pick up new proxy settings.
	winINetChanged = "wininet.dll,SetInternetExplorerProxy"
)

// ── System proxy ─────────────────────────────────────────────────────────────

func setSystemProxy(cfg ProxyConfig) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, inetKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("platform: open registry key: %w", err)
	}
	defer k.Close()

	// Save the current state so ClearSystemProxy can restore it.
	if err := saveSnapshot(k); err != nil {
		return err
	}

	if err := k.SetStringValue("ProxyServer", cfg.Server()); err != nil {
		return fmt.Errorf("platform: set ProxyServer: %w", err)
	}
	// Override: apply proxy to all protocols except localhost.
	if err := k.SetStringValue("ProxyOverride", "<local>"); err != nil {
		return fmt.Errorf("platform: set ProxyOverride: %w", err)
	}
	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		return fmt.Errorf("platform: enable proxy: %w", err)
	}

	notifyWinINet()
	fmt.Printf("[proxy] Windows system proxy set to %s\n", cfg.Server())
	return nil
}

func clearSystemProxy() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, inetKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("platform: open registry key: %w", err)
	}
	defer k.Close()

	if err := restoreSnapshot(k); err != nil {
		// No snapshot — just disable.
		_ = k.SetDWordValue("ProxyEnable", 0)
	}

	notifyWinINet()
	fmt.Println("[proxy] Windows system proxy restored")
	return nil
}

// notifyWinINet broadcasts the settings change so all running browsers
// (Chrome, Edge, IE) pick it up without needing a restart.
func notifyWinINet() {
	// rundll32 url.dll,FileProtocolHandler is the canonical way to trigger the
	// WinINet change notification from a non-GUI process.
	_ = exec.Command("rundll32.exe", winINetChanged).Run()
}

// ── Snapshot helpers — save/restore previous proxy state ────────────────────

// We persist the previous values as registry strings under a "MitmSnapshot"
// key so that ClearSystemProxy can restore exactly what was there before.

const snapshotKey = inetKey + `\MitmSnapshot`

func saveSnapshot(k registry.Key) error {
	snap, _, err := registry.CreateKey(registry.CURRENT_USER, snapshotKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("platform: create snapshot key: %w", err)
	}
	defer snap.Close()

	wasEnabled, _, _ := k.GetIntegerValue("ProxyEnable")
	prevServer, _, _ := k.GetStringValue("ProxyServer")
	prevOverride, _, _ := k.GetStringValue("ProxyOverride")

	_ = snap.SetDWordValue("ProxyEnable", uint32(wasEnabled))
	_ = snap.SetStringValue("ProxyServer", prevServer)
	_ = snap.SetStringValue("ProxyOverride", prevOverride)
	return nil
}

func restoreSnapshot(k registry.Key) error {
	snap, err := registry.OpenKey(registry.CURRENT_USER, snapshotKey, registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("platform: no snapshot found")
	}
	defer snap.Close()

	wasEnabled, _, _ := snap.GetIntegerValue("ProxyEnable")
	prevServer, _, _ := snap.GetStringValue("ProxyServer")
	prevOverride, _, _ := snap.GetStringValue("ProxyOverride")

	_ = k.SetDWordValue("ProxyEnable", uint32(wasEnabled))
	_ = k.SetStringValue("ProxyServer", prevServer)
	_ = k.SetStringValue("ProxyOverride", prevOverride)

	// Clean up the snapshot key.
	_ = registry.DeleteKey(registry.CURRENT_USER, snapshotKey)
	return nil
}

// ── CA trust ─────────────────────────────────────────────────────────────────

func trustCA(caCertPath string) error {
	// certutil is built into every Windows version since Vista.
	// -addstore Root  → adds to the machine-wide "Trusted Root CAs" store.
	// Running without elevation adds only to the current-user store, which is
	// sufficient for Chrome/Edge/IE. Firefox uses its own store (see below).
	cmd := exec.Command("certutil", "-addstore", "-user", "Root", caCertPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("platform: certutil: %w\n%s", err, out)
	}
	fmt.Println("[ca] Root certificate trusted in Windows certificate store (current user)")
	fmt.Println("[ca] Note: Firefox uses its own trust store — see --install-ca for instructions")
	return nil
}

func isTrusted(commonName string) (bool, error) {
	// certutil -verifystore Root <CN> exits 0 if found.
	out, err := exec.Command("certutil", "-verifystore", "-user", "Root", commonName).CombinedOutput()
	if err != nil {
		return false, nil
	}
	return strings.Contains(string(out), commonName), nil
}
