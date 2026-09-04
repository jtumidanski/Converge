package session

import (
	"errors"
	"fmt"
	"time"

	"github.com/jtumidanski/converge/internal/diff"
	"github.com/jtumidanski/converge/internal/gitx"
)

// ResolvedChangeParams is the input to NewResolvedChange.
type ResolvedChangeParams struct {
	Number      int
	Title       string
	Author      string
	WebURL      string
	MergedAt    time.Time
	Strategy    Strategy
	LandingSHAs []string
	SourceSHA   string
}

// ResolvedChange is an immutable record of how a selected change landed.
type ResolvedChange struct {
	number      int
	title       string
	author      string
	webURL      string
	mergedAt    time.Time
	strategy    Strategy
	landingSHAs []string
	sourceSHA   string
}

// NewResolvedChange validates params.
func NewResolvedChange(p ResolvedChangeParams) (ResolvedChange, error) {
	if p.Number <= 0 {
		return ResolvedChange{}, errors.New("resolved change: number must be positive")
	}
	if p.Title == "" {
		return ResolvedChange{}, errors.New("resolved change: title is required")
	}
	switch p.Strategy {
	case StrategyMerge, StrategySquash, StrategyRebase:
	default:
		return ResolvedChange{}, fmt.Errorf("resolved change: unknown strategy %q", p.Strategy)
	}
	if len(p.LandingSHAs) == 0 {
		return ResolvedChange{}, errors.New("resolved change: landing shas required")
	}
	for _, s := range p.LandingSHAs {
		if err := gitx.ValidateSHA(s); err != nil {
			return ResolvedChange{}, err
		}
	}
	if p.SourceSHA != "" {
		if err := gitx.ValidateSHA(p.SourceSHA); err != nil {
			return ResolvedChange{}, err
		}
	}
	return ResolvedChange{
		number:      p.Number,
		title:       p.Title,
		author:      p.Author,
		webURL:      p.WebURL,
		mergedAt:    p.MergedAt,
		strategy:    p.Strategy,
		landingSHAs: append([]string(nil), p.LandingSHAs...),
		sourceSHA:   p.SourceSHA,
	}, nil
}

func (r ResolvedChange) Number() int         { return r.number }
func (r ResolvedChange) Title() string       { return r.title }
func (r ResolvedChange) Author() string      { return r.author }
func (r ResolvedChange) WebURL() string      { return r.webURL }
func (r ResolvedChange) MergedAt() time.Time { return r.mergedAt }
func (r ResolvedChange) Strategy() Strategy  { return r.strategy }
func (r ResolvedChange) SourceSHA() string   { return r.sourceSHA }
func (r ResolvedChange) LandingSHAs() []string {
	return append([]string(nil), r.landingSHAs...)
}

// Session is an immutable review session; transitions return new values.
type Session struct {
	id               string
	providerID       string
	repository       string
	baseBranch       string
	baseSHA          string
	headSHA          string
	requestedChanges []int
	resolved         []ResolvedChange
	status           Status
	stage            string
	err              *ReviewError
	totals           *diff.Totals
	files            []diff.FileSummary
	createdAt        time.Time
	updatedAt        time.Time
	expiresAt        time.Time
}

func (s Session) ID() string           { return s.id }
func (s Session) ProviderID() string   { return s.providerID }
func (s Session) Repository() string   { return s.repository }
func (s Session) BaseBranch() string   { return s.baseBranch }
func (s Session) BaseSHA() string      { return s.baseSHA }
func (s Session) HeadSHA() string      { return s.headSHA }
func (s Session) Status() Status       { return s.status }
func (s Session) Stage() string        { return s.stage }
func (s Session) CreatedAt() time.Time { return s.createdAt }
func (s Session) UpdatedAt() time.Time { return s.updatedAt }
func (s Session) ExpiresAt() time.Time { return s.expiresAt }
func (s Session) Error() *ReviewError  { return s.err.clone() }
func (s Session) RequestedChanges() []int {
	return append([]int(nil), s.requestedChanges...)
}
func (s Session) ResolvedChanges() []ResolvedChange {
	return append([]ResolvedChange(nil), s.resolved...)
}
func (s Session) Files() []diff.FileSummary { return append([]diff.FileSummary(nil), s.files...) }
func (s Session) Totals() *diff.Totals {
	if s.totals == nil {
		return nil
	}
	t := *s.totals
	return &t
}

// IsExpired reports whether now is past the expiry.
func (s Session) IsExpired(now time.Time) bool { return !now.Before(s.expiresAt) }

// IsActive is true unless the session is FINISHED or EXPIRED.
func (s Session) IsActive() bool { return s.status != StatusFinished && s.status != StatusExpired }

// EarliestChange returns the first resolved change (sorted by merge time).
func (s Session) EarliestChange() (ResolvedChange, bool) {
	if len(s.resolved) == 0 {
		return ResolvedChange{}, false
	}
	return s.resolved[0], true
}

func (s Session) touch(now time.Time) Session { s.updatedAt = now; return s }

func (s Session) WithStage(stage string, now time.Time) Session {
	s.stage = stage
	return s.touch(now)
}

func (s Session) WithResolved(rcs []ResolvedChange, now time.Time) Session {
	s.resolved = append([]ResolvedChange(nil), rcs...)
	return s.touch(now)
}

func (s Session) WithBase(sha string, now time.Time) (Session, error) {
	if err := gitx.ValidateSHA(sha); err != nil {
		return Session{}, err
	}
	s.baseSHA = sha
	return s.touch(now), nil
}

// Ready marks the session READY; requires a base SHA.
func (s Session) Ready(headSHA string, files []diff.FileSummary, totals diff.Totals, now time.Time) (Session, error) {
	if s.baseSHA == "" {
		return Session{}, errors.New("session: cannot be ready without a base sha")
	}
	if err := gitx.ValidateSHA(headSHA); err != nil {
		return Session{}, err
	}
	s.headSHA = headSHA
	s.files = append([]diff.FileSummary(nil), files...)
	s.totals = &totals
	s.status = StatusReady
	s.stage = ""
	s.err = nil
	return s.touch(now), nil
}

func (s Session) Conflicted(err *ReviewError, now time.Time) Session {
	s.status = StatusConflicted
	s.stage = ""
	s.err = err.clone()
	return s.touch(now)
}

func (s Session) Failed(err *ReviewError, now time.Time) Session {
	s.status = StatusFailed
	s.stage = ""
	s.err = err.clone()
	return s.touch(now)
}

func (s Session) Finished(now time.Time) Session {
	s.status = StatusFinished
	s.stage = ""
	return s.touch(now)
}

func (s Session) Expired(now time.Time) Session {
	s.status = StatusExpired
	s.stage = ""
	return s.touch(now)
}
