package gorules

// Shared fixture-module harness for TestRulesFire: one temp module, one
// lint fork, per-file hit attribution. Split from rules_test.go at the
// 500-line file budget; same package.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// assertFixtureHits counts ruleguard diagnostics attributed to one
// fixture file. golangci-lint prints file paths, so sharing a module
// does not blur which fixture fired.
func assertFixtureHits(t *testing.T, out, file string, tc ruleFixture) {
	t.Helper()
	got := 0
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, "ruleguard:") && strings.Contains(line, file+":") {
			got++
		}
	}
	if got < tc.wantAtLeast {
		t.Fatalf("ruleguard hits in %s = %d, want at least %d\nlinter output:\n%s",
			file, got, tc.wantAtLeast, out)
	}
	if tc.wantAtLeast == 0 && got != 0 {
		t.Fatalf("expected no diagnostics on clean code in %s, got %d\n%s", file, got, out)
	}
}

// writeSharedFixtureModule lays out ONE module holding every fixture as
// its own file (subdir-aware, so path-scoped rules keep working) plus
// the copied rules and a minimal golangci config. Returns the file name
// per fixture, aligned with fixtures(), for hit attribution.
func writeSharedFixtureModule(
	t *testing.T, dir string, rulesSrc []byte, dslVersion, goDirective string, fxs []ruleFixture,
) []string {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(dir, "rules"), 0o750); err != nil {
		t.Fatal(err)
	}
	writeIn(t, filepath.Join(dir, "go.mod"),
		"module fixture\n\ngo "+goDirective+"\n\nrequire github.com/quasilyte/go-ruleguard/dsl "+dslVersion+"\n")
	files := make([]string, len(fxs))
	for i, tc := range fxs {
		name := "fixture" + twoDigits(i) + ".go"
		files[i] = name
		target := filepath.Join(dir, name)
		if tc.subdir != "" {
			if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(tc.subdir)), 0o750); err != nil {
				t.Fatal(err)
			}
			target = filepath.Join(dir, filepath.FromSlash(tc.subdir), name)
		}
		writeIn(t, target, tc.source)
	}
	writeIn(t, filepath.Join(dir, "rules", "rules.go"), string(rulesSrc))

	// Mirrors the project's own ruleguard block, minus every other linter, so a
	// fixture's diagnostic count has exactly one source.
	writeIn(t, filepath.Join(dir, "golangci.yml"), `version: "2"
linters:
  enable: [gocritic]
  settings:
    gocritic:
      enabled-checks: [ruleguard]
      settings:
        ruleguard:
          rules: "rules/rules.go"
  exclusions:
    generated: lax
    presets: [comments, common-false-positives, legacy, std-error-handling]
`)
	return files
}

func twoDigits(i int) string {
	if i < 10 {
		return "0" + strconv.Itoa(i)
	}
	return strconv.Itoa(i)
}
