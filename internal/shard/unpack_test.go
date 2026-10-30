package shard

import (
	"path/filepath"
	"testing"
)

func TestSafeJoin(t *testing.T) {
	root := t.TempDir()
	got, err := safeJoin(root, "a/b.txt")
	if err != nil {
		t.Fatalf("safeJoin valid path: %v", err)
	}
	if want := filepath.Join(root, "a/b.txt"); got != want {
		t.Fatalf("safeJoin = %q, want %q", got, want)
	}
	for _, rel := range []string{"", "/etc/passwd", "../x", "a/../../x", ".."} {
		if _, err := safeJoin(root, rel); err == nil {
			t.Fatalf("safeJoin(%q) expected error", rel)
		}
	}
}
