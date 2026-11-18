package main

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/calionauta/gogogo/internal/prompts"
)

// serverInstructionsID is the prompt file holding the server's `instructions`
// field. Model-facing text is data (see cmd/check-prompts), and a client loads
// this once at initialize — before any tool call, and without the user having
// installed a single skill. It is the one place the route can be stated where
// the client is guaranteed to read it.
const serverInstructionsID = "mcp/server-instructions"

// serverOptions carries the instructions, and nothing else.
//
// prompts.Text panics on a missing file, which is the intended failure: an
// embedded prompt cannot go missing at runtime without the build being wrong,
// and a server that starts silently without its route is worse than one that
// refuses to start.
func serverOptions() *mcp.ServerOptions {
	return &mcp.ServerOptions{Instructions: prompts.Text(serverInstructionsID)}
}
