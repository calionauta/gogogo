// SCOPE:plugin — Spike 1 evidence: the declaration seam behaves as claimed.
//
// The binary success criterion from the research page: mounting a recipe set must
// touch nothing but the declaration and the generated registration, and a
// collision must be reportable BEFORE anything is built or booted.
package recipe

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// fakeProvider is a recipe's copied code, reduced to the contract.
type fakeProvider struct {
	id   string
	decl Declaration
	h    chi.Router
}

func (f fakeProvider) ID() string               { return f.id }
func (f fakeProvider) Declaration() Declaration { return f.decl }
func (f fakeProvider) Routes() chi.Router       { return f.h }

// newProvider builds a provider answering one path.
func newProvider(id string, decl Declaration, pattern string) fakeProvider {
	r := chi.NewRouter()
	r.Get(pattern, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(id))
	})
	return fakeProvider{id: id, decl: decl, h: r}
}

// TestSeamMountsRecipeWithoutCoreEdit is the binary criterion in miniature: a
// recipe reaches the live router through the seam, and the root never restated
// the URL.
func TestSeamMountsRecipeWithoutCoreEdit(t *testing.T) {
	reg := &Registry{}
	reg.Add(newProvider("pomodoro", Declaration{
		Routes: []Route{{Method: "GET", Pattern: "/pomodoro"}},
		Jobs:   []string{"ext:pomodoro:tick"},
		Tables: []string{"ext_pomodoro_session"},
	}, "/pomodoro"))

	root := chi.NewRouter()
	if err := MountWith(reg, root); err != nil {
		t.Fatalf("mount must succeed for a valid set: %v", err)
	}

	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/pomodoro", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected the recipe route to answer 200, got %d", rec.Code)
	}
	if rec.Body.String() != "pomodoro" {
		t.Fatalf("expected the recipe's own handler to answer, got %q", rec.Body.String())
	}
}

// TestSeamIgnoresADisabledProvider is the optional-unit rule: a unit whose
// engine did not start returns a nil provider, and registration must be a
// no-op rather than a declaration with nothing behind it. The route gate reads
// the registry, so a phantom claim would be reported as "declares but does not
// mount" — the failure this rule removes.
func TestSeamIgnoresADisabledProvider(t *testing.T) {
	reg := &Registry{}
	reg.Add(nil)

	if got := len(reg.Providers()); got != 0 {
		t.Fatalf("a nil provider must not be registered, got %d providers", got)
	}
	root := chi.NewRouter()
	if err := MountWith(reg, root); err != nil {
		t.Fatalf("mount of an empty set must succeed: %v", err)
	}
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/room", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a disabled unit must serve nothing, got %d", rec.Code)
	}
}

// TestSeamRejectsDuplicateRouteBeforeBuild is the property that makes a community
// safe: two recipes claiming one method+pattern is reported with BOTH owners
// named, and Mount refuses. The existing router panic stays as the last line of
// defence; this is the first, and it runs on declarations alone.
func TestSeamRejectsDuplicateRouteBeforeBuild(t *testing.T) {
	reg := &Registry{}
	decl := Declaration{Routes: []Route{{Method: "GET", Pattern: "/dashboard"}}}
	reg.Add(newProvider("pomodoro", decl, "/dashboard"))
	reg.Add(newProvider("timer", decl, "/dashboard"))

	conflicts := reg.Validate()
	if len(conflicts) == 0 {
		t.Fatal("two recipes claiming GET /dashboard must conflict")
	}
	if !strings.Contains(conflicts[0].String(), "pomodoro") ||
		!strings.Contains(conflicts[0].String(), "timer") {
		t.Fatalf("the conflict must name both owners, got %q", conflicts[0].String())
	}

	root := chi.NewRouter()
	if err := MountWith(reg, root); err == nil {
		t.Fatal("mount must refuse an invalid set rather than panic at request time")
	}
}

// TestSeamRejectsUnprefixedJob asserts the attribution rule: a job type that does
// not carry its recipe prefix cannot be drained or attributed on uninstall, so it
// is refused at validation rather than discovered later.
func TestSeamRejectsUnprefixedJob(t *testing.T) {
	reg := &Registry{}
	reg.Add(newProvider("pomodoro", Declaration{
		Routes: []Route{{Method: "GET", Pattern: "/pomodoro"}},
		Jobs:   []string{"tick"},
	}, "/pomodoro"))

	conflicts := reg.Validate()
	if len(conflicts) == 0 {
		t.Fatal("an unprefixed job type must be refused")
	}
	if !strings.Contains(conflicts[0].String(), "no recipe prefix") {
		t.Fatalf("the refusal must explain the rule, got %q", conflicts[0].String())
	}
}

// TestSeamRejectsRecipeOwningACoreTable is the same boundary as the drift check,
// enforced one step earlier: at validation, before anything is applied.
func TestSeamRejectsRecipeOwningACoreTable(t *testing.T) {
	reg := &Registry{}
	reg.Add(newProvider("pomodoro", Declaration{
		Routes: []Route{{Method: "GET", Pattern: "/pomodoro"}},
		Tables: []string{"todos"},
	}, "/pomodoro"))

	conflicts := reg.Validate()
	if len(conflicts) == 0 {
		t.Fatal("a recipe declaring a core table must be refused")
	}
	if !strings.Contains(conflicts[0].String(), "core") {
		t.Fatalf("the refusal must name the core owner, got %q", conflicts[0].String())
	}
}

// TestSeamIsDeterministic asserts mounting order is ID order, not registration
// order. Without it a boot log and the generated file can disagree, which makes a
// conflict report depend on file order.
func TestSeamIsDeterministic(t *testing.T) {
	reg := &Registry{}
	reg.Add(newProvider("zeta", Declaration{}, "/z"))
	reg.Add(newProvider("alpha", Declaration{}, "/a"))

	got := reg.Providers()
	if got[0].ID() != "alpha" || got[1].ID() != "zeta" {
		t.Fatalf("providers must be in ID order, got %s then %s", got[0].ID(), got[1].ID())
	}
}

// TestSeamValidSetReportsNoConflict is the negative control: without it every
// rejection test above could be passing by rejecting everything.
func TestSeamValidSetReportsNoConflict(t *testing.T) {
	reg := &Registry{}
	reg.Add(newProvider("pomodoro", Declaration{
		Routes: []Route{{Method: "GET", Pattern: "/pomodoro"}},
		Jobs:   []string{"ext:pomodoro:tick"},
		Tables: []string{"ext_pomodoro_session"},
		Slots:  []string{"dashboard.card"},
	}, "/pomodoro"))
	reg.Add(newProvider("review", Declaration{
		Routes: []Route{{Method: "GET", Pattern: "/review"}},
		Tables: []string{"ext_review_item"},
	}, "/review"))

	if conflicts := reg.Validate(); len(conflicts) != 0 {
		t.Fatalf("a valid set must report no conflicts, got %v", conflicts)
	}
}
