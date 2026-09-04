package review

import (
	"context"
	"errors"
	"testing"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/session"
)

func TestCreateInputValidate(t *testing.T) {
	ctx := context.Background()
	runner := &gitx.FakeRunner{}
	good := CreateInput{ProviderID: "gh", Repository: "atlas/server", BaseBranch: "main", Changes: []int{427, 421, 421}}
	if _, err := good.Validate(ctx, runner); err == nil {
		t.Fatal("duplicate change numbers must be rejected")
	}
	good.Changes = []int{427, 421}
	out, err := good.Validate(ctx, runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Changes) != 2 || out.Changes[0] != 421 || out.Changes[1] != 427 {
		t.Errorf("changes not normalised: %v", out.Changes)
	}
	cases := []struct {
		name string
		in   CreateInput
		code session.Code
	}{
		{"provider", CreateInput{ProviderID: "", Repository: "a/b", BaseBranch: "main", Changes: []int{1}}, CodeInvalidProvider},
		{"repository", CreateInput{ProviderID: "gh", Repository: "../x", BaseBranch: "main", Changes: []int{1}}, CodeInvalidRepository},
		{"branch", CreateInput{ProviderID: "gh", Repository: "a/b", BaseBranch: "-x", Changes: []int{1}}, CodeInvalidBranch},
		{"changes empty", CreateInput{ProviderID: "gh", Repository: "a/b", BaseBranch: "main"}, CodeInvalidChanges},
		{"changes zero", CreateInput{ProviderID: "gh", Repository: "a/b", BaseBranch: "main", Changes: []int{0}}, CodeInvalidChanges},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.in.Validate(ctx, runner)
			var ie *InputError
			if !errors.As(err, &ie) || ie.Code != tc.code {
				t.Fatalf("err = %v, want code %s", err, tc.code)
			}
		})
	}
	// an empty base branch is allowed here; the service fills the repository default
	if _, err := (CreateInput{ProviderID: "gh", Repository: "a/b", Changes: []int{1}}).Validate(ctx, runner); err != nil {
		t.Fatalf("empty base branch must be allowed: %v", err)
	}
}
