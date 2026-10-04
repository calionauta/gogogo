// SCOPE:layer=infra,removal=plugin — installer engine: opinionated stack guidance, reads nothing, changes nothing
package installer

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/calionauta/gogogo/internal/capabilities"
)

// advisePreset is one use-case → stack opinion. Keep/Drop name installer
// unit ids (or core/runtime markers an LLM should not try to trim);
// Note carries the one line that saves a wrong decision. Idea is the
// portable mechanism for non-Go codebases (same preset, pattern only).
type advisePreset struct {
	Name  string   `json:"name"`
	Match []string `json:"-"`
	Keep  []string `json:"keep"`
	Drop  []string `json:"drop,omitempty"`
	Note  string   `json:"note"`
	Idea  string   `json:"idea"`
	Copy  []string `json:"copy,omitempty"`
}

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
}

// adviseCap is one registry capability with its installer mapping resolved.
// Dirs/Files are the owned paths: what to copy when the target is a foreign
// (non-scaffolded) codebase that `add` refuses.
type adviseCap struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Summary    string   `json:"summary"`
	Trim       string   `json:"trim"`
	RuntimeOff string   `json:"runtimeOff,omitempty"`
	Note       string   `json:"note,omitempty"`
	Dirs       []string `json:"dirs,omitempty"`
	Files      []string `json:"files,omitempty"`
}

// adviseDoc is the full guidance document (text and JSON share it).
type adviseDoc struct {
	Scope        string         `json:"scope"`
	Stack        string         `json:"stack,omitempty"`
	Rules        []string       `json:"rules"`
	Presets      []advisePreset `json:"presets"`
	Capabilities []adviseCap    `json:"capabilities,omitempty"`
	FirstRun     *nextSteps     `json:"firstRun,omitempty"`
}

// foreignRules replace the template rules when the need names a non-Go
// stack: nothing here installs there, so trim mechanics stay silent.
var foreignRules = []string{
	"Copy the pattern, not the code: owned dirs below are the reference " +
		"implementation to read, not packages to install.",
	"This tool does not track other ecosystems — check their docs for " +
		"the managed option before building it yourself.",
}

// Stack labels shared between the dotted and whole-word maps (one
// spelling per ecosystem: goconst-quiet by construction).
const (
	stackNode     = "Node.js"
	stackPython   = "Python"
	stackRust     = "Rust"
	scopePatterns = "patterns"
	scopeTemplate = "template"
)

// foreignStack maps dotted-first signals (split away by word tokenizing)
// to ecosystem labels. A Go mention anywhere wins (see detectStack):
// mixed codebases get template advice for the Go side.
var foreignDotted = map[string]string{
	"next.js": "Next.js",
	"node.js": stackNode,
	"vue.js":  "Vue",
}

// foreignWords maps whole-word signals to ecosystem labels. Short words
// stay exact-only via the shared prefix rule (airplane/ai precedent);
// "java" is deliberately absent (javascript false-positives) — spring
// and kotlin carry the JVM signal instead.
var foreignWords = map[string]string{
	"nextjs": "Next.js", "react": "React", "remix": "React",
	"vue": "Vue", "nuxt": "Vue", "svelte": "Svelte", "sveltekit": "Svelte",
	"astro": "Astro", "angular": "Angular", "node": stackNode,
	"nodejs": stackNode, "express": stackNode, "fastify": stackNode,
	"nestjs": stackNode, "hono": stackNode, "bun": "Bun", "deno": "Deno",
	"typescript": "TypeScript", "javascript": "JavaScript",
	"python": stackPython, "django": stackPython, "flask": stackPython,
	"fastapi": stackPython, "streamlit": stackPython,
	"rust": stackRust, "axum": stackRust, "actix": stackRust, "tauri": stackRust,
	"ruby": "Ruby", "rails": "Ruby", "php": "PHP", "laravel": "PHP",
	"spring": "Java/Kotlin", "kotlin": "Java/Kotlin",
	"flutter": "Flutter", "dart": "Flutter", "dotnet": "C#/.NET",
	"csharp": "C#/.NET",
}

// goSignals keep template-scoped answers when the need names Go anywhere
// ("Go API serving a Next.js frontend" is still a Go backend question).
var goSignals = []string{
	"go", "golang", "templ", "gogogo", "gin", "fiber",
	"pocketbase", "dagnats", "goqite",
}

// needWords tokenizes a need the same way preset matching does.
func needWords(need string) []string {
	return strings.FieldsFunc(strings.ToLower(need), func(r rune) bool {
		return r < 'a' || r > 'z'
	})
}

// detectStack names a non-Go ecosystem when the need signals one without
// any Go signal. Empty means template scope (Go or unknown: advise owns it).
func detectStack(need string) string {
	lowered := strings.ToLower(need)
	words := needWords(need)
	inWords := func(kw string) bool {
		for _, w := range words {
			if w == kw || (len(kw) >= 4 && strings.HasPrefix(w, kw)) ||
				(len(w) >= 4 && strings.HasPrefix(kw, w)) {
				return true
			}
		}
		return false
	}
	if slices.ContainsFunc(goSignals, inWords) {
		return ""
	}
	for dotted, label := range foreignDotted {
		if strings.Contains(lowered, dotted) {
			return label
		}
	}
	for kw, label := range foreignWords {
		if inWords(kw) {
			return label
		}
	}
	return ""
}

// capUnit inverts unitCaps: capability id → owning installer unit.
func capUnit() map[string]string {
	out := map[string]string{}
	for unit, caps := range unitCaps {
		for _, c := range caps {
			out[c] = unit
		}
	}
	return out
}

// presetCopyDirs resolves a preset's Keep entries to owned repo paths:
// unit ids expand to their capabilities' dirs+files (sorted, deduped),
// core/runtime markers contribute nothing (no code to copy).
func presetCopyDirs(p advisePreset) []string {
	byUnit := map[string]trimUnit{}
	for _, u := range manifestUnits {
		byUnit[u.id] = u
	}
	byID := capabilities.ByID()
	seen := map[string]bool{}
	var out []string
	add := func(paths ...string) {
		for _, d := range paths {
			if !seen[d] {
				seen[d] = true
				out = append(out, d)
			}
		}
	}
	for _, keep := range p.Keep {
		u, ok := byUnit[keep]
		if !ok {
			continue
		}
		for _, id := range u.caps() {
			if c, ok := byID[id]; ok {
				add(c.Dirs...)
				add(c.Files...)
			}
		}
	}
	sort.Strings(out)
	return out
}

// buildAdvise resolves the registry into guidance, filtering presets by
// need (empty need returns every preset, most useful first is meaningless
// without a query — manifest order wins). A non-Go stack switches the
// scope to patterns: no trim mechanics, no capability table, owned paths
// as copy reference.
func buildAdvise(need string) adviseDoc {
	owners := capUnit()
	unitKind := map[string]capabilities.Kind{}
	for _, u := range manifestUnits {
		unitKind[u.id] = u.meta().kind
	}
	matched := matchPresets(need)
	// Annotate copies, never the shared registry: matchPresets returns
	// the global slice for empty needs.
	annotated := make([]advisePreset, len(matched))
	for i := range matched {
		annotated[i] = matched[i]
		annotated[i].Copy = presetCopyDirs(matched[i])
	}
	matched = annotated
	if stack := detectStack(need); stack != "" {
		return adviseDoc{
			Scope: scopePatterns, Stack: stack,
			Rules: foreignRules, Presets: matched,
		}
	}
	doc := adviseDoc{
		Scope: scopeTemplate, Rules: adviseRules,
		FirstRun: func() *nextSteps { n := buildNextSteps("<dir>"); return &n }(),
	}
	for _, c := range capabilities.All {
		ac := adviseCap{
			ID: c.ID, Kind: string(c.Kind), Summary: c.Summary,
			RuntimeOff: c.RuntimeOff, Note: c.Note,
			Dirs: c.Dirs, Files: c.Files,
		}
		if unit, ok := owners[c.ID]; ok {
			if unitKind[unit] == capabilities.KindFeature {
				ac.Trim = "--features " + unit
			} else {
				ac.Trim = "--plugins " + unit
			}
		} else if c.RuntimeOff != "" {
			ac.Trim = "not an installer unit (runtime switch)"
		} else {
			ac.Trim = "not an installer unit (core — always in)"
		}
		doc.Capabilities = append(doc.Capabilities, ac)
	}
	doc.Presets = matchPresets(need)
	return doc
}

// matchPresets scores presets by keyword hits in need. Empty need returns
// all presets in manifest order; a need with no hits returns none (the
// caller prints the preset names as the recovery hint).
func matchPresets(need string) []advisePreset {
	if strings.TrimSpace(need) == "" {
		return advisePresets
	}
	words := strings.FieldsFunc(strings.ToLower(need), func(r rune) bool {
		return r < 'a' || r > 'z'
	})
	type scored struct {
		p advisePreset
		n int
	}
	var out []scored
	for _, p := range advisePresets {
		n := 0
		for _, kw := range p.Match {
			for _, w := range words {
				if w == kw || (len(kw) >= 4 && strings.HasPrefix(w, kw)) ||
					(len(w) >= 4 && strings.HasPrefix(kw, w)) {
					n++
					break
				}
			}
		}
		if n > 0 {
			out = append(out, scored{p, n})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].n > out[j].n })
	presets := make([]advisePreset, 0, len(out))
	for _, s := range out {
		presets = append(presets, s.p)
	}
	return presets
}

// renderForeign is the patterns-scope text: no trim mechanics, no
// capability table — the portable idea plus copy reference per preset.
// Split out so Advise stays under the gocyclo gate.
func renderForeign(doc adviseDoc) string {
	var b strings.Builder
	fmt.Fprintf(&b, "stack detected: %s — advise knows the gogogo (Go) "+
		"template only.\nUnits below are NOT installable here; take the "+
		"pattern, copy the idea.\n\n", doc.Stack)
	b.WriteString("rules:\n")
	for _, r := range doc.Rules {
		fmt.Fprintf(&b, "  - %s\n", r)
	}
	b.WriteString("\npresets (use-case → portable pattern + reference paths):\n")
	if len(doc.Presets) == 0 {
		b.WriteString("  no preset matched — describe the use-case with " +
			"plain verbs (realtime, jobs, offline, AI).\n")
	}
	for _, p := range doc.Presets {
		fmt.Fprintf(&b, "  %s: %s\n", p.Name, p.Idea)
		if len(p.Copy) > 0 {
			fmt.Fprintf(&b, "    reference: %s\n", strings.Join(p.Copy, ", "))
		}
	}
	return b.String()
}

// Advise renders guidance for need in text|json. Pure: reads the registry,
// touches nothing. LLMs call this when they want opinions, not changes.
func Advise(need, format string) (string, error) {
	if format != planFormatText && format != planFormatJSON {
		return "", fmt.Errorf("unknown --format %q (want text|json)", format)
	}
	doc := buildAdvise(need)
	if format == planFormatJSON {
		raw, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return "", err
		}
		return string(raw) + "\n", nil
	}
	var b strings.Builder
	b.WriteString("gogogo advise — opinions, not changes (nothing was installed):\n\n")
	if doc.Scope == scopePatterns {
		return renderForeign(doc), nil
	}
	b.WriteString("rules:\n")
	for _, r := range doc.Rules {
		fmt.Fprintf(&b, "  - %s\n", r)
	}
	b.WriteString("\npresets (use-case → keep/drop):\n")
	if len(doc.Presets) == 0 {
		names := make([]string, 0, len(advisePresets))
		for _, p := range advisePresets {
			names = append(names, p.Name)
		}
		fmt.Fprintf(&b, "  no preset matched %q — available: %s\n", need, strings.Join(names, ", "))
	}
	for _, p := range doc.Presets {
		fmt.Fprintf(&b, "  %s: keep [%s]", p.Name, strings.Join(p.Keep, ", "))
		if len(p.Drop) > 0 {
			fmt.Fprintf(&b, " drop [%s]", strings.Join(p.Drop, ", "))
		}
		fmt.Fprintf(&b, "\n    %s\n", p.Note)
	}
	b.WriteString("\ncapabilities (id, kind, how to switch each off):\n")
	for _, c := range doc.Capabilities {
		fmt.Fprintf(&b, "  %-14s %-7s %s\n", c.ID, c.Kind, c.Summary)
		fmt.Fprintf(&b, "  %-14s         trim: %s", "", c.Trim)
		if c.RuntimeOff != "" {
			fmt.Fprintf(&b, " | off: %s", c.RuntimeOff)
		}
		b.WriteString("\n")
		if len(c.Dirs)+len(c.Files) > 0 {
			fmt.Fprintf(&b, "  %-14s         copy: %s\n", "",
				strings.Join(append(c.Dirs, c.Files...), ", "))
		}
	}
	n := doc.FirstRun
	b.WriteString("\nfirst run (defaults; PORT overrides the port):\n")
	fmt.Fprintf(&b, "  %s\n", strings.Replace(n.Dev, "<dir>", "my-app", 1))
	fmt.Fprintf(&b, "  app %s · login %s\n", n.App, n.Login)
	fmt.Fprintf(&b, "  admin %s\n", n.Admin)
	fmt.Fprintf(&b, "  workflows %s\n", n.Flows)
	return b.String(), nil
}

func runAdvise(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("advise", flag.ContinueOnError)
	need := fs.String("need", "",
		"your use-case in a few words (empty lists every preset)")
	format := fs.String("format", planFormatText, "output format: text|json")
	fs.SetOutput(stdout)
	if err := fs.Parse(args); err != nil {
		return err
	}
	out, err := Advise(*need, *format)
	if err != nil {
		return err
	}
	fmt.Fprint(stdout, out)
	return nil
}
