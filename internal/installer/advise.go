// SCOPE:plugin — installer engine: opinionated stack guidance, reads nothing, changes nothing
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
	// Tree records what the optional --dir probe found, so a reader can tell
	// "I was told this is a gogogo checkout" from "nobody looked". Empty
	// means no path was given — the answer is about the need alone.
	Tree string `json:"tree,omitempty"`
	// Reason names WHY a non-template scope was chosen, so the caller does not
	// have to infer it from the rules. Two conditions share the go-standards
	// scope (the need forbids dependencies; the path is not a checkout) and
	// they call for different next steps.
	Reason string `json:"reason,omitempty"`
	// Intent is the route this answer carries (update, port, add-unit,
	// new-feature, scaffold) and IntentSource says where it came from: "given"
	// when the caller named it, "detected" when the wording implied it,
	// "default" when a non-checkout path left porting as the only step that
	// can work. Empty intent means no route — never a guess.
	Intent       string         `json:"intent,omitempty"`
	IntentSource string         `json:"intentSource,omitempty"`
	Next         []adviseStep   `json:"next,omitempty"`
	Rules        []string       `json:"rules"`
	Presets      []advisePreset `json:"presets,omitempty"`
	Capabilities []adviseCap    `json:"capabilities,omitempty"`
	FirstRun     *nextSteps     `json:"firstRun,omitempty"`
}

// adviseStep is one line of the route: what to run, and why. Run is empty for
// advice that has no command (a foreign tree has no base to compare, say) —
// the sentence then IS the step.
type adviseStep struct {
	Run string `json:"run,omitempty"`
	Why string `json:"why"`
}

// needWords tokenizes a need the same way preset matching does.
func needWords(need string) []string {
	return strings.FieldsFunc(strings.ToLower(need), func(r rune) bool {
		return r < 'a' || r > 'z'
	})
}

// wordHit is the shared keyword rule (preset matching and constraint
// signals): exact hits always count; a need-word covers a keyword stem from
// length 4+ ("airplanes"→"airplane"), but a need-word only abbreviates a
// LONGER keyword from length 5+ ("collab"→"collaborat"). The asymmetry is
// the point: 4-letter English words ("back", "dash") must never match longer
// keywords by prefix ("backend", "dashboard") — that direction is the entire
// false-positive family, while genuine abbreviations are 5+ letters. Short
// words ("ai") never prefix-match in either direction (airplane/ai precedent).
func wordHit(words []string, kw string) bool {
	for _, w := range words {
		if w == kw || (len(kw) >= 4 && strings.HasPrefix(w, kw)) ||
			(len(w) >= 5 && strings.HasPrefix(kw, w)) {
			return true
		}
	}
	return false
}

// wantsZig reports whether the need names the repo's one documented native
// exception. Checked before everything need-shaped: a Zig kernel gets the
// gate, never trim mechanics and never a guessed foreign label.
func wantsZig(need string) bool {
	words := needWords(need)
	for _, z := range zigSignals {
		if wordHit(words, z) {
			return true
		}
	}
	return false
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

// buildAdvise is buildAdviseIn with no path and no route: the pure, need-only
// answer. Kept as the entry point every existing caller and test uses, so the
// default output is unchanged byte-for-byte by the --dir/--intent features.
func buildAdvise(need string) adviseDoc {
	return buildAdviseIn(need, "", "")
}

// buildAdviseIn resolves the registry into guidance, filtering presets by
// need (empty need returns every preset, most useful first is meaningless
// without a query — manifest order wins). There is deliberately no stack
// detector: the only path probe is a stat for the template's own files
// (looksLikeTemplate), so a need that matches nothing gets the full map
// (same as empty) instead of a guessed stack label. Generic English words
// ("fast", "path", "native") can never route outside Go scopes — there is
// no branch left that names another ecosystem.
//
// dir is optional and opt-in. Empty keeps the answer a pure function of need
// — the tool still reads nothing unless asked. When dir IS given, the probe is
// one stat call: the capability table is only meaningful inside a gogogo
// checkout, and a tree that is NOT one gets a scope of its own — with the copy
// surface it can use (dirs/files), because `add` refuses there and copying is
// the only path forward.
//
// intent is the caller's route (--intent / the MCP arg). Empty means "say
// nothing": detection from the need is the fallback, and IntentSource records
// which of the two produced the route, so a stated intent is distinguishable
// from an inferred one.
func buildAdviseIn(need, dir, intent string) adviseDoc {
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
	// One probe, reused by every branch (and by the route, which must not name
	// a command this tree would refuse).
	tree := probeTree(dir)
	route, source := resolveIntent(need, intent, tree)
	// The one documented exception first: a Zig kernel gets the gate even
	// when the need also carries a dependency constraint ("Zig, zero deps"
	// is about the kernel, and the gate is the opinion that applies).
	if wantsZig(need) {
		return adviseDoc{
			Scope: scopeExceptionToGo, Tree: tree, Rules: exceptionToGoRules,
			Intent: route, IntentSource: source, Next: routeFor(route, tree, dir),
		}
	}
	// The path outranks the need's wording: a tree that is not a gogogo
	// checkout cannot install a unit, but it CAN take one — so the answer is
	// the copy surface plus the route, never the trim mechanics.
	if tree == treeNotCheckout {
		return adviseDoc{
			Scope: scopeForeignTree, Reason: reasonNotCheckout,
			Tree: tree, Presets: matched, Rules: notCheckoutRules,
			Intent: route, IntentSource: source, Next: routeFor(route, tree, dir),
			Capabilities: buildCaps(owners, unitKind),
		}
	}
	// A need that constrains itself to the standard library cannot use
	// any capability (each one is or pulls a dependency), so answering with
	// a 36-entry table buries the useful part. Answer with the Go
	// standards pointer instead.
	if wantsStdlibOnly(need) {
		return adviseDoc{
			Scope: scopeGoStdlib, Reason: reasonStdlibOnly,
			Tree: tree, Rules: stdlibRules,
			Intent: route, IntentSource: source, Next: routeFor(route, tree, dir),
		}
	}
	doc := adviseDoc{
		Scope: scopeTemplate, Tree: tree, Rules: adviseRules,
		Intent: route, IntentSource: source, Next: routeFor(route, tree, dir),
		FirstRun: func() *nextSteps { n := buildNextSteps("<dir>", nil); return &n }(),
	}
	// A need that matches nothing is out of the tool's vocabulary (a foreign
	// stack, a typo, a toaster). The full map below is still the honest
	// answer — same as empty — but the scaffold first-run is withheld: with
	// zero matched presets there is no evidence a gogogo scaffold is what
	// the caller wants. Structural, no ecosystem list involved.
	if strings.TrimSpace(need) != "" && len(matched) == 0 {
		doc.FirstRun = nil
	}
	doc.Capabilities = buildCaps(owners, unitKind)
	doc.Presets = matched
	return doc
}

// buildCaps renders the capability registry as the advise table: one row per
// capability, with the installer switch and (the part that matters on a
// foreign tree) the owned paths — `Dirs`/`Files` are literally what to copy
// when `add` refuses.
func buildCaps(owners map[string]string, unitKind map[string]capabilities.Kind) []adviseCap {
	out := make([]adviseCap, 0, len(capabilities.All))
	for _, c := range capabilities.All {
		out = append(out, adviseCap{
			ID: c.ID, Kind: string(c.Kind), Summary: c.Summary,
			RuntimeOff: c.RuntimeOff, Note: c.Note,
			Dirs: c.Dirs, Files: c.Files,
			Trim: adviseTrim(c, owners, unitKind),
		})
	}
	return out
}

// adviseTrim names the installer switch that removes a capability, so a
// reader of the advise output knows the exact flag. The two non-unit cases
// are spelled out by reason (runtime switch vs core) rather than left as an
// empty cell — "no unit owns it" has two different meanings for the caller
// deciding what to trim.
func adviseTrim(c capabilities.Capability, owners map[string]string, unitKind map[string]capabilities.Kind) string {
	unit, ok := owners[c.ID]
	if !ok {
		if c.RuntimeOff != "" {
			return "not an installer unit (runtime switch)"
		}
		if c.Kind != capabilities.KindCore {
			// A manual-removal slice is not core: it can go, just not through
			// the trim engine. Saying "core" here would tell the reader the
			// opposite of the unit's own contract.
			return "not an installer unit (manual removal)"
		}
		return "not an installer unit (core — always in)"
	}
	if unitKind[unit] == capabilities.KindFeature {
		return "--features " + unit
	}
	return "--plugins " + unit
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
// registry and touches nothing else; dir is an opt-in probe that lets the
// answer say "this path is not a gogogo checkout" instead of listing
// capabilities the caller cannot install there — and, in that case, hand over
// the copy surface plus the porting route.
//
// intent is the caller's route. Empty asks the answer to infer it from the
// need (and says so); naming it overrides the guess.
func Advise(need, format, dir, intent string) (string, error) {
	if format != planFormatText && format != planFormatJSON {
		return "", fmt.Errorf("unknown --format %q (want text|json)", format)
	}
	if !validIntent(intent) {
		return "", fmt.Errorf("unknown --intent %q (want none|%s)",
			intent, strings.Join(intentNames, "|"))
	}
	doc := buildAdviseIn(need, dir, intent)
	if format == planFormatJSON {
		raw, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return "", err
		}
		return string(raw) + "\n", nil
	}
	if doc.Scope == scopeExceptionToGo {
		return renderExceptionToGo(doc), nil
	}
	if doc.Scope == scopeGoStdlib {
		return renderStdlib(doc), nil
	}
	if doc.Scope == scopeForeignTree {
		return renderForeignTree(doc, need), nil
	}
	return renderTemplate(doc, need), nil
}

func runAdvise(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("advise", flag.ContinueOnError)
	need := fs.String("need", "",
		"your use-case in a few words (empty lists every preset)")
	format := fs.String("format", planFormatText, "output format: text|json")
	dir := fs.String("dir", "",
		"optional: check this path too, so the answer can say it is not a "+
			"gogogo checkout (omitted = the answer depends on --need alone)")
	intent := fs.String("intent", "",
		"optional route to answer for: "+strings.Join(intentNames, "|")+
			" (omitted = infer from --need, and say so)")
	fs.SetOutput(stdout)
	if err := fs.Parse(args); err != nil {
		return err
	}
	out, err := Advise(*need, *format, *dir, *intent)
	if err != nil {
		return err
	}
	fmt.Fprint(stdout, out)
	return nil
}
