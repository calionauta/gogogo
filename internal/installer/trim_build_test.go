// SCOPE:plugin — installer engine: proves a trimmed tree still builds.
//
// The other installer tests verify the SHAPE of a trim — that markers were
// found, that the right lines went away. None of them answers the only
// question that matters to a user running `gogogo trim goakt`: does the
// result compile? A strip rule that matches (so the receipt looks perfect)
// but leaves a dangling reference passes every existing test and produces a
// checkout that does not build.
//
// This test copies the real repo, trims one unit, and builds the tree. It is
// the reason a new strip rule can be written with confidence instead of
// hope.
package installer

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// skipInCopy are paths that must not land in the temp copy: VCS internals,
// JS dependencies, the 127 MB prebuilt desktop binary, and the transient
// dirs a scratch checkout has no use for.
var skipInCopy = map[string]bool{
	".git": true, "node_modules": true, "tmp": true, "data": true,
	"desktop": true, "coverage.out": true,
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("runtime.Caller unavailable")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// copyRepoForTrim mirrors the checkout into dir, skipping skipInCopy. A
// symlink is copied as a symlink rather than followed, so a large linked
// tree cannot balloon the copy or fail on a broken link.
func copyRepoForTrim(t *testing.T, dir string) {
	t.Helper()
	root := repoRoot(t)
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		if skipInCopy[d.Name()] && d.IsDir() {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dir, rel), 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		copyOneFile(t, path, filepath.Join(dir, rel))
		return nil
	})
	if walkErr != nil {
		t.Fatalf("copy repo: %v", walkErr)
	}
}

func copyOneFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open %s: %v", src, err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create %s: %v", dst, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy %s: %v", src, err)
	}
}

// trimAndRegenerate copies the checkout, trims one unit, and runs the code
// generators the installer's prove step runs (templ, because .templ files are
// edited in place). Without the regenerate the test would flag stale
// *_templ.go files — exactly the false positive the older import test excluded
// by hand.
//
// The receipt is asserted on: a strip rule whose start marker no longer
// matches is a FAILURE here, not the warning the installer prints. That is the
// difference between drift being caught and drift being reported to nobody.
func trimAndRegenerate(t *testing.T, u trimUnit) string {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	copyRepoForTrim(t, dir)
	rc := &Receipt{}
	if err := applyTrim(dir, []trimUnit{u}, rc); err != nil {
		t.Fatalf("trim: %v", err)
	}
	for _, unit := range rc.Units {
		if len(unit.StripsMissed) > 0 {
			t.Errorf("unit %q has %d strip rule(s) that matched nothing — "+
				"the source drifted from the manifest:\n  %s",
				unit.ID, len(unit.StripsMissed), strings.Join(unit.StripsMissed, "\n  "))
		}
		if unit.StripsApplied == 0 && len(unit.StripsMissed) == 0 {
			// Not an error: a unit may legitimately own only dirs/files.
			t.Logf("unit %q applied no strips (dirs/files only)", unit.ID)
		}
	}
	for _, step := range [][]string{
		{"go", "tool", "templ", "generate"},
		{"gofmt", "-w", "."},
	} {
		//nolint:gosec // step is a literal from this file, not caller input.
		cmd := exec.CommandContext(ctx, step[0], step[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s failed in trimmed tree:\n%s", strings.Join(step, " "), out)
		}
	}
	return dir
}

// TestTrimmedTreeStillBuilds is the gate that makes every strip rule in this
// package trustworthy. Run it after adding a unit's rules — not before, and
// not instead. It answers the only question that matters to a user running
// `gogogo trim goakt`: does the result compile?
func TestTrimmedTreeStillBuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("copies and builds the whole repo per unit")
	}
	for _, u := range manifestUnits {
		t.Run(u.id, func(t *testing.T) {
			dir := trimAndRegenerate(t, u)
			// vet, not build: `go build ./...` ignores _test.go, and the
			// composition test is exactly the file a trim is most likely to
			// leave broken. vet compiles tests without running them.
			cmd := exec.CommandContext(context.Background(), "go", "vet", "./...")
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("trimmed tree does not vet:\n%s", out)
			}
			// api-routes, not just vet: a trim can drop a route registration
			// while leaving everything compiling (a mux.Handle line is not
			// an import), and the result 404s at runtime. The same contract
			// test CI runs on the full tree runs here against the trimmed
			// copy — no parallel reimplementation that could disagree with
			// it (see bin/check-api-routes.sh).
			cmd = exec.CommandContext(context.Background(),
				"go", "test", "./internal/capabilities/",
				"-run", "TestTemplAPIURLsHaveRoutes", "-count=1")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("trimmed tree lost a route:\n%s", out)
			}
		})
	}
}

// TestTrimmedTreeRouteGateCatchesLoss proves the negative: the api-routes
// step above must FAIL when a template fires a route nothing registers, or
// it is green forever (the same reason TestRouteCoversMatcher exists for
// the matcher). Trims sounds (the smallest unit), plants a probe template
// firing a bogus endpoint, and requires the contract test to report it.
//
// Deliberately NOT a mux.Handle victim: deleting a mux line for a
// remaining feature does not break this gate, because the feature's own
// package still declares the same /api/* literal and the scanner counts
// literals, not mux wiring. A mux line lost while its package survives is
// a runtime 404 the source scan cannot see (documented residual); what this
// gate catches is the stale-ref direction — a template firing an endpoint
// no Go source names at all.
func TestTrimmedTreeRouteGateCatchesLoss(t *testing.T) {
	if testing.Short() {
		t.Skip("copies the whole repo")
	}
	dir := trimAndRegenerate(t, findUnit(t, "sounds"))
	const probe = "features/todo/components/zz_trim_probe.templ"
	body := "package components\n\ntempl TrimProbe() {\n" +
		"\t<span data-trim-probe=\"'/api/bogus-trim-probe'\" class=\"hidden\">probe</span>\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(probe)), []byte(body), 0o600); err != nil {
		t.Fatalf("write probe: %v", err)
	}
	cmd := exec.CommandContext(context.Background(),
		"go", "test", "./internal/capabilities/",
		"-run", "TestTemplAPIURLsHaveRoutes", "-count=1")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("route gate stayed green with an unregistered /api ref:\n%s", out)
	}
	// The gate must fail ON the probe, not on collateral (a generate or
	// compile error in the copy would also be red, and would prove nothing
	// about route coverage).
	if !strings.Contains(string(out), "/api/bogus-trim-probe") {
		t.Fatalf("route gate failed, but not on the probe ref:\n%s", out)
	}
}

// TestTrimmedTreeHasNoDanglingImports is the cheap half of the same proof: no
// build, so it runs in -short mode, and it names the offending file and
// package. The build test above is what makes the pair more than a heuristic.
func TestTrimmedTreeHasNoDanglingImports(t *testing.T) {
	for _, u := range manifestUnits {
		t.Run(u.id, func(t *testing.T) {
			dir := trimAndRegenerate(t, u)
			pkgs := unitImportPaths(u)
			walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
					return err
				}
				rel, relErr := filepath.Rel(dir, path)
				if relErr != nil {
					return relErr
				}
				rel = filepath.ToSlash(rel)
				if unitCoversPath(u, u.meta(), rel) {
					return nil
				}
				hits, impErr := fileImportsOwn(path, pkgs)
				if impErr != nil {
					return impErr
				}
				for _, h := range hits {
					t.Errorf("%s still imports %s after trim", rel, h)
				}
				return nil
			})
			if walkErr != nil {
				t.Fatal(walkErr)
			}
		})
	}
}

// unitImportPaths is the package set a unit owns, derived from its dirs.
// Extracted from checkUnitTrimImports so both tests agree on the answer.
func unitImportPaths(u trimUnit) []string {
	m := u.meta()
	var pkgs []string
	for _, d := range m.dirs {
		if strings.HasPrefix(d, "internal/") || strings.HasPrefix(d, "features/") {
			pkgs = append(pkgs, "github.com/calionauta/gogogo/"+d)
		}
	}
	return pkgs
}
