// Installer engine: `gogogo new` scaffolds one feature.
//
// The template's own answer to "copy the todo slice" was prose: a new session
// read four directories, guessed the wiring, and learned about the RBAC
// coverage lint only after `make ci-local` failed. This command makes the
// skeleton mechanical and proves it: it writes the handler + page, wires the
// composition root, registers the unit, adds the policy rows, regenerates the
// generated docs, and builds.
//
// It is deliberately opinionated: one page (`GET /<name>`, auth-gated) and one
// JSON probe (`GET /api/<name>/ping`). Grow it from there — the gate that keeps
// a grown feature honest is the same one that guards every other unit.
package installer

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/calionauta/gogogo/internal/capabilities"
	"github.com/calionauta/gogogo/internal/prompts"
	"github.com/calionauta/gogogo/internal/recipe"
)

// newNameRE is the kebab-case id every capability, directory and route shares:
// no leading/trailing hyphen, no double hyphen, lowercase only.
var newNameRE = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// newOptions is the parsed `gogogo new feature` invocation.
type newOptions struct {
	name   string
	dir    string
	dryRun bool
	yes    bool
	format string
}

// newPaths are the files the scaffold touches, relative to the project root.
type newPaths struct {
	handler  string
	page     string
	prompt   string
	chi      string
	registry string
	policy   string
}

func newProjectPaths(name string) newPaths {
	return newPaths{
		handler:  filepath.ToSlash(filepath.Join("features", name, "handlers", "chi.go")),
		page:     filepath.ToSlash(filepath.Join("features", name, "components", "page.templ")),
		prompt:   filepath.ToSlash(filepath.Join(prompts.Dir, "panels", name+".prompt")),
		chi:      "internal/server/chi.go",
		registry: "internal/capabilities/registry.go",
		policy:   "internal/rbac/casbin/policy.csv",
	}
}

// runNew is the subcommand entrypoint: parse, plan, confirm, apply, prove.
func runNew(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	opt, err := parseNewArgs(args, stdout)
	if err != nil {
		return err
	}
	plan, err := buildNewPlan(opt)
	if err != nil {
		return err
	}
	// JSON apply mirrors the scaffold flow: the plan is what --dry-run prints,
	// and an apply emits exactly one envelope (no plan + progress + result
	// mixed into one stream a client cannot parse).
	jsonApply := opt.format == planFormatJSON && !opt.dryRun
	if !jsonApply {
		if planErr := printNewPlan(stdout, opt, plan); planErr != nil {
			return planErr
		}
		if opt.dryRun {
			return nil
		}
	}
	if !opt.yes && !confirmNew(stdout, stdin, opt.name) {
		fmt.Fprintln(stdout, "gogogo: aborted — nothing changed (re-run with --yes to skip this prompt)")
		return nil
	}
	progress := stdout
	if jsonApply {
		progress = io.Discard
	}
	err = applyNew(ctx, opt, plan, progress)
	if jsonApply {
		printNewEnvelope(stdout, opt, plan, err)
		return err
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, "gogogo: done — review the diff, then run `make dev`")
	return nil
}

// newEnvelope is the single JSON document a mutating `new` emits.
type newEnvelope struct {
	Name       string   `json:"name"`
	Dir        string   `json:"dir"`
	Created    []string `json:"created"`
	Edits      []string `json:"edits"`
	BuildOk    bool     `json:"buildOk"`
	BuildError string   `json:"buildError,omitempty"`
	Next       []string `json:"next"`
}

// printNewEnvelope writes the apply result. A proof failure is reported in
// the envelope AND returned, so both a human reading the transcript and an
// agent branching on the exit code see the same verdict.
func printNewEnvelope(w io.Writer, opt newOptions, plan newPlan, applyErr error) {
	env := newEnvelope{
		Name:    opt.name,
		Dir:     opt.dir,
		Created: []string{plan.paths.handler, plan.paths.page},
		Edits:   []string{plan.paths.chi, plan.paths.registry, plan.paths.policy},
		BuildOk: applyErr == nil,
		Next:    newNextSteps(opt.name),
	}
	if applyErr != nil {
		env.BuildError = applyErr.Error()
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(env)
}

// parseNewArgs accepts both `new reports` and `new --dir X reports`.
func parseNewArgs(args []string, stdout io.Writer) (newOptions, error) {
	fs := flag.NewFlagSet("gogogo new feature", flag.ContinueOnError)
	var opt newOptions
	fs.StringVar(&opt.dir, "dir", ".", "project checkout (default: current directory)")
	fs.BoolVar(&opt.dryRun, "dry-run", false, "print the plan and stop; changes nothing")
	fs.BoolVar(&opt.yes, "yes", false, "apply without asking (agents: always pin this)")
	fs.StringVar(&opt.format, "format", planFormatText, "plan format: text|json")
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprintln(stdout, "usage: gogogo new feature <name> [--dir PROJECT] [--dry-run] [--yes] [--format text|json]")
		fs.PrintDefaults()
	}
	// The stdlib parser stops at the first positional, so pull the name out
	// first and parse the rest (same positional-first rule the other
	// subcommands follow). The literal
	// "feature" is accepted because the command reads better as
	// `gogogo new feature reports`; both spellings are the same command.
	words := make([]string, 0, len(args))
	rest := args
	if len(rest) > 0 && rest[0] == "feature" {
		rest = rest[1:]
	}
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		words = append(words, rest[0])
		rest = rest[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return opt, err
	}
	words = append(words, fs.Args()...)
	if len(words) != 1 {
		fs.Usage()
		return opt, &ExitError{code: 1, msg: "usage: gogogo new feature <name> [--dir PROJECT] [--dry-run] [--yes]"}
	}
	if opt.format != planFormatText && opt.format != planFormatJSON {
		return opt, fmt.Errorf("unknown --format %q (want text|json)", opt.format)
	}
	opt.name = words[0]
	if err := validateNewName(opt.name); err != nil {
		return opt, err
	}
	return opt, nil
}

// validateNewName rejects ids that cannot become a directory, a route and an
// import in one shot, plus anything the route space already owns.
func validateNewName(name string) error {
	if len(name) < 2 || len(name) > 32 || !newNameRE.MatchString(name) {
		return &ExitError{code: 1, msg: fmt.Sprintf(
			"invalid feature name %q: use kebab-case (a-z, 0-9, -), 2-32 chars", name)}
	}
	if _, taken := capabilities.ByID()[name]; taken {
		return &ExitError{code: 1, msg: fmt.Sprintf(
			"capability %q already exists — pick another name", name)}
	}
	if slices.Contains(reservedNames, name) {
		return &ExitError{code: 1, msg: fmt.Sprintf("%q is a reserved route prefix", name)}
	}
	return nil
}

// reservedNames are path prefixes the template already serves; a feature
// named after one would shadow or collide with an existing mount.
var reservedNames = []string{"api", "static", "health", "login", "logout", "console", "dagnats", "sw", "mux", "wb"}

// newPlan is everything the scaffold will create and edit, computed before
// anything is written so a refusal leaves the tree untouched.
type newPlan struct {
	paths  newPaths
	chi    string
	reg    string
	policy string
	// module is the target project's module path (read from go.mod): every
	// real checkout is renamed, so generated imports must use it.
	module string
}

// buildNewPlan validates the checkout and prepares the edited file contents.
func buildNewPlan(opt newOptions) (newPlan, error) {
	var plan newPlan
	root, err := filepath.Abs(opt.dir)
	if err != nil {
		return plan, &ExitError{code: 1, msg: "cannot resolve --dir: " + opt.dir}
	}
	plan.paths = newProjectPaths(opt.name)
	for _, required := range []string{"go.mod", plan.paths.chi, plan.paths.registry, plan.paths.policy} {
		if _, statErr := os.Stat(filepath.Join(root, required)); statErr != nil {
			return plan, &ExitError{code: 1, msg: fmt.Sprintf(
				"%s is not a gogogo checkout (missing %s) — `gogogo new` only edits scaffolded trees",
				root, required)}
		}
	}
	if _, statErr := os.Stat(filepath.Join(root, "features", opt.name)); statErr == nil {
		return plan, &ExitError{code: 1, msg: "features/" + opt.name + " already exists — nothing to scaffold"}
	}
	module, modErr := goModModule(filepath.Join(root, "go.mod"))
	if modErr != nil {
		return plan, &ExitError{code: 1, msg: "read module path from go.mod: " + modErr.Error()}
	}
	plan.module = module
	targets, err := readNewTargets(root, plan.paths)
	if err != nil {
		return plan, err
	}
	plan.chi, err = editChiGo(targets.chi, opt.name, plan.module)
	if err != nil {
		return plan, err
	}
	plan.reg, err = editRegistry(targets.registry, opt.name)
	if err != nil {
		return plan, err
	}
	plan.policy = appendPolicyRows(targets.policy, opt.name)
	return plan, nil
}

// newTargets holds the current contents of the files the plan edits.
type newTargets struct {
	chi      string
	registry string
	policy   string
}

func readNewTargets(root string, paths newPaths) (newTargets, error) {
	var t newTargets
	for label, path := range map[string]*string{
		"chi":      &t.chi,
		"registry": &t.registry,
		"policy":   &t.policy,
	} {
		raw, err := os.ReadFile(filepath.Join(root, paths.fileFor(label)))
		if err != nil {
			return t, &ExitError{code: 1, msg: "read " + paths.fileFor(label) + ": " + err.Error()}
		}
		*path = string(raw)
	}
	return t, nil
}

// fileFor resolves the plan label to its path.
func (p newPaths) fileFor(label string) string {
	switch label {
	case "chi":
		return p.chi
	case "registry":
		return p.registry
	default:
		return p.policy
	}
}

// confirmNew asks before mutating an existing checkout.
func confirmNew(stdout io.Writer, stdin io.Reader, name string) bool {
	fmt.Fprintf(stdout, "Scaffold feature %q? [y/N]: ", name)
	var answer [8]byte
	n, _ := stdin.Read(answer[:])
	resp := strings.ToLower(strings.TrimSpace(string(answer[:n])))
	return resp == "y" || resp == "yes"
}

// printNewPlan renders the plan in text or JSON. Both shapes name every file
// written and every file edited, so a dry run is the review.
func printNewPlan(w io.Writer, opt newOptions, plan newPlan) error {
	creates := []string{
		"features/" + opt.name + "/handlers/chi.go",
		"features/" + opt.name + "/components/page.templ",
		"features/" + opt.name + "/handlers/register.go",
		"features/" + opt.name + "/handlers/recipe.go",
		"features/" + opt.name + "/handlers/genui_panel.go",
		".recipes/" + opt.name + ".yml",
		prompts.Dir + "/panels/" + opt.name + ".prompt",
	}
	if opt.format == planFormatJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"name":    opt.name,
			"dir":     opt.dir,
			"dryRun":  opt.dryRun,
			"creates": creates,
			"edits":   []string{plan.paths.chi, plan.paths.registry, plan.paths.policy},
			"proof": []string{
				"templ generate", "generated docs", "registry conformance",
				"check-flatdeps", "check-prompts", "go build ./cmd/web", "recipe check",
			},
			"next": newNextSteps(opt.name),
		})
	}
	fmt.Fprintf(w, "gogogo: new feature %q in %s\n", opt.name, opt.dir)
	fmt.Fprintf(w, "  creates %s\n", strings.Join(creates, ", "))
	fmt.Fprintf(w, "  edits   %s (import + parameter + mount)\n", plan.paths.chi)
	fmt.Fprintf(w, "  edits   %s (one feature unit)\n", plan.paths.registry)
	fmt.Fprintf(w, "  edits   %s (two user rows)\n", plan.paths.policy)
	fmt.Fprintln(w, "  proof   templ generate, generated docs, registry conformance,")
	fmt.Fprintln(w, "  proof   check-flatdeps, check-prompts, go build ./cmd/web, recipe check")
	return nil
}

// newNextSteps is what the scaffold deliberately leaves to the author.
func newNextSteps(name string) []string {
	return []string{
		"Add a sidebar entry in internal/ui/modules.go and vendor its icon in " +
			"internal/icon/icons.go (the shell has no navbar).",
		"Replace the page body in features/" + name + "/components/page.templ; run `make css` for new Tailwind classes.",
		"Read docs/unit-contracts.md for the routes/state your unit now owns.",
		"Run `gogogo recipe check " + name + " --dir .` — warnings are the work list; errors block `doctor` and CI.",
	}
}

// applyNew writes the files, applies the prepared edits, and runs the proof.
func applyNew(ctx context.Context, opt newOptions, plan newPlan, stdout io.Writer) error {
	root, err := filepath.Abs(opt.dir)
	if err != nil {
		return err
	}
	if err := guardMutable(root); err != nil {
		return err
	}
	// Refuse a toolchain that cannot build the tree BEFORE the files land, or
	// the proof fails on a half-written feature.
	if err := checkToolchainForTree(ctx, root, stdout, opt.format == planFormatJSON); err != nil {
		return err
	}
	files := map[string]string{
		plan.paths.handler: newHandlerSource(opt.name, plan.module),
		plan.paths.page:    newPageSource(opt.name, plan.module),
		plan.paths.prompt:  newPanelPromptSource(opt.name),
		"features/" + opt.name + "/handlers/register.go":    newRegisterSource(opt.name, plan.module),
		"features/" + opt.name + "/handlers/recipe.go":      newRecipeSource(opt.name, plan.module),
		"features/" + opt.name + "/handlers/genui_panel.go": newPanelSource(opt.name, plan.module),
	}
	for rel, src := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), dirMode); err != nil {
			return &ExitError{code: 1, msg: "mkdir " + rel + ": " + err.Error()}
		}
		if err := os.WriteFile(full, []byte(src), scaffoldFileMode); err != nil {
			return &ExitError{code: 1, msg: "write " + rel + ": " + err.Error()}
		}
	}
	// The receipt pins the pristine hashes of the files just written, so
	// recipe check can compare declaration vs code from the first boot.
	hashes := make(map[string]string, len(files))
	for rel, src := range files {
		hashes[rel] = recipe.Hash([]byte(src))
	}
	receiptRel := ".recipes/" + opt.name + ".yml"
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(receiptRel)),
		[]byte(newReceiptSource(opt.name, hashes)), scaffoldFileMode); err != nil {
		return &ExitError{code: 1, msg: "write " + receiptRel + ": " + err.Error()}
	}
	edits := map[string]string{plan.paths.chi: plan.chi, plan.paths.registry: plan.reg, plan.paths.policy: plan.policy}
	for rel, src := range edits {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(src), scaffoldFileMode); err != nil {
			return &ExitError{code: 1, msg: "write " + rel + ": " + err.Error()}
		}
	}
	return proveNewFeature(ctx, root, opt.name, stdout)
}

// proveNewFeature runs the gates a new unit must pass, in the order that
// reveals mistakes earliest. Failures keep the tree mutated on purpose: the
// diff is the debugging context, and `git checkout` is one command away.
func proveNewFeature(ctx context.Context, root, name string, stdout io.Writer) error {
	steps := []struct {
		label string
		name  string
		args  []string
	}{
		{"gofmt", "gofmt", []string{"-w", "internal/server/chi.go", "internal/capabilities/registry.go"}},
		{"templ generate", "go", []string{"tool", "templ", "generate"}},
	}
	for _, step := range steps {
		fmt.Fprintf(stdout, "gogogo: prove: %s …\n", step.label)
		if out, err := runIn(ctx, root, step.name, step.args...); err != nil {
			return &ExitError{code: 2, msg: fmt.Sprintf("%s failed — the scaffold is applied but broken:\n%s",
				step.label, tailLines(string(out), proofBuildTailLines))}
		}
	}
	fmt.Fprintln(stdout, "gogogo: prove: generated docs …")
	if out, err := runIn(ctx, root, "go", "test", "./internal/capabilities/",
		"-run", "Test(ApiRoutes|CapabilityContracts)DocFresh", "-update", "-count=1"); err != nil {
		return &ExitError{code: 2, msg: "generated docs regen failed:\n" + tailLines(string(out), proofBuildTailLines)}
	}
	// Explicitly regen the bundled registration so a self-mounting scaffold
	// lands in units.gen.go without a hand edit.
	if out, err := runIn(ctx, root, "go", "test", "./internal/capabilities/",
		"-run", "TestBundledUnitRegistrationIsCurrent", "-update", "-count=1"); err != nil {
		return &ExitError{code: 2, msg: "units.gen regen failed:\n" + tailLines(string(out), proofBuildTailLines)}
	}
	for _, gate := range []struct{ label, pkg string }{
		{"check-flatdeps", "./cmd/check-flatdeps"},
	} {
		fmt.Fprintf(stdout, "gogogo: prove: %s …\n", gate.label)
		if out, err := runIn(ctx, root, "go", "run", gate.pkg); err != nil {
			return &ExitError{code: 2, msg: fmt.Sprintf("%s failed:\n%s", gate.label,
				tailLines(string(out), proofBuildTailLines))}
		}
	}
	// The prompt gate runs only on a tree that carries it: an older
	// checkout still scaffolds, with the step skipped instead of failing on a
	// command it does not have.
	if _, statErr := os.Stat(filepath.Join(root, "cmd", "check-prompts")); statErr == nil {
		fmt.Fprintln(stdout, "gogogo: prove: check-prompts …")
		if out, err := runIn(ctx, root, "go", "run", "./cmd/check-prompts"); err != nil {
			return &ExitError{code: 2, msg: "check-prompts failed — the scaffolded prompt is not a file:\n" +
				tailLines(string(out), proofBuildTailLines)}
		}
	}
	fmt.Fprintln(stdout, "gogogo: prove: go build ./cmd/web …")
	tmp, err := os.CreateTemp("", "gogogo-new-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)
	if out, buildErr := runIn(ctx, root, "go", "build", "-o", tmpPath, "./cmd/web"); buildErr != nil {
		return &ExitError{code: 2, msg: "proof build failed — the scaffold is applied but broken:\n" +
			tailLines(string(out), proofBuildTailLines)}
	}
	fmt.Fprintln(stdout, "gogogo: prove: build OK")
	fmt.Fprintf(stdout, "gogogo: prove: recipe check %s …\n", name)
	if out, err := runIn(ctx, root, "go", "run", "./cmd/gogogo", "recipe", "check", name, "--dir", "."); err != nil {
		return &ExitError{code: 2, msg: "recipe check failed — the scaffold is applied but blocked:\n" +
			tailLines(string(out), proofBuildTailLines)}
	}
	return nil
}
