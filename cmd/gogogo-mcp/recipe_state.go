// Runtime recipe switch tools: thin adapters over `recipe enable|disable`.
//
// Split from main.go for the file-size gate; the behavior is the CLI's, so the
// MCP cannot grow a second state store.
package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/calionauta/gogogo/internal/installer"
)

func handleRecipeEnable(
	ctx context.Context, _ *mcp.CallToolRequest, args recipeArgs,
) (*mcp.CallToolResult, any, error) {
	return handleRecipeState(ctx, args, "enable")
}

func handleRecipeDisable(
	ctx context.Context, _ *mcp.CallToolRequest, args recipeArgs,
) (*mcp.CallToolResult, any, error) {
	return handleRecipeState(ctx, args, "disable")
}

// handleRecipeState maps the runtime switch tools to the same CLI the
// developer runs; the product's /config page writes the same recipe_state row.
func handleRecipeState(
	ctx context.Context, args recipeArgs, verb string,
) (*mcp.CallToolResult, any, error) {
	if !args.Confirm {
		return nil, nil, fmt.Errorf("refusing without confirm:true")
	}
	if args.ID == "" || args.Dir == "" {
		return nil, nil, fmt.Errorf("recipe_%s needs id and dir", verb)
	}
	cli := []string{"recipe", verb, args.ID, "--dir", args.Dir}
	if args.DB != "" {
		cli = append(cli, "--db", args.DB)
	}
	var buf bytes.Buffer
	err := installer.Run(ctx, cli, strings.NewReader(""), &buf)
	return runResult(buf.String(), err)
}
