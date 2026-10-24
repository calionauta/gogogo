// SCOPE:plugin — the panel boundary at the CATALOG: what a model may write,
// what parses, and what renders. The todo feature's own rendering is tested
// there (features/todo/components); this file is about the seam.
package components

import (
	"slices"
	"strings"
	"testing"
)

// panelNameInTest / todoFiltersInTest / TodoRowInTest describe the STUB panel
// this package installs in TestMain (registry_test.go): the catalog seam is
// provider-based, so these tests exercise the boundary through a provider, not
// through the todo feature.

// slicesContain keeps the assertion readable without pulling in a helper
// package for one call.
func slicesContain(haystack []string, needle string) bool {
	return slices.Contains(haystack, needle)
}

// TestTodoListValidatesTheFilter pins the closed set: the model chooses
// WHICH slice to show, and anything outside the enum fails closed, so a bad
// filter is a corrected answer rather than a query the store cannot answer.
func TestTodoListValidatesTheFilter(t *testing.T) {
	t.Parallel()
	for _, filter := range todoFiltersInTest {
		raw := `{"components":[{"type":"todo_list","props":{"filter":"` + filter + `"}}]}`
		dirs, err := ParseDirectives(raw)
		if err != nil {
			t.Fatalf("filter %q rejected: %v", filter, err)
		}
		if len(dirs) != 1 || dirs[0].Component != panelNameInTest {
			t.Fatalf("filter %q parsed to %+v", filter, dirs)
		}
	}
	raw := `{"components":[{"type":"todo_list","props":{"filter":"mine"}}]}`
	if _, err := ParseDirectives(raw); err == nil {
		t.Error("an unknown filter parsed, want it rejected")
	}
}

// TestTodoListRejectsUnknownProps is the point of a system component: the
// panel shows the APP's state, so the model picks a slice and nothing else.
//
// This test used to accept the HYDRATED shape (filter + rows) as parseable,
// on the reasoning that rows exist so hydration can fill them. That was the
// leak: rows live in TodoListProps because it is the RENDER shape, and the
// parse path validated against that struct, so a model answer supplying its
// own rows parsed clean and was only corrected because Hydrate happens to
// overwrite them. Safety by provider discipline, not by construction.
//
// The boundary is now panelSpec.writable: the model may write `filter` and
// nothing else, so the hydrated shape is rejected when it comes from the
// model (a repairable shape error) and still renders when it comes from
// hydration (see TestHydratedPanelRenders below).
func TestTodoListRejectsUnknownProps(t *testing.T) {
	t.Parallel()
	serverOwned := `{"components":[{"type":"todo_list","props":{"filter":"open",` +
		`"rows":[{"id":"1","title":"rows are the app's to write","done":false}]}}]}`
	if _, err := ParseDirectives(serverOwned); err == nil {
		t.Error("a model answer supplying rows parsed, want it rejected — rows are hydration's")
	}
	invented := `{"components":[{"type":"todo_list","props":{"filter":"open",` +
		`"todos":["invented by the model"]}}]}`
	if _, err := ParseDirectives(invented); err == nil {
		t.Error("a prop outside the render shape parsed, want it rejected")
	}
}

// TestHydratedPanelRenders is the other half of the boundary: rejecting rows
// at PARSE time must not break RENDER, because persisted answers are stored
// hydrated and re-rendered through the same renderers (a chat reload renders
// rows straight from the store). If the writable list ever leaked into the
// render path, stored history would stop rendering.
func TestHydratedPanelRenders(t *testing.T) {
	t.Parallel()
	hydrated := Directive{Component: panelNameInTest, Props: map[string]any{
		"filter": "open",
		"rows":   []TodoRowInTest{{ID: "1", Title: "real row from the store"}},
	}}
	comps, err := RenderAll([]Directive{hydrated})
	if err != nil {
		t.Fatalf("RenderAll(hydrated panel): %v", err)
	}
	if len(comps) != 1 {
		t.Fatalf("rendered %d components, want 1", len(comps))
	}
}

// TestSurfaceBoundedAdvertisement pins the advertisement boundary per SURFACE,
// which is the contract that replaced "both are chat-only".
//
// todo_list is NATIVE: it shows the app's own data, so it is advertised
// exactly when the native band is on. html_block is CUSTOM: the model invents
// the markup, so it is in Chat's default and absent from a catalog-only set —
// and absent from that set's ROUTER candidates too, so no classifier pick can
// reach a surface the prompt never named. Every band toggles independently;
// all off is prose-only.
func TestSurfaceBoundedAdvertisement(t *testing.T) {
	t.Parallel()
	catalogNative := NormalizeSurfaces([]Surface{SurfaceCatalog, SurfaceNative})
	catalogOnly := NormalizeSurfaces([]Surface{SurfaceCatalog})
	chat := NormalizeSurfaces(DefaultSurfacesChat)

	for _, set := range [][]Surface{catalogNative, chat} {
		if !slicesContain(CatalogComponentsFor(set), panelNameInTest) {
			t.Errorf("native %s missing from the router's candidates for %v", panelNameInTest, Names(set))
		}
		if !strings.Contains(PromptForSurfaces(set), panelNameInTest) {
			t.Errorf("native %s not advertised in the prompt for %v", panelNameInTest, Names(set))
		}
	}
	if slicesContain(CatalogComponentsFor(catalogOnly), panelNameInTest) {
		t.Errorf("native-off router still offers %s: %v", panelNameInTest, CatalogComponentsFor(catalogOnly))
	}
	if strings.Contains(PromptForSurfaces(catalogOnly), panelNameInTest) {
		t.Errorf("native-off prompt still names %s", panelNameInTest)
	}

	// Custom is in Chat's default, by prompt AND by router candidates, and
	// withheld from a catalog-only set.
	if !strings.Contains(PromptForChat(), htmlBlockName) {
		t.Error("chat prompt does not advertise html_block")
	}
	if strings.Contains(PromptForSurfaces(catalogOnly), htmlBlockName) {
		t.Error("catalog-only prompt names html_block, want the invented-markup surface withheld")
	}
	if slicesContain(CatalogComponentsFor(catalogOnly), htmlBlockName) {
		t.Errorf("catalog-only router offers %s: %v", htmlBlockName, CatalogComponentsFor(catalogOnly))
	}
	if got := PromptForCatalogLeadSurfaces(htmlBlockName, catalogOnly); got != PromptForSurfaces(catalogOnly) {
		t.Error("a custom-only lead changed the catalog-only prompt")
	}

	// The catalog band is on by default, so a leaf is routable everywhere.
	for _, set := range [][]Surface{catalogOnly, chat} {
		if !slicesContain(CatalogComponentsFor(set), "data_table") {
			t.Errorf("catalog leaf missing for %v", Names(set))
		}
	}
}
