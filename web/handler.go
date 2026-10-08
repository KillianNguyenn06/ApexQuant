// Package web serves the local dashboard from the Go executable.
package web

import (
	"bytes"
	"embed"
	"net/http"
	"strings"
	"time"
)

//go:embed assets/index.html assets/styles.css assets/app.mjs assets/api.mjs assets/form.mjs assets/replay.mjs assets/charts.mjs assets/details.mjs assets/motion.mjs
var assets embed.FS

// Handler keeps API requests on the same origin; no proxy or credential-bearing
// browser requests to external market-data services are needed.
func Handler(api http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'")
		w.Header().Set("Cache-Control", "no-store")
		path, contentType := "", ""
		switch r.URL.Path {
		case "/":
			path, contentType = "assets/index.html", "text/html; charset=utf-8"
		case "/assets/styles.css":
			path, contentType = "assets/styles.css", "text/css; charset=utf-8"
		case "/assets/app.mjs", "/assets/api.mjs", "/assets/form.mjs", "/assets/replay.mjs", "/assets/charts.mjs", "/assets/details.mjs", "/assets/motion.mjs":
			path, contentType = strings.TrimPrefix(r.URL.Path, "/"), "text/javascript; charset=utf-8"
		default:
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Use GET or HEAD.", http.StatusMethodNotAllowed)
			return
		}
		data, err := assets.ReadFile(path)
		if err != nil {
			http.Error(w, "Dashboard unavailable.", 500)
			return
		}
		w.Header().Set("Content-Type", contentType)
		http.ServeContent(w, r, path, time.Time{}, bytes.NewReader(data))
	})
}
