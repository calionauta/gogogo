// Package gogogofullstacktemplate root deploy-contract tests.
//
// The LLM triple (GOAI_API_KEY + GOAI_BASE_URL + GOAI_MODEL) must belong
// to ONE provider and live in ONE place: the secrets env_file the deploy
// workflow renders (GH secrets + vars). Compose `environment` always wins
// over `env_file`, so a GOAI_BASE_URL/GOAI_MODEL default there silently
// shadows the matching secrets values and splits the triple across
// providers — the real outage behind Ask/Chat failing every call with
// OpenRouter's 401 "Missing Authentication header" while the key was
// Groq's. This pins the invariant: re-adding either key under
// environment fails the test.
package gogogofullstacktemplate

import (
	"os"
	"strings"
	"testing"
)

// TestDeployComposeKeepsLLMTripleInSecretsFile pins the single-source rule
// for the LLM triple. It scans the gogogo service's `environment:` block
// for GOAI_BASE_URL / GOAI_MODEL keys (any value, including a passthrough
// default: even an empty expansion shadows env_file) and requires the
// secrets env_file to stay referenced.
func TestDeployComposeKeepsLLMTripleInSecretsFile(t *testing.T) {
	raw, err := os.ReadFile("deploy/docker-compose.prod.yml")
	if err != nil {
		t.Fatalf("read compose file: %v", err)
	}
	lines := strings.Split(string(raw), "\n")

	// Locate the gogogo service block, then its environment block,
	// tracking indent so nested keys (healthcheck test args, logging
	// options) cannot confuse the scan.
	svcIdx := -1
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "gogogo:" && indentOf(ln) == 2 {
			svcIdx = i
			break
		}
	}
	if svcIdx < 0 {
		t.Fatal("gogogo service block not found in compose file")
	}
	envIdx := -1
	for i := svcIdx + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "environment:" && indentOf(lines[i]) == 4 {
			envIdx = i
			break
		}
		// A same-or-lower indent key ends the service block.
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") &&
			strings.HasSuffix(trimmed, ":") && indentOf(lines[i]) <= 2 {
			break
		}
	}
	if envIdx < 0 {
		t.Fatal("environment block not found under the gogogo service")
	}
	for i := envIdx + 1; i < len(lines); i++ {
		ln := lines[i]
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if indentOf(ln) <= 4 {
			break // end of the environment block
		}
		key := trimmed
		if cut, _, ok := strings.Cut(trimmed, ":"); ok {
			key = strings.TrimSpace(cut)
		}
		if key == "GOAI_BASE_URL" || key == "GOAI_MODEL" {
			t.Errorf("compose environment sets %s (line %d): it shadows the "+
				"secrets env_file and splits the LLM triple across providers; "+
				"set it via GH vars instead", key, i+1)
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	// The secrets file must stay referenced: without env_file the triple
	// has no home at all.
	joined := string(raw)
	if !strings.Contains(joined, "env_file:") || !strings.Contains(joined, ".secrets/") {
		t.Error("compose file no longer references the secrets env_file (.secrets/); " +
			"the LLM triple would have nowhere to load from")
	}
}

// indentOf counts leading spaces. Tabs never appear in this file; a tab
// indents as 4 so a mixed-indent edit still terminates blocks sanely.
func indentOf(s string) int {
	n := 0
	for _, r := range s {
		if r == ' ' {
			n++
		} else if r == '\t' {
			n += 4
		} else {
			break
		}
	}
	return n
}
