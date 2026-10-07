package installer

import (
	"strings"
	"testing"
)

func TestAdviseTemplateVocabularyWinsOverStackWords(t *testing.T) {
	for _, need := range []string{
		"Next.js dashboard with realtime cursors",
		"golang API serving a Next.js frontend with background jobs",
		"I want to go with Next.js for realtime",
	} {
		doc := buildAdvise(need)
		if doc.Scope != scopeTemplate {
			t.Errorf("%q → scope %q, want template", need, doc.Scope)
		}
	}
	doc := buildAdvise("Next.js dashboard with realtime cursors")
	if len(doc.Presets) == 0 || doc.Presets[0].Name != "realtime-collab" {
		t.Fatalf("realtime need did not surface realtime-collab: %+v", doc.Presets)
	}
}

func TestAdviseGenericWordsNeverLeaveGoScopes(t *testing.T) {
	for _, need := range []string{
		"native serialization fast path behind a C ABI, with a pure-Go fallback",
		"fast",
		"fast path codec",
	} {
		if doc := buildAdvise(need); doc.Scope == scopeNativeKernel {
			t.Errorf("%q → scope native-kernel, want a Go scope", need)
		} else if doc.Scope != scopeTemplate && doc.Scope != scopeGoStdlib {
			t.Errorf("%q → scope %q, want template or go-standards", need, doc.Scope)
		}
	}
	// The full need carries "pure-Go", so it must land exactly on the
	// stdlib-only scope; without the constraint it is a full-map miss.
	doc := buildAdvise("native serialization fast path behind a C ABI")
	if doc.Scope != scopeTemplate {
		t.Errorf("unconstrained codec need → %q, want template full map", doc.Scope)
	}
	doc = buildAdvise("native serialization fast path behind a C ABI, with a pure-Go fallback")
	if doc.Scope != scopeGoStdlib || doc.Reason != reasonStdlibOnly {
		t.Errorf("constrained codec need → %q/%q, want go-standards/stdlib-only", doc.Scope, doc.Reason)
	}
}

func TestAdviseRoomAuthoritySurfacesGoAkt(t *testing.T) {
	// The reference demo must be discoverable: presenter/room/turn needs
	// surface room-authority (keep goakt), the unit the demo ships in.
	for _, need := range []string{
		"take turns presenting in a shared room",
		"game lobby with player arbitration",
	} {
		found := false
		for _, p := range buildAdvise(need).Presets {
			if p.Name == "room-authority" {
				found = true
				if len(p.Keep) != 1 || p.Keep[0] != "goakt" {
					t.Errorf("%q keep = %v, want [goakt]", need, p.Keep)
				}
			}
		}
		if !found {
			t.Errorf("%q did not surface room-authority", need)
		}
	}
}

// TestAdviseLayVocabularySurfacesRightPresets pins lay words (several are
// cross-language loanwords): people never say "offline-first", they say
// "no internet" — and the preset must still surface.
func TestAdviseLayVocabularySurfacesRightPresets(t *testing.T) {
	cases := []struct{ need, preset string }{
		{"market with no internet, sync later on wifi", "offline-first"},
		{"live chat with the crew", "realtime-collab"},
		{"reminder emails every night", "background-jobs"},
		{"a simple website with a blog", "marketing-site"},
	}
	for _, tc := range cases {
		found := false
		for _, p := range buildAdvise(tc.need).Presets {
			if p.Name == tc.preset {
				found = true
			}
		}
		if !found {
			t.Errorf("%q did not surface %s: %+v", tc.need, tc.preset, buildAdvise(tc.need).Presets)
		}
	}
}

func TestAdviseFourLetterWordsDoNotAbbreviateKeywords(t *testing.T) {
	// "back" (as in "sync when back") must not match the "backend"
	// keyword: 4-letter words never abbreviate longer keywords.
	for _, p := range buildAdvise("sync when back").Presets {
		if p.Name == "quiet-api" {
			t.Error("back matched quiet-api/backend via prefix — need 5+ letters to abbreviate")
		}
	}
	// Genuine 5+ letter abbreviations still match.
	matched := false
	for _, p := range buildAdvise("collab canvas").Presets {
		if p.Name == "realtime-collab" {
			matched = true
		}
	}
	if !matched {
		t.Error("collab canvas did not surface realtime-collab")
	}
}

func TestAdviseConstraintNeedsStayOnGoScopes(t *testing.T) {
	for _, need := range []string{
		"Rust server, no dependencies",
		"TCP server in Python, stdlib only",
		"Next.js app with no external deps",
	} {
		doc := buildAdvise(need)
		if doc.Scope != scopeGoStdlib {
			t.Errorf("%q → scope %q, want go-standards", need, doc.Scope)
		}
		if len(doc.Capabilities) != 0 {
			t.Errorf("%q carries %d capabilities, want none", need, len(doc.Capabilities))
		}
	}
}

// TestAdviseZigGetsTheGate guards the one documented exception: Zig is not
// a foreign stack, it is the repo's native escape hatch, so it gets the
// gate plus the skill pointer — never trim mechanics, never a disclaimer
// to "check their docs" (the docs are ours: docs/native-zig.md).
func TestAdviseZigGetsTheGate(t *testing.T) {
	for _, need := range []string{
		"Zig kernel, zero dependencies",
		"zig kernel with no deps",
		"Zig TCP server stdlib only",
	} {
		doc := buildAdvise(need)
		if doc.Scope != scopeNativeKernel {
			t.Errorf("%q → scope %q, want native-kernel", need, doc.Scope)
		}
		if len(doc.Capabilities) != 0 {
			t.Errorf("%q carries %d capabilities, want none", need, len(doc.Capabilities))
		}
		if doc.FirstRun != nil {
			t.Errorf("%q carries scaffold first-run, want none", need)
		}
		joined := strings.Join(doc.Rules, " ")
		for _, want := range []string{"docs/native-zig.md", "gogogo-coding-standards", "pprof"} {
			if !strings.Contains(joined, want) {
				t.Errorf("%q rules missing %q", need, want)
			}
		}
	}
}

// TestAdviseZigzagDoesNotTriggerZig pins the near-miss: "zigzag" is codec
// vocabulary (varint spec), not the language. wordHit must not route it to
// the native-kernel scope — "zig" is 3 letters so the prefix rule cannot
// engage in either direction.
func TestAdviseZigzagDoesNotTriggerZig(t *testing.T) {
	doc := buildAdvise("zigzag varint codec in pure go, no dependencies")
	if doc.Scope != scopeGoStdlib {
		t.Errorf("zigzag need → scope %q, want go-standards", doc.Scope)
	}
}

// TestAdviseUnmatchedNeedGetsFullMapWithoutScaffold pins the out-of-domain
// answer: no ecosystem list, no guessed label — the full map (same as
// empty), the preset names as recovery hint, but no scaffold first-run
// inviting a scaffold nothing matched.
func TestAdviseUnmatchedNeedGetsFullMapWithoutScaffold(t *testing.T) {
	// "blog" is a universal loanword: even a Django blog is a static-public
	// use-case, so it matches marketing-site (keep landing) with the full
	// map behind it.
	blog := buildAdvise("Django blog")
	if blog.Scope != scopeTemplate || blog.FirstRun == nil {
		t.Errorf("Django blog → %q (firstrun %v), want template with first-run", blog.Scope, blog.FirstRun != nil)
	}
	blogged := false
	for _, p := range blog.Presets {
		if p.Name == "marketing-site" {
			blogged = true
		}
	}
	if !blogged {
		t.Errorf("Django blog did not surface marketing-site: %+v", blog.Presets)
	}
	for _, need := range []string{"quantum toaster firmware"} {
		doc := buildAdvise(need)
		if doc.Scope != scopeTemplate {
			t.Errorf("%q → scope %q, want template", need, doc.Scope)
		}
		if len(doc.Presets) != 0 {
			t.Errorf("%q matched %d presets, want 0", need, len(doc.Presets))
		}
		if len(doc.Capabilities) == 0 {
			t.Errorf("%q must still show the full capability map", need)
		}
		if doc.FirstRun != nil {
			t.Errorf("%q must omit scaffold first-run", need)
		}
	}
	out, err := Advise("quantum toaster firmware", planFormatText, "")
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	for _, want := range []string{"no preset matched", "only knows the gogogo (Go) template"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "first run (defaults") {
		t.Error("text output must omit first-run for an unmatched need")
	}
}

// TestAdviseNativeKernelText asserts the rendered gate says the gate.
func TestAdviseNativeKernelText(t *testing.T) {
	out, err := Advise("Zig kernel", planFormatText, "")
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	for _, want := range []string{"native kernel (Zig)", "docs/native-zig.md", "gogogo-coding-standards"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "capabilities (id, kind") {
		t.Error("text output must not print the capability table for a kernel need")
	}
}

// TestAdviseStdlibTextNamesTheMismatch asserts the rendered text says WHY the
// template does not apply, rather than returning a silent, near-empty answer.
