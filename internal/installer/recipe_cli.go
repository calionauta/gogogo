// Installer CLI: the recipe commands (install, update,
// uninstall, status).
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
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
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
	ID string `yaml:"id"`
	// Summary is what the recipe does, in the author's words. Install copies
	// it into the receipt, where discovery reads it; empty is allowed for
	// older manifests but `recipe check` asks for it on installed recipes.
	Summary string `yaml:"summary,omitempty"`
	// Version is the author's version claim, vMAJOR.MINOR.PATCH. Optional:
	// without it the receipt records no version and `recipe outdated`
	// honestly reports unknown instead of guessing.
	Version     string             `yaml:"version,omitempty"`
	Upstream    recipe.Upstream    `yaml:"upstream"`
	Declaration recipe.Declaration `yaml:"declaration"`
	// Register is the repo-relative package exporting `Register`. Empty is a
	// data-only recipe: installed and receipted, but it mounts nothing.
	Register string `yaml:"register,omitempty"`
	// Module is the Go module the copied imports were written against. Install
	// rewrites it to the target tree's own module, because a recipe is written
	// once and installed into projects whose module path differs.
	Module string `yaml:"module,omitempty"`
}

// runRecipe is the `recipe` subcommand group. A subcommand (not a top-level
// verb) because the noun is what the user names: lifecycle verbs
// (install/update/status/uninstall/outdated) and discovery verbs
// (list/describe/invoke) are the same noun with different verbs.
func runRecipe(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return &ExitError{code: 1, msg: "recipe needs a subcommand: " +
			"`recipe install <dir>`, `recipe check [id]`, `recipe test [id]`, `recipe status [id]`, " +
			"`recipe outdated [id]`, `recipe uninstall <id>`, `recipe list`, `recipe describe <id>` " +
			"or `recipe invoke <id> <verb>`"}
	}
	switch args[0] {
	case "install":
		return runRecipeInstall(args[1:], stdout, false)
	case "update":
		return runRecipeInstall(args[1:], stdout, true)
	case "check":
		return runRecipeCheck(args[1:], stdout)
	case "test":
		return runRecipeTest(ctx, args[1:], stdout, stderr)
	case "status":
		return runRecipeStatus(args[1:], stdout)
	case "outdated":
		return runRecipeOutdated(args[1:], stdout)
	case "uninstall":
		return runRecipeUninstall(args[1:], stdout)
	case "list":
		return runRecipeList(args[1:], stdout)
	case "describe", "show":
		return runRecipeDescribe(args[1:], stdout)
	case "invoke":
		return runRecipeInvoke(ctx, args[1:], stdout)
	}
	return &ExitError{code: 1, msg: fmt.Sprintf(
		"unknown recipe subcommand %q (want install|check|test|status|outdated|uninstall|list|describe|invoke)", args[0])}
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
	if m.Version != "" && !recipe.IsValidVersion(m.Version) {
		return m, nil, &ExitError{code: 1, msg: fmt.Sprintf(
			"recipe install: version %q must be vMAJOR.MINOR.PATCH (e.g. v1.2.0)", m.Version)}
	}
	if m.Register != "" {
		if verr := validateRegisterPath(m.Register); verr != nil {
			return m, nil, verr
		}
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
	if m.Register != "" && !fileSetHasDir(files, m.Register) {
		return m, nil, &ExitError{code: 1, msg: fmt.Sprintf(
			"recipe install: register names %q, but the recipe ships no files under it "+
				"(the generated registration would import a package that does not exist)", m.Register)}
	}
	// A declared slot is content the receipt claims. If no page in this build
	// renders it the contribution is dropped, so the refusal has to happen before
	// a single file is copied — after the copy it reads as a working install.
	if problems := recipe.SlotProblems(m.Declaration.Slots); len(problems) > 0 {
		return m, nil, &ExitError{code: 1, msg: fmt.Sprintf(
			"recipe install: %s declares content this build cannot render:\n  - %s",
			m.ID, strings.Join(problems, "\n  - "))}
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
// steps that remain. Everything the command could do for the user it reports in
// the past tense — the one thing it could not (a generator that is not
// installed) is named with the exact command that finishes it.
func printRecipeInstalled(w io.Writer, m recipeManifest, res recipe.ApplyResult, r recipe.Receipt,
	schema, policy []string, codegen codegenReport,
) {
	fmt.Fprintf(w, "recipe %s: %d file(s) installed", m.ID, len(res.Written))
	if len(res.Removed) > 0 {
		fmt.Fprintf(w, ", %d obsolete file(s) removed", len(res.Removed))
	}
	fmt.Fprintln(w)
	if m.Version != "" {
		fmt.Fprintf(w, "  version: %s\n", m.Version)
	}
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
	if r.Register != "" {
		fmt.Fprintf(w, "  register: %s (was added to %s)\n", r.Register, recipeRegistrationFile)
	}
	if codegen.Templ != "" {
		fmt.Fprintln(w, "  pages:    templ generated the recipe's _templ.go in the tree")
	}
	if len(schema) > 0 {
		fmt.Fprintf(w, "  schema:   %d migration file(s) mirrored to %s\n", len(schema), recipeSchemaDir)
		switch codegen.SQLC {
		case codegenRan:
			fmt.Fprintln(w, "  queries:  regenerated (internal/db/sqlcgen) — the recipe's queries compile")
		case codegenSkipped:
			fmt.Fprintln(w, "            sqlc is not on PATH: run `make sqlc` so the queries it ships compile")
		}
	}
	if len(policy) > 0 {
		fmt.Fprintf(w, "  policy:   %d row(s) in %s (removed by `recipe uninstall`)\n",
			len(policy), recipePolicyFile)
	}
	if declaresNav(r.Declaration) {
		fmt.Fprintln(w, navDigestNotice)
	}
	fmt.Fprintln(w, "  next:     the code is yours now — rename it, delete what you do not need.")
	fmt.Fprintln(w, "            register it through the seam (the handler's Recipe()) and keep")
	fmt.Fprintln(w, "            the declaration in step with the code; `gogogo recipe status` ")
	fmt.Fprintln(w, "            reports what you changed.")
}

// navDigestNotice is the one gate a nav-contributing recipe turns red on purpose.
//
// The page digests pin the shell's structure, so a new rail destination changes
// every page's digest — which is the gate working, not a regression. The fix is
// to read the diff and re-bless, and nobody guesses that command from a red test.
const navDigestNotice = "  nav:      a sidebar entry was added, so the page digests moved — read the diff,\n" +
	"            then re-bless with: PAGES_DIGEST=update go test ./internal/server -run TestPagesDigest"

// declaresNav reports whether a declaration contributes a sidebar destination.
func declaresNav(d recipe.Declaration) bool {
	return slices.Contains(d.Slots, string(recipe.SlotNavSidebar))
}

// validateRegisterPath refuses a path that is not a clean repo-relative package
// directory: an absolute path or a ".." would make the generated import point
// outside the module.
func validateRegisterPath(rel string) error {
	if path.IsAbs(rel) || path.Clean(rel) != rel || rel == "." || rel == ".." ||
		strings.HasPrefix(rel, "../") {
		return &ExitError{code: 1, msg: fmt.Sprintf(
			"recipe install: register %q must be a clean repo-relative package path "+
				"(e.g. features/pomodoro/handlers)", rel)}
	}
	return nil
}

// fileSetHasDir reports whether the recipe carries code under dir.
func fileSetHasDir(files map[string]string, dir string) bool {
	prefix := strings.TrimSuffix(dir, "/") + "/"
	for p := range files {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}
