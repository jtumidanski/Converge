// Package provider defines the provider-agnostic model and interface.
package provider

import (
	"strconv"
	"strings"
)

const (
	DefaultPageSize = 30
	MaxPageSize     = 100
)

// Page is a 1-based page request.
type Page struct {
	Number int
	Size   int
}

// Normalize applies defaults and clamps.
func (p Page) Normalize() Page {
	if p.Number < 1 {
		p.Number = 1
	}
	if p.Size < 1 {
		p.Size = DefaultPageSize
	}
	if p.Size > MaxPageSize {
		p.Size = MaxPageSize
	}
	return p
}

// Slice is one page of results.
type Slice[T any] struct {
	Items   []T
	HasNext bool
}

// ParseSearchNumber interprets "#421", "!421" or "421" as a change number.
func ParseSearchNumber(search string) (int, bool) {
	s := strings.TrimSpace(search)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "#"), "!")
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
