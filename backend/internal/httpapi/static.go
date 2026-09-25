package httpapi

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// WithStaticSite adds the production React build without changing the API-only
// handler used during local development.
func WithStaticSite(api http.Handler, dist string) (http.Handler, error) {
	root, err := filepath.Abs(dist)
	if err != nil {
		return nil, fmt.Errorf("resolve static directory: %w", err)
	}
	index := filepath.Join(root, "index.html")
	info, err := os.Stat(index)
	if err != nil {
		return nil, fmt.Errorf("find React build at %q: %w", index, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("React build index %q is not a regular file", index)
	}
	files := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
			api.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.Contains(r.URL.Path, "\\") {
			http.NotFound(w, r)
			return
		}

		cleanPath := path.Clean("/" + r.URL.Path)
		name := filepath.FromSlash(strings.TrimPrefix(cleanPath, "/"))
		if name != "" && !filepath.IsLocal(name) {
			http.NotFound(w, r)
			return
		}
		if name != "" {
			filePath := filepath.Join(root, name)
			if stat, statErr := os.Stat(filePath); statErr == nil && stat.Mode().IsRegular() {
				if strings.HasPrefix(cleanPath, "/assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		if strings.HasPrefix(cleanPath, "/assets/") || path.Ext(cleanPath) != "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, index)
	}), nil
}
