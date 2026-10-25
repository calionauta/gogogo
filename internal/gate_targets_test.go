// SCOPE:core — the Makefile's targets are a contract, not a convenience list.
//
// CI calls them, the git hooks call them, and AGENTS.md tells a human to call
// them — while a MISSING target is invisible: `make <missing>` prints "Nothing
// to be done" and exits 0.
//
// Not hypothetical: commit 70706d9 (a scripted edit) lost 44 targets at once —
// ci-local, ci-local-fast, signoff, smoke, site, sqlc, setup, help, docs-index,
// check-docs, check-flatdeps, check-secrets-env, cover-diff, the lot. CI kept
// calling the underlying Go commands and scripts directly, and the pre-commit
// hooks did too, so every gate still ran where it mattered; what broke was the
// surface a human and AGENTS.md use. `make ci-local` and `make signoff` became
// no-ops — a signoff that stamps a commit green without running a test — and
// `make smoke`, `make site`, `make sqlc`, `make setup`, `make help` all exited 0
// doing nothing. A command that reports success without work is worse than a
// missing one.
package internal_test

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	makePhonyRE  = regexp.MustCompile(`(?m)^\.PHONY:(.*)$`)
	makeTargetRE = regexp.MustCompile(`(?m)^([a-zA-Z0-9_.-]+):`)

	// A CI step that runs a make target: `run: make x` or a bare `make x` line
	// inside a `run: |` block. Anchored on both sides so prose that merely
	// mentions a target (a comment explaining "same script as make check-mcp")
	// is not read as an invocation.
	ciMakeRE = regexp.MustCompile(`(?m)^\s*(?:-\s*)?(?:run:\s*)?make\s+([a-zA-Z0-9_-]+)\s*$`)
)

// makeTargets parses the rules defined in the Makefile.
func makeTargets(t *testing.T, root string) map[string]bool {
	t.Helper()
	src := readFile(t, root, "Makefile")
	out := map[string]bool{}
	for _, m := range makeTargetRE.FindAllStringSubmatch(src, -1) {
		out[m[1]] = true
	}
	if len(out) == 0 {
		t.Fatal("parsed no rules from Makefile — a truncated file would look like this")
	}
	return out
}

// TestEveryPhonyTargetHasARule catches the truncated-Makefile shape directly:
// the .PHONY list survives a scripted edit that removes recipes, so the
// declaration is a built-in checklist of what should exist.
func TestEveryPhonyTargetHasARule(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	src := readFile(t, root, "Makefile")
	m := makePhonyRE.FindStringSubmatch(src)
	if m == nil {
		t.Fatal("no .PHONY declaration in Makefile")
	}
	targets := makeTargets(t, root)

	var missing []string
	for name := range strings.FieldsSeq(m[1]) {
		if !targets[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf(".PHONY names with no rule: %v — `make <name>` exits 0 doing nothing", missing)
	}
}

// TestEveryCITargetExists keeps a future workflow step honest: a step that calls
// `make x` must have an `x` to run, or the step passes without doing its job.
func TestEveryCITargetExists(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	targets := makeTargets(t, root)

	workflows, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatalf("glob workflows: %v", err)
	}
	if len(workflows) == 0 {
		t.Fatal("no workflow files found")
	}

	for _, wf := range workflows {
		rel, rerr := filepath.Rel(root, wf)
		if rerr != nil {
			t.Fatalf("rel %s: %v", wf, rerr)
		}
		src := readFile(t, root, rel)
		for _, m := range ciMakeRE.FindAllStringSubmatch(src, -1) {
			if !targets[m[1]] {
				t.Errorf("%s runs `make %s`, which no rule defines — the step "+
					"would pass without running anything", rel, m[1])
			}
		}
	}
}

// TestDocumentedCommandsExist pins the commands AGENTS.md hands a human. If a
// target is renamed, the test forces the doc and the Makefile to move together.
func TestDocumentedCommandsExist(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	targets := makeTargets(t, root)

	want := []string{
		// AGENTS.md Commands table.
		"dev", "build", "templ", "datastar-lint", "css", "css-check",
		"check-scope", "test", "check-generated", "ci-local-fast", "lint-safe",
		"ci-local", "signoff", "setup", "go-bump",
		// The class gates the repo publishes (Makefile targets, not scripts).
		"check-classes", "daisyui-lint", "vocabulary",
	}
	for _, name := range want {
		if !targets[name] {
			t.Errorf("documented command `make %s` has no rule", name)
		}
	}
}
