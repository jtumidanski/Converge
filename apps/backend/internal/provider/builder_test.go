package provider

import (
	"errors"
	"testing"

	"github.com/jtumidanski/converge/internal/gitx"
)

func TestBranchBuilder(t *testing.T) {
	const sha = "6140736dbb1a0a0f1f0e2a1b3c4d5e6f70819a2b"
	cases := []struct {
		name    string
		build   func() (Branch, error)
		wantErr bool
	}{
		{"valid with sha", func() (Branch, error) {
			return NewBranchBuilder().SetName("main").SetSHA(sha).SetDefault(true).Build()
		}, false},
		{"valid without sha", func() (Branch, error) {
			return NewBranchBuilder().SetName("release/1.0").Build()
		}, false},
		{"empty name", func() (Branch, error) {
			return NewBranchBuilder().SetName("").Build()
		}, true},
		{"option-like name", func() (Branch, error) {
			return NewBranchBuilder().SetName("--upload-pack=evil").Build()
		}, true},
		{"traversal in name", func() (Branch, error) {
			return NewBranchBuilder().SetName("feat/../x").Build()
		}, true},
		{"short sha", func() (Branch, error) {
			return NewBranchBuilder().SetName("main").SetSHA("614073").Build()
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.build()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %+v", got)
				}
				if !errors.Is(err, gitx.ErrInvalid) {
					t.Errorf("error = %v, want gitx.ErrInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBranchGetters(t *testing.T) {
	const sha = "6140736dbb1a0a0f1f0e2a1b3c4d5e6f70819a2b"
	b, err := NewBranchBuilder().SetName("main").SetSHA(sha).SetDefault(true).Build()
	if err != nil {
		t.Fatal(err)
	}
	if b.Name() != "main" || b.SHA() != sha || !b.IsDefault() {
		t.Errorf("branch = %+v", b)
	}
}
