package api

import (
	"encoding/json"
	"net/http"
	"os/exec"

	"github.com/jtumidanski/converge/internal/buildinfo"
)

type healthResponse struct {
	Status  string            `json:"status"`
	Version string            `json:"version"`
	Checks  map[string]string `json:"checks"`
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	git := "ok"
	if _, err := exec.LookPath("git"); err != nil {
		git = "missing"
	}
	checks := map[string]string{"git": git}
	// The key is absent in standalone mode, so the response is byte-identical
	// to the previous release there.
	if s.deps.DBPing != nil {
		checks["database"] = "ok"
		if err := s.deps.DBPing(r.Context()); err != nil {
			checks["database"] = "unreachable"
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(healthResponse{Status: "ok", Version: buildinfo.Version, Checks: checks}); err != nil {
		s.deps.Log.Error("write health failed", "error", err)
	}
}
