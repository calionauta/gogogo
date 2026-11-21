package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/recipe"
)

// configReadSecret is the canary, built at runtime so no credential-shaped
// literal lives in the tree: a secret scanner cannot tell a fixture from a key,
// and it should not have to. It is long enough that masked() keeps a
// recognisable prefix, so "the value is absent" cannot be satisfied by a mask
// that leaks the whole thing.
var configReadSecret = "sk-fixture-" + strings.Repeat("ab12", 8)

// The security property of this read: an agent may learn WHAT is configured,
// never the value. A JSON view is the dangerous shape — the HTML page masks in
// the template, but a serializer that reads the config directly would ship the
// secret whole.
func TestConfigReadNeverCarriesASecretValue(t *testing.T) {
	cfg := &config.Config{
		AppName: "probe",
		GoAI:    config.GoAIConfig{APIKey: configReadSecret},
	}

	rec := httptest.NewRecorder()
	writeConfigJSON(t.Context(), rec, configReadBody(cfg))

	body := rec.Body.String()
	if strings.Contains(body, configReadSecret) {
		t.Fatalf("the read leaked a secret value verbatim:\n%s", body)
	}
	// The mask keeps a family prefix on purpose; the point is the value is not
	// recoverable, not that the field disappears.
	if !strings.Contains(body, `"masked":true`) {
		t.Errorf("no row reads as masked — the field should read as configured: %s", body)
	}

	var got configPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var sawAPIKey bool
	for _, g := range got.Groups {
		for _, r := range g.Rows {
			// Two groups carry an APIKey (GoAI, JEV review); the env name is
			// what tells them apart.
			if r.Key != "APIKey" || r.Env != "GOAI_API_KEY" {
				continue
			}
			sawAPIKey = true
			if !r.Set {
				t.Error("APIKey reads as unset while a key is configured")
			}
			if !r.Masked {
				t.Error("APIKey is not flagged as masked")
			}
		}
	}
	if !sawAPIKey {
		t.Fatal("the read does not list APIKey at all — an agent cannot tell what is configured")
	}
}

// AGE_SECRET_KEY is bool-only: the page never shows it, so neither does the read.
func TestConfigReadKeepsBoolOnlyFieldsBoolOnly(t *testing.T) {
	t.Setenv("AGE_SECRET_KEY", configReadSecret)
	cfg := &config.Config{AppName: "probe"}

	rec := httptest.NewRecorder()
	writeConfigJSON(t.Context(), rec, configReadBody(cfg))

	body := rec.Body.String()
	if strings.Contains(body, configReadSecret) {
		t.Fatalf("bool-only field leaked a value:\n%s", body)
	}

	var got configPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, g := range got.Groups {
		for _, r := range g.Rows {
			if r.Key != "AGESecretKey" {
				continue
			}
			if !r.BoolOnly {
				t.Error("AGESecretKey is not flagged bool-only")
			}
			if r.Display != "" {
				t.Errorf("AGESecretKey display = %q, want empty", r.Display)
			}
			if !r.Set {
				t.Error("AGESecretKey reads as unset while it is set")
			}
		}
	}
}

// The declaration is the contract the gate reads.
func TestConfigDeclaresTheReadVerbAndKeepsTheMutationsOmitted(t *testing.T) {
	p := (&Handler{}).Recipe()
	d := p.Declaration()

	var hasRead bool
	for _, a := range d.Actions {
		if a.Verb == "read" {
			hasRead = true
			if a.Route.Pattern != configAPIPath {
				t.Errorf("read targets %q, want %q", a.Route.Pattern, configAPIPath)
			}
			if a.Route.Method != http.MethodGet {
				t.Errorf("read method = %s, want GET", a.Route.Method)
			}
		}
	}
	if !hasRead {
		t.Fatal("no read verb declared")
	}

	// The three mutations stay omissions: letting an agent change a role, revoke
	// a session or toggle a capability is an agent authoring its own permissions.
	omitted := map[string]bool{}
	for _, o := range d.Omitted {
		omitted[o.Route.Method+" "+o.Route.Pattern] = true
	}
	for _, want := range []string{
		"POST /config/users/role",
		"POST /config/users/revoke",
		"POST /config/recipes/toggle",
	} {
		if !omitted[want] {
			t.Errorf("%s is no longer a declared omission", want)
		}
	}
	if problems := recipe.HeadlessCoverageProblems(d); len(problems) > 0 {
		t.Errorf("headless coverage: %v", problems)
	}
}

// An empty config is still a valid answer: the read must not panic or 500 on a
// trimmed build, it must say "nothing is configured".
func TestConfigReadAnswersForAnEmptyConfig(t *testing.T) {
	rec := httptest.NewRecorder()
	writeConfigJSON(t.Context(), rec, configReadBody(nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d for a nil config", rec.Code)
	}
	var got configPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.AppName != "" {
		t.Errorf("app name = %q, want empty", got.AppName)
	}
}
