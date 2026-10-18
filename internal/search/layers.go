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

// Modes lists the layer names in presentation order (cheap to strongest).
// The order is the prompt/UI contract: a caller renders a picker from this and
// never hardcodes the names, so a fourth layer appears everywhere at once.
func Modes() []string {
	return []string{SubstringMode, FuzzyMode, BM25Mode}
}

// ModeValid reports whether a mode name is one this plugin builds.
func ModeValid(mode string) bool {
	switch mode {
	case SubstringMode, FuzzyMode, BM25Mode:
		return true
	default:
		return false
	}
}

// NewAll builds every mode the caller has the inputs for, so a consumer can
// offer the choice at RUNTIME instead of pinning it at boot.
//
// Every layer is built from the SAME source and index, so switching modes
// changes only how the same set of rows is ranked — never which rows are
// candidates. A mode whose required input is missing is simply absent from the
// map (no index → no bm25), which is also how a trimmed build keeps working:
// the caller renders a picker from the keys it got.
//
// Building all three costs nothing until one is used: the substring and fuzzy
// layers are query-time structs, and BM25 only holds the (already-built)
// index. The index's WRITE cost is the consumer's decision, unchanged by this.
func NewAll(source Source, index Index) map[string]Searcher {
	layers := make(map[string]Searcher, len(Modes()))
	if source != nil {
		layers[SubstringMode] = Substring{Source: source}
		layers[FuzzyMode] = Fuzzy{Source: source}
	}
	if index != nil {
		layers[BM25Mode] = BM25{Index: index}
	}
	return layers
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
