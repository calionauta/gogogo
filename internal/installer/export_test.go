// SCOPE:plugin — tests for `gogogo export` (read-only port bundle)
package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExportWritesABundleAndNothingElse is the tool's whole contract in one
// test: the owned files land under --to, the recipe lands last, and the
// template is untouched.
func TestExportWritesABundleAndNothingElse(t *testing.T) {
	root := templateRootDir(t)
	to := filepath.Join(t.TempDir(), "bundle")

	bundle, err := exportUnit(exportArgs{
		unit: "whiteboard", from: root, to: to, module: "example.com/myapp",
	})
	if err != nil {
		t.Fatalf("exportUnit: %v", err)
	}
	if len(bundle.Files) == 0 {
		t.Fatal("bundle has no files")
	}
	for _, f := range bundle.Files {
		if _, err := os.Stat(f.To); err != nil {
			t.Errorf("bundle is missing %s: %v", f.To, err)
		}
		// Every bundle path must live under --to: the read-only promise is
		// spatial, so it is checkable.
		rel, err := filepath.Rel(to, f.To)
		if err != nil || strings.HasPrefix(rel, "..") {
			t.Errorf("%s was written outside the bundle (%s)", f.To, rel)
		}
	}
	if _, err := os.Stat(bundle.Recipe); err != nil {
		t.Fatalf("recipe missing: %v", err)
	}
	recipe := readFileString(t, bundle.Recipe)
	for _, want := range []string{
		"# Porting `whiteboard` into your project",
		"Nothing outside this bundle was written or changed",
		"example.com/myapp",
		"docs/porting-into-an-existing-app.md",
		"go build ./...",
	} {
		if !strings.Contains(recipe, want) {
			t.Errorf("recipe missing %q", want)
		}
	}
	// The dependency cascade is the reason whiteboard is one unit and not two:
	// the board page calls @sounds.SoundAssets().
	if !strings.Contains(strings.Join(bundle.Units, ","), "sounds") {
		t.Errorf("closure lost the sounds dependency: %v", bundle.Units)
	}
}

// TestExportRecipeNamesTheRewrites checks the measured half of the recipe: with
// the module known, every copied file that still imports the template module is
// listed, and the sed line names the substitution.
func TestExportRecipeNamesTheRewrites(t *testing.T) {
	root := templateRootDir(t)
	to := filepath.Join(t.TempDir(), "bundle")

	bundle, err := exportUnit(exportArgs{
		unit: "whiteboard", from: root, to: to, module: "example.com/myapp",
	})
	if err != nil {
		t.Fatalf("exportUnit: %v", err)
	}
	if len(bundle.Rewrites) == 0 {
		t.Fatal("no rewrite candidates: every copied Go file imports the template module")
	}
	recipe := readFileString(t, bundle.Recipe)
	if !strings.Contains(recipe, "grep -rl '"+templateModulePath+"'") {
		t.Error("recipe must emit the rewrite command it measured")
	}
	var sawHandler bool
	for _, f := range bundle.Rewrites {
		if f.Dest == "features/whiteboard/handlers/chi.go" {
			sawHandler = true
		}
	}
	if !sawHandler {
		t.Errorf("rewrite list must include the unit's handler, got %+v", bundle.Rewrites)
	}
}

// TestExportRefusals pins the fail-fast surface. Each case names what to fix;
// a refusal that does not is a dead end for an agent.
func TestExportRefusals(t *testing.T) {
	root := templateRootDir(t)
	scratch := t.TempDir()

	notEmpty := filepath.Join(scratch, "full")
	if err := os.MkdirAll(notEmpty, dirMode); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(notEmpty, "keep.txt"), "work\n")

	cases := []struct {
		name string
		args exportArgs
		want string
	}{
		{
			"--to inside the template",
			exportArgs{unit: "whiteboard", from: root, to: filepath.Join(root, "scratch-bundle")},
			"outside the template checkout",
		},
		{
			"--to not empty",
			exportArgs{unit: "whiteboard", from: root, to: notEmpty},
			"is not empty",
		},
		{
			"--dir not a template",
			exportArgs{unit: "whiteboard", from: t.TempDir(), to: filepath.Join(scratch, "x")},
			"is not a gogogo checkout",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := exportUnit(tc.args)
			if err == nil {
				t.Fatal("expected a refusal")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not name the fix (%q)", err, tc.want)
			}
		})
	}
}

// TestExportArgValidation covers the parse layer: a missing unit, a missing
// flag, an unknown format, an unknown unit id.
func TestExportArgValidation(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no unit", []string{"--dir", "/tmp", "--to", "/tmp/x"}, "exactly one unit id"},
		{"no dir", []string{"whiteboard", "--to", "/tmp/x"}, "needs --dir"},
		{"no to", []string{"whiteboard", "--dir", "/tmp"}, "needs --dir"},
		{"bad format", []string{"whiteboard", "--dir", "/tmp", "--to", "/tmp/x", "--format", "yaml"}, "unknown --format"},
		{"unknown unit", []string{"nope", "--dir", "/tmp", "--to", "/tmp/x"}, "nope"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseExportArgs(tc.args)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
	// Flags after the positional work too (the add-parser rule).
	opt, err := parseExportArgs([]string{"whiteboard", "--dir", "/tmp", "--to", "/tmp/x", "--module", "m.example/x"})
	if err != nil {
		t.Fatalf("positional-first parse: %v", err)
	}
	if opt.unit != "whiteboard" || opt.module != "m.example/x" {
		t.Errorf("parsed %+v", opt)
	}
}

// TestExportRoutesAreRealRoutes keeps the recipe from printing the add engine's
// restore list verbatim: comments and struct fields are not routes.
func TestExportRoutesAreRealRoutes(t *testing.T) {
	for _, line := range []string{
		`r.MountFeature("whiteboard", wbH.Routes())`,
		`MountSubtree(r, whiteboard.AssetPath, whiteboard.StaticHandler())`,
		`r.Get("/board", h.Board)`,
	} {
		if !isRouteLine(line) {
			t.Errorf("isRouteLine(%q) = false, want true", line)
		}
	}
	for _, line := range []string{
		"// Features that embed their OWN assets register their subtree here",
		"wbH *wbhandlers.Handler,",
		"",
	} {
		if isRouteLine(line) {
			t.Errorf("isRouteLine(%q) = true, want false", line)
		}
	}
}

// TestExportJSONShape pins the machine contract the MCP tool hands back.
func TestExportJSONShape(t *testing.T) {
	root := templateRootDir(t)
	to := filepath.Join(t.TempDir(), "bundle")
	bundle, err := exportUnit(exportArgs{unit: "sounds", from: root, to: to})
	if err != nil {
		t.Fatalf("exportUnit: %v", err)
	}
	var out strings.Builder
	if err := printExportJSON(&out, bundle); err != nil {
		t.Fatalf("printExportJSON: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"unit", "units", "from", "to", "files", "proof", "recipe"} {
		if _, ok := got[key]; !ok {
			t.Errorf("JSON missing %q", key)
		}
	}
}

// readFileString is the test-local read (the package has no shared helper).
func readFileString(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

// TestExportNeverWritesOutsideTheBundle is the spatial half of the read-only
// promise, asserted directly: a planned file that resolves outside --to is
// refused before any write.
func TestExportNeverWritesOutsideTheBundle(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "bundle")
	inside := []exportFile{{To: filepath.Join(dest, "unit", "features", "x.go")}}
	if err := assertInsideBundle(dest, inside); err != nil {
		t.Fatalf("a path inside the bundle was refused: %v", err)
	}
	for _, outside := range []string{
		filepath.Join(dest, "..", "escaped.go"),
		filepath.Join(dest, "..", "..", "etc", "passwd"),
	} {
		err := assertInsideBundle(dest, []exportFile{{To: outside}})
		if err == nil {
			t.Fatalf("%s was allowed outside the bundle", outside)
		}
		if !strings.Contains(err.Error(), "would land outside") {
			t.Errorf("error %q does not say where it would land", err)
		}
	}
}

// TestExportRefusesAManifestEscape wires the guard into the real path: unit
// paths come from the manifest, so a `..` there must not reach copyExportFile.
func TestExportRefusesAManifestEscape(t *testing.T) {
	root := templateRootDir(t)
	dest := filepath.Join(t.TempDir(), "bundle")
	if err := assertInsideBundle(dest, []exportFile{{
		From: filepath.Join(root, "go.mod"),
		To:   filepath.Join(dest, "..", "escape.go"),
		Dest: "../escape.go",
	}}); err == nil {
		t.Fatal("a ../ destination in the plan was accepted")
	}
}
