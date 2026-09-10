package api

import (
	"encoding/json"
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

// TestUIHandlerRejectsNonGetMethods pins R37: uiHandler is registered at "/"
// with no method restriction (a method-restricted "GET /" pattern conflicts
// with the existing "/api/" registration and panics at startup), so
// uiHandler itself must refuse anything but GET/HEAD. Every other method
// gets the same 404 NOT_FOUND JSON:API response "/api/" returns for unknown
// endpoints, in both the built and not-yet-built states.
func TestUIHandlerRejectsNonGetMethods(t *testing.T) {
	for _, present := range []bool{true, false} {
		fsys := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>app</html>")}}
		h := uiHandler(fsys, present)
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(method, "/", nil))
			if w.Code != http.StatusNotFound {
				t.Errorf("present=%v %s / = %d, want 404", present, method, w.Code)
				continue
			}
			var doc struct {
				Errors []struct{ Code string } `json:"errors"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
				t.Fatalf("decode error doc %s: %v", w.Body.String(), err)
			}
			if len(doc.Errors) != 1 || doc.Errors[0].Code != "NOT_FOUND" {
				t.Errorf("present=%v %s errors = %s, want code NOT_FOUND", present, method, w.Body.String())
			}
		}
	}
}

// TestUIHandlerAllowsHead pins that HEAD, like GET, still reaches the SPA
// handling rather than being rejected by the GET/HEAD gate added for R37.
func TestUIHandlerAllowsHead(t *testing.T) {
	fsys := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>app</html>")}}
	h := uiHandler(fsys, true)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/", nil))
	if w.Code != http.StatusOK {
		t.Errorf("HEAD / = %d, want 200", w.Code)
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
