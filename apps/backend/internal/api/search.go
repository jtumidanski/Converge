package api

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jtumidanski/converge/internal/jsonapi"
)

// maxSearchLen bounds ?search before it reaches a provider API. Measured in
// runes, not bytes, so a multi-byte query is judged by what the user typed.
const maxSearchLen = 200

// searchFrom trims ?search and reports whether it is acceptable. On rejection
// it has already written the 400 document, matching the inline INVALID_STATE
// style in changes.go.
func searchFrom(w http.ResponseWriter, r *http.Request) (string, bool) {
	search := strings.TrimSpace(r.URL.Query().Get("search"))
	if utf8.RuneCountInString(search) > maxSearchLen {
		_ = jsonapi.WriteError(w, http.StatusBadRequest, "INVALID_SEARCH",
			jsonapi.StatusTitle(http.StatusBadRequest),
			"The search text is too long.")
		return "", false
	}
	return search, true
}
