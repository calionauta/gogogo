//go:build !windows

// SCOPE:layer=infra,removal=plugin — installer engine: --run handoff via exec (Unix)
package installer

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
)

// handoff replaces this process with `make dev` inside dir: the one-line
// install (`go run ...@latest --run`) ends inside the dev loop instead of
// printing a `cd` a child process could never perform itself. Signals go
// straight to make/air (no supervisor left to confuse them); Ctrl-C stops
// dev and returns the terminal.
func handoff(dir string, stdout io.Writer) error {
	fmt.Fprintf(stdout, "gogogo: handing over to `make dev` in %s "+
		"(Ctrl-C stops dev)…\n", dir)
	makePath, err := exec.LookPath("make")
	if err != nil {
		return &ExitError{code: 1, msg: "gogogo: make not found — " +
			"run `cd " + dir + " && make dev` manually"}
	}
	if err := os.Chdir(dir); err != nil {
		return err
	}
	return syscall.Exec(makePath, []string{"make", "dev"}, os.Environ())
}
