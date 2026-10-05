// SCOPE:layer=infra,removal=core — tests for the installer's path-confined IO.
package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCopyIntoRoot_RefusesSymlinkEscape is the security regression guard.
//
// The installer rewrites a checkout that may have come from an untrusted
// source. A lexical guard (joinRoot) blocks `../` but NOT a symlink planted
// inside the tree: with `root/evil -> <outside>`, the old code wrote through
// the link and landed outside the root entirely. os.Root refuses this at the
// kernel level.
//
// Red-proof: this test FAILS if copyIntoRoot is reverted to plain
// os.MkdirAll + os.WriteFile (verified — the write then succeeds outside).
func TestCopyIntoRoot_RefusesSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{root, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Plant the attack: root/evil is a symlink to a directory outside root.
	if err := os.Symlink(outside, filepath.Join(root, "evil")); err != nil {
		t.Fatalf("plant symlink: %v", err)
	}

	// A payload file in a trusted source tree.
	src := filepath.Join(base, "src", "payload.txt")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("pwned"), 0o600); err != nil {
		t.Fatal(err)
	}

	r, err := openTree(root)
	if err != nil {
		t.Fatalf("openTree: %v", err)
	}
	defer r.Close()

	err = r.copyInto(src, filepath.Join(root, "evil", "pwned.txt"))
	if err == nil {
		t.Fatal("copyInto allowed a write through an escaping symlink")
	}
	if !strings.Contains(err.Error(), "path") && !strings.Contains(err.Error(), "escape") {
		t.Logf("refusal error (informational): %v", err)
	}

	// The decisive assertion: nothing landed outside the root.
	if _, statErr := os.Stat(filepath.Join(outside, "pwned.txt")); statErr == nil {
		t.Fatal("SECURITY: write escaped the root via symlink")
	}
}

// TestJoinRoot_BlocksDotDot pins the lexical guard that still runs first:
// a `..` component is refused before os.Root is even reached.
func TestJoinRoot_BlocksDotDot(t *testing.T) {
	root := t.TempDir()
	if _, err := joinRoot(root, "../escaped.txt"); err == nil {
		t.Fatal("joinRoot accepted a ../ traversal")
	}
	if _, err := joinRoot(root, "ok/nested.txt"); err != nil {
		t.Fatalf("joinRoot refused a legitimate path: %v", err)
	}
}

// TestTreeFS_WriteFile_CreatesParents proves the happy path still works:
// nested parent directories are created inside the root.
func TestTreeFS_WriteFile_CreatesParents(t *testing.T) {
	root := t.TempDir()
	r, err := openTree(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	abs := filepath.Join(root, "a", "b", "c", "file.txt")
	if writeErr := r.WriteFile(abs, []byte("hi"), scaffoldFileMode); writeErr != nil {
		t.Fatalf("WriteFile: %v", writeErr)
	}
	got, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "hi" {
		t.Fatalf("content = %q, want %q", got, "hi")
	}
}

// TestTreeFS_RefusesOutsideAbsolutePath proves a lexically-escaping absolute
// path is refused before it can reach the filesystem.
func TestTreeFS_RefusesOutsideAbsolutePath(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := openTree(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if err := r.WriteFile(filepath.Join(base, "outside.txt"), []byte("x"), scaffoldFileMode); err == nil {
		t.Fatal("WriteFile accepted an absolute path outside the root")
	}
}
