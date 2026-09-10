// Package gitx executes git with strict argument validation and credential isolation.
package gitx

import (
	"context"
	"fmt"
	"io"
	"time"
)

// Category labels a git invocation for logging and default timeouts.
type Category string

const (
	CategoryClone      Category = "clone"
	CategoryFetch      Category = "fetch"
	CategoryWorktree   Category = "worktree"
	CategoryCherryPick Category = "cherry-pick"
	CategoryDiff       Category = "diff"
	CategoryCleanup    Category = "cleanup"
	CategoryQuery      Category = "query"
)

// Spec describes one git invocation. Args never include "git".
type Spec struct {
	Dir      string
	Args     []string
	Env      []string
	Stdin    io.Reader
	Category Category
	Timeout  time.Duration
	Repo     string
	Session  string
}

// Result is the captured outcome of an invocation.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Duration time.Duration
}

// ExitError is returned when git exits non-zero.
type ExitError struct {
	Category Category
	Result   Result
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("git %s exited with code %d", e.Category, e.Result.ExitCode)
}

// IsExit reports whether err is an ExitError with the given code.
func IsExit(err error, code int) bool {
	var ee *ExitError
	if !asExit(err, &ee) {
		return false
	}
	return ee.Result.ExitCode == code
}

// Runner executes git.
type Runner interface {
	Run(ctx context.Context, s Spec) (Result, error)
}
