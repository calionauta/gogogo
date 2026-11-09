// Command gogogo-mcp exposes the gogogo installer as MCP tools over stdio.
//
// Thin adapter by design: every tool maps to installer.Run arguments, so
// the CLI and the tool transport execute the same code path with the same
// validation, receipts, and proof. Run it from an MCP client:
//
//	{"mcpServers": {"gogogo": {"command": "/path/to/gogogo-mcp"}}}
//
// Destructive tools (trim_apply, export_unit) respect confirm:true — the same
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

// The server reports installer.Version, never a variable of its own: the
// engine and the server are the same build, and two version numbers in one
// binary is how a check starts comparing the wrong one (the release build
// stamped main.version here and left installer.Version at "dev").

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

// buildServer registers every tool. Shared by main and tests so the
// served surface is exactly the tested surface.
func buildServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "gogogo", Version: installer.Version}, nil)
	for _, d := range toolCatalog {
		d.add(server, &mcp.Tool{Name: d.name, Description: d.desc})
	}
	return server
}

// --- tool argument shapes (JSON in, validated like CLI flags) ---

type dirArgs struct {
	Dir string `json:"dir,omitempty" jsonschema:"absolute path of the target checkout (required)"`
}

type versionArgs struct {
	Dir    string `json:"dir,omitempty" jsonschema:"optional: a scaffolded project checkout, so the answer also compares its recorded template revision"`
	Format string `json:"format,omitempty" jsonschema:"text or json (default json for tools)"`
}

type trimArgs struct {
	Name    string `json:"name,omitempty" jsonschema:"new project name, e.g. my-app (required)"`
	Owner   string `json:"owner,omitempty" jsonschema:"GitHub owner for the module path (default calionauta)"`
	Dir     string `json:"dir,omitempty" jsonschema:"target checkout directory (default ./<name>)"`
	Format  string `json:"format,omitempty" jsonschema:"text or json (default json for tools)"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"trim_apply only: must be true (preview with trim_plan first)"`
}

type migrateArgs struct {
	From      string `json:"from,omitempty" jsonschema:"base revision to start after (default: the project's recorded scaffolded-from tag)"`
	To        string `json:"to,omitempty" jsonschema:"newest revision to include (default: the latest release)"`
	Dir       string `json:"dir,omitempty" jsonschema:"project checkout to read the base marker from (default: current directory)"`
	Changelog string `json:"changelog,omitempty" jsonschema:"read the changelog from this file instead of the network (offline)"`
	Files     bool   `json:"files,omitempty" jsonschema:"also list the files upstream changed between the tags, each labelled with the capability it serves and whether this tree kept the unit (needs the compare API; this is the \"which of these apply to me\" half)"`
	Format    string `json:"format,omitempty" jsonschema:"text or json (default json for tools)"`
}

type newFeatureArgs struct {
	Name    string `json:"name,omitempty" jsonschema:"feature id, kebab-case, e.g. reports (required)"`
	Dir     string `json:"dir,omitempty" jsonschema:"target checkout (required)"`
	Format  string `json:"format,omitempty" jsonschema:"text or json (default json for tools)"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"must be true (preview with dryRun first)"`
	DryRun  bool   `json:"dryRun,omitempty" jsonschema:"print the plan and stop when true"`
}

type recipeArgs struct {
	Src     string `json:"src,omitempty" jsonschema:"recipe directory, or recipe id for install/update (required unless entry is set)"`
	Entry   string `json:"entry,omitempty" jsonschema:"optional catalog entry (.yml); src is then the pristine contents directory"`
	ID      string `json:"id,omitempty" jsonschema:"installed recipe id (for status/uninstall)"`
	Dir     string `json:"dir,omitempty" jsonschema:"target checkout directory (required)"`
	Catalog string `json:"catalog,omitempty" jsonschema:"optional catalog directory for status/outdated (default: <dir>/catalog)"`
	Format  string `json:"format,omitempty" jsonschema:"text or json (default json for tools)"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"must be true for install/update/uninstall (preview with dryRun first)"`
	DryRun  bool   `json:"dryRun,omitempty" jsonschema:"print the plan and stop; changes nothing"`
}

type recipeListArgs struct {
	Dir    string `json:"dir,omitempty" jsonschema:"template checkout directory (default current directory)"`
	Format string `json:"format,omitempty" jsonschema:"text or json (default json for tools)"`
}

type recipeDescribeArgs struct {
	ID     string `json:"id,omitempty" jsonschema:"recipe id (built-in capability id)"`
	Dir    string `json:"dir,omitempty" jsonschema:"template checkout directory (default current directory)"`
	Format string `json:"format,omitempty" jsonschema:"text or json (default json for tools)"`
}

// exportArgs is the port-bundle tool: it writes ONLY into `to` (a scratch
// directory) and never touches `dir`, so it needs no confirm gate — the
// destructive-tool rule protects the tree the caller cares about.
type exportArgs struct {
	Unit   string `json:"unit,omitempty" jsonschema:"installer unit id, e.g. whiteboard (required)"`
	Dir    string `json:"dir,omitempty" jsonschema:"pristine template checkout to copy the unit FROM (required)"`
	To     string `json:"to,omitempty" jsonschema:"scratch directory to write the bundle INTO; must not exist or be empty (required)"`
	Module string `json:"module,omitempty" jsonschema:"your project's module path, filled into the import rewrite"`
	Format string `json:"format,omitempty" jsonschema:"text or json (default json for tools)"`
}

type adviseArgs struct {
	Need   string `json:"need,omitempty" jsonschema:"use-case in your words (empty lists everything)"`
	Intent string `json:"intent,omitempty" jsonschema:"optional route: update|port|add-unit|new-feature|scaffold (omitted = infer from need, and the answer says so)"`
	Format string `json:"format,omitempty" jsonschema:"text or json (default json for tools)"`
	Dir    string `json:"dir,omitempty" jsonschema:"optional: also check this path, so the answer can say it is not a gogogo checkout (omitted = the answer depends on need alone)"`
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
receipts, and tidy+build proof. Version `+installer.Version+`.`)
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
	format := a.Format
	if format == "" {
		format = defaultFormat
	}
	args = append(args, "--format", format)
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

func handleVersionStatus(
	ctx context.Context, _ *mcp.CallToolRequest, args versionArgs,
) (*mcp.CallToolResult, any, error) {
	format := args.Format
	if format == "" {
		format = defaultFormat
	}
	cli := []string{"--check-update", "--format", format}
	if args.Dir != "" {
		cli = append(cli, "--dir", args.Dir)
	}
	var buf bytes.Buffer
	err := installer.Run(ctx, cli, strings.NewReader(""), &buf)
	// Exit 3 means "an update exists" — a verdict the caller asked for, not a
	// failed call. Wrapping it as an error would teach the agent to stop reading
	// the one answer it wanted.
	if err != nil && installer.ExitCode(err) != 3 {
		return runResult(buf.String(), err)
	}
	return textResult(buf.String()), nil, nil
}

func handleMigratePlan(
	ctx context.Context, _ *mcp.CallToolRequest, args migrateArgs,
) (*mcp.CallToolResult, any, error) {
	format := args.Format
	if format == "" {
		format = defaultFormat
	}
	cli := []string{"migrate", "--format", format}
	if args.Files {
		cli = append(cli, "--files")
	}
	for _, kv := range [][2]string{
		{"--from", args.From},
		{"--to", args.To},
		{"--dir", args.Dir},
		{"--changelog", args.Changelog},
	} {
		if kv[1] != "" {
			cli = append(cli, kv[0], kv[1])
		}
	}
	var buf bytes.Buffer
	err := installer.Run(ctx, cli, strings.NewReader(""), &buf)
	return runResult(buf.String(), err)
}

func handleNewFeature(
	ctx context.Context, _ *mcp.CallToolRequest, args newFeatureArgs,
) (*mcp.CallToolResult, any, error) {
	if args.Name == "" || args.Dir == "" {
		return nil, nil, fmt.Errorf("name and dir are both required")
	}
	format := args.Format
	if format == "" {
		format = defaultFormat
	}
	cli := []string{"new", "feature", args.Name, "--dir", args.Dir, "--format", format}
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

func recipeCLIArgs(subcommand string, args recipeArgs) ([]string, error) {
	if args.Dir == "" {
		return nil, fmt.Errorf("dir is required")
	}
	format := args.Format
	if format == "" {
		format = defaultFormat
	}
	cli := []string{"recipe", subcommand}
	switch subcommand {
	case "install", "update":
		if args.Src == "" {
			return nil, fmt.Errorf("src is required (recipe directory, catalog contents directory, or recipe id)")
		}
		cli = append(cli, args.Src, "--dir", args.Dir, "--format", format)
		if args.Entry != "" {
			cli = append(cli, "--entry", args.Entry)
		}
	case "status":
		if args.ID != "" {
			cli = append(cli, args.ID)
		}
		cli = append(cli, "--dir", args.Dir, "--format", format)
		if args.Catalog != "" {
			cli = append(cli, "--catalog", args.Catalog)
		}
	case "outdated":
		if args.ID != "" {
			cli = append(cli, args.ID)
		}
		cli = append(cli, "--dir", args.Dir, "--format", format)
		if args.Catalog != "" {
			cli = append(cli, "--catalog", args.Catalog)
		}
	case "uninstall":
		if args.ID == "" {
			return nil, fmt.Errorf("id is required")
		}
		cli = append(cli, args.ID, "--dir", args.Dir)
	}
	if !args.DryRun {
		if subcommand != "status" && subcommand != "outdated" {
			if !args.Confirm {
				return nil, fmt.Errorf("refusing without confirm:true (or dryRun:true to preview)")
			}
			cli = append(cli, "--yes")
		}
	} else if subcommand != "status" && subcommand != "outdated" {
		// `recipe uninstall` has no --dry-run; preview through status. Only
		// install/update can honor a dry run.
		if subcommand == "uninstall" {
			return nil, fmt.Errorf("uninstall has no dry-run; use recipe_status, then confirm:true")
		}
	}
	return cli, nil
}

func handleRecipeInstall(
	ctx context.Context, _ *mcp.CallToolRequest, args recipeArgs,
) (*mcp.CallToolResult, any, error) {
	cli, err := recipeCLIArgs("install", args)
	if err != nil {
		return nil, nil, err
	}
	var buf bytes.Buffer
	err = installer.Run(ctx, cli, strings.NewReader(""), &buf)
	return runResult(buf.String(), err)
}

func handleRecipeUpdate(
	ctx context.Context, _ *mcp.CallToolRequest, args recipeArgs,
) (*mcp.CallToolResult, any, error) {
	cli, err := recipeCLIArgs("update", args)
	if err != nil {
		return nil, nil, err
	}
	var buf bytes.Buffer
	err = installer.Run(ctx, cli, strings.NewReader(""), &buf)
	return runResult(buf.String(), err)
}

func handleRecipeUninstall(
	ctx context.Context, _ *mcp.CallToolRequest, args recipeArgs,
) (*mcp.CallToolResult, any, error) {
	cli, err := recipeCLIArgs("uninstall", args)
	if err != nil {
		return nil, nil, err
	}
	var buf bytes.Buffer
	err = installer.Run(ctx, cli, strings.NewReader(""), &buf)
	return runResult(buf.String(), err)
}

func handleRecipeStatus(
	ctx context.Context, _ *mcp.CallToolRequest, args recipeArgs,
) (*mcp.CallToolResult, any, error) {
	cli, err := recipeCLIArgs("status", args)
	if err != nil {
		return nil, nil, err
	}
	var buf bytes.Buffer
	err = installer.Run(ctx, cli, strings.NewReader(""), &buf)
	return runResult(buf.String(), err)
}

func handleRecipeOutdated(
	ctx context.Context, _ *mcp.CallToolRequest, args recipeArgs,
) (*mcp.CallToolResult, any, error) {
	cli, err := recipeCLIArgs("outdated", args)
	if err != nil {
		return nil, nil, err
	}
	var buf bytes.Buffer
	err = installer.Run(ctx, cli, strings.NewReader(""), &buf)
	// Exit 3 is the verdict ("an update exists"), not a failed call — the
	// same rule `version_status` follows, for the same reason.
	if err != nil && installer.ExitCode(err) != 3 {
		return runResult(buf.String(), err)
	}
	return textResult(buf.String()), nil, nil
}

func handleRecipeList(
	ctx context.Context, _ *mcp.CallToolRequest, args recipeListArgs,
) (*mcp.CallToolResult, any, error) {
	format := args.Format
	if format == "" {
		format = defaultFormat
	}
	cli := []string{"recipe", "list", "--dir", args.Dir, "--format", format}
	if args.Dir == "" {
		cli = []string{"recipe", "list", "--format", format}
	}
	var buf bytes.Buffer
	err := installer.Run(ctx, cli, strings.NewReader(""), &buf)
	return runResult(buf.String(), err)
}

func handleRecipeDescribe(
	ctx context.Context, _ *mcp.CallToolRequest, args recipeDescribeArgs,
) (*mcp.CallToolResult, any, error) {
	format := args.Format
	if format == "" {
		format = defaultFormat
	}
	cli := []string{"recipe", "describe", "--format", format}
	dir := args.Dir
	if dir == "" {
		dir = "."
	}
	cli = append(cli, "--dir", dir)
	if args.ID != "" {
		cli = append(cli, args.ID)
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
	out, err := installer.Advise(args.Need, format, args.Dir, args.Intent)
	if err != nil {
		return nil, nil, err
	}
	return textResult(out), nil, nil
}

func handleExportUnit(
	ctx context.Context, _ *mcp.CallToolRequest, args exportArgs,
) (*mcp.CallToolResult, any, error) {
	if args.Unit == "" || args.Dir == "" || args.To == "" {
		return nil, nil, fmt.Errorf("unit, dir and to are all required")
	}
	format := args.Format
	if format == "" {
		format = defaultFormat
	}
	cli := []string{"export", args.Unit, "--dir", args.Dir, "--to", args.To, "--format", format}
	if args.Module != "" {
		cli = append(cli, "--module", args.Module)
	}
	var buf bytes.Buffer
	err := installer.Run(ctx, cli, strings.NewReader(""), &buf)
	return runResult(buf.String(), err)
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
