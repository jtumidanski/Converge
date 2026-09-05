package main

import (
	"errors"
	"testing"

	"github.com/jtumidanski/converge/internal/review"
	"github.com/jtumidanski/converge/internal/session"
)

func TestParseChanges(t *testing.T) {
	got, err := parseChanges("421,427, 435")
	if err != nil || len(got) != 3 || got[0] != 421 || got[2] != 435 {
		t.Fatalf("got %v err %v", got, err)
	}
	for _, bad := range []string{"", "a,b", "1,,2", "0", "-1"} {
		if _, err := parseChanges(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestExitCodeFor(t *testing.T) {
	cases := []struct {
		name string
		s    session.Status
		code session.Code
		want int
	}{
		{"ready", session.StatusReady, "", 0},
		{"conflict", session.StatusConflicted, session.CodeConflict, 2},
		{"not merged", session.StatusFailed, session.CodeNotMerged, 3},
		{"targets", session.StatusFailed, session.CodeIncompatibleTargets, 3},
		{"not on base", session.StatusFailed, session.CodeNotOnBaseBranch, 3},
		{"missing commits", session.StatusFailed, session.CodeMissingCommits, 3},
		{"base undetermined", session.StatusFailed, session.CodeBaseUndetermined, 3},
		{"provider auth", session.StatusFailed, session.CodeProviderAuth, 4},
		{"provider unavailable", session.StatusFailed, session.CodeProviderUnavailable, 4},
		{"repository", session.StatusFailed, session.CodeRepositoryUnavailable, 4},
		{"git", session.StatusFailed, session.CodeGitFailure, 1},
		{"interrupted", session.StatusFailed, session.CodeInterrupted, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCodeFor(tc.s, tc.code); got != tc.want {
				t.Errorf("got %d want %d", got, tc.want)
			}
		})
	}
	if got := exitCodeForError(&review.InputError{Code: review.CodeInvalidChanges}); got != 3 {
		t.Errorf("input error = %d", got)
	}
	if got := exitCodeForError(&session.ReviewError{Code: session.CodeProviderAuth}); got != 4 {
		t.Errorf("review error = %d", got)
	}
	if got := exitCodeForError(errors.New("boom")); got != 1 {
		t.Errorf("plain error = %d", got)
	}
}
