// SCOPE:plugin — a Porter stemmer, implemented here rather than vendored.
//
// WHY THIS EXISTS. The three layers matched on exact tokens only, so a field
// containing "invoices" was unreachable from a query for "invoice" — the
// recall gap that is the ONE thing an external engine (SQLite's FTS5 with
// `tokenize = 'porter'`) offered that this package did not. FTS5 is not
// available in this driver (see the file header of search.go), and the fix
// does not need it: folding is a pure function of one token.
//
// WHY NOT A DEPENDENCY. A stemmer is ~200 lines of well-specified string
// rules with no I/O and no state, so the trade is the repo's usual one: a
// small, verifiable amount of local code beats a dependency whose transitive
// graph and update cadence we would then own. The algorithm is Porter (1980),
// the same one FTS5's `porter` tokenizer implements, so behavior is the known
// one and not an invention.
//
// WHY `Stem` IS SEPARATE FROM `Tokenize`. This is the design decision that
// matters, and it is a correctness one rather than a taste one: highlight
// offsets are computed by searching the NORMALIZED field for a raw token
// (tiers.go). If Tokenize itself stemmed, a token like "happily" would fold
// to "happili", which is not a substring of the field text, `strings.Index`
// would return -1, and the highlight would silently disappear — a search that
// finds a row but cannot say where it matched. So the tokenizer keeps
// producing the document's real words (positions stay exact) and folding
// happens where two tokens are COMPARED, plus where a posting is keyed.
//
// Only ASCII letter folding is attempted; a token with no ASCII vowels (CJK,
// Cyrillic, emoji) is returned unchanged. That is deliberate: Porter's rules
// are English morphology, and applying them to another script would corrupt
// tokens rather than stem them.
package search

import "strings"

// stemMinLen is the shortest token the stemmer touches. Below it the rules
// have nothing to work with ("sky", "try", "as") and the risk of a bad fold
// outweighs the recall win, which is the same cutoff FTS5's porter usage
// implies.
const stemMinLen = 4

// Stem folds one ALREADY-LOWERCASED token to its Porter stem. Pure function:
// same input, same output, no tables.
//
// THE CONTRACT IS SINGLE-APPLICATION, NOT IDEMPOTENCE, and the difference is
// measured rather than argued. Canonical Porter is NOT idempotent for some
// tokens: the reference implementation folds "cease" -> "ceas" -> "cea" and
// "agreed" -> "agre" -> "agr" (verified against the 42,603-word reference
// corpus, which produces both steps). That is the algorithm, not a bug here.
//
// So the invariant this code upholds is the one the index needs: Stem is applied
// exactly ONCE to an original token, on both the write path (the stored term)
// and the query path (the lookup key). No call site stems a stem, and a test
// pins that (`TestStemIsAppliedExactlyOnce`). If Stem were ever applied twice
// on one side only, the key would move and the row would become unfindable —
// which is the failure idempotence would have prevented, avoided here by
// construction instead.
//
// Stem is stable across processes and releases, which is what makes it safe as
// a PERSISTED key in search_postings. That is why this must not be "improved"
// in place once an index has shipped: the term column holds this output, so a
// changed algorithm orphans every stored posting and is a schema bump plus a
// reindex, not an edit.
//
// FIDELITY: exact parity with the canonical 42,603-word Porter reference corpus
// (snowball-data/porter/voc.txt + output.txt), apart from tokens shorter than
// stemMinLen that we deliberately leave alone — 136 words out of 42,603, all of
// them below the cutoff. No unexplained divergence. That parity is what makes
// this safe to treat as a drop-in for the `porter` tokenizer family, and it is
// enforced rather than claimed: TestStemMatchesReferenceCorpus replays the
// corpus (skipping, loudly, when the files are absent).
//
// Two rules are load-bearing and easy to "fix" into a regression: step2 uses the
// PUBLISHED `abli`->`able` (not the tartarus C source's `-DEPARTURE-`
// `bli`->`ble`), and there is no `logi` rule. Both choices are what the corpus
// encodes. Re-run the corpus check before changing either.
func Stem(tok string) string {
	if len(tok) < stemMinLen || !isASCIIAlpha(tok) {
		return tok
	}
	tok = step1aPlural(tok)
	tok = step1bTense(tok)
	tok = step1cY(tok)
	tok = replaceIfMeasured(tok, step2Rules)  // step 2
	tok = replaceIfMeasured(tok, step3Rules)  // step 3
	tok = stripIfMeasured(tok, step4Rules, 1) // step 4
	tok = step5E(tok)
	return step5DoubledL(tok)
}

// step1aPlural strips plural endings. Porter strips a bare trailing "s" even
// in words like "bus" and "lens"; that is the algorithm (and what the reference
// corpus encodes), so it is kept rather than "fixed".
func step1aPlural(tok string) string {
	switch {
	case strings.HasSuffix(tok, "sses"):
		return tok[:len(tok)-2]
	case strings.HasSuffix(tok, "ies"):
		return tok[:len(tok)-2]
	case strings.HasSuffix(tok, "ss"):
		return tok
	case strings.HasSuffix(tok, "s"):
		return tok[:len(tok)-1]
	}
	return tok
}

// step1bTense strips past tense and gerund endings. `measure` gates the eed
// removal and `hasVowel` the other two, which is the check that keeps "feed"
// from becoming "fe".
func step1bTense(tok string) string {
	switch {
	case strings.HasSuffix(tok, "eed"):
		if measure(tok[:len(tok)-3]) > 0 {
			return tok[:len(tok)-1]
		}
		return tok
	case strings.HasSuffix(tok, "ed") && hasVowel(tok[:len(tok)-2]):
		return step1bCleanup(tok[:len(tok)-2])
	case strings.HasSuffix(tok, "ing") && hasVowel(tok[:len(tok)-3]):
		return step1bCleanup(tok[:len(tok)-3])
	}
	return tok
}

// step1cY turns a final y into i when the stem has a vowel ("happy" -> "happi",
// but "sky" stays).
func step1cY(tok string) string {
	if strings.HasSuffix(tok, "y") && hasVowel(tok[:len(tok)-1]) {
		return tok[:len(tok)-1] + "i"
	}
	return tok
}

// step5E drops a final e when the stem is still pronounceable without it
// ("probate" -> "probat", but "rate" keeps its e).
func step5E(tok string) string {
	if !strings.HasSuffix(tok, "e") {
		return tok
	}
	stem := tok[:len(tok)-1]
	if m := measure(stem); m > 1 || (m == 1 && !endsCVC(stem)) {
		return stem
	}
	return tok
}

// step5DoubledL collapses a doubled l ("controll" -> "control") when the stem
// measures more than 1 — the only doubling Porter allows here, since allowing it
// generally would turn "fall" into "fal".
func step5DoubledL(tok string) string {
	if strings.HasSuffix(tok, "ll") && measure(tok) > 1 {
		return tok[:len(tok)-1]
	}
	return tok
}

// step1bCleanup applies the follow-ups that only make sense after step 1b
// removed a suffix: restore a doubled consonant ("hopping" -> "hop"), or add
// an e back ("hoping" -> "hope").
//
// The doubled-consonant arm requires a CONSONANT, which the naive
// `tok[n-1] == tok[n-2]` test does not: it also collapses a doubled vowel, and
// that turned "agree" into "agre" (measured 0.1% divergence from the reference
// — and worse, "agre" then folds further on a second pass, breaking the
// idempotence the persisted key needs).
func step1bCleanup(tok string) string {
	switch {
	case strings.HasSuffix(tok, "at"), strings.HasSuffix(tok, "bl"), strings.HasSuffix(tok, "iz"):
		return tok + "e"
	case len(tok) >= 2 && tok[len(tok)-1] == tok[len(tok)-2] &&
		isConsonantAt(tok, len(tok)-1) &&
		!strings.ContainsRune("lsz", rune(tok[len(tok)-1])):
		return tok[:len(tok)-1]
	case measure(tok) == 1 && endsCVC(tok):
		return tok + "e"
	}
	return tok
}

// step2Rules are the step-2 suffix replacements, as (suffix, replacement).
//
// ORDER IS LOAD-BEARING: the rule list is scanned first-match-wins, so a
// longer suffix must precede any shorter one it ends with. (Measured: with
// "biliti" after a bare "iti"-style entry, the -ability family diverged.)
//
// THIS LIST FOLLOWS THE PUBLISHED 1980 ALGORITHM, deliberately, and NOT the
// tartarus C source. The C source carries two explicit `-DEPARTURE-` markers:
// it uses `bli`->`ble` where the paper has `abli`->`able`, and adds a
// `logi`->`log` rule the paper lacks. We follow the PAPER because that is what
// the canonical 42,603-word reference corpus was generated from, so the
// contract is exact parity rather than "99% plus a list of known exceptions" —
// an exception list is the kind of thing that silently grows and never gets
// re-checked (see TestStemMatchesReferenceCorpus).
//
// The practical cost is small and known: "possibly" and "possible" do not fold
// together under the paper's rules (they do under the C departures). Both are
// rare query pairs next to the plurals and tenses this exists for, and a search
// that treats them as distinct words is a recall miss, not a wrong answer.
// Revisit only with the corpus check in hand.
var step2Rules = []struct{ suffix, repl string }{
	{"ational", "ate"},
	{"tional", "tion"},
	{"enci", "ence"},
	{"anci", "ance"},
	{"izer", "ize"},
	{"abli", "able"}, // the paper's form; the C source departs with "bli"->"ble"
	{"alli", "al"},
	{"entli", "ent"},
	{"eli", "e"},
	{"ousli", "ous"},
	{"ization", "ize"},
	{"ation", "ate"},
	{"ator", "ate"},
	{"alism", "al"},
	{"iveness", "ive"},
	{"fulness", "ful"},
	{"ousness", "ous"},
	{"aliti", "al"},
	{"iviti", "ive"},
	{"biliti", "ble"},
}

// step3Rules are the step-3 suffix replacements.
var step3Rules = []struct{ suffix, repl string }{
	{"icate", "ic"},
	{"ative", ""},
	{"alize", "al"},
	{"iciti", "ic"},
	{"ical", "ic"},
	{"ful", ""},
	{"ness", ""},
}

// step4Rules are the step-4 suffixes: stripped when the remaining stem
// measures more than 1. Only the suffix matters (nothing is replaced).
var step4Rules = []string{
	"al", "ance", "ence", "er", "ic", "able", "ible", "ant", "ement", "ment",
	"ent", "ion", "ou", "ism", "ate", "iti", "ous", "ive", "ize",
}

// replaceIfMeasured applies the first matching rule whose suffix leaves a stem
// with measure > 0, which is Porter's condition for step 2 and 3. The rule
// list is scanned in order, so a longer suffix that shares a prefix with a
// shorter one must be listed first.
func replaceIfMeasured(tok string, rules []struct{ suffix, repl string }) string {
	for _, r := range rules {
		if !strings.HasSuffix(tok, r.suffix) {
			continue
		}
		if measure(tok[:len(tok)-len(r.suffix)]) > 0 {
			return tok[:len(tok)-len(r.suffix)] + r.repl
		}
		// A matched suffix whose stem does not measure up blocks the rest:
		// falling through to a shorter rule would strip a different suffix
		// than the one the token actually ends with.
		return tok
	}
	return tok
}

// stripIfMeasured removes the first matching suffix when the remaining stem
// measures more than minMeasure.
func stripIfMeasured(tok string, suffixes []string, minMeasure int) string {
	for _, suffix := range suffixes {
		if !strings.HasSuffix(tok, suffix) {
			continue
		}
		stem := tok[:len(tok)-len(suffix)]
		// "ion" only strips when the stem ends in s or t ("adoption" ->
		// "adopt", but "lion" stays).
		if suffix == "ion" && (stem == "" || !strings.ContainsRune("st", rune(stem[len(stem)-1]))) {
			return tok
		}
		if measure(stem) > minMeasure {
			return stem
		}
		return tok
	}
	return tok
}

// measure counts the VC sequences in a token: Porter's m, which is what every
// rule is gated on because it approximates "is this a plausible word stem".
//
// It counts VOWEL→CONSONANT transitions, which is what (VC)^m means: "trouble"
// (C C V V C C V) is [C](VC)[V] with m=1, not 2. Counting the other direction
// (each time a vowel STARTS) reads as equivalent and is not — it counts a
// doubled consonant run as two pairs. (Measured: that mistake took disagreement
// with the reference from 0.4% to 2.2% of a 42,603-word corpus, so it is a real
// distinction and not pedantry.)
//
// The classification goes through isConsonantAt, NOT a fixed aeiou set,
// because Porter treats 'y' as a vowel when it follows a consonant ("bygon" is
// C V C V C, m=2, which is what lets "bygone" lose its 'e').
func measure(tok string) int {
	m := 0
	for i := 1; i < len(tok); i++ {
		if !isConsonantAt(tok, i-1) && isConsonantAt(tok, i) {
			m++
		}
	}
	return m
}

// hasVowel reports whether any character is a vowel under Porter's rule
// (through isConsonantAt, so 'y' counts when it follows a consonant). Used to
// gate step 1b, where the question is "would removing the suffix leave a
// pronounceable stem".
func hasVowel(tok string) bool {
	for i := 0; i < len(tok); i++ {
		if !isConsonantAt(tok, i) {
			return true
		}
	}
	return false
}

// isVowel is Porter's vowel set. 'y' is a vowel only when it is not the first
// character, which is why the caller passes position-free single bytes here
// and endsCVC handles the y case separately.
func isVowel(c byte) bool {
	switch c {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	}
	return false
}

// endsCVC reports whether a token ends in consonant-vowel-consonant, with the
// final consonant not w, x or y — Porter's condition for restoring an e
// ("hop" -> "hope", but "row" -> "row").
func endsCVC(tok string) bool {
	if len(tok) < 3 {
		return false
	}
	last := tok[len(tok)-1]
	if last == 'w' || last == 'x' || last == 'y' {
		return false
	}
	return isConsonantAt(tok, len(tok)-3) &&
		!isConsonantAt(tok, len(tok)-2) &&
		isConsonantAt(tok, len(tok)-1)
}

// isConsonantAt reports whether the byte at i is a consonant, treating y as a
// consonant when it follows a vowel ("boy") and a vowel otherwise ("happy").
func isConsonantAt(tok string, i int) bool {
	c := tok[i]
	if c == 'y' {
		if i == 0 {
			return true
		}
		return !isConsonantAt(tok, i-1)
	}
	return !isVowel(c)
}

// stemAll folds every token in place and de-duplicates, returning a copy.
//
// De-duplication is not cosmetic here: a query of "invoice invoices" and a
// query of "invoice" must cost the same posting lookup, and the fuzzy tier
// scores per term, so a repeated term would double-count a match.
func stemAll(tokens []string) []string {
	if len(tokens) == 0 {
		return tokens
	}
	out := make([]string, 0, len(tokens))
	seen := make(map[string]struct{}, len(tokens))
	for _, t := range tokens {
		folded := Stem(t)
		if _, dup := seen[folded]; dup {
			continue
		}
		seen[folded] = struct{}{}
		out = append(out, folded)
	}
	return out
}

// isASCIIAlpha reports whether every byte is an ASCII letter.
func isASCIIAlpha(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}
