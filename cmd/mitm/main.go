package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"mitm/internal/api"
	"mitm/internal/bus"
	"mitm/internal/platform"
	"mitm/internal/proxy"
	"mitm/internal/store"
	gotls "mitm/internal/tls"
	"mitm/plugins/http2decoder"
)

func main() {
	proxyAddr := flag.String("addr", ":8080", "Proxy listen address")
	apiAddr := flag.String("api-addr", ":9000", "API + GUI listen address")
	caDir := flag.String("ca-dir", defaultCADir(), "Directory for CA cert/key storage")
	storeCap := flag.Int("store-cap", 10_000, "Maximum exchanges to keep in memory")
	passthru := flag.String("passthrough", "", "Comma-separated host globs to tunnel without interception")
	installCA := flag.Bool("install-ca", false, "Trust the CA in the OS store and exit")
	noProxy := flag.Bool("no-proxy", false, "Do not configure the OS system proxy automatically")
	noBrowser := flag.Bool("no-browser", false, "Do not open the browser automatically on startup")
	verbose := flag.Bool("v", false, "Log each exchange to stdout")
	flag.Parse()

	// ── CA ────────────────────────────────────────────────────────────────────
	ca, err := gotls.LoadOrCreate(*caDir)
	if err != nil {
		log.Fatalf("CA init: %v", err)
	}

	caCertPath := filepath.Join(*caDir, "ca.crt")

	// --install-ca: trust the cert in the OS store then exit.
	if *installCA {
		trusted, _ := platform.IsTrusted("GoProxy Local CA")
		if trusted {
			fmt.Println("[ca] Certificate is already trusted.")
			os.Exit(0)
		}
		if err := platform.TrustCA(caCertPath); err != nil {
			log.Fatalf("Trust CA: %v", err)
		}
		os.Exit(0)
	}

	// ── Auto-trust CA if not already trusted ─────────────────────────────────
	trusted, _ := platform.IsTrusted("GoProxy Local CA")
	if !trusted {
		fmt.Println("[ca] Root certificate not yet trusted — attempting automatic installation…")
		if err := platform.TrustCA(caCertPath); err != nil {
			fmt.Printf("[ca] Auto-trust failed: %v\n", err)
			fmt.Printf("[ca] Run manually:  mitm --install-ca\n")
		}
	}

	// ── Store + Bus ───────────────────────────────────────────────────────────
	exchStore := store.New(*storeCap)
	exchBus := bus.New()

	if *verbose {
		logSub := exchBus.Subscribe(256)
		go func() {
			for ev := range logSub.C() {
				ex := ev.Exchange
				method, rawURL, status := exchangeSummary(ex)
				fmt.Printf("[%5d] %-6s %-60s %s  %v\n",
					ex.ID, method, rawURL, status, ex.Timing.Duration())
			}
		}()
	}

	// ── Passthrough ───────────────────────────────────────────────────────────
	var passthroughHosts []string
	if *passthru != "" {
		for _, h := range strings.Split(*passthru, ",") {
			if h = strings.TrimSpace(h); h != "" {
				passthroughHosts = append(passthroughHosts, h)
			}
		}
	}

	// ── Proxy server ──────────────────────────────────────────────────────────
	proxySrv := proxy.New(proxy.Config{
		Addr:      *proxyAddr,
		CA:        ca,
		H2Decoder: http2decoder.Decoder{},
		OnExchange: proxy.MultiHandler(
			exchStore.Handler(),
			exchBus.Handler(),
		),
		PassthroughHosts: passthroughHosts,
	})

	// ── API server ────────────────────────────────────────────────────────────
	apiSrv := api.New(exchStore, exchBus, ca, *apiAddr)

	// ── OS system proxy ───────────────────────────────────────────────────────
	// Extract the port from proxyAddr (strips the leading colon).
	proxyPort := strings.TrimPrefix(*proxyAddr, ":")
	if !*noProxy {
		if err := platform.SetSystemProxy(platform.ProxyConfig{
			Host: "127.0.0.1",
			Port: proxyPort,
		}); err != nil {
			fmt.Printf("[proxy] Could not set system proxy: %v\n", err)
			fmt.Println("[proxy] Set it manually: HTTP + HTTPS → 127.0.0.1" + *proxyAddr)
		}
	}

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Restore system proxy on exit.
	go func() {
		<-ctx.Done()
		if !*noProxy {
			_ = platform.ClearSystemProxy()
		}
	}()

	banner(*proxyAddr, *apiAddr, *caDir, passthroughHosts)

	// Open the dashboard in the default browser.
	if !*noBrowser {
		go platform.OpenBrowser("http://localhost" + *apiAddr)
	}

	go func() {
		if err := apiSrv.Start(); err != nil {
			log.Printf("[api] stopped: %v", err)
		}
	}()

	if err := proxySrv.ListenAndServe(ctx); err != nil {
		log.Printf("[proxy] stopped: %v", err)
	}

	fmt.Printf("\n[mitm] Shutdown complete. %s\n", exchStore.Stats())
}

func banner(proxyAddr, apiAddr, caDir string, passthrough []string) {
	sep := strings.Repeat("═", 46)
	fmt.Printf("╔%s╗\n", sep)
	fmt.Printf("║%s║\n", center("mitm  —  HTTPS interception proxy", 46))
	fmt.Printf("╠%s╣\n", sep)
	fmt.Printf("║  Proxy    %-35s║\n", "127.0.0.1"+proxyAddr)
	fmt.Printf("║  GUI      %-35s║\n", "http://localhost"+apiAddr)
	fmt.Printf("║  CA dir   %-35s║\n", caDir)
	if len(passthrough) > 0 {
		fmt.Printf("║  Skip     %-35s║\n", strings.Join(passthrough, ", "))
	}
	fmt.Printf("╚%s╝\n\n", sep)
}

func center(s string, width int) string {
	pad := (width - len(s)) / 2
	if pad < 0 {
		pad = 0
	}
	right := width - len(s) - pad
	if right < 0 {
		right = 0
	}
	return strings.Repeat(" ", pad) + s + strings.Repeat(" ", right)
}

func defaultCADir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".mitm"
	}
	return filepath.Join(home, ".config", "mitm")
}

func exchangeSummary(ex *proxy.Exchange) (method, rawURL, status string) {
	method, rawURL, status = "?", "?", "?"
	if ex.Request != nil {
		method = ex.Request.Method
		if ex.Request.URL != nil {
			rawURL = ex.Request.URL.String()
		}
	}
	if ex.Response != nil {
		status = fmt.Sprintf("%d", ex.Response.StatusCode)
	}
	if ex.Error != nil {
		status = "ERR"
	}
	return
}
