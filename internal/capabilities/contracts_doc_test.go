// SCOPE:plugin — generated unit contract cards (drift test).
//
// docs/unit-contracts.md is generated from the registry plus the sources each
// unit owns, never written by hand. The registry already answers "what class
// is this and what happens if I delete it"; the card adds the facts an agent
// needs before editing a unit — which routes it registers and which generated
// queries it calls — so a new session does not have to grep four directories
// to find out that `notes` owns no route but calls 19 queries.
//
// Regenerate with:
//
//	go test ./internal/capabilities/ -run TestCapabilityContractsDocFresh -update

package capabilities

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// contractRouteRE matches the route-registration idiom every feature router
// uses, the same shape the RBAC coverage lint scans for. Registration is the
// only honest route source: a literal in a comment is not a route.
var contractRouteRE = regexp.MustCompile(
	`r\.(?:With\([^)]*\)\.)?(?:Get|Post|Put|Patch|Delete|Head)\(["\x60](/[^"\x60]*)["\x60]`)

// contractQueryRE matches a method call whose name sqlc generated. Handlers
// reach the store through a *dbgen.Queries field (`h.Q.CreateTodo(…)`), not
// through the package qualifier, so matching the call name and intersecting
// with the generated set is the only honest scan. The intersection also keeps
// parameter structs and row types out.
var contractQueryRE = regexp.MustCompile(`\.([A-Z][A-Za-z0-9_]*)\s*\(`)

// sqlcNameRE matches the query-name header sqlc keeps in every source file.
var sqlcNameRE = regexp.MustCompile(`(?m)^-- name: ([A-Za-z0-9_]+) `)

// TestCapabilityContractsDocFresh pins the generated cards. It shares the
// -update flag with TestApiRoutesDocFresh, so one command refreshes both
// generated docs (the installer's proof step runs that command).
func TestCapabilityContractsDocFresh(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "docs", "unit-contracts.md")
	want := renderUnitContracts(t, root)
	if *apiRoutesDocUpdate {
		if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (regenerate with -update)", path, err)
	}
	if string(raw) != want {
		t.Fatalf("docs/unit-contracts.md is stale (%d units on disk):\n"+
			"regenerate with go test ./internal/capabilities/ -run TestCapabilityContractsDocFresh -update",
			len(All))
	}
}

// renderUnitContracts produces the whole document deterministically: units
// sorted by class then id, and every list sorted, so an unrelated edit never
// dirties the file.
func renderUnitContracts(t *testing.T, root string) string {
	t.Helper()
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rootFS.Close() }()

	units := slices.Clone(All)
	slices.SortFunc(units, func(a, b Capability) int {
		if ka, kb := contractKindRank(a.Kind), contractKindRank(b.Kind); ka != kb {
			return ka - kb
		}
		return strings.Compare(a.ID, b.ID)
	})

	var b strings.Builder
	b.WriteString("# Unit contracts\n\n")
	b.WriteString("> Generated from `internal/capabilities/registry.go` and the sources\n")
	b.WriteString("> each unit owns — do not edit by hand.\n")
	b.WriteString("> Regenerate: `go test ./internal/capabilities/ -run TestCapabilityContractsDocFresh -update`\n")
	b.WriteString("> Classes and removal rules: [Scope taxonomy](scope-taxonomy.md).\n\n")
	b.WriteString("One card per unit: what surface it serves, which routes it registers,\n")
	b.WriteString("which generated queries it calls, how it is hidden at runtime, and what\n")
	b.WriteString("removing it takes. Cards are the first thing to read after the SCOPE line\n")
	b.WriteString("of the file you are about to touch.\n\n")
	renderRegistrySummary(&b, units)
	for _, c := range units {
		renderUnitCard(&b, rootFS, c)
	}
	return b.String()
}

// renderRegistrySummary is the ONE place counts live. Prose must link here
// instead of writing "N capabilities": a hand-written count is what drifted
// (24 vs 35) and no reviewer catches it in a diff.
func renderRegistrySummary(b *strings.Builder, units []Capability) {
	counts := map[Kind]int{}
	offered := 0
	for _, c := range units {
		counts[c.Kind]++
		if c.Offered {
			offered++
		}
	}
	fmt.Fprintf(b, "## Registry at a glance\n\n")
	fmt.Fprintf(b, "- **Units:** %d — %d core · %d plugin · %d feature\n",
		len(units), counts[KindCore], counts[KindPlugin], counts[KindFeature])
	fmt.Fprintf(b, "- **Offered to the installer:** %d\n", offered)
	b.WriteString("- **Source of truth:** `internal/capabilities/registry.go`; this doc is\n")
	b.WriteString("  regenerated from it, so the counts above cannot drift.\n\n")
}

// contractKindRank orders cards core → plugin → feature.
func contractKindRank(k Kind) int {
	switch k {
	case KindCore:
		return 0
	case KindPlugin:
		return 1
	default:
		return 2
	}
}

// renderUnitCard writes one unit's card.
func renderUnitCard(b *strings.Builder, rootFS *os.Root, c Capability) {
	routes, queries := scanOwnedSurface(rootFS, c, contractQueryNames(rootFS))

	fmt.Fprintf(b, "## `%s` — %s\n\n%s\n\n", c.ID, c.Kind, c.Summary)
	writeList(b, "Owned", append(slices.Clone(c.Dirs), c.Files...))
	writeList(b, "Routes", routes)
	writeList(b, "Store calls", queries)
	if c.RuntimeOff != "" {
		fmt.Fprintf(b, "- **Runtime off:** `%s`\n", c.RuntimeOff)
	}
	if c.UISignal != "" {
		fmt.Fprintf(b, "- **UI signal:** `todo.Signals.%s`\n", c.UISignal)
	}
	if len(c.DependsOn) > 0 {
		fmt.Fprintf(b, "- **Depends on:** %s\n", strings.Join(c.DependsOn, ", "))
	}
	fmt.Fprintf(b, "- **Trim:** %s\n", contractTrim(c))
	if c.Reason != "" {
		fmt.Fprintf(b, "- **Why that class:** %s\n", c.Reason)
	}
	if c.Note != "" {
		fmt.Fprintf(b, "- **Note:** %s\n", c.Note)
	}
	for _, w := range c.Warns {
		fmt.Fprintf(b, "- **Warning:** %s\n", w)
	}
	b.WriteString("\n")
}

// contractTrim states how the unit goes away: installer unit or manual. It
// deliberately names no command: there is no `gogogo trim <id>` subcommand —
// a unit is dropped by excluding it from a scaffold's keep lists/presets.
func contractTrim(c Capability) string {
	if c.Offered {
		return "offered to the installer (a scaffold's keep lists/presets drop it)"
	}
	return "manual (no installer unit)"
}

// writeList writes a labelled inline code list, skipping empty values.
func writeList(b *strings.Builder, label string, items []string) {
	kept := make([]string, 0, len(items))
	for _, it := range items {
		if it == "" || slices.Contains(kept, it) {
			continue
		}
		kept = append(kept, it)
	}
	if len(kept) == 0 {
		return
	}
	quoted := make([]string, len(kept))
	for i, it := range kept {
		quoted[i] = "`" + it + "`"
	}
	fmt.Fprintf(b, "- **%s:** %s\n", label, strings.Join(quoted, ", "))
}

// scanOwnedSurface returns the routes and generated-query calls found in the
// files a unit owns. Owned paths that do not exist (trimmed trees) are
// skipped: the card describes the tree it is generated from.
func scanOwnedSurface(rootFS *os.Root, c Capability, queryNames map[string]bool) (routes, queries []string) {
	seenRoute := map[string]bool{}
	seenQuery := map[string]bool{}
	for _, rel := range ownedSourceFiles(rootFS, c) {
		f, err := rootFS.Open(rel)
		if err != nil {
			continue
		}
		raw, readErr := io.ReadAll(f)
		_ = f.Close()
		if readErr != nil {
			continue
		}
		for _, m := range contractRouteRE.FindAllStringSubmatch(string(raw), -1) {
			// Template sources (the installer's scaffold strings) carry
			// placeholders rather than served paths.
			if strings.Contains(m[1], "{{") {
				continue
			}
			seenRoute[m[1]] = true
		}
		for _, m := range contractQueryRE.FindAllStringSubmatch(string(raw), -1) {
			if queryNames[m[1]] {
				seenQuery["dbgen."+m[1]] = true
			}
		}
	}
	return sortedKeys(seenRoute), sortedKeys(seenQuery)
}

// contractQueryNames collects the query names sqlc generates from
// internal/db/queries/*.sql. A call site can only reach one of these, so the
// intersection is exact — no heuristic for parameter structs or row types.
func contractQueryNames(rootFS *os.Root) map[string]bool {
	out := map[string]bool{}
	entries, err := readDirRecursive(rootFS, "internal/db/queries", func(rel string) bool {
		return strings.HasSuffix(rel, ".sql")
	})
	if err == nil {
		for _, rel := range entries {
			f, openErr := rootFS.Open(rel)
			if openErr != nil {
				continue
			}
			raw, readErr := io.ReadAll(f)
			_ = f.Close()
			if readErr != nil {
				continue
			}
			for _, m := range sqlcNameRE.FindAllStringSubmatch(string(raw), -1) {
				out[m[1]] = true
			}
		}
	}
	return out
}

// ownedSourceFiles expands the unit's dirs and files into repo-relative .go
// sources, skipping generated templ and tests (neither registers a served
// route nor calls the store in production).
func ownedSourceFiles(rootFS *os.Root, c Capability) []string {
	var out []string
	for _, dir := range c.Dirs {
		entries, err := readDirRecursive(rootFS, dir, contractSource)
		if err != nil {
			continue
		}
		out = append(out, entries...)
	}
	for _, f := range c.Files {
		if contractSource(f) {
			out = append(out, f)
		}
	}
	return out
}

// readDirRecursive lists the files under dir that match keep, walking
// subdirectories. Trimmed trees lack some owned dirs; callers treat the error
// as "nothing owned here".
func readDirRecursive(rootFS *os.Root, dir string, keep func(string) bool) ([]string, error) {
	f, err := rootFS.Open(filepath.ToSlash(dir))
	if err != nil {
		return nil, err
	}
	entries, readErr := f.ReadDir(-1)
	_ = f.Close()
	if readErr != nil {
		return nil, readErr
	}
	var out []string
	for _, e := range entries {
		rel := filepath.ToSlash(filepath.Join(dir, e.Name()))
		if e.IsDir() {
			sub, subErr := readDirRecursive(rootFS, rel, keep)
			if subErr == nil {
				out = append(out, sub...)
			}
			continue
		}
		if keep(rel) {
			out = append(out, rel)
		}
	}
	return out, nil
}

// contractSource reports whether rel is a hand-written Go source.
func contractSource(rel string) bool {
	if !strings.HasSuffix(rel, ".go") {
		return false
	}
	return !strings.HasSuffix(rel, "_test.go") && !strings.HasSuffix(rel, "_templ.go")
}

// sortedKeys returns the map keys in sorted order.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
