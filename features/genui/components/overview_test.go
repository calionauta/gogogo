// SCOPE:plugin — the operator catalog covers the registry it documents.
package components

import (
	"slices"
	"testing"
)

// TestCatalogOverviewCoversRegistry pins the page against the prompt: every
// name the model may emit appears in the overview, and every overview entry
// resolves in the registry — a component that renders but is not listed is
// an operator blind spot, and one listed but unrenderable is a lie.
func TestCatalogOverviewCoversRegistry(t *testing.T) {
	t.Parallel()
	got := CatalogOverview()
	names := make([]string, 0, len(got))
	for _, info := range got {
		names = append(names, info.Name)
		if !Registered(ResolvePanelName(info.Name)) && info.Name != sectionName {
			t.Errorf("overview lists %q, which resolves to nothing", info.Name)
		}
		if info.Blurb == "" {
			t.Errorf("overview entry %q carries no blurb", info.Name)
		}
	}
	for _, want := range CatalogComponentsFor(DefaultSurfacesChat) {
		if !slices.Contains(names, want) {
			t.Errorf("overview misses %q, which the Chat prompt advertises", want)
		}
	}
}
