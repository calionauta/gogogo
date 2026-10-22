// SCOPE:core — the bump surface: every file must move, in every occurrence.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureContent is what each file of the surface looks like before a bump.
// Every surface entry needs an entry here: a new file in the surface without
// a fixture fails the test loudly, so the two never drift apart.
var fixtureContent = map[string]string{
	"go.mod":                "module example\n\ngo 1.27.3\n",
	"cmd/gogogo-mcp/go.mod": "module example/mcp\n\ngo 1.27.3\n",
	"bin/install-go-toolchain.sh": "#   GO_VERSION=1.27.3 is the default\n" +
		"VERSION=\"${GO_VERSION:-1.27.3}\"\n",
	"Dockerfile": "#   1. builder  — golang:1.27.3-alpine\n" +
		"FROM golang:1.27.3-alpine AS builder\n",
	"deploy/Dockerfile.prod": "FROM golang:1.27.3-alpine AS builder\n",
	"skills/gogogo-coding-standards/SKILL.md": "This skill requires **Go 1.27.3 or newer**. " +
		"`go 1.27.3`, no `toolchain` pin, and the skill assumes at least 1.27.3. " +
		"Do not write fallback code for older toolchains.\n",
	"docs/troubleshooting.md": "a go1.27.2 toolchain will not build a tree that declares " +
		"`go 1.27.3`\n",
	"internal/toolchain_pin_test.go": "package internal_test\n\n" +
		"// go.mod                                  go 1.27.3        (authoritative)\n",
}

// writeFixture builds a tree with every file of the surface.
func writeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range surface {
		content, ok := fixtureContent[f.Path]
		if !ok {
			t.Fatalf("the surface names %s but the fixture has no content for it — "+
				"add one, so the test and the tool never disagree", f.Path)
		}
		path := filepath.Join(root, f.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", f.Path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", f.Path, err)
		}
	}
	return root
}

// read returns a file of the fixture.
func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// TestBumpMovesEverySurfaceFile is the core promise: one command moves the
// pin EVERYWHERE it is named, so the tree never ships with one file behind.
func TestBumpMovesEverySurfaceFile(t *testing.T) {
	t.Parallel()
	root := writeFixture(t)

	changed, err := bump(root, "1.27.3", "1.27.4", false)
	if err != nil {
		t.Fatalf("bump: %v", err)
	}
	if len(changed) != len(surface) {
		t.Errorf("bump changed %d files, want %d — a file of the surface was skipped",
			len(changed), len(surface))
	}

	for _, f := range surface {
		body := read(t, root, f.Path)
		if !containsVersionToken(body, "1.27.4") {
			t.Errorf("%s does not carry Go 1.27.4 after the bump:\n%s", f.Path, body)
		}
		if containsVersionToken(body, "1.27.3") {
			t.Errorf("%s still carries Go 1.27.3 after the bump:\n%s", f.Path, body)
		}
	}
}

// TestBumpKeepsThePinsInTheirOwnShape guards the SHAPE of each rewrite, not
// just the version: the pin test reads these with specific patterns, so a
// rewrite that keeps the version but breaks the pattern is still a break.
func TestBumpKeepsThePinsInTheirOwnShape(t *testing.T) {
	t.Parallel()
	root := writeFixture(t)

	if _, err := bump(root, "1.27.3", "1.27.4", false); err != nil {
		t.Fatalf("bump: %v", err)
	}

	cases := []struct {
		file string
		want string
	}{
		{"go.mod", "\ngo 1.27.4\n"},
		{"cmd/gogogo-mcp/go.mod", "\ngo 1.27.4\n"},
		{"bin/install-go-toolchain.sh", `GO_VERSION:-1.27.4`},
		{"bin/install-go-toolchain.sh", `GO_VERSION=1.27.4 is the default`},
		{"Dockerfile", "FROM golang:1.27.4-alpine AS builder"},
		{"deploy/Dockerfile.prod", "FROM golang:1.27.4-alpine AS builder"},
		{"skills/gogogo-coding-standards/SKILL.md", "**Go 1.27.4 or newer**"},
		{"skills/gogogo-coding-standards/SKILL.md", "assumes at least 1.27.4."},
		{"docs/troubleshooting.md", "declares `go 1.27.4`"},
		{"internal/toolchain_pin_test.go", "go 1.27.4        (authoritative)"},
	}
	for _, c := range cases {
		if body := read(t, root, c.file); !strings.Contains(body, c.want) {
			t.Errorf("%s lost %q after the bump:\n%s", c.file, c.want, body)
		}
	}
}

// TestBumpMovesEveryOccurrenceNotJustOne is the invariant the pin test
// cannot see. The pin test asks "does this file carry the version?"; a file
// that says "Go 1.27.3 or newer" in one place and "at least 1.27.3" in
// another answers yes while contradicting itself. The first version of the
// rewrite rule had exactly that hole — a sentence's full stop was read as
// part of the number, so "at least 1.27.3." never moved and the pin test
// stayed green.
//
// The counts here are RAW SUBSTRINGS, deliberately not countVersionTokens:
// counting with the same token rule the rewrite uses cannot see a bug in that
// rule, it just agrees with it. That is not a hypothetical — the first version
// of this test counted by token and stayed GREEN under exactly the mutation it
// claims to guard (make a trailing dot part of the number). RED-proof found it;
// a raw count is the independent second opinion.
//
// A longer neighbour ("1.27.30") is the one case a raw count reads as a hit, so
// the fixture carries none; TestBumpLeavesNeighbouringVersionsAlone covers that
// with a token-level assertion instead.
func TestBumpMovesEveryOccurrenceNotJustOne(t *testing.T) {
	t.Parallel()
	root := writeFixture(t)

	before := map[string]int{}
	for _, f := range surface {
		before[f.Path] = strings.Count(read(t, root, f.Path), "1.27.3")
		if before[f.Path] == 0 {
			t.Fatalf("the fixture for %s carries no version — the test would prove nothing",
				f.Path)
		}
	}

	if _, err := bump(root, "1.27.3", "1.27.4", false); err != nil {
		t.Fatalf("bump: %v", err)
	}

	for _, f := range surface {
		body := read(t, root, f.Path)
		if n := strings.Count(body, "1.27.3"); n != 0 {
			t.Errorf("%s still contains the old version %d time(s) — every occurrence must "+
				"move, or the file contradicts itself:\n%s", f.Path, n, body)
		}
		if after := strings.Count(body, "1.27.4"); after != before[f.Path] {
			t.Errorf("%s carried %d occurrence(s) of Go 1.27.3 and now carries %d of Go 1.27.4 — "+
				"every occurrence must move, or the file contradicts itself:\n%s",
				f.Path, before[f.Path], after, body)
		}
	}
}

// TestBumpLeavesNeighbouringVersionsAlone proves the rewrite is a WHOLE
// token: a neighbouring version in the same file must survive, or the tool
// rewrites history.
func TestBumpLeavesNeighbouringVersionsAlone(t *testing.T) {
	t.Parallel()
	root := writeFixture(t)

	neighbour := filepath.Join(root, "docs/troubleshooting.md")
	body, err := os.ReadFile(neighbour)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	extra := "a go1.27.30 toolchain is a different toolchain entirely\n"
	// #nosec G703 — the path is a file this test just wrote inside t.TempDir().
	if err := os.WriteFile(neighbour, append(body, []byte(extra)...), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := bump(root, "1.27.3", "1.27.4", false); err != nil {
		t.Fatalf("bump: %v", err)
	}

	got := read(t, root, "docs/troubleshooting.md")
	if !strings.Contains(got, "go1.27.2") {
		t.Errorf("the older example version go1.27.2 was rewritten:\n%s", got)
	}
	if !strings.Contains(got, "go1.27.30") {
		t.Errorf("the longer version go1.27.30 was rewritten:\n%s", got)
	}
}

// TestBumpPreservesTheFileMode guards the promise fileMode makes in
// surface.go: os.WriteFile only applies the mode when it CREATES a file, so a
// rewrite leaves an executable script executable. A rewrite that dropped the
// bit would make bin/install-go-toolchain.sh non-executable the first time
// someone bumped the pin, and the failure would land in CI, not here.
func TestBumpPreservesTheFileMode(t *testing.T) {
	t.Parallel()
	root := writeFixture(t)

	const script = "bin/install-go-toolchain.sh"
	path := filepath.Join(root, script)
	// 0700, not 0755: the invariant is the executable bit, and a mode with no
	// group/other access keeps the test off gosec's permissive-chmod rule.
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if _, err := bump(root, "1.27.3", "1.27.4", false); err != nil {
		t.Fatalf("bump: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("%s has mode %o after the bump, want 700 — a rewrite must not "+
			"drop the executable bit", script, got)
	}
}

// TestBumpFailsLoudlyWhenTheSurfaceDrifted is the failure that matters: a
// bump tool that skips a file it cannot find is the #191 bug with a nicer
// name.
func TestBumpFailsLoudlyWhenTheSurfaceDrifted(t *testing.T) {
	t.Parallel()
	root := writeFixture(t)

	missing := filepath.Join(root, "deploy/Dockerfile.prod")
	if err := os.Remove(missing); err != nil {
		t.Fatalf("remove: %v", err)
	}

	_, err := bump(root, "1.27.3", "1.27.4", false)
	if err == nil {
		t.Fatal("bump succeeded although a file of the surface is missing — a bump that " +
			"skips a file is the bug this tool exists to prevent")
	}
	if !strings.Contains(err.Error(), "deploy/Dockerfile.prod") {
		t.Errorf("the error does not name the missing file: %v", err)
	}
}

// TestBumpFailsLoudlyWhenAFileNoLongerCarriesTheVersion covers the other
// drift: the file exists but stopped naming the version, so the rewrite
// would silently do nothing there.
//
// The file it breaks is the LAST of the surface, and the assertion that nothing
// carries the NEW version is the all-or-nothing half: the first
// implementation wrote as it looped, so this scenario left go.mod bumped and
// the rest of the surface behind.
func TestBumpFailsLoudlyWhenAFileNoLongerCarriesTheVersion(t *testing.T) {
	t.Parallel()
	root := writeFixture(t)

	stale := surface[len(surface)-1]
	path := filepath.Join(root, stale.Path)
	if err := os.WriteFile(path, []byte("nothing to see here\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := bump(root, "1.27.3", "1.27.4", false)
	if err == nil {
		t.Fatal("bump succeeded although a file of the surface no longer carries the version")
	}
	if !strings.Contains(err.Error(), stale.Path) {
		t.Errorf("the error does not name the stale file: %v", err)
	}
	// Nothing may carry the NEW version: the file this test broke carries
	// neither, so "still on 1.27.3" is the wrong assertion here.
	for _, f := range surface {
		if body := read(t, root, f.Path); containsVersionToken(body, "1.27.4") {
			t.Errorf("a failed bump still rewrote %s — the failure must leave the tree "+
				"untouched:\n%s", f.Path, body)
		}
	}
}

// TestReplaceVersionTokenBoundaries: the token rule, on its own.
func TestReplaceVersionTokenBoundaries(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in       string
		want     string
		wantHits int
	}{
		{"go 1.27.3\n", "go 1.27.4\n", 1},
		{"golang:1.27.3-alpine", "golang:1.27.4-alpine", 1},
		{"${GO_VERSION:-1.27.3}", "${GO_VERSION:-1.27.4}", 1},
		{"1.27.3 and 1.27.3", "1.27.4 and 1.27.4", 2},
		// A sentence's full stop is NOT part of the number: this line is the
		// one that stayed behind in the first implementation.
		{"assumes at least 1.27.3.", "assumes at least 1.27.4.", 1},
		{"version (1.27.3), because", "version (1.27.4), because", 1},
		{"go1.27.30", "go1.27.30", 0},
		{"go1.27.2", "go1.27.2", 0},
		{"11.27.3", "11.27.3", 0},
		{"1.27.3.4", "1.27.3.4", 0},
		{"0.1.27.3", "0.1.27.3", 0},
	}
	for _, c := range cases {
		got, hits := replaceVersionToken(c.in, "1.27.3", "1.27.4")
		if got != c.want || hits != c.wantHits {
			t.Errorf("replaceVersionToken(%q) = %q (%d hits), want %q (%d hits)",
				c.in, got, hits, c.want, c.wantHits)
		}
	}
}
