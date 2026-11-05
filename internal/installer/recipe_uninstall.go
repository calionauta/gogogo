// SCOPE:plugin — installer CLI: `recipe uninstall`.
package installer

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/calionauta/gogogo/internal/recipe"
)

// recipeUninstallArgs is the parsed CLI surface for uninstall.
type recipeUninstallArgs struct {
	id  string
	dir string
	yes bool
}

// runRecipeUninstall removes the pristine files of one recipe and marks the
// receipt removed. Three rules, all from Spike 3: only pristine files are
// deleted, the receipt stays (it still records the data owner), and the
// generated registration stops importing the package in the same step.
func runRecipeUninstall(args []string, stdout io.Writer) error {
	opt, err := parseRecipeUninstallArgs(args)
	if err != nil {
		return err
	}
	root, aerr := filepath.Abs(opt.dir)
	if aerr != nil {
		return &ExitError{code: 1, msg: "recipe uninstall: " + aerr.Error()}
	}
	if !looksLikeTemplate(root) {
		return &ExitError{code: 1, msg: "recipe uninstall: " + opt.dir + " is not a gogogo checkout"}
	}
	r, lerr := recipe.LoadReceipt(root, opt.id)
	if lerr != nil {
		if os.IsNotExist(lerr) {
			return &ExitError{code: 1, msg: fmt.Sprintf(
				"recipe uninstall: %s is not installed (no %s)", opt.id, recipe.ReceiptPath(opt.id))}
		}
		return &ExitError{code: 1, msg: "recipe uninstall: " + lerr.Error()}
	}
	if r.Removed != "" {
		return &ExitError{code: 1, msg: fmt.Sprintf(
			"recipe uninstall: %s was already removed (%s)", opt.id, r.Removed)}
	}
	drift, derr := recipe.Check(root, r)
	if derr != nil {
		return &ExitError{code: 1, msg: "recipe uninstall: " + derr.Error()}
	}
	pristine, modified, deleted := drift.Counts()
	if !opt.yes {
		fmt.Fprintf(stdout, "recipe %s: would remove %d pristine file(s); %d modified stay, %d already gone\n",
			opt.id, pristine, modified, deleted)
		fmt.Fprintln(stdout, "recipe uninstall: nothing changed; re-run with --yes to apply")
		return nil
	}
	res, rerr := recipe.Remove(root, r)
	if rerr != nil {
		return &ExitError{code: 1, msg: "recipe uninstall: " + rerr.Error()}
	}
	// The DDL copy is the tool's, not the user's: it is never edited, so it goes
	// with the recipe rather than lingering as schema nobody owns.
	removedDDL, cerr := removeRecipeSchema(root, opt.id)
	if cerr != nil {
		return &ExitError{code: 1, msg: "recipe uninstall: " + cerr.Error()}
	}
	r.Removed = time.Now().UTC().Format(time.RFC3339)
	if serr := recipe.SaveReceipt(root, r); serr != nil {
		return &ExitError{code: 1, msg: "recipe uninstall: write receipt: " + serr.Error()}
	}
	if gerr := regenerateRegistration(root); gerr != nil {
		return gerr
	}
	fmt.Fprintf(stdout, "recipe %s: removed %d pristine file(s)\n", opt.id, len(res.Removed))
	if kept := len(res.Skipped); kept > 0 {
		fmt.Fprintf(stdout, "  %d file(s) left in place (yours) — uninstall never deletes what you wrote\n", kept)
	}
	fmt.Fprintf(stdout, "  receipt kept: %s (marked removed) — it still records the data owner\n",
		recipe.ReceiptPath(r.ID))
	if removedDDL {
		fmt.Fprintf(stdout, "  schema:   dropped %s/%s — run `make sqlc` so no generated query\n",
			recipeSchemaDir, opt.id)
		fmt.Fprintln(stdout, "            still reads a table this recipe no longer creates.")
	}
	fmt.Fprintf(stdout, "  registration regenerated: %s\n", recipeRegistrationFile)
	fmt.Fprintln(stdout, "  next: run `go build ./...` in your tree.")
	return nil
}

// parseRecipeUninstallArgs accepts the id before or after the flags.
func parseRecipeUninstallArgs(args []string) (recipeUninstallArgs, error) {
	opt := recipeUninstallArgs{}
	fs := flag.NewFlagSet("recipe uninstall", flag.ContinueOnError)
	fs.StringVar(&opt.dir, "dir", ".", "checkout to uninstall from")
	fs.BoolVar(&opt.yes, "yes", false, "apply the removal (without it the command prints and changes nothing)")
	fs.SetOutput(io.Discard)

	rest := args
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		opt.id = rest[0]
		rest = rest[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return opt, &ExitError{code: 1, msg: "recipe uninstall: " + err.Error()}
	}
	if opt.id == "" && fs.NArg() > 0 {
		opt.id = fs.Arg(0)
	}
	if opt.id == "" {
		return opt, &ExitError{code: 1, msg: "recipe uninstall needs a recipe id " +
			"(`recipe uninstall pomodoro`); `recipe status` lists what is installed"}
	}
	if fs.NArg() > 1 {
		return opt, &ExitError{code: 1, msg: "recipe uninstall takes one recipe id"}
	}
	return opt, nil
}
