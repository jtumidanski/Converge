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

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	git := "ok"
	if _, err := exec.LookPath("git"); err != nil {
		git = "missing"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(healthResponse{Status: "ok", Version: buildinfo.Version, Checks: map[string]string{"git": git}}); err != nil {
		s.deps.Log.Error("write health failed", "error", err)
	}
}
