package ui

import (
	"io/fs"
	"testing"
)

func TestFSIsReadable(t *testing.T) {
	if _, err := fs.ReadDir(FS(), "."); err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
}

// TestPresentReflectsIndexHTML independently stats index.html and checks
// Present() agrees, so it catches Present() being hardcoded to either
// true or false regardless of the embedded dist contents. Both outcomes
// (index.html present after `make build`, absent in a source checkout)
// are valid; what must hold is that Present() tracks reality.
func TestPresentReflectsIndexHTML(t *testing.T) {
	_, statErr := fs.Stat(FS(), "index.html")
	want := statErr == nil
	if got := Present(); got != want {
		t.Fatalf("Present() = %v, want %v (fs.Stat(\"index.html\") err = %v)", got, want, statErr)
	}
}
