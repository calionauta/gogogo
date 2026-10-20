// SCOPE:plugin — installer engine: flags, plan,
// apply, prove, and exit codes shared by the CLI and the MCP server.
package installer

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/calionauta/gogogo/internal/capabilities"
)

type options struct {
	name     string
	owner    string
	plugins  string
	features string
	preset   string
	noTUI    bool
	dir      string
	yes      bool
	dryRun   bool
	check    bool
	run      bool
	updateCh bool
	format   string
}

// exitError carries a stable process exit code for agents.
type ExitError struct {
	code int
	msg  string
}

func (e *ExitError) Error() string { return e.msg }

func ExitCode(err error) int {
	if ee, ok := errors.AsType[*ExitError](err); ok {
		return ee.code
	}
	return 1
}

// Version is the release tag (v-prefixed). Set via -ldflags:
// -X github.com/calionauta/gogogo/internal/installer.Version=...
// Plain `go build` leaves the dev default.
var Version = "dev"

func unitOneLiner(u trimUnit) string {
	byID := capabilities.ByID()
	if c, ok := byID[u.caps()[0]]; ok {
		return c.Summary
	}
	return u.id
}

func Run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) > 0 && (args[0] == "--version" || args[0] == "-version") {
		fmt.Fprintln(stdout, "gogogo "+Version)
		return nil
	}
	if handled, err := runSubcommand(ctx, args, stdin, stdout); handled {
		return err
	}
	opt, err := loadOptions(args, stdin, stdout)
	if err != nil {
		return err
	}
	if opt.updateCh {
		return printUpdateCheck(ctx, stdout)
	}
	keep := parseKeep(opt.plugins, opt.features)
	drop := planTrim(keep)
	plan := buildPlan(opt.name, opt.owner, opt.dir, keep, drop, opt.dryRun)
	plan.Preset = opt.preset
	if opt.check {
		return runCheck(opt, stdout)
	}
	if opt.format == planFormatJSON {
		if opt.dryRun {
			return printPlanJSON(stdout, plan)
		}
		// Apply path: agents previewed via --dry-run; the single
		// envelope (plan + receipt + build result) prints at the end.
	} else {
		printPlanText(stdout, plan)
		if opt.dryRun {
			return nil
		}
	}
	return runApply(ctx, opt, drop, plan, stdin, stdout)
}

// runSubcommand dispatches the mutating/guiding subcommands, keeping Run's
// own complexity under the gocyclo gate. It reports whether argv named one.
func runSubcommand(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "add":
		return true, runAdd(ctx, args[1:], stdin, stdout)
	case "new":
		return true, runNew(ctx, args[1:], stdin, stdout)
	case "advise":
		return true, runAdvise(args[1:], stdout)
	}
	return false, nil
}

// loadOptions parses flags, runs the interactive form when needed, and
// validates the result. It never touches the filesystem.
func loadOptions(args []string, stdin io.Reader, stdout io.Writer) (options, error) {
	fs := flag.NewFlagSet("gogogo", flag.ContinueOnError)
	opt := options{}
	fs.StringVar(&opt.name, "name", "", "new project name (e.g. my-app)")
	fs.StringVar(&opt.owner, "owner", "calionauta", "GitHub owner/org for the module path")
	fs.StringVar(&opt.plugins, "plugins", "", "comma-separated plugins to KEEP (default: all; none drops all)")
	fs.StringVar(&opt.features, "features", "", "comma-separated features to KEEP (default: all; none drops all)")
	fs.StringVar(&opt.preset, "preset", "",
		"named drop set instead of keep lists: minimal|no-ai|no-collab (conflicts with --plugins/--features)")
	fs.BoolVar(&opt.noTUI, "no-tui", false, "non-interactive mode (requires --name)")
	fs.StringVar(&opt.dir, "dir", "", "target checkout directory (default: ./<name>)")
	fs.BoolVar(&opt.yes, "yes", false, "apply without asking (agents: always pin this)")
	fs.BoolVar(&opt.dryRun, "dry-run", false, "print the plan and stop; changes nothing")
	fs.BoolVar(&opt.check, "check", false, "verify manifest markers against --dir, change nothing (drift gate)")
	fs.BoolVar(&opt.updateCh, "check-update", false, "compare this binary against the latest release, change nothing")
	fs.BoolVar(&opt.run, "run", false, "hand over to `make dev` after a "+
		"successful proof (takes this terminal; Ctrl-C stops dev; humans only)")
	fs.StringVar(&opt.format, "format", planFormatText, "plan format: text|json")
	fs.Usage = func() { PrintUsage(stdout, fs) }
	fs.SetOutput(stdout)
	if err := fs.Parse(args); err != nil {
		return opt, err
	}
	if opt.format != planFormatText && opt.format != planFormatJSON {
		return opt, fmt.Errorf("unknown --format %q (want text|json)", opt.format)
	}
	if opt.check {
		// --check never prompts and needs no project name: it reads.
		opt.noTUI = true
		if opt.name == "" {
			opt.name = "check"
		}
	}
	if opt.updateCh {
		// --check-update never prompts either: pure network read.
		opt.noTUI = true
		if opt.name == "" {
			opt.name = "check-update"
		}
	}
	if !opt.noTUI && opt.name == "" {
		interactive := promptForm(stdin, stdout)
		opt.name = interactive.name
		opt.owner = interactive.owner
		opt.plugins = interactive.plugins
		opt.features = interactive.features
	}
	if opt.name == "" {
		fs.Usage()
		return opt, errors.New("project name is required (--name my-app or interactive mode)")
	}
	if err := validateName(opt.name); err != nil {
		return opt, err
	}
	if err := checkKeepIDs(opt.plugins, opt.features); err != nil {
		return opt, err
	}
	if err := resolvePreset(&opt); err != nil {
		return opt, err
	}
	if opt.dir == "" {
		opt.dir = opt.name
	}
	return opt, nil
}

// templateCloneURL is the only network source the installer pulls: a
// public template checkout to scaffold from when --dir is missing.
const templateCloneURL = "https://github.com/calionauta/gogogo"

// requireCheckoutDir is the strict gate for read-only paths (--check):
// nothing is created, so a missing directory is a plain error.
func requireCheckoutDir(opt options) error {
	if _, err := os.Stat(opt.dir); err != nil {
		return &ExitError{code: 1, msg: fmt.Sprintf(
			"directory %s not found — nothing to check "+
				"(scaffold it first: go run github.com/calionauta/gogogo/cmd/gogogo@latest --yes)",
			opt.dir)}
	}
	return nil
}

// ensureCheckoutDir is the apply-path gate: an existing directory is used
// as-is, a missing one is cloned from the template with explicit consent
// (--yes, or an interactive [y/N]). Cloning rides on the same consent
// model as trimming: never on an assumption.
func ensureCheckoutDir(ctx context.Context, opt options, stdin io.Reader, stdout io.Writer) error {
	if _, err := os.Stat(opt.dir); err == nil {
		return nil
	}
	if !opt.yes && !confirmClone(stdout, stdin, opt.dir) {
		return &ExitError{code: 1, msg: fmt.Sprintf(
			"directory %s not found — aborted, nothing cloned "+
				"(re-run with --yes to clone the template and scaffold)",
			opt.dir)}
	}
	if opt.format != planFormatJSON {
		fmt.Fprintf(stdout, "gogogo: cloning %s into %s …\n", templateCloneURL, opt.dir)
	}
	if err := gitClone(ctx, opt.dir); err != nil {
		return &ExitError{code: 1, msg: fmt.Sprintf(
			"git clone failed (%v) — is git installed and the network up?\n"+
				"manual fallback: git clone %s %s, then re-run with --dir %s --yes",
			err, templateCloneURL, opt.dir, opt.dir)}
	}
	return nil
}

// confirmClone asks one question with a safe default (no).
func confirmClone(stdout io.Writer, stdin io.Reader, dir string) bool {
	fmt.Fprintf(stdout, "Directory %s not found — clone the template into it and continue? [y/N]: ", dir)
	var answer [8]byte
	n, _ := stdin.Read(answer[:])
	resp := strings.ToLower(strings.TrimSpace(string(answer[:n])))
	return resp == "y" || resp == "yes"
}

// gitClone is a seam for tests (no network in unit tests).
var gitClone = func(ctx context.Context, dir string) error {
	_, err := runIn(ctx, ".", "git", "clone", "--depth", "1", templateCloneURL, dir)
	return err
}

// applyAndProve trims, renames, writes AGENTS.md, and proves the result.
func applyAndProve(ctx context.Context, opt options, drop []trimUnit, stdout io.Writer) error {
	rc := &Receipt{}
	if err := applyTrim(opt.dir, drop, rc); err != nil {
		return err
	}
	if err := renameTree(opt.dir, opt.name, opt.owner); err != nil {
		return err
	}
	if err := writeAgents(opt.dir, opt.name, drop); err != nil {
		return err
	}
	printReceiptText(stdout, rc)
	if err := prove(ctx, opt.dir, drop, stdout); err != nil {
		return err
	}
	printNextSteps(stdout, opt.dir, drop)
	if opt.run {
		return handoff(opt.dir, stdout)
	}
	return nil
}

// nextSteps is the printed + machine-readable handoff after a successful
// scaffold. Without --run the installer ends with the exact commands: it
// runs from an ephemeral `go run @latest` module and a child process
// cannot cd its parent, so `cd <dir> && make dev` is printed, not run.
// PORT is the scaffolded app's default; the binary reads it at boot.
type nextSteps struct {
	Dir   string `json:"dir"`
	Dev   string `json:"dev"`
	App   string `json:"app"`
	Todo  string `json:"todo"`
	Login string `json:"login"`
	Admin string `json:"admin"`
	Flows string `json:"workflows"`
}

// applyAndProveJSON is the machine-readable apply path: one envelope at
// the end carrying plan + receipt + proof result.
func applyAndProveJSON(ctx context.Context, opt options, drop []trimUnit, stdout io.Writer, plan scaffoldPlan) error {
	rc := &Receipt{Units: []UnitReceipt{}}
	if err := applyTrim(opt.dir, drop, rc); err != nil {
		return err
	}
	if err := renameTree(opt.dir, opt.name, opt.owner); err != nil {
		return err
	}
	if err := writeAgents(opt.dir, opt.name, drop); err != nil {
		return err
	}
	// Silence prove chatter in JSON mode: discard its progress lines,
	// keep only the outcome for the envelope.
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer func() { _ = devNull.Close() }()
	proveErr := prove(ctx, opt.dir, drop, devNull)
	env := envelope{Plan: plan, Receipt: *rc, BuildOk: proveErr == nil, Next: buildNextSteps(opt.dir, drop)}
	if proveErr != nil {
		env.BuildErr = proveErr.Error()
	}
	if err := printEnvelopeJSON(stdout, env); err != nil {
		return err
	}
	if proveErr != nil {
		return proveErr
	}
	if opt.run {
		return handoff(opt.dir, stdout)
	}
	return nil
}

func confirm(stdout io.Writer, stdin io.Reader, nDrop int) bool {
	if nDrop == 0 {
		fmt.Fprint(stdout, "Proceed with rename only? [y/N]: ")
	} else {
		fmt.Fprintf(stdout, "Delete %d unit(s) above and rename? [y/N]: ", nDrop)
	}
	var answer [8]byte
	n, _ := stdin.Read(answer[:])
	resp := strings.ToLower(strings.TrimSpace(string(answer[:n])))
	return resp == "y" || resp == "yes"
}

// plan output format names.
const (
	planFormatText = "text"
	planFormatJSON = "json"
)

// prove output tails kept short for humans; full logs stay in the checkout.
const (
	proofTemplTailLines = 10
	proofTidyTailLines  = 20
	proofBuildTailLines = 30
)

// prove runs templ generate (only when .templ files were edited in place),
// go mod tidy, and the build. A failing proof exits 2 with compiler output.
func prove(ctx context.Context, dir string, drop []trimUnit, stdout io.Writer) error {
	if needsTemplGen(drop) {
		fmt.Fprintln(stdout, "gogogo: prove: go tool templ generate …")
		if out, err := runIn(ctx, dir, "go", "tool", "templ", "generate"); err != nil {
			fmt.Fprintf(stdout,
				"gogogo: WARN: templ generate failed (%v) — run `make templ` manually:\n%s\n",
				err, tailLines(string(out), proofTemplTailLines))
		}
	}
	fmt.Fprintln(stdout, "gogogo: prove: go mod tidy …")
	if out, err := runIn(ctx, dir, "go", "mod", "tidy"); err != nil {
		return &ExitError{code: 2, msg: fmt.Sprintf(
			"go mod tidy failed (%v):\n%s", err, tailLines(string(out), proofTidyTailLines))}
	}
	// Regenerate the generated docs (route inventory + unit contract cards)
	// so the tree is green immediately.
	fmt.Fprintln(stdout, "gogogo: prove: generated docs …")
	// Warn-only here (failure is a stale doc, not a broken tree); the gate
	// below treats the same error as fatal.
	_ = proveAPIRoutesDoc(ctx, dir, stdout)
	fmt.Fprintln(stdout, "gogogo: prove: go build ./cmd/web …")
	tmp, err := os.CreateTemp("", "gogogo-proof-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)
	if out, err := runIn(ctx, dir, "go", "build", "-o", tmpPath, "./cmd/web"); err != nil {
		return &ExitError{code: 2, msg: fmt.Sprintf(
			"proof build failed (%v) — the trim is applied but broken:\n%s",
			err, tailLines(string(out), proofBuildTailLines))}
	}
	fmt.Fprintln(stdout, "gogogo: prove: build OK")
	return nil
}

func runIn(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// proveAPIRoutesDoc regenerates the generated docs in the target tree: the
// route inventory and the unit contract cards. Both are pinned by tests, so a
// trim that deletes a unit without regenerating them leaves CI red.
// It returns the regen error (callers decide warn-only vs fatal) after
// printing the WARN with the manual regen command, so no failure mode is
// silent. Shared with the preset composition gate, which proves it on
// copies.
func proveAPIRoutesDoc(ctx context.Context, dir string, stdout io.Writer) error {
	if out, err := runIn(ctx, dir, "go", "test", "./internal/capabilities/",
		"-run", "Test(ApiRoutes|CapabilityContracts)DocFresh", "-update", "-count=1"); err != nil {
		fmt.Fprintf(stdout,
			"gogogo: WARN: generated docs regen failed (%v) — run "+
				"`go test ./internal/capabilities/ -run 'Test(ApiRoutes|CapabilityContracts)DocFresh' -update` manually:\n%s\n",
			err, tailLines(string(out), proofTidyTailLines))
		return err
	}
	return nil
}

func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// UnitView is one installer unit for display (matrix, menus, docs).
type UnitView struct {
	ID      string
	Kind    string
	Summary string
}

// Units lists installer units in manifest order.
func Units() []UnitView {
	out := make([]UnitView, 0, len(manifestUnits))
	for _, u := range manifestUnits {
		m := u.meta()
		summary := u.id
		if s := unitOneLiner(u); s != u.id {
			summary = s
		}
		out = append(out, UnitView{ID: u.id, Kind: string(m.kind), Summary: summary})
	}
	return out
}
