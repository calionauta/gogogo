// SCOPE:plugin — the Chat router's contract at the prompt seam.
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
	return s.ChooseFor(ctx, request, DefaultSurfacesChat)
}

// ChooseFor is the surface-aware form the Chat leg calls. It returns the
// stub's name verbatim — including a name outside the given surfaces — so
// the tests can prove the CALLER rejects it (that rejection is the
// contract).
func (s stubRouter) ChooseFor(context.Context, string, []Surface) (string, error) {
	return s.name, s.err
}

// TestConversationPromptLeadsWithRouterPick is the feature's whole point:
// the router's name reaches the prompt as the lead component. Red-proof:
// drop the lead call from ConversationPrompt and this fails on the missing
// sentence.
func TestConversationPromptLeadsWithRouterPick(t *testing.T) {
	t.Parallel()
	got := ConversationPrompt(context.Background(), stubRouter{name: "bar_chart"}, "plot my week", nil)
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

// TestConversationPromptRouterOffIsUnchanged is the compatibility guarantee
// the opt-in default rests on: with no router the prompt is byte-identical
// to the plain prompt, so an install that never enables the router cannot
// be affected by this feature's existence.
func TestConversationPromptRouterOffIsUnchanged(t *testing.T) {
	t.Parallel()
	want := PromptForSurfaces(nil) + "\nUser request: plan my day"
	if got := ConversationPrompt(context.Background(), nil, "plan my day", nil); got != want {
		t.Errorf("router-less prompt = %q, want %q", got, want)
	}
	// A router with no confident pick answers ("", nil) — the same bytes,
	// not merely a similar prompt.
	if got := ConversationPrompt(context.Background(), stubRouter{}, "plan my day", nil); got != want {
		t.Errorf("empty pick = %q, want %q", got, want)
	}
}

// TestConversationPromptRouterFailureFallsBack pins the fail-open shape: a
// broken router (endpoint down, bad credentials) must not fail the request.
// The model still answers with the whole vocabulary.
func TestConversationPromptRouterFailureFallsBack(t *testing.T) {
	t.Parallel()
	want := PromptForSurfaces(nil) + "\nUser request: plan my day"
	got := ConversationPrompt(context.Background(), stubRouter{err: errors.New("dial tcp: refused")}, "plan my day", nil)
	if got != want {
		t.Errorf("failed router changed the prompt: %q, want %q", got, want)
	}
}

// TestConversationPromptIgnoresUnusableNames covers labels that must never
// become a lead with a catalog-only surface set: html_block (not advertised
// without the custom surface) and a name outside the registry.
func TestConversationPromptIgnoresUnusableNames(t *testing.T) {
	t.Parallel()
	catalogOnly := []Surface{SurfaceCatalog, SurfaceNative}
	want := PromptForSurfaces(catalogOnly) + "\nUser request: x"
	for _, name := range []string{"html_block", "not_a_component", ""} {
		if got := ConversationPrompt(context.Background(), stubRouter{name: name}, "x", catalogOnly); got != want {
			t.Errorf("pick %q changed the prompt: %q, want %q", name, got, want)
		}
	}
	// Unknown names never become a lead, whatever the set.
	defWant := PromptForSurfaces(nil) + "\nUser request: x"
	for _, name := range []string{"not_a_component", ""} {
		if got := ConversationPrompt(context.Background(), stubRouter{name: name}, "x", nil); got != defWant {
			t.Errorf("pick %q changed the default prompt: %q, want %q", name, got, defWant)
		}
	}
}

// TestCatalogComponentsMatchesWhatChatAdvertises keeps the router's labels
// and the prompt's promised names from drifting apart: every label the
// classifier may choose is a name Chat advertises.
func TestCatalogComponentsMatchesWhatChatAdvertises(t *testing.T) {
	t.Parallel()
	prompt := PromptForChat()
	names := CatalogComponents()
	if len(names) < 2 {
		t.Fatalf("CatalogComponents returned %d names, want the registry", len(names))
	}
	for _, name := range names {
		if !strings.Contains(prompt, name+" {") && name != "section" {
			t.Errorf("router label %q is not advertised by the Chat prompt", name)
		}
		if !Registered(name) && name != "section" {
			t.Errorf("router label %q does not resolve in the registry", name)
		}
	}
}
