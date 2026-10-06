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
	Scope string `json:"scope"`
	Stack string `json:"stack,omitempty"`
	// Tree records what the optional --dir probe found, so a reader can tell
	// "I was told this is a gogogo checkout" from "nobody looked". Empty
	// means no path was given — the answer is about the need alone.
	Tree string `json:"tree,omitempty"`
	// Reason names WHY a non-template scope was chosen, so the caller does not
	// have to infer it from the rules. Two conditions share the go-standards
	// scope (the need forbids dependencies; the path is not a checkout) and
	// they call for different next steps.
	Reason       string         `json:"reason,omitempty"`
	Rules        []string       `json:"rules"`
	Presets      []advisePreset `json:"presets"`
	Capabilities []adviseCap    `json:"capabilities,omitempty"`
	FirstRun     *nextSteps     `json:"firstRun,omitempty"`
}

// needWords tokenizes a need the same way preset matching does.
func needWords(need string) []string {
	return strings.FieldsFunc(strings.ToLower(need), func(r rune) bool {
		return r < 'a' || r > 'z'
	})
}

// wordHit is the shared keyword rule (preset matching and stack signals):
// exact hits always count; prefixes need length 4+ both ways so short
// words ("ai", "bun", "vue") never prefix-match (airplane/ai precedent).
func wordHit(words []string, kw string) bool {
	for _, w := range words {
		if w == kw || (len(kw) >= 4 && strings.HasPrefix(w, kw)) ||
			(len(w) >= 4 && strings.HasPrefix(kw, w)) {
			return true
		}
	}
	return false
}

// detectStack names a non-Go ecosystem when the need signals one without
// any Go signal. Empty means template scope (Go or unknown: advise owns it).
func detectStack(need string) string {
	lowered := strings.ToLower(need)
	words := needWords(need)
	if slices.ContainsFunc(goSignals, func(g string) bool { return wordHit(words, g) }) {
		return ""
	}
	for _, s := range stackSignals {
		if s.dotted != "" {
			if strings.Contains(lowered, s.dotted) {
				return s.label
			}
			continue
		}
		if wordHit(words, s.word) {
			return s.label
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

// buildAdvise is buildAdviseIn with no path: the pure, need-only answer.
// Kept as the entry point every existing caller and test uses, so the
// default output is unchanged byte-for-byte by the --dir feature.
func buildAdvise(need string) adviseDoc {
	return buildAdviseIn(need, "")
}

// buildAdviseIn resolves the registry into guidance, filtering presets by
// need (empty need returns every preset, most useful first is meaningless
// without a query — manifest order wins). A non-Go stack switches the
// scope to patterns: no trim mechanics, no capability table, owned paths
// as copy reference.
//
// dir is optional and opt-in. Empty keeps the answer a pure function of need
// — the tool still reads nothing unless asked. When dir IS given, the probe is
// one stat call: the capability table is only meaningful inside a gogogo
// checkout, so outside one the answer says so instead of listing 24 units the
// reader cannot use.
func buildAdviseIn(need, dir string) adviseDoc {
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
			Scope: scopePatterns, Stack: stack, Tree: probeTree(dir),
			Rules: foreignRules, Presets: matched,
		}
	}
	// The path outranks the need's wording: if the caller points at a tree
	// that is not a gogogo checkout, the capability table is inapplicable
	// whatever the need says. Checked after detectStack because a non-Go
	// stack already has the shorter, correct answer.
	if dir != "" && !looksLikeTemplate(dir) {
		return adviseDoc{
			Scope: scopeGoStdlib, Reason: reasonNotCheckout,
			Tree: treeNotCheckout, Presets: matched, Rules: notCheckoutRules,
		}
	}
	// A Go need that constrains itself to the standard library cannot use
	// any capability (each one is or pulls a dependency), so answering with
	// the 24-entry table buries the useful part. Answer with the Go
	// standards pointer instead. Checked AFTER detectStack so an explicit
	// non-Go stack still wins: "Rust, no dependencies" is a Rust need.
	if wantsStdlibOnly(need) {
		return adviseDoc{
			Scope: scopeGoStdlib, Reason: reasonStdlibOnly,
			Tree: probeTree(dir), Rules: stdlibRules,
		}
	}
	doc := adviseDoc{
		Scope: scopeTemplate, Tree: probeTree(dir), Rules: adviseRules,
		FirstRun: func() *nextSteps { n := buildNextSteps("<dir>", nil); return &n }(),
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
			if wordHit(words, kw) {
				n++
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

// renderStdlib is the go-standards-scope text: no capability table, no trim
// mechanics, no presets — just the rules that survive without the template.
//
// It renders doc.Rules rather than a fixed list, because two different
// conditions reach this scope (the need forbids dependencies, or --dir is not a
// checkpoint) and each ships its own rule set. Printing the header from
// doc.Reason keeps the explanation matched to the cause.
// Split out so Advise stays under the gocyclo gate, like renderForeign.
func renderStdlib(doc adviseDoc) string {
	var b strings.Builder
	switch doc.Reason {
	case reasonNotCheckout:
		b.WriteString("the path is not a gogogo checkout — the template does not apply here.\n" +
			"No capability is installable into it: this tool trims and extends its own scaffold,\n" +
			"not an arbitrary project.\n\n")
	default:
		b.WriteString("stdlib-only Go — the gogogo template does not apply here.\n" +
			"No capability is installable: each one adds or belongs to a dependency\n" +
			"this need forbids, so the registry is omitted rather than shown empty.\n\n")
	}
	b.WriteString("rules:\n")
	for _, r := range doc.Rules {
		fmt.Fprintf(&b, "  - %s\n", r)
	}
	return b.String()
}

// probeTree reports what the optional --dir check found, or "" when no path
// was given. It is the ONLY thing in this file that touches the filesystem,
// and only when the caller opts in with a path — so `advise --need X` remains
// a pure function of its input.
func probeTree(dir string) string {
	if dir == "" {
		return treeUnknown
	}
	if looksLikeTemplate(dir) {
		return treeCheckout
	}
	return treeNotCheckout
}

// Advise renders guidance for need in text|json. With dir empty it reads the
// registry and touches nothing else, exactly as before; dir is an opt-in probe
// that lets the answer say "this path is not a gogogo checkout" instead of
// listing capabilities the caller cannot use there.
func Advise(need, format, dir string) (string, error) {
	if format != planFormatText && format != planFormatJSON {
		return "", fmt.Errorf("unknown --format %q (want text|json)", format)
	}
	doc := buildAdviseIn(need, dir)
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
	if doc.Scope == scopeGoStdlib {
		return renderStdlib(doc), nil
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
	dir := fs.String("dir", "",
		"optional: check this path too, so the answer can say it is not a "+
			"gogogo checkout (omitted = the answer depends on --need alone)")
	fs.SetOutput(stdout)
	if err := fs.Parse(args); err != nil {
		return err
	}
	out, err := Advise(*need, *format, *dir)
	if err != nil {
		return err
	}
	fmt.Fprint(stdout, out)
	return nil
}
