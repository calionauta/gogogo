package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/prompts"
)

// writeCandidate writes one prompt file into a candidate tree, with the given
// version, variable list and body.
func writeCandidate(t *testing.T, dir, id string, version int, variables []string, body string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(id+".prompt"))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := fmt.Sprintf("---\nname: %s\ndescription: \"candidate fixture\"\nversion: %d\nvariables: [%s]\n---\n%s\n",
		id, version, strings.Join(variables, ", "), body)
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// todoListPrompt returns the library's copy of the reference panel prompt,
// which is what the fixtures compare against.
func todoListPrompt(t *testing.T) prompts.Prompt {
	t.Helper()
	p, err := prompts.Get("panels/todo-list")
	if err != nil {
		t.Fatalf("prompts.Get(panels/todo-list) = %v", err)
	}
	return p
}

// TestOverrideGateAcceptsACompatibleCandidate pins the happy path: an unchanged
// copy passes, so pointing the gate at a full candidate tree is not noise.
func TestOverrideGateAcceptsACompatibleCandidate(t *testing.T) {
	const id = "panels/todo-list"
	p := todoListPrompt(t)
	dir := t.TempDir()
	writeCandidate(t, dir, id, p.Version, p.Variables, p.Body)

	problems, checked := checkOverrideTree(dir)
	if len(problems) != 0 {
		t.Fatalf("problems = %v, want none", problems)
	}
	if checked != 1 {
		t.Errorf("checked = %d, want 1", checked)
	}
}

// TestOverrideGateAcceptsABumpedRewrite is the optimizer's normal move: change
// the body, bump the version.
func TestOverrideGateAcceptsABumpedRewrite(t *testing.T) {
	const id = "panels/todo-list"
	p := todoListPrompt(t)
	dir := t.TempDir()
	writeCandidate(t, dir, id, p.Version+1, p.Variables, p.Body+" Please answer briefly.")

	problems, _ := checkOverrideTree(dir)
	if len(problems) != 0 {
		t.Fatalf("problems = %v, want none", problems)
	}
}

// TestOverrideGateRejectsAnUnbumpedRewrite is the invariant the version field
// exists for: a changed body under the old number makes the two evaluations
// incomparable.
func TestOverrideGateRejectsAnUnbumpedRewrite(t *testing.T) {
	const id = "panels/todo-list"
	p := todoListPrompt(t)
	dir := t.TempDir()
	writeCandidate(t, dir, id, p.Version, p.Variables, p.Body+" Please answer briefly.")

	problems, _ := checkOverrideTree(dir)
	if !hasProblem(problems, "version stayed") {
		t.Fatalf("problems = %v, want a version-bump complaint", problems)
	}
}

// TestOverrideGateRejectsChangedVariables proves the candidate cannot rename a
// variable: the call site passes the library's names, so a rename is a broken
// render rather than an experiment.
func TestOverrideGateRejectsChangedVariables(t *testing.T) {
	const id = "panels/todo-list"
	p := todoListPrompt(t)
	dir := t.TempDir()
	writeCandidate(t, dir, id, p.Version+1, []string{"scope"}, p.Body)

	problems, _ := checkOverrideTree(dir)
	if !hasProblem(problems, "variables") {
		t.Fatalf("problems = %v, want a variables complaint", problems)
	}
}

// TestOverrideGateRejectsUnknownAndInvalidIDs covers the two mechanical
// failures: a candidate that invents an id no call site loads, and a file that
// does not parse at all.
func TestOverrideGateRejectsUnknownAndInvalidIDs(t *testing.T) {
	dir := t.TempDir()
	writeCandidate(t, dir, "panels/invented", 1, nil, "Do the thing.")
	if err := os.WriteFile(filepath.Join(dir, "broken.prompt"), []byte("not a prompt"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	problems, _ := checkOverrideTree(dir)
	if !hasProblem(problems, "no embedded prompt") {
		t.Errorf("problems = %v, want an unknown-id complaint", problems)
	}
	if !hasProblem(problems, "broken.prompt") {
		t.Errorf("problems = %v, want a parse complaint", problems)
	}
}

// hasProblem reports whether one problem message contains want.
func hasProblem(problems []string, want string) bool {
	for _, p := range problems {
		if strings.Contains(p, want) {
			return true
		}
	}
	return false
}
