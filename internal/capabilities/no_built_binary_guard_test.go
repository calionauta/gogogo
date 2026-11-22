// Repo hygiene guard: a built binary is not source.
//
// `make check-classes`, `make check-typed-states` and `make prompt-eval` write
// a binary at the repository root (`go build -o <name>`), and for a long time
// three of them were TRACKED — 4 to 7 MB each, committed by an unrelated
// feature PR and never noticed. The cost is not disk: it is that every clone
// carries a stale copy of the gate that will disagree with the source the
// moment the source changes, and nobody rebuilds a file git says is clean.
//
// The rule is mechanical, so it lives in a test rather than in review
// attention: a tracked file at the repository root that is executable, has no
// extension, and is not a known script is a build artifact that escaped.
//
// Scripts are the exception, and they are named explicitly. A new script at the
// root must be added here — which is the point: the list is short, reviewed, and
// did not grow by accident.
package capabilities

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// rootScripts are the executable, extensionless files at the repository root
// that ARE source. `make setup` installs .githooks; the rest are invoked by the
// Makefile or by hand.
var rootScripts = map[string]bool{
	"init.sh": true,
}

// TestNoBuiltBinaryAtTheRoot asks GIT, not the filesystem.
//
// The filesystem is the wrong question: `make` leaves a binary at the root in
// every developer's checkout, and none of those is a problem. What is a problem
// is the same file TRACKED — which is what happened, three times, unnoticed.
// `git ls-files` is the only view that answers it, and it is available in CI
// (the shallow checkout has an index) as well as locally.
func TestNoBuiltBinaryAtTheRoot(t *testing.T) {
	root := repoRoot(t)
	tracked := trackedRootFiles(t, root)
	// gitignoreList is what keeps a FUTURE binary out. A guard that only
	// reported the escape would let the next `make check-*` re-add it, so this
	// also asserts the ignore rule exists for every gate that builds at the
	// root.
	ignored := readGitignore(t, root)
	for _, name := range tracked {
		if strings.Contains(name, ".") {
			continue // source, config, or a script with an extension
		}
		if rootScripts[name] {
			continue
		}
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil || info.Mode()&0o111 == 0 {
			continue // not executable: a plain root-level file, not a binary
		}
		t.Errorf("%s is an executable file at the repository root and is not a known script; "+
			"a built binary is not source — remove it from the index (git rm --cached %s) "+
			"and add /%s to .gitignore", name, name, name)
	}

	// The gates that build at the root must be ignored, or the escape comes
	// back on the next run.
	for _, built := range rootBuiltGates() {
		if !ignored[built] {
			t.Errorf(".gitignore must list /%s: make builds it at the root, so without the "+
				"rule it is one `git add -A` away from being committed", built)
		}
	}
}

// trackedRootFiles is the tracked, root-level, extensionless paths — the files
// git says are part of the repository.
func trackedRootFiles(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "-C", root, "ls-files", "--", ":/")
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("git ls-files unavailable (%v): this guard needs an index", err)
	}
	var files []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if line == "" || strings.Contains(line, "/") {
			continue // nested: never a root artifact
		}
		files = append(files, line)
	}
	return files
}

// rootBuiltGates is every `go build -o <name>` target the Makefile writes at the
// repository root. Named here so adding a gate is a two-line change with a test
// that says so.
func rootBuiltGates() []string {
	return []string{
		"check-classes",
		"check-typed-states",
		"prompt-eval",
	}
}

// readGitignore returns the root-level rules as a set of names without the
// leading slash, so `/check-classes` reads as `check-classes`.
func readGitignore(t *testing.T, root string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out[strings.TrimPrefix(line, "/")] = true
	}
	return out
}
