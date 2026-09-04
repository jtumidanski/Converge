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
}

// NewStore creates a store; root must already exist.
func NewStore(root string, ttl time.Duration, cleaner Cleaner, log *slog.Logger, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{root: root, ttl: ttl, cleaner: cleaner, log: log, now: now, index: map[string]Session{}, corrupt: map[string]error{}}
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

// Finish cleans up and marks FINISHED. Unknown or already finished sessions are no-ops.
func (s *Store) Finish(ctx context.Context, id string) error {
	sess, ok := s.Get(id)
	if !ok || sess.Status() == StatusFinished {
		return nil
	}
	if err := s.cleaner.Cleanup(ctx, sess); err != nil {
		return fmt.Errorf("session %s: cleanup: %w", id, err)
	}
	s.mu.Lock()
	s.index[id] = sess.Finished(s.now())
	s.mu.Unlock()
	return nil
}

// expire cleans up and marks EXPIRED (in memory only; the directory is gone).
func (s *Store) expire(ctx context.Context, sess Session) {
	if err := s.cleaner.Cleanup(ctx, sess); err != nil {
		s.log.Warn("expire cleanup failed", slog.String("session", sess.ID()), slog.String("error", err.Error()))
	}
	s.mu.Lock()
	s.index[sess.ID()] = sess.Expired(s.now())
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
		s.mu.Unlock()
	}
	s.Sweep(ctx)
	return nil
}
