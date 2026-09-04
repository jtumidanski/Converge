package gitx

import (
	"context"
	"errors"
	"sync"
)

func asExit(err error, target **ExitError) bool { return errors.As(err, target) }

// FakeRunner records specs and returns canned results. Safe for concurrent use.
type FakeRunner struct {
	Handler func(Spec) (Result, error)
	mu      sync.Mutex
	Calls   []Spec
}

// Run records the spec and delegates to Handler (or returns an empty success).
func (f *FakeRunner) Run(_ context.Context, s Spec) (Result, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, s)
	f.mu.Unlock()
	if f.Handler == nil {
		return Result{}, nil
	}
	return f.Handler(s)
}

// Reset clears recorded calls.
func (f *FakeRunner) Reset() {
	f.mu.Lock()
	f.Calls = nil
	f.mu.Unlock()
}

// Snapshot returns a copy of recorded calls.
func (f *FakeRunner) Snapshot() []Spec {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Spec, len(f.Calls))
	copy(out, f.Calls)
	return out
}
