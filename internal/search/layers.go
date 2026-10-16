// SCOPE:plugin — the three layers behind one name, selected by convention.
package search

import (
	"context"
	"errors"
	"fmt"
)

// Mode names, and the default. The zero value means SubstringMode, so a
// consumer that forgets to configure search still gets working search
// rather than an error — the convention over configuration this plugin
// promises.
const (
	SubstringMode = "substring"
	FuzzyMode     = "fuzzy"
	BM25Mode      = "bm25"
)

// New builds the searcher for a mode. The switch is the ONLY place that
// knows the three exist: consumers hold a Searcher and never branch on a
// mode (that is what keeps a fourth layer — the database's own FTS, when
// the driver ships it — a one-line addition here).
func New(mode string, store Store) (Searcher, error) {
	if store == nil {
		return nil, errors.New("search: nil store")
	}
	switch mode {
	case "", SubstringMode:
		return Substring{Store: store}, nil
	case FuzzyMode:
		return Fuzzy{Store: store}, nil
	case BM25Mode:
		return BM25{Store: store}, nil
	default:
		return nil, fmt.Errorf("search: unknown mode %q (want %s, %s or %s)",
			mode, SubstringMode, FuzzyMode, BM25Mode)
	}
}

// Substring is the first layer: the store does the work in SQL. It has no
// ranking and no typo tolerance — it exists because it costs nothing (no
// extra write, no scan in Go) and it is the layer that still works when the
// heavy modes are switched off or the plugin is trimmed.
type Substring struct {
	Store Store
}

// Search implements Searcher.
func (s Substring) Search(ctx context.Context, q Query) ([]Hit, error) {
	return s.Store.Substring(ctx, q)
}
