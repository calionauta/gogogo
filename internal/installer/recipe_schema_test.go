// The sqlc mirror: install writes a recipe's DDL where codegen
// reads it, status notices when that copy stops describing the tree, and
// uninstall takes it away with the recipe.
package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	sessionDDL = "CREATE TABLE ext_timer_sessions (id TEXT PRIMARY KEY);\n"
	indexDDL   = "CREATE INDEX idx_sessions ON ext_timer_sessions (id);\n"
)

// newDDLRecipeDir writes a recipe that declares a table and ships its DDL the
// way a real one does: inside its own package, under migrations/.
func newDDLRecipeDir(t *testing.T, ref string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := "id: timer\n" +
		"upstream:\n  repo: example.com/recipes\n  ref: " + ref + "\n" +
		"declaration:\n  routes:\n    - {method: GET, pattern: /timer}\n" +
		"  tables:\n    - ext_timer_sessions\n"
	writeFile(t, filepath.Join(dir, recipeManifestFile), manifest)
	for rel, body := range files {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(rel)), body)
	}
	return dir
}

// mirrorAt is where one installed migration lands in the mirror.
func mirrorAt(root, name string) string {
	return filepath.Join(root, filepath.FromSlash(recipeSchemaDir), "timer__"+name)
}

// TestRecipeSchemaMirrorsAreFlat pins the measurement the layout depends on:
// sqlc reads a schema entry one level deep, so a nested mirror would be silently
// ignored and the recipe's table would have no generated queries.
func TestRecipeSchemaMirrorsAreFlat(t *testing.T) {
	got := schemaMirrors("timer", map[string]string{
		"features/timer/migrations/0001_init.sql":      "h1",
		"features/timer/migrations/2026/0002_more.sql": "h2",
		"features/timer/handlers/chi.go":               "h3",
	})
	want := []string{"timer__0001_init.sql", "timer__2026_0002_more.sql"}
	if len(got) != len(want) {
		t.Fatalf("got %d mirrors, want %d: %+v", len(got), len(want), got)
	}
	for i, name := range want {
		if got[i].name != name || strings.ContainsRune(name, '/') {
			t.Fatalf("mirror %d = %q, want flat %q", i, got[i].name, name)
		}
	}
}

func TestRecipeSchemaSourcesPicksMigrationsOnly(t *testing.T) {
	cases := map[string]bool{
		"migrations/0001_init.sql":                     true,
		"features/timer/migrations/0001_init.sql":      true,
		"features/timer/migrations/2026/0002_more.sql": true,
		"features/timer/seed.sql":                      false,
		"features/timer/migrations/README.md":          false,
		"features/timer/handlers/chi.go":               false,
	}
	for path, want := range cases {
		if got := isRecipeMigration(path); got != want {
			t.Errorf("isRecipeMigration(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestRecipeInstallMirrorsItsDDLForCodegen(t *testing.T) {
	root := newRecipeCheckout(t)
	src := newDDLRecipeDir(t, "v1", map[string]string{
		"features/timer/handlers/chi.go":          recipeHandlerV1,
		"features/timer/migrations/0001_init.sql": sessionDDL,
	})

	out, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	got, rerr := os.ReadFile(mirrorAt(root, "0001_init.sql"))
	if rerr != nil {
		t.Fatalf("the recipe's DDL was not mirrored where sqlc reads it: %v", rerr)
	}
	if string(got) != sessionDDL {
		t.Fatalf("mirror holds %q, want the migration's own bytes", got)
	}
	// The mirror is only useful if codegen actually read it, so install runs the
	// generator and says so — nobody has to be told to run `make sqlc` after a
	// command whose promise is "install, then build".
	if !strings.Contains(out, "queries:  regenerated") {
		t.Fatalf("install must report the regenerated queries, got:\n%s", out)
	}
	if _, serr := os.Stat(mirrorAt(root, "chi.go")); !os.IsNotExist(serr) {
		t.Fatal("the mirror copied files that are not migrations; it holds DDL only")
	}
}

func TestRecipeInstallMirrorsNothingForATableLessRecipe(t *testing.T) {
	root := newRecipeCheckout(t)
	src := newRecipeDir(t, "v1", map[string]string{
		"features/timer/handlers/chi.go": recipeHandlerV1,
	})

	if _, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes"); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := os.Stat(mirrorAt(root, "0001_init.sql")); !os.IsNotExist(err) {
		t.Fatalf("a recipe with no DDL left a mirror behind: %v", err)
	}
}

func TestRecipeInstallPrunesAMigrationTheRecipeDropped(t *testing.T) {
	root := newRecipeCheckout(t)
	v1 := newDDLRecipeDir(t, "v1", map[string]string{
		"features/timer/handlers/chi.go":           recipeHandlerV1,
		"features/timer/migrations/0001_init.sql":  sessionDDL,
		"features/timer/migrations/0002_index.sql": indexDDL,
	})
	if _, err := runRecipeCLI(t, "install", v1, "--dir", root, "--yes"); err != nil {
		t.Fatalf("install: %v", err)
	}

	v2 := newDDLRecipeDir(t, "v2", map[string]string{
		"features/timer/handlers/chi.go":          recipeHandlerV1,
		"features/timer/migrations/0001_init.sql": sessionDDL,
	})
	if _, err := runRecipeCLI(t, "update", v2, "--dir", root, "--yes"); err != nil {
		t.Fatalf("update: %v", err)
	}

	if _, err := os.Stat(mirrorAt(root, "0002_index.sql")); !os.IsNotExist(err) {
		t.Fatal("the mirror kept DDL the recipe no longer ships: codegen would describe a table nothing creates")
	}
	if _, err := os.Stat(mirrorAt(root, "0001_init.sql")); err != nil {
		t.Fatalf("update dropped a migration the recipe still ships: %v", err)
	}
}

// TestRecipeInstallMirrorsTheUsersEditedMigration is the property that makes the
// mirror trustworthy: the bytes a reinstall copies are the tree's, because the
// runtime embeds the tree's. Copying upstream there would leave codegen reading
// one schema while the database applies another.
func TestRecipeInstallMirrorsTheUsersEditedMigration(t *testing.T) {
	root := newRecipeCheckout(t)
	src := newDDLRecipeDir(t, "v1", map[string]string{
		"features/timer/handlers/chi.go":          recipeHandlerV1,
		"features/timer/migrations/0001_init.sql": sessionDDL,
	})
	if _, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes"); err != nil {
		t.Fatalf("install: %v", err)
	}

	edited := sessionDDL + "CREATE TABLE ext_timer_notes (id TEXT);\n"
	writeFile(t, filepath.Join(root, "features", "timer", "migrations", "0001_init.sql"), edited)
	if _, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes"); err != nil {
		t.Fatalf("reinstall: %v", err)
	}

	got, rerr := os.ReadFile(mirrorAt(root, "0001_init.sql"))
	if rerr != nil {
		t.Fatalf("read mirror: %v", rerr)
	}
	if string(got) != edited {
		t.Fatalf("mirror holds %q, want the user's migration %q", got, edited)
	}
}

func TestRecipeUninstallRemovesTheSchemaMirror(t *testing.T) {
	root := newRecipeCheckout(t)
	src := newDDLRecipeDir(t, "v1", map[string]string{
		"features/timer/handlers/chi.go":          recipeHandlerV1,
		"features/timer/migrations/0001_init.sql": sessionDDL,
	})
	if _, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes"); err != nil {
		t.Fatalf("install: %v", err)
	}

	out, err := runRecipeCLI(t, "uninstall", "timer", "--dir", root, "--yes")
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, serr := os.Stat(mirrorAt(root, "0001_init.sql")); !os.IsNotExist(serr) {
		t.Fatalf("the mirror survived the uninstall: %v", serr)
	}
	// The mirror left with the recipe, so the generated queries were rewritten
	// without it in the same command — saying "run make sqlc" here would be
	// telling the operator to redo what just happened.
	if !strings.Contains(out, "queries:  regenerated") {
		t.Fatalf("uninstall must report the regenerated queries, got:\n%s", out)
	}
}

func TestRecipeStatusReportsAStaleSchemaMirror(t *testing.T) {
	root := newRecipeCheckout(t)
	src := newDDLRecipeDir(t, "v1", map[string]string{
		"features/timer/handlers/chi.go":          recipeHandlerV1,
		"features/timer/migrations/0001_init.sql": sessionDDL,
	})
	if _, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes"); err != nil {
		t.Fatalf("install: %v", err)
	}

	// A hand-edit to the generated copy: the tree and the runtime still create
	// ext_timer_sessions, while codegen would read a table that never exists.
	writeFile(t, mirrorAt(root, "0001_init.sql"), "CREATE TABLE ext_timer_old (id TEXT);\n")

	out, err := runRecipeCLI(t, "status", "--dir", root)
	if err == nil {
		t.Fatalf("status accepted a mirror that no longer matches the tree, output:\n%s", out)
	}
	if !strings.Contains(out, "sqlc mirror") {
		t.Fatalf("status must name the mirror as the problem, got:\n%s", out)
	}
}
