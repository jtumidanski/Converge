// Package diff produces combined.diff, the file summary, and per-file diffs.
package diff

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"unicode/utf8"

	"github.com/jtumidanski/converge/internal/gitx"
)

// FileStatus is the change type of a file.
type FileStatus string

const (
	StatusAdded    FileStatus = "added"
	StatusModified FileStatus = "modified"
	StatusDeleted  FileStatus = "deleted"
	StatusRenamed  FileStatus = "renamed"
)

// MaxFileDiffBytes caps per-file diff text.
const MaxFileDiffBytes = 1 << 20

// FileSummary is one entry of the file tree.
type FileSummary struct {
	Path         string     `json:"path"`
	PreviousPath string     `json:"previousPath,omitempty"`
	Status       FileStatus `json:"status"`
	Additions    int        `json:"additions"`
	Deletions    int        `json:"deletions"`
	Binary       bool       `json:"binary"`
}

// Totals are session-level counts.
type Totals struct {
	Files     int `json:"files"`
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

// FileDiff is one file's unified diff.
type FileDiff struct {
	FileSummary
	Truncated bool
	Diff      string
}

func validateRange(base, head string) error {
	if err := gitx.ValidateSHA(base); err != nil {
		return err
	}
	return gitx.ValidateSHA(head)
}

// WriteCombined writes `git diff --find-renames base head` atomically to outPath.
func WriteCombined(ctx context.Context, r gitx.Runner, repoDir, base, head, outPath string) error {
	if err := validateRange(base, head); err != nil {
		return err
	}
	res, err := r.Run(ctx, gitx.Spec{Dir: repoDir, Args: []string{"diff", "--find-renames", base, head}, Category: gitx.CategoryDiff})
	if err != nil {
		return fmt.Errorf("diff: combined: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(outPath), ".combined-*.tmp")
	if err != nil {
		return fmt.Errorf("diff: temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(res.Stdout); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("diff: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("diff: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("diff: close: %w", err)
	}
	return os.Rename(tmp.Name(), outPath)
}

// Summarize joins --raw and --numstat output into FileSummaries sorted by path.
func Summarize(ctx context.Context, r gitx.Runner, repoDir, base, head string) ([]FileSummary, Totals, error) {
	if err := validateRange(base, head); err != nil {
		return nil, Totals{}, err
	}
	rawRes, err := r.Run(ctx, gitx.Spec{Dir: repoDir, Args: []string{"diff", "--raw", "-z", "--find-renames", base, head}, Category: gitx.CategoryDiff})
	if err != nil {
		return nil, Totals{}, fmt.Errorf("diff: raw: %w", err)
	}
	numRes, err := r.Run(ctx, gitx.Spec{Dir: repoDir, Args: []string{"diff", "--numstat", "-z", "--find-renames", base, head}, Category: gitx.CategoryDiff})
	if err != nil {
		return nil, Totals{}, fmt.Errorf("diff: numstat: %w", err)
	}
	raws, err := parseRaw(rawRes.Stdout)
	if err != nil {
		return nil, Totals{}, err
	}
	nums, err := parseNumstat(numRes.Stdout)
	if err != nil {
		return nil, Totals{}, err
	}
	counts := make(map[string]numstatEntry, len(nums))
	for _, n := range nums {
		counts[n.Path] = n
	}
	files := make([]FileSummary, 0, len(raws))
	var totals Totals
	seen := make(map[string]struct{}, len(raws))
	for _, e := range raws {
		n, ok := counts[e.Path]
		if !ok {
			return nil, Totals{}, fmt.Errorf("diff: raw/numstat mismatch: %s present in --raw but missing from --numstat", e.Path)
		}
		seen[e.Path] = struct{}{}
		f := FileSummary{Path: e.Path, PreviousPath: e.PreviousPath, Status: e.Status, Additions: n.Additions, Deletions: n.Deletions, Binary: n.Binary}
		files = append(files, f)
		totals.Files++
		totals.Additions += f.Additions
		totals.Deletions += f.Deletions
	}
	// The converse (a --numstat path absent from --raw) is checked too: both
	// streams are produced by the same `git diff` invocation over the same
	// range, so a --numstat-only path indicates the same class of stream
	// desynchronisation as a --raw-only path, just discovered from the other
	// direction. Catching it here surfaces the inconsistency instead of
	// silently dropping the file from the summary.
	for _, n := range nums {
		if _, ok := seen[n.Path]; !ok {
			return nil, Totals{}, fmt.Errorf("diff: raw/numstat mismatch: %s present in --numstat but missing from --raw", n.Path)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, totals, nil
}

// FileContent returns the unified diff for one file, capped at MaxFileDiffBytes.
func FileContent(ctx context.Context, r gitx.Runner, repoDir, base, head string, f FileSummary) (FileDiff, error) {
	if err := validateRange(base, head); err != nil {
		return FileDiff{}, err
	}
	if err := gitx.ValidatePathArg(f.Path); err != nil {
		return FileDiff{}, err
	}
	args := []string{"diff", "--find-renames", base, head, "--", f.Path}
	if f.PreviousPath != "" {
		if err := gitx.ValidatePathArg(f.PreviousPath); err != nil {
			return FileDiff{}, err
		}
		args = append(args, f.PreviousPath)
	}
	out := FileDiff{FileSummary: f}
	if f.Binary {
		return out, nil
	}
	res, err := r.Run(ctx, gitx.Spec{Dir: repoDir, Args: args, Category: gitx.CategoryDiff})
	if err != nil {
		return FileDiff{}, fmt.Errorf("diff: file %s: %w", f.Path, err)
	}
	text := res.Stdout
	if len(text) > MaxFileDiffBytes {
		text = truncateToRuneBoundary(text, MaxFileDiffBytes)
		out.Truncated = true
	}
	out.Diff = string(text)
	return out, nil
}

// truncateToRuneBoundary cuts b to at most max bytes, backing off from max
// until it lands on a UTF-8 rune boundary so the result is never invalid
// UTF-8. The returned slice is always <= max bytes.
func truncateToRuneBoundary(b []byte, max int) []byte {
	cut := max
	for cut > 0 && !utf8.RuneStart(b[cut]) {
		cut--
	}
	return b[:cut]
}
