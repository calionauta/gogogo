// SCOPE:layer=infra,removal=plugin — installer engine: --run requirement checks (make+air)
package installer

import (
	"fmt"
	"io"
	"os/exec"
	"runtime"
)

// lookPath is a seam for tests.
var lookPath = exec.LookPath

// lookGit reports whether git resolves (clone needs it).
var lookGit = func() error { _, err := lookPath("git"); return err }

// preflightRun verifies the two tools the --run handoff needs. Called up
// front with the rest of preflight: failing after a minutes-long
// scaffold+prove would be cruel.
func preflightRun(w io.Writer) error {
	for _, tool := range []struct{ name, hint string }{
		{"make", "install make (macOS: xcode-select --install; " +
			"Windows: choco install make, or use WSL)"},
		{"air", "go install github.com/air-verse/air@latest"},
	} {
		if _, err := lookPath(tool.name); err != nil {
			return &ExitError{code: 1, msg: "gogogo: " + tool.name +
				" not found — needed for --run (`make dev`). " +
				"Install it: " + tool.hint}
		}
	}
	if runtime.GOOS == "windows" {
		fmt.Fprintln(w, "gogogo: note: --run on Windows keeps a supervisor "+
			"process (no exec); Ctrl-C still stops dev")
	}
	return nil
}
