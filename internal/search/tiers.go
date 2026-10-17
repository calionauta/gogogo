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
func scoreDoc(id, query string, terms []string, field string) (Hit, bool) {
	norm, fieldNorm := normalize(query), normalize(field)
	if norm != "" {
		if start := strings.Index(fieldNorm, norm); start >= 0 {
			return Hit{ID: id, Score: tierExact, Spans: []Span{{Start: start, End: start + len(norm)}}}, true
		}
	}
	fieldTokens := Tokenize(field)
	weakest, first := tierExact, -1
	for _, term := range terms {
		tier, pos, ok := bestTier(term, fieldTokens, fieldNorm)
		if !ok {
			return Hit{}, false
		}
		if tier < weakest {
			weakest = tier
		}
		if first < 0 || (pos >= 0 && pos < first) {
			first = pos
		}
	}
	hit := Hit{ID: id, Score: weakest}
	if first >= 0 {
		hit.Spans = []Span{{Start: first, End: first + len(terms[0])}}
	}
	return hit, true
}

// bestTier returns the strongest tier one query token reaches against a
// field, plus the byte offset of that match when the tier has one.
func bestTier(term string, fieldTokens []string, fieldNorm string) (float64, int, bool) {
	best, pos := 0.0, -1
	for _, ft := range fieldTokens {
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
		if best > 0 && pos < 0 {
			if i := strings.Index(fieldNorm, ft); i >= 0 {
				pos = i
			}
		}
	}
	return best, pos, best > 0
}
