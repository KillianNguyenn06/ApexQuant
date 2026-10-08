package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddedDashboardAndAPIDispatch(t *testing.T) {
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/config" {
			t.Error("unexpected API route")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"max_range_days":366}`)
	})
	h := Handler(api)
	for _, path := range []string{"/", "/assets/styles.css", "/assets/app.mjs", "/assets/api.mjs", "/assets/form.mjs", "/assets/replay.mjs", "/assets/charts.mjs", "/assets/details.mjs", "/assets/motion.mjs", "/api/config"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Body.Len() == 0 || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s: %d", path, w.Code)
		}
		if path == "/" && (!strings.Contains(w.Body.String(), "Your portfolio") || !strings.Contains(w.Header().Get("Content-Security-Policy"), "connect-src 'self'")) {
			t.Fatal("missing dashboard or same-origin policy")
		}
		if strings.HasSuffix(path, ".mjs") && !strings.HasPrefix(w.Header().Get("Content-Type"), "text/javascript") {
			t.Fatal("browser modules have incorrect content type")
		}
	}
	for _, path := range []string{"/tests/api.test.mjs", "/package.json", "/assets/../handler.go", "/unknown"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Fatalf("exposed %s", path)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
	if w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" {
		t.Fatal("static dashboard accepted a mutation")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("HEAD", "/", nil))
	if w.Code != 200 || w.Body.Len() != 0 {
		t.Fatal("invalid HEAD response")
	}
}
