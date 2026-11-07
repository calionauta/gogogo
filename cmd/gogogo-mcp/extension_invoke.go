// The invoke tool: a thin MCP wrapper over the CLI's `extension invoke`.
//
// The MCP module carries no discovery or HTTP logic of its own — the CLI
// (`internal/installer`) owns validation, the loopback guard and the
// forwarding, so the two transports cannot drift.
package main

import (
	"bytes"
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/calionauta/gogogo/internal/installer"
)

type extensionInvokeArgs struct {
	ID      string            `json:"id,omitempty" jsonschema:"extension id (built-in capability id or installed recipe id)"`
	Verb    string            `json:"verb,omitempty" jsonschema:"declared invocable verb (see extension_describe)"`
	Server  string            `json:"server,omitempty" jsonschema:"running server base URL (loopback only, e.g. http://127.0.0.1:18099)"`
	Session string            `json:"session,omitempty" jsonschema:"operator session token (gogogo_auth cookie value)"`
	Params  map[string]string `json:"params,omitempty" jsonschema:"form fields for the verb"`
	Dir     string            `json:"dir,omitempty" jsonschema:"template checkout directory (default current directory)"`
	Format  string            `json:"format,omitempty" jsonschema:"text or json (default json for tools)"`
}

func handleExtensionInvoke(
	ctx context.Context, _ *mcp.CallToolRequest, args extensionInvokeArgs,
) (*mcp.CallToolResult, any, error) {
	format := args.Format
	if format == "" {
		format = defaultFormat
	}
	dir := args.Dir
	if dir == "" {
		dir = "."
	}
	cli := []string{
		"extension", "invoke",
		"--dir", dir, "--server", args.Server, "--session", args.Session,
		"--format", format,
	}
	for k, v := range args.Params {
		cli = append(cli, "--param", k+"="+v)
	}
	if args.ID != "" {
		cli = append(cli, args.ID)
	}
	if args.Verb != "" {
		cli = append(cli, args.Verb)
	}
	var buf bytes.Buffer
	err := installer.Run(ctx, cli, strings.NewReader(""), &buf)
	return runResult(buf.String(), err)
}
