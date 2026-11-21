package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/ui"
)

// The dashboard is the product's summary, so an agent that cannot read it is
// blind to the one place the instance describes itself. This pins the shape the
// headless `read` verb answers with, and — the part that matters — that it is
// the SAME data the page renders, taken from the same loadCounts call.
func TestDashboardReadAnswersTheSameCountsThePageRenders(t *testing.T) {
	t.Parallel()

	// A session as the page sees it: own scope, nothing declared.
	var c counts
	cards := buildCards(t.Context(), []ui.Module(nil), nil, c, false)

	rec := httptest.NewRecorder()
	writeDashboardJSON(t.Context(), rec, cards, false)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}

	var got dashboardPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
	}
	if got.Scope != "mine" {
		t.Errorf("scope = %q, want mine", got.Scope)
	}
	if len(got.Cards) != len(cards) {
		t.Errorf("cards = %d, want %d", len(got.Cards), len(cards))
	}
}

// The admin view is the same payload with the instance-wide scope, and the
// fence is the handler's — RBAC starts off in dev, so the route alone is not a
// guarantee. A read that skipped this would hand every session admin counts.
func TestDashboardReadRefusesNonAdminForInstanceScope(t *testing.T) {
	t.Parallel()

	h := &Handler{}
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/dashboard?scope=all", nil)

	h.read(w, r, &auth.ValidatedSession{Role: "user"})

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a non-admin asking for instance scope", w.Code)
	}
}

// An unknown scope is a caller error, not a silent fallback to "mine": a typo
// must not read as a successful request for the wrong view.
func TestDashboardReadRejectsUnknownScope(t *testing.T) {
	t.Parallel()

	h := &Handler{}
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/dashboard?scope=everyone", nil)

	h.read(w, r, &auth.ValidatedSession{Role: "admin"})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unknown scope", w.Code)
	}
}

// The declaration is the contract the gate reads: the route must be present and
// the verb must name it, or the headless rule reports the slice as invisible.
func TestDashboardDeclaresTheReadVerbOverTheRouteItServes(t *testing.T) {
	t.Parallel()

	p := (&Handler{}).Recipe()
	d := p.Declaration()

	var route *recipe.Route
	for i := range d.Routes {
		if d.Routes[i].Pattern == dashboardAPIPath {
			route = &d.Routes[i]
			break
		}
	}
	if route == nil {
		t.Fatalf("declaration does not serve %s", dashboardAPIPath)
	}
	if route.Method != http.MethodGet {
		t.Errorf("%s method = %s, want GET (a read mutates nothing)", dashboardAPIPath, route.Method)
	}

	var verb *recipe.InvokeAction
	for i := range d.Actions {
		if d.Actions[i].Verb == "read" {
			verb = &d.Actions[i]
			break
		}
	}
	if verb == nil {
		t.Fatal(`no "read" action: the dashboard stays invisible to an agent`)
	}
	if verb.Route.Pattern != dashboardAPIPath {
		t.Errorf("read verb targets %q, want %q", verb.Route.Pattern, dashboardAPIPath)
	}
	if problems := recipe.HeadlessCoverageProblems(d); len(problems) > 0 {
		t.Errorf("headless coverage: %v", problems)
	}
}
