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
		fmt.Fprintln(os.Stderr, "gogogo:", err)
		os.Exit(installer.ExitCode(err))
	}
}
