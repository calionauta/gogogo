// SCOPE:plugin — installer engine: export a unit into a scratch port bundle
package installer

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// templateModulePath is the import prefix every scaffolded tree rewrites to its
// own module. export lists the copied files that still carry it, because that
// rewrite is the one thing a port cannot skip.
const templateModulePath = "github.com/" + templateRepoRef

// exportFile is one row of the bundle: where a file sits in the template, where
// it lands inside the scratch bundle, and where it belongs in the reader's repo.
type exportFile struct {
	From string `json:"from"`
	To   string `json:"to"`
	Dest string `json:"dest"`
}

// exportBundle is the machine shape of one export run (and the recipe's data).
type exportBundle struct {
	Unit      string       `json:"unit"`
	Units     []string     `json:"units"`
	From      string       `json:"from"`
	To        string       `json:"to"`
	Module    string       `json:"module,omitempty"`
	Release   string       `json:"sourceRelease,omitempty"`
	Files     []exportFile `json:"files"`
	Rewrites  []exportFile `json:"rewrites,omitempty"`
	Routes    []string     `json:"routes,omitempty"`
	Manual    []string     `json:"manual,omitempty"`
	FollowUps []string     `json:"followUps,omitempty"`
	Proof     []string     `json:"proof"`
	Recipe    string       `json:"recipe"`
}

// exportArgs is the CLI surface.
type exportArgs struct {
	unit   string
	from   string
	to     string
	module string
	format string
}

// runExport is the `export` subcommand: read the template, write a scratch
// bundle, never touch the target tree.
func runExport(args []string, stdout io.Writer) error {
	opt, err := parseExportArgs(args)
	if err != nil {
		return err
	}
	bundle, err := exportUnit(opt)
	if err != nil {
		return err
	}
	if opt.format == planFormatJSON {
		return printExportJSON(stdout, bundle)
	}
	printExportText(stdout, bundle)
	return nil
}

// parseExportArgs validates the shape. Every refusal happens here, before
// anything is written: a bundle half-written into a directory the caller cannot
// use is worse than a refusal that names the flag to fix.
func parseExportArgs(args []string) (exportArgs, error) {
	var opt exportArgs
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	dir := fs.String("dir", "", "pristine template checkout to copy the unit FROM (required)")
	to := fs.String("to", "", "scratch directory to write the port bundle INTO (required; must not exist or be empty)")
	module := fs.String("module", "", "your project's module path, filled into the import rewrite (optional)")
	format := fs.String("format", planFormatText, "output format: text|json")
	fs.SetOutput(io.Discard)
	// Accept the unit before the flags (`export whiteboard --dir …`): the
	// stdlib parser stops at the first positional, so pull it out first and
	// support both orders — the same rule `add` follows.
	cliArgs := args
	names := make([]string, 0, 1+len(args))
	if len(cliArgs) > 0 && !strings.HasPrefix(cliArgs[0], "-") {
		names = append(names, cliArgs[0])
		cliArgs = cliArgs[1:]
	}
	if err := fs.Parse(cliArgs); err != nil {
		return opt, &ExitError{code: 1, msg: "export: " + err.Error()}
	}
	names = append(names, fs.Args()...)
	if len(names) != 1 {
		return opt, &ExitError{code: 1, msg: "export needs exactly one unit id (e.g. " +
			"`export whiteboard`); `gogogo --help` lists the offered units"}
	}
	if *dir == "" || *to == "" {
		return opt, &ExitError{code: 1, msg: "export needs --dir (a template checkout) " +
			"and --to (a scratch directory)"}
	}
	if *format != planFormatText && *format != planFormatJSON {
		return opt, &ExitError{code: 1, msg: fmt.Sprintf("unknown --format %q (want text|json)", *format)}
	}
	if _, err := addClosure(names[0]); err != nil {
		return opt, &ExitError{code: 1, msg: err.Error()}
	}
	return exportArgs{unit: names[0], from: *dir, to: *to, module: *module, format: *format}, nil
}

// exportUnit resolves the unit closure, pre-flights every source file, then
// writes the bundle. Source problems fail before any write; the recipe is
// written LAST, so a half-copied bundle never carries a recipe that claims a
// complete one.
func exportUnit(opt exportArgs) (exportBundle, error) {
	root, dest, resolveErr := resolveExportPaths(opt)
	if resolveErr != nil {
		return exportBundle{}, resolveErr
	}
	units, closureErr := addClosure(opt.unit)
	if closureErr != nil {
		return exportBundle{}, &ExitError{code: 1, msg: closureErr.Error()}
	}
	bundle, planErr := planBundle(root, dest, opt, units)
	if planErr != nil {
		return exportBundle{}, planErr
	}
	for _, f := range bundle.Files {
		if copyErr := copyExportFile(f); copyErr != nil {
			return exportBundle{}, copyErr
		}
	}
	bundle.Recipe = filepath.Join(dest, "PORTING.md")
	recipeErr := os.WriteFile(bundle.Recipe, []byte(renderPortRecipe(bundle)), scaffoldFileMode)
	if recipeErr != nil {
		return exportBundle{}, &ExitError{code: 1, msg: "export: write recipe: " + recipeErr.Error()}
	}
	return bundle, nil
}

// resolveExportPaths turns the flags into absolute paths and refuses the two
// destinations that would make this tool a mutator.
func resolveExportPaths(opt exportArgs) (root, dest string, err error) {
	root, err = filepath.Abs(opt.from)
	if err != nil {
		return "", "", &ExitError{code: 1, msg: "export: " + err.Error()}
	}
	if !looksLikeTemplate(root) {
		return "", "", &ExitError{code: 1, msg: "export: " + opt.from +
			" is not a gogogo checkout (no cmd/web/main.go or internal/installer/run.go), " +
			"so there is nothing to copy a unit out of"}
	}
	dest, err = filepath.Abs(opt.to)
	if err != nil {
		return "", "", &ExitError{code: 1, msg: "export: " + err.Error()}
	}
	if checkErr := checkExportDest(root, dest); checkErr != nil {
		return "", "", checkErr
	}
	return root, dest, nil
}

// planBundle lists every file the closure owns, attaches the wiring data, and
// pre-flights the sources: nothing is written until every source is readable.
func planBundle(root, dest string, opt exportArgs, units []trimUnit) (exportBundle, error) {
	bundle := exportBundle{
		Unit: opt.unit, From: root, To: dest, Module: opt.module,
		Release: sourceRelease(root),
		Proof:   []string{"go build ./...", "go test ./..."},
	}
	for _, u := range units {
		if err := addUnitToBundle(&bundle, root, dest, u); err != nil {
			return exportBundle{}, err
		}
	}
	if len(bundle.Files) == 0 {
		return exportBundle{}, &ExitError{code: 1, msg: "export: " + opt.unit + " owns no files"}
	}
	if err := assertInsideBundle(dest, bundle.Files); err != nil {
		return exportBundle{}, err
	}
	for _, f := range bundle.Files {
		if _, err := os.Stat(f.From); err != nil {
			return exportBundle{}, &ExitError{code: 1, msg: "export: " + f.From + ": " + err.Error()}
		}
	}
	bundle.Rewrites = rewriteCandidates(bundle.Files)
	sort.Slice(bundle.Rewrites, func(i, j int) bool { return bundle.Rewrites[i].Dest < bundle.Rewrites[j].Dest })
	return bundle, nil
}

// addUnitToBundle records one unit's files and wiring. The wiring lists are
// what `add` would restore mechanically; in a foreign tree they are the work.
func addUnitToBundle(bundle *exportBundle, root, dest string, u trimUnit) error {
	bundle.Units = append(bundle.Units, u.id)
	routes := exportRoutes(u)
	bundle.Routes = append(bundle.Routes, routes...)
	for _, line := range addManualWiring(u) {
		if manualDuplicatesRoute(line, routes) {
			continue
		}
		bundle.Manual = append(bundle.Manual, line)
	}
	bundle.FollowUps = append(bundle.FollowUps, addFollowUps(u)...)
	m := u.meta()
	for _, rel := range m.dirs {
		files, err := collectUnitDir(root, rel)
		if err != nil {
			return err
		}
		for _, f := range files {
			bundle.Files = append(bundle.Files, exportFile{
				From: filepath.Join(root, filepath.FromSlash(f)),
				To:   filepath.Join(dest, u.id, filepath.FromSlash(f)),
				Dest: f,
			})
		}
	}
	for _, rel := range m.files {
		src := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(src); err != nil {
			return &ExitError{code: 1, msg: "export: " + rel +
				" is missing from " + root + " — the checkout is older or newer than this installer"}
		}
		bundle.Files = append(bundle.Files, exportFile{
			From: src,
			To:   filepath.Join(dest, u.id, filepath.FromSlash(rel)),
			Dest: rel,
		})
	}
	return nil
}

// checkExportDest refuses the destinations that would make this tool a
// mutator: inside the template (writing into the source), or a non-empty
// directory (clobbering someone's work). Both refusals name the fix.
func checkExportDest(root, dest string) error {
	if rel, err := filepath.Rel(root, dest); err == nil && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return &ExitError{code: 1, msg: "export: --to must be outside the template checkout " +
			"(this would write into " + root + "); point it at a scratch directory"}
	}
	entries, err := os.ReadDir(dest)
	switch {
	case err == nil && len(entries) > 0:
		return &ExitError{code: 1, msg: "export: --to " + dest + " is not empty; " +
			"point it at a new or empty directory so nothing is clobbered"}
	case err != nil && !os.IsNotExist(err):
		return &ExitError{code: 1, msg: "export: " + err.Error()}
	}
	return nil
}

// collectUnitDir lists every regular file under one owned directory, as
// slash-separated paths relative to the template root.
func collectUnitDir(root, rel string) ([]string, error) {
	base := filepath.Join(root, filepath.FromSlash(rel))
	var out []string
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		relPath, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		out = append(out, filepath.ToSlash(relPath))
		return nil
	})
	if err != nil {
		return nil, &ExitError{code: 1, msg: "export: read " + rel + ": " + err.Error()}
	}
	sort.Strings(out)
	return out, nil
}

// rewriteCandidates lists the copied files that still import the template
// module. It reads the sources it is about to copy, so the recipe states a
// measured fact rather than "check your imports".
func rewriteCandidates(files []exportFile) []exportFile {
	var out []exportFile
	for _, f := range files {
		raw, err := os.ReadFile(f.From)
		if err != nil {
			continue
		}
		if strings.Contains(string(raw), templateModulePath) {
			out = append(out, f)
		}
	}
	return out
}

// assertInsideBundle refuses any planned file outside dest. The manifest's
// relative paths are trusted, but "trusted" is not a proof, and the write is
// the one place where a wrong path escapes the scratch directory.
func assertInsideBundle(dest string, files []exportFile) error {
	for _, f := range files {
		rel, err := filepath.Rel(dest, f.To)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return &ExitError{code: 1, msg: "export: " + f.To + " would land outside " + dest +
				"; refusing to write it"}
		}
	}
	return nil
}

// copyExportFile writes one bundle file, creating parents.
func copyExportFile(f exportFile) error {
	raw, err := os.ReadFile(f.From)
	if err != nil {
		return &ExitError{code: 1, msg: "export: " + err.Error()}
	}
	if err := os.MkdirAll(filepath.Dir(f.To), dirMode); err != nil {
		return &ExitError{code: 1, msg: "export: " + err.Error()}
	}
	//nolint:gosec // f.To is --to (the caller's own destination) plus a manifest
	// path, and assertInsideBundle has already proved it stays under --to.
	if err := os.WriteFile(f.To, raw, scaffoldFileMode); err != nil {
		return &ExitError{code: 1, msg: "export: " + err.Error()}
	}
	return nil
}

// manualDuplicatesRoute drops a manual line whose payload already printed as a
// route: `internal/server/chi.go: MountSubtree(…)` would otherwise appear
// twice, once as wiring and once as the route it is.
func manualDuplicatesRoute(line string, routes []string) bool {
	payload := line
	if _, after, ok := strings.Cut(line, ": "); ok {
		payload = after
	}
	return slices.Contains(routes, payload)
}

// exportRoutes lists the router lines a unit owns, filtered to the ones that
// actually register something. routerCallLines is the add engine's restore list
// and deliberately carries comments and struct fields too (it re-creates a
// block verbatim); a port recipe that prints those would read as route wiring.
func exportRoutes(u trimUnit) []string {
	var out []string
	for _, line := range routerCallLines(u) {
		trimmed := strings.TrimSpace(line)
		if isRouteLine(trimmed) {
			out = append(out, trimmed)
		}
	}
	return out
}

// isRouteLine reports whether a line mounts or registers a route. MountSubtree
// appears both as a method call (`r.MountSubtree(…)`) and as a bare helper
// call, so both forms are matched.
func isRouteLine(s string) bool {
	if s == "" || strings.HasPrefix(s, "//") {
		return false
	}
	for _, marker := range []string{
		".MountFeature(", ".MountSubtree(", ".MountPath(",
		"MountFeature(", "MountSubtree(", "MountPath(",
		"mux.Handle", "r.Get(", "r.NotFound(",
	} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

// printExportText is the human receipt: where the bundle is, what it holds, and
// the one command to run next.
func printExportText(w io.Writer, b exportBundle) {
	fmt.Fprintf(w, "gogogo: export %s from %s into %s\n", b.Unit, b.From, b.To)
	if len(b.Units) > 1 {
		fmt.Fprintf(w, "  units: %s (dependencies cascade)\n", strings.Join(b.Units, ", "))
	}
	fmt.Fprintf(w, "  %d file(s) copied", len(b.Files))
	if n := len(b.Rewrites); n > 0 {
		fmt.Fprintf(w, ", %d carrying the %s import path", n, templateModulePath)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  recipe: %s\n", b.Recipe)
	if b.Release != "" {
		// The reader is porting from THIS release; the recipe is a snapshot,
		// so naming the release is what lets them come back for the rest.
		fmt.Fprintf(w, "  source release: v%s\n", b.Release)
	}
	fmt.Fprintln(w, "  next: follow the recipe (move files, rewrite imports, wire by hand), "+
		"then `go build ./...` in YOUR tree — nothing here was written outside the bundle.")
}

// printExportJSON is the machine shape.
func printExportJSON(w io.Writer, b exportBundle) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(b)
}
