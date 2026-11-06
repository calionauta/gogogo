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
	derived, derr := removeDerived(root, opt.id, res.Removed, r)
	if derr != nil {
		return &ExitError{code: 1, msg: "recipe uninstall: " + derr.Error()}
	}
	r.Removed = time.Now().UTC().Format(time.RFC3339)
	if serr := recipe.SaveReceipt(root, r); serr != nil {
		return &ExitError{code: 1, msg: "recipe uninstall: write receipt: " + serr.Error()}
	}
	if gerr := regenerateRegistration(root); gerr != nil {
		return gerr
	}
	printRecipeUninstallReceipt(stdout, opt.id, res, r, derived)
	return nil
}

// printRecipeUninstallReceipt reports what left the tree and the one step the
// removal leaves behind. Split from the removal so each reads as one job.
func printRecipeUninstallReceipt(stdout io.Writer, id string, res recipe.ApplyResult,
	r recipe.Receipt, d uninstallDerived,
) {
	fmt.Fprintf(stdout, "recipe %s: removed %d pristine file(s)\n", id, len(res.Removed))
	if kept := len(res.Skipped); kept > 0 {
		fmt.Fprintf(stdout, "  %d file(s) left in place (yours) — uninstall never deletes what you wrote\n", kept)
	}
	fmt.Fprintf(stdout, "  receipt kept: %s (marked removed) — it still records the data owner\n",
		recipe.ReceiptPath(r.ID))
	if d.Policy {
		fmt.Fprintf(stdout, "  policy:   dropped its block from %s\n", recipePolicyFile)
	}
	if declaresNav(r.Declaration) {
		// The rail lost an entry, so the page digests moved back.
		fmt.Fprintln(stdout, navDigestNotice)
	}
	if len(d.Generated) > 0 {
		fmt.Fprintf(stdout, "  codegen:  removed %d generated file(s)\n", len(d.Generated))
	}
	switch d.Codegen.SQLC {
	case codegenRan:
		fmt.Fprintln(stdout, "  queries:  regenerated (internal/db/sqlcgen) — nothing reads the dropped table")
	case codegenSkipped:
		fmt.Fprintln(stdout, "  queries:  sqlc is not on PATH: run `make sqlc` so no generated query")
		fmt.Fprintln(stdout, "            still reads a table this recipe no longer creates.")
	}
	if d.DDL {
		fmt.Fprintf(stdout, "  schema:   dropped %s/%s\n", recipeSchemaDir, id)
	}
	fmt.Fprintf(stdout, "  registration regenerated: %s\n", recipeRegistrationFile)
	fmt.Fprintln(stdout, "  next: run `go build ./...` in your tree.")
}

// uninstallDerived is what leaves with the recipe besides its own files: the
// artifacts the tool wrote, which the receipt cannot list because they did not
// exist when it was computed.
type uninstallDerived struct {
	Generated []string
	Policy    bool
	DDL       bool
	Codegen   codegenReport
}

// removeDerived removes those artifacts and then runs the generators, in that
// order: sqlc must see the tree without the dropped table, and templ must not
// regenerate a page whose source just left.
func removeDerived(root, id string, removed []string, r recipe.Receipt) (uninstallDerived, error) {
	var d uninstallDerived
	var err error
	// The DDL copy is the tool's, not the user's: it is never edited, so it goes
	// with the recipe rather than lingering as schema nobody owns.
	if d.DDL, err = removeRecipeSchema(root, id); err != nil {
		return d, err
	}
	// Generated output derived from the removed sources goes with them.
	if d.Generated, err = removeRecipeGenerated(root, removed); err != nil {
		return d, err
	}
	if d.Codegen, err = runRecipeCodegen(root, r); err != nil {
		return d, err
	}
	// The policy rows are the tool's too: an installed recipe's block goes with
	// it, or the file keeps granting paths nothing serves.
	d.Policy, err = removeRecipePolicy(root, id)
	return d, err
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
