// SCOPE:core — Tests for the docs claim guard.
package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestCountMatches pins what the count rule does and does not flag: plural
// counts of drifting nouns are banned, ordinary prose ("one unit") is not.
func TestCountMatches(t *testing.T) {
	for _, tc := range []struct {
		text string
		hit  bool
	}{
		{"the 24 capabilities + presets", true},
		{"all nine slices implement it", true},
		{"35 units in the registry", true},
		{"`add` restores one unit back", false},
		{"two roles cover the template", false},
		{"three shared UI leaves", false},
		{"a single slice", false},
	} {
		if got := countRE.MatchString(tc.text); got != tc.hit {
			t.Errorf("countRE.MatchString(%q) = %v, want %v", tc.text, got, tc.hit)
		}
	}
}

// TestDeadPath pins the path rule against the false positives that made the
// first pass noisy: package.Symbol notation, hypothetical files, and
// illustrative segments are not claims about this repo.
func TestDeadPath(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"features/todo/handlers", "internal/auth"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(rel string) {
		t.Helper()
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("features/todo/handlers/chi.go")

	for _, tc := range []struct{ token, want string }{
		{"features/todo", ""},
		{"features/todo/handlers/chi.go", ""},
		{"internal/auth", ""},
		{"features/gone", "features/gone"},
		{"internal/datastar.RenderAndPatch", ""},
		{"web/resources/static/app.min.css.map", ""},
		{"internal/foo/", ""},
		{"features/<name>", ""},
		{"net/http", ""},
	} {
		if got := deadPath(root, tc.token, nil); got != tc.want {
			t.Errorf("deadPath(%q) = %q, want %q", tc.token, got, tc.want)
		}
	}
}

// TestTrimmedPaths pins the scaffold exemption: a generated project keeps
// the template's docs while whole units are gone, so a path recorded in
// AGENTS.md's provenance is not a dead claim (it was removed on purpose).
func TestTrimmedPaths(t *testing.T) {
	root := t.TempDir()
	agents := "# app\n\n## Trim provenance\n\nremoved-paths: features/landing,features/whiteboard,internal/goakt\n"
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(agents), 0o600); err != nil {
		t.Fatal(err)
	}
	trimmed := trimmedPaths(root)
	for _, path := range []string{"features/landing", "features/landing/components", "internal/goakt"} {
		if !trimmedPath(trimmed, path) {
			t.Errorf("trimmedPath(%q) = false, want true", path)
		}
		if got := deadPath(root, path, trimmed); got != "" {
			t.Errorf("deadPath(%q) flagged a deliberately trimmed path: %q", path, got)
		}
	}
	if trimmedPath(trimmed, "features/todo") {
		t.Error("trimmedPath(features/todo) = true, want false (it still exists)")
	}
	if got := deadPath(root, "features/todo", trimmed); got != "features/todo" {
		t.Errorf("deadPath(features/todo) = %q, want it flagged (missing, not trimmed)", got)
	}

	// No provenance section (the full template) is strict.
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# app\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(trimmedPaths(root)) != 0 {
		t.Error("trimmedPaths without a provenance section must be empty")
	}
}

// TestStaleScopeDialect keeps the retired two-axis annotation out of prose:
// docs taught it while `make check-scope` rejects it.
func TestStaleScopeDialect(t *testing.T) {
	root := t.TempDir()
	rel := "doc.md"
	body := "# doc\n\n- `// SCOPE:layer=feature,removal=core` on new files\n- `// SCOPE:plugin` is fine\n"
	if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got := checkStaleScopeDialect(root, []string{rel})
	if len(got) != 1 {
		t.Fatalf("checkStaleScopeDialect = %v, want one offender", got)
	}
}

// TestCommandMatches proves the rule that caught the invented `gogogo trim`
// subcommand.
func TestCommandMatches(t *testing.T) {
	for _, tc := range []struct {
		span string
		sub  string
	}{
		{"gogogo trim genui", "trim"},
		{"gogogo new feature reports", "new"},
		{"go run ./cmd/gogogo new feature reports", "new"},
		{"gogogo advise --need x", "advise"},
		{"gogogo --check --dir .", ""},
	} {
		got := ""
		if m := commandRE.FindStringSubmatch(tc.span); m != nil {
			got = m[1]
		}
		if got != tc.sub {
			t.Errorf("commandRE(%q) = %q, want %q", tc.span, got, tc.sub)
		}
	}
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	subs, err := knownSubcommands(root)
	if err != nil {
		t.Fatalf("knownSubcommands: %v", err)
	}
	if slices.Contains(subs, "trim") {
		t.Fatal("premise broken: trim became a subcommand — update the rule and the docs")
	}
	for _, want := range []string{"add", "new", "advise"} {
		if !slices.Contains(subs, want) {
			t.Errorf("subcommand %q missing from the dispatch parser: %v", want, subs)
		}
	}
}

// TestRunGreenOnThisTree is the integration half: the repo's own docs pass
// the guard (counts generated, paths alive, commands real, all pages published).
func TestRunGreenOnThisTree(t *testing.T) {
	if err := run(); err != nil {
		t.Fatalf("run() = %v, want nil (docs claims are current)", err)
	}
}
