// Command gogogo scaffolds a new project from gogogo.
//
// Thin entrypoint: all logic lives in internal/installer (shared with
// the MCP server), so the CLI and tool transports behave identically.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/calionauta/gogogo/internal/installer"
)

func main() {
	if err := installer.Run(context.Background(), os.Args[1:], os.Stdin, os.Stdout); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		// A verdict-only exit (--check-update's code 3) prints its answer on
		// stdout and ErrorText returns "" for it; an empty line on stderr would
		// read like a second failure.
		if msg := installer.ErrorText(err); msg != "" {
			fmt.Fprintln(os.Stderr, msg)
		}
		os.Exit(installer.ExitCode(err))
	}
}
