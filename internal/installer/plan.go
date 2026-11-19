// Installer engine: plan preview, receipts, and JSON envelopes
package installer

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/calionauta/gogogo/internal/capabilities"
	shapekit "github.com/calionauta/gogogo/internal/shape"
)

// keptUnit is what ships: id + kind + one-line summary, so deciders
// reading --format json see what each capability is, not just its id.
type keptUnit struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Summary string `json:"summary"`
}

// scaffoldPlan is the full preview of what the installer will do. Scaffold
// starts from the whole tree and then drops the demo slices the operator did
// not keep; Keep lists the remaining capabilities, each with its removal class
// so an agent reading --format json has the same three words the registry uses.
//
// Removal of a shipped slice is NOT a scaffold decision: the bundled demos are
// wired into core (chi.go, desktopboot, sqlc queries), so dropping one is a core
// edit, not a file delete. The plan says so once, via RecipePolicy.
type scaffoldPlan struct {
	Name   string     `json:"name"`
	Owner  string     `json:"owner"`
	Module string     `json:"module"`
	Dir    string     `json:"dir"`
	Keep   []keptUnit `json:"keepUnits"`
	// Shape is the product shape the scaffold will apply, and ShapeRemove the
	// entrypoints it prunes. `full` means "every target" — a plain clone.
	Shape       string   `json:"shape"`
	ShapeRemove []string `json:"shapeRemoves,omitempty"`
	DryRun      bool     `json:"dryRun"`
	// Recipes is the selector the operator gave (none|all|<ids>).
	Recipes string `json:"recipes"`
	// DropRecipes leave the tree before the proof build; KeepRecipes stay, and
	// a keep with a Why is a slice the scaffold could not remove safely.
	DropRecipes []string    `json:"dropRecipes,omitempty"`
	KeepRecipes []recipePin `json:"keepRecipes,omitempty"`
}

func buildPlan(name, owner, dir, shapeName string, dryRun bool) scaffoldPlan {
	p := scaffoldPlan{
		Name:    name,
		Owner:   owner,
		Module:  "github.com/" + owner + "/" + name,
		Dir:     dir,
		Keep:    []keptUnit{},
		DryRun:  dryRun,
		Shape:   shapeName,
		Recipes: recipesNone,
	}
	if shapeName == "" {
		p.Shape = string(shapekit.Full)
	}
	if cfg, err := shapekit.Parse(shapekit.Config{Shape: shapekit.Shape(p.Shape)}); err == nil {
		keep := map[string]bool{}
		for _, ep := range cfg.Entrypoints {
			keep[ep] = true
		}
		for _, ep := range shapeEntrypoints {
			if !keep[ep] {
				p.ShapeRemove = append(p.ShapeRemove, ep)
			}
		}
	}
	for _, capability := range capabilities.All {
		p.Keep = append(p.Keep, keptUnit{
			ID:      capability.ID,
			Kind:    string(capability.Kind),
			Summary: capability.Summary,
		})
	}
	return p
}

// The selector's two shorthands: drop every demo slice, or keep them all. Any
// other value is a comma-separated list of demo ids to drop.
const (
	recipesNone = "none"
	recipesAll  = "all"
)

// demoSlices are the bundled slices a fresh product does not need. They are
// reference material: read one with `recipe source`, copy one with
// `recipe install`. Platform surfaces (account, dashboard, config, credits,
// license, notifications, auth views) are NOT here — the shell calls them, so
// removing one is a product decision, not a demo trim.
//
// `todo` IS here, on purpose. It is the reference implementation, but "keep the
// reference" is a default, not a law: a product that ships its own domain has
// no reason to carry it. The import gate decides — and today it keeps todo
// because internal/desktopboot and cmd/gui import it, which the plan prints.
// Silently refusing the id would hide that; naming the importers is the answer
// an operator can act on (`--shape web` first, then drop it).
var demoSlices = []string{
	recipeChat, recipeKiosk, recipeNotes, recipeReview, recipeRoom, recipeTodo, recipeWhiteboard,
}

// recipePin is one slice the scaffold keeps, with the reason when the reason is
// not "you asked for it".
type recipePin struct {
	ID  string `json:"id"`
	Why string `json:"why,omitempty"`
}

func printPlanText(w io.Writer, p scaffoldPlan) {
	fmt.Fprintf(w, "gogogo: scaffolding %s\n", p.Module)
	fmt.Fprintf(w, "  dir:            %s\n", p.Dir)
	fmt.Fprintf(w, "  capabilities:   %s\n", kindLists(p.Keep))
	if p.Shape != "" && p.Shape != string(shapekit.Full) {
		fmt.Fprintf(w, "  shape:          %s (drops %s)\n", p.Shape, strings.Join(p.ShapeRemove, ", "))
	}
	fmt.Fprintf(w, "  recipes:        %s\n", p.Recipes)
	if len(p.DropRecipes) > 0 {
		fmt.Fprintf(w, "  drop recipes:   %s (read any of them in the template with `recipe source <id>`)\n",
			strings.Join(p.DropRecipes, ", "))
	}
	for _, keep := range p.KeepRecipes {
		if keep.Why != "" {
			fmt.Fprintf(w, "  keeps %-9s %s\n", keep.ID+":", keep.Why)
		}
	}
	fmt.Fprintln(w, "  removal:        core is never removed; servants by hand; `recipe uninstall` for a recipe")
}

// kindLists renders the kept units grouped by removal class, in the class
// order core → servant → feature. It reads the plan's own Kind values, so the
// text cannot disagree with the JSON.
func kindLists(units []keptUnit) string {
	order := []capabilities.Kind{capabilities.KindCore, capabilities.KindServant, capabilities.KindFeature}
	var parts []string
	for _, kind := range order {
		var ids []string
		for _, u := range units {
			if u.Kind == string(kind) {
				ids = append(ids, u.ID)
			}
		}
		if len(ids) > 0 {
			parts = append(parts, string(kind)+": "+strings.Join(ids, ", "))
		}
	}
	return strings.Join(parts, "; ")
}

func printPlanJSON(w io.Writer, p scaffoldPlan) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(p)
}

// envelope is the single machine-readable document for apply runs: the plan as
// previewed plus the proof. Each dropped slice keeps its receipt in
// `.recipes/<id>.yml`, marked removed, which `recipe status` and `recipe source`
// still read.
type envelope struct {
	Plan     scaffoldPlan `json:"plan"`
	BuildOk  bool         `json:"buildOk"`
	BuildErr string       `json:"buildError,omitempty"`
	Next     nextSteps    `json:"next"`
}

func printEnvelopeJSON(w io.Writer, e envelope) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(e)
}

// applyRecipeSelection resolves the selector against the tree on disk. The
// default is best-effort: a demo slice something still imports stays, with the
// importer named. An explicit request is a promise, so the same condition is an
// error — a tree that does not build is not a product.
func applyRecipeSelection(p *scaffoldPlan, opt options) error {
	selector := strings.TrimSpace(opt.recipes)
	if selector == "" {
		selector = recipesNone
	}
	p.Recipes, p.DropRecipes, p.KeepRecipes = selector, nil, nil
	if selector == recipesAll {
		for _, id := range demoSlices {
			p.KeepRecipes = append(p.KeepRecipes, recipePin{ID: id})
		}
		return nil
	}
	explicit := selector != recipesNone
	want := map[string]bool{}
	if explicit {
		for _, id := range splitIDs(selector) {
			if !slices.Contains(demoSlices, id) {
				return fmt.Errorf("unknown demo slice %q (removable: %s; everything else is a platform "+
					"surface the shell calls)", id, strings.Join(demoSlices, ", "))
			}
			want[id] = true
		}
	}
	for _, id := range demoSlices {
		if explicit && !want[id] {
			p.KeepRecipes = append(p.KeepRecipes, recipePin{ID: id})
			continue
		}
		users, err := sliceImporters(opt.dir, id, removedPrefixes(p.Shape))
		if err != nil {
			return err
		}
		if len(users) > 0 {
			why := "still imported by " + strings.Join(firstN(users, 3), ", ")
			if len(users) > 3 {
				why += fmt.Sprintf(" (+%d more)", len(users)-3)
			}
			if explicit {
				return fmt.Errorf("cannot remove %s: %s", id, why)
			}
			p.KeepRecipes = append(p.KeepRecipes, recipePin{ID: id, Why: why})
			continue
		}
		p.DropRecipes = append(p.DropRecipes, id)
	}
	p.withoutDroppedUnits()
	return nil
}

// withoutDroppedUnits takes the removed slices out of the capabilities list, so
// the plan cannot say "feature: kiosk" and "drop recipes: kiosk" at once.
//
// It deliberately does NOT cascade to servants. DependsOn records the edges a
// unit's own code needs to compile — it is not a reachability graph for "is this
// still wanted": components, datastar, nats and events are depended on by
// almost nobody yet are load-bearing for everything. A cascade over this graph
// dropped 30 capabilities on a `--recipes none` run (measured), including the
// Datastar runtime and the component library, so inferring removal from it is
// how a template silently guts itself. Dropping a servant is a decision, and it
// stays one: it needs an explicit request.
func (p *scaffoldPlan) withoutDroppedUnits() {
	if len(p.DropRecipes) == 0 || len(p.Keep) == 0 {
		return
	}
	dropped := make(map[string]bool, len(p.DropRecipes))
	for _, id := range p.DropRecipes {
		dropped[id] = true
	}
	kept := p.Keep[:0]
	for _, u := range p.Keep {
		if !dropped[u.ID] {
			kept = append(kept, u)
		}
	}
	p.Keep = kept
}

// sliceImporters lists the kept Go files that import the slice. It is the
// removal gate: deleting a package while something still imports it produces a
// tree that does not compile, and the proof build would only say so after the
// files are gone.
func sliceImporters(root, id string, skipPrefixes []string) ([]string, error) {
	if root == "" {
		return nil, nil
	}
	if _, statErr := os.Stat(filepath.Join(root, "go.mod")); statErr != nil {
		// A directory the scaffold will clone: nothing here can import it yet.
		return nil, nil
	}
	mod, err := modulePath(root)
	if err != nil {
		return nil, err
	}
	if mod == "" {
		return nil, nil
	}
	target := mod + "/features/" + id
	own := filepath.Join(root, "features", id) + string(filepath.Separator)
	tree, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tree.Close() }()
	var users []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if skippedScanDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if hasAnyPrefix(path, root, skipPrefixes) {
			return nil
		}
		rel, hit, ferr := fileImportsSlice(tree, root, path, own, target)
		if ferr != nil {
			return ferr
		}
		if hit {
			users = append(users, rel)
		}
		return nil
	})
	sort.Strings(users)
	return users, err
}

// removedPrefixes names the directories a shape will delete. The import scan
// runs on the tree BEFORE the shape is applied, so without this a `--shape web`
// tree would keep a slice only because cmd/gui — which the shape removes —
// imports it. Scanning what survives is the honest question.
func removedPrefixes(shapeName string) []string {
	cfg, err := shapekit.Parse(shapekit.Config{Shape: shapekit.Shape(shapeName)})
	if err != nil || cfg.Shape == shapekit.Full {
		return nil
	}
	keep := map[string]bool{}
	for _, ep := range cfg.Entrypoints {
		keep[ep] = true
	}
	var out []string
	for _, ep := range shapeEntrypoints {
		if !keep[ep] {
			out = append(out, ep)
		}
	}
	return out
}

// fileImportsSlice answers for one file: does it import the slice? It reads
// through the tree root, so a symlink cannot point the gate at a file the
// removal will not touch.
func fileImportsSlice(tree *os.Root, root, path, own, target string) (string, bool, error) {
	if !strings.HasSuffix(path, ".go") || strings.HasPrefix(path, own) {
		return "", false, nil
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", false, err
	}
	f, err := tree.Open(rel)
	if err != nil {
		return "", false, err
	}
	src, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		return "", false, err
	}
	// A generated file names its slice only until the generator runs again;
	// prune regenerates the registration itself, so its import is not a reason
	// to keep the slice.
	if generatedFile(src) {
		return "", false, nil
	}
	hit, err := importsSlice(rel, src, target)
	return filepath.ToSlash(rel), hit, err
}

// The directories a tree-scanning gate must not read. Named once because three
// gates (the import scan, the drift scan, the rename walk) skip the same noise.
const (
	dirNodeModules = "node_modules"
	dirData        = "data"
	dirSite        = "site"
	dirVendor      = "vendor"
	dirTmp         = "tmp"
)

// skippedScanDir is the noise a removal gate must not read: VCS data, installed
// packages, runtime data, generated site output, vendored code, scratch copies.
func skippedScanDir(name string) bool {
	switch name {
	case gitDirName, dirNodeModules, dirData, dirSite, dirVendor, dirTmp:
		return true
	}
	return false
}

// hasAnyPrefix reports whether path lives under one of the directories the
// shape is about to delete.
func hasAnyPrefix(path, root string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	for _, prefix := range prefixes {
		if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
			return true
		}
	}
	return false
}

// importsSlice reports whether one file imports the slice package or any of its
// subpackages.
func importsSlice(rel string, src []byte, target string) (bool, error) {
	file, err := parser.ParseFile(token.NewFileSet(), rel, src, parser.ImportsOnly)
	if err != nil {
		return false, err
	}
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if path == target || strings.HasPrefix(path, target+"/") {
			return true, nil
		}
	}
	return false, nil
}

// generatedHeaderBytes bounds the header scan: every generator in this tree
// writes its marker in the first lines.
const generatedHeaderBytes = 512

// generatedFile reports the "Code generated ... DO NOT EDIT." header. Those
// files are rewritten, not kept.
func generatedFile(src []byte) bool {
	if len(src) > generatedHeaderBytes {
		src = src[:generatedHeaderBytes]
	}
	text := string(src)
	return strings.Contains(text, "Code generated") && strings.Contains(text, "DO NOT EDIT")
}

// splitIDs is the comma list without the empty entries a trailing comma makes.
func splitIDs(selector string) []string {
	var out []string
	for id := range strings.SplitSeq(selector, ",") {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// firstN caps an error message at the files that make the point.
func firstN(items []string, n int) []string {
	if len(items) <= n {
		return items
	}
	return items[:n]
}
