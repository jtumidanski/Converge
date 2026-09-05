package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestUIHandlerServesSPAWithFallback(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":           &fstest.MapFile{Data: []byte("<html>app</html>")},
		"assets/app-abc123.js": &fstest.MapFile{Data: []byte("console.log(1)")},
	}
	h := uiHandler(fsys, true)
	for _, path := range []string{"/", "/reviews/7f14b2c8", "/select"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK || w.Body.String() != "<html>app</html>" {
			t.Errorf("%s: %d %q", path, w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("%s cache header = %q", path, w.Header().Get("Cache-Control"))
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/assets/app-abc123.js", nil))
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") == "no-cache" {
		t.Errorf("asset: %d %q", w.Code, w.Header().Get("Cache-Control"))
	}
	// traversal is refused
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/../secret", nil))
	if w.Code == http.StatusOK && w.Body.String() != "<html>app</html>" {
		t.Errorf("traversal served %q", w.Body.String())
	}
}

func TestUIHandlerWithoutBuild(t *testing.T) {
	h := uiHandler(fstest.MapFS{}, false)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d", w.Code)
	}
	if !contains(w.Body.String(), "make build") {
		t.Errorf("body = %q", w.Body.String())
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
