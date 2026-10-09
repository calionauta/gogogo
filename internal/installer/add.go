// SCOPE:layer=infra,removal=plugin — installer engine: add units to existing checkouts (copy, rewire, prove)
package installer

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/calionauta/gogogo/internal/capabilities"
)

// Shared file paths for add/trim mechanics (goconst-quiet, single spelling).
const (
	todoGoFile   = "features/todo/handlers/todo.go"
	todoRepoFile = "features/todo/handlers/todo_repo.go"
	dirMode      = 0o755
)

// Insertion anchors for add: where extracted spans and call lines land in
// the target checkout. Keyed by file path (convention over configuration):
// scaffolded trees keep these shapes, anything else fails fast.
const (
	// routerCallsAnchor ends the OnServe closure; capability calls land
	// directly above it, in manifest order.
	routerCallsAnchor = "\t\treturn se.Next()"
	// mainCallsAnchor is the shutdown defer; dagnats boot lines land below.
	mainCallsAnchor = "\tdefer shutdown()"
	// mainBlankUse is removed when dagnats comes back (todoH used again).
	mainBlankUse = "\t_ = todoH // dagnats removed: handler stays wired via router"
	// desktopBlockAnchor opens the Wails boot section; the Phase C demo
	// block lands above it.
	desktopBlockAnchor = "\taddr := fmt.Sprintf(\"%s:%d\", cfg.Host, cfg.Port)"
	// navLinkAnchorSubstr marks the Todo nav link; section links land below.
	navLinkAnchorSubstr = "}>Collab Todo</a>"
	// todoLayoutAnchor is the DaisyUI fallback; skin dispatches land above.
	todoLayoutAnchor = "\treturn components.Layout("
	// todoRegionAnchor is the region fallback; skin cases land above.
	todoRegionAnchor = "\tdefault:"
)

// AddReceipt records what one add run actually changed. Touched lists
// every file written, so agents (and review) can scope the diff.
type AddReceipt struct {
	ID            string   `json:"id"`
	DirsCopied    int      `json:"dirsCopied"`
	FilesCopied   int      `json:"filesCopied"`
	LinesInserted int      `json:"linesInserted"`
	Warnings      []string `json:"warnings"`
	Touched       []string `json:"touchedFiles"`
}

// addUnit copies one registry unit from a pristine template checkout into
// an existing project and rewires it, proving with tidy+build. It is the
// inverse of trim: spans are extracted from the source tree with the same
// markers trim strips by, then inserted at the anchor table above.
// .templ call sites and the navbar brand stay manual (warned): their
// positions vary per layout and a wrong guess is worse than guidance.
func addUnit(from, root string, u trimUnit, rc *AddReceipt) error {
	m := u.meta()
	if err := requireScaffold(root); err != nil {
		return err
	}
	t, err := openTree(root)
	if err != nil {
		return err
	}
	defer t.Close()
	if err := copyUnitPaths(t, from, root, m, rc); err != nil {
		return err
	}
	if err := addRouterCalls(t, root, u, rc); err != nil {
		return err
	}
	if err := addSpans(t, from, root, u, rc); err != nil {
		return err
	}
	if err := addImports(t, from, root, u, rc); err != nil {
		return err
	}
	if u.id == unitDagnats {
		mp := filepath.Join(root, "cmd", "web", "main.go")
		if err := dropLineContaining(t, mp, mainBlankUse); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := rebaseModulePrefix(t, from, root, u, rc); err != nil {
		return err
	}
	rc.Warnings = append(rc.Warnings, addFollowUps(u)...)
	return nil
}

// copyUnitPaths copies missing owned dirs/files from the template tree.
// Present paths are skipped (idempotent re-add).
//
// Writes go through treeFS (copyInto) so a symlink planted inside the target
// tree cannot redirect a write outside it. joinRoot still runs first as
// lexical defense-in-depth.
func copyUnitPaths(t *treeFS, from, root string, m unitMeta, rc *AddReceipt) error {
	for _, d := range m.dirs {
		dst, err := joinRoot(root, d)
		if err != nil {
			return err
		}
		if _, err := t.Lstat(dst); err == nil {
			continue
		}
		if err := t.copyInto(filepath.Join(from, filepath.FromSlash(d)), dst); err != nil {
			return fmt.Errorf("copy %s: %w", d, err)
		}
		rc.DirsCopied++
	}
	for _, f := range m.files {
		dst, err := joinRoot(root, f)
		if err != nil {
			return err
		}
		if _, err := t.Lstat(dst); err == nil {
			continue
		}
		if err := t.copyInto(filepath.Join(from, filepath.FromSlash(f)), dst); err != nil {
			return fmt.Errorf("copy %s: %w", f, err)
		}
		rc.FilesCopied++
	}
	return nil
}

// requireScaffold fails fast on trees the installer cannot rewire:
// without the flat Init list, insertion anchors mean nothing.
func requireScaffold(root string) error {
	for _, f := range []string{
		filepath.Join("router", "router.go"),
		"go.mod",
	} {
		p, err := joinRoot(root, f)
		if err != nil {
			return &ExitError{code: 1, msg: err.Error()}
		}
		if _, err := os.Stat(p); err != nil {
			return &ExitError{code: 1, msg: "not a gogogo-scaffolded checkout (missing " + f + ")"}
		}
	}
	return nil
}

// touch records a written file for the rebase pass and the receipt.
func (rc *AddReceipt) touch(path string) {
	if slices.Contains(rc.Touched, path) {
		return
	}
	rc.Touched = append(rc.Touched, path)
}

// addSpans extracts each strip span from the source tree and inserts it at
// its anchor. Anchors resolve per rule (addBefore/addAfter win) with path
// defaults: main.go after the shutdown defer, desktop before the Wails boot
// section, .templ/handler files from templSpanAnchor.
func addSpans(t *treeFS, from, root string, u trimUnit, rc *AddReceipt) error {
	type item struct {
		path   string
		rule   stripRule
		anchor spanAnchor
	}
	pathDefault := func(path string) spanAnchor {
		switch path {
		case filepath.Join("cmd", "web", "main.go"):
			return anchorAfter(mainCallsAnchor)
		case filepath.Join("cmd", "desktop", "main.go"):
			return anchorBefore(desktopBlockAnchor)
		default:
			if anchor, ok := templSpanAnchor(path); ok {
				return anchor
			}
			return spanAnchor{}
		}
	}
	var items []item
	collect := func(path string, rules []stripRule) {
		def := pathDefault(path)
		for _, r := range rules {
			anchor := def
			if r.addBefore != "" || r.addAfter != "" {
				anchor = spanAnchor{before: r.addBefore, after: r.addAfter}
			}
			items = append(items, item{path: path, rule: r, anchor: anchor})
		}
	}
	collect(filepath.Join("cmd", "web", "main.go"), u.mainStrips)
	collect(filepath.Join("cmd", "desktop", "main.go"), u.desktopStrips)
	for _, es := range u.extraStrips {
		collect(es.path, es.rules)
	}
	for _, it := range items {
		if it.anchor == (spanAnchor{}) {
			return fmt.Errorf("%s: no insertion anchor (add convention covers listed paths only)", it.path)
		}
		dst := filepath.Join(root, filepath.FromSlash(it.path))
		if _, err := t.Lstat(dst); os.IsNotExist(err) {
			// Sibling trim deleted the target (e.g. sounds call sites
			// in a removed layout): nothing to restore, not an error.
			continue
		}
		n, err := insertRuleSpan(t, from, root, it.path, it.rule, it.anchor, rc)
		if err != nil {
			return err
		}
		rc.LinesInserted += n
	}
	return nil
}

// addFollowUps lists the manual steps add cannot do: .templ call sites
// (positions vary per layout) and the navbar brand (user taste).
func addFollowUps(u trimUnit) []string {
	var out []string
	switch u.id {
	case unitSounds:
		out = append(out,
			"sounds has no UI until call sites return: re-add "+
				"@sounds.SoundAssets() to page layouts and "+
				"@sounds.SoundToggle() to the navbar, then `make templ` "+
				"(checklist in features/sounds/sounds.go).")
	case unitLanding:
		out = append(out,
			"navbar brand still points at /todo: point it back at / "+
				"if the landing page is the front door again.")
	}
	return out
}

// addEnvelope is the single machine-readable document for add runs.
type addEnvelope struct {
	Unit     string       `json:"unit"`
	From     string       `json:"from"`
	Dir      string       `json:"dir"`
	Receipts []AddReceipt `json:"receipts"`
	BuildOk  bool         `json:"buildOk"`
}

// addClosure orders unit + its transitive deps (deps first). Soft edges
// like landing→sounds exist because copied .templ files import the
// package; trim handles the reverse with strips, add needs the package
// present. Units without an installer entry fail fast.
func addClosure(unitID string) ([]trimUnit, error) {
	byUnit := map[string]trimUnit{}
	for _, u := range manifestUnits {
		byUnit[u.id] = u
	}
	byCap := capabilities.ByID()
	unitOf := map[string]string{}
	for _, u := range manifestUnits {
		for _, c := range u.caps() {
			unitOf[c] = u.id
		}
	}
	var ordered []trimUnit
	visited := map[string]bool{}
	var visit func(id string) error
	visit = func(id string) error {
		if visited[id] {
			return nil
		}
		visited[id] = true
		u, ok := byUnit[id]
		if !ok {
			return fmt.Errorf("unknown unit %q (valid: %s)", id, strings.Join(dropIDs(manifestUnits), ", "))
		}
		for _, dep := range u.caps() {
			c := byCap[dep]
			for _, d := range c.DependsOn {
				du, ok := unitOf[d]
				if !ok {
					return fmt.Errorf("capability %q needs %q, which no installer unit provides", dep, d)
				}
				if err := visit(du); err != nil {
					return err
				}
			}
		}
		ordered = append(ordered, u)
		return nil
	}
	if err := visit(unitID); err != nil {
		return nil, err
	}
	return ordered, nil
}

// addOptions are the parsed `gogogo add` flags plus resolved units.
type addOptions struct {
	units  []trimUnit
	unitID string
	from   string
	dir    string
	yes    bool
	dryRun bool
	format string
}

// runAdd implements `gogogo add <unit> --from TEMPLATE --dir PROJECT`:
// the inverse of trim for existing checkouts (scaffolded or evolved).
func runAdd(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	opt, err := parseAddArgs(args, stdout)
	if err != nil {
		return err
	}
	if opt.format == planFormatJSON {
		// Single JSON document per invocation: plan on dry-run,
		// envelope on apply.
		if opt.dryRun {
			for _, u := range opt.units {
				printAddPlan(stdout, opt.format, u, opt.from, opt.dir)
			}
			return nil
		}
	} else {
		for _, u := range opt.units {
			printAddPlan(stdout, opt.format, u, opt.from, opt.dir)
		}
		if opt.dryRun {
			return nil
		}
	}
	return executeAdd(ctx, opt, stdin, stdout)
}

// executeAdd applies units, proves, and reports (text plan already shown,
// JSON envelope here).
func executeAdd(ctx context.Context, opt addOptions, stdin io.Reader, stdout io.Writer) error {
	if err := guardMutable(opt.dir, opt.from); err != nil {
		return err
	}
	if err := preflight(ctx, stdout, false, true, opt.format == planFormatJSON); err != nil {
		return err
	}
	if err := requireScaffold(opt.dir); err != nil {
		return err
	}
	if !opt.yes && !confirmAdd(stdout, stdin, opt.unitID) {
		fmt.Fprintln(stdout, "gogogo: aborted — nothing changed (re-run with --yes to skip this prompt)")
		return nil
	}
	var receipts []AddReceipt
	for _, u := range opt.units {
		rc := AddReceipt{ID: u.id, Warnings: []string{}}
		if err := addUnit(opt.from, opt.dir, u, &rc); err != nil {
			return err
		}
		receipts = append(receipts, rc)
	}
	proveErr := prove(ctx, opt.dir, opt.units, io.Discard)
	if opt.format == planFormatJSON {
		env := addEnvelope{
			Unit: opt.unitID, From: opt.from, Dir: opt.dir,
			Receipts: receipts, BuildOk: proveErr == nil,
		}
		if err := printAddJSON(stdout, env); err != nil {
			return err
		}
		return proveErr
	}
	for i := range receipts {
		printAddReceipt(stdout, &receipts[i])
	}
	if proveErr != nil {
		return proveErr
	}
	fmt.Fprintln(stdout, "gogogo: done — review the diff, then run `make dev`")
	return nil
}

// parseAddArgs parses flags, resolves the unit, and validates paths.
func parseAddArgs(args []string, stdout io.Writer) (addOptions, error) {
	var opt addOptions
	fs := flag.NewFlagSet("gogogo add", flag.ContinueOnError)
	fs.StringVar(&opt.from, "from", "", "pristine template checkout to copy from (required)")
	fs.StringVar(&opt.dir, "dir", "", "target project checkout (required)")
	fs.BoolVar(&opt.yes, "yes", false, "apply without asking (agents: always pin this)")
	fs.BoolVar(&opt.dryRun, "dry-run", false, "print the plan and stop; changes nothing")
	fs.StringVar(&opt.format, "format", planFormatText, "plan format: text|json")
	fs.SetOutput(stdout)
	// Accept the unit before flags (`add whiteboard --from …`): the
	// stdlib flag parser stops at the first positional, so pull it out
	// first to support both orders.
	cliArgs := args
	var positional []string
	if len(cliArgs) > 0 && !strings.HasPrefix(cliArgs[0], "-") {
		positional = []string{cliArgs[0]}
		cliArgs = cliArgs[1:]
	}
	if err := fs.Parse(cliArgs); err != nil {
		return opt, err
	}
	if opt.format != planFormatText && opt.format != planFormatJSON {
		return opt, fmt.Errorf("unknown --format %q (want text|json)", opt.format)
	}
	names := make([]string, 0, len(positional)+len(fs.Args()))
	names = append(names, positional...)
	names = append(names, fs.Args()...)
	if len(names) != 1 {
		fs.Usage()
		return opt, errors.New("usage: gogogo add <unit> --from TEMPLATE --dir PROJECT")
	}
	opt.unitID = names[0]
	units, err := addClosure(names[0])
	if err != nil {
		return opt, err
	}
	opt.units = units
	if opt.from == "" || opt.dir == "" {
		fs.Usage()
		return opt, errors.New("--from and --dir are both required")
	}
	if _, err := os.Stat(opt.from); err != nil {
		return opt, &ExitError{code: 1, msg: "template source not found: " + opt.from}
	}
	return opt, nil
}

func confirmAdd(stdout io.Writer, stdin io.Reader, unitID string) bool {
	fmt.Fprintf(stdout, "Add unit %q from template? [y/N]: ", unitID)
	var answer [8]byte
	n, _ := stdin.Read(answer[:])
	resp := strings.ToLower(strings.TrimSpace(string(answer[:n])))
	return resp == "y" || resp == "yes"
}

// printAddPlan previews an add: unit metadata plus what would change.
func printAddPlan(w io.Writer, format string, u trimUnit, from, dir string) {
	if format == planFormatJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{
			"unit": u.id, "from": from, "dir": dir,
			"summary": unitOneLiner(u), "dryRun": true,
		})
		return
	}
	m := u.meta()
	fmt.Fprintf(w, "gogogo: add %s (%s) from %s into %s\n", u.id, m.kind, from, dir)
	fmt.Fprintf(w, "  copies %d path(s), %d wiring strip(s) to restore\n",
		len(m.dirs)+len(m.files),
		len(u.mainStrips)+len(u.desktopStrips)+len(u.extraStrips))
	for _, warn := range addFollowUps(u) {
		fmt.Fprintf(w, "  manual follow-up: %s\n", warn)
	}
}

func printAddJSON(w io.Writer, env addEnvelope) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

// printAddReceipt reports counts plus follow-ups.
func printAddReceipt(w io.Writer, rc *AddReceipt) {
	fmt.Fprintf(w, "gogogo: receipt: %d dir(s), %d file(s) copied, %d line(s) inserted\n",
		rc.DirsCopied, rc.FilesCopied, rc.LinesInserted)
	for _, warn := range rc.Warnings {
		fmt.Fprintf(w, "gogogo: follow-up: %s\n", warn)
	}
}
