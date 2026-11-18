// Installer CLI: `recipe uninstall`.
package installer

import (
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/calionauta/gogogo/internal/capabilities"
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
// runRecipeUninstall deletes a recipe's files. It guards the read-only rail
// FIRST, before the tree is touched or even judged: uninstall is the last verb
// that should slip past GOGOGO_READONLY=1, and it did — exactly like
// `recipe install` — until a test started asking. The rail names itself; a
// downstream "not a gogogo checkout" reads like a bug and sends the operator
// looking at the wrong thing.
func runRecipeUninstall(args []string, stdout io.Writer) error {
	opt, err := parseRecipeUninstallArgs(args)
	if err != nil {
		return err
	}
	if railErr := guardMutable(opt.dir); railErr != nil {
		return railErr
	}
	root, aerr := filepath.Abs(opt.dir)
	if aerr != nil {
		return &ExitError{code: 1, msg: "recipe uninstall: " + aerr.Error()}
	}
	if !looksLikeTemplate(root) {
		return &ExitError{code: 1, msg: "recipe uninstall: " + opt.dir + " is not a gogogo checkout"}
	}
	r, rerr := loadUninstallReceipt(root, opt.id)
	if rerr != nil {
		return rerr
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
	if merr := r.MarkRemoved(time.Now()); merr != nil {
		return &ExitError{code: 1, msg: "recipe uninstall: " + merr.Error()}
	}
	if serr := recipe.SaveReceipt(root, r); serr != nil {
		return &ExitError{code: 1, msg: "recipe uninstall: write receipt: " + serr.Error()}
	}
	if gerr := regenerateRegistration(root); gerr != nil {
		return gerr
	}
	// Bundled (builtin) units mount via units.gen.go (generated from the
	// registry), not recipes.gen.go. Uninstalling one must drop it there
	// too, or the build imports a package that is gone. Regenerating
	// from the registry after marking the receipt removed would still
	// list it (the registry entry is data, not the receipt), so we
	// re-run the bundled generator with removed ids filtered via the
	// receipts that say Removed.
	if gerr := regenerateBundledUnits(root); gerr != nil {
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
	// The code leaves, the data does not. Migrations are append-only, so an
	// uninstall that dropped a table would rewrite history under the operator's
	// feet; naming what stays is the difference between "removed" and "gone".
	if len(r.Declaration.Tables) > 0 {
		fmt.Fprintf(stdout, "  data:     tables left in place: %s\n",
			strings.Join(r.Declaration.Tables, ", "))
		fmt.Fprintln(stdout, "            reason: migrations are append-only; drop them with a new"+
			" migration if the data must go")
	}
	if len(r.Declaration.Jobs) > 0 {
		fmt.Fprintf(stdout, "  jobs:     stop with the code: %s\n",
			strings.Join(r.Declaration.Jobs, ", "))
	}
	if len(r.Declaration.Metrics) > 0 {
		fmt.Fprintln(stdout, "  metrics:  its dashboard readings disappear with the declaration")
	}
	if d.Policy {
		fmt.Fprintf(stdout, "  policy:   dropped its block from %s\n", recipePolicyFile)
	}
	if d.Nav {
		fmt.Fprintf(stdout, "  nav:      dropped its entry from %s\n", modulesFile)
	}
	if d.Card {
		fmt.Fprintf(stdout, "  card:     removed %s/%s.md\n", capabilities.CardDir, id)
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
	// Card reports that the slice's written boundary (docs/domain/<id>.md)
	// left the tree. check-domains refuses a card with no slice, so an
	// uninstall that kept it would make the very next commit red.
	Card bool
	// Nav reports that a sidebar entry left internal/ui/modules.go. A bundled
	// unit keeps that entry in source (installed recipes contribute one at
	// runtime), so removing the package without the line would leave the rail
	// pointing at a page that no longer exists.
	Nav     bool
	Codegen codegenReport
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
	if err != nil {
		return d, err
	}
	// The rail entry is spread across two places (Modules and viewerModules);
	// both are removed here so "uninstall" means "no dead link", not "no
	// package". A runtime nav contribution has no line and this is a no-op.
	d.Nav, err = removeModuleEntry(root, id)
	if err != nil {
		return d, err
	}
	d.Card, err = removeDomainCard(root, id)
	return d, err
}

// removeDomainCard deletes docs/domain/<id>.md when it exists.
//
// The card is the slice's boundary, so it leaves with the slice. It is checked
// for before removing because a data-only recipe (policy or schema, no code)
// legitimately has no card, and "no card" is success, not a refusal.
func removeDomainCard(root, id string) (bool, error) {
	path := capabilities.CardPath(root, id)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if err := os.Remove(path); err != nil {
		return false, err
	}
	return true, nil
}

// sourceFileMode is the mode for rewriting a tracked source file in place.
const sourceFileMode = 0o644

// removeModuleEntry drops a destination from the shell's module registry.
//
// The registry is the one place a bundled page becomes navigable, so an
// uninstall that skipped it left the rail linking to a package that was gone.
// Handles both entry shapes in the file (a one-line `{ID: "x", ...}` and the
// multi-line `{` + `ID: "x",` form) and the viewer lookup beside it.
func removeModuleEntry(root, id string) (bool, error) {
	path := filepath.Join(root, filepath.FromSlash(modulesFile))
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	lines := strings.Split(string(raw), "\n")
	var out []string
	changed := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.Contains(line, `{ID: "`+id+`",`) {
			changed = true
			continue
		}
		if strings.TrimSpace(line) == "{" {
			// Multi-line entry: drop it only when its ID line names this unit.
			if end := blockEntryEnd(lines, i, id); end >= 0 {
				changed = true
				i = end
				continue
			}
		}
		if next, ok := dropMapKey(line, id); ok {
			changed = true
			line = next
		} else if strings.Contains(line, `"`+id+`":`) && strings.Contains(line, "true") {
			// A pair line inside a multi-line map: the braces live on other
			// lines, so only this entry goes and the declaration survives.
			changed = true
			continue
		}
		out = append(out, line)
	}
	if !changed {
		return false, nil
	}
	formatted, ferr := format.Source([]byte(strings.Join(out, "\n")))
	if ferr != nil {
		// Leaving the file unformatted is still better than leaving the entry:
		// the gate that formats is cheaper than a dead rail link.
		formatted = []byte(strings.Join(out, "\n"))
	}
	if err := os.WriteFile(path, formatted, sourceFileMode); err != nil { //nolint:gosec // tracked source file
		return false, err
	}
	return true, nil
}

// blockEntryEnd returns the closing line of the multi-line entry that starts at
// i when that entry names id, or -1. Keeping the scan here leaves the caller a
// loop with one branch instead of a nested one.
func blockEntryEnd(lines []string, i int, id string) int {
	end := i + 1
	for end < len(lines) && !strings.HasSuffix(strings.TrimSpace(lines[end]), "},") {
		end++
	}
	block := strings.Join(lines[i:min(end+1, len(lines))], "\n")
	if strings.Contains(block, `ID:        "`+id+`",`) || strings.Contains(block, `ID: "`+id+`",`) {
		return end
	}
	return -1
}

// dropMapKey removes one `"id": true` pair from a one-line map literal and
// keeps the DECLARATION: deleting `var viewerModules = map[string]bool{...}`
// when its last entry goes leaves every reader of the map undefined, which is a
// compile error introduced by a removal that was supposed to delete a link.
// An empty map is the honest result.
func dropMapKey(line, id string) (string, bool) {
	const marker = "map[string]bool{"
	open := strings.Index(line, marker)
	closeIdx := strings.LastIndex(line, "}")
	if open < 0 || closeIdx < open {
		return line, false
	}
	var keep []string
	removed := false
	for part := range strings.SplitSeq(line[open+len(marker):closeIdx], ",") {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}
		key, _, ok := strings.Cut(entry, ":")
		if ok && strings.Trim(strings.TrimSpace(key), `"`) == id {
			removed = true
			continue
		}
		keep = append(keep, entry)
	}
	if !removed {
		return line, false
	}
	return line[:open] + marker + strings.Join(keep, ", ") + "}" + line[closeIdx+1:], true
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
			"(`recipe uninstall timer`); `recipe status` lists what is installed"}
	}
	if fs.NArg() > 1 {
		return opt, &ExitError{code: 1, msg: "recipe uninstall takes one recipe id"}
	}
	return opt, nil
}

// regenerateBundledUnits drops removed bundled units from units.gen.go.
// Bundled units mount via that file (registry-driven), not recipes.gen.go,
// so uninstalling one must edit it too or the build imports a gone package.
//
//nolint:gocyclo // one job: filter lines by two string sets, still under ~15 branches.
func regenerateBundledUnits(root string) error {
	genPath := filepath.Join(root, "internal/server/units.gen.go")
	raw, err := os.ReadFile(genPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return &ExitError{code: 1, msg: "recipe uninstall: read units.gen.go: " + err.Error()}
	}
	ids, lerr := recipe.ListReceipts(root)
	if lerr != nil {
		return &ExitError{code: 1, msg: "recipe uninstall: list receipts: " + lerr.Error()}
	}
	removed := map[string]bool{}
	for _, id := range ids {
		r, lerr := recipe.LoadReceipt(root, id)
		if lerr != nil || r.Removed == "" || r.Register == "" {
			continue
		}
		removed[r.Register] = true
	}
	if len(removed) == 0 {
		return nil
	}
	aliasToRemove := map[string]bool{}
	for rp := range removed {
		parts := strings.Split(strings.Trim(rp, "/"), "/")
		tail := parts[len(parts)-1]
		if len(parts) > 1 {
			tail = parts[len(parts)-2] + tail
		}
		aliasToRemove[tail] = true
	}
	lines := strings.Split(string(raw), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		drop := false
		for rp := range removed {
			if strings.Contains(line, rp) {
				drop = true
				break
			}
		}
		if !drop {
			for alias := range aliasToRemove {
				if strings.Contains(line, alias+".Register") {
					drop = true
					break
				}
			}
		}
		if !drop {
			kept = append(kept, line)
		}
	}
	out := strings.Join(kept, "\n")
	//nolint:gosec // genPath stays inside the validated checkout (looksLikeTemplate).
	if werr := os.WriteFile(genPath, []byte(out), scaffoldFileMode); werr != nil {
		return &ExitError{code: 1, msg: "recipe uninstall: write units.gen.go: " + werr.Error()}
	}
	return nil
}

// loadUninstallReceipt reads the receipt a removal is based on, naming the two
// states that are not failures but are not removable either: never installed,
// and already removed. Split out of runRecipeUninstall to keep that function
// under the complexity gate after the read-only rail was added to it.
func loadUninstallReceipt(root, id string) (recipe.Receipt, error) {
	r, err := recipe.LoadReceipt(root, id)
	if err != nil {
		if os.IsNotExist(err) {
			return r, &ExitError{code: 1, msg: fmt.Sprintf(
				"recipe uninstall: %s is not installed (no %s)", id, recipe.ReceiptPath(id))}
		}
		return r, &ExitError{code: 1, msg: "recipe uninstall: " + err.Error()}
	}
	if r.Removed != "" {
		return r, &ExitError{code: 1, msg: fmt.Sprintf(
			"recipe uninstall: %s was already removed (%s)", id, r.Removed)}
	}
	return r, nil
}
