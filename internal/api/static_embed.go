package api

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// static holds the compiled Angular app.
// The directory is populated by `ng build --output-path ../internal/api/static`.
// When the directory doesn't exist at compile time Go embeds an empty FS,
// and the handler falls back to a helpful message — so the binary always
// compiles whether or not you've run `npm run build` yet.

//go:embed static
var staticFiles embed.FS

// staticHandler returns an http.Handler that serves the Angular SPA.
// All paths that don't match a real file are rewritten to index.html so
// Angular's client-side router works correctly on direct URL access or refresh.
func staticHandler() http.Handler {
	// Serve from the `static` subdirectory inside the embedded FS.
	sub, err := fs.Sub(staticFiles, "static/browser")
	if err != nil {
		sub, err = fs.Sub(staticFiles, "static")
	}
	if err != nil {
		// embed.FS.Sub only errors if the path doesn't exist in the FS.
		// Return a plain 404 handler so the binary still runs without a build.
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w,
				"Angular UI not built yet.\n\nRun:\n  cd ui && npm install && npx ng build --configuration production --output-path ../internal/api/static\nthen rebuild the Go binary.",
				http.StatusServiceUnavailable)
		})
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the exact file first.
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean == "" {
			clean = "index.html"
		}
		f, err := sub.Open(clean)
		if err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// Not found — serve index.html so Angular's router takes over.
		urlCopy := *r.URL  // dereference: makes a new url.URL value on the stack
		urlCopy.Path = "/" // mutate the copy, not the original
		r2 := *r           // copy the request
		r2.URL = &urlCopy  // point r2.URL at our copy — correct type *url.URL
		fileServer.ServeHTTP(w, &r2)

	})
}
