package installer

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestAdviseJSONListsEveryCapability(t *testing.T) {
	out, err := Advise("", planFormatJSON)
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	var doc struct {
		Rules   []string `json:"rules"`
		Presets []struct {
			Name string `json:"name"`
		} `json:"presets"`
		Capabilities []struct {
			ID         string `json:"id"`
			Trim       string `json:"trim"`
			RuntimeOff string `json:"runtimeOff"`
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
	for _, c := range doc.Capabilities {
		if c.ID == "nats" && strings.Contains(c.RuntimeOff, "NATS_ENABLED=false") {
			found = true
		}
	}
	if !found {
		t.Error("nats capability lost its NATS_ENABLED=false off switch")
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
	out, err := Advise("", planFormatText)
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	for _, want := range []string{"Zig", "--features whiteboard", "demo@demo.app / demo1234456", "/dagnats/"} {
		if !strings.Contains(out, want) {
			t.Errorf("advise text missing %q", want)
		}
	}
}

func TestAdviseRejectsUnknownFormat(t *testing.T) {
	if _, err := Advise("", "yaml"); err == nil {
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
