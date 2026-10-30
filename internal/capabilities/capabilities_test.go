package capabilities

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

var idRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// repoRoot resolves the module root (internal/capabilities -> ../..).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// trimmedPaths reads the scaffold's trim provenance (AGENTS.md,
// removed-paths:) without importing the installer (which would cycle:
// installer imports this package). Paths listed there were deliberately
// deleted at scaffold time, so the registry conformance tests skip them
// instead of reporting a trimmed tree's own removals as missing.
//
// No provenance section (the full template, or a hand-made tree) yields an
// empty set: strict, exactly as before. A "none" value also yields empty.
func trimmedPaths(root string) map[string]bool {
	out := map[string]bool{}
	raw, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		return out
	}
	text := string(raw)
	if !strings.Contains(text, "## Trim provenance") {
		return out
	}
	for line := range strings.SplitSeq(text, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "removed-paths:")
		if !ok {
			continue
		}
		for p := range strings.SplitSeq(strings.TrimSpace(rest), ",") {
			if p = strings.TrimSpace(p); p != "" && p != "none" {
				out[filepath.FromSlash(p)] = true
			}
		}
	}
	return out
}

func TestRegistryIDsUniqueAndWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range All {
		if c.ID == "" {
			t.Errorf("capability missing id: %+v", c)
		}
		if !idRE.MatchString(c.ID) {
			t.Errorf("capability id %q must be kebab-case", c.ID)
		}
		if seen[c.ID] {
			t.Errorf("duplicate capability id %q", c.ID)
		}
		seen[c.ID] = true
		if c.Kind != KindCore && c.Kind != KindPlugin && c.Kind != KindFeature {
			t.Errorf("capability %q has kind %q", c.ID, c.Kind)
		}
		if c.Summary == "" {
			t.Errorf("capability %q missing summary", c.ID)
		}
	}
}

func TestRegistryDependsOnResolves(t *testing.T) {
	byID := ByID()
	for _, c := range All {
		for _, dep := range c.DependsOn {
			if _, ok := byID[dep]; !ok {
				t.Errorf("capability %q depends on unknown %q", c.ID, dep)
			}
		}
	}
}

func TestRegistryCoversPackages(t *testing.T) {
	// Fail fast on drift: every top-level package under internal/ and
	// features/ must be owned by at least one capability, or a new
	// package ships without lifecycle metadata (no installer story,
	// no docs row, no runtime-off answer).
	root := repoRoot(t)
	checkRegistryCoversPackages(t, root)
}

func checkRegistryCoversPackages(t *testing.T, root string) {
	t.Helper()
	trimmed := trimmedPaths(root)
	owned := map[string]bool{}
	for _, c := range All {
		for _, d := range c.Dirs {
			owned[d] = true
			full := filepath.Join(root, filepath.FromSlash(d))
			if st, err := os.Stat(full); err != nil || !st.IsDir() {
				if trimmed[filepath.FromSlash(d)] {
					continue
				}
				t.Errorf("capability %q owns missing dir %q", c.ID, d)
			}
		}
	}
	for _, tree := range []string{"internal", "features"} {
		entries, err := os.ReadDir(filepath.Join(root, tree))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			pkg := tree + "/" + e.Name()
			if !owned[pkg] {
				t.Errorf("package %q owned by no capability — add it to internal/capabilities", pkg)
			}
		}
	}
}

func TestRegistryFilesExist(t *testing.T) {
	// Owned files must exist: a stale Files entry means the installer
	// deletes (or asserts) a path that is already gone.
	root := repoRoot(t)
	checkRegistryFilesExist(t, root)
}

func checkRegistryFilesExist(t *testing.T, root string) {
	t.Helper()
	trimmed := trimmedPaths(root)
	for _, c := range All {
		for _, f := range c.Files {
			full := filepath.Join(root, filepath.FromSlash(f))
			if _, err := os.Stat(full); err != nil {
				if trimmed[filepath.FromSlash(f)] {
					continue
				}
				t.Errorf("capability %q owns missing file %q", c.ID, f)
			}
		}
	}
}

// TestRegistrySkipsTrimmedPaths pins the provenance contract: only paths
// listed under a Trim-provenance removed-paths: line are skipped. The
// integration (a trimmed tree staying green) is proven by scaffolding, not
// here — a fake tree cannot host the full registry without reimplementing it.
func TestRegistrySkipsTrimmedPaths(t *testing.T) {
	dir := t.TempDir()
	if got := trimmedPaths(dir); len(got) != 0 {
		t.Errorf("missing AGENTS.md, want empty skip set, got %v", got)
	}
	// An AGENTS.md without a provenance section is not provenance
	// (the template root itself has none): strict.
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"),
		[]byte("# AGENTS\n\nNo provenance here.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := trimmedPaths(dir); len(got) != 0 {
		t.Errorf("no provenance section, want empty skip set, got %v", got)
	}
	agents := "# AGENTS\n\n## Trim provenance\n\nremoved: genui\n" +
		"removed-paths: features/chat, internal/server/genui_stubs_test.go\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(agents), 0o600); err != nil {
		t.Fatal(err)
	}
	got := trimmedPaths(dir)
	for _, want := range []string{
		filepath.FromSlash("features/chat"),
		filepath.FromSlash("internal/server/genui_stubs_test.go"),
	} {
		if !got[want] {
			t.Errorf("skip set missing %q, got %v", want, got)
		}
	}
	if got["features/todo"] {
		t.Errorf("unlisted path must stay strict, got %v", got)
	}
	// A "none" trim skips nothing (strict, like the full template).
	agentsNone := "# AGENTS\n\n## Trim provenance\n\nremoved: none\nremoved-paths: none\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(agentsNone), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := trimmedPaths(dir); len(got) != 0 {
		t.Errorf("removed-paths: none, want empty skip set, got %v", got)
	}
}

func TestCapabilityUISignalsWired(t *testing.T) {
	// The anti-dead-UI guarantee: a capability claiming UISignal must
	// have the field on todo.Signals AND at least one .templ reading
	// signals.<Field>. Otherwise trimming (or env-off) leaves buttons
	// pointing at gone routes.
	root := repoRoot(t)
	models, err := os.ReadFile(filepath.Join(root, "features", "todo", "models.go"))
	if err != nil {
		t.Fatal(err)
	}
	var templFiles []string
	collect := func(tree string) {
		err := filepath.WalkDir(filepath.Join(root, tree), func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(p, ".templ") {
				templFiles = append(templFiles, p)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	collect("features")
	for _, c := range All {
		if c.UISignal == "" {
			continue
		}
		if !strings.Contains(string(models), c.UISignal+" ") &&
			!strings.Contains(string(models), c.UISignal+"\t") {
			t.Errorf("capability %q claims UISignal %q missing from todo.Signals",
				c.ID, c.UISignal)
			continue
		}
		ref := "signals." + c.UISignal
		found := ""
		for _, f := range templFiles {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), ref) {
				found = f
				break
			}
		}
		if found == "" {
			t.Errorf("capability %q claims UISignal %q read by no .templ", c.ID, ref)
		}
	}
}

func TestKindsFollowServantPrinciple(t *testing.T) {
	// Pins the taxonomy against casual relabeling: plugins serve other
	// capabilities (dagnats→onboarding, credits→metering, sounds→
	// pages, nats/llm/collab→their consumers); features are terminal
	// user surfaces (pages/journeys). Directories do not decide:
	// features/credits is a plugin, features/todo is a feature.
	pluginIDs := []string{
		"nats", "dagnats", "goakt", "llm", "genkit", "collab", "datastar", "components",
		"credits", "sounds", "offline-sync",
		"capabilities", "genui",
	}
	featureIDs := []string{"todo", "whiteboard", "landing", "config-view", "room", "review"}
	byID := ByID()
	for _, id := range pluginIDs {
		c, ok := byID[id]
		if !ok {
			t.Errorf("capability %q missing from registry", id)
			continue
		}
		if c.Kind != KindPlugin {
			t.Errorf("capability %q is %q, want plugin (it serves other capabilities)", id, c.Kind)
		}
	}
	for _, id := range featureIDs {
		c, ok := byID[id]
		if !ok {
			t.Errorf("capability %q missing from registry", id)
			continue
		}
		if c.Kind != KindFeature {
			t.Errorf("capability %q is %q, want feature (it is a terminal user surface)", id, c.Kind)
		}
	}
}

func TestOfferedSetMatchesInstaller(t *testing.T) { // The installer offers exactly these units; the registry is the
	// source of truth both sides check.
	want := []string{
		"dagnats", "goakt", "whiteboard", "landing", "config-view",
		"credits", "sounds", "room", "genui", "review", "search",
	}
	byID := ByID()
	got := slices.Clone(OfferedIDs())
	slices.Sort(got)
	wantSorted := slices.Clone(want)
	slices.Sort(wantSorted)
	if !slices.Equal(got, wantSorted) {
		t.Errorf("OfferedIDs = %v, want set %v", OfferedIDs(), want)
	}
	for _, id := range want {
		c, ok := byID[id]
		if !ok || !c.Class().Offered() {
			t.Errorf("capability %q must exist and be offered", id)
		}
	}
}

// TestScaffoldClassIsCoherent pins the two ends of the class: core is never
// offered, a non-core unit is never core, and every opt-in is a real
// non-core capability (the scaffold prompt lists it as an extra).
func TestScaffoldClassIsCoherent(t *testing.T) {
	for _, c := range All {
		if c.Kind == KindCore && c.Class() != ScaffoldCore {
			t.Errorf("core capability %q has scaffold %q, want core", c.ID, c.Class())
		}
		if c.Kind != KindCore && c.Class() == ScaffoldCore {
			t.Errorf("non-core capability %q is scaffold-core", c.ID)
		}
		if c.Class() == ScaffoldOptIn && c.Kind == KindCore {
			t.Errorf("core capability %q is opt-in", c.ID)
		}
	}
}

// TestRegistryCoversDagnatsImports is the guard for a trim that leaves a
// dangling import.
//
// Trimming a unit deletes its Dirs and Files. If a file that imports one of
// those packages is NOT listed, it survives the trim and the proof build fails
// with `module .../internal/dagnats: not found` — which is exactly what
// happened: onboarding_lifecycle_test.go imports internal/dagnats but was
// missing from the dagnats unit's Files, breaking `gogogo --trim dagnats`.
//
// The check is narrow and mechanical: for each unit owning a dir, no file that
// imports that dir's package may live outside the unit's Files. It is scoped to
// internal/dagnats today because that is the only dir whose package is imported
// by files that a trim does not obviously own; widening it needs the import
// graph, not a substring scan.
func TestRegistryCoversDagnatsImports(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rootFS.Close() }()

	// The one package that trim units delete wholesale.
	const pkgDir = "internal/dagnats"
	const importPath = "github.com/calionauta/gogogo/internal/dagnats"

	// This test file necessarily names the import path to do its job; it is not
	// part of the shipped tree the trim proof builds.
	self := "internal/capabilities/capabilities_test.go"

	// The unit individual what owns pkgDir, and the set of files it deletes:
	// the Files list plus everything under its Dirs (a whole deleted
	// directory needs no per-file entry).
	var owned map[string]bool
	var ownedDirs []string
	for _, c := range All {
		if !slices.Contains(c.Dirs, pkgDir) {
			continue
		}
		owned = make(map[string]bool, len(c.Files))
		for _, f := range c.Files {
			owned[f] = true
		}
		for _, d := range c.Dirs {
			if d != pkgDir {
				ownedDirs = append(ownedDirs, d+"/")
			}
		}
	}
	if owned == nil {
		t.Fatalf("no capability owns %s — the dagnats unit changed shape", pkgDir)
	}

	// Scan the trees a trimmed checkout keeps: the unit's own imports can
	// appear anywhere in cmd/, features/, internal/ or router/.
	var offenders []string
	for _, tree := range []string{"cmd", "features", "internal"} {
		walkErr := filepath.WalkDir(filepath.Join(root, tree), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			if strings.HasPrefix(rel, pkgDir+"/") {
				return nil // the package itself goes with the unit
			}
			for _, d := range ownedDirs {
				if strings.HasPrefix(rel, d) {
					return nil // a sibling deleted directory goes with it
				}
			}
			// root-scoped read (os.Root), not os.ReadFile(path): the skill
			// requires os.OpenRoot for a walk into a tree not fully under our
			// control, and gosec G122 flags the race-prone form.
			f, openErr := rootFS.Open(rel)
			if openErr != nil {
				return openErr
			}
			body, readErr := io.ReadAll(f)
			_ = f.Close()
			if readErr != nil {
				return readErr
			}
			if rel == self {
				return nil
			}
			if strings.Contains(string(body), `"`+importPath+`"`) && !owned[rel] {
				offenders = append(offenders, rel)
			}
			return nil
		})
		if walkErr != nil {
			t.Fatal(walkErr)
		}
	}

	if len(offenders) > 0 {
		t.Errorf("%s is trimmed by a unit, but these files import it and are NOT "+
			"in that unit's Files — trimming leaves a dangling import and fails the "+
			"proof build:\n  %s",
			pkgDir, strings.Join(offenders, "\n  "))
	}
}

// TestNonCoreUnitsExplainTheirRemoval is the "no silent capability" rule: a
// unit that is not core must say what disappears with it — either it is an
// installer unit (the trim proof speaks for it) or it carries the reason/
// note the reader needs. The scaffolded feature ships that reason, so a
// generated unit cannot land without a removal story.
func TestNonCoreUnitsExplainTheirRemoval(t *testing.T) {
	for _, c := range All {
		if c.Kind == KindCore {
			continue
		}
		if c.Class().Offered() || c.Reason != "" || c.Note != "" {
			continue
		}
		t.Errorf("capability %q (%s) explains neither how it is removed nor what is lost", c.ID, c.Kind)
	}
}

// TestCoreUnitsAreNeverOffered is the other half: core means "customize,
// never remove", so offering it to the installer would contradict the class
// (and let a scaffold delete its own auth or database).
func TestCoreUnitsAreNeverOffered(t *testing.T) {
	for _, c := range All {
		if c.Kind == KindCore && c.Class().Offered() {
			t.Errorf("core capability %q is offered for trimming — core means never remove", c.ID)
		}
	}
}
