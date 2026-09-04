package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDoJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Header().Set("Link", `<x>; rel="next"`)
			_, _ = w.Write([]byte(`{"a":1}`))
		case "/bad":
			w.WriteHeader(502)
			_, _ = w.Write([]byte(`{"message":"secret-body"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/ok", nil)
	var out struct{ A int }
	h, err := DoJSON(context.Background(), srv.Client(), req, &out)
	if err != nil || out.A != 1 || h.Get("Link") == "" {
		t.Fatalf("ok: %v %+v %v", err, out, h)
	}
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/bad", nil)
	_, err = DoJSON(context.Background(), srv.Client(), req, &out)
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "secret-body") {
		t.Fatalf("bad: %v", err)
	}
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/missing", nil)
	if _, err = DoJSON(context.Background(), srv.Client(), req, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	srv.Close()
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/ok", nil)
	if _, err = DoJSON(context.Background(), srv.Client(), req, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("network: %v", err)
	}
}
