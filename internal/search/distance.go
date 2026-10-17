// SCOPE:plugin — the two string distances the fuzzy tiers use.
package search

// editWithinOne reports whether a and b are at most one edit apart, where
// an edit is a substitution, an insertion, a deletion or a TRANSPOSITION of
// two adjacent characters (Damerau). The transposition arm is the one that
// matters in practice: "smoek" for "smoke" and "invioce" for "invoice" are
// two adjacent swaps, and a plain Levenshtein would score both as two edits
// and miss them.
//
// Cost: O(len(a)+len(b)) — one pass, no matrix, because the budget is one.
func editWithinOne(a, b string) bool {
	if a == b {
		return true
	}
	if diff := len(a) - len(b); diff > 1 || diff < -1 {
		return false
	}
	switch {
	case len(a) == len(b):
		// One substitution, or one adjacent transposition.
		first := -1
		for i := range len(a) {
			if a[i] == b[i] {
				continue
			}
			if first < 0 {
				first = i
				continue
			}
			// Second difference: only a transposition of the pair fits the
			// budget, and only if the two characters are each other's.
			return i == first+1 && a[first] == b[i] && a[i] == b[first]
		}
		return first >= 0
	case len(a) == len(b)+1:
		return oneExtra(a, b)
	default:
		return oneExtra(b, a)
	}
}

// oneExtra reports whether longer is shorter plus exactly one character.
func oneExtra(longer, shorter string) bool {
	skipped := false
	for i, j := 0, 0; i < len(longer); i++ {
		if j < len(shorter) && longer[i] == shorter[j] {
			j++
			continue
		}
		if skipped {
			return false
		}
		skipped = true
	}
	return true
}

// subsequenceEither reports whether one token is a subsequence of the other
// with the ORDER preserved — the tier that survives a dropped or doubled
// letter ("handooff" contains "handoff"). Guarded by a length floor and a
// ratio so a short common fragment ("the" inside "thereabouts") cannot
// match every row: below minSubsequenceLen a token may only match by prefix
// or edit, and the shorter side must be at least subsequenceRatio of the
// longer.
const (
	minSubsequenceLen = 3
	subsequenceRatio  = 0.6
)

func subsequenceEither(a, b string) bool {
	short, long := a, b
	if len(short) > len(long) {
		short, long = long, short
	}
	if len(short) < minSubsequenceLen {
		return false
	}
	if float64(len(short)) < subsequenceRatio*float64(len(long)) {
		return false
	}
	return isSubsequence(short, long)
}

// isSubsequence reports whether every byte of short appears in long, in
// order.
func isSubsequence(short, long string) bool {
	j := 0
	for i := 0; i < len(long) && j < len(short); i++ {
		if long[i] == short[j] {
			j++
		}
	}
	return j == len(short)
}
