package db

import (
	"io"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The built-in todo owns ext_todo_todos. The legacy core `todos` table
// only exists for append-only history and rows in flight; runtime queries
// must not reintroduce it after the ownership split.
func TestQueriesTargetOwnedTodosTable(t *testing.T) {
	legacy := regexp.MustCompile(`\b(?:FROM|INTO|UPDATE|JOIN)\s+todos\s*(?:WHERE|SET|ORDER|JOIN|;|$)`)
	owned := regexp.MustCompile(`\bext_todo_todos\b`)
	root, err := os.OpenRoot("queries")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		f, openErr := root.Open(entry.Name())
		if openErr != nil {
			t.Fatal(openErr)
		}
		buf, readErr := io.ReadAll(f)
		_ = f.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		for line := range strings.SplitSeq(string(buf), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "--") {
				continue
			}
			if legacy.MatchString(line) && !strings.Contains(line, "ext_todo_todos") {
				t.Errorf("%s references the legacy todos table: %s", entry.Name(), trimmed)
			}
		}
	}
	if !owned.MatchString("ext_todo_todos") {
		t.Fatal("owned-table marker must exist in queries")
	}
}
