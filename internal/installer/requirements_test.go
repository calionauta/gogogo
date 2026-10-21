package installer

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreflightSkipsNothingNeeded(t *testing.T) {
	if err := preflight(context.Background(), devNull(t), false, false, false); err != nil {
		t.Fatalf("no requirements should always pass: %v", err)
	}
}

func TestPreflightGitMissing(t *testing.T) {
	old := lookGit
	defer func() { lookGit = old }()
	lookGit = func() error { return errors.New("exec: git not in PATH") }
	err := preflight(context.Background(), devNull(t), true, false, false)
	if err == nil || !strings.Contains(err.Error(), "git not found") {
		t.Fatalf("expected a git-not-found refusal, got %v", err)
	}
}

func TestPreflightGoMissing(t *testing.T) {
	oldGit, oldGo := lookGit, goVersionOut
	defer func() { lookGit, goVersionOut = oldGit, oldGo }()
	lookGit = func() error { return nil }
	goVersionOut = func(_ context.Context) (string, error) {
		return "", errors.New("exec: go not in PATH")
	}
	err := preflight(context.Background(), devNull(t), true, true, false)
	if err == nil || !strings.Contains(err.Error(), "go not found") {
		t.Fatalf("expected a go-not-found refusal, got %v", err)
	}
}

func TestPreflightGoTooOld(t *testing.T) {
	old := goVersionOut
	defer func() { goVersionOut = old }()
	goVersionOut = func(_ context.Context) (string, error) {
		return "go version go1.20.14 linux/amd64\n", nil
	}
	err := preflight(context.Background(), devNull(t), false, true, false)
	if err == nil || !strings.Contains(err.Error(), "too old") {
		t.Fatalf("expected a too-old refusal, got %v", err)
	}
}

func TestPreflightGoCurrentPasses(t *testing.T) {
	old := goVersionOut
	defer func() { goVersionOut = old }()
	goVersionOut = func(_ context.Context) (string, error) {
		return "go version go1.23.4 linux/amd64\n", nil
	}
	var out strings.Builder
	if err := preflight(context.Background(), &out, false, true, false); err != nil {
		t.Fatalf("current go should pass: %v", err)
	}
	if !strings.Contains(out.String(), "toolchain OK") {
		t.Error("pass should narrate the accepted toolchain")
	}
}

func TestPreflightQuietSuppressesNarration(t *testing.T) {
	old := goVersionOut
	defer func() { goVersionOut = old }()
	goVersionOut = func(_ context.Context) (string, error) {
		return "go version go1.23.4 linux/amd64\n", nil
	}
	var out strings.Builder
	if err := preflight(context.Background(), &out, false, true, true); err != nil {
		t.Fatalf("quiet pass should succeed: %v", err)
	}
	if out.String() != "" {
		t.Errorf("quiet preflight must print nothing, got %q", out.String())
	}
}

func TestParseGoVersion(t *testing.T) {
	v, ok := parseGoVersion("go version go1.27.1 linux/amd64\n")
	if !ok || v != (goVer{major: 1, minor: 27, patch: 1}) {
		t.Errorf("parse = %+v,%v, want 1.27.1,true", v, ok)
	}
	if _, ok := parseGoVersion("not a version"); ok {
		t.Error("garbage should not parse")
	}
}

// stubToolchain points the two toolchain seams at fixed answers for one test.
func stubToolchain(t *testing.T, version, policy string) {
	t.Helper()
	oldVer, oldPol := goVersionOut, toolchainPolicyOut
	t.Cleanup(func() { goVersionOut, toolchainPolicyOut = oldVer, oldPol })
	goVersionOut = func(context.Context) (string, error) {
		return "go version " + version + " linux/amd64\n", nil
	}
	toolchainPolicyOut = func(context.Context) (string, error) { return policy + "\n", nil }
}

// writeGoMod drops a go.mod with one `go` directive into dir.
func writeGoMod(t *testing.T, dir, goLine string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "go.mod"), "module github.com/example/x\n\ngo "+goLine+"\n")
}

func TestCheckToolchainForTreeSatisfied(t *testing.T) {
	// An INSTALLED toolchain newer than the tree asks for must pass: patch
	// comparison could easily refuse here by comparing equality.
	dir := t.TempDir()
	writeGoMod(t, dir, "1.21")
	stubToolchain(t, "go1.27.3", "local")
	if err := checkToolchainForTree(context.Background(), dir, devNull(t), false); err != nil {
		t.Fatalf("a satisfying toolchain must pass: %v", err)
	}
}

func TestCheckToolchainForTreeNoGoMod(t *testing.T) {
	stubToolchain(t, "go1.20.1", "local")
	if err := checkToolchainForTree(context.Background(), t.TempDir(), devNull(t), false); err != nil {
		t.Fatalf("no go.mod means no requirement: %v", err)
	}
}

// TestCheckToolchainForTreePinnedOlder is the #207 follow-up: the go.mod
// `go` directive is enforced at PATCH level, so go1.27.2 cannot build a tree
// that asks for 1.27.3. With GOTOOLCHAIN pinned there is no self-upgrade, and
// the apply would destroy the wiring before `go mod tidy` said so.
func TestCheckToolchainForTreePinnedOlder(t *testing.T) {
	dir := t.TempDir()
	writeGoMod(t, dir, "1.27.3")
	for _, policy := range []string{"go1.27.2", "local"} {
		stubToolchain(t, "go1.27.2", policy)
		err := checkToolchainForTree(context.Background(), dir, devNull(t), false)
		if err == nil {
			t.Fatalf("GOTOOLCHAIN=%s must refuse a tree that needs go1.27.3", policy)
		}
		msg := err.Error()
		for _, want := range []string{"needs go1.27.3", "GOTOOLCHAIN=" + policy, "go env -w GOTOOLCHAIN=auto"} {
			if !strings.Contains(msg, want) {
				t.Errorf("refusal is missing %q:\n%s", want, msg)
			}
		}
	}
}

// TestCheckToolchainForTreeAutoUpgrades: the go default may fetch the tree's
// version, so an older INSTALLED toolchain is not a refusal — but it must be
// said out loud instead of printing a bare "toolchain OK".
func TestCheckToolchainForTreeAutoUpgrades(t *testing.T) {
	dir := t.TempDir()
	writeGoMod(t, dir, "1.27.3")
	stubToolchain(t, "go1.27.2", "auto")
	var out strings.Builder
	if err := checkToolchainForTree(context.Background(), dir, &out, false); err != nil {
		t.Fatalf("GOTOOLCHAIN=auto must not refuse: %v", err)
	}
	if !strings.Contains(out.String(), "needs go1.27.3") {
		t.Errorf("auto upgrade should be narrated, got %q", out.String())
	}
	out.Reset()
	if err := checkToolchainForTree(context.Background(), dir, &out, true); err != nil {
		t.Fatalf("quiet pass failed: %v", err)
	}
	if out.String() != "" {
		t.Errorf("quiet mode must print nothing, got %q", out.String())
	}
}

// TestCheckToolchainForTreeUnknownPolicyDoesNotBlock: a GOTOOLCHAIN name this
// installer does not model must not fail an apply — the real go command is the
// authority on it, and go.mod may not even be involved.
func TestCheckToolchainForTreeUnknownPolicyDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	writeGoMod(t, dir, "1.27.3")
	stubToolchain(t, "go1.27.2", "gccgo")
	if err := checkToolchainForTree(context.Background(), dir, devNull(t), false); err != nil {
		t.Fatalf("an unmodelled policy must not block: %v", err)
	}
}
