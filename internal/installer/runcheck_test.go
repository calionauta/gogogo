package installer

import (
	"errors"
	"strings"
	"testing"
)

func TestPreflightRunPassesWithTools(t *testing.T) {
	old := lookPath
	defer func() { lookPath = old }()
	lookPath = func(string) (string, error) { return "/usr/bin/x", nil }
	var out strings.Builder
	if err := preflightRun(&out); err != nil {
		t.Fatalf("tools present should pass: %v", err)
	}
}

func TestPreflightRunFailsWithoutAir(t *testing.T) {
	old := lookPath
	defer func() { lookPath = old }()
	// make resolves, air does not.
	lookPath = func(name string) (string, error) {
		if name == "make" {
			return "/usr/bin/make", nil
		}
		return "", errors.New("exec: air not in PATH")
	}
	err := preflightRun(devNull(t))
	if err == nil || !strings.Contains(err.Error(), "air") {
		t.Fatalf("expected an air-not-found refusal, got %v", err)
	}
	if !strings.Contains(err.Error(), "go install") {
		t.Errorf("refusal should name the fix, got %q", err.Error())
	}
}

func TestRunFlagParses(t *testing.T) {
	opt, _, err := loadOptions(
		[]string{"--name", "my-app", "--no-tui", "--run", "--dry-run"},
		devNull(t), devNull(t))
	if err != nil {
		t.Fatalf("loadOptions: %v", err)
	}
	if !opt.run {
		t.Error("--run should set options.run")
	}
}
