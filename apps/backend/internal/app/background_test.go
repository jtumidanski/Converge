package app

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// Close removes the git runner's shared HOME and hooks directories, so it must
// not return while background work (builds, the sweeper) is still using them.
func TestCloseWaitsForBackgroundWork(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(1)
	a := &App{Background: wg}

	var stopped boolFlag
	go func() {
		time.Sleep(100 * time.Millisecond)
		stopped.set()
		wg.Done()
	}()
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !stopped.get() {
		t.Error("Close returned before background work stopped")
	}
}

// A drain that never finishes must not hang shutdown forever, and must not be
// reported as a clean close either.
func TestCloseReportsDrainTimeout(t *testing.T) {
	original := backgroundDrainTimeout
	backgroundDrainTimeout = 20 * time.Millisecond
	t.Cleanup(func() { backgroundDrainTimeout = original })

	wg := &sync.WaitGroup{}
	wg.Add(1)
	t.Cleanup(wg.Done) // let waitTimeout's helper goroutine exit
	a := &App{Background: wg}

	err := a.Close()
	if !errors.Is(err, ErrBackgroundDrainTimeout) {
		t.Fatalf("Close err = %v, want ErrBackgroundDrainTimeout", err)
	}
}

// boolFlag is a tiny race-free boolean flag.
type boolFlag struct {
	mu sync.Mutex
	v  bool
}

func (a *boolFlag) set() { a.mu.Lock(); a.v = true; a.mu.Unlock() }
func (a *boolFlag) get() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.v
}
