// SCOPE:core — the bump surface and the version-token rewrite.
//
// Two rules live here, and both exist because the pin test cannot see a
// partial rewrite: it asks "does this file carry the version?", so a file that
// moved one mention and kept another still passes it.
//
//  1. Every file in the surface must exist and must still name the version. A
//     tool that silently skips a file it cannot find is the #191 bug with a
//     nicer name: deploy/Dockerfile.prod stayed on `golang:1.27-alpine` while
//     go.mod declared `go 1.27.3`, so prod could be built by a toolchain older
//     than the tree requires.
//  2. Every OCCURRENCE in a file must move. "Go 1.27.3 or newer" in one place
//     and "at least 1.27.3" in another satisfies rule 1 while the file
//     contradicts itself.
//
// This list says what to WRITE. internal/toolchain_pin_test.go says what to
// VERIFY, and keeps its own list on purpose: one shared list means a file that
// drops out drops out of both, and the net stops being a net.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// fileMode is the mode a rewritten file keeps. os.WriteFile only applies it
// when it CREATES a file, so an executable script stays executable.
const fileMode = 0o644

var surface = []struct {
	Path string
	Why  string
}{
	{goModPath, "the authoritative pin"},
	{"cmd/gogogo-mcp/go.mod", "the second module builds in the same CI job"},
	{"bin/install-go-toolchain.sh", "the toolchain every CI job installs"},
	{"Dockerfile", "the dev/desktop image builder"},
	{"deploy/Dockerfile.prod", "the production image builder promoted by digest"},
	{"skills/gogogo-coding-standards/SKILL.md", "the skill tells agents which toolchain builds the tree"},
	{"docs/troubleshooting.md", "the troubleshooting page names the tree's version"},
	{"internal/toolchain_pin_test.go", "the pin test documents the surface it checks"},
}

// bump rewrites every file of the surface from old to new, and returns the
// paths it changed.
//
// Two failures, both of which the pin test cannot see:
//
//   - a file of the surface does not exist, or no longer mentions the old
//     version: the tool's list and the tree have drifted, and a bump that
//     skips a file is exactly the bug this tool exists to prevent;
//   - a file carries MORE occurrences than the rewrite moved. A file that says
//     "Go 1.27.3 or newer" in one place and "at least 1.27.3" in another must
//     move BOTH, or it ends up contradicting itself. The pin test only asks
//     whether the file carries the version SOMEWHERE, so it would pass.
//
// Every file is checked before ANY is written. The first implementation wrote
// as it went, so a drift detected on the last file left the first ones already
// rewritten: go.mod on the new version, the Dockerfile still on the old one,
// and the pin test red with no hint that the bump tool did it. All-or-nothing
// makes "it failed" mean "the tree is unchanged".
func bump(root, old, new string, dryRun bool) ([]string, error) {
	type pending struct {
		path string
		rel  string
		body []byte
	}
	var writes []pending

	for _, f := range surface {
		path := filepath.Join(root, f.Path)
		// #nosec G703 — root is --root or the checkout this maintainer tool is
		// run from, and the file list is a literal table above; no request or
		// user input reaches this path.
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w — the surface drifted: this file is in the bump "+
				"tool's list but is not in the tree", f.Path, err)
		}
		before := countVersionTokens(string(body), old)
		if before == 0 {
			return nil, fmt.Errorf("%s does not mention Go %s — the surface drifted: this file "+
				"is in the bump tool's list but no longer carries the version", f.Path, old)
		}
		out, moved := replaceVersionToken(string(body), old, new)
		if moved != before {
			return nil, fmt.Errorf("%s carries %d occurrence(s) of Go %s but only %d were "+
				"rewritten — a partial rewrite leaves the file contradicting itself, and the "+
				"pin test cannot see it because it only checks that the file carries the "+
				"version somewhere", f.Path, before, old, moved)
		}
		if out == string(body) {
			continue
		}
		writes = append(writes, pending{path: path, rel: f.Path, body: []byte(out)})
	}

	changed := make([]string, 0, len(writes))
	for _, w := range writes {
		changed = append(changed, w.rel)
	}
	sort.Strings(changed)

	if dryRun {
		return changed, nil
	}

	for _, w := range writes {
		// #nosec G703 — same path as the read above: --root plus a literal
		// table entry, never request or user input.
		if err := os.WriteFile(w.path, w.body, fileMode); err != nil {
			return nil, fmt.Errorf("write %s: %w", w.rel, err)
		}
	}
	return changed, nil
}

// countVersionTokens counts how many times s mentions the version as a whole
// token.
func countVersionTokens(s, v string) int {
	if v == "" {
		return 0
	}
	count := 0
	for i := 0; i+len(v) <= len(s); i++ {
		if s[i:i+len(v)] != v {
			continue
		}
		if wholeVersionToken(s, i, i+len(v)) {
			count++
		}
	}
	return count
}

// verify re-reads the surface and confirms the rewrite landed everywhere:
// every file carries the new version and none carries the old one.
func verify(root, old, new string) error {
	for _, f := range surface {
		body, err := os.ReadFile(filepath.Join(root, f.Path))
		if err != nil {
			return fmt.Errorf("verify %s: %w", f.Path, err)
		}
		s := string(body)
		if !containsVersionToken(s, new) {
			return fmt.Errorf("%s does not carry Go %s after the bump — the rewrite did not land",
				f.Path, new)
		}
		if containsVersionToken(s, old) {
			return fmt.Errorf("%s still carries Go %s after the bump", f.Path, old)
		}
	}
	return nil
}

// replaceVersionToken replaces every occurrence of old that is a WHOLE
// version token, so "1.27.3" never rewrites the middle of "1.27.30" and
// "1.27.2" is left alone when the old version is "1.27.3".
func replaceVersionToken(s, old, new string) (string, int) {
	if old == "" {
		return s, 0
	}
	var b strings.Builder
	count := 0
	for i := 0; i < len(s); {
		idx := strings.Index(s[i:], old)
		if idx < 0 {
			b.WriteString(s[i:])
			break
		}
		start := i + idx
		end := start + len(old)
		b.WriteString(s[i:start])
		if wholeVersionToken(s, start, end) {
			b.WriteString(new)
			count++
		} else {
			b.WriteString(old)
		}
		i = end
	}
	return b.String(), count
}

// containsVersionToken reports whether s mentions the version as a whole
// token.
func containsVersionToken(s, v string) bool {
	for i := 0; i+len(v) <= len(s); i++ {
		if s[i:i+len(v)] != v {
			continue
		}
		if wholeVersionToken(s, i, i+len(v)) {
			return true
		}
	}
	return false
}

// wholeVersionToken reports whether s[start:end] is a complete version token
// rather than part of a LONGER version number.
//
// A digit next to the token means it continues a version. A DOT next to it
// only means that when a digit follows the dot — a dot followed by a space is
// a sentence's full stop. Treating that dot as part of the number is how
// "assumes at least 1.27.3." survived a bump untouched while the file's
// other lines moved: the pin test still passed, because it only checks that
// the file carries the version SOMEWHERE.
func wholeVersionToken(s string, start, end int) bool {
	if start > 0 {
		if isDigit(s[start-1]) {
			return false
		}
		if s[start-1] == '.' && start >= 2 && isDigit(s[start-2]) {
			return false
		}
	}
	if end < len(s) {
		if isDigit(s[end]) {
			return false
		}
		if s[end] == '.' && end+1 < len(s) && isDigit(s[end+1]) {
			return false
		}
	}
	return true
}

// isDigit reports whether b is an ASCII digit.
func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
