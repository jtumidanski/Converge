package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jtumidanski/converge/internal/diff"
	"github.com/jtumidanski/converge/internal/jsonapi"
	"github.com/jtumidanski/converge/internal/review"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/workspace"
)

type createReviewAttributes struct {
	Provider   string `json:"provider"`
	Repository string `json:"repository"`
	BaseBranch string `json:"baseBranch"`
	Changes    []int  `json:"changes"`
}

type includedChange struct {
	Number   int        `json:"number"`
	Title    string     `json:"title"`
	Author   string     `json:"author"`
	MergedAt *time.Time `json:"mergedAt"`
	WebURL   string     `json:"webUrl"`
	Strategy string     `json:"strategy"`
}

type reviewAttributes struct {
	Status          string               `json:"status"`
	Stage           *string              `json:"stage"`
	Provider        string               `json:"provider"`
	Repository      string               `json:"repository"`
	BaseBranch      string               `json:"baseBranch"`
	BaseSHA         *string              `json:"baseSha"`
	HeadSHA         *string              `json:"headSha"`
	BaseDescription string               `json:"baseDescription"`
	Changes         []int                `json:"changes"`
	Included        []includedChange     `json:"included"`
	Totals          *diff.Totals         `json:"totals"`
	Error           *session.ReviewError `json:"error"`
	CreatedAt       time.Time            `json:"createdAt"`
	UpdatedAt       time.Time            `json:"updatedAt"`
	ExpiresAt       time.Time            `json:"expiresAt"`
}

func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// baseDescription renders "Immediately before #421".
func baseDescription(s session.Session) string {
	if rc, ok := s.EarliestChange(); ok {
		return fmt.Sprintf("Immediately before #%d", rc.Number())
	}
	return ""
}

func reviewResource(s session.Session) jsonapi.Resource {
	included := make([]includedChange, 0, len(s.ResolvedChanges()))
	for _, rc := range s.ResolvedChanges() {
		included = append(included, includedChange{Number: rc.Number(), Title: rc.Title(), Author: rc.Author(), MergedAt: timePtr(rc.MergedAt()), WebURL: rc.WebURL(), Strategy: string(rc.Strategy())})
	}
	return jsonapi.Resource{
		Type: "reviews", ID: s.ID(),
		Attributes: reviewAttributes{
			Status: string(s.Status()), Stage: stringPtr(s.Stage()), Provider: s.ProviderID(), Repository: s.Repository(),
			BaseBranch: s.BaseBranch(), BaseSHA: stringPtr(s.BaseSHA()), HeadSHA: stringPtr(s.HeadSHA()),
			BaseDescription: baseDescription(s), Changes: s.RequestedChanges(), Included: included, Totals: s.Totals(),
			Error: s.Error(), CreatedAt: s.CreatedAt(), UpdatedAt: s.UpdatedAt(), ExpiresAt: s.ExpiresAt(),
		},
		Relationships: map[string]jsonapi.Relationship{
			"files": {Links: jsonapi.Links{Related: "/api/reviews/" + s.ID() + "/files"}},
		},
	}
}

func (s *server) createReview(w http.ResponseWriter, r *http.Request) {
	attrs, err := jsonapi.Decode[createReviewAttributes](r, "reviews")
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	sess, err := s.deps.Service.Create(r.Context(), review.CreateInput{
		ProviderID: attrs.Provider, Repository: attrs.Repository, BaseBranch: attrs.BaseBranch, Changes: attrs.Changes,
	})
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	s.deps.Service.StartBuild(s.buildCtx, sess.ID())
	if err := jsonapi.WriteOne(w, http.StatusAccepted, reviewResource(sess)); err != nil {
		s.deps.Log.Error("write review failed", "error", err)
	}
}

func (s *server) listReviews(w http.ResponseWriter, _ *http.Request) {
	sessions := s.deps.Service.List()
	out := make([]jsonapi.Resource, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, reviewResource(sess))
	}
	if err := jsonapi.WriteList(w, http.StatusOK, out, nil); err != nil {
		s.deps.Log.Error("write reviews failed", "error", err)
	}
}

// sessionFor resolves {id}, writing 404 when it genuinely does not exist and
// 500 GIT_FAILURE when the store found its session.json unreadable or
// invalid (session.Store.Corrupted) — Get's bool alone cannot tell the two
// apart, per session.Store.Get's documented contract.
func (s *server) sessionFor(w http.ResponseWriter, r *http.Request) (session.Session, bool) {
	id := r.PathValue("id")
	if err := workspace.ValidateSessionID(id); err != nil {
		_ = jsonapi.WriteError(w, http.StatusNotFound, "NOT_FOUND", jsonapi.StatusTitle(http.StatusNotFound), "No review exists with that id.")
		return session.Session{}, false
	}
	sess, ok := s.deps.Service.Get(id)
	if !ok {
		if s.deps.Service.Corrupted(id) {
			s.deps.Log.Error("session record unreadable", slog.String("session", id))
			_ = jsonapi.WriteError(w, http.StatusInternalServerError, "GIT_FAILURE", jsonapi.StatusTitle(http.StatusInternalServerError), "This review's stored state could not be read.")
			return session.Session{}, false
		}
		_ = jsonapi.WriteError(w, http.StatusNotFound, "NOT_FOUND", jsonapi.StatusTitle(http.StatusNotFound), "No review exists with that id.")
		return session.Session{}, false
	}
	return sess, true
}

func (s *server) getReview(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFor(w, r)
	if !ok {
		return
	}
	if err := jsonapi.WriteOne(w, http.StatusOK, reviewResource(sess)); err != nil {
		s.deps.Log.Error("write review failed", "error", err)
	}
}

// deleteReview is idempotent and always answers 204.
func (s *server) deleteReview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := workspace.ValidateSessionID(id); err == nil {
		if err := s.deps.Service.Finish(r.Context(), id); err != nil && !errors.Is(err, session.ErrNotFound) {
			s.deps.Log.Warn("finish failed", "session", id, "error", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
