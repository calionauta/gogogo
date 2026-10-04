//go:build windows

// SCOPE:layer=infra,removal=plugin — installer engine: --run handoff via child (Windows, no exec)
package installer

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

// handoff runs `make dev` as an attached child: Windows has no exec, so a
// supervisor stays resident. Stdio is wired through, so interactive Air
// output behaves; Ctrl-C propagates to the process group.
func handoff(dir string, stdout io.Writer) error {
	fmt.Fprintf(stdout, "gogogo: starting `make dev` in %s "+
		"(Ctrl-C stops dev)…\n", dir)
	cmd := exec.Command("make", "dev")
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
