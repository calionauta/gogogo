// SCOPE:plugin - installer engine: the toolchain pin proof.
//
// It lives beside the main proof (prove, in run.go) but in its own file because
// it is conditional: only a real template tree carries internal/toolchain_pin_test.go.
// The synthetic fixtures do not, and the proof must not invent a failure there.
package installer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// proveToolchainPin runs the pin test when the tree carries it.
//
// The pin surface names files by PATH (cmd/<name>-mcp/go.mod,
// skills/<name>-coding-standards/SKILL.md), so a rename that rewrote the
// references but left the directories behind breaks it while the build above
// stays green. This is the cheapest check that can see the difference; the
// synthetic fixtures have no internal/ directory and skip it.
func proveToolchainPin(ctx context.Context, dir string, stdout io.Writer) error {
	// A tree without the pin surface is a fixture, not a failure; anything else
	// from Stat (a permission error, a missing --dir) must not be read as "no
	// pin here", which would silently skip the proof.
	switch _, statErr := os.Stat(filepath.Join(dir, "internal", "toolchain_pin_test.go")); {
	case errors.Is(statErr, fs.ErrNotExist):
		return nil
	case statErr != nil:
		return fmt.Errorf("look for the toolchain pin surface: %w", statErr)
	}
	fmt.Fprintln(stdout, "gogogo: prove: go test ./internal/ -run TestGoToolchainPin …")
	out, err := runIn(ctx, dir, "go", "test", "-count=1", "./internal/", "-run", "TestGoToolchainPin")
	if err != nil {
		return &ExitError{code: 2, msg: fmt.Sprintf(
			"toolchain pin proof failed (%v) - a path the pin surface names does not "+
				"exist, which is what a rename that rewrote paths without moving "+
				"directories leaves behind:\n%s",
			err, tailLines(string(out), proofBuildTailLines))}
	}
	return nil
}
