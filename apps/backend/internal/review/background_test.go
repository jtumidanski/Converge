package review

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/session"
)

// A build keeps running git after the HTTP server has drained. Shutdown
// removes the git runner's shared HOME and hooks directories, so StartBuild's
// goroutine must be registered on Deps.Background: without it, Wait returns
// immediately and the directories are deleted under a live git process.
func TestStartBuildIsTrackedByBackgroundWaitGroup(t *testing.T) {
	f := newServiceFixture(t)
	s := f.create(t)

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	f.app.set(func(context.Context, session.ResolvedChange) {
		once.Do(func() { close(entered) })
		<-release
	}, nil)

	f.svc.StartBuild(context.Background(), s.ID())
	select {
	case <-entered:
	case <-time.After(60 * time.Second):
		close(release)
		t.Fatal("build never reached the applicator")
	}

	waited := make(chan struct{})
	go func() {
		defer close(waited)
		f.svc.deps.Background.Wait()
	}()
	select {
	case <-waited:
		close(release)
		t.Fatal("Background.Wait returned while a build was still running: shutdown would delete the git directories out from under it")
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	select {
	case <-waited:
	case <-time.After(60 * time.Second):
		t.Fatal("Background.Wait did not return after the build finished")
	}
}
