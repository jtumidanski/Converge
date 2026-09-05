package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jtumidanski/converge/internal/jsonapi"
	"github.com/jtumidanski/converge/internal/provider"
)

type changeAttributes struct {
	Number       int        `json:"number"`
	Title        string     `json:"title"`
	Author       string     `json:"author"`
	SourceBranch string     `json:"sourceBranch"`
	TargetBranch string     `json:"targetBranch"`
	MergedAt     *time.Time `json:"mergedAt"`
	CreatedAt    *time.Time `json:"createdAt"`
	LandingSHA   *string    `json:"landingSha"`
	WebURL       string     `json:"webUrl"`
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func changeResource(c provider.ChangeRequest) jsonapi.Resource {
	var landing *string
	for _, candidate := range []string{c.MergeCommitSHA(), c.SquashCommitSHA()} {
		if candidate != "" {
			v := candidate
			landing = &v
			break
		}
	}
	return jsonapi.Resource{Type: "changes", ID: strconv.Itoa(c.Number()), Attributes: changeAttributes{
		Number: c.Number(), Title: c.Title(), Author: c.Author(), SourceBranch: c.SourceBranch(), TargetBranch: c.TargetBranch(),
		MergedAt: timePtr(c.MergedAt()), CreatedAt: timePtr(c.CreatedAt()), LandingSHA: landing, WebURL: c.WebURL(),
	}}
}

func (s *server) listChanges(w http.ResponseWriter, r *http.Request) {
	p, ok := s.providerFor(w, r)
	if !ok {
		return
	}
	name, err := repoNameFrom(r)
	if err != nil {
		_ = jsonapi.WriteError(w, http.StatusBadRequest, "INVALID_REPOSITORY", jsonapi.StatusTitle(http.StatusBadRequest), "The repository must look like owner/name.")
		return
	}
	q := r.URL.Query()
	if state := q.Get("state"); state != "" && state != "merged" {
		_ = jsonapi.WriteError(w, http.StatusBadRequest, "INVALID_STATE", jsonapi.StatusTitle(http.StatusBadRequest), "Only state=merged is supported.")
		return
	}
	repo, err := p.GetRepository(r.Context(), name)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	target := q.Get("target")
	if target == "" {
		target = repo.DefaultBranch()
	}
	page := pageFrom(r)
	res, err := p.ListMergedChanges(r.Context(), repo, target, q.Get("search"), page)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	out := make([]jsonapi.Resource, 0, len(res.Items))
	for _, c := range res.Items {
		out = append(out, changeResource(c))
	}
	meta := &jsonapi.Meta{Page: &jsonapi.PageMeta{Number: page.Number, Size: page.Size, HasNext: res.HasNext}}
	if err := jsonapi.WriteList(w, http.StatusOK, out, meta); err != nil {
		s.deps.Log.Error("write changes failed", "error", err)
	}
}
