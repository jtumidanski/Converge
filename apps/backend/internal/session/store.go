package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

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
	s.mu.Lock()
	s.index[sess.ID()] = sess
	delete(s.corrupt, sess.ID()) // a successful write means this id is no longer corrupt
	s.mu.Unlock()
	return nil
}

// Get returns the indexed session. Its bool return only ever means "this id
// is currently in the live index" — it deliberately cannot distinguish "id
// never existed" from "id existed but LoadAll found its session.json
// unreadable or invalid". That distinction is not lost, though: LoadAll
// records every such id (see Corrupted), so a caller building a 404 vs. 500
// response can check Corrupted(id) after a failed Get without any change to
// this signature.
func (s *Store) Get(id string) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.index[id]
	return sess, ok
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

// List returns active sessions, newest first.
func (s *Store) List() []Session {
	s.mu.RLock()
	out := make([]Session, 0, len(s.index))
	for _, sess := range s.index {
		if sess.IsActive() {
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
func (s *Store) Finish(ctx context.Context, id string) error {
	sess, ok := s.Get(id)
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
