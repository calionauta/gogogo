// SCOPE:plugin — Capability registry entries (data only)
//
// Split from capabilities.go for the file-sizes gate: the type + API live
// there, the table lives here, so the table can grow a capability without
// pushing the file over the limit.
package capabilities

// All is the full capability map. Installer units cover a subset (see
// Offered); the rest are env-blessed, core, or manual-removal with the
// reason recorded.
var All = []Capability{
	{
		ID:      "capabilities",
		Kind:    KindPlugin,
		Summary: "capability registry + installer engine (CLI and MCP share it)",
		Dirs:    []string{"internal/capabilities", "internal/installer"},
		Note:    "Meta-capability: deleting it breaks cmd/gogogo, not the web binary.",
	},
	{
		ID:      "queue",
		Kind:    KindCore,
		Summary: "SSE Hub (realtime fan-out) + goqite jobs + workers + retry",
		Dirs:    []string{"internal/queue"},
		Note: "Core, and the Hub is the load-bearing half: every streaming " +
			"handler (todo, genui, chat, notes, whiteboard, review, room) " +
			"names *queue.SSEHub in its constructor, so no trimmed tree " +
			"compiles without it — even with NATS off. The job queue shares " +
			"the package because workers stream through the same hub.",
	},
	{
		ID:   "shell",
		Kind: KindCore,
		Summary: "one app shell (sidebar + module header + work area), the theme " +
			"catalog, and the skin registry that supplies its assets",
		Dirs: []string{"internal/ui", "internal/themes", "web/skins"},
		Note: "Core: every module page renders a body inside ui.Shell, and the " +
			"shell resolves the active skin's assets. One skin ships (DaisyUI); " +
			"visual variants are DaisyUI themes (docs/ui-skins.md), not bundles.",
	},
	{
		ID:      "secrets",
		Kind:    KindCore,
		Summary: "age-decrypted env loader",
		Dirs:    []string{"internal/secrets"},
		Note:    "Core: config.Load reads through it. Customize, never remove.",
	},
	{
		ID:      "server",
		Kind:    KindCore,
		Summary: "flat backend boot (Turso + auth + handlers + queues + workers)",
		Dirs:    []string{"internal/server"},
		Note: "Core: cmd/web and cmd/desktop both boot through BootChi/StartChi. " +
			"attach JetStream, then serve.",
	},
	{
		ID:      "database",
		Kind:    KindCore,
		Summary: "flat data layer: Turso Database connection + migrations + sqlc queries",
		Dirs:    []string{"internal/db", "internal/dbschema"},
		Note: "Core: handlers call the generated dbgen.* functions directly; " +
			"migrations under internal/dbschema/migrations are the schema (driver-free " +
			"so zero-CGO targets share it; internal/db owns only the tursogo open).",
	},
	{
		ID:      "appconfig",
		Kind:    KindCore,
		Summary: "env config (single source of truth for vars + defaults)",
		Dirs:    []string{"config"},
		Note:    "Core: every env var is documented here.",
	},
	{
		ID:      "app",
		Kind:    KindPlugin,
		Summary: "AppContext (dependency bundle for the native window)",
		Dirs:    []string{"features/app"},
		Note: "Plugin, not core: cmd/web never links it — handlers take " +
			"their deps directly, so only cmd/gui (the gogpu/ui proof) reads " +
			"Context. Deleting the package drops that second frontend, never " +
			"the web app.",
	},
	{
		ID:      "demo-seed",
		Kind:    KindCore,
		Summary: "fresh-install demo account (one idempotent Signup)",
		Dirs:    []string{"internal/demo"},
		Note: "Core contract: a fresh clone has a way in. Boot (internal/server) " +
			"and the native window both call it; Signup is idempotent.",
	},
	{
		ID:      "instance",
		Kind:    KindCore,
		Summary: "stable instance identity for cross-instance durables and event origins",
		Dirs:    []string{"internal/instance"},
		Note: "Core: the CRUD consumer's durable and every published event carry " +
			"it (APP_INSTANCE_ID, else the hostname).",
	},
	{
		ID:      "auth",
		Kind:    KindCore,
		Summary: "cookie sessions + middleware (flat auth + shared login UI)",
		Dirs:    []string{"internal/auth", "features/authviews"},
		Note: "Core middleware; the login page is a removable UI (manual). " +
			"internal/auth is the owned argon2id stack (gogogo_auth cookie); " +
			"features/authviews holds the login/navbar/theme components. " +
			"No framework middleware remains; the owned stack is the whole story.",
	},
	{
		ID:      "crudwire",
		Kind:    KindCore,
		Summary: "todo CRUD wire protocol (subjects + payloads + publisher)",
		Dirs:    []string{"internal/natswire"},
		Note: "Core, not plugin: chi handlers name these types in signatures " +
			"even with NATS off (nil publisher), so trimming would not compile. " +
			"The engines behind it (internal/nats) stay a plugin.",
	},
	{
		ID:      "hostgate",
		Kind:    KindCore,
		Summary: "host-capacity gate for heavy Go tests (skip on saturated boxes)",
		Dirs:    []string{"internal/hostgate"},
		Note: "Core test support: heavy suites (trim-build repo copies + " +
			"forked vets) skip with reason off-CI when the box is constrained, " +
			"instead of wedging a shared host. CI never skips. Customize " +
			"thresholds via HOSTGATE_* env, never remove.",
	},
	{
		ID:         "nats",
		Kind:       KindPlugin,
		Summary:    "NATS JetStream: cross-instance broadcast + CRUD proxy + leaf nodes",
		RuntimeOff: "NATS_ENABLED=false (in-memory fallback)",
		Dirs:       []string{"internal/nats"},
		Reason:     "15+ importers carry NATS types in signatures; deletion is a manual refactor, not line strips.",
	},
	{
		ID:         "dagnats",
		Kind:       KindPlugin,
		Summary:    "durable workflows (DagNats over JetStream) + onboarding worker",
		RuntimeOff: "DAGNATS_ENABLED=false",
		Offered:    true,
		UISignal:   "DagNatsEnabled",
		Dirs:       []string{"internal/dagnats", "features/onboarding"},
		Files: []string{
			// Every file that imports internal/dagnats: trimming the unit
			// must remove them too — otherwise the proof build fails with a
			// dangling import (`module .../internal/dagnats: not found`).
			// The todo slice keeps only its wire constants and renderers,
			// which import nothing from the engine, so it needs no entry.
			"internal/nats/single_nats_test.go",
			"cmd/web/dagnats.go",
			"cmd/web/start_nats_test.go",
		},
		Warns: []string{
			"Runtime alternative (no deletion): DAGNATS_ENABLED=false.",
		},
		Note: "Deletion removes the engine + onboarding worker. " +
			"The workflow tab, button, and hints auto-hide via the " +
			"dagnatsUIEnabled signal (registration truth, not config " +
			"truth).",
	},
	{
		ID:         "llm",
		Kind:       KindPlugin,
		Summary:    "GoAI LLM client behind an injectable interface",
		RuntimeOff: "Unset GOAI_API_KEY (suggest button hides)",
		UISignal:   "LLMEnabled",
		Dirs:       []string{"internal/llm"},
		Reason: "Load-bearing in core structs: features/app AppContext holds " +
			"LLM *llm.Client (plus llm.Cfg/llm.Queue globals), TodoHandler calls " +
			"llm methods directly, and credits implements llm.Biller (cascade). " +
			"The goai dependency is shared with credits and internal/queue, so " +
			"deletion saves source only, never deps. Runtime-off is the blessed path.",
	},
	{
		ID:      "genkit",
		Kind:    KindPlugin,
		Summary: "typed AI flows (Genkit Go): orchestration + traces above internal/llm",
		Reason: "Single consumer (review): trimming the review unit plus " +
			"`go mod tidy` drops github.com/firebase/genkit/go from the " +
			"build. Generation reaches providers only through llm.Client, " +
			"so no provider SDK rides along. Runtime-off needs no flag — " +
			"without GOAI_API_KEY the flows 503 and the UI hides.",
	},
	{
		ID:      "collab",
		Kind:    KindPlugin,
		Summary: "Loro CRDT transport: DocStore, sync workers, presence",
		Dirs:    []string{"internal/collab"},
		Note: "Shared by whiteboard + notes on the flat stack (their chi " +
			"slices wire the transport). Snapshot durability is runtime: " +
			"COLLAB_SNAPSHOTS=memory keeps snapshots process-local (draw " +
			"without Turso writes; docs start empty after a restart). " +
			"Files is empty on purpose: the " +
			"importers outside internal/collab are EDITED (line drops), " +
			"never deleted — internal/server/chi.go is core.",
		Reason: "Bundled into the whiteboard unit: whiteboard embeds collab " +
			"types (no-compile without it) and collab without whiteboard " +
			"was dead until notes arrived as the second consumer " +
			"(features/notes shares the transport on its own subject " +
			"space). Split trigger met — see the notes entry: trimming " +
			"the whiteboard bundle removes collab, which notes needs.",
	},
	{
		ID:      "datastar",
		Kind:    KindPlugin,
		Summary: "Datastar rendering helpers",
		Dirs:    []string{"internal/datastar"},
		Reason:  "Every .templ page renders through it; deletion is manual.",
	},
	{
		ID:      "genui",
		Kind:    KindPlugin,
		Summary: "generative UI: LLM answers render as Templ directives over the SSE hub",
		Offered: true,
		Dirs:    []string{"features/genui"},
		// Owned by no dir: the composition test's model stubs, and the todo
		// feature's half of the panel seam (all of them would otherwise outlive
		// the unit and trip the deadcode or import gate).
		Files: []string{
			"internal/server/genui_stubs_test.go",
			// The panel seam: four files that exist ONLY to let the app answer
			// with its own todos through this catalog. Nothing else in
			// features/todo references them, so without genui they are dead code
			// (the deadcode gate would report them, the repo forbids legacy, and
			// the provider they implement has no registry left to register
			// into). Their one caller (`NewTodoPanel` in bootAI) is removed by
			// this unit's strip rule, which is what makes deleting them compile.
			"features/todo/handlers/genui_panel.go",
			"features/todo/handlers/genui_panel_test.go",
			"features/todo/handlers/genui_owner_test.go",
			"features/todo/panel.go",
			"features/todo/panel_filter_test.go",
			"features/todo/components/panel.templ",
			"features/todo/components/panel_templ.go",
		},
		Warns: []string{
			"ci-local smoke: export SMOKE_FEATURES without genui " +
				"(e.g. SMOKE_FEATURES=todo,room,login) or the harness fails on the trimmed route.",
		},
		Note: "Deletion removes the Ask demo + chat demo (one bundle: chat " +
			"imports genui components, so chat without genui does not " +
			"compile — same honest shape as whiteboard+collab+notes). " +
			"The navbar Ask AI link is stripped automatically.",
	},
	{
		ID:      "components",
		Kind:    KindPlugin,
		Summary: "shared UI helpers (Toast + OfflineBanner) + skin imports",
		Dirs:    []string{"internal/components", "features/todo/components"},
		Reason:  "Every page layout calls into it; deletion is manual.",
	},
	{
		ID:         "credits",
		Kind:       KindPlugin,
		Summary:    "AI credits + BYOK ledger (ai-credits)",
		RuntimeOff: "CREDITS_ENABLED=false (already the default)",
		Offered:    true,
		Dirs:       []string{"features/credits"},
		Warns: []string{
			"Todo AI Suggest becomes unmetered (still works with " +
				"GOAI_API_KEY, no ledger).",
		},
		Note: "If Stripe top-ups were the only stripe-go user, " +
			"`go mod tidy` drops stripe-go too.",
	},
	{
		ID:      depSounds,
		Kind:    KindPlugin,
		Summary: "UI sound feedback (cuelume, vendored)",
		Offered: true,
		Dirs:    []string{"features/sounds", "web/resources/static/cuelume"},
		Files:   []string{"web/resources/static/cuelume.js"},
		Warns: []string{
			"data-cuelume-* attributes stay in the markup but are inert " +
				"without cuelume.js.",
			"The installer re-runs `go tool templ generate`; if that " +
				"fails, run `make templ` manually before building.",
		},
		Note: "Call sites are stripped automatically (this used to be manual).",
	},
	{
		ID:      "search",
		Kind:    KindPlugin,
		Summary: "search layers (substring, fuzzy, BM25) behind one seam",
		Offered: true,
		Dirs:    []string{"internal/search"},
		// Every file that imports internal/search goes here, or trimming
		// leaves a dangling import: the consumer half of the seam, its test,
		// the boot heal, and the composition-root wiring. The handler's search
		// SLICE (features/todo/handlers/search.go) deliberately does NOT — it
		// holds a stdlib-only interface, so it survives and the endpoint
		// keeps answering with the unfiltered list (the degrade path).
		Files: []string{
			"features/todo/handlers/search_source.go",
			"features/todo/handlers/search_source_test.go",
			"features/todo/handlers/search_test.go",
			"features/todo/handlers/search_mode_test.go",
			"features/todo/handlers/search_heal.go",
			"features/todo/handlers/search_heal_test.go",
			"internal/search/sqlindex_inventory.go",
			"internal/server/boot_search.go",
		},
		Warns: []string{
			"A consumer that loses this keeps the SQL substring layer: it " +
				"holds a search.Searcher and degrades (that is why it is swappable).",
		},
		Note: "No external engine on purpose: this driver ships no FTS5 and no " +
			"Turso native FTS (tursogo v0.8.2/v0.8.3/v0.9.0-pre.1), and a " +
			"second index store would put a row's truth in two places.",
	},
	{
		ID:         "offline-sync",
		Kind:       KindPlugin,
		Summary:    "hybrid offline sync (SW queue + NATS CRUD proxy + idempotency)",
		RuntimeOff: "OFFLINE_SYNC_ENABLED=false",
		// The unit used to own no path at all, which made its removal story
		// unverifiable: sw.js IS the implementation (the banner + templ
		// registration live in internal/components, owned by that unit).
		Files:  []string{"web/resources/static/sw.js"},
		Reason: "Spans a config struct field, sw.js, and templ registration; deletion is manual.",
	},
	{
		ID:      "todo",
		Kind:    KindFeature,
		Summary: "Todo MVC demo (reference implementation)",
		Dirs:    []string{"features/todo"},
		Reason:  "Reference implementation — remove manually later per docs/scope-taxonomy.md.",
	},
	{
		ID:        "chat",
		Kind:      KindFeature,
		Summary:   "conversation demo (persisted history + catalog answers on genui)",
		DependsOn: []string{"genui"},
		Dirs:      []string{"features/chat"},
		Reason: "Bundled into the genui unit: chat imports genui components " +
			"(no-compile without it) and genui without chat is unproven " +
			"surface — one unit is the honest shape, same as " +
			"whiteboard+collab+notes.",
	},
	{
		ID:        "review",
		Kind:      KindFeature,
		Summary:   "review loop demo (writer → critic → classifier judge, streamed verdict)",
		Offered:   true,
		DependsOn: []string{"genkit"},
		Dirs:      []string{"features/review"},
		Warns: []string{
			"The navbar Review link is stripped automatically.",
			"ci-local smoke: export SMOKE_FEATURES without review " +
				"(e.g. SMOKE_FEATURES=todo,room,login) or the harness fails on the trimmed route.",
		},
		Note: "No UISignal claim by design: the run UI gates on the " +
			"todo.Signals ReviewEnabled field at runtime (keyless hides " +
			"it), but the only .templ reader ships inside features/review " +
			"— claiming it would fail the trimmed-tree conformance, the " +
			"same reason SimulatedLLM is unclaimed. Trim removes the " +
			"navbar link, so no dead UI either way. " +
			"DependsOn lists genkit only: llm is never trimmed " +
			"(runtime-off is blessed), so declaring it would break " +
			"`gogogo add review` (add fails on deps no unit provides) " +
			"for zero mechanical gain — the LLM requirement rides the " +
			"Summary and docs instead.",
	},
	{
		ID:         "goakt",
		Kind:       KindPlugin,
		Summary:    "entity actors (GoAkt rooms: roster + presenter lock)",
		RuntimeOff: "GOAKT_ENABLED=false",
		Offered:    true,
		Dirs:       []string{"internal/goakt", "features/room"},
		Files: []string{
			// Owned by no dir: the engine boot in the web entry point.
			// internal/server/chi.go is EDITED (line drops), not deleted.
			"cmd/web/goakt.go",
			"cmd/web/goakt_test.go",
		},
		Warns: []string{
			"Runtime alternative (no deletion): GOAKT_ENABLED=false.",
		},
		Note: "Deletion removes the engine + room demo. The navbar Room " +
			"link is stripped automatically; main.go boot lines go with " +
			"the unit's mainStrips.",
	},
	{
		ID:      "room",
		Kind:    KindFeature,
		Summary: "room presence demo (one grain per room, crash hook included)",
		Offered: true,
		Dirs:    []string{"features/room"},
		Warns: []string{
			"The navbar Room link is stripped automatically.",
		},
	},
	{
		ID:      "whiteboard",
		Kind:    KindFeature,
		Summary: "collaborative canvas (Loro + Rough.js + presence)",
		Offered: true,
		DependsOn: []string{
			depCollab,
			depSounds,
		},
		Dirs: []string{"features/whiteboard"},
		Warns: []string{
			"The navbar Whiteboard link is stripped automatically.",
			"ci-local smoke: export SMOKE_FEATURES without whiteboard " +
				"(e.g. SMOKE_FEATURES=todo,room,login) or the harness fails on the trimmed route.",
		},
	},
	{
		ID:      "notes",
		Kind:    KindFeature,
		Summary: "shared plain-text notes (server-owned Loro Text + SSE)",
		DependsOn: []string{
			depCollab,
			depSounds,
		},
		Dirs: []string{"features/notes"},
		Note: "Manual removal (or trim the whiteboard unit, which covers " +
			"whiteboard+collab+notes): delete the dir + the notes roots in " +
			"buildChiMux (internal/server/chi.go), remove the Notes " +
			"navbar link in features/authviews/navbar.templ, and remove " +
			"this entry.",
	},
	{
		ID:      "landing",
		Kind:    KindFeature,
		Summary: "public marketing page on GET /",
		Offered: true,
		DependsOn: []string{
			depSounds,
		},
		Dirs: []string{"features/landing"},
		Warns: []string{
			"GET / will 404 (landing owned the only root route); " +
				"the navbar brand is retargeted to /todo, which is kept.",
		},
	},
	{
		ID:      "config-view",
		Kind:    KindFeature,
		Summary: "auth-gated read-only /config view",
		Offered: true,
		DependsOn: []string{
			depSounds,
		},
		Dirs: []string{"features/config"},
		Warns: []string{
			"The navbar Config link is stripped automatically.",
		},
	},
	{
		ID:      "observability",
		Kind:    KindCore,
		Summary: "core observability (slog + tracing)",
		Dirs:    []string{"internal/observability"},
		Note:    "core infra, removal=core",
	},
	{
		ID:      "rbac",
		Kind:    KindCore,
		Summary: "RBAC with Casbin (embedded model + policy, enforced at the mux)",
		Dirs:    []string{"internal/rbac"},
		Note: "Wired middleware, not a dormant package: BootChi builds one " +
			"enforcer and wraps the whole mux (pass-through when " +
			"RBAC_ENABLED=false, enforced denies with 403 + warn log when " +
			"true). Roles come from users.role (migration 0008, ADMIN_EMAILS " +
			"promotes existing rows at boot). /config is the admin-only " +
			"surface; the policy test pins every mux route for user-or-admin.",
	},
}
