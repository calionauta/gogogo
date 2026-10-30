package main

import "github.com/modelcontextprotocol/go-sdk/mcp"

var toolCatalog = []toolDef{
	{
		"capabilities_list",
		"Raw registry JSON: every template capability with id, kind, scaffold " +
			"class (core/default/opt-in/manual), summary, and off-switch. Registry " +
			"only — takes no path, so it answers from any directory. Use it to look " +
			"up an id/class/off-switch; use advise_stack to decide WHICH ones apply.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleCapabilitiesList) },
	},
	{
		"build_prerequisites",
		"Read-only: how to build the desktop shell or the Android target on this " +
			"host. Pass `platform` desktop|mobile (empty returns both). Covers " +
			"prerequisites, the build/smoke commands, the Android driver swap " +
			"(tursogo -> ncruces) and the mobile limits. Same text as `gogogo --help`.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleBuildPrerequisites) },
	},
	{
		"sync_prerequisites",
		"Read-only: what a desktop build needs to sync with a server (a reachable " +
			"NATS leaf listener, NATS_TOKEN, CLOUD_SYNC_URL, account identity, a " +
			"private overlay first). Same guidance as `gogogo --help`; the normative " +
			"recipe is docs/desktop-mobile.md#cloud-sync. Takes no arguments and " +
			"changes nothing.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleSyncPrerequisites) },
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
			"Defaults are kept unless dropped; opt-in units (perf) are dropped unless " +
			"named in optIn. Changes nothing. Requires a gogogo checkout at `dir` unless it does not " +
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
