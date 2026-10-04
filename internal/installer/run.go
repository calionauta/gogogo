// SCOPE:layer=infra,removal=plugin — installer engine: flags, plan,
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
	noTUI    bool
	dir      string
	yes      bool
	dryRun   bool
	check    bool
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

func unitOneLiner(u trimUnit) string {
	byID := capabilities.ByID()
	if c, ok := byID[u.caps()[0]]; ok {
		return c.Summary
	}
	return u.id
}

func Run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if handled, err := runSubcommand(ctx, args, stdin, stdout); handled {
		return err
	}
	opt, _, err := loadOptions(args, stdin, stdout)
	if err != nil {
		return err
	}
	keep := parseKeep(opt.plugins, opt.features)
	drop := planTrim(keep)
	plan := buildPlan(opt.name, opt.owner, opt.dir, keep, drop, opt.dryRun)
	if opt.check {
		if err := requireCheckoutDir(opt); err != nil {
			return err
		}
		if failed := printCheck(stdout, checkTree(opt.dir)); failed > 0 {
			return &ExitError{code: 1, msg: fmt.Sprintf(
				"%d unit(s) cannot apply cleanly here — manifest drift "+
					"or already-trimmed tree (see CHECK-FAIL lines above)", failed)}
		}
		fmt.Fprintln(stdout, "gogogo: check OK — every unit applies cleanly here")
		return nil
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
	if err := requireCheckoutDir(opt); err != nil {
		return err
	}
	if !opt.yes && !confirm(stdout, stdin, len(drop)) {
		fmt.Fprintln(stdout, "gogogo: aborted — nothing changed (re-run with --yes to skip this prompt)")
		return nil
	}
	if opt.format == planFormatJSON {
		return applyAndProveJSON(ctx, opt, drop, stdout, plan)
	}
	return applyAndProve(ctx, opt, drop, stdout)
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
	case "advise":
		return true, runAdvise(args[1:], stdout)
	}
	return false, nil
}

// loadOptions parses flags, runs the interactive form when needed, and
// validates the result. It never touches the filesystem.
func loadOptions(args []string, stdin io.Reader, stdout io.Writer) (options, *flag.FlagSet, error) {
	fs := flag.NewFlagSet("gogogo", flag.ContinueOnError)
	opt := options{}
	fs.StringVar(&opt.name, "name", "", "new project name (e.g. my-app)")
	fs.StringVar(&opt.owner, "owner", "calionauta", "GitHub owner/org for the module path")
	fs.StringVar(&opt.plugins, "plugins", "", "comma-separated plugins to KEEP (default: all; none drops all)")
	fs.StringVar(&opt.features, "features", "", "comma-separated features to KEEP (default: all; none drops all)")
	fs.BoolVar(&opt.noTUI, "no-tui", false, "non-interactive mode (requires --name)")
	fs.StringVar(&opt.dir, "dir", "", "target checkout directory (default: ./<name>)")
	fs.BoolVar(&opt.yes, "yes", false, "apply without asking (agents: always pin this)")
	fs.BoolVar(&opt.dryRun, "dry-run", false, "print the plan and stop; changes nothing")
	fs.BoolVar(&opt.check, "check", false, "verify manifest markers against --dir, change nothing (drift gate)")
	fs.StringVar(&opt.format, "format", planFormatText, "plan format: text|json")
	fs.Usage = func() { PrintUsage(stdout, fs) }
	fs.SetOutput(stdout)
	if err := fs.Parse(args); err != nil {
		return opt, fs, err
	}
	if opt.format != planFormatText && opt.format != planFormatJSON {
		return opt, fs, fmt.Errorf("unknown --format %q (want text|json)", opt.format)
	}
	if opt.check {
		// --check never prompts and needs no project name: it reads.
		opt.noTUI = true
		if opt.name == "" {
			opt.name = "check"
		}
	}
	if !opt.noTUI && opt.name == "" {
		interactive, err := promptForm(stdin, stdout)
		if err != nil {
			return opt, fs, err
		}
		opt.name = interactive.name
		opt.owner = interactive.owner
		opt.plugins = interactive.plugins
		opt.features = interactive.features
	}
	if opt.name == "" {
		fs.Usage()
		return opt, fs, fmt.Errorf("project name is required (--name my-app or interactive mode)")
	}
	if err := validateName(opt.name); err != nil {
		return opt, fs, err
	}
	if err := checkKeepIDs(opt.plugins, opt.features); err != nil {
		return opt, fs, err
	}
	if opt.dir == "" {
		opt.dir = opt.name
	}
	return opt, fs, nil
}

// requireCheckoutDir stops early when v0.1 has nothing to operate on: the
// installer edits an existing checkout, it does not clone one.
func requireCheckoutDir(opt options) error {
	if _, err := os.Stat(opt.dir); err != nil {
		return &ExitError{code: 1, msg: fmt.Sprintf(
			"directory %s not found — clone the template first:\n"+
				"  gh repo create %s --template calionauta/gogogo --clone\n"+
				"  (or: git clone <url> %s, then re-run with --dir %s --yes)",
			opt.dir, opt.name, opt.dir, opt.dir)}
	}
	return nil
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
	if err := writeAgents(opt.dir, opt.name); err != nil {
		return err
	}
	printReceiptText(stdout, rc)
	if err := prove(ctx, opt.dir, drop, stdout); err != nil {
		return err
	}
	printNextSteps(stdout, opt.dir)
	return nil
}

// nextSteps is the printed + machine-readable handoff after a successful
// scaffold. The installer runs from an ephemeral `go run @latest` module,
// so it cannot own the new project's dev loop (Air, ports, browser) —
// instead it ends with the exact commands. PORT is the scaffolded app's
// default; the binary reads it at boot.
type nextSteps struct {
	Dir   string `json:"dir"`
	Dev   string `json:"dev"`
	App   string `json:"app"`
	Todo  string `json:"todo"`
	Login string `json:"login"`
	Admin string `json:"admin"`
	Flows string `json:"workflows"`
}

func buildNextSteps(dir string) nextSteps {
	return nextSteps{
		Dir:   dir,
		Dev:   "cd " + dir + " && make dev",
		App:   "http://localhost:8080 (PORT overrides)",
		Todo:  "http://localhost:8080/todo",
		Login: "demo@demo.app / demo1234456 (prefilled on the sign-in form)",
		Admin: "http://localhost:8080/_/ (PocketBase — create the superuser on first visit)",
		Flows: "http://localhost:8080/dagnats/ (DagNats console)",
	}
}

func printNextSteps(w io.Writer, dir string) {
	n := buildNextSteps(dir)
	fmt.Fprintln(w, "gogogo: done — next:")
	fmt.Fprintf(w, "  %s\n", n.Dev)
	fmt.Fprintf(w, "  app:       %s\n", n.App)
	fmt.Fprintf(w, "  login:     %s\n", n.Login)
	fmt.Fprintf(w, "  admin:     %s\n", n.Admin)
	fmt.Fprintf(w, "  workflows: %s\n", n.Flows)
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
	if err := writeAgents(opt.dir, opt.name); err != nil {
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
	env := envelope{Plan: plan, Receipt: *rc, BuildOk: proveErr == nil, Next: buildNextSteps(opt.dir)}
	if proveErr != nil {
		env.BuildErr = proveErr.Error()
	}
	if err := printEnvelopeJSON(stdout, env); err != nil {
		return err
	}
	return proveErr
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
	var out []UnitView
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

func PrintUsage(w io.Writer, fs *flag.FlagSet) {
	fmt.Fprintln(w, `gogogo — scaffold a project from gogogo.

  Interactive (humans: 4 questions, then a plan to confirm):

    go run github.com/calionauta/gogogo/cmd/gogogo@latest

  Scripted (agents: always pin --yes; preview with --dry-run first):

    go run ./cmd/gogogo --name my-app --owner myorg \
      --plugins dagnats,whiteboard --features landing,config-view \
      --no-tui --dry-run --format json --dir ./my-app
    go run ./cmd/gogogo --name my-app --no-tui --yes --dir ./my-app

  Drift gate (maintainers/CI): verify every marker against a pristine
  checkout without changing anything:

    go run ./cmd/gogogo --check --dir ./my-app

  Add a unit to an existing scaffolded checkout (deps cascade automatically):

    go run ./cmd/gogogo add whiteboard --from ~/gogogo --dir ./my-app --yes

  Opinions, not changes (for LLMs deciding what to use — reads nothing, changes nothing):

    go run ./cmd/gogogo advise --need "offline-first todo with AI" --format json

  What it does, in order:
    1. shows the trim plan with every consequence (never silent),
    2. deletes skipped plugins/features with their wiring calls,
    3. renames the module path + every reference (rename-project.py rules),
    4. writes AGENTS.md with the upstream-first rule,
    5. proves it: templ generate (when .templ edited) + go mod tidy +
       go build ./cmd/web.

  Units (id, kind). Kind follows the servant principle, not the directory:
  a plugin serves other capabilities (sounds, skins, credits serve pages);
  a feature is a terminal user surface (todo, whiteboard, landing, config).
  --plugins matches plugin-kind units, --features matches feature-kind units. Not offered here —
  todo (reference implementation), auth (middleware is core), queue (core),
  nats (runtime: NATS_ENABLED=false), llm (runtime: unset GOAI_API_KEY),
  offline-sync (runtime: OFFLINE_SYNC_ENABLED=false),
  entity-store (runtime: ENTITY_STORE=pb|crdt).`)
	fmt.Fprintln(w, "\n  Trim units:")
	for _, u := range Units() {
		fmt.Fprintf(w, "    %-12s %-7s %s\n", u.ID, u.Kind, u.Summary)
	}
	fmt.Fprintln(w, `
  Flags:`)
	fs.SetOutput(w)
	fs.PrintDefaults()
	fmt.Fprintln(w, `
  Agent contract: --format json emits stable field names.
  --dry-run prints the plan; apply prints one envelope
  {plan, receipt{units[{id, dirsRemoved, filesRemoved,
  stripsApplied, stripsMissed}]}, buildOk, buildError,
  next{dir, dev, app, todo, login, admin, workflows}}.
  Exit codes: 0 ok/plan-only, 1 usage or apply error, 2 proof build failed.`)
}
