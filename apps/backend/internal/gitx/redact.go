package gitx

import (
	"bytes"
	"regexp"
)

var headerRe = regexp.MustCompile(`(?i)(authorization|private-token)\s*:\s*[^\r\n;]+`)

// Redact scrubs known header values and any configured secret from b.
func Redact(b []byte, secrets []string) []byte {
	out := headerRe.ReplaceAllFunc(b, func(m []byte) []byte {
		i := bytes.IndexByte(m, ':')
		return append(append([]byte{}, m[:i]...), []byte(": [redacted]")...)
	})
	for _, s := range secrets {
		if s != "" {
			out = bytes.ReplaceAll(out, []byte(s), []byte("[redacted]"))
		}
	}
	return out
}
