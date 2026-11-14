// Installer CLI: `gogogo shape check`.
//
// The check itself lives in internal/shape, so the stand-alone gate
// (cmd/check-shape), this subcommand and any future MCP tool answer with the
// same rules. This file is the flag plumbing only: the CLI exists because a
// developer inside a checkout should not have to remember which `go run` path
// holds the gate.
package installer

import (
	"flag"
	"fmt"
	"io"

	"github.com/calionauta/gogogo/internal/shape"
)

// runShape dispatches the shape subcommand. Only `check` exists today: the
// record is written by the scaffolder, and a verb that rewrote it by hand would
// be a second way to declare the same product.
func runShape(args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "check" {
		return &ExitError{code: 1, msg: "shape needs a subcommand: check [--dir .]"}
	}
	fs := flag.NewFlagSet("shape check", flag.ContinueOnError)
	dir := fs.String("dir", ".", "tree to check")
	quiet := fs.Bool("quiet", false, "print only failures")
	fs.SetOutput(stdout)
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	problems, err := shape.Check(*dir)
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(stdout, p)
		}
		return &ExitError{code: 1, msg: fmt.Sprintf(
			"shape check: %d problem(s); the record lives in %s", len(problems), shape.Path)}
	}
	if !*quiet {
		c, _ := shape.Load(*dir)
		fmt.Fprintf(stdout, "shape check: %s — entrypoints %v, %d removed path(s)\n",
			c.Shape, c.Entrypoints, len(c.Removed))
	}
	return nil
}
