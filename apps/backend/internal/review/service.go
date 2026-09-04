package review

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jtumidanski/converge/internal/diff"
	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/workspace"
)

// ErrNotReady is returned for file endpoints on a session that is not READY.
var ErrNotReady = errors.New("review: not ready")

// CombinedDiffFile is the file name inside a session directory.
const CombinedDiffFile = "combined.diff"

// buildCeiling bounds a single background build.
const buildCeiling = 60 * time.Minute

// defaultMaxConcurrentBuilds mirrors the MAX_CONCURRENT_BUILDS default and is
// used only when a caller supplies a non-positive value.
const defaultMaxConcurrentBuilds = 4

// Deps are the service's collaborators.
type Deps struct {
	Providers           *provider.Registry
	Mirrors             *mirror.Cache
	Workspaces          *workspace.Manager
	Store               *session.Store
	Applicator          ChangeApplicator
	Runner              gitx.Runner
	Log                 *slog.Logger
	SessionTTL          time.Duration
	MaxConcurrentBuilds int
	Now                 func() time.Time
}

// Service orchestrates reconstruction: validate, resolve, apply, diff.
type Service struct {
	deps     Deps
	resolver *Resolver
	sem      chan struct{}
}

// NewService wires the orchestrator.
func NewService(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		// Never reach for the package-global logger; a caller that supplies
		// no logger gets silence, not someone else's handler.
		d.Log = slog.New(slog.DiscardHandler)
	}
	if d.MaxConcurrentBuilds <= 0 {
		d.MaxConcurrentBuilds = defaultMaxConcurrentBuilds
	}
	return &Service{
		deps:     d,
		resolver: NewResolver(d.Mirrors, d.Log),
		sem:      make(chan struct{}, d.MaxConcurrentBuilds),
	}
}

// Get returns a session by ID.
func (s *Service) Get(id string) (session.Session, bool) { return s.deps.Store.Get(id) }

// List returns active sessions, newest first.
func (s *Service) List() []session.Session { return s.deps.Store.List() }

// Finish cleans up and marks the session FINISHED. It is idempotent.
func (s *Service) Finish(ctx context.Context, id string) error {
	if err := s.deps.Store.Finish(ctx, id); err != nil {
		return fmt.Errorf("review: finish %s: %w", id, err)
	}
	return nil
}

// Create validates the request and persists a CREATING session. It does not
// start the build; callers run StartBuild (HTTP) or Build (CLI).
func (s *Service) Create(ctx context.Context, in CreateInput) (session.Session, error) {
	in, err := in.Validate(ctx, s.deps.Runner)
	if err != nil {
		return session.Session{}, err
	}
	p, ok := s.deps.Providers.Get(in.ProviderID)
	if !ok {
		return session.Session{}, &InputError{
			Code:    CodeInvalidProvider,
			Field:   "provider",
			Message: fmt.Sprintf("Provider %q is not configured.", in.ProviderID),
		}
	}
	repo, err := p.GetRepository(ctx, in.Repository)
	if err != nil {
		if re := MapProviderError(p.ID(), in.Repository, err); re != nil {
			return session.Session{}, re
		}
		return session.Session{}, &session.ReviewError{
			Code:    session.CodeRepositoryUnavailable,
			Message: MsgRepositoryUnavailable(in.Repository),
		}
	}
	if in.BaseBranch == "" {
		in.BaseBranch = repo.DefaultBranch()
		if err := gitx.ValidateBranch(ctx, s.deps.Runner, in.BaseBranch); err != nil {
			return session.Session{}, &InputError{
				Code:    CodeInvalidBranch,
				Field:   "baseBranch",
				Message: "The repository's default branch name is not usable.",
			}
		}
	}
	id, err := session.NewID()
	if err != nil {
		return session.Session{}, fmt.Errorf("review: create: %w", err)
	}
	sess, err := session.NewBuilder().SetID(id).SetProviderID(p.ID()).SetRepository(repo.FullName()).
		SetBaseBranch(in.BaseBranch).SetRequestedChanges(in.Changes).
		SetCreatedAt(s.deps.Now()).SetTTL(s.deps.SessionTTL).Build()
	if err != nil {
		return session.Session{}, fmt.Errorf("review: create: %w", err)
	}
	if err := s.persist(sess); err != nil {
		return session.Session{}, err
	}
	s.deps.Log.Info("review created",
		slog.String("session", id),
		slog.String("provider", p.ID()),
		slog.String("repository", repo.FullName()),
		slog.Any("changes", in.Changes))
	return sess, nil
}

// StartBuild runs Build on a background goroutine bounded by the semaphore.
//
// ctx must be the server's lifetime context, not a request context: the build
// deliberately outlives the HTTP request that triggered it, and cancelling it
// on request completion would abort every asynchronous build immediately.
// Shutdown cancels the lifetime context so in-flight builds stop promptly;
// the next startup's LoadAll records them as INTERRUPTED.
func (s *Service) StartBuild(ctx context.Context, id string) {
	go func() {
		// Waiting for a slot is cancellable: on shutdown a queued build must
		// drain instead of acquiring a slot only to run a pipeline whose
		// context is already dead. A session that never starts stays CREATING,
		// so the next startup's LoadAll records it as INTERRUPTED (FR-8.6).
		select {
		case s.sem <- struct{}{}:
		case <-ctx.Done():
			s.deps.Log.Info("build not started; shutting down", slog.String("session", id))
			return
		}
		defer func() { <-s.sem }()
		buildCtx, cancel := context.WithTimeout(ctx, buildCeiling)
		defer cancel()
		s.Build(buildCtx, id)
	}()
}

// Build runs the pipeline synchronously and returns the final session.
//
// Every failure reaches the returned session as a terminal status plus its
// own error code; nothing is downgraded to a benign-looking result. The one
// case that cannot be reported through the session (the signature carries no
// error) is an id that is not in the store at all: there is no session to
// mark, so a zero Session is returned. It is unambiguous because every real
// session has a non-empty id: callers distinguish "unknown id" from any
// pipeline outcome with final.ID() == "" (its status is likewise the empty
// string, never READY), and the condition is logged at Error level.
func (s *Service) Build(ctx context.Context, id string) (final session.Session) {
	sess, ok := s.deps.Store.Get(id)
	if !ok {
		s.deps.Log.Error("build requested for unknown session", slog.String("session", id))
		return session.Session{}
	}
	if sess.Status() != session.StatusCreating {
		// Rebuilding a session that already reached a terminal or READY state
		// would recreate a workspace for a session whose workspace may have
		// been cleaned up. Report what it actually is instead.
		s.deps.Log.Warn("build skipped: session is not CREATING, no pipeline was run",
			slog.String("session", id),
			slog.String("status", string(sess.Status())))
		return sess
	}
	// live is the pipeline's running state. build updates it in place as each
	// stage lands, so a failure (or a panic) records the terminal status on
	// the session as it actually is — base SHA, resolved changes and stage
	// included (FR-8.3) — rather than rolling back to this pre-build snapshot.
	live := sess
	defer func() {
		if r := recover(); r != nil {
			s.deps.Log.Error("build panicked", slog.String("session", id), slog.Any("panic", r))
			final = s.finishWithError(live, &session.ReviewError{
				Code:    session.CodeGitFailure,
				Message: MsgGitFailure(),
			})
		}
	}()
	built, err := s.build(ctx, &live)
	if err != nil {
		return s.finishWithError(live, s.classify(ctx, id, err))
	}
	return built
}

// classify maps a build error onto the review error code that is recorded on
// the session. Cancellation is checked first and deliberately outranks the
// generic git failure the dying git process would otherwise produce: a build
// cut short by shutdown (or by the build ceiling) is a classified outcome
// with its own code, and recording it as FAILED/GIT_FAILURE would also take
// the session out of CREATING, permanently denying LoadAll the chance to mark
// it INTERRUPTED on the next startup (FR-8.6, design §8.3).
func (s *Service) classify(ctx context.Context, id string, err error) *session.ReviewError {
	if isCancellation(ctx, err) {
		s.deps.Log.Info("build interrupted", slog.String("session", id))
		return &session.ReviewError{Code: session.CodeInterrupted, Message: MsgInterrupted()}
	}
	if re := asReviewError(err); re != nil {
		return re
	}
	// An error that is not already classified is an internal build failure.
	// Log the real reason (never surfaced to the client) so the generic code
	// does not hide what happened.
	s.deps.Log.Error("build failed", slog.String("session", id), slog.String("error", err.Error()))
	return &session.ReviewError{Code: session.CodeGitFailure, Message: MsgGitFailure()}
}

// isCancellation reports whether the build's context was cut short. Both the
// context state and the error chain are inspected: a git process killed by
// cancellation reports its own exit error, which carries no context sentinel.
func isCancellation(ctx context.Context, err error) bool {
	return ctx.Err() != nil ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}

// persist writes a session through the store. A persistence failure is
// returned to the caller rather than swallowed: the store's index is only
// updated by a successful Save, so a silent failure would leave clients
// observing a stale status forever.
func (s *Service) persist(sess session.Session) error {
	if err := s.deps.Store.Save(sess); err != nil {
		s.deps.Log.Error("persist session failed",
			slog.String("session", sess.ID()),
			slog.String("error", err.Error()))
		return fmt.Errorf("review: persist session %s: %w", sess.ID(), err)
	}
	return nil
}

// progress persists an intermediate pipeline state. Unlike a terminal
// transition, a stage marker that failed to reach disk cannot mislead a
// client — the session stays CREATING either way — so the build continues
// after logging (persist already logs at Error level). Terminal transitions
// never use this helper.
func (s *Service) progress(sess session.Session) session.Session {
	_ = s.persist(sess)
	return sess
}

// finishWithError records CONFLICTED for conflicts and FAILED otherwise. The
// returned session always carries re, so a caller that ignores the status
// still sees the failure — unless the session already left the active states
// while the build was running, in which case the stored record wins (see
// terminalWins) and is returned as itself.
func (s *Service) finishWithError(sess session.Session, re *session.ReviewError) session.Session {
	if re.Diagnostics == nil {
		re.Diagnostics = &session.Diagnostics{
			WorkspacePath: s.deps.Workspaces.RepoDir(sess.ID()),
			Branch:        s.deps.Workspaces.BranchName(sess.ID()),
		}
	}
	var terminal session.Session
	if re.Code == session.CodeConflict {
		s.deps.Log.Info("review conflicted", slog.String("session", sess.ID()), slog.Int("change", re.Change))
		terminal = sess.Conflicted(re, s.deps.Now())
	} else {
		s.deps.Log.Info("review failed", slog.String("session", sess.ID()), slog.String("code", string(re.Code)))
		terminal = sess.Failed(re, s.deps.Now())
	}
	// Re-read immediately before the write, not earlier: a Finish or expiry
	// that landed mid-build has already deleted this session's workspace, so
	// a status derived from the build's own copy would resurrect it.
	if stored, dropped := s.terminalWins(sess.ID(), string(re.Code)); dropped {
		return stored
	}
	// The terminal status is the thing that must survive; if it cannot be
	// persisted the value is still returned (so the synchronous caller sees
	// the real outcome) and the failure is logged by persist. A restart then
	// recovers the session as INTERRUPTED rather than resurrecting it.
	_ = s.persist(terminal)
	return terminal
}

// terminalWins re-reads the authoritative record and reports whether the
// session left the active states (FINISHED or EXPIRED) while the build was
// running. A build holds its own copy of the session for the whole pipeline,
// so every transition it computes is derived from a value that may already be
// stale; Session's own terminal guards inspect that stale copy and therefore
// cannot see a concurrent Finish. DELETE /api/reviews/{id} is allowed on a
// CREATING session, so this is reachable in normal use: without the re-read,
// a build finishing just after a discard writes READY (or FAILED) over
// FINISHED and Store.Save re-creates the session directory that Cleanup had
// just removed, leaving a review that reports READY forever while pointing at
// a deleted worktree.
//
// The stored value is returned so the caller reports the session as it truly
// is rather than as a benign-looking value of its own making.
func (s *Service) terminalWins(id, reason string) (session.Session, bool) {
	stored, ok := s.deps.Store.Get(id)
	if !ok || stored.IsActive() {
		return session.Session{}, false
	}
	s.deps.Log.Info("session reached a terminal state during build; transition dropped",
		slog.String("session", id),
		slog.String("status", string(stored.Status())),
		slog.String("dropped", reason))
	return stored, true
}

// build is the pipeline proper. Every failure is returned as an error —
// classified as a *session.ReviewError wherever the cause is known — and
// never as a partially-populated session with a nil error.
//
// live is the caller's running copy of the session and is updated in place as
// each stage lands, so the caller can record a terminal status on the session
// as it actually is (base SHA, resolved changes, stage) instead of on the
// pre-build snapshot. It is only ever touched from the build's own goroutine.
func (s *Service) build(ctx context.Context, live *session.Session) (session.Session, error) {
	p, ok := s.deps.Providers.Get(live.ProviderID())
	if !ok {
		return *live, &session.ReviewError{
			Code:    session.CodeProviderUnavailable,
			Message: MsgProviderUnavailable(live.ProviderID()),
		}
	}
	repo, err := p.GetRepository(ctx, live.Repository())
	if err != nil {
		if re := MapProviderError(p.ID(), live.Repository(), err); re != nil {
			return *live, re
		}
		return *live, &session.ReviewError{
			Code:    session.CodeRepositoryUnavailable,
			Message: MsgRepositoryUnavailable(live.Repository()),
		}
	}

	report := func(stage string) { *live = s.progress(live.WithStage(stage, s.deps.Now())) }
	resolved, err := s.resolver.Resolve(ctx, p, repo, live.BaseBranch(), live.RequestedChanges(), report)
	if err != nil {
		return *live, err
	}
	*live = s.progress(live.WithResolved(resolved.Changes, s.deps.Now()))
	based, err := live.WithBase(resolved.BaseSHA, s.deps.Now())
	if err != nil {
		return *live, fmt.Errorf("record base sha: %w", err)
	}
	*live = s.progress(based)

	report(session.StageCreatingWorkspace)
	repoDir, err := s.deps.Workspaces.Create(ctx, resolved.MirrorPath, live.ID(), resolved.BaseSHA)
	if err != nil {
		s.deps.Log.Error("create workspace failed",
			slog.String("session", live.ID()),
			slog.String("error", err.Error()))
		return *live, &session.ReviewError{Code: session.CodeGitFailure, Message: MsgGitFailure()}
	}

	applied := make([]int, 0, len(resolved.Changes))
	for i, rc := range resolved.Changes {
		report(fmt.Sprintf("%s%d", session.StageApplyingPrefix, rc.Number()))
		res, err := s.deps.Applicator.Apply(ctx, repoDir, live.ProviderID(), rc)
		if err != nil {
			s.deps.Log.Error("apply change failed",
				slog.String("session", live.ID()),
				slog.Int("change", rc.Number()),
				slog.String("error", err.Error()))
			return *live, &session.ReviewError{
				Code: session.CodeGitFailure, Message: MsgGitFailure(), Change: rc.Number(),
			}
		}
		switch res.Outcome {
		case OutcomeConflict:
			dependency := i > 0
			return *live, &session.ReviewError{
				Code:               session.CodeConflict,
				Message:            MsgConflict(rc.Number(), dependency),
				Change:             rc.Number(),
				Commit:             res.Commit,
				ConflictingFiles:   res.ConflictingPaths,
				AppliedChanges:     applied,
				PossibleDependency: dependency,
				Diagnostics: &session.Diagnostics{
					WorkspacePath: repoDir,
					Branch:        s.deps.Workspaces.BranchName(live.ID()),
					Strategy:      string(rc.Strategy()),
					SourceSHA:     rc.SourceSHA(),
				},
			}
		case OutcomeApplied, OutcomeEmpty:
			// An empty pick still produced a commit (--empty=keep) and is a
			// legitimate outcome: the change's content was already present.
			applied = append(applied, rc.Number())
		default:
			// An unrecognised outcome is a programming error, not a review
			// result. Fail rather than pretend the change applied.
			return *live, fmt.Errorf("apply #%d: unknown outcome %q", rc.Number(), res.Outcome)
		}
	}

	report(session.StageDiffing)
	head, err := s.head(ctx, repoDir, live.ID())
	if err != nil {
		s.deps.Log.Error("read head failed",
			slog.String("session", live.ID()),
			slog.String("error", err.Error()))
		return *live, &session.ReviewError{Code: session.CodeGitFailure, Message: MsgGitFailure()}
	}
	outPath := filepath.Join(s.deps.Workspaces.SessionDir(live.ID()), CombinedDiffFile)
	if err := diff.WriteCombined(ctx, s.deps.Runner, repoDir, resolved.BaseSHA, head, outPath); err != nil {
		s.deps.Log.Error("write combined diff failed",
			slog.String("session", live.ID()),
			slog.String("error", err.Error()))
		return *live, &session.ReviewError{Code: session.CodeGitFailure, Message: MsgGitFailure()}
	}
	files, totals, err := diff.Summarize(ctx, s.deps.Runner, repoDir, resolved.BaseSHA, head)
	if err != nil {
		s.deps.Log.Error("summarize diff failed",
			slog.String("session", live.ID()),
			slog.String("error", err.Error()))
		return *live, &session.ReviewError{Code: session.CodeGitFailure, Message: MsgGitFailure()}
	}
	// READY is a transition out of CREATING computed from this build's own
	// copy, so it must not be written if the session was discarded (or
	// expired) meanwhile: its workspace is gone and Save would re-create the
	// directory. Re-read the authoritative record first.
	if _, ok := s.deps.Store.Get(live.ID()); !ok {
		return *live, fmt.Errorf("session %s disappeared from the store during the build", live.ID())
	}
	ready, err := live.Ready(head, files, totals, s.deps.Now())
	if err != nil {
		return *live, fmt.Errorf("mark ready: %w", err)
	}
	// Re-read immediately before the write: see terminalWins.
	if stored, dropped := s.terminalWins(live.ID(), string(session.StatusReady)); dropped {
		return stored, nil
	}
	// READY must be durable before it is reported: a client told READY whose
	// record never reached the store would poll a CREATING session forever.
	if err := s.persist(ready); err != nil {
		return *live, err
	}
	s.deps.Log.Info("review ready", slog.String("session", ready.ID()), slog.Int("files", totals.Files))
	return ready, nil
}

// head reads HEAD of the session workspace.
func (s *Service) head(ctx context.Context, repoDir, id string) (string, error) {
	res, err := s.deps.Runner.Run(ctx, gitx.Spec{
		Dir: repoDir, Args: []string{"rev-parse", "HEAD"}, Category: gitx.CategoryQuery, Session: id,
	})
	if err != nil {
		return "", fmt.Errorf("rev-parse HEAD: %w", err)
	}
	head := strings.TrimSpace(string(res.Stdout))
	// A blank or malformed HEAD would otherwise flow into the diff range and
	// produce a wrong diff; reject it here rather than downstream.
	if err := gitx.ValidateSHA(head); err != nil {
		return "", fmt.Errorf("rev-parse HEAD: %w", err)
	}
	return head, nil
}

// Files returns the stored summary for a READY session.
func (s *Service) Files(id string) ([]diff.FileSummary, error) {
	sess, ok := s.deps.Store.Get(id)
	if !ok {
		return nil, fmt.Errorf("review: session %s: %w", id, session.ErrNotFound)
	}
	if sess.Status() != session.StatusReady {
		return nil, fmt.Errorf("review: session %s is %s: %w", id, sess.Status(), ErrNotReady)
	}
	return sess.Files(), nil
}

// FileDiff renders one file's diff on demand.
func (s *Service) FileDiff(ctx context.Context, id, path string) (diff.FileDiff, error) {
	sess, ok := s.deps.Store.Get(id)
	if !ok {
		return diff.FileDiff{}, fmt.Errorf("review: session %s: %w", id, session.ErrNotFound)
	}
	if sess.Status() != session.StatusReady {
		return diff.FileDiff{}, fmt.Errorf("review: session %s is %s: %w", id, sess.Status(), ErrNotReady)
	}
	for _, f := range sess.Files() {
		if f.Path == path {
			fd, err := diff.FileContent(ctx, s.deps.Runner, s.deps.Workspaces.RepoDir(id), sess.BaseSHA(), sess.HeadSHA(), f)
			if err != nil {
				return diff.FileDiff{}, fmt.Errorf("review: file diff %s: %w", id, err)
			}
			return fd, nil
		}
	}
	return diff.FileDiff{}, fmt.Errorf("%w: file %q is not part of this review", session.ErrNotFound, path)
}

// CombinedDiffPath returns the on-disk combined.diff for a READY session.
func (s *Service) CombinedDiffPath(id string) (string, error) {
	sess, ok := s.deps.Store.Get(id)
	if !ok {
		return "", fmt.Errorf("review: session %s: %w", id, session.ErrNotFound)
	}
	if sess.Status() != session.StatusReady {
		return "", fmt.Errorf("review: session %s is %s: %w", id, sess.Status(), ErrNotReady)
	}
	p := filepath.Join(s.deps.Workspaces.SessionDir(id), CombinedDiffFile)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("%w: combined diff missing for session %s", session.ErrNotFound, id)
	}
	return p, nil
}
