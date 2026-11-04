// SCOPE:plugin — installer CLI: the recipe commands (install, status).
//
// Spike 3 built the engine (plan, apply, receipt, drift) and deliberately left
// the dispatch out, because a command name is a product decision. This file is
// that decision: `gogogo recipe install <dir>` and `gogogo recipe status`.
//
// The install is deliberately not interactive: without --yes it prints the plan
// and changes nothing. That is the shape an agent can run twice safely, and it
// matches `--check`/`--dry-run` on the other commands.

package installer

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/calionauta/gogogo/internal/recipe"
)

// recipeManifestFile is the one file a recipe directory must carry. Its name
// starts with a dot so the recipe directory can still be a git checkout whose
// manifest is not part of the copied tree.
const recipeManifestFile = ".recipe.yml"

// recipeIDPattern is the receipt filename and the prefix a recipe may use for
// its tables and jobs: lowercase, no separators, so it can never escape
// .recipes/ or make two recipes collide by case.
var recipeIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// recipeManifest is the installable half of a recipe: everything the receipt
// needs except the file hashes, which are measured from the bytes copied.
type recipeManifest struct {
	ID          string             `yaml:"id"`
	Upstream    recipe.Upstream    `yaml:"upstream"`
	Declaration recipe.Declaration `yaml:"declaration"`
}

// runRecipe is the `recipe` subcommand group. A subcommand (not a top-level
// verb) because the noun is what the user names, and update/uninstall are the
// same noun with a different verb.
func runRecipe(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return &ExitError{code: 1, msg: "recipe needs a subcommand: " +
			"`recipe install <dir>` or `recipe status [id]`"}
	}
	switch args[0] {
	case "install":
		return runRecipeInstall(args[1:], stdout)
	case "status":
		return runRecipeStatus(args[1:], stdout)
	}
	return &ExitError{code: 1, msg: fmt.Sprintf(
		"unknown recipe subcommand %q (want install|status)", args[0])}
}

// recipeInstallArgs is the parsed CLI surface.
type recipeInstallArgs struct {
	src    string
	dir    string
	yes    bool
	format string
}

// runRecipeInstall plans, and with --yes applies, one recipe directory into a
// checkout. It is the whole install: copied tree plus receipt, no anchor, no
// core patch.
func runRecipeInstall(args []string, stdout io.Writer) error {
	opt, err := parseRecipeInstallArgs(args)
	if err != nil {
		return err
	}
	manifest, upstream, err := loadRecipeSource(opt.src)
	if err != nil {
		return err
	}
	root, err := filepath.Abs(opt.dir)
	if err != nil {
		return &ExitError{code: 1, msg: "recipe install: " + err.Error()}
	}
	if !looksLikeTemplate(root) {
		return &ExitError{code: 1, msg: "recipe install: " + opt.dir +
			" is not a gogogo checkout (no cmd/web/main.go or internal/installer/run.go); " +
			"install into the tree the recipe belongs to"}
	}
	// A receipt that exists but cannot be read must stop the install rather
	// than be treated as a fresh one: overwriting it would forget which
	// files the user owns.
	prev, rerr := recipe.LoadReceipt(root, manifest.ID)
	if rerr != nil && !os.IsNotExist(rerr) {
		return &ExitError{code: 1, msg: "recipe install: " + rerr.Error()}
	}
	if rerr == nil && prev.ID != manifest.ID {
		return &ExitError{code: 1, msg: fmt.Sprintf(
			"recipe install: %s already holds receipt for %q", recipe.ReceiptPath(manifest.ID), prev.ID)}
	}
	plan := recipe.Plan(prev, upstream, treeReader(root))
	if plan.Blocked() {
		printRecipePlan(stdout, manifest.ID, plan, opt.yes)
		return &ExitError{code: 1, msg: fmt.Sprintf(
			"recipe install: %s has %d file(s) you edited that upstream also changed; "+
				"refusing to apply — merge them by hand, then re-run", manifest.ID, len(plan.Conflicts))}
	}
	if !opt.yes {
		printRecipePlan(stdout, manifest.ID, plan, false)
		fmt.Fprintln(stdout, "recipe install: nothing changed; re-run with --yes to apply")
		return nil
	}
	res, aerr := recipe.Apply(root, plan)
	if aerr != nil {
		return &ExitError{code: 1, msg: "recipe install: " + aerr.Error()}
	}
	next := recipe.ReceiptAfter(prev, plan)
	next.ID = manifest.ID
	next.Upstream = manifest.Upstream
	next.Declaration = manifest.Declaration
	if serr := recipe.SaveReceipt(root, next); serr != nil {
		return &ExitError{code: 1, msg: "recipe install: write receipt: " + serr.Error()}
	}
	printRecipeInstalled(stdout, manifest, res, next)
	return checkInstalledDeclaration(root, next, stdout)
}

// checkInstalledDeclaration is the install-time drift gate: the receipt claims a
// surface, and the copied code either registers it or it does not. A mismatch is
// reported after the install rather than silently accepted, because a recipe that
// cannot honour its declaration is the one defect the receipt exists to expose.
func checkInstalledDeclaration(root string, r recipe.Receipt, stdout io.Writer) error {
	drift, err := recipe.Check(root, r)
	if err != nil {
		return &ExitError{code: 1, msg: "recipe install: drift check: " + err.Error()}
	}
	if len(drift.Problems) == 0 {
		return nil
	}
	var sb strings.Builder
	drift.Render(&sb)
	fmt.Fprint(stdout, sb.String())
	return &ExitError{code: 1, msg: "recipe install: installed, but the declaration does not match " +
		"the copied code (see DECL lines); fix the recipe, not the receipt"}
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

// loadRecipeSource reads and validates a recipe directory: the manifest first
// (it names the id and the declared surface), then every file it would copy.
// Nothing is written here.
func loadRecipeSource(src string) (recipeManifest, map[string]string, error) {
	var m recipeManifest
	base, err := filepath.Abs(src)
	if err != nil {
		return m, nil, &ExitError{code: 1, msg: "recipe install: " + err.Error()}
	}
	raw, merr := os.ReadFile(filepath.Join(base, recipeManifestFile))
	if merr != nil {
		return m, nil, &ExitError{code: 1, msg: "recipe install: " + src + " has no " +
			recipeManifestFile + " (it carries the id, the upstream and the declaration)"}
	}
	if uerr := yaml.Unmarshal(raw, &m); uerr != nil {
		return m, nil, &ExitError{code: 1, msg: "recipe install: parse " +
			filepath.Join(src, recipeManifestFile) + ": " + uerr.Error()}
	}
	if !recipeIDPattern.MatchString(m.ID) {
		return m, nil, &ExitError{code: 1, msg: fmt.Sprintf(
			"recipe install: id %q must be lowercase letters/digits/dashes and start with a letter", m.ID)}
	}
	files, ferr := collectRecipeFiles(base)
	if ferr != nil {
		return m, nil, ferr
	}
	if len(files) == 0 {
		return m, nil, &ExitError{code: 1, msg: "recipe install: " + src +
			" holds no files besides " + recipeManifestFile + "; a recipe with nothing to copy " +
			"would write a receipt claiming an empty surface"}
	}
	return m, files, nil
}

// collectRecipeFiles is the file set: every regular file under the recipe
// directory except the manifest, as repo-relative slash paths plus content. The
// layout is the destination layout, which is what keeps Go packages, Templ
// files and their imports where the code expects them.
func collectRecipeFiles(base string) (map[string]string, error) {
	rootFS, err := os.OpenRoot(base)
	if err != nil {
		return nil, &ExitError{code: 1, msg: "recipe install: " + err.Error()}
	}
	defer func() { _ = rootFS.Close() }()

	out := map[string]string{}
	walkErr := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// A recipe may itself be a checkout; .git is never part of it.
			if d.Name() == gitDirName {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(base, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if rel == recipeManifestFile {
			return nil
		}
		// Read through the root so a symlink swapped in after the walk cannot
		// escape the recipe directory the caller named.
		f, oerr := rootFS.Open(rel)
		if oerr != nil {
			return oerr
		}
		body, berr := io.ReadAll(f)
		_ = f.Close()
		if berr != nil {
			return berr
		}
		out[rel] = string(body)
		return nil
	})
	if walkErr != nil {
		return nil, &ExitError{code: 1, msg: "recipe install: read file set: " + walkErr.Error()}
	}
	return out, nil
}

// treeReader answers "what does the user have" for the planner, without the
// planner touching the filesystem.
func treeReader(root string) func(string) (string, bool) {
	return func(rel string) (string, bool) {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return "", false
		}
		return string(body), true
	}
}

// printRecipePlan is the human plan. It renders the engine's own plan and adds
// the one line the caller needs: what --yes would do.
func printRecipePlan(w io.Writer, id string, plan recipe.PlanResult, applied bool) {
	var sb strings.Builder
	plan.Render(&sb)
	fmt.Fprintf(w, "recipe %s — %s\n", id, strings.TrimSpace(strings.SplitN(sb.String(), "\n", 2)[0]))
	_, after, _ := strings.Cut(sb.String(), "\n")
	fmt.Fprint(w, after)
	if applied {
		fmt.Fprintln(w, "recipe install: applying the plan above")
	}
}

// printRecipeInstalled is the receipt: what landed, where it came from, and the
// two things the user must do that no installer can do for them.
func printRecipeInstalled(w io.Writer, m recipeManifest, res recipe.ApplyResult, r recipe.Receipt) {
	fmt.Fprintf(w, "recipe %s: %d file(s) installed", m.ID, len(res.Written))
	if len(res.Removed) > 0 {
		fmt.Fprintf(w, ", %d obsolete file(s) removed", len(res.Removed))
	}
	fmt.Fprintln(w)
	if m.Upstream.Repo != "" {
		fmt.Fprintf(w, "  baseline: %s", m.Upstream.Repo)
		if m.Upstream.Ref != "" {
			fmt.Fprintf(w, " @ %s", m.Upstream.Ref)
		}
		if m.Upstream.Path != "" {
			fmt.Fprintf(w, " (%s)", m.Upstream.Path)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "  receipt:  %s\n", recipe.ReceiptPath(r.ID))
	fmt.Fprintln(w, "  next:     the code is yours now — rename it, delete what you do not need.")
	fmt.Fprintln(w, "            register it through the seam (the handler's Recipe()) and keep")
	fmt.Fprintln(w, "            the declaration in step with the code; `gogogo recipe status` ")
	fmt.Fprintln(w, "            reports what you changed.")
}

// recipeStatusArgs is the parsed CLI surface for status.
type recipeStatusArgs struct {
	id     string
	dir    string
	format string
}

// runRecipeStatus reports the drift of one recipe (or every installed one): what
// is pristine, what the user edited, and whether the declaration still matches
// the copied code.
func runRecipeStatus(args []string, stdout io.Writer) error {
	opt, err := parseRecipeStatusArgs(args)
	if err != nil {
		return err
	}
	root, aerr := filepath.Abs(opt.dir)
	if aerr != nil {
		return &ExitError{code: 1, msg: "recipe status: " + aerr.Error()}
	}
	if !looksLikeTemplate(root) {
		return &ExitError{code: 1, msg: "recipe status: " + opt.dir + " is not a gogogo checkout"}
	}
	ids, ierr := statusIDs(root, opt.id)
	if ierr != nil {
		return ierr
	}
	if len(ids) == 0 {
		fmt.Fprintln(stdout, "recipe status: no receipts in "+recipe.ReceiptDir+
			"/ — nothing is installed yet")
		return nil
	}
	problems := 0
	for _, id := range ids {
		r, rerr := recipe.LoadReceipt(root, id)
		if rerr != nil {
			return &ExitError{code: 1, msg: "recipe status: " + rerr.Error()}
		}
		drift, derr := recipe.Check(root, r)
		if derr != nil {
			return &ExitError{code: 1, msg: "recipe status: " + derr.Error()}
		}
		problems += len(drift.Problems)
		if opt.format == planFormatJSON {
			if encErr := printDriftJSON(stdout, drift); encErr != nil {
				return encErr
			}
			continue
		}
		var sb strings.Builder
		drift.Render(&sb)
		fmt.Fprint(stdout, sb.String())
	}
	if problems > 0 {
		return &ExitError{code: 1, msg: fmt.Sprintf(
			"recipe status: %d declaration problem(s) — the receipt and the code disagree", problems)}
	}
	return nil
}

// statusIDs resolves "one recipe" or "every installed recipe" into ids.
func statusIDs(root, id string) ([]string, error) {
	if id != "" {
		if _, err := recipe.LoadReceipt(root, id); err != nil {
			return nil, &ExitError{code: 1, msg: "recipe status: " + err.Error()}
		}
		return []string{id}, nil
	}
	ids, err := recipe.ListReceipts(root)
	if err != nil {
		return nil, &ExitError{code: 1, msg: "recipe status: " + err.Error()}
	}
	sort.Strings(ids)
	return ids, nil
}

// parseRecipeStatusArgs accepts the optional id before or after the flags.
func parseRecipeStatusArgs(args []string) (recipeStatusArgs, error) {
	opt := recipeStatusArgs{}
	fs := flag.NewFlagSet("recipe status", flag.ContinueOnError)
	fs.StringVar(&opt.dir, "dir", ".", "checkout to inspect")
	fs.StringVar(&opt.format, "format", planFormatText, "output format: text|json")
	fs.SetOutput(io.Discard)

	rest := args
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		opt.id = rest[0]
		rest = rest[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return opt, &ExitError{code: 1, msg: "recipe status: " + err.Error()}
	}
	if opt.id == "" && fs.NArg() > 0 {
		opt.id = fs.Arg(0)
	}
	if fs.NArg() > 1 {
		return opt, &ExitError{code: 1, msg: "recipe status takes at most one recipe id"}
	}
	if opt.format != planFormatText && opt.format != planFormatJSON {
		return opt, &ExitError{code: 1, msg: fmt.Sprintf("unknown --format %q (want text|json)", opt.format)}
	}
	return opt, nil
}

// printDriftJSON is the machine shape of one recipe's drift.
func printDriftJSON(w io.Writer, d recipe.Drift) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(d)
}
