// Package browser provides a cross-platform helper to open a URL in the
// user's default web browser.
package browser

import (
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// Open opens url in the default browser after a short delay to let the HTTP
// server start accepting connections. It is non-blocking — the open happens
// in a background goroutine and the caller returns immediately.
func Open(url string) {
	go func() {
		// Give the API server a moment to bind its port before the browser hits it.
		time.Sleep(300 * time.Millisecond)
		if err := openURL(url); err != nil {
			fmt.Printf("[browser] could not open %s: %v\n", url, err)
		}
	}()
}

func openURL(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("cmd", "/c", "start", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default: // linux, bsd …
		if err := exec.Command("xdg-open", url).Start(); err == nil {
			return nil
		}
		for _, b := range []string{"google-chrome", "firefox", "chromium-browser", "chromium"} {
			if err := exec.Command(b, url).Start(); err == nil {
				return nil
			}
		}
		return fmt.Errorf("no suitable browser found; open %s manually", url)
	}
}
