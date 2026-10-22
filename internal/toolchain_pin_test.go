// SCOPE:core — the Go toolchain pin, asserted across every file that declares
// it. A test, not a doc: the bump in #191 touched these files by hand, and
// nothing failed when one was missed.
//
// It was missed. deploy/Dockerfile.prod still said `FROM golang:1.27-alpine`
// while go.mod declared `go 1.27.3`, so the production image could be built by
// a toolchain older than the tree requires. Go then either refuses outright or
// silently downloads the pinned toolchain mid-build — which makes the shipped
// artifact depend on the network and on Docker Hub's tag drift, in a pipeline
// that otherwise promotes an image by digest precisely so the bytes are the
// ones the tests proved.
//
// THE BUMP SURFACE, in one place. Raising the version means editing all of:
//
//	go.mod                                  go 1.27.3        (authoritative)
//	cmd/gogogo-mcp/go.mod                   go 1.27.3
//	bin/install-go-toolchain.sh             GO_VERSION:-1.27.3
//	Dockerfile                              golang:1.27.3-alpine
//	deploy/Dockerfile.prod                  golang:1.27.3-alpine
//	skills/.../SKILL.md                     "Go 1.27.3 or newer"
//
// and, at MINOR granularity (the level those files speak):
//
//	AGENTS.md, README.md, docs/stack-layers.md, docs/exception-to-go.md,
//	scripts/desktop-build.sh                "Go 1.27"
//
// Not asserted, on purpose:
//
//   - CI step names stay version-agnostic ("Install the pinned Go toolchain"):
//     the number lives in the script default, so a bump cannot leave a stale
//     step name behind.
//   - install.sh asks go.dev for the newest release — that is the right answer
//     for a machine that has no Go, and pinning it would defeat the point.
//   - docs/troubleshooting.md and the installer tests name OLDER versions as
//     examples (a go1.27.2 toolchain refusing a 1.27.3 tree). Those are the
//     subject matter, not a pin.
package internal_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	// goDirectiveRE captures `go 1.27.3` on its own line in a go.mod.
	goDirectiveRE = regexp.MustCompile(`(?m)^go (\d+\.\d+(?:\.\d+)?)$`)
	// goVersionEnvRE captures the default in `GO_VERSION:-1.27.3`.
	goVersionEnvRE = regexp.MustCompile(`GO_VERSION:-([0-9][0-9.]*)`)
	// golangImageRE captures every `golang:<version>-alpine` mention, in a
	// FROM line or a comment: a stale comment is still a file that lies.
	golangImageRE = regexp.MustCompile(`golang:([0-9][0-9.]*)-alpine`)
)

// TestGoToolchainPinIsConsistent fails when any file declares a Go version
// other than the one go.mod requires. go.mod is the source of truth.
func TestGoToolchainPinIsConsistent(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	full := singleCapture(t, root, "go.mod", goDirectiveRE)
	if strings.Count(full, ".") != 2 {
		t.Fatalf("go.mod declares `go %s`: the bump surface assumes a FULL patch "+
			"version (1.27.3), because that is what CI installs and what the "+
			"container images tag", full)
	}
	parts := strings.Split(full, ".")
	minor := parts[0] + "." + parts[1]

	for _, pin := range []struct {
		file string
		re   *regexp.Regexp
		want string
		why  string
	}{
		{"cmd/gogogo-mcp/go.mod", goDirectiveRE, full, "the second module in the repo builds in the same CI job"},
		{"bin/install-go-toolchain.sh", goVersionEnvRE, full, "this is the toolchain every CI job installs"},
		{"Dockerfile", golangImageRE, full, "the dev/desktop image builder"},
		{"deploy/Dockerfile.prod", golangImageRE, full, "the production image builder promoted by digest"},
	} {
		allEqual(t, root, pin.file, pin.re, pin.want, pin.why)
	}

	for _, claim := range []struct {
		file string
		want string
	}{
		{"skills/gogogo-coding-standards/SKILL.md", full},
		{"AGENTS.md", "Go " + minor},
		{"README.md", "Go " + minor},
		{"docs/stack-layers.md", "Go " + minor},
		{"docs/exception-to-go.md", "Go " + minor},
		{"scripts/desktop-build.sh", "Go " + minor},
	} {
		body := readFile(t, root, claim.file)
		if !strings.Contains(body, claim.want) {
			t.Errorf("%s does not mention %q: a Go bump has to reach the files that "+
				"tell users and agents which toolchain builds the tree", claim.file, claim.want)
		}
	}
}

// singleCapture returns the one capture of re in file, failing when the match
// count is not exactly one (a missing pattern reads as "consistent" otherwise).
func singleCapture(t *testing.T, root, file string, re *regexp.Regexp) string {
	t.Helper()
	m := re.FindAllStringSubmatch(readFile(t, root, file), -1)
	if len(m) != 1 {
		t.Fatalf("%s: found %d matches for %s, want exactly 1", file, len(m), re)
	}
	return m[0][1]
}

// allEqual fails when any capture of re in file differs from want, or when the
// file has no capture at all.
func allEqual(t *testing.T, root, file string, re *regexp.Regexp, want, why string) {
	t.Helper()
	m := re.FindAllStringSubmatch(readFile(t, root, file), -1)
	if len(m) == 0 {
		t.Errorf("%s no longer declares a Go version (%s): %s", file, re, why)
		return
	}
	for _, c := range m {
		if c[1] != want {
			t.Errorf("%s declares Go %s, go.mod requires %s — %s. A builder older than "+
				"the tree either refuses or downloads the pinned toolchain mid-build, "+
				"so the artifact stops being reproducible.", file, c[1], want, why)
		}
	}
}

// readFile reads a repo-relative file.
func readFile(t *testing.T, root, file string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, file))
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	return string(b)
}
