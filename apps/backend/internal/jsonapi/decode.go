package jsonapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// MaxBodyBytes bounds request bodies read by Decode.
const MaxBodyBytes = 1 << 20

// DecodeError is a client-fixable request body problem.
type DecodeError struct{ Detail string }

func (e *DecodeError) Error() string { return e.Detail }

type requestDocument struct {
	Data struct {
		Type       string          `json:"type"`
		ID         string          `json:"id"`
		Attributes json.RawMessage `json:"attributes"`
	} `json:"data"`
}

// Decode reads at most MaxBodyBytes from r, requires the JSON:API document's
// data.type to equal wantType, and unmarshals data.attributes into T,
// rejecting unknown attribute fields. Every failure path returns a
// *DecodeError.
func Decode[T any](r *http.Request, wantType string) (T, error) {
	var zero T
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxBodyBytes+1))
	if err != nil {
		return zero, &DecodeError{Detail: "The request body could not be read."}
	}
	if len(body) > MaxBodyBytes {
		return zero, &DecodeError{Detail: "The request body is too large."}
	}
	var doc requestDocument
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&doc); err != nil {
		return zero, &DecodeError{Detail: "The request body is not a valid JSON:API document."}
	}
	if doc.Data.Type == "" {
		return zero, &DecodeError{Detail: `The request body must contain a "data" object with a "type".`}
	}
	if doc.Data.Type != wantType {
		return zero, &DecodeError{Detail: fmt.Sprintf("Expected resource type %q, got %q.", wantType, doc.Data.Type)}
	}
	if len(doc.Data.Attributes) == 0 {
		return zero, &DecodeError{Detail: `The request body must contain "data.attributes".`}
	}
	attrDec := json.NewDecoder(bytes.NewReader(doc.Data.Attributes))
	attrDec.DisallowUnknownFields()
	var out T
	if err := attrDec.Decode(&out); err != nil {
		return zero, &DecodeError{Detail: "The request attributes are invalid: " + err.Error()}
	}
	return out, nil
}
