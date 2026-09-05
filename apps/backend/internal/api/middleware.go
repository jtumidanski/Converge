package api

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jtumidanski/converge/internal/jsonapi"
)

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b[:])
}

// withMiddleware applies request id, logging, recovery, and content negotiation.
func withMiddleware(h http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := newRequestID()
		w.Header().Set("X-Request-Id", requestID)
		sw := &statusWriter{ResponseWriter: w}
		start := time.Now()
		defer func() {
			if rec := recover(); rec != nil {
				log.Error("panic serving request", slog.String("request_id", requestID), slog.String("path", r.URL.Path), slog.Any("panic", rec))
				if sw.status == 0 {
					// No code named "INTERNAL" exists in this project; an
					// unclassified server-side failure reports as GIT_FAILURE,
					// matching writeDomainError's default branch.
					_ = jsonapi.WriteError(sw, http.StatusInternalServerError, "GIT_FAILURE", jsonapi.StatusTitle(http.StatusInternalServerError), "An unexpected error occurred.")
				}
			}
			log.Info("request",
				slog.String("request_id", requestID),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", sw.status),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			)
		}()
		if strings.HasPrefix(r.URL.Path, "/api/") && !acceptable(r.Header.Get("Accept")) {
			_ = jsonapi.WriteError(sw, http.StatusNotAcceptable, "NOT_ACCEPTABLE", jsonapi.StatusTitle(http.StatusNotAcceptable), "This endpoint returns "+jsonapi.MediaType+".")
			return
		}
		h.ServeHTTP(sw, r)
	})
}

// acceptable allows */*, application/json and the JSON:API media type.
func acceptable(accept string) bool {
	if strings.TrimSpace(accept) == "" {
		return true
	}
	for _, part := range strings.Split(accept, ",") {
		media := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if media == "*/*" || media == "application/*" || media == "application/json" || media == jsonapi.MediaType {
			return true
		}
	}
	return false
}
