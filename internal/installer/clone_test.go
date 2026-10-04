package installer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureExistingDirTouchesNothing(t *testing.T) {
	dir := t.TempDir()
	opt := options{dir: dir}
	if err := ensureCheckoutDir(context.Background(), opt, devNull(t), devNull(t)); err != nil {
		t.Fatalf("existing dir should pass untouched: %v", err)
	}
}

func TestEnsureMissingDirAbortsWithoutConsent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nope")
	opt := options{dir: dir}
	in := strings.NewReader("n\n")
	var out strings.Builder
	err := ensureCheckoutDir(context.Background(), opt, in, &out)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected a --yes hint refusal, got %v", err)
	}
	if _, stat := os.Stat(dir); !os.IsNotExist(stat) {
		t.Error("refused clone must not create the directory")
	}
}

func TestEnsureMissingDirClonesWithYes(t *testing.T) {
	old := gitClone
	defer func() { gitClone = old }()
	cloned := ""
	gitClone = func(_ context.Context, dir string) error {
		cloned = dir
		return os.MkdirAll(dir, 0o755)
	}
	dir := filepath.Join(t.TempDir(), "my-app")
	opt := options{dir: dir, yes: true}
	var out strings.Builder
	if err := ensureCheckoutDir(context.Background(), opt, devNull(t), &out); err != nil {
		t.Fatalf("consented clone should succeed: %v", err)
	}
	if cloned != dir {
		t.Errorf("cloned into %q, want %q", cloned, dir)
	}
	if !strings.Contains(out.String(), "cloning") {
		t.Error("clone should narrate itself on stdout")
	}
}

func TestEnsureCloneFailureExplainsItself(t *testing.T) {
	old := gitClone
	defer func() { gitClone = old }()
	gitClone = func(_ context.Context, _ string) error {
		return os.ErrNotExist
	}
	dir := filepath.Join(t.TempDir(), "my-app")
	opt := options{dir: dir, yes: true}
	var out strings.Builder
	err := ensureCheckoutDir(context.Background(), opt, devNull(t), &out)
	if err == nil || !strings.Contains(err.Error(), "git clone failed") {
		t.Fatalf("expected a wrapped clone failure, got %v", err)
	}
}
