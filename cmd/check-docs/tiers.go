package main

// The feedback tiers are named in more than one page, and the lists drifted.
//
// Two pages describe the same ladder: `AGENTS.md` (the normative 20-30 line
// file every agent reads) and `docs/local-ci.md` (the table). They disagreed —
// the table carried six stops and AGENTS.md seven, because the operator's
// click was added to the prose and to `AGENTS.md` but never to the table.
// Nothing broke: no build reads a tier number, so only a mechanical scan
// keeps the two in step.
//
// The ladder was renumbered from a T1–T5 run plus two fractional labels to a
// flat T1–T7, because the fractions read as sub-steps of the tier before them
// when neither is one: the fast gate re-runs T1+T2 repo-wide, and the
// operator's click is not a test at all.
// This rule still only compares SETS across the two pages — it does not police
// which number means what, so a future rename does not have to touch it.

import (
	"fmt"
	"regexp"
	"sort"
)

// tierSources are the pages that name the ladder. The first is normative.
// agentsMDFile is the normative agent file, named by several rules here.
const agentsMDFile = "AGENTS.md"

var tierSources = []string{agentsMDFile, "docs/local-ci.md"}

// tierRE matches a tier label: T1 … T7. The boundary stops "T1" from matching
// inside "T10", and a fraction shape is rejected outright so a reintroduced
// fractional label fails loudly rather than passing as a stop nobody defined.
var tierRE = regexp.MustCompile(`\bT([1-9])(?:\.([1-9]))?\b`)

// fractionalTierRE matches the retired shape. A label with a fraction is a
// regression: the ladder has one integer per stop now (see the package note).
var fractionalTierRE = regexp.MustCompile(`\bT[1-9]\.[1-9]\b`)

// checkTierLists compares the tier labels each page names.
func checkTierLists(root string) []string {
	var out []string
	named := make(map[string]map[string]bool, len(tierSources))
	for _, rel := range tierSources {
		lines, err := fileLines(root, rel)
		if err != nil {
			// The page's absence is checkPaths' business, not this rule's.
			continue
		}
		set, frac := collectTiers(rel, lines)
		out = append(out, frac...)
		named[rel] = set
	}
	return append(out, compareTierSets(named)...)
}

// collectTiers reads one page: the set of tier labels it names, plus a problem
// for every fractional label, which the ladder no longer has.
func collectTiers(rel string, lines []string) (map[string]bool, []string) {
	var frac []string
	set := map[string]bool{}
	for i, line := range lines {
		if m := fractionalTierRE.FindString(line); m != "" {
			frac = append(frac, fmt.Sprintf(
				"%s:%d: fractional tier %q — the ladder is a flat T1–T7, one integer "+
					"per stop (a fraction reads as a sub-step of the tier before it, "+
					"and none of these are)",
				rel, i+1, m))
		}
		for _, m := range tierRE.FindAllStringSubmatch(line, -1) {
			label := "T" + m[1]
			if m[2] != "" {
				label += "." + m[2]
			}
			set[label] = true
		}
	}
	return set, frac
}

// compareTierSets reports a label one page names and another does not.
func compareTierSets(named map[string]map[string]bool) []string {
	if len(named) < 2 {
		return nil
	}
	var out []string
	for _, rel := range tierSources {
		other := tierSources[0]
		if rel == tierSources[0] {
			other = tierSources[1]
		}
		mine, theirs := named[rel], named[other]
		if len(mine) == 0 || len(theirs) == 0 {
			continue
		}
		var missing []string
		for label := range theirs {
			if !mine[label] {
				missing = append(missing, label)
			}
		}
		if len(missing) == 0 {
			continue
		}
		sort.Strings(missing)
		out = append(out, fmt.Sprintf(
			"%s: names %d feedback tier(s) that %s does not: %v — "+
				"the ladder is one list; add the missing stop or drop it from both",
			rel, len(missing), other, missing))
	}
	return out
}
