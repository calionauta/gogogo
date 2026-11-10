// Help output for the MCP server.
package main

import (
	"fmt"
	"os"

	"github.com/calionauta/gogogo/internal/installer"
)

// printHelp documents tools and client wiring for humans. The tool list
// renders from the catalog so it cannot drift from the served surface.
func printHelp(w *os.File) {
	fmt.Fprintln(w, "gogogo-mcp — gogogo installer as MCP tools (stdio).")
	fmt.Fprintln(w, "\nTools:")
	for _, d := range toolCatalog {
		fmt.Fprintf(w, "  %s — %s\n", d.name, d.desc)
	}
	fmt.Fprintln(w, `Destructive tools require confirm:true (preview first). advise_stack changes nothing.

Recipes need no per-recipe tool: recipe_list and recipe_describe read the
capabilities plus .recipes/*.yml at call time, and recipe_invoke dispatches
one declared verb. This server is the installer/evolution surface; a
product's own clients use the running server's API, not these tools.

Installed recipes appear in discovery automatically (no help regeneration).

Claude Code / Cursor / BB client config:
  {"mcpServers": {"gogogo": {"command": "/path/to/gogogo-mcp"}}}

Same engine as cmd/gogogo (internal/installer): identical validation,
receipts, and tidy+build proof. Version `+installer.Version+`.`)
}

// --- handlers (pure enough to unit-test: args in, text/JSON out) ---
