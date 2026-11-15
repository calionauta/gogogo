// Command gogogo-mcp exposes the gogogo installer as MCP tools over stdio.
//
// Thin adapter by design: every tool maps to installer.Run arguments, so
// the CLI and the tool transport execute the same code path with the same
// validation, receipts, and proof. Run it from an MCP client:
//
//	{"mcpServers": {"gogogo": {"command": "/path/to/gogogo-mcp"}}}
//
// Destructive tools (scaffold_apply, export_unit) respect confirm:true — the same
// rule as --yes on the CLI. Preview with scaffold_plan / add dry-run first.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

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
	// params is the one-line parameter summary the documentation tables
	// carry (generated from this catalog).
	params string
	// effect is the safety verdict the documentation tables carry: does the
	// tool change anything? The pairing with `confirm` is pinned by the
	// catalog test, so the safety column cannot drift away from the guard.
	effect string
	add    func(s *mcp.Server, t *mcp.Tool)
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

// The CLI spellings the tools pass through. Named once so a flag rename in the
// installer fails in one place, not in six string literals.
const (
	cliRecipe   = "recipe"
	flagDir     = "--dir"
	flagFormat  = "--format"
	flagEntry   = "--entry"
	flagCatalog = "--catalog"
)

// --- tool argument shapes (JSON in, validated like CLI flags) ---

type versionArgs struct {
	Dir    string `json:"dir,omitempty" jsonschema:"optional: a scaffolded project checkout, so the answer also compares its recorded template revision"`
	Format string `json:"format,omitempty" jsonschema:"text or json (default json for tools)"`
}

type scaffoldArgs struct {
	Name    string `json:"name,omitempty" jsonschema:"new project name, e.g. my-app (required)"`
	Owner   string `json:"owner,omitempty" jsonschema:"GitHub owner for the module path (default calionauta)"`
	Dir     string `json:"dir,omitempty" jsonschema:"target checkout directory (default ./<name>)"`
	Format  string `json:"format,omitempty" jsonschema:"text or json (default json for tools)"`
	Shape   string `json:"shape,omitempty" jsonschema:"product shape: full|web|api|desktop|gui|mobile (which entrypoints the tree ships; default full)"`
	Recipes string `json:"recipes,omitempty" jsonschema:"demo slices to drop: none|all|<ids> (default none). A slice your tree still imports stays, with the importer named"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"scaffold_apply only: must be true (preview with scaffold_plan first)"`
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
	ID      string `json:"id,omitempty" jsonschema:"installed recipe id (for status/uninstall/enable/disable)"`
	Dir     string `json:"dir,omitempty" jsonschema:"target checkout directory (required)"`
	Catalog string `json:"catalog,omitempty" jsonschema:"optional catalog directory for status/outdated (default: <dir>/catalog)"`
	DB      string `json:"db,omitempty" jsonschema:"optional app database path for enable/disable (default TURSO_DB_PATH or <dir>/data/app.db)"`
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

func scaffoldCLIArgs(a scaffoldArgs, extra ...string) []string {
	args := []string{"--name", a.Name}
	if a.Owner != "" {
		args = append(args, "--owner", a.Owner)
	}
	if a.Dir != "" {
		args = append(args, "--dir", a.Dir)
	}
	if a.Shape != "" {
		args = append(args, "--shape", a.Shape)
	}
	if a.Recipes != "" {
		args = append(args, "--recipes", a.Recipes)
	}
	format := a.Format
	if format == "" {
		format = defaultFormat
	}
	args = append(args, "--format", format)
	return append(args, extra...)
}

func handleScaffoldPlan(
	ctx context.Context, _ *mcp.CallToolRequest, args scaffoldArgs,
) (*mcp.CallToolResult, any, error) {
	var buf bytes.Buffer
	err := installer.Run(ctx, append(scaffoldCLIArgs(args), "--dry-run"), strings.NewReader(""), &buf)
	return runResult(buf.String(), err)
}

func handleScaffoldApply(
	ctx context.Context, _ *mcp.CallToolRequest, args scaffoldArgs,
) (*mcp.CallToolResult, any, error) {
	if !args.Confirm {
		return nil, nil, errors.New("refusing without confirm:true — preview with scaffold_plan first")
	}
	var buf bytes.Buffer
	err := installer.Run(ctx, append(scaffoldCLIArgs(args), "--yes"), strings.NewReader(""), &buf)
	return runResult(buf.String(), err)
}

func handleVersionStatus(
	ctx context.Context, _ *mcp.CallToolRequest, args versionArgs,
) (*mcp.CallToolResult, any, error) {
	format := args.Format
	if format == "" {
		format = defaultFormat
	}
	cli := []string{"--check-update", flagFormat, format}
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
	cli := []string{"migrate", flagFormat, format}
	if args.Files {
		cli = append(cli, "--files")
	}
	for _, kv := range [][2]string{
		{"--from", args.From},
		{"--to", args.To},
		{flagDir, args.Dir},
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
		return nil, nil, errors.New("name and dir are both required")
	}
	format := args.Format
	if format == "" {
		format = defaultFormat
	}
	cli := []string{"new", "feature", args.Name, flagDir, args.Dir, flagFormat, format}
	if args.DryRun {
		cli = append(cli, "--dry-run")
	} else {
		if !args.Confirm {
			return nil, nil, errors.New("refusing without confirm:true (or dryRun:true to preview)")
		}
		cli = append(cli, "--yes")
	}
	var buf bytes.Buffer
	err := installer.Run(ctx, cli, strings.NewReader(""), &buf)
	return runResult(buf.String(), err)
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

type localesArgs struct {
	Dir    string `json:"dir,omitempty" jsonschema:"checkout whose locales.yml to read or write (default: current directory)"`
	Set    string `json:"set,omitempty" jsonschema:"comma-separated tags to record (e.g. en,pt-BR,es); omit to read"`
	Format string `json:"format,omitempty" jsonschema:"text or json (default json)"`
}

// handleLocales is the operator question as a tool: read what this tree ships,
// or record the answer once so every later gate grades against it.
func handleLocales(
	ctx context.Context, _ *mcp.CallToolRequest, args localesArgs,
) (*mcp.CallToolResult, any, error) {
	format := args.Format
	if format == "" {
		format = defaultFormat
	}
	cli := []string{"locales", flagFormat, format}
	if args.Dir != "" {
		cli = append(cli, flagDir, args.Dir)
	}
	if args.Set != "" {
		cli = append(cli, "--set", args.Set)
	}
	var buf bytes.Buffer
	if err := installer.Run(ctx, cli, strings.NewReader(""), &buf); err != nil {
		return runResult(buf.String(), err)
	}
	return textResult(buf.String()), nil, nil
}

type receiptsArgs struct {
	Dir    string `json:"dir,omitempty" jsonschema:"checkout whose receipts to verify (default: current directory)"`
	Write  bool   `json:"write,omitempty" jsonschema:"re-hash the stale files in place (the fix); omit to report only"`
	Format string `json:"format,omitempty" jsonschema:"text or json (default json)"`
}

// handleRecipeReceipts answers "do the declared hashes still describe the
// tree?". A fork's edits are not drift — this is for the case where the RECORD
// rotted, which silently breaks install/prune/test-scope downstream.
func handleRecipeReceipts(
	ctx context.Context, _ *mcp.CallToolRequest, args receiptsArgs,
) (*mcp.CallToolResult, any, error) {
	format := args.Format
	if format == "" {
		format = defaultFormat
	}
	cli := []string{cliRecipe, "receipts", flagFormat, format}
	if args.Dir != "" {
		cli = append(cli, flagDir, args.Dir)
	}
	if args.Write {
		cli = append(cli, "--write")
	}
	var buf bytes.Buffer
	if err := installer.Run(ctx, cli, strings.NewReader(""), &buf); err != nil {
		return runResult(buf.String(), err)
	}
	return textResult(buf.String()), nil, nil
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
	cli := []string{cliRecipe, "list", flagDir, args.Dir, flagFormat, format}
	if args.Dir == "" {
		cli = []string{cliRecipe, "list", flagFormat, format}
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
	cli := []string{cliRecipe, "describe", "--format", format}
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
		return nil, nil, errors.New("unit, dir and to are all required")
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
