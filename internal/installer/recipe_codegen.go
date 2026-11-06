// SCOPE:plugin — install regenerates the tree's generated code after copying.
package installer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/calionauta/gogogo/internal/recipe"
)

// The two generators an install has to run are different in kind, and the
// difference matters:
//
//   - templ is a `go tool` dependency of THIS module, so it always resolves and
//     is always the pinned version. A recipe ships page.templ and never the
//     generated page_templ.go, which is the same rule the repo keeps: generated
//     files are regenerated, not hand-carried.
//   - sqlc is a standalone binary the Makefile treats as "run it when it is
//     there, warn when it is not, never fetch it" (pulling its module graph into
//     go.mod is not worth it). Install keeps the same stance.
//
// Both are seams (variables) so tests never shell out to a tool the machine may
// not have — the same pattern `lookPath` gives the preflight checks.

// recipeTempl regenerates .templ → _templ.go in the target tree.
var recipeTempl = runTemplGenerate

// recipeSQLC regenerates the target tree's queries from its DDL.
var recipeSQLC = runSQLCGenerate

// The report vocabulary. The two values mean different things to the operator:
// codegenRan means the tree is regenerated, codegenSkipped means the tool was
// not here and the named manual step is theirs.
const (
	codegenRan     = "ran"
	codegenSkipped = "skipped"
)

// codegenReport is what install did to the tree's generated code. An empty
// field means the step did not apply; codegenSkipped means the tool was missing.
type codegenReport struct {
	Templ string `json:"templ,omitempty"`
	SQLC  string `json:"sqlc,omitempty"`
}

// runTemplGenerate runs the module's pinned templ generator in root.
func runTemplGenerate(root string) (bool, error) {
	cmd := exec.CommandContext(context.Background(), "go", "tool", "templ", "generate")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("recipe codegen: go tool templ generate: %w: %s",
			err, strings.TrimSpace(string(out)))
	}
	return true, nil
}

// runSQLCGenerate regenerates internal/db/sqlcgen in the target tree when sqlc
// resolves on PATH.
//
// Why the installer may shell out at all: without it, install leaves the tree
// uncompilable — the copied handler calls the generated query, and that query
// exists only because the mirror this same command just wrote is readable by
// codegen. "Run `make sqlc` yourself" after a command whose whole promise is
// "install, then build" is the kind of half-step that reads as a broken install.
func runSQLCGenerate(root string) (bool, error) {
	// Absent is not an error: install names the manual step instead of failing a
	// copy that is otherwise complete.
	if sqlcPath, _ := lookPath("sqlc"); sqlcPath == "" {
		return false, nil
	}
	// A trimmed tree can drop the data layer; there is then no config to read and
	// nothing to generate, which is a skip rather than a failure.
	cfgPath := filepath.Join(root, "internal", "db", "sqlc.yaml")
	if info, _ := os.Stat(cfgPath); info == nil || info.IsDir() {
		return false, nil
	}
	cmd := exec.CommandContext(context.Background(), "sqlc", "generate")
	cmd.Dir = filepath.Join(root, "internal", "db")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("recipe codegen: sqlc generate: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return true, nil
}

// runRecipeCodegen runs the generators this install actually needs: templ when
// the recipe ships a page, sqlc when it ships DDL.
//
// The receipt is the input, not the apply result: a re-install that writes
// nothing (everything pristine) still has to heal a generated file the user
// deleted, and "did this run write .templ files" would answer no.
func runRecipeCodegen(root string, r recipe.Receipt) (codegenReport, error) {
	var rep codegenReport
	if shipsTempl(r.Files) {
		ran, err := recipeTempl(root)
		if err != nil {
			return rep, err
		}
		if ran {
			rep.Templ = codegenRan
		}
	}
	if shipsMigration(r.Files) {
		ran, err := recipeSQLC(root)
		if err != nil {
			return rep, err
		}
		if ran {
			rep.SQLC = codegenRan
		} else {
			rep.SQLC = codegenSkipped
		}
	}
	return rep, nil
}

// removeRecipeGenerated deletes the files codegen derived from sources this
// uninstall just removed, and returns what it deleted.
//
// Why this is the installer's job: a generated file cannot be in the receipt —
// it does not exist until install has run — so `recipe.Remove` never sees it. A
// `page_templ.go` left behind does not compile once its `.templ` is gone, which
// would make "uninstall" mean "uninstall, then repair the build by hand".
//
// Only sources the tool actually removed are considered: a file the user edited
// stays in the tree, and its generated companion must stay with it.
func removeRecipeGenerated(root string, removed []string) ([]string, error) {
	var deleted []string
	for _, src := range removed {
		for _, gen := range generatedPathsFor(src) {
			full := filepath.Join(root, filepath.FromSlash(gen))
			if err := os.Remove(full); err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return deleted, fmt.Errorf("recipe uninstall: %w", err)
			}
			deleted = append(deleted, gen)
		}
	}
	sort.Strings(deleted)
	return deleted, nil
}

// generatedPathsFor maps one recipe source to the files codegen writes from it.
// Both mappings are the generators' own conventions, and both outputs are
// generated: the user cannot have meaningfully edited bytes there.
func generatedPathsFor(src string) []string {
	switch {
	case strings.HasSuffix(src, ".templ"):
		return []string{strings.TrimSuffix(src, ".templ") + "_templ.go"}
	case strings.HasPrefix(src, "internal/db/queries/") && strings.HasSuffix(src, ".sql"):
		base := strings.TrimSuffix(path.Base(src), ".sql")
		return []string{path.Join("internal/db/sqlcgen", base+".sql.go")}
	}
	return nil
}

// shipsTempl reports whether the recipe carries a templ source.
func shipsTempl(files map[string]string) bool {
	for p := range files {
		if strings.HasSuffix(strings.ToLower(p), ".templ") {
			return true
		}
	}
	return false
}

// shipsMigration reports whether the recipe carries DDL of its own.
func shipsMigration(files map[string]string) bool {
	for p := range files {
		if isRecipeMigration(p) {
			return true
		}
	}
	return false
}
