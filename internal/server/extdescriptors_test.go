package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/recipe"
)

// Discovery is the read half of invoking. A caller that can see the verbs but
// not their shape sends params to the wrong place; one that can see neither is
// guessing. This pins the shape a consuming agent reads.
func TestDescriptorsAnswerTheCallSurface(t *testing.T) {
	reg := &recipe.Registry{}
	reg.Add(probeProvider{decl: recipe.Declaration{
		Actions: []recipe.InvokeAction{
			{
				Verb:        "read",
				Description: "Read the summary.",
				Route:       recipe.Route{Method: http.MethodGet, Pattern: "/probe/read"},
				Query:       []string{"scope"},
				BrowserPath: "/probe",
			},
			{
				Verb:        "create",
				Description: "Make one.",
				Route:       recipe.Route{Method: http.MethodPost, Pattern: "/probe"},
				Params:      []string{"title"},
				RawBody:     "body",
			},
		},
	}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, descriptorsPath, nil)
	// #nosec G124 — test cookie: the attributes are the server's concern.
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "session"})
	extDescriptorsHandler(reg).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	var got []unitDescriptor
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("units = %d, want 1", len(got))
	}
	d := got[0]
	if d.ID != probeID {
		t.Errorf("id = %q, want %q", d.ID, probeID)
	}
	if len(d.Verbs) != 2 {
		t.Fatalf("verbs = %d, want 2", len(d.Verbs))
	}
	// Sorted by verb, so two deployments compare without noise.
	if d.Verbs[0].Verb != "create" || d.Verbs[1].Verb != "read" {
		t.Errorf("verbs not sorted: %v", []string{d.Verbs[0].Verb, d.Verbs[1].Verb})
	}
	// WHERE a param travels is part of the contract: a caller that sends a query
	// param in the body gets a handler that sees nothing.
	if q := d.Verbs[1].Query; len(q) != 1 || q[0] != "scope" {
		t.Errorf("read query = %v, want [scope]", q)
	}
	if d.Verbs[1].Method != http.MethodGet {
		t.Errorf("read method = %q, want GET", d.Verbs[1].Method)
	}
	if d.Verbs[0].RawBody != "body" {
		t.Errorf("create rawBody = %q, want body", d.Verbs[0].RawBody)
	}
}

// Discovery is as sensitive as invoke: it names what an authenticated caller can
// do. Anonymous gets nothing.
func TestDescriptorsRefuseWithoutASession(t *testing.T) {
	reg := &recipe.Registry{}
	reg.Add(probeProvider{decl: recipe.Declaration{}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, descriptorsPath, nil)
	extDescriptorsHandler(reg).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for an anonymous request", rec.Code)
	}
}

// The answer is stable: same registry, same bytes. An agent diffing two
// deployments must not read a map reordering as a change.
func TestDescriptorsAreStableAcrossCalls(t *testing.T) {
	reg := &recipe.Registry{}
	for _, id := range []string{"zeta", "alpha", "mike"} {
		reg.Add(staticProvider{id: id})
	}

	first := descriptorsOf(reg)
	second := descriptorsOf(reg)
	if len(first) != len(second) {
		t.Fatalf("lengths differ: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("order differs at %d: %q vs %q", i, first[i].ID, second[i].ID)
		}
	}
	// Registry.Providers() sorts by id, which is what makes this true.
	for i := 1; i < len(first); i++ {
		if first[i-1].ID > first[i].ID {
			t.Errorf("not sorted: %q before %q", first[i-1].ID, first[i].ID)
		}
	}
}
