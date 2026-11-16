// Installer engine: flags, plan,
// apply, prove, and exit codes shared by the CLI and the MCP server.
package installer

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/calionauta/gogogo/internal/capabilities"
	shapekit "github.com/calionauta/gogogo/internal/shape"
)

type options struct {
	name     string
	owner    string
	noTUI    bool
	dir      string
	yes      bool
	dryRun   bool
	run      bool
	updateCh bool
	// dirGiven distinguishes "--dir was passed" from the project-name default,
	// which read-only paths (--check-update) must not mistake for a target.
	dirGiven bool
	format   string
	// recipes selects which demo slices the scaffold drops: "none" (the
	// default), "all", or a comma list of ids to drop.
	recipes string
	// locales is the language set the product ships. Empty means "keep the
	// record the template already carries" (or write nothing when there is
	// none): the scaffold never invents a language decision.
	locales string

	// shape selects which entrypoints the scaffolded tree ships. Empty is
	// `full` (every target), which is what a template clone already is.
	shape string
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

// ErrorText renders an installer error as the CLI's one-line stderr message
// with the program prefix applied exactly once. Many ExitError literals are
// written for humans to read verbatim and already start with "gogogo:", so the
// entrypoint must not add a second one.
//
// An empty message returns empty, not "gogogo: ". A verdict-only exit
// (--check-update's code 3) prints its answer on stdout and needs no stderr
// line at all, and a bare prefix there reads as a second failure.
func ErrorText(err error) string {
	msg := err.Error()
	if msg == "" {
		return ""
	}
	if strings.HasPrefix(msg, "gogogo:") {
		return msg
	}
	return "gogogo: " + msg
}

// Version is the release version, without a leading "v": goreleaser's
// {{.Version}} template strips the tag prefix, so a v-prefixed release becomes
// "0.53.0". Set via -ldflags:
// -X github.com/calionauta/gogogo/internal/installer.Version=...
// Plain `go build` leaves the dev default. Comparison strips a "v" anyway, so
// either spelling orders correctly; this comment exists so nobody writes code
// that assumes the prefix.
var Version = "dev"

func unitOneLiner(u builtinUnit) string {
	byID := capabilities.ByID()
	if c, ok := byID[u.caps()[0]]; ok {
		return c.Summary
	}
	return u.id
}

func Run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	return RunTo(ctx, args, stdin, stdout, os.Stderr)
}

// RunTo is Run with an explicit stderr. The MCP server captures the
// transcript into a buffer, and a caller that captures stdout but not stderr
// loses the half that matters on failure: `go test` prints a compile error to
// stderr, so a recipe whose test does not build would answer with an empty
// failure. One writer per stream, decided by the caller.
func RunTo(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) > 0 && (args[0] == "--version" || args[0] == "-version") {
		fmt.Fprintln(stdout, "gogogo "+Version)
		return nil
	}
	if handled, err := runSubcommand(ctx, args, stdin, stdout, stderr); handled {
		return err
	}
	opt, err := loadOptions(args, stdin, stdout)
	if err != nil {
		return err
	}
	if opt.updateCh {
		// A dir the caller did not name must not be invented: loadOptions
		// defaults --dir to the project name, which is meaningless here.
		dir := ""
		if opt.dirGiven {
			dir = opt.dir
		}
		return printUpdateCheck(ctx, stdout, opt.format, dir)
	}
	plan := buildPlan(opt.name, opt.owner, opt.dir, opt.shape, opt.dryRun)
	if serr := applyRecipeSelection(&plan, opt); serr != nil {
		return serr
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
	return runApply(ctx, opt, plan, stdin, stdout)
}

// runSubcommand dispatches the mutating/guiding subcommands, keeping Run's
// own complexity under the gocyclo gate. It reports whether argv named one.
func runSubcommand(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "new":
		return true, runNew(ctx, args[1:], stdin, stdout)
	case "advise":
		return true, runAdvise(args[1:], stdout)
	case "migrate":
		return true, runMigrate(ctx, args[1:], stdout)
	case "export":
		return true, runExport(args[1:], stdout)
	case "recipe":
		return true, runRecipe(ctx, args[1:], stdout, stderr)
	case "doctor":
		return true, runDoctor(args[1:], stdout)
	case "generate":
		return true, runGenerate(ctx, args[1:], stdout)
	case "shape":
		return true, runShape(args[1:], stdout)
	case "locales":
		return true, runLocales(args[1:], stdout)
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
	fs.BoolVar(&opt.noTUI, "no-tui", false, "non-interactive mode (requires --name)")
	fs.StringVar(&opt.dir, "dir", "", "target checkout directory (default: ./<name>)")
	fs.BoolVar(&opt.yes, "yes", false, "apply without asking (agents: always pin this)")
	fs.BoolVar(&opt.dryRun, "dry-run", false, "print the plan and stop; changes nothing")
	fs.BoolVar(&opt.updateCh, "check-update", false, "compare this binary against the latest release, change nothing")
	// No backticks in a flag usage: `flag` reads them as the value PLACEHOLDER,
	// so "hand over to `make dev`" printed the flag as "run make dev".
	fs.BoolVar(&opt.run, "run", false, "hand over to make dev after a "+
		"successful proof (takes this terminal; Ctrl-C stops dev; humans only)")
	fs.StringVar(&opt.format, "format", planFormatText, "plan format: text|json")
	fs.StringVar(&opt.locales, "locales", "",
		"languages to ship, comma-separated (e.g. en,pt-BR); records locales.yml and seeds every new catalog")
	fs.StringVar(&opt.recipes, "recipes", recipesNone,
		"demo slices to drop: none|all|<ids> (default none; read one with: recipe source <id>)")
	fs.StringVar(&opt.shape, "shape", string(shapekit.Full),
		"product shape: full|web|api|desktop|gui|mobile (which entrypoints the tree ships)")
	fs.Usage = func() { PrintUsage(stdout, fs) }
	fs.SetOutput(stdout)
	if err := fs.Parse(args); err != nil {
		return opt, err
	}
	opt.dirGiven = flagGiven(fs, "dir")
	if err := validateOptions(&opt, fs, stdin, stdout); err != nil {
		return opt, err
	}
	return opt, nil
}

// validateOptions is the half of loadOptions that decides rather than parse:
// check the enum flags, then fill the identity from the form when the operator
// asked for it. Kept apart so the flag block reads as a flag block.
func validateOptions(opt *options, fs *flag.FlagSet, stdin io.Reader, stdout io.Writer) error {
	if !shapekit.Valid(shapekit.Shape(opt.shape)) {
		return fmt.Errorf("unknown --shape %q (want %s)", opt.shape, shapeNames())
	}
	if opt.format != planFormatText && opt.format != planFormatJSON {
		return fmt.Errorf("unknown --format %q (want text|json)", opt.format)
	}
	if opt.updateCh {
		// --check-update never prompts either: pure network read.
		opt.noTUI = true
		if opt.name == "" {
			opt.name = "check-update"
		}
	}
	if !opt.noTUI && opt.name == "" {
		fillFromForm(opt, fs, stdin, stdout)
	}
	if opt.name == "" {
		fs.Usage()
		return errors.New("project name is required (--name my-app or interactive mode)")
	}
	if err := validateName(opt.name); err != nil {
		return err
	}
	if opt.dir == "" {
		opt.dir = opt.name
	}
	return nil
}

// fillFromForm asks the identity and shape questions and applies the answers. A flag the
// operator typed wins over the form's default; the form fills only what was not
// named. That keeps `--shape api` meaning "api" even though the form also asked.
func fillFromForm(opt *options, fs *flag.FlagSet, stdin io.Reader, stdout io.Writer) {
	interactive := promptForm(stdin, stdout)
	opt.name = interactive.name
	opt.owner = interactive.owner
	if !flagGiven(fs, "shape") {
		opt.shape = interactive.shape
	}
	if !flagGiven(fs, "recipes") {
		opt.recipes = interactive.recipes
	}
	if !flagGiven(fs, "locales") {
		opt.locales = interactive.locales
	}
}

// The template's identity, in one place: the clone URL scaffolding pulls from
// when --dir is missing, and the "owner/repo" written into every scaffolded
// AGENTS.md as its base marker. Derived from the same pair so a fork that
// changes one cannot leave the other pointing at upstream.
const (
	templateOwner = "calionauta"
	templateRepo  = "gogogo"
)

const templateCloneURL = "https://github.com/" + templateOwner + "/" + templateRepo

// binName is what an operator types for this tool: the tree's own name. Advice
// that hardcoded "gogogo" sent a fork's reader to a command that does not
// exist. The rename rewrites templateRepo, so this follows the tree.
const binName = templateRepo

// ensureCheckoutDir is the apply-path gate: an existing directory is used
// as-is, a missing one is cloned from the template with explicit consent
// (--yes, or an interactive [y/N]). Cloning rides on the same consent
// model as removal: never on an assumption.
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

// runApply is the mutating tail: preflight, clone-if-missing, confirm,
// then rename and prove. Separated so Run stays under the gocyclo gate.
// (The apply-then-remove body lived in apply.go until the removal retired; the
// function keeps its name so the call order in Run reads the same.)
func runApply(ctx context.Context, opt options, plan scaffoldPlan, stdin io.Reader, stdout io.Writer) error {
	if err := guardMutable(opt.dir); err != nil {
		return err
	}
	if err := preflight(ctx, stdout, true, true, opt.format == planFormatJSON); err != nil {
		return err
	}
	if opt.run {
		if err := preflightRun(stdout); err != nil {
			return err
		}
	}
	if err := ensureCheckoutDir(ctx, opt, stdin, stdout); err != nil {
		return err
	}
	if err := checkToolchainForTree(ctx, opt.dir, stdout, opt.format == planFormatJSON); err != nil {
		return err
	}
	// The selector was previewed against the directory Run saw; now that the
	// checkout exists (a clone may have just landed), resolve it again. The
	// import gate is the difference between a product and a tree that does not
	// build, so it runs on the tree that will be edited.
	if serr := applyRecipeSelection(&plan, opt); serr != nil {
		return serr
	}
	if !opt.yes && !confirm(stdout, stdin, 0) {
		fmt.Fprintln(stdout, "gogogo: aborted — nothing changed (re-run with --yes to skip this prompt)")
		return nil
	}
	if opt.format == planFormatJSON {
		return applyAndProveJSON(ctx, opt, stdout, plan)
	}
	return applyAndProve(ctx, opt, plan, stdout)
}
