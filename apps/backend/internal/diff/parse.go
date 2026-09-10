package diff

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// rawEntry is one decoded record from `git diff --raw -z`.
type rawEntry struct {
	Path         string
	PreviousPath string
	Status       FileStatus
}

// numstatEntry is one decoded record from `git diff --numstat -z`.
type numstatEntry struct {
	Path         string
	PreviousPath string
	Additions    int
	Deletions    int
	Binary       bool
}

// splitNul splits a NUL-separated byte stream into tokens, dropping a
// trailing terminator and returning nil for empty input.
func splitNul(b []byte) []string {
	parts := strings.Split(string(bytes.TrimSuffix(b, []byte{0})), "\x00")
	if len(parts) == 1 && parts[0] == "" {
		return nil
	}
	return parts
}

// parseRaw reads `git diff --raw -z` output.
func parseRaw(b []byte) ([]rawEntry, error) {
	toks := splitNul(b)
	var out []rawEntry
	for i := 0; i < len(toks); {
		hdr := toks[i]
		if !strings.HasPrefix(hdr, ":") {
			return nil, fmt.Errorf("diff: unexpected raw token %q", hdr)
		}
		fields := strings.Fields(hdr[1:])
		if len(fields) < 5 {
			return nil, fmt.Errorf("diff: malformed raw header %q", hdr)
		}
		code := fields[4][0]
		var e rawEntry
		switch code {
		case 'R', 'C':
			if i+2 >= len(toks) {
				return nil, fmt.Errorf("diff: truncated rename record")
			}
			e = rawEntry{Path: toks[i+2], PreviousPath: toks[i+1], Status: StatusRenamed}
			i += 3
		default:
			if i+1 >= len(toks) {
				return nil, fmt.Errorf("diff: truncated record")
			}
			e = rawEntry{Path: toks[i+1], Status: statusFor(code)}
			i += 2
		}
		out = append(out, e)
	}
	return out, nil
}

func statusFor(code byte) FileStatus {
	switch code {
	case 'A':
		return StatusAdded
	case 'D':
		return StatusDeleted
	default:
		return StatusModified
	}
}

// parseNumstat reads `git diff --numstat -z` output.
func parseNumstat(b []byte) ([]numstatEntry, error) {
	toks := splitNul(b)
	var out []numstatEntry
	for i := 0; i < len(toks); {
		parts := strings.SplitN(toks[i], "\t", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("diff: malformed numstat %q", toks[i])
		}
		e := numstatEntry{}
		if parts[0] == "-" || parts[1] == "-" {
			e.Binary = true
		} else {
			var err error
			if e.Additions, err = strconv.Atoi(parts[0]); err != nil {
				return nil, fmt.Errorf("diff: numstat additions %q", parts[0])
			}
			if e.Deletions, err = strconv.Atoi(parts[1]); err != nil {
				return nil, fmt.Errorf("diff: numstat deletions %q", parts[1])
			}
		}
		if parts[2] == "" {
			if i+2 >= len(toks) {
				return nil, fmt.Errorf("diff: truncated numstat rename")
			}
			e.PreviousPath, e.Path = toks[i+1], toks[i+2]
			i += 3
		} else {
			e.Path = parts[2]
			i++
		}
		out = append(out, e)
	}
	return out, nil
}
