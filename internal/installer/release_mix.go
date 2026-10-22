// SCOPE:plugin — installer engine: refuse to mix template releases
package installer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// sourceRelease reports the release a template checkout is at, read from the
// top of its changelog. Empty means "not knowable" — an extracted tarball, a
// shallow export, a dev build — and unknown must never be treated as a match.
func sourceRelease(dir string) string {
	raw, err := os.ReadFile(filepath.Join(dir, "CHANGELOG.md"))
	if err != nil {
		return ""
	}
	releases := parseChangelog(string(raw))
	if len(releases) == 0 {
		return ""
	}
	return strings.TrimPrefix(releases[0].Version, "v")
}

// projectBase reports the release a scaffolded project records, without the
// repo prefix. Empty when the marker is absent, untagged or a dev build.
func projectBase(dir string) string {
	m := readTemplateMarker(dir)
	if !m.Found {
		return ""
	}
	_, tag, ok := strings.Cut(m.Ref, "@")
	if !ok {
		return ""
	}
	tag = strings.TrimPrefix(tag, "v")
	if tag == "" || tag == "dev" {
		return ""
	}
	return tag
}

// checkReleaseMix refuses to copy a unit out of a template release other than
// the one the project was scaffolded from, and says so when it cannot tell.
//
// This is the quiet failure `add` cannot otherwise prove against. `add` copies
// files AND restores wiring anchors; if the unit's anchors moved between the
// two releases it refuses loudly (good), but if they did not it will happily
// graft next release's files onto this release's composition root and the build
// proof compiles something the project never had.
//
// Unknown on either side is NOT a mismatch: a checkout without a changelog or a
// project whose marker predates the tag has no release to compare, and refusing
// those would break exactly the trees this check exists to protect. The skip is
// announced, because "checked and fine" and "nothing to check" are different
// claims.
func checkReleaseMix(projectDir, templateDir string, note io.Writer) error {
	base, src := projectBase(projectDir), sourceRelease(templateDir)
	switch {
	case base == "" || src == "":
		fmt.Fprintf(note, "gogogo: release check skipped (project base %s, template %s) — "+
			"copying from whatever this checkout is\n", asRelease(base), asRelease(src))
		return nil
	case base == src:
		return nil
	}
	return &ExitError{code: 1, msg: fmt.Sprintf(
		"gogogo: add: the template checkout is at v%s but this project was scaffolded from v%s — "+
			"copying across releases mixes two trees. Check the base out first: "+
			"git -C %s checkout v%s — or bring the project up to date with "+
			"`gogogo migrate --dir %s`",
		src, base, templateDir, base, projectDir)}
}

// asRelease renders a possibly-empty release for the skip line.
func asRelease(v string) string {
	if v == "" {
		return "unknown"
	}
	return "v" + v
}
