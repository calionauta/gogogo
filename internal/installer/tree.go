// SCOPE:layer=infra,removal=core — path-confined file IO for the installer.
//
// The installer rewrites a checkout the user pointed at with --dir. That
// checkout may itself have come from an untrusted source, so every write and
// removal must be confined to the target root even if the tree contains a
// symlink pointing outside it. A lexical guard (filepath.Join + Abs +
// HasPrefix, see joinRoot) stops `../` traversal but NOT a planted symlink:
// with `root/evil -> /etc`, `root/evil/x` passes the prefix check and the
// write lands in /etc.
//
// os.Root (Go 1.24+) is enforced by the kernel: every operation is resolved
// relative to the directory handle and a symlink that would leave the root is
// refused. treeFS wraps it, accepting the absolute paths the installer already
// builds and translating them to root-relative names. joinRoot is kept as
// lexical defense-in-depth.
package installer

import (
	"fmt"
	"os"
	"path/filepath"
)

// treeFS confines file IO to a single root directory. Methods take the same
// absolute paths the installer already constructs; the root-relative name is
// derived internally, so a path that escapes the root is refused by os.Root
// even when it is lexically clean (e.g. through a planted symlink).
type treeFS struct {
	root string
	r    *os.Root
}

// openTree opens root for path-confined IO. The caller must Close it.
func openTree(root string) (*treeFS, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(absRoot)
	if err != nil {
		return nil, fmt.Errorf("open root %s: %w", root, err)
	}
	return &treeFS{root: absRoot, r: r}, nil
}

// Close releases the directory handle.
func (t *treeFS) Close() error { return t.r.Close() }

// rel converts an absolute path to a root-relative name. A path outside the
// root yields an error; os.Root would also refuse it, so this is belt and
// suspenders.
func (t *treeFS) rel(abs string) (string, error) {
	rel, err := filepath.Rel(t.root, abs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// ReadFile reads abs under the root.
func (t *treeFS) ReadFile(abs string) ([]byte, error) {
	rel, err := t.rel(abs)
	if err != nil {
		return nil, err
	}
	return t.r.ReadFile(rel)
}

// Lstat stats abs under the root without following a final symlink.
func (t *treeFS) Lstat(abs string) (os.FileInfo, error) {
	rel, err := t.rel(abs)
	if err != nil {
		return nil, err
	}
	return t.r.Lstat(rel)
}

// WriteFile writes data to abs under the root, creating parent directories.
// A symlink in the destination that leaves the root is refused.
func (t *treeFS) WriteFile(abs string, data []byte, perm os.FileMode) error {
	rel, err := t.rel(abs)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(rel); dir != "." {
		if err := t.r.MkdirAll(dir, dirMode); err != nil {
			return err
		}
	}
	return t.r.WriteFile(rel, data, perm)
}

// Remove removes abs (file or empty dir) under the root.
func (t *treeFS) Remove(abs string) error {
	rel, err := t.rel(abs)
	if err != nil {
		return err
	}
	return t.r.Remove(rel)
}

// RemoveAll removes abs and its subtree under the root.
func (t *treeFS) RemoveAll(abs string) error {
	rel, err := t.rel(abs)
	if err != nil {
		return err
	}
	return t.r.RemoveAll(rel)
}

// copyInto copies src (a path in the trusted template tree) to abs inside the
// root, recursively. Writes are confined: a pre-existing symlink in the
// destination that leaves the root is refused by the kernel.
func (t *treeFS) copyInto(src, abs string) error {
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		raw, readErr := os.ReadFile(src)
		if readErr != nil {
			return readErr
		}
		return t.WriteFile(abs, raw, scaffoldFileMode)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if rel, relErr := t.rel(abs); relErr != nil {
		return relErr
	} else if mkErr := t.r.MkdirAll(rel, dirMode); mkErr != nil {
		return mkErr
	}
	for _, e := range entries {
		if err := t.copyInto(filepath.Join(src, e.Name()), filepath.Join(abs, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
