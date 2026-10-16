// SCOPE:plugin — the plugin's contract: one name per layer, one default.
package search

import "testing"

// TestNewSelectsByMode pins convention over configuration: an empty mode is
// the substring layer (a consumer that never configures anything still gets
// search), each mode yields its own type, and an unknown mode is an error
// naming the valid ones rather than a silent fallback — a typo in an env var
// must not pick a layer the operator did not ask for.
func TestNewSelectsByMode(t *testing.T) {
	t.Parallel()
	store := indexDocs(exampleCorpus().docs, "title")
	for mode, want := range map[string]any{
		"":            Substring{},
		SubstringMode: Substring{},
		FuzzyMode:     Fuzzy{},
		BM25Mode:      BM25{},
	} {
		got, err := New(mode, store)
		if err != nil {
			t.Fatalf("mode %q: %v", mode, err)
		}
		switch want.(type) {
		case Substring:
			if _, ok := got.(Substring); !ok {
				t.Errorf("mode %q = %T, want Substring", mode, got)
			}
		case Fuzzy:
			if _, ok := got.(Fuzzy); !ok {
				t.Errorf("mode %q = %T, want Fuzzy", mode, got)
			}
		case BM25:
			if _, ok := got.(BM25); !ok {
				t.Errorf("mode %q = %T, want BM25", mode, got)
			}
		}
	}
	if _, err := New("turso-fts", store); err == nil {
		t.Error("an unknown mode was accepted, want an error naming the valid ones")
	}
	if _, err := New(FuzzyMode, nil); err == nil {
		t.Error("a nil store was accepted")
	}
}
