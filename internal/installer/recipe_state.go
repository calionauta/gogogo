// Runtime recipe state for the CLI and MCP.
//
// Install/uninstall is a build fact (the code is copied or removed). Enabled/
// disabled is the durable recipe_state row the product's /config page writes.
// The developer CLI reads and writes the SAME table, so there is one source of
// truth: a CLI toggle and an admin toggle cannot disagree.
package installer

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// recipeStateDB is the default database a checkout's app writes. The env
// override matches the product's own TURSO_DB_PATH.
func recipeStateDB(dir string) string {
	if path := os.Getenv("TURSO_DB_PATH"); path != "" {
		return path
	}
	return filepath.Join(dir, "data", "app.db")
}

// The two words the CLI prints and the MCP returns. Constants so a rename
// cannot make text and JSON disagree.
const (
	recipeEnabledWord  = "enabled"
	recipeDisabledWord = "disabled"
)

// runtimeRecipeStates reads the durable switches, or nil when the checkout has
// no database yet (a fresh tree before its first boot: every recipe is
// enabled).
func runtimeRecipeStates(dir string) map[string]bool {
	path := recipeStateDB(dir)
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	sqlDB, err := db.OpenAt(path)
	if err != nil {
		return nil
	}
	defer func() { _ = sqlDB.Close() }()
	rows, err := dbgen.New(sqlDB).ListRecipeStates(context.Background())
	if err != nil {
		return nil
	}
	out := make(map[string]bool, len(rows))
	for _, row := range rows {
		out[row.ID] = row.Enabled != 0
	}
	return out
}

// applyRuntimeStates folds the durable switch into discovery descriptors. A
// missing row leaves Enabled nil ("all enabled"), which `recipe list` prints as
// no extra word rather than a claim it cannot prove.
func applyRuntimeStates(dir string, descs []recipeDescriptor) {
	states := runtimeRecipeStates(dir)
	if len(states) == 0 {
		return
	}
	for i := range descs {
		if enabled, ok := states[descs[i].ID]; ok {
			value := enabled
			descs[i].Enabled = &value
		}
	}
}

// runRecipeEnable is `recipe enable <id>`.
func runRecipeEnable(args []string, stdout io.Writer) error {
	return runRecipeSetState(args, true, stdout)
}

// runRecipeDisable is `recipe disable <id>`.
func runRecipeDisable(args []string, stdout io.Writer) error {
	return runRecipeSetState(args, false, stdout)
}

type recipeStateArgs struct {
	id     string
	dir    string
	db     string
	format string
}

func parseRecipeStateArgs(command string, args []string) (recipeStateArgs, error) {
	var opt recipeStateArgs
	fs := flag.NewFlagSet("recipe "+command, flag.ContinueOnError)
	fs.StringVar(&opt.dir, "dir", ".", "checkout whose app database holds the switch")
	fs.StringVar(&opt.db, "db", "", "database path (default: TURSO_DB_PATH or <dir>/data/app.db)")
	fs.StringVar(&opt.format, "format", planFormatText, "output format: text|json")
	fs.SetOutput(io.Discard)

	rest := args
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		opt.id = rest[0]
		rest = rest[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return opt, &ExitError{code: 1, msg: "recipe " + command + ": " + err.Error()}
	}
	if opt.id == "" && fs.NArg() > 0 {
		opt.id = fs.Arg(0)
	}
	if opt.id == "" || fs.NArg() > 1 {
		return opt, &ExitError{code: 1, msg: "recipe " + command + " needs exactly one recipe id"}
	}
	if opt.format != planFormatText && opt.format != planFormatJSON {
		return opt, &ExitError{code: 1, msg: fmt.Sprintf("unknown --format %q (want text|json)", opt.format)}
	}
	return opt, nil
}

// ensureToggleable refuses an unknown or removed recipe before the DB is
// touched: a state row for code that is not installed is a switch with nothing
// behind it.
func ensureToggleable(descs []recipeDescriptor, id, root string) error {
	for _, d := range descs {
		if d.ID != id {
			continue
		}
		if d.Removed || d.State == StateRemovedRecipe {
			return &ExitError{code: 1, msg: fmt.Sprintf(
				"recipe %s: is removed; install it before toggling its runtime switch", id)}
		}
		return nil
	}
	return &ExitError{code: 1, msg: fmt.Sprintf("recipe %s: not installed in %s", id, root)}
}

// writeRecipeState opens the app database and writes the row. The DB must
// already exist: the evolution CLI never creates the product's data directory.
func writeRecipeState(path, id string, enabled bool) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("no database at %s — boot the product once (or pass --db)", path)
	}
	sqlDB, err := db.OpenAt(path)
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()
	var flag int64
	if enabled {
		flag = 1
	}
	return dbgen.New(sqlDB).SetRecipeState(context.Background(), dbgen.SetRecipeStateParams{
		ID: id, Enabled: flag, UpdatedBy: "cli", UpdatedAt: dbschema.Now(),
	})
}

// runRecipeSetState writes one switch. It refuses an unknown id and a missing
// database: creating an app database from the evolution CLI would hide the fact
// that the product has never booted, and a typo would create a row nobody reads.
func runRecipeSetState(args []string, enabled bool, stdout io.Writer) error {
	command := "disable"
	if enabled {
		command = "enable"
	}
	opt, err := parseRecipeStateArgs(command, args)
	if err != nil {
		return err
	}
	root, aerr := filepath.Abs(opt.dir)
	if aerr != nil {
		return &ExitError{code: 1, msg: "recipe " + command + ": " + aerr.Error()}
	}
	descs, derr := recipeDescriptors(root)
	if derr != nil {
		return derr
	}
	if err := ensureToggleable(descs, opt.id, root); err != nil {
		return err
	}
	path := opt.db
	if path == "" {
		path = recipeStateDB(root)
	}
	if err := writeRecipeState(path, opt.id, enabled); err != nil {
		return &ExitError{code: 1, msg: "recipe " + command + ": " + err.Error()}
	}
	if opt.format == planFormatJSON {
		return json.NewEncoder(stdout).Encode(map[string]any{
			"id": opt.id, "enabled": enabled, "next": "takes effect on the next boot",
		})
	}
	fmt.Fprintf(stdout, "recipe %s: %s (takes effect on the next boot)\n",
		opt.id, map[bool]string{true: recipeEnabledWord, false: recipeDisabledWord}[enabled])
	return nil
}
