package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeModule lays down a minimal module: one package with a named string type
// and one file whose comparisons the gate reads. Only types matter — the gate
// parses and type-checks, it does not build.
func writeModule(t *testing.T, root string, files map[string]string) {
	t.Helper()
	files["go.mod"] = "module github.com/calionauta/gogogo/fixture\n\ngo 1.27\n"
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

const stateType = `package fixture

type State string

const (
	Installed State = "installed"
	Removed   State = "removed"
)
`

// TestComparisonAgainstLiteralIsReported is the rule's reason to exist: a
// literal comparison compiles, so only a type-aware gate can see it.
func TestComparisonAgainstLiteralIsReported(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, map[string]string{
		"state.go": stateType,
		"use.go": `package fixture

func Check(s State) bool {
	return s == "removed"
}
`,
	})
	problems, err := check(root)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(problems) != 1 {
		t.Fatalf("problems = %v, want exactly one", problems)
	}
	if !strings.Contains(problems[0], `compared with "removed"`) {
		t.Errorf("problem = %q, want it to name the literal", problems[0])
	}
	if !strings.Contains(problems[0], "State's constants") {
		t.Errorf("problem = %q, want it to name the fix", problems[0])
	}
}

// TestSwitchCaseAgainstLiteralIsReported covers the other spelling: a switch on
// a state whose cases are literals is the same mistake, and it is the one that
// fails silently by falling through.
func TestSwitchCaseAgainstLiteralIsReported(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, map[string]string{
		"state.go": stateType,
		"use.go": `package fixture

func Describe(s State) string {
	switch s {
	case "installed":
		return "yes"
	case Removed:
		return "no"
	}
	return ""
}
`,
	})
	problems, err := check(root)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(problems) != 1 {
		t.Fatalf("problems = %v, want exactly one (the literal case only)", problems)
	}
	if !strings.Contains(problems[0], `"installed"`) {
		t.Errorf("problem = %q, want the literal case", problems[0])
	}
}

// TestConstantsAndOtherTypesPass pins the false-positive side, so the gate
// cannot be "satisfied" by banning comparisons altogether.
func TestConstantsAndOtherTypesPass(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, map[string]string{
		"state.go": stateType,
		"use.go": `package fixture

type Plain string

func Check(s State, p Plain, raw string) bool {
	if s == Removed {
		return true
	}
	// A plain string field is not a state; the gate is about named types.
	if raw == "removed" {
		return true
	}
	// A named type from another module is not ours to change, and the local
	// declaration of a constant is exactly where literals belong.
	return p == Plain("x")
}
`,
	})
	problems, err := check(root)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("problems = %v, want none", problems)
	}
}
