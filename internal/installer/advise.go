// SCOPE:layer=infra,removal=plugin — installer engine: opinionated stack guidance, reads nothing, changes nothing
package installer

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/calionauta/gogogo/internal/capabilities"
)

// advisePreset is one use-case → stack opinion. Keep/Drop name installer
// unit ids (or core/runtime markers an LLM should not try to trim);
// Note carries the one line that saves a wrong decision.
type advisePreset struct {
	Name  string   `json:"name"`
	Match []string `json:"-"`
	Keep  []string `json:"keep"`
	Drop  []string `json:"drop,omitempty"`
	Note  string   `json:"note"`
}

var advisePresets = []advisePreset{
	{
		Name: "realtime-collab",
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
		Match: []string{"admin", "config", "inspect", "dashboard", "observab"},
		Keep:  []string{unitConfigView},
		Note:  "Surfaces: /config (auth-gated env view), /_/ (PocketBase admin), /dagnats/ (workflow console).",
	},
	{
		Name:  "marketing-site",
		Match: []string{unitLanding, "marketing", "homepage", "site", "hero"},
		Keep:  []string{unitLanding},
		Note:  "Public GET / with no auth. Brand lives here; /todo is the app behind it.",
	},
	{
		Name:  "quiet-api",
		Match: []string{"api", "headless", "backend", "minimal", "embed", "library"},
		Keep:  []string{"(core only — auth middleware, queue, router)"},
		Drop:  []string{unitLanding, unitWhiteboard, unitSounds, unitSkinsExtra},
		Note:  "Todo stays as the reference implementation — delete features/todo/ manually when done reading it.",
	},
	{
		Name:  "sound-feedback",
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
type adviseCap struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Summary    string `json:"summary"`
	Trim       string `json:"trim"`
	RuntimeOff string `json:"runtimeOff,omitempty"`
	Note       string `json:"note,omitempty"`
}

// adviseDoc is the full guidance document (text and JSON share it).
type adviseDoc struct {
	Rules        []string       `json:"rules"`
	Presets      []advisePreset `json:"presets"`
	Capabilities []adviseCap    `json:"capabilities"`
	FirstRun     nextSteps      `json:"firstRun"`
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

// buildAdvise resolves the registry into guidance, filtering presets by
// need (empty need returns every preset, most useful first is meaningless
// without a query — manifest order wins).
func buildAdvise(need string) adviseDoc {
	owners := capUnit()
	unitKind := map[string]capabilities.Kind{}
	for _, u := range manifestUnits {
		unitKind[u.id] = u.meta().kind
	}
	doc := adviseDoc{Rules: adviseRules, FirstRun: buildNextSteps("<dir>")}
	for _, c := range capabilities.All {
		ac := adviseCap{ID: c.ID, Kind: string(c.Kind), Summary: c.Summary, RuntimeOff: c.RuntimeOff, Note: c.Note}
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
