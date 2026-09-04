package ui

import (
	"io/fs"
	"testing"
)

func TestFSIsReadableAndPresentReflectsIndex(t *testing.T) {
	entries, err := fs.ReadDir(FS(), ".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	// dist/ only holds .gitkeep in a source checkout, so Present must be false.
	if Present() {
		t.Fatalf("Present() = true with entries %v, want false without index.html", entries)
	}
}
