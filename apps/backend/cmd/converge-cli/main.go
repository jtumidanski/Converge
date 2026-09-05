// Command converge-cli reconstructs a combined review without the web UI.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jtumidanski/converge/internal/app"
	"github.com/jtumidanski/converge/internal/buildinfo"
	"github.com/jtumidanski/converge/internal/review"
	"github.com/jtumidanski/converge/internal/session"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func usage(w io.Writer) {
	_, _ = fmt.Fprint(w, `converge-cli reconstructs the combined net diff of merged PRs/MRs.

Usage:
  converge-cli build --provider <id> --repo <owner/repo> [--base <branch>] --changes 421,427 [--out <dir>] [--cleanup]
  converge-cli --version

Exit codes: 0 ready, 2 conflict, 3 validation or base error, 4 provider error, 1 unexpected error.
`)
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 1
	}
	if args[0] == "--version" || args[0] == "-version" {
		_, _ = fmt.Fprintln(stdout, buildinfo.Version)
		return 0
	}
	if args[0] != "build" {
		usage(stderr)
		return 1
	}
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	providerID := fs.String("provider", "", "configured provider id")
	repo := fs.String("repo", "", "repository as owner/name")
	base := fs.String("base", "", "base branch (defaults to the repository default)")
	changesRaw := fs.String("changes", "", "comma-separated PR/MR numbers")
	out := fs.String("out", "", "output directory (default ./converge-out/<session-id>)")
	cleanup := fs.Bool("cleanup", false, "remove the workspace after writing output")
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}
	changes, err := parseChanges(*changesRaw)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "converge-cli: %v\n", err)
		return 3
	}
	ctx := context.Background()
	application, err := app.New(ctx, os.Environ())
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "converge-cli: %v\n", err)
		return exitCodeForError(err)
	}
	defer func() { _ = application.Close() }()

	sess, err := application.Service.Create(ctx, review.CreateInput{ProviderID: *providerID, Repository: *repo, BaseBranch: *base, Changes: changes})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "converge-cli: %v\n", err)
		return exitCodeForError(err)
	}
	final := application.Service.Build(ctx, sess.ID())

	dir := *out
	if dir == "" {
		dir = filepath.Join("converge-out", final.ID())
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		_, _ = fmt.Fprintf(stderr, "converge-cli: create output directory: %v\n", err)
		return 1
	}
	record := session.ToRecord(final)
	metadata, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "converge-cli: encode metadata: %v\n", err)
		return 1
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), append(metadata, '\n'), 0o600); err != nil {
		_, _ = fmt.Fprintf(stderr, "converge-cli: write metadata: %v\n", err)
		return 1
	}
	if final.Status() == session.StatusReady {
		src, err := application.Service.CombinedDiffPath(final.ID())
		if err == nil {
			if err := copyFile(src, filepath.Join(dir, review.CombinedDiffFile)); err != nil {
				_, _ = fmt.Fprintf(stderr, "converge-cli: copy diff: %v\n", err)
				return 1
			}
		}
	}
	_, _ = fmt.Fprintln(stdout, string(metadata))
	code := 0
	if re := final.Error(); re != nil {
		code = exitCodeFor(final.Status(), re.Code)
	} else {
		code = exitCodeFor(final.Status(), "")
	}
	if *cleanup {
		if err := application.Service.Finish(ctx, final.ID()); err != nil {
			_, _ = fmt.Fprintf(stderr, "converge-cli: cleanup: %v\n", err)
		}
	}
	return code
}

// copyFile copies the combined diff into the requested output directory. src
// is the service's own on-disk path for the session (not user input) and dst
// is built from the operator-supplied --out directory, both intentionally
// dynamic for a CLI whose whole job is writing to a caller-chosen location.
func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // G304: src is Service.CombinedDiffPath's own session output path
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // G304: dst is the operator-supplied --out directory, the CLI's entire purpose
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	_, err = io.Copy(out, in)
	return err
}

// parseChanges splits and validates the --changes value.
func parseChanges(raw string) ([]int, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("--changes is required")
	}
	parts := strings.Split(raw, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid change number %q", strings.TrimSpace(p))
		}
		out = append(out, n)
	}
	return out, nil
}

// exitCodeFor maps a final status and code to a process exit code.
func exitCodeFor(status session.Status, code session.Code) int {
	switch status {
	case session.StatusReady:
		return 0
	case session.StatusConflicted:
		return 2
	}
	switch code {
	case session.CodeNotMerged, session.CodeIncompatibleTargets, session.CodeNotOnBaseBranch, session.CodeMissingCommits, session.CodeBaseUndetermined:
		return 3
	case session.CodeProviderAuth, session.CodeProviderUnavailable, session.CodeRepositoryUnavailable:
		return 4
	}
	return 1
}

// exitCodeForError maps a pre-build error to an exit code.
func exitCodeForError(err error) int {
	var ie *review.InputError
	if errors.As(err, &ie) {
		return 3
	}
	var re *session.ReviewError
	if errors.As(err, &re) {
		return exitCodeFor(session.StatusFailed, re.Code)
	}
	return 1
}
