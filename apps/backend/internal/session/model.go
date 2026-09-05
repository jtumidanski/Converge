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

// IsActive reports whether the session is in one of the live states. It is
// a whitelist rather than "not FINISHED and not EXPIRED" so that a
// zero-value Session (empty status — never a real session) is not reported
// as active: callers such as Store.SaveActive treat "active" as permission
// to write, and a value that was never built must never earn that.
func (s Session) IsActive() bool {
	switch s.status {
	case StatusCreating, StatusReady, StatusConflicted, StatusFailed:
		return true
	default:
		return false
	}
}

// EarliestChange returns the resolved change with the earliest MergedAt
// timestamp. Ties (two changes landed with the same merge timestamp) are
// broken by ascending change Number, so the result is deterministic
// regardless of the order changes were resolved or supplied to
// WithResolved.
func (s Session) EarliestChange() (ResolvedChange, bool) {
	if len(s.resolved) == 0 {
		return ResolvedChange{}, false
	}
	earliest := s.resolved[0]
	for _, rc := range s.resolved[1:] {
		if rc.mergedAt.Before(earliest.mergedAt) ||
			(rc.mergedAt.Equal(earliest.mergedAt) && rc.number < earliest.number) {
			earliest = rc
		}
	}
	return earliest, true
}

// isTerminal reports whether the session is in a state (FINISHED or
// EXPIRED) from which no further transition is meaningful: its workspace
// has already been cleaned up, so any transition that would make it appear
// active again (directly or indirectly) must be rejected rather than
// applied.
func (s Session) isTerminal() bool {
	return s.status == StatusFinished || s.status == StatusExpired
}

func (s Session) touch(now time.Time) Session { s.updatedAt = now; return s }

// WithStage sets the current processing stage. A terminal session (FINISHED
// or EXPIRED) is left unchanged: this is a no-op that returns the receiver
// as-is rather than an error, since callers driving a background pipeline
// should not need to fork their error handling for a session that raced a
// sweep or Finish.
func (s Session) WithStage(stage string, now time.Time) Session {
	if s.isTerminal() {
		return s
	}
	s.stage = stage
	return s.touch(now)
}

// WithResolved records the resolved changes. A terminal session is left
// unchanged; see WithStage.
func (s Session) WithResolved(rcs []ResolvedChange, now time.Time) Session {
	if s.isTerminal() {
		return s
	}
	s.resolved = append([]ResolvedChange(nil), rcs...)
	return s.touch(now)
}

// WithBase records the base SHA. Unlike the no-error transitions, this
// (along with Ready) already returns an error for structural reasons, so a
// terminal receiver is reported as an error rather than silently ignored.
func (s Session) WithBase(sha string, now time.Time) (Session, error) {
	if s.isTerminal() {
		return Session{}, fmt.Errorf("session: cannot set base on a terminal session (status %s)", s.status)
	}
	if err := gitx.ValidateSHA(sha); err != nil {
		return Session{}, err
	}
	s.baseSHA = sha
	return s.touch(now), nil
}

// Ready marks the session READY; requires a base SHA. A terminal receiver
// (FINISHED or EXPIRED) is rejected with an error rather than silently
// resurrected: its workspace has already been cleaned up, so marking it
// READY again would point a viewer at files that no longer exist.
func (s Session) Ready(headSHA string, files []diff.FileSummary, totals diff.Totals, now time.Time) (Session, error) {
	if s.isTerminal() {
		return Session{}, fmt.Errorf("session: cannot become ready from a terminal session (status %s)", s.status)
	}
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

// Conflicted marks the session CONFLICTED. A terminal session is left
// unchanged; see WithStage.
func (s Session) Conflicted(err *ReviewError, now time.Time) Session {
	if s.isTerminal() {
		return s
	}
	s.status = StatusConflicted
	s.stage = ""
	s.err = err.clone()
	return s.touch(now)
}

// Failed marks the session FAILED. A terminal session is left unchanged;
// see WithStage.
func (s Session) Failed(err *ReviewError, now time.Time) Session {
	if s.isTerminal() {
		return s
	}
	s.status = StatusFailed
	s.stage = ""
	s.err = err.clone()
	return s.touch(now)
}

// Finished marks the session FINISHED. Calling Finished on an already
// terminal session (FINISHED or EXPIRED) is a no-op that returns the
// receiver unchanged: an EXPIRED session must not be able to "un-expire"
// itself back to FINISHED.
func (s Session) Finished(now time.Time) Session {
	if s.isTerminal() {
		return s
	}
	s.status = StatusFinished
	s.stage = ""
	return s.touch(now)
}

// Expired marks the session EXPIRED. Calling Expired on an already terminal
// session is a no-op that returns the receiver unchanged; see Finished.
func (s Session) Expired(now time.Time) Session {
	if s.isTerminal() {
		return s
	}
	s.status = StatusExpired
	s.stage = ""
	return s.touch(now)
}
