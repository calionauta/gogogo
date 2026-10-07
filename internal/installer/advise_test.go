package installer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdviseJSONListsEveryCapability(t *testing.T) {
	out, err := Advise("", planFormatJSON, "")
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	var doc struct {
		Rules   []string `json:"rules"`
		Presets []struct {
			Name string `json:"name"`
		} `json:"presets"`
		Capabilities []struct {
			ID         string   `json:"id"`
			Trim       string   `json:"trim"`
			RuntimeOff string   `json:"runtimeOff"`
			Dirs       []string `json:"dirs"`
		} `json:"capabilities"`
		FirstRun struct {
			Login string `json:"login"`
		} `json:"firstRun"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("advise is not valid JSON: %v\n%s", err, out)
	}
	if len(doc.Rules) == 0 {
		t.Error("advise JSON carries no rules")
	}
	if len(doc.Presets) == 0 {
		t.Error("empty need should list every preset")
	}
	if !strings.Contains(doc.FirstRun.Login, "demo1234456") {
		t.Errorf("firstRun.login = %q, want the seeded demo credentials", doc.FirstRun.Login)
	}
	byID := map[string]string{}
	for _, c := range doc.Capabilities {
		byID[c.ID] = c.Trim
	}
	// Spot-check the mapping contract: unit-owned caps name their flag,
	// runtime caps refuse trim, core caps say so.
	if trim := byID["whiteboard"]; !strings.Contains(trim, "--features whiteboard") {
		t.Errorf("whiteboard trim = %q, want --features whiteboard", trim)
	}
	if trim := byID["dagnats"]; !strings.Contains(trim, "--plugins dagnats") {
		t.Errorf("dagnats trim = %q, want --plugins dagnats", trim)
	}
	if trim := byID["llm"]; !strings.Contains(trim, "not an installer unit") {
		t.Errorf("llm trim = %q, want the not-a-unit marker", trim)
	}
	found := false
	wbDirs := false
	for _, c := range doc.Capabilities {
		if c.ID == "nats" && strings.Contains(c.RuntimeOff, "NATS_ENABLED=false") {
			found = true
		}
		if c.ID == "whiteboard" {
			for _, d := range c.Dirs {
				if d == "features/whiteboard" {
					wbDirs = true
				}
			}
		}
	}
	if !found {
		t.Error("nats capability lost its NATS_ENABLED=false off switch")
	}
	if !wbDirs {
		t.Error("whiteboard advise lost its owned dirs (brownfield copy guidance)")
	}
}

func TestAdviseNeedMatchesOfflinePresetFirst(t *testing.T) {
	doc := buildAdvise("my app must work on airplanes with flaky sync")
	if len(doc.Presets) == 0 {
		t.Fatal("need matched no preset")
	}
	if doc.Presets[0].Name != "offline-first" {
		t.Errorf("first preset = %q, want offline-first", doc.Presets[0].Name)
	}
}

// TestAdviseTemplateVocabularyWinsOverStackWords pins the allowlist-only
// rule: with no ecosystem detector left, template vocabulary decides. A
// need that names a foreign stack AND a template use-case is answered with
// the use-case (keep/drop you can act on), never with a stack label.
func TestAdviseEmptyNeedIsTemplate(t *testing.T) {
	doc := buildAdvise("")
	if doc.Scope != "template" || doc.FirstRun == nil {
		t.Errorf("empty need must be full template scope, got %+v", doc.Scope)
	}
}

// TestAdviseGenericWordsNeverLeaveGoScopes pins the allowlist-only
// invariant: generic English words must never route outside Go scopes.
// "fast" once prefix-matched a web-framework signal and sent a Go
// serialization need to a foreign scope; with no ecosystem detector left,
// that routing error has no branch to fall into.
func TestAdviseNeedWithNoMatchReturnsNone(t *testing.T) {
	if got := buildAdvise("quantum toaster firmware"); len(got.Presets) != 0 {
		t.Errorf("nonsense need matched %d presets, want 0", len(got.Presets))
	}
}

func TestAdviseShortKeywordsDoNotPrefixMatch(t *testing.T) {
	// "airplane" must not match the 2-letter "ai" keyword.
	for _, p := range buildAdvise("airplane mode").Presets {
		if p.Name == "ai-features" {
			t.Error("airplane matched ai-features via prefix — short keywords need exact hits")
		}
	}
}

func TestAdviseTextStatesGoFirstRule(t *testing.T) {
	out, err := Advise("", planFormatText, "")
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	for _, want := range []string{
		"Zig", "--features whiteboard",
		"demo@demo.app / demo1234456", "/dagnats/", "docs/use-cases",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("advise text missing %q", want)
		}
	}
}

func TestAdviseRejectsUnknownFormat(t *testing.T) {
	if _, err := Advise("", "yaml", ""); err == nil {
		t.Error("Advise(yaml) should fail fast")
	}
}

func TestRunVersionFlag(t *testing.T) {
	for _, argv := range [][]string{{"--version"}, {"-version"}} {
		out := captureStdout(t, func(w *os.File) {
			if err := Run(context.Background(), argv, devNull(t), w); err != nil {
				t.Fatalf("run %v: %v", argv, err)
			}
		})
		if !strings.HasPrefix(out, "gogogo ") {
			t.Errorf("run %v = %q, want the version line", argv, out)
		}
	}
}

func TestRunAdviseDispatch(t *testing.T) {
	out := captureStdout(t, func(w *os.File) {
		args := []string{"advise", "--need", "AI chatbot", "--format", "json"}
		if err := Run(context.Background(), args, devNull(t), w); err != nil {
			t.Fatalf("run advise: %v", err)
		}
	})
	var doc struct {
		Presets []struct {
			Name string `json:"name"`
		} `json:"presets"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("advise dispatch is not valid JSON: %v\n%s", err, out)
	}
	if len(doc.Presets) == 0 || doc.Presets[0].Name != "ai-features" {
		t.Errorf("AI need did not surface ai-features first: %v", doc.Presets)
	}
}

// TestAdviseStdlibOnlyGoScope covers the third answer shape.
//
// A Go need that constrains itself to the standard library cannot use any
// capability (each is or pulls a dependency), so the 24-row table was both
// useless and misleading: an LLM reading it would recommend PocketBase for a
// task whose spec forbids it. The scope now says so explicitly instead.
func TestAdviseStdlibOnlyGoScope(t *testing.T) {
	cases := []string{
		"TCP RESP key-value server in Go, stdlib only, concurrent clients",
		"Go CLI, no dependencies",
		"single binary Go service",
		"pure go CSV parser",
		"Go library, zero deps",
		"dependency-free Go worker pool",
		"Go, no external packages",
		"vanilla go router",
	}
	for _, need := range cases {
		t.Run(need, func(t *testing.T) {
			doc := buildAdvise(need)
			if doc.Scope != scopeGoStdlib {
				t.Fatalf("scope = %q, want %q", doc.Scope, scopeGoStdlib)
			}
			// The whole point: no capability table and no scaffold steps, or
			// the answer is as expensive as before and still misleading.
			if len(doc.Capabilities) != 0 {
				t.Errorf("stdlib scope must omit the capability table, got %d rows", len(doc.Capabilities))
			}
			if doc.FirstRun != nil {
				t.Error("stdlib scope must omit scaffold first-run")
			}
			if len(doc.Rules) == 0 {
				t.Fatal("stdlib scope must still give the caller opinions")
			}
			// It must name where the Go standards live, or the caller is left
			// with an empty answer.
			joined := strings.Join(doc.Rules, " ")
			if !strings.Contains(joined, "gogogo-coding-standards") {
				t.Errorf("stdlib scope must point at the Go standards, got: %s", joined)
			}
		})
	}
}

// TestAdviseStdlibScopeDoesNotHijackTemplateNeeds is the reverse guard: the
// stdlib scope must not swallow needs the template actually answers —
// suppressing the capability table for someone who can use it is the most
// expensive wrong answer this tool can give.
func TestAdviseStdlibScopeDoesNotHijackTemplateNeeds(t *testing.T) {
	cases := []string{
		"",
		"offline-first todo with AI",
		"realtime whiteboard",
		"background jobs and email",
		"durable workflow onboarding",
		"todo app with file uploads",
		"SaaS with credits and billing",
		"crdt store with storage",
		"golang API serving a Next.js frontend with background jobs",
	}
	for _, need := range cases {
		t.Run(need, func(t *testing.T) {
			if doc := buildAdvise(need); doc.Scope != scopeTemplate {
				t.Errorf("scope = %q, want template for %q", doc.Scope, need)
			}
		})
	}
}

// TestAdviseConstraintNeedsStayOnGoScopes pins the post-detector ordering:
// with no foreign branch left, a dependency constraint routes by the
// constraint itself. "Rust server, no dependencies" cannot be answered as
// Rust (nothing here knows Rust); it is answered as what it constrains to —
// no template capabilities apply — with rules that no longer assume Go.
func TestAdviseStdlibTextNamesTheMismatch(t *testing.T) {
	out, err := Advise("Go CLI, no dependencies", planFormatText, "")
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	for _, want := range []string{"does not apply", "gogogo-coding-standards"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}
	// The capability table must be gone, not rendered empty.
	if strings.Contains(out, "capabilities (id, kind") {
		t.Errorf("text output still prints the capability header:\n%s", out)
	}
}

// TestAdviseDirProbeChangesTheAnswer covers the opt-in --dir signal.
//
// Without --dir the answer is a pure function of --need, and that stayed
// byte-identical (verified against the previous revision). With it, a path that
// is not a gogogo checkout must suppress the capability table even for a
// template-shaped need — 24 units are not installable into an arbitrary
// project, so listing them is the same misleading answer the stdlib scope was
// introduced to stop.
func TestAdviseDirProbeChangesTheAnswer(t *testing.T) {
	notCheckout := t.TempDir()
	writeFile(t, notCheckout+"/go.mod", "module example.com/ng\n\ngo 1.22\n")
	writeFile(t, notCheckout+"/cmd/svc/main.go", "package main\n\nfunc main() {}\n")

	t.Run("foreign path suppresses the capability table", func(t *testing.T) {
		doc := buildAdviseIn("realtime whiteboard", notCheckout)
		if doc.Scope != scopeGoStdlib {
			t.Fatalf("scope = %q, want %q", doc.Scope, scopeGoStdlib)
		}
		if doc.Reason != reasonNotCheckout {
			t.Errorf("reason = %q, want %q", doc.Reason, reasonNotCheckout)
		}
		if doc.Tree != treeNotCheckout {
			t.Errorf("tree = %q, want %q", doc.Tree, treeNotCheckout)
		}
		if len(doc.Capabilities) != 0 {
			t.Errorf("capability table must be omitted off-template, got %d rows", len(doc.Capabilities))
		}
		if doc.FirstRun != nil {
			t.Error("scaffold first-run must be omitted off-template")
		}
		if len(doc.Rules) == 0 {
			t.Error("must still explain why and what to do instead")
		}
	})

	t.Run("no dir keeps the need-only answer", func(t *testing.T) {
		doc := buildAdviseIn("realtime whiteboard", "")
		if doc.Scope != scopeTemplate {
			t.Fatalf("scope = %q, want template", doc.Scope)
		}
		if doc.Tree != treeUnknown {
			t.Errorf("tree = %q, want empty (nobody looked)", doc.Tree)
		}
		if doc.Reason != "" {
			t.Errorf("reason = %q, want empty on the template path", doc.Reason)
		}
	})

	t.Run("the template's own tree still answers as template", func(t *testing.T) {
		// A dir probe that finds a real checkout must NOT change the answer.
		doc := buildAdviseIn("realtime whiteboard", templateRootDir(t))
		if doc.Scope != scopeTemplate {
			t.Fatalf("scope = %q, want template", doc.Scope)
		}
		if doc.Tree != treeCheckout {
			t.Errorf("tree = %q, want %q", doc.Tree, treeCheckout)
		}
	})
}

// TestAdviseReasonDistinguishesTheTwoStdlibCauses pins the field that keeps the
// two conditions from looking identical. Both produce scope=go-standards, but
// "your need forbids dependencies" and "your path is not a checkout" call for
// different next steps, so a caller branching on scope alone would be wrong.
func TestAdviseReasonDistinguishesTheTwoStdlibCauses(t *testing.T) {
	foreign := t.TempDir()
	writeFile(t, foreign+"/go.mod", "module example.com/x\n\ngo 1.22\n")

	byNeed := buildAdviseIn("Go CLI, no dependencies", "")
	byPath := buildAdviseIn("realtime whiteboard", foreign)

	if byNeed.Scope != byPath.Scope {
		t.Fatalf("both conditions must share the scope, got %q vs %q", byNeed.Scope, byPath.Scope)
	}
	if byNeed.Reason != reasonStdlibOnly {
		t.Errorf("need-driven reason = %q, want %q", byNeed.Reason, reasonStdlibOnly)
	}
	if byPath.Reason != reasonNotCheckout {
		t.Errorf("path-driven reason = %q, want %q", byPath.Reason, reasonNotCheckout)
	}
	if byNeed.Reason == byPath.Reason {
		t.Error("the two causes must be distinguishable")
	}
}

// templateRootDir walks up from the test's working directory to the checkout
// that contains cmd/web/main.go. Local to this file: it is the only place here
// that needs the real tree rather than a temp copy.
func templateRootDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "cmd", "web", "main.go")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("walked to the filesystem root without finding cmd/web/main.go")
		}
		dir = parent
	}
}
