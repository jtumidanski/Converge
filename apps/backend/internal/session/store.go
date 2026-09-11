package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/jtumidanski/converge/internal/identity"
	"github.com/jtumidanski/converge/internal/workspace"
)

const recordFile = "session.json"

// Cleaner removes a session's git state and directory.
type Cleaner interface {
	Cleanup(ctx context.Context, s Session) error
	RemoveDir(ctx context.Context, id string) error
}

// Store persists sessions under WORKSPACE_ROOT with an in-memory index.
type Store struct {
	root    string
	ttl     time.Duration
	cleaner Cleaner
	log     *slog.Logger
	now     func() time.Time
	mu      sync.RWMutex
	index   map[string]Session
	corrupt map[string]error
	// pendingCleanup tracks ids whose terminal status is durable (index and,
	// on the failure path, disk) but whose Cleanup call failed, so their
	// workspace directory may still exist. Sweep retries these on every pass
	// until Cleanup succeeds or maxCleanupRetries is reached (see
	// retryCleanup), at which point the id is dropped and no further
	// automatic retry happens. The map only ever holds one entry per
	// currently-failing id, so it cannot grow unbounded.
	pendingCleanup map[string]int
	// onWriteRecord, when non-nil, is called at the top of writeRecord with
	// the id being written. It exists so a test can observe the state of the
	// store *during* the write — specifically, that SaveActive holds the
	// store's lock across it — instead of trying to hit that window with a
	// sleep. It is never set outside tests (nothing exported can set it).
	onWriteRecord func(id string)
}

// maxCleanupRetries bounds how many times Sweep will retry a failed Cleanup
// for the same session before giving up on it. This guarantees a
// permanently failing Cleanup cannot spin forever or grow pendingCleanup
// without bound.
const maxCleanupRetries = 5

// NewStore creates a store; root must already exist.
func NewStore(root string, ttl time.Duration, cleaner Cleaner, log *slog.Logger, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{root: root, ttl: ttl, cleaner: cleaner, log: log, now: now, index: map[string]Session{}, corrupt: map[string]error{}, pendingCleanup: map[string]int{}}
}

func (s *Store) Root() string         { return s.root }
func (s *Store) Dir(id string) string { return filepath.Join(s.root, id) }

// Save writes session.json atomically (temp + fsync + rename) and updates the index.
func (s *Store) Save(sess Session) error {
	if err := s.writeRecord(sess); err != nil {
		return err
	}
	s.mu.Lock()
	s.index[sess.ID()] = sess
	delete(s.corrupt, sess.ID()) // a successful write means this id is no longer corrupt
	s.mu.Unlock()
	return nil
}

// SaveActive is Save guarded by a compare-and-swap on the stored status: the
// write happens only if the indexed session for this id is still active, and
// the check plus the write happen under a single acquisition of the store's
// lock, so there is no window in which another mutator can slip a terminal
// transition between them.
//
// This is what a build's terminal writes (READY/FAILED/CONFLICTED) must use.
// A build holds its own copy of the session for the whole pipeline, so every
// transition it computes derives from a value that may already be stale, and
// Session's own terminal guards inspect that stale copy and therefore cannot
// see a concurrent Finish. DELETE /api/reviews/{id} is allowed on a CREATING
// session, so this is reachable in normal use: with a plain Save, a build
// finishing just after a discard writes READY over FINISHED and re-creates
// the session directory Cleanup had just removed, leaving a review that
// reports READY forever while pointing at a deleted worktree.
//
// Returns:
//   - (sess, nil) when the write happened;
//   - (stored, ErrTerminal) when the stored session is FINISHED or EXPIRED —
//     nothing was written, and the stored value is returned so the caller can
//     report the session as it truly is;
//   - (zero, ErrNotFound) when the id is not in the index at all: nothing in
//     the store ever deletes an indexed id (a discard marks it FINISHED, it
//     does not remove it), so this only happens for an id that was never
//     saved — writing would create a record for a session this store has
//     never seen;
//   - (zero, err) when the write itself failed.
//
// The disk write is deliberately performed while the lock is held. Splitting
// it out would reopen exactly the check/write gap this method exists to
// close, and the cost is bounded: a build makes a handful of guarded writes
// (its intermediate stage markers plus one terminal transition) and
// concurrent builds are capped by MAX_CONCURRENT_BUILDS, so the lock is held
// only for small fsync+rename operations, never for git or network work
// (Cleanup still runs outside the lock — see Finish).
func (s *Store) SaveActive(sess Session) (Session, error) {
	id := sess.ID()
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.index[id]
	if !ok {
		return Session{}, fmt.Errorf("session %s: %w", id, ErrNotFound)
	}
	if !stored.IsActive() {
		return stored, fmt.Errorf("session %s is %s: %w", id, stored.Status(), ErrTerminal)
	}
	if err := s.writeRecord(sess); err != nil {
		return Session{}, err
	}
	s.index[id] = sess
	delete(s.corrupt, id)
	return sess, nil
}

// writeRecord writes session.json atomically (temp + fsync + rename). It
// takes no locks so it can be called either standalone (Save) or with the
// store's lock already held (SaveActive).
func (s *Store) writeRecord(sess Session) error {
	if s.onWriteRecord != nil {
		s.onWriteRecord(sess.ID())
	}
	dir := s.Dir(sess.ID())
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("session: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(ToRecord(sess), "", "  ")
	if err != nil {
		return fmt.Errorf("session: marshal: %w", err)
	}
	tmp, err := os.CreateTemp(dir, recordFile+".*.tmp")
	if err != nil {
		return fmt.Errorf("session: temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("session: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("session: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("session: close: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(dir, recordFile)); err != nil {
		return fmt.Errorf("session: rename: %w", err)
	}
	return nil
}

// Get returns the session if it exists and is visible to scope. A session
// owned by another user, or an unowned session in hosted mode, is reported
// exactly as a session that does not exist — which is what makes a cross-user
// request answer 404 rather than 403 (FR-4.2), through the existing
// ErrNotFound -> 404 mapping, with no handler making that decision.
//
// Its bool return only ever means "this id is currently in the live index
// and visible to scope" — it deliberately cannot distinguish "id never
// existed" from "id existed but LoadAll found its session.json unreadable or
// invalid". That distinction is not lost, though: LoadAll records every such
// id (see Corrupted), so a caller building a 404 vs. 500 response can check
// Corrupted(id) after a failed Get without any change to this signature.
func (s *Store) Get(id string, scope identity.Scope) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.index[id]
	if !ok || !scope.Matches(sess.Owner()) {
		return Session{}, false
	}
	return sess, true
}

// Corrupted reports whether id was found, at the most recent LoadAll, to
// have an unreadable or invalid session.json — i.e. whether a subsequent
// Get(id) returning false means "was corrupt" rather than "never existed".
// It is cleared for an id the moment that id is next written successfully
// via Save (including LoadAll's own rewrite of a recovered CREATING
// session), so it never reports a stale corruption once the id is healthy
// again.
func (s *Store) Corrupted(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.corrupt[id]
	return ok
}

// List returns the active sessions visible to scope, newest first.
func (s *Store) List(scope identity.Scope) []Session {
	s.mu.RLock()
	out := make([]Session, 0, len(s.index))
	for _, sess := range s.index {
		if sess.IsActive() && scope.Matches(sess.Owner()) {
			out = append(out, sess)
		}
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt().After(out[j].CreatedAt()) })
	return out
}

// Finish marks FINISHED and cleans up. Unknown or already finished sessions
// are no-ops. The terminal status is written to the index before Cleanup
// runs (not after) so that a concurrent Get/List can never observe this
// session as still active while its workspace is being deleted underneath
// it. Cleanup is not run under the store's lock: it does filesystem and git
// work and would otherwise serialise the whole store behind it. If Cleanup
// fails, the FINISHED status is already durable in the index — the error is
// still returned so the caller can log/handle the cleanup failure, but it no
// longer leaves the session stuck non-terminal. On a Cleanup failure the
// terminal record is also persisted to the surviving session.json (see
// persistCleanupFailure) and the id is registered for retry on a later
// Sweep, so a permanently-successful-on-retry Cleanup eventually still
// removes the workspace, and a restart before that happens sees the true
// FINISHED status rather than a stale active one.
func (s *Store) Finish(ctx context.Context, id string, scope identity.Scope) error {
	sess, ok := s.Get(id, scope)
	if !ok || sess.Status() == StatusFinished {
		return nil
	}
	finished := sess.Finished(s.now())
	s.mu.Lock()
	s.index[id] = finished
	s.mu.Unlock()
	if err := s.cleaner.Cleanup(ctx, sess); err != nil {
		s.log.Warn("finish cleanup failed", slog.String("session", id), slog.String("error", err.Error()))
		s.persistCleanupFailure(id, finished)
		return fmt.Errorf("session %s: cleanup: %w", id, err)
	}
	return nil
}

// Unowned reports how many indexed sessions carry no owner. In hosted mode
// these are invisible to every user (FR-6.3); app.New logs the count once at
// startup so an operator can clean them up by hand. They are never swept
// early and never reassigned.
func (s *Store) Unowned() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, sess := range s.index {
		if sess.Owner() == "" {
			n++
		}
	}
	return n
}

// Purge removes every session owned by userID — active or terminal — along
// with its workspace. This is the account-deletion path (FR-2.7) and is
// deliberately not scope-checked: the caller has already verified the
// account's password. Errors from individual cleanups are joined rather than
// short-circuited, so one stuck workspace does not strand the rest.
//
// Cleanup already removes the session directory for both active and terminal
// sessions (workspace.Manager.Cleanup unconditionally os.RemoveAlls it), so a
// separate RemoveDir call would be redundant.
func (s *Store) Purge(ctx context.Context, userID string) error {
	if userID == "" {
		return errors.New("session: purge requires a user id")
	}
	s.mu.Lock()
	ids := make([]string, 0)
	victims := make([]Session, 0)
	for id, sess := range s.index {
		if sess.Owner() == userID {
			ids = append(ids, id)
			victims = append(victims, sess)
		}
	}
	for _, id := range ids {
		delete(s.index, id)
		delete(s.corrupt, id)
		delete(s.pendingCleanup, id)
	}
	s.mu.Unlock()

	// Cleanup runs outside the lock: it does filesystem and git work and
	// would otherwise serialise the whole store behind it, matching Finish.
	var errs []error
	for _, sess := range victims {
		if err := s.cleaner.Cleanup(ctx, sess); err != nil {
			errs = append(errs, fmt.Errorf("session %s: %w", sess.ID(), err))
		}
	}
	return errors.Join(errs...)
}

// expire marks EXPIRED and cleans up. The terminal status is written to the
// index before Cleanup runs so a concurrent Get/List never observes an
// about-to-be-wiped session as still active; Cleanup runs outside the lock
// so it doesn't serialise the store. On success the session directory
// (including session.json) is gone, so nothing further is persisted — the
// terminal status lives only in memory, same as before. On failure the
// directory survives holding a stale, non-terminal record; persistCleanupFailure
// writes the true EXPIRED status to that surviving session.json and
// registers the id for retry on a later Sweep.
func (s *Store) expire(ctx context.Context, sess Session) {
	expired := sess.Expired(s.now())
	s.mu.Lock()
	s.index[sess.ID()] = expired
	s.mu.Unlock()
	if err := s.cleaner.Cleanup(ctx, sess); err != nil {
		s.log.Warn("expire cleanup failed", slog.String("session", sess.ID()), slog.String("error", err.Error()))
		s.persistCleanupFailure(sess.ID(), expired)
	}
}

// persistCleanupFailure writes the terminal record to the surviving
// session.json (via the existing atomic Save path) and registers id for
// retry on a later Sweep. It is only called after Cleanup has already
// failed, i.e. only on the path where the session's directory is known to
// still exist. If Save itself fails, that is logged too — the in-memory
// terminal status (already written by the caller) is never lost or rolled
// back either way.
func (s *Store) persistCleanupFailure(id string, terminal Session) {
	if err := s.Save(terminal); err != nil {
		s.log.Warn("failed to persist terminal status after cleanup failure", slog.String("session", id), slog.String("error", err.Error()))
	}
	s.mu.Lock()
	if _, ok := s.pendingCleanup[id]; !ok {
		s.pendingCleanup[id] = 0
	}
	s.mu.Unlock()
}

// retryCleanup re-attempts Cleanup for a session whose prior Cleanup failed.
// It never touches the session's status: the id was already terminal in the
// index (and on disk) before this is ever called, so a retry — successful
// or not — cannot make the session visible as active again. Retries are
// bounded by maxCleanupRetries: once reached, the id is dropped from
// pendingCleanup and no further automatic retry happens (the failure is
// logged at Error level so it isn't silently lost).
func (s *Store) retryCleanup(ctx context.Context, sess Session) {
	id := sess.ID()
	if err := s.cleaner.Cleanup(ctx, sess); err != nil {
		s.mu.Lock()
		s.pendingCleanup[id]++
		attempts := s.pendingCleanup[id]
		s.mu.Unlock()
		if attempts >= maxCleanupRetries {
			s.log.Error("giving up on session cleanup after repeated failures", slog.String("session", id), slog.Int("attempts", attempts), slog.String("error", err.Error()))
			s.mu.Lock()
			delete(s.pendingCleanup, id)
			s.mu.Unlock()
			return
		}
		s.log.Warn("retrying cleanup failed", slog.String("session", id), slog.Int("attempts", attempts), slog.String("error", err.Error()))
		return
	}
	s.mu.Lock()
	delete(s.pendingCleanup, id)
	s.mu.Unlock()
}

// LoadAll implements FR-8.6 startup recovery.
//
// A session directory can be in one of these shapes on restart:
//   - a valid, parseable session.json: loaded, and if its status was
//     CREATING (the process died mid-build, before Ready/Conflicted/Failed
//     could be recorded) it is deliberately failed with CodeInterrupted so
//     the operator sees an explicit failure rather than a session stuck
//     forever in CREATING. The corrected record is rewritten to disk before
//     being indexed, so a second restart doesn't need to redo this.
//   - unreadable or corrupt (missing file, invalid JSON, failed validation):
//     this must never be silently dropped or treated as "absent". It is
//     logged as a warning, and the id is recorded in s.corrupt so that
//     Get(id) returning false can be distinguished from "id never existed"
//     via Corrupted(id) for the remainder of the process's life (or until a
//     later Save for that id succeeds). If the directory is older than the
//     store's TTL, it is very unlikely to ever recover (nothing will ever
//     finish writing to it) so the cleaner removes it. If it is younger
//     than the TTL, it is left alone in case a concurrent writer is
//     mid-Save; a future restart will reconsider it.
//   - not a session directory at all (fails ValidateSessionID, e.g. a stray
//     file or a directory this store doesn't own): skipped entirely, never
//     touched.
//
// After reconciling every entry, an immediate Sweep expires anything already
// past its TTL (including sessions that were just marked CREATING->FAILED
// but whose expiry had also already passed).
func (s *Store) LoadAll(ctx context.Context) error {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return fmt.Errorf("session: read root: %w", err)
	}
	now := s.now()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		// #nosec G304 -- id comes from os.ReadDir(s.root) itself, not external input.
		raw, readErr := os.ReadFile(filepath.Join(s.root, id, recordFile))
		var rec Record
		if readErr == nil {
			readErr = json.Unmarshal(raw, &rec)
		}
		var sess Session
		if readErr == nil {
			sess, readErr = FromRecord(rec)
		}
		if readErr != nil {
			if workspace.ValidateSessionID(id) != nil {
				continue // not ours
			}
			s.log.Warn("unreadable or corrupt session record", slog.String("session", id), slog.String("error", readErr.Error()))
			s.mu.Lock()
			s.corrupt[id] = readErr
			s.mu.Unlock()
			info, statErr := e.Info()
			if statErr == nil && now.Sub(info.ModTime()) > s.ttl {
				s.log.Warn("removing invalid session directory", slog.String("session", id))
				if err := s.cleaner.RemoveDir(ctx, id); err != nil {
					s.log.Warn("remove failed", slog.String("session", id), slog.String("error", err.Error()))
				}
			}
			continue
		}
		if sess.Status() == StatusCreating {
			sess = sess.Failed(&ReviewError{Code: CodeInterrupted, Message: "The review was interrupted by a server restart before it finished building."}, now)
			if err := s.Save(sess); err != nil {
				return err
			}
		}
		s.mu.Lock()
		s.index[id] = sess
		if !sess.IsActive() {
			// A terminal (FINISHED/EXPIRED) session whose directory still
			// exists on disk can only mean a prior Cleanup failed before the
			// process restarted (a successful Cleanup removes the directory
			// entirely). Register it for retry so Sweep picks the cleanup
			// back up instead of leaking the workspace forever.
			if _, ok := s.pendingCleanup[id]; !ok {
				s.pendingCleanup[id] = 0
			}
		}
		s.mu.Unlock()
	}
	s.Sweep(ctx)
	return nil
}
