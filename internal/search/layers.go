// SCOPE:plugin — the three layers behind one name, selected by convention.
package search

import (
	"context"
	"errors"
	"fmt"
)

// Mode names, and the default. The zero value means SubstringMode, so a
// consumer that forgets to configure search still gets working search rather
// than an error — the convention over configuration this plugin promises.
const (
	SubstringMode = "substring"
	FuzzyMode     = "fuzzy"
	BM25Mode      = "bm25"
)

// New builds the searcher for a mode. The switch is the ONLY place that knows
// the three exist: consumers hold a Searcher and never branch on a mode,
// which is what keeps a fourth layer — the database's own FTS, the day the
// driver ships it — a one-line addition here.
//
// source is the consumer's rows (required by the two in-memory layers);
// index is the plugin's postings (required only by BM25). Passing an index
// the mode does not need is not an error: a caller wires what it has.
func New(mode string, source Source, index Index) (Searcher, error) {
	switch mode {
	case "", SubstringMode:
		if source == nil {
			return nil, errors.New("search: substring mode needs a source")
		}
		return Substring{Source: source}, nil
	case FuzzyMode:
		if source == nil {
			return nil, errors.New("search: fuzzy mode needs a source")
		}
		return Fuzzy{Source: source}, nil
	case BM25Mode:
		if index == nil {
			return nil, errors.New("search: bm25 mode needs an index")
		}
		return BM25{Index: index}, nil
	default:
		return nil, fmt.Errorf("search: unknown mode %q (want %s, %s or %s)",
			mode, SubstringMode, FuzzyMode, BM25Mode)
	}
}

// Substring is the first layer: the SOURCE does the work in its own SQL. It
// has no ranking and no typo tolerance — it exists because it costs nothing
// (no extra write, no scan in Go) and because it is the layer that still
// works when the heavy modes are off or the plugin is trimmed.
type Substring struct {
	Source Source
}

// Search implements Searcher.
func (s Substring) Search(ctx context.Context, q Query) ([]Hit, error) {
	return s.Source.Substring(ctx, q)
}
