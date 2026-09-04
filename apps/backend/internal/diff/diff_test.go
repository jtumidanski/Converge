package diff

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if files[0].Path > files[1].Path {
		t.Error("not sorted")
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
	if err != nil || !fd.Truncated || len(fd.Diff) != MaxFileDiffBytes {
		t.Fatalf("err=%v truncated=%v len=%d", err, fd.Truncated, len(fd.Diff))
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
