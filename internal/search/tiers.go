// SCOPE:plugin — the fuzzy tier rules, split from fuzzy.go for the file gate.
package search

import "strings"

// Tier scores: named because the numbers are a contract (a test asserts
// each) and a caller may reason about "stronger than a typo match".
const (
	tierExact       = 1.00 // the whole query appears in the field, contiguous
	tierToken       = 0.95 // every query token IS a field token (not contiguous)
	tierPrefix      = 0.90 // every query token is a prefix of a field token
	tierEdit        = 0.80 // every query token is one edit away
	tierSubsequence = 0.75 // every query token is a subsequence
)

// scoreDoc applies the tiers to one document with AND semantics: one token
// that matches nothing rejects the whole row, which is what a user typing
// two words expects.
//
// THREE PARALLEL VIEWS of the field, built in one pass: the raw tokens, their
// folds, and each token's byte offset in the normalized text. The offsets are
// computed by scanning FORWARD from the previous token's end rather than by
// searching the whole string per token — a whole-string `strings.Index` finds
// the first occurrence anywhere, so the second "invoice" in a field would
// report the first one's offset, and a highlight would land on the wrong word.
//
// Comparison happens between FOLDS; the span is the RAW token's. Folding for
// LOCATING would be the tempting simplification and it is wrong: a stem
// ("invoic") is not a substring of the document ("invoices"), so the offset
// lookup would silently return -1 and the row would be found but not
// highlightable.
func scoreDoc(id, query string, terms []string, field string) (Hit, bool) {
	norm, fieldNorm := normalize(query), normalize(field)
	if norm != "" {
		if start := strings.Index(fieldNorm, norm); start >= 0 {
			return Hit{ID: id, Score: tierExact, Spans: []Span{{Start: start, End: start + len(norm)}}}, true
		}
	}
	raw, folded, offsets := tokenizeWithOffsets(field)
	weakest, first, firstLen := tierExact, -1, 0
	for _, term := range terms {
		// terms arrive already folded (stemAll); folding here too would apply
		// Stem twice, and Porter is not idempotent on every INPUT token
		// ("decis" folds to "deci") — so a double fold can move a query token
		// away from the index key it should have matched.
		tier, idx, ok := bestTier(term, folded)
		if !ok {
			return Hit{}, false
		}
		if tier < weakest {
			weakest = tier
		}
		if idx >= 0 && (first < 0 || offsets[idx] < first) {
			first, firstLen = offsets[idx], len(raw[idx])
		}
	}
	hit := Hit{ID: id, Score: weakest}
	if first >= 0 {
		hit.Spans = []Span{{Start: first, End: first + firstLen}}
	}
	return hit, true
}

// tokenizeWithOffsets returns a field's raw tokens, their folds, and each
// token's byte offset in the normalized field — aligned by index.
//
// The forward scan is what makes the offsets exact; `normalize` collapses runs
// of punctuation into single spaces, so token i's offset is where the previous
// token ended plus one separator, and searching on from there is O(n) with no
// ambiguity.
func tokenizeWithOffsets(field string) (raw, folded []string, offsets []int) {
	fieldNorm := normalize(field)
	raw = Tokenize(field)
	folded = make([]string, len(raw))
	offsets = make([]int, len(raw))
	from := 0
	for i, tok := range raw {
		j := strings.Index(fieldNorm[from:], tok)
		if j < 0 {
			// Cannot happen for a token this function itself produced (every
			// token comes from this field); leaving the offset at 0 is the
			// honest fallback — better a wide highlight than a wrong one — and
			// the scan does not advance, so a following token can still find
			// its own position.
			continue
		}
		offsets[i] = from + j
		from = offsets[i] + len(tok)
		folded[i] = Stem(tok)
	}
	return raw, folded, offsets
}

// bestTier returns the strongest tier one (already folded) query token reaches
// against a field, plus the INDEX of the field token that matched. Callers map
// that index through the offsets slice, which is why this returns an index and
// not a byte offset — the two parallel views cannot then drift apart.
func bestTier(term string, folded []string) (float64, int, bool) {
	best, at := 0.0, -1
	for i, ft := range folded {
		switch {
		case ft == term:
			best = max(best, tierToken)
		case len(ft) >= len(term) && strings.HasPrefix(ft, term):
			best = max(best, tierPrefix)
		case editWithinOne(term, ft):
			best = max(best, tierEdit)
		case subsequenceEither(term, ft):
			best = max(best, tierSubsequence)
		}
		if best > 0 && at < 0 {
			at = i
		}
	}
	return best, at, best > 0
}
