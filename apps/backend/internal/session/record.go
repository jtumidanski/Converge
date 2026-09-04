package session

import (
	"fmt"
	"time"

	"github.com/jtumidanski/converge/internal/diff"
)

// SchemaVersion of session.json.
const SchemaVersion = 1

// ResolvedChangeRecord is the DTO for ResolvedChange.
type ResolvedChangeRecord struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Author      string    `json:"author"`
	WebURL      string    `json:"webUrl"`
	MergedAt    time.Time `json:"mergedAt"`
	Strategy    Strategy  `json:"strategy"`
	LandingSHAs []string  `json:"landingShas"`
	SourceSHA   string    `json:"sourceSha,omitempty"`
}

// Record is the on-disk shape of a Session (PRD §6).
type Record struct {
	SchemaVersion    int                    `json:"schemaVersion"`
	ID               string                 `json:"id"`
	ProviderID       string                 `json:"providerId"`
	Repository       string                 `json:"repository"`
	BaseBranch       string                 `json:"baseBranch"`
	BaseSHA          *string                `json:"baseSha"`
	HeadSHA          *string                `json:"headSha"`
	RequestedChanges []int                  `json:"requestedChanges"`
	ResolvedChanges  []ResolvedChangeRecord `json:"resolvedChanges"`
	Status           Status                 `json:"status"`
	Stage            *string                `json:"stage"`
	Error            *ReviewError           `json:"error"`
	Totals           *diff.Totals           `json:"totals"`
	Files            []diff.FileSummary     `json:"files,omitempty"`
	CreatedAt        time.Time              `json:"createdAt"`
	UpdatedAt        time.Time              `json:"updatedAt"`
	ExpiresAt        time.Time              `json:"expiresAt"`
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ToRecord converts a Session for serialisation.
func ToRecord(s Session) Record {
	r := Record{
		SchemaVersion:    SchemaVersion,
		ID:               s.id,
		ProviderID:       s.providerID,
		Repository:       s.repository,
		BaseBranch:       s.baseBranch,
		BaseSHA:          optional(s.baseSHA),
		HeadSHA:          optional(s.headSHA),
		RequestedChanges: s.RequestedChanges(),
		ResolvedChanges:  make([]ResolvedChangeRecord, 0, len(s.resolved)),
		Status:           s.status,
		Stage:            optional(s.stage),
		Error:            s.Error(),
		Totals:           s.Totals(),
		Files:            s.Files(),
		CreatedAt:        s.createdAt,
		UpdatedAt:        s.updatedAt,
		ExpiresAt:        s.expiresAt,
	}
	for _, rc := range s.resolved {
		r.ResolvedChanges = append(r.ResolvedChanges, ResolvedChangeRecord{
			Number:      rc.number,
			Title:       rc.title,
			Author:      rc.author,
			WebURL:      rc.webURL,
			MergedAt:    rc.mergedAt,
			Strategy:    rc.strategy,
			LandingSHAs: rc.LandingSHAs(),
			SourceSHA:   rc.sourceSHA,
		})
	}
	return r
}

// FromRecord rebuilds a Session, validating enums and identifiers.
func FromRecord(r Record) (Session, error) {
	if r.SchemaVersion != SchemaVersion {
		return Session{}, fmt.Errorf("session %s: unsupported schema version %d", r.ID, r.SchemaVersion)
	}
	ttl := r.ExpiresAt.Sub(r.CreatedAt)
	s, err := NewBuilder().SetID(r.ID).SetProviderID(r.ProviderID).SetRepository(r.Repository).SetBaseBranch(r.BaseBranch).
		SetRequestedChanges(r.RequestedChanges).SetCreatedAt(r.CreatedAt).SetTTL(ttl).Build()
	if err != nil {
		return Session{}, fmt.Errorf("session %s: %w", r.ID, err)
	}
	switch r.Status {
	case StatusCreating, StatusReady, StatusConflicted, StatusFailed, StatusFinished, StatusExpired:
	default:
		return Session{}, fmt.Errorf("session %s: unknown status %q", r.ID, r.Status)
	}
	for _, rr := range r.ResolvedChanges {
		rc, err := NewResolvedChange(ResolvedChangeParams(rr))
		if err != nil {
			return Session{}, fmt.Errorf("session %s: %w", r.ID, err)
		}
		s.resolved = append(s.resolved, rc)
	}
	s.baseSHA = deref(r.BaseSHA)
	s.headSHA = deref(r.HeadSHA)
	s.status = r.Status
	s.stage = deref(r.Stage)
	s.err = r.Error.clone()
	if r.Totals != nil {
		t := *r.Totals
		s.totals = &t
	}
	s.files = append([]diff.FileSummary(nil), r.Files...)
	s.updatedAt = r.UpdatedAt
	s.expiresAt = r.ExpiresAt
	return s, nil
}
