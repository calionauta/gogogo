package installer

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPreflightSkipsNothingNeeded(t *testing.T) {
	if err := preflight(context.Background(), devNull(t), false, false); err != nil {
		t.Fatalf("no requirements should always pass: %v", err)
	}
}

func TestPreflightGitMissing(t *testing.T) {
	old := lookGit
	defer func() { lookGit = old }()
	lookGit = func() error { return errors.New("exec: git not in PATH") }
	err := preflight(context.Background(), devNull(t), true, false)
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
	err := preflight(context.Background(), devNull(t), true, true)
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
	err := preflight(context.Background(), devNull(t), false, true)
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
	if err := preflight(context.Background(), &out, false, true); err != nil {
		t.Fatalf("current go should pass: %v", err)
	}
	if !strings.Contains(out.String(), "toolchain OK") {
		t.Error("pass should narrate the accepted toolchain")
	}
}

func TestParseGoVersion(t *testing.T) {
	major, minor, ok := parseGoVersion("go version go1.27.1 linux/amd64\n")
	if !ok || major != 1 || minor != 27 {
		t.Errorf("parse = %d.%d,%v, want 1.27,true", major, minor, ok)
	}
	if _, _, ok := parseGoVersion("not a version"); ok {
		t.Error("garbage should not parse")
	}
}
