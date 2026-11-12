// Catalog version contracts.
//
// The fingerprint is derived from the advertised vocabulary, so the tests pin
// the properties a hand-bumped version cannot offer: stable for the same set,
// different for any structural change.
package components

import (
	"slices"
	"testing"
)

func TestCatalogVersionOfIsStableAndOrderSensitive(t *testing.T) {
	names := []string{"data_table", "section", "text_note"}
	if a, b := catalogVersionOf(names), catalogVersionOf(slices.Clone(names)); a != b {
		t.Fatalf("same names produced %q then %q", a, b)
	}
	// Different order is a different advertised list (CatalogComponentsFor
	// sorts), so it must not collide with a stable hash over the sequence.
	reordered := []string{"section", "data_table", "text_note"}
	if catalogVersionOf(names) == catalogVersionOf(reordered) {
		t.Fatal("reordering the vocabulary did not change the fingerprint")
	}
}

func TestCatalogVersionOfMovesOnAnyChange(t *testing.T) {
	base := []string{"data_table", "section", "text_note"}
	cases := map[string][]string{
		"added":     {"data_table", "plan_cards", "section", "text_note"},
		"removed":   {"data_table", "text_note"},
		"renamed":   {"data_table", "section", "text_card"},
		"empty set": {},
	}
	want := catalogVersionOf(base)
	for name, names := range cases {
		if got := catalogVersionOf(names); got == want {
			t.Errorf("%s vocabulary produced the base fingerprint %q", name, got)
		}
	}
}

// TestCatalogVersionFollowsTheAdvertisedSet is the join between the pure hash
// and the registry: the version chat stamps is the hash of what the chat
// surfaces advertise, so adding or retiring a component moves it without an
// edit anywhere.
func TestCatalogVersionFollowsTheAdvertisedSet(t *testing.T) {
	if got, want := CatalogVersion(), catalogVersionOf(CatalogComponents()); got != want {
		t.Fatalf("CatalogVersion() = %q, want the fingerprint of the advertised set %q", got, want)
	}
	if CatalogVersion() == "" {
		t.Fatal("CatalogVersion() is empty")
	}
}
