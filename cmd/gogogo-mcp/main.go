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

var toolCatalog = []toolDef{
	{
		"capabilities_list",
		"Raw registry JSON: every template capability with id, kind, summary, and " +
			"off-switch. Registry only — takes no path, so it answers from any " +
			"directory. Use it to look up an id/off-switch; use advise_stack to " +
			"decide WHICH ones apply.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleCapabilitiesList) },
	},
	{
		"version_status",
		"Read-only version check: compares this tool AND the project's recorded " +
			"template revision (the scaffolded-from: line in its AGENTS.md) against " +
			"the latest release. Pass `dir` to get the project half — without it, only " +
			"the tool is compared, which reports a months-old tree as current. An " +
			"update existing is a VERDICT, not a failure. Never runs on its own: nothing " +
			"here phones home unless you call it. To see what actually changed, use " +
			"CLI `gogogo migrate`.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleVersionStatus) },
	},
	{
		"trim_plan",
		"Preview scaffolding/trimming a checkout (validates ids, shows consequences). " +
			"Changes nothing. Requires a gogogo checkout at `dir` unless it does not " +
			"exist yet (then it previews the clone) — on an unrelated project it " +
			"cannot preview anything, so call advise_stack for guidance instead. " +
			"Decide what to keep with advise_stack first (business-language fit: " +
			"https://calionauta.github.io/gogogo/docs/use-cases/).",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleTrimPlan) },
	},
	{
		"trim_apply",
		"Scaffold/trim a checkout (rename, trim, prove with build). Requires " +
			"confirm:true. Mutates the target tree; only for a gogogo checkout or a " +
			"path to scaffold into.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleTrimApply) },
	},
	{
		"check_tree",
		"Verify installer markers without changing anything (drift gate). Requires " +
			"a gogogo checkout at `dir`: it compares the trim manifest against the " +
			"tree. On a project that is not a gogogo checkout it returns a single " +
			"'not a gogogo checkout' failure rather than a marker inventory — do not " +
			"read that as the project being broken.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleCheckTree) },
	},
	{
		"migrate_plan",
		"Read-only migration list: every release between the project's recorded " +
			"base revision (the scaffolded-from: line in its AGENTS.md) and the latest, " +
			"plus the gates that prove a migrated tree conforms. This is the AI-readable " +
			"diff between 'the template you copied' and 'the template now' — apply it " +
			"deliberately; nothing here edits the tree. CLI equivalent: `gogogo migrate`.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleMigratePlan) },
	},
	{
		"add_unit",
		"Add one template unit to a checkout (deps, rebase, proof). Requires " +
			"confirm:true, and needs BOTH `dir` (a gogogo checkout) and `from` " +
			"(a pristine template copy to copy the unit's files from).",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleAddUnit) },
	},
	{
		"export_unit",
		"Read-only PORT BUNDLE for a tree that is not a gogogo checkout (where " +
			"add_unit refuses): copies the unit's owned files into the scratch `to` " +
			"directory and writes PORTING.md with the measured wiring — routes it " +
			"registers, imports to rewrite, what YOU must write by hand — plus the " +
			"proof commands. Changes nothing in `dir` and nothing outside `to`; " +
			"apply it deliberately, it is a copy and not an installer.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleExportUnit) },
	},
	{
		"new_feature",
		"Scaffold one new feature inside a gogogo checkout: handler + page, " +
			"composition-root wiring, capability unit, RBAC rows, then templ generate, " +
			"generated docs, check-scope, check-flatdeps and a build proof. Requires " +
			"confirm:true (or dryRun:true to preview). Needs `dir` (a scaffolded " +
			"checkout) and `name` (kebab-case, unused).",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleNewFeature) },
	},
	{
		"advise_stack",
		"Opinions, not changes: which units to keep for a use-case, how each " +
			"switches off, and the exact command ROUTE for the goal. " +
			"Empty need returns the full map; no preset matched means decide from the " +
			"capabilities table or retry with broader terms. Business-language fit per " +
			"unit: https://calionauta.github.io/gogogo/docs/use-cases/. Start here when " +
			"deciding. Pass `intent` (update|port|add-unit|new-feature|scaffold) to get " +
			"the ordered commands for that goal; omitted = inferred from `need`, and the " +
			"answer says which it used. Pass `dir` so the answer can say whether that " +
			"path is a gogogo checkout: a tree that is NOT one gets its own scope with " +
			"the copy surface (owned paths) and the export route, instead of 'cannot'. " +
			"Answers in one of four scopes: `template` (a gogogo-shaped need, or " +
			"anything unmatched — the full map instead of a guess), " +
			"`exception-to-go` (a Zig need — the gate, nothing installs under it), " +
			"`go-standards` (a need that forbids dependencies), " +
			"`foreign-tree` (dir is not a gogogo checkout — port, do not install). " +
			"The Go coding standards it enforces are the gogogo-coding-standards skill — " +
			"https://github.com/calionauta/gogogo (install: " +
			"npx skills add calionauta/gogogo); it is not shipped in this binary.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleAdvise) },
	},
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
	Name     string `json:"name,omitempty" jsonschema:"new project name, e.g. my-app (required)"`
	Owner    string `json:"owner,omitempty" jsonschema:"GitHub owner for the module path (default calionauta)"`
	Dir      string `json:"dir,omitempty" jsonschema:"target checkout directory (default ./<name>)"`
	Plugins  string `json:"plugins,omitempty" jsonschema:"comma-separated plugins to KEEP (default all, none drops all)"`
	Features string `json:"features,omitempty" jsonschema:"comma-separated features to KEEP (default all, none drops all)"`
	Preset   string `json:"preset,omitempty" jsonschema:"named drop set instead of keep lists: minimal|no-ai|no-collab (conflicts with plugins/features)"`
	Format   string `json:"format,omitempty" jsonschema:"text or json (default json for tools)"`
	Confirm  bool   `json:"confirm,omitempty" jsonschema:"trim_apply only: must be true (preview with trim_plan first)"`
}

type migrateArgs struct {
	From      string `json:"from,omitempty" jsonschema:"base revision to start after (default: the project's recorded scaffolded-from tag)"`
	To        string `json:"to,omitempty" jsonschema:"newest revision to include (default: the latest release)"`
	Dir       string `json:"dir,omitempty" jsonschema:"project checkout to read the base marker from (default: current directory)"`
	Changelog string `json:"changelog,omitempty" jsonschema:"read the changelog from this file instead of the network (offline)"`
	Format    string `json:"format,omitempty" jsonschema:"text or json (default json for tools)"`
}

type addArgs struct {
	Unit    string `json:"unit,omitempty" jsonschema:"installer unit id, e.g. whiteboard (required)"`
	From    string `json:"from,omitempty" jsonschema:"pristine template checkout to copy from (required)"`
	Dir     string `json:"dir,omitempty" jsonschema:"target project checkout (required)"`
	Format  string `json:"format,omitempty" jsonschema:"text or json (default json for tools)"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"must be true (preview with dry-run first)"`
	DryRun  bool   `json:"dryRun,omitempty" jsonschema:"print the plan and stop when true"`
}

type newFeatureArgs struct {
	Name    string `json:"name,omitempty" jsonschema:"feature id, kebab-case, e.g. reports (required)"`
	Dir     string `json:"dir,omitempty" jsonschema:"target checkout (required)"`
	Format  string `json:"format,omitempty" jsonschema:"text or json (default json for tools)"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"must be true (preview with dryRun first)"`
	DryRun  bool   `json:"dryRun,omitempty" jsonschema:"print the plan and stop when true"`
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
	if a.Plugins != "" {
		args = append(args, "--plugins", a.Plugins)
	}
	if a.Features != "" {
		args = append(args, "--features", a.Features)
	}
	if a.Preset != "" {
		args = append(args, "--preset", a.Preset)
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
	// No --no-tui here: add/new take no TUI flag (it belongs to the root
	// scaffolder), and passing it made this tool fail at flag parse on every
	// call. The plumbing test below is what catches the next one (make check-mcp).
	cli := []string{"add", args.Unit, "--from", args.From, "--dir", args.Dir, "--format", format}
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
