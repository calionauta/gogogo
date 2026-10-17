// SCOPE:plugin — the reference-corpus parity check for the stemmer.
//
// This is the test that makes the stemmer's correctness claim falsifiable
// instead of asserted. The corpus is the canonical Porter reference set (the
// same voc.txt/output.txt pair shipped with the snowball-data project, generated
// from the published 1980 algorithm): 42,603 English words and their expected
// stems. If our Stem disagrees with it anywhere except the documented short-token
// cutoff, this fails with the exact word.
//
// WHY IT LIVES HERE AND SKIPS BY DEFAULT. The corpus is ~700 KB of test data
// that nothing else in the repo needs, so committing it would be 700 KB of
// vendored data for one unit test. Instead the test reads two paths from the
// environment and SKIPS — loudly, naming the command — when they are absent. The
// skip is not a silent gap: the invariant it guards (parity with the reference)
// is also covered by the explicit word pairs in stem_test.go, which run always.
// This test is the arbiter for CHANGING a rule.
//
// To run it:
//
//	curl -sL https://raw.githubusercontent.com/snowballstem/snowball-data/master/porter/voc.txt -o /tmp/porter-voc.txt
//	curl -sL https://raw.githubusercontent.com/snowballstem/snowball-data/master/porter/output.txt -o /tmp/porter-out.txt
//	PORTER_VOC=/tmp/porter-voc.txt PORTER_OUT=/tmp/porter-out.txt \
//	  go test ./internal/search/ -run TestStemMatchesReferenceCorpus -v
package search

import (
	"bufio"
	"os"
	"testing"
)

const (
	envPorterVoc = "PORTER_VOC"
	envPorterOut = "PORTER_OUT"
)

// TestStemMatchesReferenceCorpus replays the canonical corpus and requires
// exact parity, except that a token shorter than stemMinLen is expected to be
// returned unchanged (the deliberate cutoff, which is a documented deviation
// from the reference for very short words).
func TestStemMatchesReferenceCorpus(t *testing.T) {
	vocPath, outPath := os.Getenv(envPorterVoc), os.Getenv(envPorterOut)
	if vocPath == "" || outPath == "" {
		t.Skipf("set %s and %s to the canonical Porter corpus to run this "+
			"(see the file header for the two curl commands); the word-pair "+
			"contract in stem_test.go runs without it", envPorterVoc, envPorterOut)
	}
	//nolint:gosec // the path is an operator-supplied test input read directly
	// (no user data reaches it): this test only runs when someone exports the
	// two variables, and reading an arbitrary local file is its purpose.
	voc, err := os.Open(vocPath)
	if err != nil {
		t.Fatalf("open %s: %v", vocPath, err)
	}
	defer func() { _ = voc.Close() }()
	//nolint:gosec // same as above: the second corpus path, same operator input.
	want, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("open %s: %v", outPath, err)
	}
	defer func() { _ = want.Close() }()

	vocScan, wantScan := bufio.NewScanner(voc), bufio.NewScanner(want)
	vocScan.Buffer(make([]byte, 0, 64*1024), 1<<20)
	checked, cutoffs, bad := 0, 0, 0
	for vocScan.Scan() && wantScan.Scan() {
		word, expected := vocScan.Text(), wantScan.Text()
		checked++
		got := Stem(word)
		if got == expected {
			continue
		}
		// The one allowed divergence: below the cutoff we leave the token
		// alone, and the reference stems it anyway.
		if len(word) < stemMinLen && got == word {
			cutoffs++
			continue
		}
		if bad < 15 {
			t.Errorf("Stem(%q) = %q, reference says %q", word, got, expected)
		}
		bad++
	}
	if err := vocScan.Err(); err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	if checked < 40_000 {
		t.Fatalf("the corpus looks truncated: only %d words read", checked)
	}
	t.Logf("parity: %d words checked, %d below the %d-char cutoff left alone, "+
		"%d unexplained divergences", checked, cutoffs, stemMinLen, bad)
	if bad > 0 {
		t.Errorf("%d unexplained divergences from the reference corpus", bad)
	}
}
