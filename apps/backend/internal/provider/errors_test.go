package provider

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestStatusErrorMapping(t *testing.T) {
	cases := map[int]error{401: ErrAuth, 403: ErrAuth, 404: ErrNotFound, 429: ErrUnavailable, 500: ErrUnavailable, 503: ErrUnavailable, 418: ErrUnavailable}
	for status, want := range cases {
		err := NewStatusError("GET", "/x", status, http.Header{})
		if !errors.Is(err, want) {
			t.Errorf("%d -> %v, want %v", status, err, want)
		}
		if !strings.Contains(err.Error(), "GET /x") || !strings.Contains(err.Error(), strconv.Itoa(status)) {
			t.Errorf("message = %q", err.Error())
		}
	}
	h := http.Header{"X-Ratelimit-Remaining": []string{"0"}, "X-Ratelimit-Reset": []string{"1700000000"}}
	err := NewStatusError("GET", "/x", 403, h)
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "1700000000") {
		t.Errorf("rate-limited 403 should be unavailable with reset: %v", err)
	}
	h2 := http.Header{"Retry-After": []string{"30"}}
	if err := NewStatusError("GET", "/x", 429, h2); !strings.Contains(err.Error(), "retry after 30") {
		t.Errorf("retry-after missing: %v", err)
	}
}
