package installer

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// addAnchorFor resolves where `gogogo add` re-inserts one rule's span: the
// rule's own pin wins, otherwise the path default. Mirrors addSpans exactly,
// so this gate cannot drift from the code it checks.
func addAnchorFor(path string, r stripRule) spanAnchor {
	if r.addBefore != "" || r.addAfter != "" {
		return spanAnchor{before: r.addBefore, after: r.addAfter}
	}
	return addSpanAnchor(path)
}

// TestEveryDeclaredAddAnchorIsLive walks the installer's real strip table —
// every unit, every rule — and asserts what `gogogo add` needs from the
// template tree:
//
//  1. the rule still matches a span in the file it names. A rule that matches
//     nothing is dead config, the orphaned-anchor class Spike 0 measured;
//  2. an anchor a rule DECLARES is a whole line that exists today, because
//     findAnchor compares exact line equality — a comment prefix silently
//     never matches;
//  3. the declared anchor is not inside the span that rule deletes, or the
//     trim removes its own re-insertion site;
//  4. the declared anchor survives every other strip of the same file, which
//     is the state `add` runs against after sibling units were trimmed.
//
// A rule with no anchor at all is not a failure: `add` refuses such a unit up
// front (refuseIrreversible) with the exact lines to restore by hand, and a
// test that demanded an anchor would be demanding the unsupported.
func TestEveryDeclaredAddAnchorIsLive(t *testing.T) {
	type ruleRef struct {
		unit   string
		path   string
		index  int
		rule   stripRule
		source []string
	}
	var all []ruleRef
	byPath := map[string][]stripRule{}

	for _, u := range manifestUnits {
		for _, sf := range stripFilesOf(u) {
			raw, ok := readRepoFile(t, sf.path)
			if !ok {
				t.Errorf("%s: strip path %s does not exist in the template tree", u.id, sf.path)
				continue
			}
			src := strings.Split(raw, "\n")
			for i, r := range sf.rules {
				all = append(all, ruleRef{unit: u.id, path: sf.path, index: i, rule: r, source: src})
				byPath[sf.path] = append(byPath[sf.path], r)
			}
		}
	}

	for _, ref := range all {
		checkDeclaredAnchor(t, ref.unit, ref.path, ref.index, ref.rule, ref.source, byPath[ref.path])
	}
}

func checkDeclaredAnchor(t *testing.T, unit, path string, index int, r stripRule, src []string, siblings []stripRule) {
	t.Helper()
	where := fmt.Sprintf("%s/%s#%d", unit, filepath.Base(path), index)

	start := -1
	for i, l := range src {
		if strings.Contains(l, r.startMarker) {
			start = i
			break
		}
	}
	if start == -1 {
		t.Errorf("%s: start marker %q matches nothing — dead strip rule", where, r.startMarker)
		return
	}
	end := stripEnd(src, start, r)
	if end == -1 {
		t.Errorf("%s: end marker %q never closes the span — dead strip rule", where, r.endMarker)
		return
	}

	anchor := addAnchorFor(path, r)
	if anchor.before == "" && anchor.after == "" {
		// Manual by design: addManualWiring reports these lines and
		// refuseIrreversible refuses the unit before touching the tree.
		t.Logf("%s: no anchor — `add` refuses this unit as manual (by design)", where)
		return
	}
	anchorLine := anchor.before
	if anchorLine == "" {
		anchorLine = anchor.after
	}

	// findAnchor matches whole lines, so a prefix is not an anchor.
	if !slices.Contains(src, anchorLine) {
		t.Errorf("%s: anchor %q is not a whole line in %s — `add` cannot place the span", where, anchorLine, path)
		return
	}

	for i := start; i <= end; i++ {
		if src[i] == anchorLine {
			t.Errorf("%s: anchor %q sits inside the span it deletes — trim removes the re-insertion site", where, anchorLine)
			return
		}
	}

	kill := make([]bool, len(src))
	for i, other := range siblings {
		if i == index {
			continue
		}
		applyStripRule(src, kill, other)
	}
	kept := make([]string, 0, len(src))
	for i, l := range src {
		if !kill[i] {
			kept = append(kept, l)
		}
	}
	if slices.Contains(collapseBlankRuns(kept), anchorLine) {
		return
	}
	t.Errorf("%s: anchor %q is removed by another strip of %s — `gogogo add %s` cannot place the span",
		where, anchorLine, path, unit)
}

// stripFilesOf flattens a unit's strip rules into path→rules, the shape the
// add path walks.
func stripFilesOf(u trimUnit) []fileStrip {
	out := make([]fileStrip, 0, 2+len(u.extraStrips))
	out = append(out,
		fileStrip{path: filepath.Join("cmd", "web", "main.go"), rules: u.mainStrips},
		fileStrip{path: filepath.Join("cmd", "desktop", "main.go"), rules: u.desktopStrips},
	)
	out = append(out, u.extraStrips...)
	kept := out[:0]
	for _, sf := range out {
		if len(sf.rules) == 0 {
			continue
		}
		kept = append(kept, sf)
	}
	return kept
}
