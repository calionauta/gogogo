// SCOPE:plugin — installer CLI: `recipe install` and `recipe update`.
package installer

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/calionauta/gogogo/internal/recipe"
)

// recipeInstallArgs is the parsed CLI surface.
type recipeInstallArgs struct {
	src    string
	dir    string
	yes    bool
	format string
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
	manifest, upstream, err := loadRecipeSource(opt.src)
	if err != nil {
		return err
	}
	root, prev, err := recipeInstallTarget(opt.dir, manifest.ID, requireExisting)
	if err != nil {
		return err
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
	printRecipeInstalled(stdout, manifest, res, next)
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
	fs.BoolVar(&opt.yes, "yes", false, "apply the plan (without it the command prints and changes nothing)")
	fs.StringVar(&opt.format, "format", planFormatText, "output format: text|json")
	fs.SetOutput(io.Discard)

	rest := args
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		opt.src = rest[0]
		rest = rest[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return opt, &ExitError{code: 1, msg: "recipe install: " + err.Error()}
	}
	if opt.src == "" && fs.NArg() > 0 {
		opt.src = fs.Arg(0)
	}
	if opt.src == "" {
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
