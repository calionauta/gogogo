// SCOPE:plugin — installer CLI: `recipe install` and `recipe update`.
package installer

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/calionauta/gogogo/internal/recipe"
)

// recipeInstallArgs is the parsed CLI surface.
type recipeInstallArgs struct {
	src    string
	entry  string
	dir    string
	yes    bool
	format string
}

// recipeSource is the install input both loaders produce: the manifest the
// receipt needs plus the bytes to copy. A recipe directory and a reviewed catalog
// entry differ only in where the two come from.
type recipeSource struct {
	manifest recipeManifest
	files    map[string]string
}

// runRecipeInstall plans, and with --yes applies, one recipe directory into a
// checkout. It is the whole install: copied tree plus receipt plus the generated
// registration, no anchor and no core patch. requireExisting distinguishes
// `update` (a receipt must be there) from `install`.
func runRecipeInstall(args []string, stdout io.Writer, requireExisting bool) error {
	opt, err := parseRecipeInstallArgs(args)
	if err != nil {
		return err
	}
	source, err := loadInstallSource(opt)
	if err != nil {
		return err
	}
	manifest, upstream := source.manifest, source.files
	root, prev, err := recipeInstallTarget(opt.dir, manifest.ID, requireExisting)
	if err != nil {
		return err
	}
	upstream, err = rebaseRecipeImports(root, manifest.Module, upstream)
	if err != nil {
		return err
	}
	if cerr := assertNoReceiptCollision(root, manifest.ID, upstream); cerr != nil {
		return cerr
	}
	plan := recipe.Plan(prev, upstream, treeReader(root))
	if plan.Blocked() {
		return blockedInstall(stdout, manifest, plan, opt)
	}
	if !opt.yes {
		if opt.format == planFormatJSON {
			return printRecipeInstallJSON(stdout, manifest, plan, recipe.ApplyResult{}, false, nil)
		}
		printRecipePlan(stdout, manifest.ID, plan, false)
		fmt.Fprintln(stdout, "recipe install: nothing changed; re-run with --yes to apply")
		return nil
	}
	return applyRecipePlan(root, manifest, prev, plan, opt, stdout)
}

// recipeInstallTarget resolves the checkout, refuses one that is not gogogo, and
// loads the receipt that is already there (a zero receipt for a fresh install).
func recipeInstallTarget(dir, id string, requireExisting bool) (string, recipe.Receipt, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return "", recipe.Receipt{}, &ExitError{code: 1, msg: "recipe install: " + err.Error()}
	}
	if !looksLikeTemplate(root) {
		return "", recipe.Receipt{}, &ExitError{code: 1, msg: "recipe install: " + dir +
			" is not a gogogo checkout (no cmd/web/main.go or internal/installer/run.go); " +
			"install into the tree the recipe belongs to"}
	}
	// A receipt that exists but cannot be read must stop the install rather
	// than be treated as a fresh one: overwriting it would forget which
	// files the user owns.
	prev, rerr := recipe.LoadReceipt(root, id)
	if rerr != nil && !os.IsNotExist(rerr) {
		return "", recipe.Receipt{}, &ExitError{code: 1, msg: "recipe install: " + rerr.Error()}
	}
	if requireExisting && os.IsNotExist(rerr) {
		return "", recipe.Receipt{}, &ExitError{code: 1, msg: fmt.Sprintf(
			"recipe update: %s is not installed (no %s); use `recipe install` first",
			id, recipe.ReceiptPath(id))}
	}
	if rerr == nil && prev.Removed != "" {
		// A reinstall after an uninstall: the files the uninstall deleted were
		// removed by the tool, not by the user, so they must come back. Dropping
		// the vanished entries from the baseline makes the planner write them;
		// entries whose file survived stay, so the user's kept bytes are still
		// compared against their pristine hash.
		prev.Removed = ""
		for p := range prev.Files {
			if _, exists := treeReader(root)(p); !exists {
				delete(prev.Files, p)
			}
		}
	}
	if rerr == nil && prev.ID != id {
		return "", recipe.Receipt{}, &ExitError{code: 1, msg: fmt.Sprintf(
			"recipe install: %s already holds receipt for %q", recipe.ReceiptPath(id), prev.ID)}
	}
	return root, prev, nil
}

// blockedInstall reports a blocked plan in the caller's format and returns the
// refusal.
func blockedInstall(stdout io.Writer, manifest recipeManifest, plan recipe.PlanResult, opt recipeInstallArgs) error {
	if opt.format == planFormatJSON {
		if jerr := printRecipeInstallJSON(stdout, manifest, plan, recipe.ApplyResult{}, false, nil); jerr != nil {
			return jerr
		}
	} else {
		printRecipePlan(stdout, manifest.ID, plan, opt.yes)
	}
	return &ExitError{code: 1, msg: fmt.Sprintf(
		"recipe install: %s has %d file(s) you edited that upstream also changed; "+
			"refusing to apply — merge them by hand, then re-run", manifest.ID, len(plan.Conflicts))}
}

// applyRecipePlan applies an unblocked plan, writes the receipt, regenerates the
// registration, and checks the declaration against the copied code.
func applyRecipePlan(root string, manifest recipeManifest, prev recipe.Receipt,
	plan recipe.PlanResult, opt recipeInstallArgs, stdout io.Writer,
) error {
	res, aerr := recipe.Apply(root, plan)
	if aerr != nil {
		return &ExitError{code: 1, msg: "recipe install: " + aerr.Error()}
	}
	next := recipe.ReceiptAfter(prev, plan)
	next.ID = manifest.ID
	next.Upstream = manifest.Upstream
	next.Declaration = manifest.Declaration
	next.Register = manifest.Register
	// A reinstall of a removed recipe is a new install: the generated
	// registration must import it again.
	next.Removed = ""
	if serr := recipe.SaveReceipt(root, next); serr != nil {
		return &ExitError{code: 1, msg: "recipe install: write receipt: " + serr.Error()}
	}
	// A recipe that owns tables ships its DDL; sqlc reads directories, so the
	// SQL is also written where codegen can see it (see recipe_schema.go). It
	// mirrors the tree, not upstream, so an edited migration is the one both
	// codegen and the runtime read.
	schema, serr := syncRecipeSchema(root, manifest.ID, next.Files)
	if serr != nil {
		return &ExitError{code: 1, msg: "recipe install: " + serr.Error()}
	}
	if gerr := regenerateRegistration(root); gerr != nil {
		return gerr
	}
	drift, derr := recipe.Check(root, next)
	if derr != nil {
		return &ExitError{code: 1, msg: "recipe install: drift check: " + derr.Error()}
	}
	if opt.format == planFormatJSON {
		return printRecipeInstallJSON(stdout, manifest, plan, res, true, drift.Problems)
	}
	printRecipeInstalled(stdout, manifest, res, next, schema)
	if len(drift.Problems) > 0 {
		var sb strings.Builder
		drift.Render(&sb)
		fmt.Fprint(stdout, sb.String())
		return &ExitError{code: 1, msg: "recipe install: installed, but the declaration does not match " +
			"the copied code (see DECL lines); fix the recipe, not the receipt"}
	}
	return nil
}

// parseRecipeInstallArgs accepts the recipe directory before or after the flags,
// the same rule `add` and `export` follow.
func parseRecipeInstallArgs(args []string) (recipeInstallArgs, error) {
	opt := recipeInstallArgs{}
	fs := flag.NewFlagSet("recipe install", flag.ContinueOnError)
	fs.StringVar(&opt.dir, "dir", ".", "target checkout to install into")
	fs.StringVar(&opt.entry, "entry", "",
		"catalog entry (.yml) to install from; the positional argument is then the contents directory")
	fs.BoolVar(&opt.yes, "yes", false, "apply the plan (without it the command prints and changes nothing)")
	fs.StringVar(&opt.format, "format", planFormatText, "output format: text|json")
	fs.SetOutput(io.Discard)

	// The positional may sit anywhere among the flags (`--entry e.yml ./dir
	// --dir x`), and the stdlib parser stops at the first non-flag token. Pull it
	// out first, knowing which flags take a value so a value is not mistaken for
	// the positional.
	src, rest := splitInstallPositional(args)
	opt.src = src
	if err := fs.Parse(rest); err != nil {
		return opt, &ExitError{code: 1, msg: "recipe install: " + err.Error()}
	}
	if opt.src == "" && fs.NArg() > 0 {
		opt.src = fs.Arg(0)
	}
	if opt.src == "" {
		if opt.entry != "" {
			return opt, &ExitError{code: 1, msg: "recipe install --entry needs the pristine " +
				"contents directory (`recipe install --entry catalog/pomodoro/v1.2.0.yml " +
				"./downloaded/pomodoro`)"}
		}
		return opt, &ExitError{code: 1, msg: "recipe install needs a recipe directory " +
			"(`recipe install ./extensions/pomodoro`) or " +
			"(`recipe install pomodoro`); `gogogo --help` lists the offered units"}
	}
	if fs.NArg() > 1 {
		return opt, &ExitError{code: 1, msg: "recipe install takes one recipe directory"}
	}
	if opt.format != planFormatText && opt.format != planFormatJSON {
		return opt, &ExitError{code: 1, msg: fmt.Sprintf("unknown --format %q (want text|json)", opt.format)}
	}
	return opt, nil
}

// recipeInstallReport is the machine shape of one install (or a plan-only run).
type recipeInstallReport struct {
	ID          string   `json:"id"`
	Applied     bool     `json:"applied"`
	Write       int      `json:"write"`
	FastForward int      `json:"fastForward"`
	Keep        int      `json:"keep"`
	Obsolete    int      `json:"obsolete"`
	Conflicts   []string `json:"conflicts,omitempty"`
	Written     []string `json:"written,omitempty"`
	Removed     []string `json:"removed,omitempty"`
	Skipped     []string `json:"skipped,omitempty"`
	Receipt     string   `json:"receipt,omitempty"`
	Register    string   `json:"register,omitempty"`
	Problems    []string `json:"declarationProblems,omitempty"`
}

// printRecipeInstallJSON renders a plan and (when applied) what landed, so an
// agent can decide without parsing prose.
func printRecipeInstallJSON(w io.Writer, m recipeManifest, plan recipe.PlanResult,
	res recipe.ApplyResult, applied bool, problems []string,
) error {
	write, ff, keep, obsolete := plan.Counts()
	rep := recipeInstallReport{
		ID: m.ID, Applied: applied,
		Write: write, FastForward: ff, Keep: keep, Obsolete: obsolete,
		Conflicts: plan.Conflicts, Written: res.Written, Removed: res.Removed,
		Skipped: res.Skipped, Problems: problems,
	}
	if applied {
		rep.Receipt = recipe.ReceiptPath(m.ID)
		rep.Register = m.Register
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}

// rebaseRecipeImports rewrites the module the recipe was written against to the
// target tree's own. A recipe is authored once and installed into projects whose
// module path differs, so a copied Go import of the author's module would not
// resolve. Only Go and Templ sources are rewritten: a README URL is not an
// import, and rewriting it would corrupt prose.
func rebaseRecipeImports(root, from string, files map[string]string) (map[string]string, error) {
	if from == "" {
		return files, nil
	}
	to, err := modulePath(root)
	if err != nil {
		return nil, err
	}
	if from == to {
		return files, nil
	}
	out := make(map[string]string, len(files))
	for p, body := range files {
		if strings.HasSuffix(p, ".go") || strings.HasSuffix(p, ".templ") {
			out[p] = strings.ReplaceAll(body, from+"/", to+"/")
			continue
		}
		out[p] = body
	}
	return out, nil
}

// assertNoReceiptCollision refuses a recipe whose file set would land on a file
// another installed recipe owns. That is the copy hazard the unit map cannot
// express (trim bundles share directories on purpose): two receipts claiming one
// path means an uninstall of either deletes code the other still needs, and the
// second install silently overwrote the first.
func assertNoReceiptCollision(root, id string, upstream map[string]string) error {
	ids, err := recipe.ListReceipts(root)
	if err != nil {
		return &ExitError{code: 1, msg: "recipe install: list receipts: " + err.Error()}
	}
	for _, other := range ids {
		if other == id {
			continue
		}
		r, lerr := recipe.LoadReceipt(root, other)
		if lerr != nil {
			return &ExitError{code: 1, msg: "recipe install: read receipt " + other + ": " + lerr.Error()}
		}
		if r.Removed != "" {
			continue
		}
		for p := range upstream {
			if _, claimed := r.Files[p]; !claimed {
				continue
			}
			return &ExitError{code: 1, msg: fmt.Sprintf(
				"recipe install: %s ships %s, which %s already installed — two recipes owning "+
					"one file is what uninstall cannot undo safely", id, p, other)}
		}
	}
	return nil
}

// loadInstallSource reads either a recipe directory (the author's shape) or a
// reviewed catalog entry plus the directory holding its bytes. Both become the
// same manifest + file set, so the install path below has one shape.
func loadInstallSource(opt recipeInstallArgs) (recipeSource, error) {
	if opt.entry != "" {
		return loadCatalogSource(opt.entry, opt.src)
	}
	manifest, files, err := loadRecipeSource(opt.src)
	if err != nil {
		return recipeSource{}, err
	}
	return recipeSource{manifest: manifest, files: files}, nil
}

// loadCatalogSource verifies the bytes against the entry before anything is
// planned: every reviewed file must be there and hash the same, and no
// unreviewed file may be installed. That is the whole point of a catalog entry —
// the review is about bytes, not about a name.
func loadCatalogSource(entryPath, contentsDir string) (recipeSource, error) {
	var out recipeSource
	entry, err := recipe.LoadCatalogEntry(entryPath)
	if err != nil {
		return out, &ExitError{code: 1, msg: "recipe install: " + err.Error()}
	}
	dir, aerr := filepath.Abs(contentsDir)
	if aerr != nil {
		return out, &ExitError{code: 1, msg: "recipe install: " + aerr.Error()}
	}
	files, ferr := collectRecipeFiles(dir)
	if ferr != nil {
		return out, ferr
	}
	for p, want := range entry.Files {
		got, ok := files[p]
		if !ok {
			return out, &ExitError{code: 1, msg: fmt.Sprintf(
				"recipe install: %s is missing %s, which %s %s reviewed",
				contentsDir, p, entry.ID, entry.Version)}
		}
		if h := recipe.Hash([]byte(got)); h != strings.TrimPrefix(want, "sha256:") {
			return out, &ExitError{code: 1, msg: fmt.Sprintf(
				"recipe install: %s does not hash to the reviewed bytes of %s %s; "+
					"refusing to install code the review did not see", p, entry.ID, entry.Version)}
		}
	}
	var extra []string
	for p := range files {
		if _, ok := entry.Files[p]; !ok {
			extra = append(extra, p)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return out, &ExitError{code: 1, msg: "recipe install: " + contentsDir +
			" ships files the entry did not review: " + strings.Join(extra, ", ")}
	}
	module := entry.Module
	if module == "" {
		// Go modules are named by their repo path, so the source repo is the
		// honest default when the entry does not name one.
		module = entry.Source.Repo
	}
	out.manifest = recipeManifest{
		ID:       entry.ID,
		Upstream: entry.Source,
		Declaration: recipe.Declaration{
			Routes: entry.Routes,
			Tables: entry.Tables,
			Jobs:   entry.Jobs,
			Slots:  entry.Slots,
		},
		Register: entry.Register,
		Module:   module,
	}
	out.files = files
	return out, nil
}

// splitInstallPositional extracts the single positional argument (the recipe
// directory or the contents directory) without consuming a flag's value.
func splitInstallPositional(args []string) (src string, rest []string) {
	valueFlags := map[string]bool{
		"--entry": true, "--dir": true, "--format": true,
		"-entry": true, "-dir": true, "-format": true,
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			if src == "" {
				src = arg
				continue
			}
			rest = append(rest, arg)
			continue
		}
		name, _, hasValue := strings.Cut(arg, "=")
		rest = append(rest, arg)
		if !hasValue && valueFlags[name] && i+1 < len(args) {
			i++
			rest = append(rest, args[i])
		}
	}
	return src, rest
}
