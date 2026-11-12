package main

import "testing"

// TestLineCountMatches pins the size rule against the sentences it must NOT
// flag: a figure with no path next to it, and a figure that belongs to
// something else.
func TestLineCountMatches(t *testing.T) {
	for _, tc := range []struct {
		text string
		hit  bool
	}{
		{"`_recipes/pomodoro`, 281 lines, one table", true},
		{"`internal/auth` is roughly 300 lines", true},
		{"~300 lines in `internal/auth`", true},
		{"It saved 40 lines", true},
		{"about 15,000 lines", false},
		{"150 typed lines you own", false},
		{"`internal/auth` holds 1200 lines", true},
		{"`internal/auth` is a few hundred lines", false},
	} {
		got := lineCountRE.MatchString(tc.text)
		if got != tc.hit {
			t.Errorf("lineCountRE.MatchString(%q) = %v, want %v", tc.text, got, tc.hit)
		}
	}
}

// TestNamesAPath pins the second half of the rule: the size claim only drifts
// when it is about a real path in this repo.
func TestNamesAPath(t *testing.T) {
	for _, tc := range []struct {
		text string
		hit  bool
	}{
		{"Pomodoro (`_recipes/pomodoro`, 281 lines)", true},
		{"`internal/auth` holds 1200 lines", true},
		{"`docs/scope-taxonomy.md` says 40 lines", true},
		{"`golang.org/x/crypto` adds 40 lines", false},
		{"`internal` holds 1200 lines", false},
		{"40 lines of prose", false},
	} {
		if got := namesAPath(tc.text); got != tc.hit {
			t.Errorf("namesAPath(%q) = %v, want %v", tc.text, got, tc.hit)
		}
	}
}
