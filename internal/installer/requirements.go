// SCOPE:plugin — installer engine: pre-apply requirement checks
package installer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// minGo is the oldest local toolchain accepted. Anything >= 1.21
// self-upgrades to the template's version via GOTOOLCHAIN=auto, so the
// check is about presence and era, not an exact pin. The EXACT version the
// tree asks for is a separate question — see checkToolchainForTree.
const (
	minGoMajor = 1
	minGoMinor = 21
)

// goVer is a toolchain version. Patch takes part in the comparison: the
// go.mod `go` directive is enforced at patch level, so go1.27.2 refuses a
// tree that asks for go1.27.3.
type goVer struct {
	major, minor, patch int
}

func (v goVer) String() string {
	return fmt.Sprintf("go%d.%d.%d", v.major, v.minor, v.patch)
}

func (v goVer) less(o goVer) bool {
	if v.major != o.major {
		return v.major < o.major
	}
	if v.minor != o.minor {
		return v.minor < o.minor
	}
	return v.patch < o.patch
}

var (
	goVersionRe = regexp.MustCompile(`go(\d+)\.(\d+)(?:\.(\d+))?`)
	// goModGoRe anchors the `go` directive to column 0 so a `// go 1.x`
	// comment inside a require block cannot be mistaken for it.
	goModGoRe = regexp.MustCompile(`(?m)^go[ \t]+(\d+)\.(\d+)(?:\.(\d+))?`)
)

// goVersionOut is a seam for tests (no exec in unit tests).
var goVersionOut = func(ctx context.Context) (string, error) {
	out, err := runIn(ctx, ".", "go", "version")
	return string(out), err
}

// toolchainPolicyOut is a seam for tests: the GOTOOLCHAIN setting that decides
// whether `go` may fetch the version a tree asks for.
var toolchainPolicyOut = func(ctx context.Context) (string, error) {
	out, err := runIn(ctx, ".", "go", "env", "GOTOOLCHAIN")
	return string(out), err
}

// preflight fails fast — before any clone, trim, or confirm — when the
// tools the apply path shells out to are missing or too old. Read-only
// paths (plan preview, advise, --check) never call it. quiet silences the
// informational lines so --format json stays a single JSON document;
// questions (clone/apply confirms) always print.
func preflight(ctx context.Context, w io.Writer, needGit, needGo, quiet bool) error {
	if needGit {
		if err := lookGit(); err != nil {
			return &ExitError{code: 1, msg: "gogogo: git not found — " +
				"needed to clone the template. Install it " +
				"(https://git-scm.com/downloads), then re-run"}
		}
	}
	if needGo {
		raw, err := goVersionOut(ctx)
		if err != nil {
			return &ExitError{code: 1, msg: "gogogo: go not found — " +
				"needed to prove the scaffold (tidy+build) and to run it. " +
				"Install it: " + goInstallHint()}
		}
		v, ok := parseGoVersion(raw)
		if !ok || v.major < minGoMajor ||
			(v.major == minGoMajor && v.minor < minGoMinor) {
			return &ExitError{code: 1, msg: fmt.Sprintf(
				"gogogo: %s is too old — need go >= 1.21 "+
					"(newer toolchains download themselves via GOTOOLCHAIN). "+
					"Upgrade: %s", firstLine(raw), goInstallHint())}
		}
		if !quiet {
			fmt.Fprintf(w, "gogogo: %s — toolchain OK\n", firstLine(raw))
		}
	}
	return nil
}

// checkToolchainForTree refuses an apply that would trim, clone, or rewire a
// tree the toolchain then cannot build. preflight accepts go >= 1.21 because
// the default GOTOOLCHAIN=auto downloads whatever go.mod asks for; a pinned
// GOTOOLCHAIN (local, or goX.Y.Z) defeats that silently, and the mismatch only
// surfaces at `go mod tidy` — after the destructive half of the apply.
func checkToolchainForTree(ctx context.Context, dir string, w io.Writer, quiet bool) error {
	required, ok := goModRequirement(dir)
	if !ok {
		return nil
	}
	raw, err := goVersionOut(ctx)
	if err != nil {
		return &ExitError{code: 1, msg: "gogogo: go not found — " +
			"needed to prove the tree (tidy+build). Install it: " + goInstallHint()}
	}
	installed, ok := parseGoVersion(raw)
	if !ok {
		// Unparsable version: the presence/era gate in preflight owns that
		// verdict, and inventing a second one here would only contradict it.
		return nil
	}
	effective, policy, canUpgrade := effectiveToolchain(ctx, installed)
	if canUpgrade {
		if !quiet && installed.less(required) {
			fmt.Fprintf(w, "gogogo: this tree needs %s; GOTOOLCHAIN=%s fetches it "+
				"on first use (installed %s)\n", required, policyOrAuto(policy), installed)
		}
		return nil
	}
	if effective.less(required) {
		return &ExitError{code: 1, msg: fmt.Sprintf(
			"gogogo: this tree needs %s, but GOTOOLCHAIN=%s pins the toolchain "+
				"to %s (installed %s), so `go mod tidy` would fail after the "+
				"trim/rename.\nfix: go env -w GOTOOLCHAIN=auto   (or install %s: %s)",
			required, policy, effective, installed, required, goInstallHint())}
	}
	return nil
}

// effectiveToolchain reports the version a `go` command will actually run for
// a tree, plus the policy that decided it and whether that policy may fetch a
// newer one. An unreadable policy is not a refusal: the real `go` command
// reports its own error, and a name this installer does not model must not
// block an apply.
func effectiveToolchain(ctx context.Context, installed goVer) (goVer, string, bool) {
	raw, err := toolchainPolicyOut(ctx)
	if err != nil {
		return installed, "", true
	}
	policy := strings.TrimSpace(raw)
	switch strings.ToLower(policy) {
	case "", "auto", "path":
		return installed, policy, true
	case "local": // the installed toolchain, no downloads
		return installed, policy, false
	}
	if pinned, ok := parseGoVersion(policy); ok {
		return pinned, policy, false
	}
	return installed, policy, true
}

// policyOrAuto names the effective policy in narration: an empty GOTOOLCHAIN
// reads as "auto" (the go default) instead of printing a blank.
func policyOrAuto(policy string) string {
	if policy == "" {
		return "auto"
	}
	return policy
}

// goModRequirement reads the `go` directive from dir's go.mod: the version the
// toolchain must reach to build that tree. A missing or unreadable directive
// means "no requirement" — the installer must not invent one.
func goModRequirement(dir string) (goVer, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return goVer{}, false
	}
	return parseVer(goModGoRe, string(raw))
}

func parseGoVersion(raw string) (goVer, bool) {
	return parseVer(goVersionRe, raw)
}

// parseVer extracts major.minor.patch from the first match of re. A missing
// patch reads as 0: `go 1.21` and `go 1.21.0` are the same requirement.
func parseVer(re *regexp.Regexp, raw string) (goVer, bool) {
	m := re.FindStringSubmatch(raw)
	if m == nil {
		return goVer{}, false
	}
	var nums [3]int
	for i, s := range m[1:] {
		if s == "" {
			continue
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return goVer{}, false
		}
		nums[i] = n
	}
	return goVer{major: nums[0], minor: nums[1], patch: nums[2]}, true
}

func firstLine(s string) string {
	for i, c := range s {
		if c == '\n' {
			return s[:i]
		}
	}
	return s
}

// goInstallHint names the install for this OS; go.dev/dl is the fallback
// everywhere (one line per OS keeps the failure message greppable).
func goInstallHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "brew install go (or https://go.dev/dl/)"
	case "windows":
		return "winget install GoLang.Go (or https://go.dev/dl/)"
	default:
		return "sudo apt install golang-go (or https://go.dev/dl/)"
	}
}
