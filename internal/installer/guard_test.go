package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuardOpenByDefault(t *testing.T) {
	t.Setenv(envReadonly, "")
	t.Setenv(envRoots, "")
	if err := guardMutable("/tmp/anything", "/"); err != nil {
		t.Fatalf("unset rails must stay open: %v", err)
	}
}

func TestGuardReadonlyRefuses(t *testing.T) {
	t.Setenv(envReadonly, "1")
	t.Setenv(envRoots, "")
	err := guardMutable(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("expected a read-only refusal, got %v", err)
	}
}

func TestGuardRootsConfine(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envReadonly, "")
	t.Setenv(envRoots, root)
	if err := guardMutable(filepath.Join(root, "proj")); err != nil {
		t.Fatalf("inside roots must pass: %v", err)
	}
	if err := guardMutable(root); err != nil {
		t.Fatalf("the root itself must pass: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "escape")
	if err := guardMutable(outside); err == nil ||
		!strings.Contains(err.Error(), "outside the allowed roots") {
		t.Fatalf("expected a confinement refusal, got %v", err)
	}
	// Sibling-prefix attack: root2 must not pass for root2-evil.
	if err := guardMutable(root + "-evil"); err == nil {
		t.Error("prefix-sibling must not pass the roots check")
	}
}

func TestGuardMultipleInputs(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envReadonly, "")
	t.Setenv(envRoots, root)
	ok := filepath.Join(root, "proj")
	bad := string(os.PathSeparator) + "elsewhere"
	if err := guardMutable(ok, bad); err == nil {
		t.Error("one bad input must fail the whole call")
	}
}
