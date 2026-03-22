//go:build windows

package platform

import (
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const inetKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
const snapshotKey = inetKey + `\MitmSnapshot`

// WinINet option constants for InternetSetOption.
// https://learn.microsoft.com/en-us/windows/win32/wininet/option-flags
const (
	internetOptionSettingsChanged = 39
	internetOptionRefresh         = 37
)

var (
	wininet               = windows.NewLazySystemDLL("wininet.dll")
	procInternetSetOption = wininet.NewProc("InternetSetOptionW")
)

// ── System proxy ─────────────────────────────────────────────────────────────

func setSystemProxy(cfg ProxyConfig) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, inetKey,
		registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("platform: open registry key: %w", err)
	}
	defer k.Close()

	if err := saveSnapshot(k); err != nil {
		return err
	}

	if err := k.SetStringValue("ProxyServer", cfg.Server()); err != nil {
		return fmt.Errorf("platform: set ProxyServer: %w", err)
	}
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
	k, err := registry.OpenKey(registry.CURRENT_USER, inetKey,
		registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("platform: open registry key: %w", err)
	}
	defer k.Close()

	if err := restoreSnapshot(k); err != nil {
		// No snapshot saved — just disable.
		_ = k.SetDWordValue("ProxyEnable", 0)
	}

	notifyWinINet()
	fmt.Println("[proxy] Windows system proxy restored")
	return nil
}

// notifyWinINet tells WinINet (and therefore Chrome, Edge, etc.) to re-read
// the proxy settings from the registry immediately, without a browser restart.
//
// The correct API is InternetSetOptionW with:
//   - INTERNET_OPTION_SETTINGS_CHANGED (39) — signals that settings changed
//   - INTERNET_OPTION_REFRESH           (37) — forces a refresh from registry
//
// Both calls use hInternet=0 (NULL), which applies the notification globally.
// This replaces the broken rundll32 approach from the previous version.
func notifyWinINet() {
	// NULL handle means "apply to all sessions".
	procInternetSetOption.Call(0, internetOptionSettingsChanged, 0, 0)
	procInternetSetOption.Call(0, internetOptionRefresh, 0, 0)
}

// ── Snapshot helpers ──────────────────────────────────────────────────────────

func saveSnapshot(k registry.Key) error {
	snap, _, err := registry.CreateKey(registry.CURRENT_USER, snapshotKey,
		registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("platform: create snapshot key: %w", err)
	}
	defer snap.Close()

	enabled, _, _ := k.GetIntegerValue("ProxyEnable")
	server, _, _ := k.GetStringValue("ProxyServer")
	override, _, _ := k.GetStringValue("ProxyOverride")

	_ = snap.SetDWordValue("ProxyEnable", uint32(enabled))
	_ = snap.SetStringValue("ProxyServer", server)
	_ = snap.SetStringValue("ProxyOverride", override)
	return nil
}

func restoreSnapshot(k registry.Key) error {
	snap, err := registry.OpenKey(registry.CURRENT_USER, snapshotKey,
		registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("platform: no snapshot found")
	}
	defer snap.Close()

	enabled, _, _ := snap.GetIntegerValue("ProxyEnable")
	server, _, _ := snap.GetStringValue("ProxyServer")
	override, _, _ := snap.GetStringValue("ProxyOverride")

	_ = k.SetDWordValue("ProxyEnable", uint32(enabled))
	_ = k.SetStringValue("ProxyServer", server)
	_ = k.SetStringValue("ProxyOverride", override)

	_ = registry.DeleteKey(registry.CURRENT_USER, snapshotKey)
	return nil
}

// ── CA trust ──────────────────────────────────────────────────────────────────

func trustCA(caCertPath string) error {
	// certutil -addstore -user Root <path>
	// -user  → writes to HKCU (current user store), no elevation needed.
	// Chrome, Edge and all WinINet-based apps trust the current-user Root store.
	cmd := exec.Command("certutil", "-addstore", "-user", "Root", caCertPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("platform: certutil: %w\n%s", err, out)
	}
	fmt.Println("[ca] Root certificate trusted (Windows current-user Root store)")
	fmt.Println("[ca] Note: Firefox manages its own trust store independently.")
	fmt.Println("[ca] To trust in Firefox: about:preferences → Certificates → Import")
	return nil
}

func isTrusted(commonName string) (bool, error) {
	out, err := exec.Command("certutil", "-verifystore", "-user", "Root",
		commonName).CombinedOutput()
	if err != nil {
		return false, nil
	}
	return strings.Contains(string(out), commonName), nil
}
