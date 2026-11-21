package productapi

// The wire shape lives in two places on purpose, and that is the risk.
//
// internal/server encodes it (unitDescriptor/verbDescriptor) for a product that
// has a Registry; this package decodes it into Unit/Verb for a client that has
// no checkout. Neither imports the other — that is the boundary
// cmd/check-product-boundary enforces — so the compiler cannot notice when one
// side renames a tag. The shipped MCP would fail to parse descriptors and every
// package test would stay green, which is the same silent, one-sided drift that
// TestDeclaredActionsMatchTheRegistry closes one layer down.
//
// These tests pin the JSON KEYS against a literal copy of the server's payload.
// They do not import internal/server: deriving the fixture from this package's
// types would test nothing, since a renamed tag would simply agree with itself.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// serverPayload is the shape internal/server/extdescriptors.go emits: a bare
// JSON ARRAY of units, not an envelope object. A client that expects
// {"units": …} decodes nothing and reports an empty call surface, so the
// top-level container is part of the contract these tests defend.
const serverPayload = `[
  {
    "id": "todo",
    "verbs": [
      {
        "verb": "create",
        "method": "POST",
        "pattern": "/todo",
        "summary": "add a row",
        "params": ["title"],
        "query": ["filter"],
        "rawBody": "application/json",
        "page": "/todo",
        "help": {"title": "what to call it"}
      }
    ]
  },
  {
    "id": "config-view",
    "roles": ["admin"],
    "verbs": [
      {"verb": "read", "method": "GET", "pattern": "/config"}
    ],
    "omitted": [
      {"route": {"method": "POST", "pattern": "/config/role"}, "reason": "an agent must not author its own permissions"}
    ]
  }
]`

func decodeServerPayload(t *testing.T) []Unit {
	t.Helper()
	var units []Unit
	if err := json.Unmarshal([]byte(serverPayload), &units); err != nil {
		t.Fatalf("the server payload no longer decodes: %v", err)
	}
	if len(units) != 2 {
		t.Fatalf("got %d units, want 2", len(units))
	}
	return units
}

// TestDecodesTheServerPayload is the drift alarm. If internal/server renames a
// json tag, this fails even though nothing here changed — which is the point.
func TestDecodesTheServerPayload(t *testing.T) {
	units := decodeServerPayload(t)

	todo := units[0]
	if todo.ID != "todo" {
		t.Errorf("unit id = %q, want todo (the `id` tag drifted)", todo.ID)
	}
	if len(todo.Verbs) != 1 {
		t.Fatalf("todo has %d verbs, want 1", len(todo.Verbs))
	}
	v := todo.Verbs[0]
	checks := []struct{ got, want, tag string }{
		{v.Verb, "create", "verb"},
		{v.Method, "POST", "method"},
		{v.Pattern, "/todo", "pattern"},
		{v.Summary, "add a row", "summary"},
		{v.RawBody, "application/json", "rawBody"},
		{v.Page, "/todo", "page"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q — the json tag drifted between the server and this client",
				c.tag, c.got, c.want)
		}
	}
	if len(v.Params) != 1 || v.Params[0] != "title" {
		t.Errorf("params = %v, want [title]", v.Params)
	}
	if len(v.Query) != 1 || v.Query[0] != "filter" {
		t.Errorf("query = %v, want [filter]", v.Query)
	}
	if v.Help["title"] != "what to call it" {
		t.Errorf("help = %v, want the title key", v.Help)
	}

	cfg := units[1]
	if len(cfg.Roles) != 1 || cfg.Roles[0] != "admin" {
		t.Errorf("roles = %v, want [admin]", cfg.Roles)
	}
	if len(cfg.Omitted) != 1 {
		t.Fatalf("omitted = %+v, want one entry", cfg.Omitted)
	}
	// The nested `route` object is the trap: a client that flattens it reads an
	// empty method and pattern while decoding without error.
	if cfg.Omitted[0].Route.Method != http.MethodPost ||
		cfg.Omitted[0].Route.Pattern != "/config/role" {
		t.Errorf("omitted route = %+v, want POST /config/role", cfg.Omitted[0].Route)
	}
	if cfg.Omitted[0].Reason == "" {
		t.Error("the omission lost its reason — the only part that tells an agent why")
	}
}

// TestDescribeAndInvokeAgreeOnTheVerbs ties the two MCP tools together: the
// verbs product_describe reports must be the ones product_invoke will accept.
// An agent reads the first and calls the second, so a mismatch is a tool that
// advertises what it cannot do.
func TestDescribeAndInvokeAgreeOnTheVerbs(t *testing.T) {
	units := decodeServerPayload(t)
	for _, u := range units {
		for _, v := range u.Verbs {
			if _, err := findVerb(units, u.ID, v.Verb); err != nil {
				t.Errorf("describe lists %s.%s but invoke refuses it: %v", u.ID, v.Verb, err)
			}
		}
	}
}

// TestInvokeRefusesAnUndeclaredVerb pins the other half: a verb that is not in
// the descriptor must fail before the wire, with an error naming what IS
// available so the agent can correct itself instead of guessing.
func TestInvokeRefusesAnUndeclaredVerb(t *testing.T) {
	units := decodeServerPayload(t)
	_, err := findVerb(units, "todo", "destroy_everything")
	if err == nil {
		t.Fatal("an undeclared verb was accepted")
	}
	if !strings.Contains(err.Error(), "create") {
		t.Errorf("the refusal does not name what is available: %v", err)
	}
}
