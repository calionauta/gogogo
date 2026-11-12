package main

import (
	"bytes"
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/calionauta/gogogo/internal/installer"
)

// The recipe evaluator and the recipe's own test runner, over MCP.
//
// Both are the CLI's verbs with no MCP counterpart until now, which left the
// agent that only speaks MCP unable to judge a declaration or run what it
// wrote — it had to shell out, which is exactly what the installer MCP
// exists to avoid. Same engine, same report, same verdicts.

// recipeCheckArgs is the evaluator surface: one recipe, or every installed
// one when the id is omitted.
type recipeCheckArgs struct {
	ID     string `json:"id,omitempty" jsonschema:"recipe id (omit to check every installed recipe)"`
	Dir    string `json:"dir,omitempty" jsonschema:"target checkout directory (required)"`
	Format string `json:"format,omitempty" jsonschema:"text or json (default json for tools)"`
}

// recipeTestArgs runs the tests ONE recipe owns. The scope is derived from
// the receipt, so the caller names no package: a declared list would be a
// second source of truth that can only disagree with the first. `noRace` is
// the fast TDD loop — the race detector is the gate's mode and the default,
// so the flag that turns it OFF is the one worth carrying.
type recipeTestArgs struct {
	ID     string `json:"id,omitempty" jsonschema:"recipe id (omit to test every installed recipe)"`
	Dir    string `json:"dir,omitempty" jsonschema:"target checkout directory (required)"`
	NoRace bool   `json:"noRace,omitempty" jsonschema:"skip the race detector (the CLI's --race=false): the fast TDD loop"`
	Run    string `json:"run,omitempty" jsonschema:"pass -run <regexp> to the test run"`
	List   bool   `json:"list,omitempty" jsonschema:"print the derived packages instead of running them"`
}

// handleRecipeCheck runs the evaluator the CLI runs: the same report, the
// same verdicts. A blocked recipe is not a crash — the call fails carrying
// the report, because "the recipe is blocked" is the answer the caller asked
// for and the transcript says which rule blocked it.
func handleRecipeCheck(
	ctx context.Context, _ *mcp.CallToolRequest, args recipeCheckArgs,
) (*mcp.CallToolResult, any, error) {
	dir := args.Dir
	if dir == "" {
		dir = "."
	}
	format := args.Format
	if format == "" {
		format = defaultFormat
	}
	cli := []string{"recipe", "check"}
	if args.ID != "" {
		cli = append(cli, args.ID)
	}
	cli = append(cli, "--dir", dir, "--format", format)
	var buf, errBuf bytes.Buffer
	err := installer.RunTo(ctx, cli, strings.NewReader(""), &buf, &errBuf)
	return runResult(buf.String()+errBuf.String(), err)
}

// handleRecipeTest runs the tests ONE recipe owns, with the scope derived
// from its receipt. stderr is captured too: a package that does not build
// prints the compile error to stderr, and a tool that dropped that stream
// would answer "exit status 1" to a question whose answer is the error.
func handleRecipeTest(
	ctx context.Context, _ *mcp.CallToolRequest, args recipeTestArgs,
) (*mcp.CallToolResult, any, error) {
	dir := args.Dir
	if dir == "" {
		dir = "."
	}
	cli := []string{"recipe", "test"}
	if args.ID != "" {
		cli = append(cli, args.ID)
	}
	cli = append(cli, "--dir", dir)
	if args.NoRace {
		cli = append(cli, "--race=false")
	}
	if args.Run != "" {
		cli = append(cli, "--run", args.Run)
	}
	if args.List {
		cli = append(cli, "--list")
	}
	var buf, errBuf bytes.Buffer
	err := installer.RunTo(ctx, cli, strings.NewReader(""), &buf, &errBuf)
	return runResult(buf.String()+errBuf.String(), err)
}
