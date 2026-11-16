// Installer CLI: `recipe source` — read a recipe's code, change nothing.
//
// WHY THIS EXISTS. A recipe is copied-and-owned when the operator wants to keep
// editing that exact slice. An agent adapting a pattern does not want the copy:
// it wants to READ how the slice is built and write its own. Before this
// command the only way to see a recipe's code was to install it, which created
// a receipt, a baseline and an update debt for something about to be rewritten.
//
// It is the read half of `recipe install`: same catalog entry, same declared
// file set, same bytes on disk — printed instead of copied. Nothing here writes,
// so it is safe for an agent to call while it is deciding what to build.
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

	"github.com/calionauta/gogogo/catalog"
	"github.com/calionauta/gogogo/internal/recipe"
)

// recipeSourceArgs is the parsed CLI surface of `recipe source`.
type recipeSourceArgs struct {
	id      string
	dir     string
	version string
	file    string
	src     string
	format  string
}

// sourceFile is one file of a recipe, as read from the tree.
type sourceFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	// Pristine is false when the tree's bytes no longer match the reviewed
	// hash. It is a note for the reader, never a refusal: the point is to read
	// what is there, and a modified recipe is exactly what a fork looks like.
	Pristine bool `json:"pristine"`
}

// sourceReport is the machine shape of `recipe source`.
type sourceReport struct {
	ID       string       `json:"id"`
	Version  string       `json:"version,omitempty"`
	Summary  string       `json:"summary,omitempty"`
	ReadOnly bool         `json:"readOnly"`
	Files    []sourceFile `json:"files"`
}

// runRecipeSource prints one recipe's code from the catalog (or a bundle
// directory) and writes nothing.
func runRecipeSource(args []string, stdout io.Writer) error {
	opt, err := parseRecipeSourceArgs(args)
	if err != nil {
		return err
	}
	report, err := readRecipeSource(opt)
	if err != nil {
		return err
	}
	if opt.format == planFormatJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	printRecipeSource(stdout, report)
	return nil
}

// referenceHint names the slices worth reading first, from the one file that
// answers that question (catalog/reference.yml). It is a suggestion, not a
// whitelist: any id in the tree reads the same way.
func referenceHint() string {
	entries, err := catalog.Reference()
	if err != nil || len(entries) == 0 {
		return ""
	}
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		parts = append(parts, e.ID+" — "+e.Why)
	}
	return "; start with: " + strings.Join(parts, "; ")
}

// parseRecipeSourceArgs reads the flags. The id comes first (like describe), so
// `recipe source todo` works without a flag.
func parseRecipeSourceArgs(args []string) (recipeSourceArgs, error) {
	opt := recipeSourceArgs{dir: ".", format: planFormatText}
	fs := flag.NewFlagSet("recipe source", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opt.dir, "dir", ".", "checkout or catalog root to read from")
	fs.StringVar(&opt.version, "version", "", "catalog version to read (default: latest)")
	fs.StringVar(&opt.file, "file", "", "only this file (exact path or suffix)")
	fs.StringVar(&opt.src, "src", "", "read a recipe bundle directory instead of the catalog")
	fs.StringVar(&opt.format, "format", planFormatText, "output format: text|json")
	// The id may come before or after the flags (`recipe source todo --file x`).
	// A bare leading word is the id; everything else goes to the flag parser,
	// which stops at the first positional it meets.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		opt.id = args[0]
		args = args[1:]
	}
	if perr := fs.Parse(args); perr != nil {
		return opt, &ExitError{code: 2, msg: "recipe source: " + perr.Error()}
	}
	if rest := fs.Args(); len(rest) > 0 {
		if opt.id != "" {
			return opt, &ExitError{code: 2, msg: "recipe source: unexpected argument " + rest[0]}
		}
		opt.id = rest[0]
	}
	if opt.src == "" && opt.id == "" {
		return opt, &ExitError{code: 2, msg: "recipe source needs a recipe id (or --src <dir>" +
			referenceHint() + ")"}
	}
	if opt.format != planFormatText && opt.format != planFormatJSON {
		return opt, &ExitError{code: 2, msg: "recipe source: unknown --format " + opt.format + " (want text|json)"}
	}
	return opt, nil
}

// readRecipeSource resolves the file set and reads each file from the tree.
func readRecipeSource(opt recipeSourceArgs) (sourceReport, error) {
	rep := sourceReport{ReadOnly: true, Files: []sourceFile{}}
	var err error
	if opt.src != "" {
		err = readFromBundle(&rep, opt.src)
	} else {
		err = readFromCatalog(&rep, opt.dir, opt.id, opt.version)
	}
	if err != nil {
		return rep, err
	}
	if opt.file != "" {
		rep.Files = filterSourceFiles(rep.Files, opt.file)
		if len(rep.Files) == 0 {
			return rep, &ExitError{code: 1, msg: "recipe source: no file matches " + opt.file}
		}
	}
	if len(rep.Files) == 0 {
		return rep, &ExitError{code: 1, msg: "recipe source: " + rep.ID + " declares no files"}
	}
	return rep, nil
}

// readFromBundle reads an author's recipe directory: the manifest is the
// declaration and every file is pristine by definition.
func readFromBundle(rep *sourceReport, src string) error {
	manifest, files, err := loadRecipeSource(src)
	if err != nil {
		return err
	}
	rep.ID, rep.Summary, rep.Version = manifest.ID, manifest.Summary, manifest.Version
	for p, body := range files {
		rep.Files = append(rep.Files, sourceFile{Path: p, Content: body, Pristine: true})
	}
	return nil
}

// readFromCatalog reads a committed recipe. The RECEIPT wins when one exists:
// it enumerates every installed file, while a catalog entry records the
// reviewed surface and may be narrower. That is what makes
// `recipe source todo` return the whole slice — and what lets a recipe read
// from a tree whose catalog is absent (a scaffolded project).
func readFromCatalog(rep *sourceReport, dir, id, version string) error {
	root, aerr := filepath.Abs(dir)
	if aerr != nil {
		return &ExitError{code: 1, msg: "recipe source: " + aerr.Error()}
	}
	rec, recErr := recipe.LoadReceipt(root, id)
	// A receipt that says "removed" is a record of what the slice owned, not a
	// description of the tree: its files are gone, so reading them would answer
	// with empty content. Fall back to the catalog instead.
	removed := recErr == nil && rec.Removed != ""
	haveReceipt := recErr == nil && !removed && len(rec.Files) > 0 && version == ""
	if !haveReceipt {
		entry, err := loadSourceEntry(dir, id, version)
		if err != nil {
			if removed {
				// The slice was dropped here, and it is not catalogued: name the
				// one place it can still be read instead of the generic miss.
				return &ExitError{code: 1, msg: fmt.Sprintf(
					"recipe source: %s was removed from this tree (%s); read it in the gogogo "+
						"template with `recipe source %s --dir PATH/TO/gogogo`", id, rec.Removed, id)}
			}
			return err
		}
		rep.ID, rep.Summary, rep.Version = entry.ID, entry.Summary, entry.Version
		return appendSourceFiles(rep, root, entry.Files)
	}
	rep.ID, rep.Summary, rep.Version = rec.ID, rec.Summary, rec.Version
	if rep.Summary == "" {
		if entry, err := loadSourceEntry(dir, id, ""); err == nil {
			rep.Summary = entry.Summary
			rep.Version = entry.Version
		}
	}
	return appendSourceFiles(rep, root, rec.Files)
}

// appendSourceFiles reads each declared path from the tree and records whether
// it still matches the declared hash.
func appendSourceFiles(rep *sourceReport, root string, files map[string]string) error {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		body, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if rerr != nil {
			// A reviewed file that is absent is a fact the reader needs: name
			// it and keep going rather than failing the whole read.
			rep.Files = append(rep.Files, sourceFile{Path: p})
			continue
		}
		want := strings.TrimPrefix(files[p], "sha256:")
		rep.Files = append(rep.Files, sourceFile{
			Path:     p,
			Content:  string(body),
			Pristine: want == "" || recipe.Hash(body) == want,
		})
	}
	return nil
}

// loadSourceEntry picks the catalog entry: the named version, or the newest one
// the catalog reviewed. It reuses the same scanner and version ordering the
// outdated check uses, so "latest" means one thing across the CLI.
func loadSourceEntry(dir, id, version string) (recipe.CatalogEntry, error) {
	catalogDir := resolveCatalogDir(dir, "")
	if version != "" {
		entry, err := recipe.LoadCatalogEntry(filepath.Join(catalogDir, id, version+".yaml"))
		if err != nil {
			if os.IsNotExist(err) {
				return entry, &ExitError{code: 1, msg: fmt.Sprintf(
					"recipe source: %s has no entry for %s %s", catalogDir, id, version)}
			}
			return entry, &ExitError{code: 1, msg: "recipe source: " + err.Error()}
		}
		return entry, nil
	}
	latest, err := catalogLatest("source", catalogDir)
	if err != nil {
		return recipe.CatalogEntry{}, err
	}
	entry, ok := latest[id]
	if !ok {
		return entry, &ExitError{code: 1, msg: fmt.Sprintf(
			"recipe source: no catalog entry for %q under %s (catalogued: %s; try `recipe list`)",
			id, catalogDir, strings.Join(sortedKeys(latest), ", "))}
	}
	return entry, nil
}

// sortedKeys lists the catalog ids in a stable order for an error message.
func sortedKeys(entries map[string]recipe.CatalogEntry) []string {
	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// filterSourceFiles keeps the exact path, or the files whose path ends with it
// (so `--file handlers/chi.go` finds the one handler without the full prefix).
func filterSourceFiles(files []sourceFile, want string) []sourceFile {
	want = filepath.ToSlash(want)
	out := make([]sourceFile, 0, 1)
	for _, f := range files {
		if f.Path == want || strings.HasSuffix(f.Path, "/"+want) {
			out = append(out, f)
		}
	}
	return out
}

// printRecipeSource renders the human shape: a header, then each file under a
// banner. The banner is `// path` so a pasted excerpt still says where it came
// from.
func printRecipeSource(w io.Writer, rep sourceReport) {
	fmt.Fprintf(w, "recipe %s", rep.ID)
	if rep.Version != "" {
		fmt.Fprintf(w, " %s", rep.Version)
	}
	fmt.Fprintln(w, " — read-only, nothing was written")
	if rep.Summary != "" {
		fmt.Fprintln(w, rep.Summary)
	}
	for _, f := range rep.Files {
		fmt.Fprintf(w, "\n===== %s", f.Path)
		if !f.Pristine {
			fmt.Fprint(w, " (modified)")
		}
		fmt.Fprintln(w, " =====")
		fmt.Fprint(w, f.Content)
		if !strings.HasSuffix(f.Content, "\n") {
			fmt.Fprintln(w)
		}
	}
}
