package diff

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/testutil"
)

func TestSummarizeWriteAndFileContent(t *testing.T) {
	src := testutil.NewRepo(t)
	src.Commit("mod.txt", "a\nb\n", "add mod")
	base := src.Head()
	src.Commit("mod.txt", "a\nc\nd\n", "change mod")
	src.Commit("new.txt", "hello\n", "add new")
	if err := os.MkdirAll(filepath.Join(src.Work, "docs"), 0o750); err != nil {
		t.Fatal(err)
	}
	src.Git("mv", "README.md", "docs/README.md")
	src.Git("commit", "-m", "rename")
	if err := os.WriteFile(filepath.Join(src.Work, "bin.dat"), []byte{0, 1, 2, 3, 0xff}, 0o644); err != nil {
		t.Fatal(err)
	}
	src.Git("add", "bin.dat")
	src.Git("commit", "-m", "binary")
	head := src.Head()

	runner, err := gitx.NewExecRunner(slog.New(slog.NewTextHandler(os.Stderr, nil)), gitx.Options{CommandTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	ctx := context.Background()
	files, totals, err := Summarize(ctx, runner, src.Work, base, head)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]FileSummary{}
	for _, f := range files {
		byPath[f.Path] = f
	}
	// mod.txt contributes 2 additions/1 deletion, new.txt contributes 1
	// addition; the rename is content-identical (0/0) and the binary add
	// reports "-"/"-" (0/0), per real git --numstat output. The brief's
	// illustrative test asserted Additions=4; verified against actual git
	// output this is 3 (see task report for detail).
	if totals.Files != 4 || totals.Additions != 3 || totals.Deletions != 1 {
		t.Errorf("totals = %+v", totals)
	}
	if f := byPath["mod.txt"]; f.Status != StatusModified || f.Additions != 2 || f.Deletions != 1 {
		t.Errorf("mod = %+v", f)
	}
	if f := byPath["new.txt"]; f.Status != StatusAdded || f.Additions != 1 {
		t.Errorf("new = %+v", f)
	}
	if f := byPath["docs/README.md"]; f.Status != StatusRenamed || f.PreviousPath != "README.md" {
		t.Errorf("rename = %+v", f)
	}
	if f := byPath["bin.dat"]; f.Status != StatusAdded || !f.Binary {
		t.Errorf("bin = %+v", f)
	}
	gotOrder := make([]string, len(files))
	for i, f := range files {
		gotOrder[i] = f.Path
	}
	wantOrder := []string{"bin.dat", "docs/README.md", "mod.txt", "new.txt"}
	if len(gotOrder) != len(wantOrder) {
		t.Fatalf("order = %v, want %v", gotOrder, wantOrder)
	}
	for i := range wantOrder {
		if gotOrder[i] != wantOrder[i] {
			t.Errorf("order = %v, want %v", gotOrder, wantOrder)
			break
		}
	}
	out := filepath.Join(t.TempDir(), "combined.diff")
	if err := WriteCombined(ctx, runner, src.Work, base, head, out); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(out)
	if !strings.Contains(string(b), "diff --git a/mod.txt b/mod.txt") || !strings.Contains(string(b), "rename from README.md") {
		t.Errorf("combined.diff content:\n%s", b)
	}
	fd, err := FileContent(ctx, runner, src.Work, base, head, byPath["mod.txt"])
	if err != nil || fd.Truncated || !strings.Contains(fd.Diff, "+c") || strings.Contains(fd.Diff, "new.txt") {
		t.Errorf("file diff: %v %+v", err, fd)
	}
	fd, err = FileContent(ctx, runner, src.Work, base, head, byPath["docs/README.md"])
	if err != nil || !strings.Contains(fd.Diff, "rename from README.md") {
		t.Errorf("rename diff: %v %q", err, fd.Diff)
	}
	fd, err = FileContent(ctx, runner, src.Work, base, head, byPath["bin.dat"])
	if err != nil || fd.Diff != "" || !fd.Binary {
		t.Errorf("binary diff: %v %+v", err, fd)
	}
	if _, err := FileContent(ctx, runner, src.Work, base, head, FileSummary{Path: "-bad"}); err == nil {
		t.Error("option-like path accepted")
	}
}

func TestFileContentTruncates(t *testing.T) {
	src := testutil.NewRepo(t)
	base := src.Head()
	big := strings.Repeat("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcde\n", 20000) // ~1.28 MiB
	src.Commit("big.txt", big, "big")
	runner, _ := gitx.NewExecRunner(slog.New(slog.NewTextHandler(os.Stderr, nil)), gitx.Options{CommandTimeout: 30 * time.Second})
	defer runner.Close()
	fd, err := FileContent(context.Background(), runner, src.Work, base, src.Head(), FileSummary{Path: "big.txt", Status: StatusAdded})
	// The fixture content is pure ASCII, so every byte is a rune boundary
	// and the rune-safe truncation lands exactly on MaxFileDiffBytes here;
	// TestFileContentTruncatesOnRuneBoundary below covers the case where it
	// must back off. len() is asserted with <= (not ==) since the guarantee
	// truncation makes is "never exceeds the cap," not "always hits it
	// exactly" once rune-boundary backoff is in play.
	if err != nil || !fd.Truncated || len(fd.Diff) > MaxFileDiffBytes {
		t.Fatalf("err=%v truncated=%v len=%d", err, fd.Truncated, len(fd.Diff))
	}
	if len(fd.Diff) != MaxFileDiffBytes {
		t.Fatalf("expected exact MaxFileDiffBytes for ASCII content, got %d", len(fd.Diff))
	}
}

// TestFileContentTruncatesOnRuneBoundary uses multi-byte UTF-8 content
// engineered so a naive byte-offset cut at MaxFileDiffBytes would split a
// rune. It asserts the result is valid UTF-8, never exceeds the cap, and is
// marked Truncated.
func TestFileContentTruncatesOnRuneBoundary(t *testing.T) {
	src := testutil.NewRepo(t)
	base := src.Head()
	// U+00E9 ("é") is 2 bytes in UTF-8. Repeating a 2-byte rune means a cut
	// at an odd byte offset always splits a rune; pad the prefix so the
	// unified diff header pushes the cut point into the repeated run at an
	// offset whose parity we don't control, so this reliably exercises the
	// split either way MaxFileDiffBytes lands.
	big := strings.Repeat("é", 700000)
	src.Commit("big.txt", big, "big")
	runner, _ := gitx.NewExecRunner(slog.New(slog.NewTextHandler(os.Stderr, nil)), gitx.Options{CommandTimeout: 30 * time.Second})
	defer runner.Close()
	fd, err := FileContent(context.Background(), runner, src.Work, base, src.Head(), FileSummary{Path: "big.txt", Status: StatusAdded})
	if err != nil {
		t.Fatal(err)
	}
	if !fd.Truncated {
		t.Fatal("expected truncation")
	}
	if len(fd.Diff) > MaxFileDiffBytes {
		t.Fatalf("diff exceeds cap: len=%d max=%d", len(fd.Diff), MaxFileDiffBytes)
	}
	if !utf8.ValidString(fd.Diff) {
		t.Fatalf("truncated diff is not valid UTF-8 (len=%d)", len(fd.Diff))
	}
}

// TestSummarizeSpacesAndDeletion exercises a deleted file and a path
// containing a space, neither of which the primary scenario covers. Real git
// quotes paths with unusual characters in --raw/-z output only when
// core.quotepath permits it and the byte isn't ASCII-printable; a plain
// space is emitted unquoted, so this also verifies parseRaw/parseNumstat
// don't accidentally split on it.
func TestSummarizeSpacesAndDeletion(t *testing.T) {
	src := testutil.NewRepo(t)
	src.Commit("doomed.txt", "bye\n", "add doomed")
	base := src.Head()
	src.Commit("has space.txt", "one\ntwo\n", "add spaced file")
	src.Git("rm", "doomed.txt")
	src.Git("commit", "-m", "remove doomed")
	head := src.Head()

	runner, err := gitx.NewExecRunner(slog.New(slog.NewTextHandler(os.Stderr, nil)), gitx.Options{CommandTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	ctx := context.Background()
	files, totals, err := Summarize(ctx, runner, src.Work, base, head)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]FileSummary{}
	for _, f := range files {
		byPath[f.Path] = f
	}
	if f, ok := byPath["has space.txt"]; !ok || f.Status != StatusAdded || f.Additions != 2 {
		t.Errorf("spaced file = %+v ok=%v", f, ok)
	}
	if f, ok := byPath["doomed.txt"]; !ok || f.Status != StatusDeleted || f.Deletions != 1 {
		t.Errorf("deleted file = %+v ok=%v", f, ok)
	}
	if totals.Files != 2 {
		t.Errorf("totals = %+v", totals)
	}
	fd, err := FileContent(ctx, runner, src.Work, base, head, byPath["has space.txt"])
	if err != nil || !strings.Contains(fd.Diff, "diff --git a/has space.txt b/has space.txt") {
		t.Errorf("spaced diff: %v %q", err, fd.Diff)
	}
}

const (
	fakeBaseSHA = "1111111111111111111111111111111111111111"
	fakeHeadSHA = "2222222222222222222222222222222222222222"
)

// TestSummarizeRawNumstatMismatch drives Summarize with a FakeRunner
// returning a --raw stream that names a path absent from --numstat. This
// is the raw-not-in-numstat direction of the cross-stream invariant: a
// join miss must surface as an error, not as a silently zero-filled
// FileSummary.
func TestSummarizeRawNumstatMismatch(t *testing.T) {
	fr := &gitx.FakeRunner{Handler: func(s gitx.Spec) (gitx.Result, error) {
		for _, a := range s.Args {
			if a == "--raw" {
				return gitx.Result{Stdout: []byte(":000000 100644 0000000000000000000000000000000000000000 1111111111111111111111111111111111111111 A\x00only-in-raw.txt\x00")}, nil
			}
			if a == "--numstat" {
				return gitx.Result{Stdout: []byte("1\t0\tother.txt\x00")}, nil
			}
		}
		return gitx.Result{}, nil
	}}
	_, _, err := Summarize(context.Background(), fr, "/repo", fakeBaseSHA, fakeHeadSHA)
	if err == nil {
		t.Fatal("expected error on raw/numstat mismatch")
	}
	if !strings.Contains(err.Error(), "only-in-raw.txt") {
		t.Errorf("error should name the mismatched path: %v", err)
	}
}

// TestSummarizeNumstatRawMismatch covers the converse: a path present in
// --numstat but absent from --raw. Both streams come from the same `git
// diff` invocation over the same range, so this indicates the same class
// of desynchronisation, just discovered from the other direction.
func TestSummarizeNumstatRawMismatch(t *testing.T) {
	fr := &gitx.FakeRunner{Handler: func(s gitx.Spec) (gitx.Result, error) {
		for _, a := range s.Args {
			if a == "--raw" {
				return gitx.Result{Stdout: []byte(":000000 100644 0000000000000000000000000000000000000000 1111111111111111111111111111111111111111 A\x00mod.txt\x00")}, nil
			}
			if a == "--numstat" {
				return gitx.Result{Stdout: []byte("1\t0\tmod.txt\x001\t0\tonly-in-numstat.txt\x00")}, nil
			}
		}
		return gitx.Result{}, nil
	}}
	_, _, err := Summarize(context.Background(), fr, "/repo", fakeBaseSHA, fakeHeadSHA)
	if err == nil {
		t.Fatal("expected error on numstat/raw mismatch")
	}
	if !strings.Contains(err.Error(), "only-in-numstat.txt") {
		t.Errorf("error should name the mismatched path: %v", err)
	}
}

// TestSummarizeEmptyDiff verifies base == head (no changes) produces an
// empty, zeroed summary with no error, against real git rather than by
// inspection alone.
func TestSummarizeEmptyDiff(t *testing.T) {
	src := testutil.NewRepo(t)
	head := src.Head()

	runner, err := gitx.NewExecRunner(slog.New(slog.NewTextHandler(os.Stderr, nil)), gitx.Options{CommandTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	ctx := context.Background()

	files, totals, err := Summarize(ctx, runner, src.Work, head, head)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("files = %+v, want empty", files)
	}
	if totals != (Totals{}) {
		t.Errorf("totals = %+v, want zero value", totals)
	}

	out := filepath.Join(t.TempDir(), "combined.diff")
	if err := WriteCombined(ctx, runner, src.Work, head, head, out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 0 {
		t.Errorf("combined.diff for empty range should be empty, got %q", b)
	}
}
