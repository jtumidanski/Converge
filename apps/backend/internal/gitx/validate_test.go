package gitx

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

func TestValidateSHA(t *testing.T) {
	good := strings.Repeat("a", 40)
	if err := ValidateSHA(good); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "abc", strings.Repeat("A", 40), strings.Repeat("a", 41), "-" + good[1:]} {
		if ValidateSHA(bad) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestValidateRepoFullName(t *testing.T) {
	for _, good := range []string{"owner/repo", "group/sub/project", "a.b/c-d_e"} {
		if err := ValidateRepoFullName(good); err != nil {
			t.Errorf("%q rejected: %v", good, err)
		}
	}
	for _, bad := range []string{"", "repo", "/owner/repo", "-owner/repo", ".owner/repo", "owner/../repo", "owner/./repo", "owner//repo", "owner/repo/", "owner/repo name", "owner/repo\x00", "owner/re\npo", "../x/y"} {
		if ValidateRepoFullName(bad) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func FuzzValidateRepoFullName(f *testing.F) {
	f.Add("owner/repo")
	f.Add("../x/y")
	re := regexp.MustCompile(`^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)+$`)
	f.Fuzz(func(t *testing.T, s string) {
		err := ValidateRepoFullName(s)
		if err == nil {
			if !re.MatchString(s) || strings.Contains(s, "..") || strings.HasPrefix(s, "-") || strings.HasPrefix(s, ".") || strings.HasPrefix(s, "/") {
				t.Fatalf("accepted invalid %q", s)
			}
			for _, seg := range strings.Split(s, "/") {
				if seg == "." || seg == ".." || seg == "" {
					t.Fatalf("accepted bad segment in %q", s)
				}
			}
		}
	})
}

func TestValidateBranchSyntax(t *testing.T) {
	for _, good := range []string{"main", "release/1.2", "feat_x.y-z"} {
		if err := ValidateBranchSyntax(good); err != nil {
			t.Errorf("%q rejected: %v", good, err)
		}
	}
	for _, bad := range []string{"", "-x", "a..b", "a b", "a\tb", "a~b", "a^b", "a:b", "a?b", "a*b", "a[b", "a\\b", "a//b", "/a", "a/", "a.lock", "@", "a@{b", "a\x00"} {
		if ValidateBranchSyntax(bad) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestValidateBranchUsesGit(t *testing.T) {
	fr := &FakeRunner{Handler: func(s Spec) (Result, error) {
		if s.Args[0] != "check-ref-format" || s.Args[1] != "--branch" || s.Args[2] != "main" {
			t.Errorf("args = %v", s.Args)
		}
		return Result{}, nil
	}}
	if err := ValidateBranch(context.Background(), fr, "main"); err != nil {
		t.Fatal(err)
	}
	if len(fr.Calls) != 1 {
		t.Fatalf("calls = %d", len(fr.Calls))
	}
	fr.Reset()
	if err := ValidateBranch(context.Background(), fr, "-bad"); err == nil || len(fr.Calls) != 0 {
		t.Fatalf("leading dash must be rejected before git runs; err=%v calls=%d", err, len(fr.Calls))
	}
	fr.Handler = func(Spec) (Result, error) { return Result{ExitCode: 1}, &ExitError{Result: Result{ExitCode: 1}} }
	if err := ValidateBranch(context.Background(), fr, "odd"); err == nil {
		t.Fatal("git rejection must propagate")
	}
}

func TestValidateChangeNumbers(t *testing.T) {
	got, err := ValidateChangeNumbers([]int{5, 3, 5, 1})
	if err != nil || len(got) != 3 || got[0] != 1 || got[1] != 3 || got[2] != 5 {
		t.Fatalf("got %v err %v", got, err)
	}
	if _, err := ValidateChangeNumbers(nil); err == nil {
		t.Error("empty accepted")
	}
	if _, err := ValidateChangeNumbers([]int{0}); err == nil {
		t.Error("zero accepted")
	}
	many := make([]int, 51)
	for i := range many {
		many[i] = i + 1
	}
	if _, err := ValidateChangeNumbers(many); err == nil {
		t.Error(">50 accepted")
	}
	if ValidatePathArg("-rf") == nil || ValidatePathArg("a\x00b") == nil || ValidatePathArg("src/x.go") != nil {
		t.Error("ValidatePathArg wrong")
	}
}
