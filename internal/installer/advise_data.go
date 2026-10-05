// SCOPE:layer=infra,removal=plugin — installer engine: advise data (presets, rules, stack signals)
package installer

var advisePresets = []advisePreset{
	{
		Name: "realtime-collab",
		Idea: "presence plus shared state over a persistent connection, converged with a CRDT",
		Match: []string{
			"realtime", "collaborat", "canvas", unitWhiteboard,
			"presence", "cursor", "multi-user", "multiplayer", "shared",
		},
		Keep: []string{unitWhiteboard},
		Note: "Single binary embeds NATS; multi-instance points at shared " +
			"JetStream (docs/async-layers.md). Todo realtime stays " +
			"regardless (PocketBase).",
	},
	{
		Name: "background-jobs",
		Idea: "durable queue with retry and backoff; progress streamed to the originator",
		Match: []string{
			"background", "job", "queue", "async", "email",
			"export", "retry", "worker", "cron", "scheduled",
		},
		Keep: []string{"queue (core — always in)"},
		Note: "Nothing to trim: goqite + SSE hub are core. Write jobs like " +
			"the todo handlers; >50ms of work never runs inline.",
	},
	{
		Name: "durable-workflows",
		Idea: "event-sourced steps with replay; every step idempotent",
		Match: []string{
			"workflow", "durable", "saga", "onboarding",
			unitDagnats, "steps", "orchestrat",
		},
		Keep: []string{unitDagnats},
		Note: "Console at /dagnats/. Onboarding demo is the reference; " +
			"delete the demo trigger, keep the engine.",
	},
	{
		Name: "offline-first",
		Idea: "local-first writes queued in an outbox, replayed with idempotency keys",
		Match: []string{
			"offline", "flaky", "airplane", "outbox", "sync",
			"reconnect", "pwa",
		},
		Keep: []string{
			"offline-sync (runtime — enabled by default)",
			"entity-store (runtime: ENTITY_STORE=pb|crdt)",
		},
		Note: "No trim involved: OFFLINE_SYNC_ENABLED=false is the off " +
			"switch. CRDT store only when tabs must converge without a " +
			"server round-trip.",
	},
	{
		Name: "ai-features",
		Idea: "async LLM calls off the request path; usage metered per key",
		Match: []string{
			"ai", "llm", "suggest", "chatbot", "agent", "byok",
			unitCredits, "openai", "anthropic",
		},
		Keep: []string{unitCredits},
		Note: "LLM itself is runtime (set GOAI_API_KEY); credits unit is " +
			"the BYOK accounting. Never commit a key — age file locally, " +
			"provider secrets in prod.",
	},
	{
		Name:  "admin-inspect",
		Idea:  "read-only env and data views behind auth",
		Match: []string{"admin", "config", "inspect", "dashboard", "observab"},
		Keep:  []string{unitConfigView},
		Note:  "Surfaces: /config (auth-gated env view), /_/ (PocketBase admin), /dagnats/ (workflow console).",
	},
	{
		Name:  "marketing-site",
		Idea:  "static public pages, no auth, no app state",
		Match: []string{unitLanding, "marketing", "homepage", "site", "hero"},
		Keep:  []string{unitLanding},
		Note:  "Public GET / with no auth. Brand lives here; /todo is the app behind it.",
	},
	{
		Name:  "quiet-api",
		Idea:  "delete the demo surfaces; keep auth, queue, router",
		Match: []string{"api", "headless", "backend", "minimal", "embed", "library"},
		Keep:  []string{"(core only — auth middleware, queue, router)"},
		Drop:  []string{unitLanding, unitWhiteboard, unitSounds, unitSkinsExtra},
		Note:  "Todo stays as the reference implementation — delete features/todo/ manually when done reading it.",
	},
	{
		Name:  "sound-feedback",
		Idea:  "tiny client-side cues with mute and reduced-motion respect",
		Match: []string{"sound", "audio", "feedback", "cue", "toggle"},
		Keep:  []string{unitSounds},
		Note:  "Zero-test client-side plugin; removal checklist is in its SCOPE doc. Mute toggle + reduced-motion included.",
	},
}

// adviseRules are the global opinions, printed before everything else.
// They encode docs/native-zig.md and the runtime-vs-trim rule in one place
// so an LLM gets them without reading the whole site.
var adviseRules = []string{
	"Go for everything; Zig only for a profiled hot kernel, codec, or OS " +
		"integration (docs/native-zig.md). A speedup claim without a Go " +
		"baseline benchmark is not a reason.",
	"Prefer runtime switches over trim when unsure: NATS_ENABLED=false, " +
		"DAGNATS_ENABLED=false, OFFLINE_SYNC_ENABLED=false, unset " +
		"GOAI_API_KEY, ENTITY_STORE=pb|crdt, UI_SKIN. Trim deletes; " +
		"switches flip back.",
	"Never add a dependency before checking the registry below and " +
		"https://calionauta.github.io/gogogo/llms.txt — todo is the " +
		"reference implementation.",
	"Go coding standards live in skills/gogogo-coding-standards/SKILL.md " +
		"(universal principles delegated to stelow-workflow-coding-standards); " +
		"audit new deps with `go mod why` + `govulncheck ./...` before adding.",
}

// foreignRules replace the template rules when the need names a non-Go
// stack: nothing here installs there, so trim mechanics stay silent.
var foreignRules = []string{
	"Copy the pattern, not the code: owned dirs below are the reference " +
		"implementation to read, not packages to install.",
	"This tool does not track other ecosystems — check their docs for " +
		"the managed option before building it yourself.",
}

// Stack labels shared below (one spelling per ecosystem: goconst-quiet
// by construction).
const (
	stackNext     = "Next.js"
	stackReact    = "React"
	stackVue      = "Vue"
	stackSvelte   = "Svelte"
	stackAstro    = "Astro"
	stackAngular  = "Angular"
	stackNode     = "Node.js"
	stackBun      = "Bun"
	stackDeno     = "Deno"
	stackTS       = "TypeScript"
	stackJS       = "JavaScript"
	stackPython   = "Python"
	stackRust     = "Rust"
	stackRuby     = "Ruby"
	stackPHP      = "PHP"
	stackJVM      = "Java/Kotlin"
	stackFlutter  = "Flutter"
	stackDotnet   = "C#/.NET"
	scopePatterns = "patterns"
	scopeTemplate = "template"
)

// stackSignal is one ordered detection rule: dotted substrings first
// (tokenizing splits "next.js" apart), then whole words. Slice order is
// the priority order — maps would answer multi-stack needs randomly.
type stackSignal struct {
	dotted string // substring of the raw need, "" when unused
	word   string // whole-word signal, "" when unused
	label  string
}

var stackSignals = []stackSignal{
	{"next.js", "", stackNext},
	{"node.js", "", stackNode},
	{"vue.js", "", stackVue},
	{"", "nextjs", stackNext},
	{"", "react", stackReact},
	{"", "remix", stackReact},
	{"", "vue", stackVue},
	{"", "nuxt", stackVue},
	{"", "svelte", stackSvelte},
	{"", "sveltekit", stackSvelte},
	{"", "astro", stackAstro},
	{"", "angular", stackAngular},
	{"", "node", stackNode},
	{"", "nodejs", stackNode},
	{"", "express", stackNode},
	{"", "fastify", stackNode},
	{"", "nestjs", stackNode},
	{"", "hono", stackNode},
	{"", "bun", stackBun},
	{"", "deno", stackDeno},
	{"", "typescript", stackTS},
	{"", "javascript", stackJS},
	{"", "python", stackPython},
	{"", "django", stackPython},
	{"", "flask", stackPython},
	{"", "fastapi", stackPython},
	{"", "streamlit", stackPython},
	{"", "rust", stackRust},
	{"", "axum", stackRust},
	{"", "actix", stackRust},
	{"", "tauri", stackRust},
	{"", "ruby", stackRuby},
	{"", "rails", stackRuby},
	{"", "php", stackPHP},
	{"", "laravel", stackPHP},
	{"", "spring", stackJVM},
	{"", "kotlin", stackJVM},
	{"", "flutter", stackFlutter},
	{"", "dart", stackFlutter},
	{"", "dotnet", stackDotnet},
	{"", "csharp", stackDotnet},
}

// goSignals keep template-scoped answers when the need names Go
// unambiguously ("golang API serving a Next.js frontend" is still a Go
// backend question). Bare "go" is deliberately absent: it collides with
// the English verb ("want to go with Next.js") and silently forces the
// wrong scope — an explicit golang/templ/gin/... wins instead.
var goSignals = []string{
	"golang", "templ", "gogogo", "gin", "fiber",
	"pocketbase", "dagnats", "goqite",
}
