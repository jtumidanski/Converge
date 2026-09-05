// Package jsonapi encodes and decodes the subset of JSON:API that Converge uses.
package jsonapi

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// MediaType is the JSON:API content type.
const MediaType = "application/vnd.api+json"

// Links holds a related link.
type Links struct {
	Related string `json:"related,omitempty"`
}

// Relationship is a links-only relationship.
type Relationship struct {
	Links Links `json:"links"`
}

// Resource is one JSON:API resource object.
type Resource struct {
	Type          string                  `json:"type"`
	ID            string                  `json:"id"`
	Attributes    any                     `json:"attributes,omitempty"`
	Relationships map[string]Relationship `json:"relationships,omitempty"`
}

// PageMeta describes a page of results.
type PageMeta struct {
	Number  int  `json:"number"`
	Size    int  `json:"size"`
	HasNext bool `json:"hasNext"`
}

// Meta is the document-level meta member.
type Meta struct {
	Page *PageMeta `json:"page,omitempty"`
}

type singleDocument struct {
	Data Resource `json:"data"`
}

type listDocument struct {
	Data []Resource `json:"data"`
	Meta *Meta      `json:"meta,omitempty"`
}

func write(w http.ResponseWriter, status int, doc any) error {
	w.Header().Set("Content-Type", MediaType)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(doc); err != nil {
		return fmt.Errorf("jsonapi: encode: %w", err)
	}
	return nil
}

// WriteOne writes a single-resource document.
func WriteOne(w http.ResponseWriter, status int, r Resource) error {
	return write(w, status, singleDocument{Data: r})
}

// WriteList writes a resource-collection document; nil resources serialise as [].
func WriteList(w http.ResponseWriter, status int, rs []Resource, meta *Meta) error {
	if rs == nil {
		rs = []Resource{}
	}
	return write(w, status, listDocument{Data: rs, Meta: meta})
}
