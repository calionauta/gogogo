// Runtime switch CLI: one recipe_state row, written by the same command the
// MCP tool maps to, and visible in `recipe list`.
package installer

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

func TestRecipeDisableWritesStateAndListShowsIt(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".recipes"), 0o755); err != nil {
		t.Fatal(err)
	}
	receipt := "id: timer\nupstream: {repo: example.com/timer, ref: v1}\nfiles: {}\n"
	if err := os.WriteFile(filepath.Join(root, ".recipes", "timer.yml"), []byte(receipt), 0o600); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(root, "data", "app.db")
	sqlDB, err := db.OpenAt(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("migrate: %v", merr)
	}
	if cerr := sqlDB.Close(); cerr != nil {
		t.Fatalf("close: %v", cerr)
	}

	var buf bytes.Buffer
	if serr := runRecipeSetState([]string{"timer", "--dir", root, "--db", dbPath}, false, &buf); serr != nil {
		t.Fatalf("disable: %v\n%s", serr, buf.String())
	}
	if !strings.Contains(buf.String(), "disabled") {
		t.Fatalf("disable output = %q", buf.String())
	}
	sqlDB, err = db.OpenAt(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	rows, err := dbgen.New(sqlDB).ListRecipeStates(context.Background())
	if err != nil || len(rows) != 1 || rows[0].ID != "timer" || rows[0].Enabled != 0 {
		t.Fatalf("recipe_state = %+v, %v; want timer disabled", rows, err)
	}

	buf.Reset()
	if err := runRecipeList([]string{"--dir", root}, &buf); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(buf.String(), "disabled") {
		t.Fatalf("list does not show the runtime switch:\n%s", buf.String())
	}
}
