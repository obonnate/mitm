//go:build darwin

package platform

import (
	"fmt"
	"os/exec"
	"strings"
)

// ── System proxy ─────────────────────────────────────────────────────────────

func setSystemProxy(cfg ProxyConfig) error {
	services, err := networkServices()
	if err != nil {
		return err
	}
	var errs []string
	for _, svc := range services {
		if err := exec.Command("networksetup", "-setwebproxy", svc, cfg.Host, cfg.Port).Run(); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", svc, err))
			continue
		}
		if err := exec.Command("networksetup", "-setsecurewebproxy", svc, cfg.Host, cfg.Port).Run(); err != nil {
			errs = append(errs, fmt.Sprintf("%s (https): %v", svc, err))
		}
		// Bypass localhost so the dashboard itself isn't proxied.
		_ = exec.Command("networksetup", "-setproxybypassdomains", svc, "localhost", "127.0.0.1").Run()
	}
	if len(errs) > 0 {
		fmt.Printf("[proxy] macOS proxy set with warnings: %s\n", strings.Join(errs, "; "))
	} else {
		fmt.Printf("[proxy] macOS system proxy set to %s\n", cfg.Server())
	}
	return nil
}

func clearSystemProxy() error {
	services, err := networkServices()
	if err != nil {
		return err
	}
	for _, svc := range services {
		_ = exec.Command("networksetup", "-setwebproxystate", svc, "off").Run()
		_ = exec.Command("networksetup", "-setsecurewebproxystate", svc, "off").Run()
	}
	fmt.Println("[proxy] macOS system proxy disabled")
	return nil
}

// networkServices returns the list of active network service names
// (e.g. "Wi-Fi", "USB 10/100/1000 LAN").
func networkServices() ([]string, error) {
	out, err := exec.Command("networksetup", "-listallnetworkservices").Output()
	if err != nil {
		return nil, fmt.Errorf("platform: networksetup list: %w", err)
	}
	var services []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		// Skip the header line and disabled services (prefixed with *).
		if line == "" || strings.HasPrefix(line, "An asterisk") || strings.HasPrefix(line, "*") {
			continue
		}
		services = append(services, line)
	}
	return services, nil
}

// ── CA trust ─────────────────────────────────────────────────────────────────

func trustCA(caCertPath string) error {
	cmd := exec.Command("security", "add-trusted-cert",
		"-d", "-r", "trustRoot",
		"-k", "/Library/Keychains/System.keychain",
		caCertPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Try current-user keychain if system keychain requires elevation.
		cmd2 := exec.Command("security", "add-trusted-cert",
			"-r", "trustRoot",
			"-k", fmt.Sprintf("%s/Library/Keychains/login.keychain-db", homeDir()),
			caCertPath,
		)
		out2, err2 := cmd2.CombinedOutput()
		if err2 != nil {
			return fmt.Errorf("platform: security add-trusted-cert: %w\nsystem: %s\nuser: %s", err2, out, out2)
		}
		fmt.Println("[ca] Root certificate trusted in macOS login keychain")
		return nil
	}
	fmt.Println("[ca] Root certificate trusted in macOS System keychain")
	return nil
}

func isTrusted(commonName string) (bool, error) {
	out, err := exec.Command("security", "find-certificate", "-c", commonName).Output()
	if err != nil {
		return false, nil
	}
	return len(out) > 0, nil
}

func homeDir() string {
	out, _ := exec.Command("sh", "-c", "echo $HOME").Output()
	return strings.TrimSpace(string(out))
}
