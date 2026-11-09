// Recipe version capture and the outdated verdict: install records the
// manifest/entry version in the receipt, outdated compares it with the
// catalog, and unknown stays unknown instead of guessing.
package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/recipe"
)

// newVersionedRecipeDir writes a recipe directory whose manifest carries a
// version. The declaration matches the one file it ships.
func newVersionedRecipeDir(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := "id: pomodoro\nversion: " + version + "\n" +
		"upstream:\n  repo: example.com/recipes\n  ref: " + version + "\n" +
		"declaration:\n  routes:\n    - {method: GET, pattern: /pomodoro}\n"
	writeFile(t, filepath.Join(dir, recipeManifestFile), manifest)
	writeFile(t, filepath.Join(dir, "features/pomodoro/handlers/chi.go"), recipeHandlerV1)
	return dir
}

// writeCatalogEntry commits one reviewed entry at <catalog>/<id>/<version>.yaml.
func writeCatalogEntry(t *testing.T, catalogDir, version string) {
	t.Helper()
	hash := recipe.Hash([]byte("catalog bytes"))
	body := "id: pomodoro\nversion: " + version + "\n" +
		"source:\n  repo: example.com/recipes\n  ref: " + version + "\n" +
		"files:\n  features/pomodoro/handlers/chi.go: sha256:" + hash + "\n" +
		"license: MIT\nminCore: v0.56.0\nupstreamDeps: []\n" +
		"reviewedAt: \"2026-11-14\"\nreviewedBy: calionauta\nreviewedAgainst: 3f8a1c2\n"
	writeFile(t, filepath.Join(catalogDir, "pomodoro", version+".yaml"), body)
}

// installVersioned installs the versioned pomodoro into a fresh checkout.
func installVersioned(t *testing.T, version string) string {
	t.Helper()
	root := newRecipeCheckout(t)
	src := newVersionedRecipeDir(t, version)
	if out, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	return root
}

func TestRecipeInstallRecordsVersion(t *testing.T) {
	root := installVersioned(t, "v1.2.0")
	rec, lerr := recipe.LoadReceipt(root, "pomodoro")
	if lerr != nil {
		t.Fatalf("receipt: %v", lerr)
	}
	if rec.Version != "v1.2.0" {
		t.Fatalf("receipt version = %q, want the manifest's", rec.Version)
	}
}

func TestRecipeInstallFromCatalogRecordsVersion(t *testing.T) {
	root := newRecipeCheckout(t)
	entry, contents := newCatalogFixture(t, catalogFiles())
	if out, err := runRecipeCLI(t, "install", "--entry", entry, contents, "--dir", root, "--yes"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	rec, lerr := recipe.LoadReceipt(root, "pomodoro")
	if lerr != nil {
		t.Fatalf("receipt: %v", lerr)
	}
	if rec.Version != "v1.2.0" {
		t.Fatalf("receipt version = %q, want the entry's", rec.Version)
	}
}

func TestRecipeInstallRejectsBadVersion(t *testing.T) {
	root := newRecipeCheckout(t)
	src := newVersionedRecipeDir(t, "1.2")
	if _, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes"); err == nil ||
		!strings.Contains(err.Error(), "must be vMAJOR.MINOR.PATCH") {
		t.Fatalf("version 1.2 must be refused, got %v", err)
	}
}

func TestRecipeOutdatedReportsBehindAndExits3(t *testing.T) {
	root := installVersioned(t, "v1.2.0")
	writeCatalogEntry(t, filepath.Join(root, "catalog"), "v1.3.0")

	out, err := runRecipeCLI(t, "outdated", "--dir", root)
	if ExitCode(err) != 3 {
		t.Fatalf("a behind verdict must exit 3, got %v\n%s", err, out)
	}
	for _, want := range []string{"pomodoro", "v1.2.0", "v1.3.0", "behind", "exit 3"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output must contain %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "v0.56.0") {
		t.Fatalf("the latest version's minCore must reach the operator:\n%s", out)
	}
}

func TestRecipeOutdatedReportsCurrent(t *testing.T) {
	root := installVersioned(t, "v1.3.0")
	writeCatalogEntry(t, filepath.Join(root, "catalog"), "v1.3.0")

	out, err := runRecipeCLI(t, "outdated", "--dir", root)
	if err != nil {
		t.Fatalf("a current verdict must not exit non-zero, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "current") {
		t.Fatalf("output must say current:\n%s", out)
	}
}

func TestRecipeOutdatedUnknownWithoutCatalog(t *testing.T) {
	root := installVersioned(t, "v1.2.0")

	out, err := runRecipeCLI(t, "outdated", "--dir", root)
	if err != nil {
		t.Fatalf("a missing catalog is unknown, not failure, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "no catalog entry to compare") {
		t.Fatalf("output must be honest about the missing catalog:\n%s", out)
	}
	// The status field still has to say unknown: a known install with no
	// catalog to compare against is not "current".
	jout, jerr := runRecipeCLI(t, "outdated", "--dir", root, "--format", "json")
	if jerr != nil {
		t.Fatalf("json: %v\n%s", jerr, jout)
	}
	var rep outdatedReport
	if uerr := json.Unmarshal([]byte(jout), &rep); uerr != nil {
		t.Fatalf("json: %v", uerr)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != "unknown" {
		t.Fatalf("status must be unknown without a catalog, got %+v", rep)
	}
}

func TestRecipeOutdatedUnknownInstalledVersion(t *testing.T) {
	root := newRecipeCheckout(t)
	src := newRecipeDir(t, "v1", map[string]string{
		"features/pomodoro/handlers/chi.go": recipeHandlerV1,
	})
	if _, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes"); err != nil {
		t.Fatal(err)
	}
	writeCatalogEntry(t, filepath.Join(root, "catalog"), "v1.3.0")

	out, err := runRecipeCLI(t, "outdated", "--dir", root)
	if err != nil {
		t.Fatalf("an unversioned install is unknown, not failure: %v\n%s", err, out)
	}
	if !strings.Contains(out, "installed without a version") {
		t.Fatalf("output must say the install has no version:\n%s", out)
	}
}

func TestRecipeOutdatedJSON(t *testing.T) {
	root := installVersioned(t, "v1.2.0")
	writeCatalogEntry(t, filepath.Join(root, "catalog"), "v1.3.0")
	writeCatalogEntry(t, filepath.Join(root, "catalog"), "v1.2.0")

	out, err := runRecipeCLI(t, "outdated", "--dir", root, "--format", "json")
	if ExitCode(err) != 3 {
		t.Fatalf("the JSON path must exit 3 when behind, got %v", err)
	}
	var rep outdatedReport
	if uerr := json.Unmarshal([]byte(out), &rep); uerr != nil {
		t.Fatalf("outdated --format json is not JSON: %v\n%s", uerr, out)
	}
	if rep.Checked != 1 || rep.Outdated != 1 || len(rep.Results) != 1 {
		t.Fatalf("unexpected report: %+v", rep)
	}
	row := rep.Results[0]
	if row.ID != "pomodoro" || row.Installed != "v1.2.0" || row.Latest != "v1.3.0" || row.Status != "behind" {
		t.Fatalf("unexpected row: %+v", row)
	}
	if row.MinCore != "v0.56.0" {
		t.Fatalf("minCore lost: %+v", row)
	}
}

func TestRecipeStatusShowsVersionLine(t *testing.T) {
	root := installVersioned(t, "v1.2.0")
	writeCatalogEntry(t, filepath.Join(root, "catalog"), "v1.3.0")

	out, err := runRecipeCLI(t, "status", "--dir", root)
	if err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	if !strings.Contains(out, "version v1.2.0 -> v1.3.0 behind") {
		t.Fatalf("status must carry the version verdict line:\n%s", out)
	}
}

func TestRecipeStatusJSONCarriesVersionVerdict(t *testing.T) {
	root := installVersioned(t, "v1.2.0")
	writeCatalogEntry(t, filepath.Join(root, "catalog"), "v1.3.0")

	out, err := runRecipeCLI(t, "status", "--dir", root, "--format", "json")
	if err != nil {
		t.Fatalf("status json: %v\n%s", err, out)
	}
	var rep struct {
		Version string `json:"version"`
		Latest  string `json:"latest"`
		Status  string `json:"status"`
	}
	if uerr := json.Unmarshal([]byte(out), &rep); uerr != nil {
		t.Fatalf("status json: %v", uerr)
	}
	if rep.Version != "v1.2.0" || rep.Latest != "v1.3.0" || rep.Status != "behind" {
		t.Fatalf("status json lost the verdict: %+v", rep)
	}
}

func TestRecipeDescribeShowsVersion(t *testing.T) {
	root := installVersioned(t, "v1.2.0")
	exts, err := recipeDescriptors(root)
	if err != nil {
		t.Fatalf("descriptors: %v", err)
	}
	found := false
	for _, e := range exts {
		if e.ID == "pomodoro" {
			found = true
			if e.Version != "v1.2.0" {
				t.Fatalf("descriptor version = %q, want the receipt's", e.Version)
			}
		}
	}
	if !found {
		t.Fatal("pomodoro descriptor not found")
	}
}

// TestLatestEntryPicksTheHighestSemver pins the ordering that picks what
// "latest" means: v1.10.0 outranks v1.2.0, which lexicographic order would miss.
func TestLatestEntryPicksTheHighestSemver(t *testing.T) {
	entries := []recipe.CatalogEntry{
		{ID: "pomodoro", Version: "v1.2.0"},
		{ID: "pomodoro", Version: "v1.10.0"},
		{ID: "pomodoro", Version: "v1.3.0"},
	}
	got, ok := latestEntry(entries)
	if !ok || got.Version != "v1.10.0" {
		t.Fatalf("latest must be v1.10.0, got %+v %v", got, ok)
	}
}

// TestRecipeInstallPreservesTheCatalogFiles ensures the receipt version
// travels with the installed bytes (no other recorded state moves with it).
// TestRecipeOutdatedOneID pins the single-recipe shape: only the named
// receipt is compared, even when another installed recipe is behind.
func TestRecipeOutdatedOneID(t *testing.T) {
	root := installVersioned(t, "v1.2.0")
	writeCatalogEntry(t, filepath.Join(root, "catalog"), "v1.3.0")

	out, err := runRecipeCLI(t, "outdated", "pomodoro", "--dir", root)
	if ExitCode(err) != 3 {
		t.Fatalf("one behind recipe must exit 3, got %v\n%s", err, out)
	}
	if strings.Count(out, "recipe pomodoro:") != 1 {
		t.Fatalf("a single id must render exactly one verdict line:\n%s", out)
	}

	out, err = runRecipeCLI(t, "outdated", "missing", "--dir", root)
	if err == nil || ExitCode(err) != 1 || !strings.Contains(err.Error(), "recipe outdated:") {
		t.Fatalf("an unknown id must refuse naming outdated, got %v\n%s", err, out)
	}
}

// TestScanErrorNamesTheCallingCommand pins the fix: a broken catalog scanned
// from status refuses as "recipe status:", never as the wrong command.
func TestScanErrorNamesTheCallingCommand(t *testing.T) {
	root := installVersioned(t, "v1.2.0")
	catalog := filepath.Join(root, "catalog", "pomodoro")
	if err := os.MkdirAll(catalog, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(catalog, "v0.1.0.yaml"), []byte("id: pomodoro\nversion: v0.1.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := runRecipeCLI(t, "status", "--dir", root); err == nil ||
		!strings.Contains(err.Error(), "recipe status:") {
		t.Fatalf("a broken catalog must fail naming status, got %v", err)
	}
	if _, err := runRecipeCLI(t, "outdated", "--dir", root); err == nil ||
		!strings.Contains(err.Error(), "recipe outdated:") {
		t.Fatalf("a broken catalog must fail naming outdated, got %v", err)
	}
}

func TestRecipeInstallPreservesTheCatalogFiles(t *testing.T) {
	root := installVersioned(t, "v1.2.0")
	if _, err := os.Stat(filepath.Join(root, "features/pomodoro/handlers/chi.go")); err != nil {
		t.Fatal("installed bytes missing — the version write must not eat the install")
	}
}
