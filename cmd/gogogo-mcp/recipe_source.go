// MCP: `recipe_source` — the read half of the recipe lifecycle.
package main

import (
	"bytes"
	"context"
	"errors"
	"strings"

	"github.com/calionauta/gogogo/internal/installer"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// recipeSourceArgs is the read-only sibling of recipeArgs. Separate struct so
// the schema cannot drift into "a source that also takes confirm".
type recipeSourceArgs struct {
	ID      string `json:"id" jsonschema:"recipe id to read (a bundled slice or a catalog entry)"`
	Dir     string `json:"dir,omitempty" jsonschema:"checkout or catalog root to read from (default: current directory)"`
	Version string `json:"version,omitempty" jsonschema:"catalog version to read (default: latest)"`
	File    string `json:"file,omitempty" jsonschema:"only this file (exact path or suffix), to keep the read small"`
	Src     string `json:"src,omitempty" jsonschema:"read a recipe bundle directory instead of the catalog"`
	Format  string `json:"format,omitempty" jsonschema:"text or json (default json)"`
}

// handleRecipeSource reads a recipe's code and writes nothing. This is the tool
// an agent should reach for when it wants to LEARN a slice and write its own:
// recipe_install is for when the operator wants the copy owned and updatable.
func handleRecipeSource(
	ctx context.Context, _ *mcp.CallToolRequest, args recipeSourceArgs,
) (*mcp.CallToolResult, any, error) {
	if args.ID == "" && args.Src == "" {
		return nil, nil, errors.New("id is required (or src for a bundle directory)")
	}
	format := args.Format
	if format == "" {
		format = defaultFormat
	}
	cli := []string{cliRecipe, "source"}
	if args.ID != "" {
		cli = append(cli, args.ID)
	}
	cli = append(cli, flagFormat, format)
	// Ordered, not a map: the argv a test asserts on must not shuffle.
	for _, opt := range []struct{ flag, val string }{
		{flagDir, args.Dir},
		{"--version", args.Version},
		{"--file", args.File},
		{"--src", args.Src},
	} {
		if opt.val != "" {
			cli = append(cli, opt.flag, opt.val)
		}
	}
	var buf bytes.Buffer
	if err := installer.Run(ctx, cli, strings.NewReader(""), &buf); err != nil {
		return runResult(buf.String(), err)
	}
	return textResult(buf.String()), nil, nil
}
