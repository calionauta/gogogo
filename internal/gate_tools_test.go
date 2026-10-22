// SCOPE:core — the gate tools are pinned, never downloaded at gate time.
//
// Why this file exists: govulncheck was installed with
// `go install golang.org/x/vuln/cmd/govulncheck@latest` in three gate paths
// (CI's checks job, the desktop workflow, and the pre-push script) while the
// repo's own AGENTS.md forbids exactly that for gates — "NO `go install
// <tool>@latest` for the gates: external tools are pinned by the `tool`
// directive in go.mod". A security scanner whose binary is fetched unpinned
// at gate time is an unpinned attack surface inside the step that decides
// whether a push is safe, and a scanner older than the toolchain can misread
// the module graph — deadcode once reported type-check errors as dead code
// that way.
//
// The rule, from here on:
//
//   - a gate tool's version lives in ONE place — go.mod's `tool` directive;
//   - it is invoked as `go tool <name>`, so it can never disagree with the
//     toolchain about which module it is scanning;
//   - nothing in a gate path downloads it at run time.
//
// The scanner itself is not the interesting part: the vuln DB is fetched live,
// so a scanner's freshness comes from the network, not from its version. What
// matters is that the CODE PATH is pinned, not that it is new.
package internal_test

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const (
	// govulncheckModule is the scanner's module path — the `tool` directive
	// entry that makes `go tool govulncheck` resolvable.
	govulncheckModule = "golang.org/x/vuln/cmd/govulncheck"

	// govulncheckTool is the name `go tool` resolves it under.
	govulncheckTool = "govulncheck"
)

// goInstallRE captures a `go install` invocation and the module ref it names.
// The ref may carry flags before it and be quoted, so both are tolerated.
var goInstallRE = regexp.MustCompile(`go install\s+(?:-\S+\s+)?("[^"]+"|'[^']+'|[^\s#]+)`)

// floatingRefs are the version refs that name a moving target. A gate that
// installs one of these runs code it did not review, decided by whoever
// published last.
var floatingRefs = map[string]bool{
	"latest": true,
	"master": true,
	"main":   true,
	"HEAD":   true,
}

// TestGateToolsAreVersionPinned fails when a gate path installs a tool
// without an explicit, non-floating version.
//
// A ref written as a shell variable is accepted only when the same file
// assigns it a literal: `@$GITLEAKS_VERSION` is pinned, `@$FLOATER` where
// FLOATER=latest is not, and the difference is invisible until the gate runs.
func TestGateToolsAreVersionPinned(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	for _, path := range gatePaths(t, root) {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("rel %s: %v", path, err)
		}
		body := readFile(t, root, rel)

		for _, m := range goInstallRE.FindAllStringSubmatch(body, -1) {
			ref := strings.Trim(m[1], `"'`)
			ver, ok := versionOf(ref)
			if !ok {
				t.Errorf("%s installs %s with no version — the gate would run "+
					"whatever the network served that minute. Pin it, or move it "+
					"to go.mod's `tool` directive and invoke it as `go tool <name>`.",
					rel, ref)
				continue
			}
			if floatingRefs[ver] {
				t.Errorf("%s installs %s@%s — a floating ref. The gate runs code "+
					"nobody reviewed. Pin it, or move it to go.mod's `tool` "+
					"directive and invoke it as `go tool <name>`.", rel, ref, ver)
				continue
			}
			if name, ok := strings.CutPrefix(ver, "$"); ok {
				if literal, ok := literalAssignment(body, name); !ok {
					t.Errorf("%s installs %s@%s, but %s is not assigned a literal "+
						"in this file — the pinned version cannot be verified from "+
						"here. Assign it a constant, or inline the version.", rel, ref, ver, name)
				} else if floatingRefs[literal] {
					t.Errorf("%s installs %s@%s, and %s=%s — a floating ref behind "+
						"a variable.", rel, ref, ver, name, literal)
				}
			}
		}
	}
}

// TestGovulncheckIsPinnedAsAGoTool fails when the scanner is not declared in
// go.mod's `tool` directive, or when a gate path installs it anyway.
//
// The two halves are one rule: the scanner's version has exactly one source
// of truth. A `tool` directive entry plus an `install` in a workflow would be
// two, and they would drift — the workflow would keep running the version it
// downloaded while go.mod claimed a different one.
func TestGovulncheckIsPinnedAsAGoTool(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	if !slices.Contains(toolDirectiveEntries(t, root), govulncheckModule) {
		t.Errorf("go.mod's `tool` directive does not declare %s — the scanner has "+
			"no pinned version, so every gate that runs it downloads whatever the "+
			"network serves. Add it with `go get -tool %s@<version>`.",
			govulncheckModule, govulncheckModule)
	}

	for _, path := range gatePaths(t, root) {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("rel %s: %v", path, err)
		}
		body := readFile(t, root, rel)
		if strings.Contains(body, govulncheckModule) && strings.Contains(body, "go install") {
			for _, m := range goInstallRE.FindAllStringSubmatch(body, -1) {
				if !strings.Contains(strings.Trim(m[1], `"'`), govulncheckModule) {
					continue
				}
				t.Errorf("%s installs the scanner with `go install` — the pinned "+
					"version lives in go.mod's `tool` directive, so install it from "+
					"there instead and run it as `go tool %s`. Two sources of truth "+
					"for one version is how they drift.", rel, govulncheckTool)
			}
		}
	}
}

// TestGatePathsRunGovulncheckPinned fails when a gate path RUNS the scanner
// without the pinned form.
//
// Naming the tool is not the requirement — a comment may mention it freely,
// and an `echo` may print its name — but a gate that invokes it must invoke
// `go tool govulncheck`. A bare `govulncheck` resolves from PATH, which is
// exactly the unpinned binary this whole change exists to remove.
//
// The check is per LINE, not per file: a comment that says "go tool
// govulncheck, not govulncheck" would otherwise mask a bare invocation in the
// same file, which is how the first version of this rule passed a red-proof
// it was supposed to fail.
func TestGatePathsRunGovulncheckPinned(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	for _, path := range gatePaths(t, root) {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("rel %s: %v", path, err)
		}
		body := readFile(t, root, rel)

		for i, line := range strings.Split(body, "\n") {
			if isCommentLine(line) {
				continue
			}
			if strings.Contains(line, "go tool "+govulncheckTool) {
				continue // the sanctioned form
			}
			if !invokesTool(line, govulncheckTool) {
				continue
			}
			t.Errorf("%s:%d runs %s without `go tool` — a bare invocation "+
				"resolves from PATH, which is the unpinned binary this gate is "+
				"supposed to avoid. Use `go tool %s`.",
				rel, i+1, govulncheckTool, govulncheckTool)
		}
	}
}

// isCommentLine reports whether a line is a comment (shell `#`, or a YAML
// comment). A comment may name any tool it likes.
func isCommentLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//")
}

// invokesTool reports whether a line uses the tool AS A COMMAND: as the
// first word, or as a path ending in the tool's name (`…/bin/govulncheck`).
//
// It deliberately does not count a mention inside an `echo` argument or a
// `command -v` probe — those print or test for the name, they do not run it.
func invokesTool(line, tool string) bool {
	trimmed := strings.TrimSpace(line)

	// First word is the tool: `govulncheck $PKGS`, `"$BIN/govulncheck"`.
	first := trimmed
	if i := strings.IndexAny(first, " \t"); i >= 0 {
		first = first[:i]
	}
	first = strings.Trim(first, `"'`)
	if first == tool {
		return true
	}

	// A path to the tool: `$(go env GOPATH)/bin/govulncheck $PKGS`.
	return strings.Contains(trimmed, "/"+tool)
}

// gatePaths lists every file that runs a gate: the CI workflows and the
// check scripts the git hooks call. A gate outside this set is a gate nobody
// audits, so the set is deliberately broad — any script named check-*.sh and
// every workflow file.
func gatePaths(t *testing.T, root string) []string {
	t.Helper()

	workflows, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatalf("glob workflows: %v", err)
	}
	scripts, err := filepath.Glob(filepath.Join(root, "bin", "check-*.sh"))
	if err != nil {
		t.Fatalf("glob check scripts: %v", err)
	}

	out := make([]string, 0, len(workflows)+len(scripts))
	out = append(out, workflows...)
	out = append(out, scripts...)

	if len(out) == 0 {
		t.Fatal("no gate paths found — the glob is wrong, and every rule below " +
			"would pass vacuously")
	}
	slices.Sort(out)
	return out
}

// versionOf splits a module ref into its version part. The module path cannot
// contain "@", so the last one separates it.
func versionOf(ref string) (string, bool) {
	at := strings.LastIndex(ref, "@")
	if at < 0 || at == len(ref)-1 {
		return "", false
	}
	return ref[at+1:], true
}

// literalAssignment finds `NAME=<literal>` in a file body and returns the
// literal. A second assignment of the same name is reported as not literal:
// two constants for one version is already drift.
func literalAssignment(body, name string) (string, bool) {
	re := regexp.MustCompile(`(?m)^\s*(?:export\s+)?` + regexp.QuoteMeta(name) + `=["']?([^"'\s]+)["']?\s*$`)
	matches := re.FindAllStringSubmatch(body, -1)
	if len(matches) != 1 {
		return "", false
	}
	val := matches[0][1]
	if strings.HasPrefix(val, "$") {
		return "", false
	}
	return val, true
}

// toolDirectiveEntries returns the module paths declared in go.mod's `tool`
// directive block.
func toolDirectiveEntries(t *testing.T, root string) []string {
	t.Helper()
	body := readFile(t, root, "go.mod")

	_, after, ok := strings.Cut(body, "\ntool (")
	if !ok {
		t.Fatalf("go.mod has no `tool (...)` block — the directive that pins the "+
			"gate tools is gone:\n%s", body)
	}
	entries, _, ok := strings.Cut(after, "\n)")
	if !ok {
		t.Fatalf("go.mod's `tool (...)` block is not closed:\n%s", after)
	}

	var out []string
	for line := range strings.SplitSeq(entries, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}
