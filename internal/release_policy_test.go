// SCOPE:core — Release versioning policy: this repo is pre-1.0 and stays there
// until a human says otherwise.
//
// release-please maps a breaking change (`feat!:`, a `BREAKING CHANGE:` footer)
// to a MAJOR bump. With `bump-minor-pre-major: false` that means one `!` in a
// commit subject opens a `release 1.0.0` PR, which is exactly what happened: a
// `feat(ui)!:` for one shell became a 1.0.0 proposal. 1.0.0 is a product
// statement (API stability, support window, the whole promise), never a side
// effect of a commit marker, so the mapping is pinned here.
//
// Two knobs, both asserted:
//
//   - `bump-minor-pre-major: true` — breaking changes bump the MINOR inside 0.x
//     (0.54 -> 0.55), the sequence this repo has always shipped.
//   - the manifest stays 0.x — a config that maps correctly but a manifest that
//     already says 1.0.0 would keep bumping from there.
//
// The release PR itself is still opened by the workflow and merged by a human;
// this test only makes "somebody flipped the knob" a red build instead of a
// release nobody asked for.
package internal_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	releaseConfigPath   = ".github/release-please-config.json"
	releaseManifestPath = ".github/.release-please-manifest.json"
)

// releasePackage is the decoded `packages["."]` object. Fields are read as raw
// JSON and decoded one by one because the file is kebab-case (`bump-minor-pre-
// major`) and this repo lints Go structs toward camelCase tags — the external
// format wins, so no struct tag is involved at all.
type releasePackage map[string]json.RawMessage

func readJSONFile(t *testing.T, root, rel string) json.RawMessage {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return json.RawMessage(body)
}

func TestReleaseConfigKeepsBreakingChangesInsideZeroX(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	body := readJSONFile(t, root, releaseConfigPath)
	var top struct {
		Packages map[string]releasePackage `json:"packages"`
	}
	if err := json.Unmarshal(body, &top); err != nil {
		t.Fatalf("parse %s: %v (the workflow passes it as config-file, so a "+
			"malformed file reverts release-please to defaults)", releaseConfigPath, err)
	}
	pkg, ok := top.Packages["."]
	if !ok {
		t.Fatalf("%s has no %q package", releaseConfigPath, ".")
	}

	var bumpMinorPreMajor *bool
	if raw, ok := pkg["bump-minor-pre-major"]; ok {
		if err := json.Unmarshal(raw, &bumpMinorPreMajor); err != nil {
			t.Fatalf("%s: bump-minor-pre-major is not a boolean: %v", releaseConfigPath, err)
		}
	}
	if bumpMinorPreMajor == nil || !*bumpMinorPreMajor {
		t.Fatalf("bump-minor-pre-major must be present and true in %s: with it "+
			"false (or absent) one `feat!:`/`BREAKING CHANGE` opens a 1.0.0 "+
			"release PR", releaseConfigPath)
	}

	if raw, ok := pkg["bump-patch-for-minor-pre-major"]; ok {
		var patchForMinor bool
		if err := json.Unmarshal(raw, &patchForMinor); err == nil && patchForMinor {
			t.Errorf("bump-patch-for-minor-pre-major is true in %s: features would "+
				"ship as patch releases, which is not the 0.MINOR sequence this "+
				"repo follows", releaseConfigPath)
		}
	}

	var releaseType string
	if raw, ok := pkg["release-type"]; ok {
		_ = json.Unmarshal(raw, &releaseType)
	}
	if releaseType != "go" {
		t.Errorf("release-type = %q in %s, want %q", releaseType, releaseConfigPath, "go")
	}
}

func TestReleaseManifestStaysPreOne(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	body := readJSONFile(t, root, releaseManifestPath)
	var manifest map[string]string
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatalf("parse %s: %v", releaseManifestPath, err)
	}
	version, ok := manifest["."]
	if !ok {
		t.Fatalf("%s has no %q entry", releaseManifestPath, ".")
	}
	if !strings.HasPrefix(version, "0.") {
		t.Fatalf("manifest version is %q in %s: this repo is pre-1.0 and only a "+
			"human request moves it past 0.x (see AGENTS.md, Releases)",
			version, releaseManifestPath)
	}
}
