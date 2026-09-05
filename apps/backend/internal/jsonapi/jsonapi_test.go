package jsonapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type attrs struct {
	Name string `json:"name"`
	N    int    `json:"n"`
}

func TestWriteOne(t *testing.T) {
	w := httptest.NewRecorder()
	err := WriteOne(w, http.StatusCreated, Resource{
		Type: "reviews", ID: "7f14b2c8", Attributes: attrs{Name: "x", N: 1},
		Relationships: map[string]Relationship{"files": {Links: Links{Related: "/api/reviews/7f14b2c8/files"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusCreated || w.Header().Get("Content-Type") != MediaType {
		t.Fatalf("code=%d ct=%q", w.Code, w.Header().Get("Content-Type"))
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	data := got["data"].(map[string]any)
	if data["type"] != "reviews" || data["id"] != "7f14b2c8" {
		t.Errorf("data = %v", data)
	}
	if data["attributes"].(map[string]any)["name"] != "x" {
		t.Errorf("attributes = %v", data["attributes"])
	}
	rel := data["relationships"].(map[string]any)["files"].(map[string]any)["links"].(map[string]any)
	if rel["related"] != "/api/reviews/7f14b2c8/files" {
		t.Errorf("relationships = %v", rel)
	}
}

func TestWriteListAlwaysHasArrayAndMeta(t *testing.T) {
	w := httptest.NewRecorder()
	if err := WriteList(w, http.StatusOK, nil, &Meta{Page: &PageMeta{Number: 1, Size: 30, HasNext: true}}); err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"data":[]`) {
		t.Errorf("empty list must serialise as []: %s", body)
	}
	if !strings.Contains(body, `"hasNext":true`) || !strings.Contains(body, `"size":30`) {
		t.Errorf("meta missing: %s", body)
	}
	w2 := httptest.NewRecorder()
	if err := WriteList(w2, http.StatusOK, []Resource{{Type: "providers", ID: "gh", Attributes: attrs{Name: "GitHub"}}}, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(w2.Body.String(), `"meta"`) {
		t.Errorf("nil meta must be omitted: %s", w2.Body.String())
	}
}

func TestWriteListMetaWithoutPageOmitsPageField(t *testing.T) {
	w := httptest.NewRecorder()
	if err := WriteList(w, http.StatusOK, []Resource{}, &Meta{}); err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"meta":{}`) {
		t.Errorf("meta with nil page must serialise as {}: %s", body)
	}
	if strings.Contains(body, `"page"`) {
		t.Errorf("nil page must be omitted: %s", body)
	}
}

func TestWriteErrors(t *testing.T) {
	w := httptest.NewRecorder()
	if err := WriteError(w, http.StatusBadRequest, "INVALID_CHANGES", "Invalid request", "Select at least one PR/MR."); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", w.Code)
	}
	var doc struct {
		Errors []struct {
			Status, Code, Title, Detail string
		} `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Errors) != 1 || doc.Errors[0].Status != "400" || doc.Errors[0].Code != "INVALID_CHANGES" || doc.Errors[0].Detail == "" {
		t.Errorf("errors = %+v", doc.Errors)
	}
}

func TestDecode(t *testing.T) {
	body := `{"data":{"type":"reviews","attributes":{"name":"x","n":3}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/reviews", strings.NewReader(body))
	got, err := Decode[attrs](req, "reviews")
	if err != nil || got.Name != "x" || got.N != 3 {
		t.Fatalf("got %+v err %v", got, err)
	}
	cases := map[string]string{
		"wrong type":     `{"data":{"type":"widgets","attributes":{}}}`,
		"missing data":   `{}`,
		"unknown field":  `{"data":{"type":"reviews","attributes":{"name":"x","nope":1}}}`,
		"malformed json": `{`,
		"array data":     `{"data":[]}`,
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/reviews", strings.NewReader(b))
			if _, err := Decode[attrs](req, "reviews"); err == nil {
				t.Fatal("accepted")
			} else {
				var de *DecodeError
				if !asDecodeError(err, &de) {
					t.Fatalf("err = %v, want *DecodeError", err)
				}
			}
		})
	}
}

func asDecodeError(err error, target **DecodeError) bool {
	return errors.As(err, target)
}

func TestDecodeRejectsOversizedBody(t *testing.T) {
	// Build a body that is *exactly* MaxBodyBytes+1 and, crucially, still a
	// complete, well-formed JSON:API document at that length: if truncation
	// by the size limit merely produced malformed JSON, a naive
	// "returns *DecodeError" assertion would pass for the wrong reason
	// (malformed JSON) without ever exercising the size check itself.
	const prefix = `{"data":{"type":"reviews","attributes":{"name":"`
	const suffix = `","n":1}}}`
	padLen := (MaxBodyBytes + 1) - len(prefix) - len(suffix)
	var buf bytes.Buffer
	buf.WriteString(prefix)
	buf.WriteString(strings.Repeat("a", padLen))
	buf.WriteString(suffix)
	if buf.Len() != MaxBodyBytes+1 {
		t.Fatalf("test body length = %d, want %d", buf.Len(), MaxBodyBytes+1)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/reviews", &buf)
	got, err := Decode[attrs](req, "reviews")
	if err == nil {
		t.Fatalf("accepted oversized body, got %+v", got)
	}
	var de *DecodeError
	if !asDecodeError(err, &de) {
		t.Fatalf("err = %v, want *DecodeError", err)
	}
	if de.Detail != "The request body is too large." {
		t.Fatalf("err.Detail = %q, want %q", de.Detail, "The request body is too large.")
	}
}

func TestWriteOneWireFormatFieldNames(t *testing.T) {
	// Assert against the raw JSON text, not a round-trip through the same
	// struct: a round trip would pass even if the struct tags were wrong,
	// because both sides would use the same (wrong) name.
	w := httptest.NewRecorder()
	if err := WriteOne(w, http.StatusOK, Resource{Type: "reviews", ID: "abc", Attributes: attrs{Name: "x", N: 1}}); err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()
	for _, want := range []string{`"data":{`, `"type":"reviews"`, `"id":"abc"`, `"attributes":{`, `"name":"x"`, `"n":1`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %s", want, body)
		}
	}
}

func TestDecodeReadsAtMostLimitReader(t *testing.T) {
	// Guards against a body that never reports EOF within the limit but is
	// itself huge; io.LimitReader must be applied to the underlying reader.
	r := io.NopCloser(io.MultiReader(strings.NewReader(`{"data":{"type":"reviews","attributes":{"name":"`), &infiniteReader{}))
	req := httptest.NewRequest(http.MethodPost, "/api/reviews", nil)
	req.Body = r
	_, err := Decode[attrs](req, "reviews")
	if err == nil {
		t.Fatal("accepted unbounded body")
	}
	var de *DecodeError
	if !errors.As(err, &de) {
		t.Fatalf("err = %v (%T), want *DecodeError", err, err)
	}
}

type infiniteReader struct{}

func (r *infiniteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}
