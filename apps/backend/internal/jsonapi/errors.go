package jsonapi

import (
	"net/http"
	"strconv"
)

// Error is one JSON:API error object.
type Error struct {
	Status string         `json:"status"`
	Code   string         `json:"code"`
	Title  string         `json:"title"`
	Detail string         `json:"detail,omitempty"`
	Meta   map[string]any `json:"meta,omitempty"`
}

type errorDocument struct {
	Errors []Error `json:"errors"`
}

// WriteErrors writes an error document.
func WriteErrors(w http.ResponseWriter, status int, errs ...Error) error {
	return write(w, status, errorDocument{Errors: errs})
}

// WriteError writes a single error document.
func WriteError(w http.ResponseWriter, status int, code, title, detail string) error {
	return WriteErrors(w, status, Error{Status: strconv.Itoa(status), Code: code, Title: title, Detail: detail})
}

// StatusTitle returns a human-readable title for an HTTP status code.
func StatusTitle(status int) string {
	if t := http.StatusText(status); t != "" {
		return t
	}
	return "Error"
}
