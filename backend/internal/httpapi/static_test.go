package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticSiteServesReactAndPreservesAPI(t *testing.T) {
	dist := t.TempDir()
	if err := os.Mkdir(filepath.Join(dist, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<!doctype html><div id=\"root\"></div>"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "assets", "app-123.js"), []byte("console.log('react')"), 0644); err != nil {
		t.Fatal(err)
	}
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/odds" || r.URL.Path == "/healthz" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		http.NotFound(w, r)
	})
	handler, err := WithStaticSite(api, dist)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		method string
		path   string
		want   int
		body   string
		cache  string
	}{
		{http.MethodGet, "/", 200, "<div id=\"root\">", "no-cache"},
		{http.MethodGet, "/games/123", 200, "<div id=\"root\">", "no-cache"},
		{http.MethodGet, "/assets/app-123.js", 200, "console.log('react')", "immutable"},
		{http.MethodGet, "/assets/missing.js", 404, "", ""},
		{http.MethodGet, "/missing.js", 404, "", ""},
		{http.MethodGet, "/..\\secret", 404, "", ""},
		{http.MethodGet, "/api/odds", 200, `{"ok":true}`, ""},
		{http.MethodGet, "/api/missing", 404, "", ""},
		{http.MethodGet, "/healthz", 200, `{"ok":true}`, ""},
		{http.MethodPost, "/games/123", 405, "", ""},
	} {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body = %q", response.Code, test.want, response.Body.String())
			}
			if test.body != "" && !strings.Contains(response.Body.String(), test.body) {
				t.Fatalf("body %q does not contain %q", response.Body.String(), test.body)
			}
			if test.cache != "" && !strings.Contains(response.Header().Get("Cache-Control"), test.cache) {
				t.Fatalf("cache control = %q, want %q", response.Header().Get("Cache-Control"), test.cache)
			}
			if test.path == "/api/missing" && strings.Contains(response.Body.String(), "<div") {
				t.Fatal("unknown API route must not return React HTML")
			}
		})
	}
}

func TestStaticSiteRequiresBuiltIndex(t *testing.T) {
	_, err := WithStaticSite(http.NotFoundHandler(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "index.html") {
		t.Fatalf("expected a missing build error, got %v", err)
	}
}
