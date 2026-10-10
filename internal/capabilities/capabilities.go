// SCOPE:layer=infra,removal=plugin — Capability registry: single source
// of truth for ids, kinds, runtime switches, and owned paths (consumed by
// cmd/gogogo, docs, and --help).
package capabilities

// Kind follows the servant principle, not the directory: a plugin serves
// other capabilities (nothing user-facing disappears, but something stops
// working — jobs, metering, sounds, skins); a feature is a terminal user
// surface (a page or journey disappears — todo, whiteboard, landing,
// config). Directories do not decide kinds (features/credits is a plugin:
// it serves todo metering, not users directly).
type Kind string

const (
	KindCore    Kind = "core"
	KindPlugin  Kind = "plugin"
	KindFeature Kind = "feature"
)

// Capability is one removable-or-toggleable unit of the template.
// Dirs and Files are repo-relative paths owned by the capability; the
// conformance test fails when a package under internal/ or features/ is
// owned by nobody, and cmd/gogogo refuses unknown ids.
type Capability struct {
	ID         string   `json:"id"`
	Kind       Kind     `json:"kind"`
	Summary    string   `json:"summary"`
	RuntimeOff string   `json:"runtimeOff,omitempty"`
	Offered    bool     `json:"offered"`
	Reason     string   `json:"reason,omitempty"`
	DependsOn  []string `json:"dependsOn,omitempty"`
	// UISignal names the todo.Signals bool field that hides this
	// capability's UI ("" when the capability has no gated UI). The
	// conformance test asserts the field exists and at least one .templ
	// reads it: no dead buttons after trim, by construction.
	UISignal string   `json:"uiSignal,omitempty"`
	Dirs     []string `json:"dirs,omitempty"`
	Files    []string `json:"files,omitempty"`
	Warns    []string `json:"warnings,omitempty"`
	Note     string   `json:"note,omitempty"`
}

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
		Summary: "goqite background jobs + SSE Hub + workers + retry",
		Dirs:    []string{"internal/queue"},
		Note:    "Core: every async path flows through it. Customize, never remove.",
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
		Summary: "shared backend boot (PocketBase + queue + router + handlers)",
		Dirs:    []string{"internal/server"},
		Note:    "Core: cmd/web and cmd/desktop both boot through it.",
	},
	{
		ID:      "routeutil",
		Kind:    KindCore,
		Summary: "shared route registration (per-method, never Router.Any)",
		Dirs:    []string{"internal/routeutil"},
		Note: "Core: Router.Any() registers a method-less pattern, which conflicts " +
			"with the app's own GET / and panics at ServeMux build time. Every " +
			"proxied route registers through here so a method list exists once.",
	},
	{
		ID:      "database",
		Kind:    KindCore,
		Summary: "PocketBase setup + collection seeds",
		Dirs:    []string{"db"},
		Note:    "Core: collections and demo seeds live here.",
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
		Kind:    KindCore,
		Summary: "AppContext (cross-cutting deps bundle)",
		Dirs:    []string{"features/app"},
		Note:    "Core: handlers receive it, never remove.",
	},
	{
		ID:      "auth",
		Kind:    KindCore,
		Summary: "cookie sessions + middleware (login UI removable separately)",
		Dirs:    []string{"features/auth"},
		Note:    "Core middleware; the login page is a removable UI (manual).",
	},
	{
		ID:         "nats",
		Kind:       KindPlugin,
		Summary:    "NATS JetStream: cross-instance broadcast + CRUD proxy + leaf nodes",
		RuntimeOff: "NATS_ENABLED=false (in-memory fallback)",
		Dirs:       []string{"internal/nats"},
		Files:      []string{"router/realtime_jet.go"},
		Reason:     "15+ importers carry NATS types in signatures; deletion is a manual refactor, not line strips.",
	},
	{
		ID:         "dagnats",
		Kind:       KindPlugin,
		Summary:    "durable workflows (DagNats over JetStream) + onboarding worker",
		RuntimeOff: "DAGNATS_ENABLED=false",
		Offered:    true,
		UISignal:   "DagNatsEnabled",
		Dirs:       []string{"internal/dagnats"},
		Files: []string{
			"router/onboarding_dagnats.go",
			"router/dagnats_proxy_dagnats.go",
			"router/dagnats_proxy_test.go",
			"router/export_test.go",
			"features/todo/handlers/onboarding.go",
			"features/todo/handlers/todo_update_job_dagnats.go",
			// Both onboarding test files import internal/dagnats, so trimming
			// the unit must remove them too — otherwise the proof build fails
			// with a dangling import (`module .../internal/dagnats: not found`).
			// onboarding_lifecycle_test.go was missing from this list and
			// reproduced exactly that. See TestManifestCoversDagnatsImports.
			"features/todo/handlers/onboarding_resume_test.go",
			"features/todo/handlers/onboarding_lifecycle_test.go",
			"features/todo/onboarding_e2e_test.go",
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
			"truth). onboarding_progress.go stays: todo_sse.go needs " +
			"its constants.",
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
		ID:      "collab",
		Kind:    KindPlugin,
		Summary: "Loro CRDT transport: DocStore, sync workers, presence",
		Dirs:    []string{"internal/collab"},
		Files:   []string{"router/collab_jetstream.go"},
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
		Summary: "AG-UI-compatible event envelope for AI streams (framing subset; no new transport, no new dep)",
		Dirs:    []string{"internal/genui", "features/genui"},
		Files:   []string{"router/genui.go"},
		Reason:  "No installer unit yet: nothing to trim, nothing to add back — the envelope rides the queue's SSE Hub.",
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
		Files:      []string{"router/credits.go"},
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
		ID:         "skins",
		Kind:       KindPlugin,
		Summary:    "pluggable UI skins (DaisyUI + Basecoat)",
		RuntimeOff: "UI_SKIN=daisyui (runtime selection IS the off switch)",
		Offered:    true,
		Dirs:       []string{"web/skins/basecoat"},
		Warns: []string{
			"?skin=basecoat will fall back to DaisyUI with a " +
				"warning log.",
			"Static bundles stay embedded but unreferenced " +
				"(web/resources/static/basecoat.min.*): delete them with " +
				"the `css-basecoat` Makefile target when css-check is " +
				"green without them.",
		},
		Note: "DaisyUI stays registered so the dispatcher fallback never fires.",
	},
	{
		ID:         "entity-store",
		Kind:       KindPlugin,
		Summary:    "pluggable persistence (pb | crdt EntityStore strategies)",
		RuntimeOff: "ENTITY_STORE=pb|crdt",
		Dirs:       []string{"features/store"},
		Reason:     "Strategy switch at runtime; deletion means editing the buildTodoStore switch (manual).",
	},
	{
		ID:         "offline-sync",
		Kind:       KindPlugin,
		Summary:    "hybrid offline sync (SW queue + NATS CRUD proxy + idempotency)",
		RuntimeOff: "OFFLINE_SYNC_ENABLED=false",
		Reason:     "Spans a config struct field, sw.js, and templ registration; deletion is manual.",
	},
	{
		ID:      "todo",
		Kind:    KindFeature,
		Summary: "Todo MVC demo (reference implementation)",
		Dirs:    []string{"features/todo"},
		Reason:  "Reference implementation — remove manually later per docs/scope-taxonomy.md.",
	},
	{
		ID:         "goakt",
		Kind:       KindPlugin,
		Summary:    "entity actors (GoAkt rooms: roster + presenter lock)",
		RuntimeOff: "GOAKT_ENABLED=false",
		Offered:    true,
		Dirs:       []string{"internal/goakt"},
		Files: []string{
			"router/room_goakt.go",
			"cmd/web/goakt.go",
			// Test files outside owned dirs ride along, or trimming
			// leaves a test referencing a deleted package (the dagnats
			// precedent: onboarding_*_test.go).
			"cmd/web/goakt_test.go",
			"router/room_goakt_internal_test.go",
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
		Files: []string{
			"router/whiteboard.go",
		},
		Warns: []string{
			"The navbar Whiteboard link is stripped automatically.",
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
		Files: []string{
			"router/notes.go",
		},
		Note: "Manual removal (or trim the whiteboard unit, which covers " +
			"whiteboard+collab+notes): delete the dir + router/notes.go, " +
			"drop registerNotesStack from router.Init, remove the Notes " +
			"navbar link in features/auth/views.templ, delete " +
			"ensureNotesCollection in db/seed.go, and remove this entry.",
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
}

// depSounds is the shared soft dependency of UI pages: their .templ
// files import the sounds package for SoundAssets.
const depSounds = "sounds"

// depCollab is the shared transport dependency of collab consumers
// (whiteboard, notes): the server-owned Loro DocStore + sync workers.
const depCollab = "collab"

// ByID indexes All by capability id.
func ByID() map[string]Capability {
	m := make(map[string]Capability, len(All))
	for _, c := range All {
		m[c.ID] = c
	}
	return m
}

// OfferedIDs returns the ids the installer may delete, in registry order.
func OfferedIDs() []string {
	var ids []string
	for _, c := range All {
		if c.Offered {
			ids = append(ids, c.ID)
		}
	}
	return ids
}
