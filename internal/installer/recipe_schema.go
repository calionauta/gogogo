// Copying a recipe's DDL where the query generator can see it.
//
// The engine applies a recipe's migrations from the recipe's own package,
// namespaced by its id. `sqlc` cannot: it reads directories, and — measured, not
// assumed — it reads only the TOP LEVEL of each `schema:` entry and skips
// directories whose name starts with a dot. So a mirror nested under
// `.recipes/schema/<id>/` is silently inert: codegen would neither fail nor see
// the table. That is the divergence this file exists to prevent, which is why
// the mirror is flat, visible and named `<id>__<migration>`.
package installer

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// recipeSchemaDir is where the mirrored DDL lives, beside sqlc.yaml because that
// is the only place the generator looks. It is committed: a clone has to be able
// to regenerate queries for the tables an installed recipe owns.
const recipeSchemaDir = "internal/db/recipe_schema"

// migrationDir is the path segment that marks a file as DDL rather than a
// fixture: a recipe's migrations ship under `migrations/`, wherever its package
// lives. The installer cannot know which directory a recipe embedded (that is
// inside `Migrations()`), so the convention is the only anchor it has.
const migrationDir = "migrations"

// schemaMirror is one DDL file: where it lives in the tree, and the flat name it
// takes in the mirror.
type schemaMirror struct {
	name string // file name inside recipeSchemaDir
	src  string // installed, repo-relative path it mirrors
}

// schemaMirrors pairs every installed migration with its mirror name, sorted by
// name so a boot, a report and a test all see the same order.
//
// The name is `<id>__<tail>`: the id because two recipes number their migrations
// from 0001 and share one directory, and the tail flattened because sqlc would
// ignore a nested one. A `.sql` file that is not under `migrations/` stays out —
// it is seed data or a test fixture, and feeding it to codegen as schema would
// be a lie about the tables.
func schemaMirrors(id string, installed map[string]string) []schemaMirror {
	mirrors := make([]schemaMirror, 0, len(installed))
	for p := range installed {
		if isRecipeMigration(p) {
			mirrors = append(mirrors, schemaMirror{name: id + "__" + migrationTail(p), src: p})
		}
	}
	sort.Slice(mirrors, func(i, j int) bool { return mirrors[i].name < mirrors[j].name })
	return mirrors
}

func isRecipeMigration(p string) bool {
	if !strings.HasSuffix(strings.ToLower(p), ".sql") {
		return false
	}
	segs := strings.Split(p, "/")
	return slices.Contains(segs[:len(segs)-1], migrationDir)
}

// migrationTail is everything after the migrations directory, flattened. It reads
// the LAST such segment, so a path that happens to repeat the word still names
// the file the way the runtime's walk does.
func migrationTail(p string) string {
	segs := strings.Split(p, "/")
	for i := len(segs) - 2; i >= 0; i-- {
		if segs[i] == migrationDir {
			return strings.Join(segs[i+1:], "_")
		}
	}
	return strings.Join(segs, "_")
}

// mirrorFilePath resolves one mirrored file, refusing a name that would leave
// the mirror directory. The names are built from recipe-supplied paths, so this
// is the boundary that keeps a hand-edited receipt or a crafted tree from
// writing outside `internal/db/recipe_schema`.
func mirrorFilePath(root, name string) (string, error) {
	dir := filepath.Join(root, filepath.FromSlash(recipeSchemaDir))
	dst := filepath.Join(dir, name)
	if name == "" || name == "." || name == ".." || filepath.Dir(dst) != dir {
		return "", fmt.Errorf("recipe schema: %q is not a file name", name)
	}
	return dst, nil
}

// syncRecipeSchema mirrors the recipe's DDL and returns the paths it wrote, so
// the caller can report them.
//
// The bytes come from the TREE after the plan applied, not from upstream: the
// runtime embeds whatever the copied files now say, so a user who edited a
// migration must see the mirror follow it. Copying upstream there would leave
// codegen reading the old schema while the database gets the edited one,
// silently.
func syncRecipeSchema(root, id string, installed map[string]string) ([]string, error) {
	mirrors := schemaMirrors(id, installed)
	want := make(map[string]bool, len(mirrors))
	owner := make(map[string]string, len(mirrors))
	for _, m := range mirrors {
		if prev, dup := owner[m.name]; dup {
			return nil, fmt.Errorf("recipe schema: %s and %s both mirror to %s", prev, m.src, m.name)
		}
		owner[m.name] = m.src
		want[m.name] = true
	}
	if err := pruneRecipeSchema(root, id, want); err != nil {
		return nil, err
	}

	var written []string
	for _, m := range mirrors {
		raw, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(m.src)))
		if os.IsNotExist(rerr) {
			// A file the user deleted is the receipt's finding, not the mirror's:
			// resurrecting the DDL would generate queries for a migration the code
			// no longer ships.
			continue
		}
		if rerr != nil {
			return nil, fmt.Errorf("recipe schema: %w", rerr)
		}
		dst, derr := mirrorFilePath(root, m.name)
		if derr != nil {
			return nil, derr
		}
		if cur, cerr := os.ReadFile(dst); cerr == nil && bytes.Equal(cur, raw) {
			continue
		}
		// os.Root, not a bare path: the name comes from the recipe's own tree, and
		// the kernel-level boundary is what keeps a crafted one inside the mirror.
		if werr := writeInSchemaDir(root, m.name, raw); werr != nil {
			return nil, werr
		}
		written = append(written, filepath.ToSlash(filepath.Join(recipeSchemaDir, m.name)))
	}
	return written, nil
}

// writeInSchemaDir creates or replaces one mirrored file inside the schema
// directory, opened as an os.Root so the name cannot reach past it.
func writeInSchemaDir(root, name string, raw []byte) error {
	dir := filepath.Join(root, filepath.FromSlash(recipeSchemaDir))
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("recipe schema: %w", err)
	}
	schemaRoot, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("recipe schema: %w", err)
	}
	defer schemaRoot.Close()

	file, err := schemaRoot.Create(name)
	if err != nil {
		return fmt.Errorf("recipe schema: %w", err)
	}
	if _, werr := file.Write(raw); werr != nil {
		file.Close()
		return fmt.Errorf("recipe schema: %w", werr)
	}
	if cerr := file.Close(); cerr != nil {
		return fmt.Errorf("recipe schema: %w", cerr)
	}
	return nil
}

// pruneRecipeSchema removes this recipe's mirror files that it no longer ships.
// The directory is shared, so the id prefix is the boundary: pruning by
// everything-but-what-I-want would delete another recipe's DDL.
func pruneRecipeSchema(root, id string, want map[string]bool) error {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(recipeSchemaDir)))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("recipe schema: %w", err)
	}
	stale := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || want[name] || !strings.HasPrefix(name, id+"__") {
			continue
		}
		full, perr := mirrorFilePath(root, name)
		if perr != nil {
			return perr
		}
		if rerr := os.Remove(full); rerr != nil {
			return fmt.Errorf("recipe schema: %w", rerr)
		}
		stale++
	}
	return nil
}

// removeRecipeSchema deletes this recipe's mirrored DDL and reports whether there
// was any. A recipe with no tables is the normal case, so nothing is not an error.
func removeRecipeSchema(root, id string) (bool, error) {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(recipeSchemaDir)))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("recipe schema: %w", err)
	}
	removed := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), id+"__") {
			continue
		}
		full, perr := mirrorFilePath(root, entry.Name())
		if perr != nil {
			return removed, perr
		}
		if rerr := os.Remove(full); rerr != nil {
			return removed, fmt.Errorf("recipe schema: %w", rerr)
		}
		removed = true
	}
	return removed, nil
}

// checkRecipeSchema compares the mirror against the tree, which is the question
// `recipe.Check` asks of the receipt in another register: do the generated views
// still describe the code? It never writes — `recipe install` is the repair.
//
// Without this pass the mirror could lag the copied migrations (a user edits a
// migration and never reinstalls), and codegen would quietly describe a schema
// the database does not have.
func checkRecipeSchema(root, id string, installed map[string]string) []string {
	mirrors := schemaMirrors(id, installed)
	var problems []string
	owner := make(map[string]string, len(mirrors))
	want := make(map[string]bool, len(mirrors))
	for _, m := range mirrors {
		if prev, dup := owner[m.name]; dup {
			problems = append(problems, fmt.Sprintf(
				"%s and %s both mirror to %s/%s", prev, m.src, recipeSchemaDir, m.name))
			continue
		}
		owner[m.name] = m.src
		want[m.name] = true

		raw, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(m.src)))
		if os.IsNotExist(rerr) {
			continue // the receipt reports the deletion
		}
		if rerr != nil {
			problems = append(problems, fmt.Sprintf("cannot read %s: %v", m.src, rerr))
			continue
		}
		dst, derr := mirrorFilePath(root, m.name)
		if derr != nil {
			problems = append(problems, derr.Error())
			continue
		}
		got, merr := os.ReadFile(dst)
		switch {
		case os.IsNotExist(merr):
			problems = append(problems, fmt.Sprintf(
				"the sqlc mirror has no %s/%s: codegen sees no DDL for a table the runtime applies",
				recipeSchemaDir, m.name))
		case merr != nil:
			problems = append(problems, fmt.Sprintf("cannot read the sqlc mirror of %s: %v", m.src, merr))
		case !bytes.Equal(got, raw):
			problems = append(problems, fmt.Sprintf(
				"the sqlc mirror of %s differs from the migration: codegen and the runtime would "+
					"disagree (re-run `gogogo recipe install %s`)", m.src, id))
		}
	}

	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(recipeSchemaDir)))
	if err != nil && !os.IsNotExist(err) {
		return append(problems, fmt.Sprintf("cannot read %s: %v", recipeSchemaDir, err))
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || want[name] || !strings.HasPrefix(name, id+"__") {
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"the sqlc mirror holds %s/%s, which no installed migration produces", recipeSchemaDir, name))
	}
	return problems
}
