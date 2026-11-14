package shape

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree lays down the files a case needs. A path ending in "/" is a
// directory; anything else gets body, or `package main` when body is empty.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if strings.HasSuffix(rel, "/") {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", rel, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
		}
		if body == "" {
			body = "package main\n\nfunc main() {}\n"
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}

// TestLoadDefaultsToFull pins the back-compat rule: a tree that never declared
// a shape is the full app, not an error and not an empty declaration.
func TestLoadDefaultsToFull(t *testing.T) {
	root := t.TempDir()
	c, err := Load(root)
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if c.Shape != Full || c.Declared {
		t.Fatalf("Load() = %+v, want full and undeclared", c)
	}
	if len(c.Entrypoints) != len(legalEntrypoints[Full]) {
		t.Fatalf("entrypoints = %v, want the full set", c.Entrypoints)
	}
}

// TestParseRejectsUnknownShape proves the enum is closed: a typo cannot
// silently mean "full", which would ship every target.
func TestParseRejectsUnknownShape(t *testing.T) {
	if _, err := Parse(Config{Shape: "webapp"}); err == nil {
		t.Fatal("Parse(webapp) = nil error, want rejection")
	}
}

// TestParseRejectsIllegalEntrypoint pins "a shape never invents a boot path".
func TestParseRejectsIllegalEntrypoint(t *testing.T) {
	_, err := Parse(Config{Shape: API, Entrypoints: []string{"cmd/web", "cmd/gui"}})
	if err == nil || !strings.Contains(err.Error(), "cannot ship cmd/gui") {
		t.Fatalf("Parse() = %v, want an illegal-entrypoint error", err)
	}
}

// TestParseRequiresThePrimaryOwnEntrypoint proves a declaration has to keep
// the binary the shape exists for.
func TestParseRequiresThePrimaryOwnEntrypoint(t *testing.T) {
	_, err := Parse(Config{Shape: GUI, Entrypoints: []string{"cmd/web"}})
	if err == nil || !strings.Contains(err.Error(), "cmd/gui") {
		t.Fatalf("Parse() = %v, want a missing-primary error", err)
	}
}

// TestCheckPassesOnAConsistentTree is the green arm: the declared entrypoint
// exists as a main package and nothing it removed is still on disk.
func TestCheckPassesOnAConsistentTree(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		Path:              "shape: web\n",
		"cmd/web/":        "",
		"cmd/web/main.go": "",
	})
	problems, err := Check(root)
	if err != nil {
		t.Fatalf("Check() = %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("Check() = %v, want none", problems)
	}
}

// TestCheckCatchesAPathThatCameBack proves `removed:` is verified rather than
// trusted: a trimmed tree that grew the directory back is inconsistent.
func TestCheckCatchesAPathThatCameBack(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		Path:                      "shape: web\nentrypoints: [cmd/web]\nremoved: [features/notes]\n",
		"cmd/web/main.go":         "",
		"features/notes/keep.go":  "",
		"features/notes/embed.go": "",
	})
	problems, err := Check(root)
	if err != nil {
		t.Fatalf("Check() = %v", err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "declared removed but still exists") {
		t.Fatalf("Check() = %v, want the resurrected-path problem", problems)
	}
}

// TestCheckCatchesAMissingEntrypoint proves the kept half is verified too.
func TestCheckCatchesAMissingEntrypoint(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{Path: "shape: api\n"})
	problems, err := Check(root)
	if err != nil {
		t.Fatalf("Check() = %v", err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "cmd/web does not exist") {
		t.Fatalf("Check() = %v, want the missing-entrypoint problem", problems)
	}
}

// TestCheckRejectsAnEntrypointThatIsNotMain keeps "entrypoint" honest: a
// directory of libraries is not a binary.
func TestCheckRejectsAnEntrypointThatIsNotMain(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		Path:             "shape: web\n",
		"cmd/web/lib.go": "package web\n",
	})
	problems, err := Check(root)
	if err != nil {
		t.Fatalf("Check() = %v", err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "no `package main`") {
		t.Fatalf("Check() = %v, want the not-main problem", problems)
	}
}
