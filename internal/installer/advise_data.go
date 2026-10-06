// SCOPE:layer=infra,removal=plugin — installer engine: advise data (presets, rules, stack signals)
package installer

import "strings"

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
	"Go coding standards live in the gogogo-coding-standards skill " +
		"(https://github.com/calionauta/gogogo — install: " +
		"npx skills add calionauta/gogogo): Go idioms plus the concurrency, " +
		"testing and profiling deltas this template holds itself to. " +
		"Universal principles (KISS/DRY/YAGNI, sizes) are delegated to " +
		"stelow-workflow-coding-standards — a pointer, not a runtime " +
		"dependency. Audit new deps with `go mod why` + `govulncheck ./...` " +
		"before adding.",
}

// foreignRules replace the template rules when the need names a non-Go
// stack: nothing here installs there, so trim mechanics stay silent.
var foreignRules = []string{
	"Copy the pattern, not the code: owned dirs below are the reference " +
		"implementation to read, not packages to install.",
	"This tool does not track other ecosystems — check their docs for " +
		"the managed option before building it yourself.",
}

// stdlibRules replace the template rules when a GO need rules the template
// out with its own constraint (stdlib-only, no dependencies, single
// binary). The capability table is not merely unhelpful there — every
// capability ships a dependency the need forbids — so this document is a
// few opinions plus a pointer to the Go standards, and nothing else.
//
// Deliberately NOT silent about the mismatch: a caller that asked for
// opinions must be told the template does not apply and why, or it will
// assume the empty answer means "nothing to say".
var stdlibRules = []string{
	"This is a Go need whose own constraint (stdlib-only / no dependencies " +
		"/ single binary) rules the gogogo template out: every capability " +
		"would add or belongs to a dependency the need forbids. Nothing from " +
		"this toolchain installs here — read the Go standards instead.",
	"Go coding standards: skills/gogogo-coding-standards/SKILL.md in the " +
		"gogogo repo (https://github.com/calionauta/gogogo). The parts that " +
		"apply without the template are references/go-concurrency-deltas.md " +
		"(goroutine ownership, lifecycle, context), the Core Go Rules and " +
		"Testing sections, and stelow-workflow-coding-standards for the " +
		"universal principles (KISS/DRY/YAGNI, 50/400 sizes; Go override " +
		"100/500). Install either with `npx skills add calionauta/gogogo` " +
		"— the skill is self-contained, and its delegation to stelow is a " +
		"pointer, not a runtime dependency.",
	"Still true without the template: `strconv` over `fmt` on hot paths, " +
		"`log/slog` over `log`, errors wrapped with `%w` at the call site, " +
		"`go test -race`, and no goroutine without an owner, an exit and a " +
		"wait.",
}

// notCheckoutRules replace the template rules when --dir points at something
// that is NOT a gogogo checkout. Distinct from stdlibRules because the cause is
// different: the need did not rule the template out, the PATH did — there is no
// trim manifest to act on, so the advice is about what can still be reused
// rather than about dependencies.
var notCheckoutRules = []string{
	"The path given with --dir is not a gogogo checkout (no cmd/web/main.go " +
		"or internal/installer/run.go), so the capability table and the trim " +
		"tooling do not apply to it. Nothing was installed and nothing changed.",
	"To get the template: scaffold a project with this tool, or `npx skills " +
		"add calionauta/gogogo` to read the Go standards " +
		"(https://github.com/calionauta/gogogo — Go idioms plus the " +
		"concurrency, testing and profiling deltas the template holds itself " +
		"to). The parts that apply without the template are " +
		"references/go-concurrency-deltas.md and the Core Go Rules and Testing " +
		"sections; universal principles (KISS/DRY/YAGNI, sizes) are delegated " +
		"to stelow-workflow-coding-standards.",
	"To adopt ONE capability in an existing project, read its dirs/files (given " +
		"per capability in the registry) and copy the pattern — `add` only " +
		"merges into a scaffolded checkout.",
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
	stackZig      = "Zig"
	scopePatterns = "patterns"
	scopeTemplate = "template"
	// scopeGoStdlib is the third answer shape: the template does not apply.
	// Two conditions produce it, distinguished by `reason`:
	//   reasonStdlibOnly  — a Go need whose own constraint forbids dependencies
	//   reasonNotCheckout — --dir points at something that is not a gogogo tree
	// Both mean "the capability table is useless here", but they call for
	// different next steps, so they share the scope and split on Reason.
	scopeGoStdlib = "go-standards"

	reasonStdlibOnly  = "stdlib-only"
	reasonNotCheckout = "not-a-gogogo-checkout"

	// treeUnknown / treeNotCheckout / treeCheckout are the three values of the
	// `tree` field, so a reader can always tell whether a path was examined.
	treeUnknown     = ""
	treeNotCheckout = "not-a-gogogo-checkout"
	treeCheckout    = "gogogo-checkout"
)

// stdlibSignals are the constraints that make the template unusable for a Go
// need. They are the counterpart of stackSignals: that one detects "this is
// not Go at all", this one detects "this is Go, but not this template".
//
// Every entry must be checkable from the need's own wording — no inference
// about the reader's intent — because the answer changes shape based on it.
var stdlibSignals = []string{
	"stdlib", "standard library", "std only",
	"no dependencies", "no dependency", "no deps", "no external",
	"dependency-free", "dependency free", "zero deps", "zero dependencies",
	"single binary", "single executable", "single file",
	"no packages", "no third-party", "no third party", "pure go", "vanilla go",
}

// wantsStdlibOnly reports whether need constrains itself to the standard
// library (so nothing in the capability table can be used).
//
// Multi-word signals are matched as substrings of the normalized need, not as
// tokens: "no dependencies" tokenizes to [no dependencies] and would never
// match either word alone, while "dependency-free" splits on the hyphen.
func wantsStdlibOnly(need string) bool {
	words := needWords(need)
	for _, s := range stdlibSignals {
		if strings.Contains(s, " ") || strings.Contains(s, "-") {
			if strings.Contains(needWordsJoined(need), s) {
				return true
			}
			continue
		}
		if wordHit(words, s) {
			return true
		}
	}
	return false
}

// needWordsJoined is need lowercased and whitespace-normalized, so a
// multi-word signal can be found as a substring without a tokenizer that
// would split it apart (needWords drops separators entirely).
func needWordsJoined(need string) string {
	return strings.Join(needWords(need), " ")
}

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
	// Zig is this repo's documented escape hatch (docs/native-zig.md), so a
	// Zig need is explicitly NOT a Go need. Without this signal "Zig, zero
	// dependencies" fell through to the Go scopes and answered about the
	// gogogo template.
	{"", "zig", stackZig},
	{"", "ziglang", stackZig},
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
