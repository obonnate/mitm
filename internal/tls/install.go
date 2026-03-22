// Package tls also provides OS-level trust store helpers.
// This file contains the InstallCA function which guides the user through
// adding the Mitm root CA to their OS and/or browser trust store.
package tls

import (
	"fmt"
	"os/exec"
	"runtime"
)

// InstallInstructions prints platform-specific instructions for trusting the CA.
func InstallInstructions(caCertPath string) {
	fmt.Printf("\n══════════════════════════════════════════════════\n")
	fmt.Printf("  Install Mitm CA certificate\n")
	fmt.Printf("  Path: %s\n", caCertPath)
	fmt.Printf("══════════════════════════════════════════════════\n\n")

	switch runtime.GOOS {
	case "darwin":
		fmt.Println("macOS:")
		fmt.Printf("  sudo security add-trusted-cert -d -r trustRoot \\\n")
		fmt.Printf("    -k /Library/Keychains/System.keychain \\\n")
		fmt.Printf("    %s\n\n", caCertPath)
		fmt.Println("  Or open Keychain Access → System → Certificates →")
		fmt.Println("  drag in the .crt file → double-click → Trust → Always Trust")

	case "linux":
		fmt.Println("Ubuntu / Debian:")
		fmt.Printf("  sudo cp %s /usr/local/share/ca-certificates/Mitm.crt\n", caCertPath)
		fmt.Println("  sudo update-ca-certificates")
		fmt.Println("Fedora / RHEL:")
		fmt.Printf("  sudo cp %s /etc/pki/ca-trust/source/anchors/Mitm.crt\n", caCertPath)
		fmt.Println("  sudo update-ca-trust")
		fmt.Println("NSS (Chrome/Firefox on Linux):")
		fmt.Printf("  certutil -d sql:$HOME/.pki/nssdb -A -t C,, -n Mitm -i %s\n", caCertPath)

	case "windows":
		fmt.Println("Windows (run in an Administrator PowerShell):")
		fmt.Printf("  Import-Certificate -FilePath \"%s\" \\\n", caCertPath)
		fmt.Println("    -CertStoreLocation Cert:\\LocalMachine\\Root")

	default:
		fmt.Println("Unknown OS. Add the CA certificate to your trust store manually.")
	}

	fmt.Println("\nFirefox (all platforms):")
	fmt.Println("  about:preferences#privacy → Certificates → View Certificates →")
	fmt.Println("  Authorities tab → Import → select the .crt file → Trust for websites")
	fmt.Printf("\n══════════════════════════════════════════════════\n\n")
}

// TryAutoInstall attempts a best-effort automatic installation on macOS.
// Returns an error if the platform is unsupported or the command fails.
func TryAutoInstall(caCertPath string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("auto-install only supported on macOS; see instructions above")
	}
	cmd := exec.Command("security", "add-trusted-cert",
		"-d", "-r", "trustRoot",
		"-k", "/Library/Keychains/System.keychain",
		caCertPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("security add-trusted-cert: %w\n%s", err, out)
	}
	fmt.Println("[CA] Root certificate installed in macOS System Keychain.")
	return nil
}
