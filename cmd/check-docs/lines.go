package main

// A size claim attached to a concrete path drifts with every commit.
//
// The repo's rule is that counts are GENERATED (docs/unit-contracts.md is
// the source), and the hand-written-count gate covers the nouns that prove
// themselves: capabilities, slices, units, linters. It did not cover a
// number of LINES — and that is the number that moves on every commit.
//
// The claim this catches is real: docs/scope-taxonomy.md said the pomodoro
// recipe is "281 lines" while the tree held 453, and #404 added 210 more.
// A reader who trusts the number is wrong by 2x and has no way to tell.

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// lineCountRE matches a hand-written size claim: a bare number followed by
// "line(s)". The leading guard keeps a figure like "15,000" out — only the
// last group of digits would match otherwise, which is not a claim anyone
// wrote.
var lineCountRE = regexp.MustCompile(`(?i)(?:^|[^0-9,.])\b([1-9][0-9]{0,3})\s+lines?\b`)

// checkLineCounts rejects a size claim that sits next to a concrete path.
func checkLineCounts(root string, files []string) []string {
	var out []string
	for _, rel := range files {
		if slices.Contains(generatedDocs, rel) {
			continue
		}
		lines, err := fileLines(root, rel)
		if err != nil {
			continue
		}
		for i, line := range lines {
			m := lineCountRE.FindStringSubmatch(line)
			if m == nil || !namesAPath(line) {
				continue
			}
			out = append(out, fmt.Sprintf(
				"%s:%d: hand-written size %q next to a path — drop the number (a size drifts with every commit)",
				rel, i+1, m[1]+" lines"))
		}
	}
	return out
}

// namesAPath reports whether the line names a concrete repo path in code
// ticks. The size claim only drifts when it is about something real: an
// approximate figure in ordinary prose is not a claim about this tree.
func namesAPath(line string) bool {
	for _, tick := range tickRE.FindAllStringSubmatch(line, -1) {
		token := tick[1]
		if !strings.Contains(token, "/") {
			continue
		}
		segments := strings.Split(token, "/")
		if slices.Contains(repoRoots, segments[0]) || segments[0] == "_recipes" {
			return true
		}
	}
	return false
}
