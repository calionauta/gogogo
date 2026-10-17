// SCOPE:plugin — the Ask router's contract at the prompt seam.
package components

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubRouter stands in for the classifier: it reports one name (or one
// error) and never touches a network or a model.
type stubRouter struct {
	name string
	err  error
}

func (s stubRouter) Choose(ctx context.Context, request string) (string, error) {
	return s.ChooseFor(ctx, request, DefaultSurfacesAsk)
}

// ChooseFor is the surface-aware form both legs call. It returns the stub's
// name verbatim — including a name outside the given surfaces — so the tests
// can prove the CALLER rejects it (that rejection is the contract).
func (s stubRouter) ChooseFor(context.Context, string, []Surface) (string, error) {
	return s.name, s.err
}

// TestAskPromptLeadsWithRouterPick is the feature's whole point: the
// router's name reaches the prompt as the lead component. Red-proof: drop
// the PromptForCatalogLead call from AskPrompt and this fails on the
// missing sentence.
func TestAskPromptLeadsWithRouterPick(t *testing.T) {
	t.Parallel()
	got := AskPrompt(context.Background(), stubRouter{name: "bar_chart"}, "plot my week", nil)
	if !strings.Contains(got, "Lead with bar_chart as the top-level component.") {
		t.Errorf("prompt did not lead with the router's pick:\n%s", got)
	}
	if !strings.Contains(got, "User request: plot my week") {
		t.Errorf("prompt lost the request:\n%s", got)
	}
	// The catalog stays advertised: a section still needs its children, so
	// narrowing the offered set would break nested answers.
	if !strings.Contains(got, "section {title, children:[<component>]}") {
		t.Errorf("prompt narrowed the catalog, want it fully advertised:\n%s", got)
	}
}

// TestAskPromptRouterOffIsUnchanged is the compatibility guarantee the
// opt-in default rests on: with no router the prompt is byte-identical to
// the catalog-only prompt, so an install that never enables the router
// cannot be affected by this feature's existence.
func TestAskPromptRouterOffIsUnchanged(t *testing.T) {
	t.Parallel()
	want := PromptForCatalog() + "\nUser request: plan my day"
	if got := AskPrompt(context.Background(), nil, "plan my day", nil); got != want {
		t.Errorf("router-less prompt = %q, want the catalog prompt %q", got, want)
	}
	// A router with no confident pick answers ("", nil) — the same bytes,
	// not merely a similar prompt.
	if got := AskPrompt(context.Background(), stubRouter{}, "plan my day", nil); got != want {
		t.Errorf("empty pick = %q, want the catalog prompt %q", got, want)
	}
}

// TestAskPromptRouterFailureFallsBack pins the fail-open shape: a broken
// router (endpoint down, bad credentials) must not fail the request. The
// model still answers with the whole catalog.
func TestAskPromptRouterFailureFallsBack(t *testing.T) {
	t.Parallel()
	want := PromptForCatalog() + "\nUser request: plan my day"
	got := AskPrompt(context.Background(), stubRouter{err: errors.New("dial tcp: refused")}, "plan my day", nil)
	if got != want {
		t.Errorf("failed router changed the prompt: %q, want %q", got, want)
	}
}

// TestAskPromptIgnoresNamesAskCannotUse covers the two labels that must
// never become a lead: chat's html_block (Ask's prompt does not advertise
// it, so leading with it invites a component Ask cannot render as intended)
// and a name outside the registry.
func TestAskPromptIgnoresNamesAskCannotUse(t *testing.T) {
	t.Parallel()
	want := PromptForCatalog() + "\nUser request: x"
	for _, name := range []string{"html_block", "not_a_component", ""} {
		if got := AskPrompt(context.Background(), stubRouter{name: name}, "x", nil); got != want {
			t.Errorf("pick %q changed the prompt: %q, want %q", name, got, want)
		}
	}
}

// TestCatalogComponentsMatchesWhatAskAdvertises keeps the router's labels
// and the prompt's promised names from drifting apart: every label the
// classifier may choose is a name Ask advertises, and html_block — chat's
// affordance — is offered to neither.
func TestCatalogComponentsMatchesWhatAskAdvertises(t *testing.T) {
	t.Parallel()
	prompt := PromptForCatalog()
	names := CatalogComponents()
	if len(names) < 2 {
		t.Fatalf("CatalogComponents returned %d names, want the registry", len(names))
	}
	for _, name := range names {
		if !strings.Contains(prompt, name+" {") {
			t.Errorf("router label %q is not advertised by the Ask prompt", name)
		}
		if !Registered(name) {
			t.Errorf("router label %q does not resolve in the registry", name)
		}
	}
	if strings.Contains(prompt, "html_block") {
		t.Error("Ask's prompt advertises html_block")
	}
	if got := strings.Join(names, ","); strings.Contains(got, "html_block") {
		t.Errorf("CatalogComponents offers html_block: %s", got)
	}
}
