package capabilities

import (
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
	owned := map[string]bool{}
	for _, c := range All {
		for _, d := range c.Dirs {
			owned[d] = true
			full := filepath.Join(root, filepath.FromSlash(d))
			if st, err := os.Stat(full); err != nil || !st.IsDir() {
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
	for _, c := range All {
		for _, f := range c.Files {
			full := filepath.Join(root, filepath.FromSlash(f))
			if _, err := os.Stat(full); err != nil {
				t.Errorf("capability %q owns missing file %q", c.ID, f)
			}
		}
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
	collect(filepath.Join("web", "skins"))
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
	// capabilities (dagnats→onboarding, credits→metering, sounds/skins→
	// pages, nats/llm/collab→their consumers); features are terminal
	// user surfaces (pages/journeys). Directories do not decide:
	// features/credits is a plugin, features/todo is a feature.
	pluginIDs := []string{
		"nats", "dagnats", "llm", "collab", "datastar", "components",
		"credits", "sounds", "skins", "entity-store", "offline-sync",
		"capabilities",
	}
	featureIDs := []string{"todo", "whiteboard", "landing", "config-view"}
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
	// source of truth both sides check. skins-extra maps to skins.
	want := []string{
		"dagnats", "whiteboard", "landing", "config-view",
		"credits", "sounds", "skins",
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
		if !ok || !c.Offered {
			t.Errorf("capability %q must exist and be offered", id)
		}
	}
}
