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
			// "chat" and "live" are lay words AND cross-language loanwords
			// (EN/PT/ES/FR/DE): "live chat", "live score", "fazer uma live".
			"chat", "live",
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
			// "remind" is the lay word for scheduled notifications
			// ("reminder emails"); EN-only, other languages fall back
			// to the full map.
			"remind",
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
			// Lay connectivity words; "internet" and "wifi" are universal
			// loanwords (EN/PT/ES/FR/DE), "connectivity" covers EN.
			// ("signal" deliberately excluded: it would also match
			// Datastar-signal needs.)
			"internet", "wifi", "connectivity",
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
		Name: "marketing-site",
		Idea: "static public pages, no auth, no app state",
		Match: []string{
			unitLanding, "marketing", "homepage", "site", "hero",
			// "website" and "blog" are THE lay words for a public site and
			// universal loanwords (EN/PT/ES/FR/DE).
			"website", "blog",
		},
		Keep: []string{unitLanding},
		Note: "Public GET / with no auth. Brand lives here; /todo is the app behind it.",
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
		Name: "room-authority",
		Idea: "one addressable owner per room: roster, locks, and timers live in the entity, not in rows",
		Match: []string{
			"presenter", "room", "turn", "arbitrat", "supervis", "lobby",
		},
		Keep: []string{unitGoAkt},
		Note: "One grain per room (turn-based: exactly one lock winner), " +
			"supervised with restart budget, roster rebuilt from heartbeats. " +
			"Demo page at /room/ with a crash hook.",
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
// They encode docs/exception-to-go.md and the runtime-vs-trim rule in one place
// so an LLM gets them without reading the whole site.
var adviseRules = []string{
	"Go for everything; Zig only for a profiled hot kernel, codec, or OS " +
		"integration (docs/exception-to-go.md). A speedup claim without a Go " +
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
		"stelow-workflow-coding-standards, which is vendored into the skill " +
		"tree (skills/stelow-workflow-coding-standards/) and pinned to an " +
		"upstream commit in its UPSTREAM_SHA — vendored rather than linked " +
		"because a skill's references/ only resolve as relative paths to a " +
		"locally present skill. Audit new deps with `go mod why` + " +
		"`govulncheck ./...` before adding.",
	"Fitting units to a use-case: docs/use-cases.md maps every " +
		"core/plugin/feature to business-language cases, decision pairs " +
		"(jobs vs workflows, pb vs crdt), and what has no unit yet " +
		"(https://calionauta.github.io/gogogo/docs/use-cases/).",
}

// notApplicableHint is the single honest answer for needs nothing in the
// registry matches: this tool only knows the gogogo (Go) template, so it
// shows the whole map instead of guessing. Folded into the template text
// path ("no preset matched — available: ..."); kept here so the sentence
// stays identical everywhere it renders.
var notApplicableHint = "this tool only knows the gogogo (Go) template — " +
	"nothing above matched, so the full map is shown instead of a guess. " +
	"Take the Idea lines as portable patterns and the reference paths as " +
	"reading pointers; nothing here installs outside a scaffolded checkout."

// stdlibRules replace the template rules when a need rules the template
// out with its own constraint (stdlib-only, no dependencies, single
// binary). The capability table is not merely unhelpful there — every
// capability ships a dependency the need forbids — so this document is a
// few opinions plus a pointer to the Go standards, and nothing else.
//
// Deliberately NOT silent about the mismatch: a caller that asked for
// opinions must be told the template does not apply and why, or it will
// assume the empty answer means "nothing to say".
var stdlibRules = []string{
	"This need forbids dependencies (stdlib-only / no deps / single " +
		"binary), which rules the gogogo template out: every capability " +
		"would add or belongs to a dependency the need forbids. Nothing from " +
		"this toolchain installs here — read the Go standards instead. " +
		"(This tool only knows the Go template, so the pointer below is " +
		"Go either way.)",
	"Go coding standards: skills/gogogo-coding-standards/SKILL.md in the " +
		"gogogo repo (https://github.com/calionauta/gogogo). The parts that " +
		"apply without the template are references/go-concurrency-deltas.md " +
		"(goroutine ownership, lifecycle, context), the Core Go Rules and " +
		"Testing sections, and stelow-workflow-coding-standards for the " +
		"universal principles (KISS/DRY/YAGNI, 50/400 sizes; Go override " +
		"100/500). Install either with `npx skills add calionauta/gogogo` " +
		"— both skills ship in the repo, stelow vendored under " +
		"skills/stelow-workflow-coding-standards/.",
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
		"to stelow-workflow-coding-standards, which ships vendored in the " +
		"repo under skills/stelow-workflow-coding-standards/.",
	"To adopt ONE capability in an existing project, read its dirs/files (given " +
		"per capability in the registry) and copy the pattern — `add` only " +
		"merges into a scaffolded checkout.",
}

// Stack labels: deliberately none. This tool only knows the gogogo (Go)
// template plus its one documented native exception (Zig, below), so there
// is no ecosystem detector to label anything with. A need that matches
// nothing gets the full map instead of a guessed stack (see buildAdviseIn).
const (
	scopeTemplate = "template"
	// scopeExceptionToGo is the second answer shape: the need names Zig, the
	// repo's one documented exception to the Go rule (docs/exception-to-go.md).
	// Capabilities do not install under an exception, so the answer is the
	// Exception-to-go gate plus the skill pointer — never trim mechanics, never a
	// foreign label.
	scopeExceptionToGo = "exception-to-go"
	// scopeGoStdlib is the third answer shape: the template does not apply.
	// Two conditions produce it, distinguished by `reason`:
	//   reasonStdlibOnly  — a need whose own constraint forbids dependencies
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

// stdlibSignals are the constraints that make the template unusable for a
// need. A need matching one gets the go-standards pointer instead of the
// capability table — every capability ships a dependency the constraint
// forbids.
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

// exceptionToGoRules is the whole opinion for a Zig-shaped need: the gate,
// the pointer to the standards that hold it, and the normative doc. No
// capability table (nothing installs into a kernel), no presets (a kernel
// need matches template vocabulary only by accident).
var exceptionToGoRules = []string{
	"Go for everything; Zig only for a profiled hot kernel, codec, or OS " +
		"integration (docs/exception-to-go.md). A speedup claim without a Go " +
		"baseline benchmark is not a reason. Order: pprof, then SIMD " +
		"(`GOEXPERIMENT=simd` + `archsimd`), then Zig — with a benchmark " +
		"proving Go is the bottleneck at each step.",
	"Go coding standards: skills/gogogo-coding-standards/SKILL.md in the " +
		"gogogo repo (https://github.com/calionauta/gogogo) — the exception-to-go " +
		"section states the evidence bar; universal principles " +
		"(KISS/DRY/YAGNI) are delegated to stelow-workflow-coding-standards, " +
		"vendored under skills/stelow-workflow-coding-standards/.",
	"Still true without the template: one package, small C ABI, " +
		"caller-owned buffers, pure-Go fallback from day one, removable in " +
		"minutes. Byte-exact kernels are compared by checksum, not by vibes.",
}

// zigSignals is the tool's entire non-Go vocabulary: one entry, the one
// documented exception. It is deliberately NOT a stack detector — anything
// else the tool does not name, and an unnamed need gets the full map rather
// than a guessed label. "zig" is 3 letters, so wordHit's prefix rule cannot
// engage for it in either direction, and "zigzag" (codec vocabulary)
// matches neither "zig" nor "ziglang" (pinned by test).
var zigSignals = []string{
	"zig", "ziglang",
}
