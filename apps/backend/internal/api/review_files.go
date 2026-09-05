package api

import (
	"net/http"
	"os"

	"github.com/jtumidanski/converge/internal/diff"
	"github.com/jtumidanski/converge/internal/jsonapi"
)

type reviewFileAttributes struct {
	Path         string `json:"path"`
	PreviousPath string `json:"previousPath"`
	Status       string `json:"status"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	Binary       bool   `json:"binary"`
}

type reviewFileDiffAttributes struct {
	reviewFileAttributes
	Truncated bool   `json:"truncated"`
	Diff      string `json:"diff"`
}

func fileAttrs(f diff.FileSummary) reviewFileAttributes {
	return reviewFileAttributes{Path: f.Path, PreviousPath: f.PreviousPath, Status: string(f.Status), Additions: f.Additions, Deletions: f.Deletions, Binary: f.Binary}
}

func (s *server) listReviewFiles(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFor(w, r)
	if !ok {
		return
	}
	// The query form ?path= renders one file instead of the list.
	if path := r.URL.Query().Get("path"); path != "" {
		s.writeFileDiff(w, r, sess.ID(), path)
		return
	}
	files, err := s.deps.Service.Files(sess.ID())
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	out := make([]jsonapi.Resource, 0, len(files))
	for _, f := range files {
		out = append(out, jsonapi.Resource{Type: "review-files", ID: f.Path, Attributes: fileAttrs(f)})
	}
	if err := jsonapi.WriteList(w, http.StatusOK, out, nil); err != nil {
		s.deps.Log.Error("write review files failed", "error", err)
	}
}

func (s *server) getReviewFile(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFor(w, r)
	if !ok {
		return
	}
	path := r.PathValue("path")
	if path == "" {
		path = r.URL.Query().Get("path")
	}
	s.writeFileDiff(w, r, sess.ID(), path)
}

func (s *server) writeFileDiff(w http.ResponseWriter, r *http.Request, id, path string) {
	fd, err := s.deps.Service.FileDiff(r.Context(), id, path)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	res := jsonapi.Resource{Type: "review-file-diffs", ID: fd.Path, Attributes: reviewFileDiffAttributes{
		reviewFileAttributes: fileAttrs(fd.FileSummary), Truncated: fd.Truncated, Diff: fd.Diff,
	}}
	if err := jsonapi.WriteOne(w, http.StatusOK, res); err != nil {
		s.deps.Log.Error("write file diff failed", "error", err)
	}
}

// getReviewDiff streams combined.diff as text/plain.
func (s *server) getReviewDiff(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFor(w, r)
	if !ok {
		return
	}
	path, err := s.deps.Service.CombinedDiffPath(sess.ID())
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	f, err := os.Open(path) //nolint:gosec // path comes from CombinedDiffPath, derived from the workspace root and a session id already validated by sessionFor, not from raw user input
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	defer func() { _ = f.Close() }()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="combined.diff"`)
	http.ServeContent(w, r, "combined.diff", sess.UpdatedAt(), f)
}
