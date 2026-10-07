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
	Reason       string         `json:"reason,omitempty"`
	Rules        []string       `json:"rules"`
	Presets      []advisePreset `json:"presets,omitempty"`
	Capabilities []adviseCap    `json:"capabilities,omitempty"`
	FirstRun     *nextSteps     `json:"firstRun,omitempty"`
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

// buildAdvise is buildAdviseIn with no path: the pure, need-only answer.
// Kept as the entry point every existing caller and test uses, so the
// default output is unchanged byte-for-byte by the --dir feature.
func buildAdvise(need string) adviseDoc {
	return buildAdviseIn(need, "")
}

// buildAdviseIn resolves the registry into guidance, filtering presets by
// need (empty need returns every preset, most useful first is meaningless
// without a query — manifest order wins). There is deliberately NO foreign
// detector: this tool only knows the template plus its Zig exception, so a
// need that matches nothing gets the full map (same as empty) instead of a
// guessed stack label. Generic English words ("fast", "path", "native")
// can never route outside Go scopes — there is no branch left that names
// another ecosystem.
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
	// The one documented exception first: a Zig kernel gets the gate even
	// when the need also carries a dependency constraint ("Zig, zero deps"
	// is about the kernel, and the gate is the opinion that applies).
	if wantsZig(need) {
		return adviseDoc{
			Scope: scopeNativeKernel, Tree: probeTree(dir),
			Rules: nativeKernelRules,
		}
	}
	// The path outranks the need's wording: if the caller points at a tree
	// that is not a gogogo checkout, the capability table is inapplicable
	// whatever the need says.
	if dir != "" && !looksLikeTemplate(dir) {
		return adviseDoc{
			Scope: scopeGoStdlib, Reason: reasonNotCheckout,
			Tree: treeNotCheckout, Presets: matched, Rules: notCheckoutRules,
		}
	}
	// A need that constrains itself to the standard library cannot use
	// any capability (each one is or pulls a dependency), so answering with
	// the 24-entry table buries the useful part. Answer with the Go
	// standards pointer instead.
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
	// A need that matches nothing is out of the tool's vocabulary (a foreign
	// stack, a typo, a toaster). The full map below is still the honest
	// answer — same as empty — but the scaffold first-run is withheld: with
	// zero matched presets there is no evidence a gogogo scaffold is what
	// the caller wants. Structural, no ecosystem list involved.
	if strings.TrimSpace(need) != "" && len(matched) == 0 {
		doc.FirstRun = nil
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
	doc.Presets = matched
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

// renderNativeKernel is the native-kernel-scope text: the gate, the skill
// pointer, the normative doc. No trim mechanics, no capability table, no
// presets — a kernel need matches template vocabulary only by accident, and
// printing it would invite installing web units into a codec.
// Split out so Advise stays under the gocyclo gate.
func renderNativeKernel(doc adviseDoc) string {
	var b strings.Builder
	b.WriteString("native kernel (Zig) — the gogogo template does not install here.\n" +
		"Zig is this repo's one documented native exception, so the opinion " +
		"below is the gate, not a foreign-stack disclaimer.\n\n")
	b.WriteString("rules:\n")
	for _, r := range doc.Rules {
		fmt.Fprintf(&b, "  - %s\n", r)
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
// Split out so Advise stays under the gocyclo gate, like renderNativeKernel.
func renderStdlib(doc adviseDoc) string {
	var b strings.Builder
	switch doc.Reason {
	case reasonNotCheckout:
		b.WriteString("the path is not a gogogo checkout — the template does not apply here.\n" +
			"No capability is installable into it: this tool trims and extends its own scaffold,\n" +
			"not an arbitrary project.\n\n")
	default:
		b.WriteString("dependency-constrained need — the gogogo template does not apply here.\n" +
			"No capability is installable: each one adds or belongs to a dependency\n" +
			"this need forbids, so the registry is omitted rather than shown empty.\n\n")
	}
	b.WriteString("rules:\n")
	for _, r := range doc.Rules {
		fmt.Fprintf(&b, "  - %s\n", r)
	}
	return b.String()
}

// renderTemplate is the template-scope text: rules, keep/drop presets with
// the no-match recovery hint, the full capability table, and the scaffold
// first-run (withheld when a non-empty need matched nothing — a miss must
// not invite scaffolding the wrong thing).
// Split out so Advise stays under the gocyclo gate, like the other renders.
func renderTemplate(doc adviseDoc, need string) string {
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
		fmt.Fprintf(&b, "  %s\n", notApplicableHint)
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
	if n := doc.FirstRun; n != nil {
		b.WriteString("\nfirst run (defaults; PORT overrides the port):\n")
		fmt.Fprintf(&b, "  %s\n", strings.Replace(n.Dev, "<dir>", "my-app", 1))
		fmt.Fprintf(&b, "  app %s · login %s\n", n.App, n.Login)
		fmt.Fprintf(&b, "  admin %s\n", n.Admin)
		fmt.Fprintf(&b, "  workflows %s\n", n.Flows)
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
	if doc.Scope == scopeNativeKernel {
		return renderNativeKernel(doc), nil
	}
	if doc.Scope == scopeGoStdlib {
		return renderStdlib(doc), nil
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
