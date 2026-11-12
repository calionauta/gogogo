package main

import "github.com/modelcontextprotocol/go-sdk/mcp"

var toolCatalog = []toolDef{
	{
		"capabilities_list",
		"Raw registry JSON: every template capability with id, kind, summary, and off-switch. Registry " +
			"only — takes no path, so it answers from any directory. Use it to look up an id or off-switch; " +
			"use advise_stack to decide WHICH ones apply.",
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
		"Preview scaffolding a checkout (validates name and owner, shows the " +
			"plan envelope). The scaffold ships the whole template — no keep/drop " +
			"list exists — and changes nothing. Requires a gogogo checkout at `dir` unless it " +
			"does not exist yet (then it previews the clone) — on an unrelated project " +
			"it cannot preview anything, so call advise_stack for guidance instead.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleTrimPlan) },
	},
	{
		"trim_apply",
		"Scaffold a checkout (rename, prove with build). Requires " +
			"confirm:true. Mutates the target tree; only for a gogogo checkout or a " +
			"path to scaffold into.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleTrimApply) },
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
		"export_unit",
		"Read-only PORT BUNDLE for a tree that is not a gogogo checkout " +
			"(a tree this build does not scaffold): copies the unit's owned files " +
			"into the scratch `to` directory and writes PORTING.md with the measured " +
			"wiring — routes it registers, imports to rewrite, what YOU must write " +
			"by hand — plus the proof commands. Changes nothing in `dir` and nothing " +
			"outside `to`; apply it deliberately, it is a copy and not an installer.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleExportUnit) },
	},
	{
		"new_feature",
		"Scaffold one new feature inside a gogogo checkout: handler + page, " +
			"composition-root wiring, capability unit, RBAC rows, then templ generate, " +
			"generated docs, registry conformance, check-flatdeps and a build proof. Requires " +
			"confirm:true (or dryRun:true to preview). Needs `dir` (a scaffolded " +
			"checkout) and `name` (kebab-case, unused).",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleNewFeature) },
	},
	{
		"recipe_install",
		"Install a recipe package (tree plus .recipe.yml) into a gogogo checkout: copy, " +
			"receipt, generated registration, schema/policy/codegen and proof. Mutates the target directory. " +
			"Requires confirm:true plus src (recipe directory or id) and dir; use dryRun:true to preview. " +
			"For a catalog entry, also provide entry and the pristine contents directory.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleRecipeInstall) },
	},
	{
		"recipe_update",
		"Update an already-installed recipe from a recipe directory or catalog entry. Same guards as recipe_install: " +
			"requires confirm:true (or dryRun:true to preview), plus src and dir.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleRecipeUpdate) },
	},
	{
		"recipe_uninstall",
		"Uninstall one installed recipe by id: removes only pristine files, keeps edited or derived state, " +
			"updates registration/policy/schema surfaces and marks the receipt removed. Requires confirm:true; " +
			"without it returns the removal plan.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleRecipeUninstall) },
	},
	{
		"recipe_status",
		"Read-only recipe drift report: pristine vs edited copy, generated/schema/policy state, or one recipe by id. " +
			"Changes nothing and needs no confirmation.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleRecipeStatus) },
	},
	{
		"recipe_outdated",
		"Read-only recipe version check: compares each installed recipe's recorded version against the latest catalog version (catalog/<id>/<version>.yaml, default <dir>/catalog). A recipe behind the latest is a verdict, not an error; receipts without a version or recipes without a catalog entry report unknown.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleRecipeOutdated) },
	},
	{
		"recipe_list",
		"Read-only recipe discovery: every built-in capability plus every user-installed recipe, with " +
			"origin, kind, summary, installed/removed state and the invocable verbs. Dynamic: an install " +
			"appears here without a help edit. Changes nothing.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleRecipeList) },
	},
	{
		"recipe_describe",
		"Describe one recipe by id: kind, origin, summary, registration surface, dependencies, " +
			"invocable verbs with a description, per-param help, and the metrics/outcomes it already " +
			"declares (job, beneficiary, counter signals). Read this before recipe_invoke so you choose " +
			"a verb the recipe actually declares, and before proposing instrumentation so you extend an " +
			"existing count instead of inventing a second one. Changes nothing.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleRecipeDescribe) },
	},
	{
		"recipe_invoke",
		"Invoke one declared verb (read or mutating) of a recipe against a running server " +
			"(loopback only): runs it through the server's own session and policy stack with the " +
			"operator token. This is the developer/operator path; a product's own clients use the " +
			"server's API. A mutating verb changes server state, so read recipe_describe first.",
		func(s *mcp.Server, t *mcp.Tool) { mcp.AddTool(s, t, handleRecipeInvoke) },
	},
	{
		"advise_stack",
		"Opinions, not changes: which units to keep for a use-case, how each " +
			"switches off, and the exact command ROUTE for the goal. " +
			"Empty need returns the full map; no preset matched means decide from the " +
			"capabilities table or retry with broader terms. Business-language fit per " +
			"unit: https://calionauta.github.io/gogogo/docs/use-cases/. Start here when " +
			"deciding. Pass `intent` (update|port|new-feature|scaffold) to get " +
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
