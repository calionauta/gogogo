// SCOPE:layer=infra,removal=plugin — installer engine: pre-apply requirement checks
package installer

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
)

// minGo is the oldest local toolchain accepted. Anything >= 1.21
// self-upgrades to the template's version via GOTOOLCHAIN=auto, so the
// check is about presence and era, not an exact pin.
const (
	minGoMajor = 1
	minGoMinor = 21
)

var goVersionRe = regexp.MustCompile(`go(\d+)\.(\d+)`)

// Seams for tests (no exec in unit tests).
var (
	lookGit      = func() error { _, err := exec.LookPath("git"); return err }
	goVersionOut = func(ctx context.Context) (string, error) {
		out, err := runIn(ctx, ".", "go", "version")
		return string(out), err
	}
)

// preflight fails fast — before any clone, trim, or confirm — when the
// tools the apply path shells out to are missing or too old. Read-only
// paths (plan preview, advise, --check) never call it.
func preflight(ctx context.Context, w io.Writer, needGit, needGo bool) error {
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
		major, minor, ok := parseGoVersion(raw)
		if !ok || major < minGoMajor ||
			(major == minGoMajor && minor < minGoMinor) {
			return &ExitError{code: 1, msg: fmt.Sprintf(
				"gogogo: %s is too old — need go >= 1.21 "+
					"(newer toolchains download themselves via GOTOOLCHAIN). "+
					"Upgrade: %s", firstLine(raw), goInstallHint())}
		}
		fmt.Fprintf(w, "gogogo: %s — toolchain OK\n", firstLine(raw))
	}
	return nil
}

func parseGoVersion(raw string) (major, minor int, ok bool) {
	m := goVersionRe.FindStringSubmatch(raw)
	if m == nil {
		return 0, 0, false
	}
	major, err1 := strconv.Atoi(m[1])
	minor, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return major, minor, true
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
