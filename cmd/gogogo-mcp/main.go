// Command gogogo-mcp exposes the gogogo installer as MCP tools over stdio.
//
// Thin adapter by design: every tool maps to installer.Run arguments, so
// the CLI and the tool transport execute the same code path with the same
// validation, receipts, and proof. Run it from an MCP client:
//
//	{"mcpServers": {"gogogo": {"command": "/path/to/gogogo-mcp"}}}
//
// Destructive tools (trim_apply, add_unit) require confirm:true — the same
// rule as --yes on the CLI. Preview with trim_plan / add dry-run first.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/calionauta/gogogo/internal/capabilities"
	"github.com/calionauta/gogogo/internal/installer"
)

// version is the release tag. Set via -ldflags -X main.version=...;
// plain `go build` leaves the dev default.
var version = "dev"

// defaultFormat is the tool transport default: machines get JSON,
// humans get text on the CLI.
const defaultFormat = "json"

func main() {
	showHelp := flag.Bool("help", false, "print tools and client config, then exit")
	flag.Parse()
	if *showHelp {
		printHelp(os.Stdout)
		return
	}
	server := buildServer()
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "gogogo-mcp:", err)
		os.Exit(1)
	}
}

// toolDef is the single source of truth for the served surface: each
// entry registers on the server, prints in --help, and is asserted in
// tests (served count) and docs (README table via TestMcpReadmeListsTools).
// Adding a tool means adding one entry — never hunting N call sites.
type toolDef struct {
	name string
	desc string
	add  func(s *mcp.Server, t *mcp.Tool)
}

var toolCatalog = []toolDef{
	{
		"capabilities_list",
		"Raw registry JSON: every template capability with id, kind, summary, and off-switch.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleCapabilitiesList) },
	},
	{
		"trim_plan",
		"Preview scaffolding/trimming a checkout (validates ids, shows consequences). Changes nothing.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleTrimPlan) },
	},
	{
		"trim_apply",
		"Scaffold/trim a checkout (rename, trim, prove with build). Requires confirm:true.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleTrimApply) },
	},
	{
		"check_tree",
		"Verify installer markers without changing anything (drift gate).",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleCheckTree) },
	},
	{
		"add_unit",
		"Add one template unit to a checkout (deps, rebase, proof). Requires confirm:true.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleAddUnit) },
	},
	{
		"advise_stack",
		"Opinions, not changes: which units to keep for a use-case " +
			"and how each switches off. Empty need returns the full map; " +
			"no preset matched means decide from the capabilities table " +
			"or retry with broader terms. Start here when deciding. " +
			"Answers in one of three scopes: `template` (a gogogo-shaped need), " +
			"`patterns` (a non-Go stack — take the idea, nothing installs), or " +
			"`go-standards` (a Go need that forbids dependencies, e.g. " +
			"stdlib-only: the template cannot apply). The Go coding standards " +
			"it enforces are the gogogo-coding-standards skill — " +
			"https://github.com/calionauta/gogogo (install: " +
			"npx skills add calionauta/gogogo); it is not shipped in this binary.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleAdvise) },
	},
}

// buildServer registers every tool. Shared by main and tests so the
// served surface is exactly the tested surface.
func buildServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "gogogo", Version: version}, nil)
	for _, d := range toolCatalog {
		d.add(server, &mcp.Tool{Name: d.name, Description: d.desc})
	}
	return server
}

// --- tool argument shapes (JSON in, validated like CLI flags) ---

type dirArgs struct {
	Dir string `json:"dir,omitempty" jsonschema:"absolute path of the target checkout (required)"`
}

type trimArgs struct {
	Name     string `json:"name,omitempty" jsonschema:"new project name, e.g. my-app (required)"`
	Owner    string `json:"owner,omitempty" jsonschema:"GitHub owner for the module path (default calionauta)"`
	Dir      string `json:"dir,omitempty" jsonschema:"target checkout directory (default ./<name>)"`
	Plugins  string `json:"plugins,omitempty" jsonschema:"comma-separated plugins to KEEP (default all, none drops all)"`
	Features string `json:"features,omitempty" jsonschema:"comma-separated features to KEEP (default all, none drops all)"`
	Format   string `json:"format,omitempty" jsonschema:"text or json (default json for tools)"`
	Confirm  bool   `json:"confirm,omitempty" jsonschema:"trim_apply only: must be true (preview with trim_plan first)"`
}

type addArgs struct {
	Unit    string `json:"unit,omitempty" jsonschema:"installer unit id, e.g. whiteboard (required)"`
	From    string `json:"from,omitempty" jsonschema:"pristine template checkout to copy from (required)"`
	Dir     string `json:"dir,omitempty" jsonschema:"target project checkout (required)"`
	Format  string `json:"format,omitempty" jsonschema:"text or json (default json for tools)"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"must be true (preview with dry-run first)"`
	DryRun  bool   `json:"dryRun,omitempty" jsonschema:"print the plan and stop when true"`
}

type adviseArgs struct {
	Need   string `json:"need,omitempty" jsonschema:"use-case in your words (empty lists everything)"`
	Format string `json:"format,omitempty" jsonschema:"text or json (default json for tools)"`
}

// printHelp documents tools and client wiring for humans. The tool list
// renders from the catalog so it cannot drift from the served surface.
func printHelp(w *os.File) {
	fmt.Fprintln(w, "gogogo-mcp — gogogo installer as MCP tools (stdio).")
	fmt.Fprintln(w, "\nTools:")
	for _, d := range toolCatalog {
		fmt.Fprintf(w, "  %s — %s\n", d.name, d.desc)
	}
	fmt.Fprintln(w, `Destructive tools require confirm:true (preview first). advise_stack changes nothing.

Claude Code / Cursor / BB client config:
  {"mcpServers": {"gogogo": {"command": "/path/to/gogogo-mcp"}}}

Same engine as cmd/gogogo (internal/installer): identical validation,
receipts, and tidy+build proof. Version `+version+`.`)
}

// --- handlers (pure enough to unit-test: args in, text/JSON out) ---

func handleCapabilitiesList(
	_ context.Context, _ *mcp.CallToolRequest, _ struct{},
) (*mcp.CallToolResult, any, error) {
	raw, err := json.MarshalIndent(capabilities.All, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return textResult(string(raw)), nil, nil
}

func trimCLIArgs(a trimArgs, extra ...string) []string {
	args := []string{"--name", a.Name}
	if a.Owner != "" {
		args = append(args, "--owner", a.Owner)
	}
	if a.Dir != "" {
		args = append(args, "--dir", a.Dir)
	}
	if a.Plugins != "" {
		args = append(args, "--plugins", a.Plugins)
	}
	if a.Features != "" {
		args = append(args, "--features", a.Features)
	}
	format := a.Format
	if format == "" {
		format = defaultFormat
	}
	args = append(args, "--format", format, "--no-tui")
	return append(args, extra...)
}

func handleTrimPlan(
	ctx context.Context, _ *mcp.CallToolRequest, args trimArgs,
) (*mcp.CallToolResult, any, error) {
	var buf bytes.Buffer
	err := installer.Run(ctx, append(trimCLIArgs(args), "--dry-run"), strings.NewReader(""), &buf)
	return runResult(buf.String(), err)
}

func handleTrimApply(
	ctx context.Context, _ *mcp.CallToolRequest, args trimArgs,
) (*mcp.CallToolResult, any, error) {
	if !args.Confirm {
		return nil, nil, fmt.Errorf("refusing without confirm:true — preview with trim_plan first")
	}
	var buf bytes.Buffer
	err := installer.Run(ctx, append(trimCLIArgs(args), "--yes"), strings.NewReader(""), &buf)
	return runResult(buf.String(), err)
}

func handleCheckTree(
	ctx context.Context, _ *mcp.CallToolRequest, args dirArgs,
) (*mcp.CallToolResult, any, error) {
	if args.Dir == "" {
		return nil, nil, fmt.Errorf("dir is required (absolute path)")
	}
	var buf bytes.Buffer
	err := installer.Run(ctx, []string{"--check", "--dir", args.Dir}, strings.NewReader(""), &buf)
	return runResult(buf.String(), err)
}

func handleAddUnit(
	ctx context.Context, _ *mcp.CallToolRequest, args addArgs,
) (*mcp.CallToolResult, any, error) {
	if args.Unit == "" || args.From == "" || args.Dir == "" {
		return nil, nil, fmt.Errorf("unit, from and dir are all required")
	}
	format := args.Format
	if format == "" {
		format = defaultFormat
	}
	cli := []string{"add", args.Unit, "--from", args.From, "--dir", args.Dir, "--format", format, "--no-tui"}
	if args.DryRun {
		cli = append(cli, "--dry-run")
	} else {
		if !args.Confirm {
			return nil, nil, fmt.Errorf("refusing without confirm:true (or dryRun:true to preview)")
		}
		cli = append(cli, "--yes")
	}
	var buf bytes.Buffer
	err := installer.Run(ctx, cli, strings.NewReader(""), &buf)
	return runResult(buf.String(), err)
}

func handleAdvise(
	_ context.Context, _ *mcp.CallToolRequest, args adviseArgs,
) (*mcp.CallToolResult, any, error) {
	format := args.Format
	if format == "" {
		format = defaultFormat
	}
	out, err := installer.Advise(args.Need, format)
	if err != nil {
		return nil, nil, err
	}
	return textResult(out), nil, nil
}

// runResult maps installer outcomes to tool results: exit-2 proof failures
// and validation errors surface as tool errors carrying the transcript,
// so the LLM sees what a human would see on the terminal.
func runResult(out string, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		msg := out
		if msg == "" {
			msg = err.Error()
		} else {
			msg += "\nerror: " + err.Error()
		}
		return nil, nil, fmt.Errorf("%s", msg)
	}
	return textResult(out), nil, nil
}

func textResult(out string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: out}},
	}
}
