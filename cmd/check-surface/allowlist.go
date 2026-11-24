// Allowlist read/write for check-surface: the baseline of accepted dead
// exports, kept as text so accepting one is a line a reviewer reads.
package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// allowHeader documents the file it is written to. The allowlist is text, one
// entry per line, sorted: a baseline a reviewer can read in a diff is the
// whole point of shipping it.
//
// Entries carry the module-relative path, not the import path. This module is a
// template that `scripts/rename-project.py` rewrites wholesale; a baseline
// spelled with the full module path would break on every rename and report a
// diff nobody caused.
const allowHeader = `# check-surface allowlist — exported names with no consumer outside
# their own package, accepted as debt. One module-relative entry per line.
#
# Accepting a dead export is a decision a reviewer should see, which is why this
# is text and not a hash. An entry that stops matching is an ERROR, not
# tolerance: otherwise a removed export could return unnoticed.
#
# Regenerate with: go run ./cmd/check-surface -update-allow
`

// allowFileMode is the baseline file mode: readable by the owner only, since a
// baseline is a working document, not a published artifact.
const allowFileMode = 0o600

// staleAllow names allowlist entries that no longer match a dead export: the
// symbol was removed, unexported, or finally consumed. Silently tolerating
// them would let a deleted-then-restored export slip through.
func staleAllow(allow []string, findings []finding) []string {
	dead := deadNames(findings)
	var out []string
	for _, a := range allow {
		if !slices.Contains(dead, a) {
			out = append(out, a)
		}
	}
	sort.Strings(out)
	return out
}

// readAllow returns allowlist entries as full import paths, re-attaching the
// module prefix the file leaves out (see allowHeader).
func readAllow(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, modulePath+"/"+strings.TrimPrefix(line, modulePath+"/"))
	}
	sort.Strings(out)
	return out, nil
}

func writeAllow(path string, names []string) error {
	var b strings.Builder
	b.WriteString(allowHeader)
	for _, n := range names {
		b.WriteString(strings.TrimPrefix(n, modulePath+"/"))
		b.WriteString("\n")
	}
	return os.WriteFile(filepath.Clean(path), []byte(b.String()), allowFileMode)
}
