// SCOPE:plugin — installer engine: read-only drift gate over manifest markers
package installer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// unitCheck is the read-only verdict for one installer unit: every owned
// path exists and every marker the trim would act on is present.
type unitCheck struct {
	ID       string
	Problems []string
}

// checkTree verifies all manifest units against a checkout without changing
// anything. It answers "would applyTrim succeed fully here?" — run it
// against a pristine template checkout (CI drift gate) or to confirm an
// already-trimmed tree (expect every marker reported missing).
func checkTree(root string) []unitCheck {
	cache := map[string]string{}
	read := func(path string) (string, bool) {
		if raw, ok := cache[path]; ok {
			return raw, true
		}
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return "", false
		}
		cache[path] = string(raw)
		return cache[path], true
	}
	out := make([]unitCheck, 0, len(manifestUnits)+1)
	// Anchors first: a dead insertion anchor breaks `add` for every unit that
	// shares it, so it is reported as its own pseudo-unit rather than buried
	// inside whichever unit happens to be checked first.
	out = append(out, unitCheck{ID: "anchors", Problems: checkAnchors(read)})
	for _, u := range manifestUnits {
		uc := unitCheck{ID: u.id}
		checkOwnedPaths(root, u, &uc)
		checkRuleSets(u, &uc, read)
		checkSubstrSets(u, &uc, read)
		checkGoModDrops(read, u, &uc)
		out = append(out, uc)
	}
	return out
}

// checkOwnedPaths verifies the registry-owned dirs and files exist.
func checkOwnedPaths(root string, u trimUnit, uc *unitCheck) {
	m := u.meta()
	for _, d := range m.dirs {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(d))); err != nil {
			uc.Problems = append(uc.Problems, "missing dir "+d)
		}
	}
	for _, f := range m.files {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(f))); err != nil {
			uc.Problems = append(uc.Problems, "missing file "+f)
		}
	}
}

// checkRuleSets verifies every strip rule's start marker is present, and that
// the rule's own addBefore/addAfter anchor still exists in the file `add`
// re-inserts into.
//
// The anchor half is not cosmetic. A trim works without it, so a dead anchor is
// invisible until someone runs `gogogo add`: `insertSpan` finds no matching
// line and fails with "cannot locate insertion anchor (evolved file?)", leaving
// the unit un-restorable. Both anchors used to point at `defer shutdown()` and
// a `// Phase 2: CRDTStore` comment that died with the framework, and every gate
// stayed green because nothing checked them. An anchor is checked against the
// main.go it will actually be inserted into (non-empty rules only; empty means
// the path default in addSpanAnchor applies, which checkAnchors covers).
func checkRuleSets(u trimUnit, uc *unitCheck, read func(string) (string, bool)) {
	check := func(path string, rules []stripRule) {
		raw, ok := read(path)
		if !ok {
			uc.Problems = append(uc.Problems, "missing file "+path)
			return
		}
		for _, r := range rules {
			if !strings.Contains(raw, r.startMarker) {
				uc.Problems = append(uc.Problems, "missing marker in "+path+": "+r.startMarker)
			}
			checkAnchor(uc, path, raw, r.addBefore)
			checkAnchor(uc, path, raw, r.addAfter)
		}
	}
	check(filepath.Join("cmd", "web", "main.go"), u.mainStrips)
	check(filepath.Join("cmd", "desktop", "main.go"), u.desktopStrips)
	for _, es := range u.extraStrips {
		check(es.path, es.rules)
	}
}

// checkAnchor reports an addBefore/addAfter anchor that is absent from the file
// it must be inserted into. Empty is skipped: that means "use the path
// default", validated once by checkAnchors rather than per rule.
func checkAnchor(uc *unitCheck, path, raw, anchor string) {
	if anchor == "" {
		return
	}
	if !strings.Contains(raw, anchor) {
		uc.Problems = append(uc.Problems, "dead add anchor in "+path+": "+anchor)
	}
}

// checkAnchors verifies the shared insertion anchors (addSpanAnchor's defaults)
// against the files they target, so a rename in cmd/web/main.go fails `--check`
// instead of silently breaking every unit that relies on the default.
func checkAnchors(read func(string) (string, bool)) []string {
	var problems []string
	for _, path := range []string{
		filepath.Join("cmd", "web", "main.go"),
		filepath.Join("cmd", "desktop", "main.go"),
	} {
		raw, ok := read(path)
		if !ok {
			problems = append(problems, "missing file "+path)
			continue
		}
		// addSpanAnchor returns either a before- or an after-anchor depending on
		// the path (cmd/web/main.go is anchorAfter(mainCallsAnchor), cmd/desktop
		// is anchorBefore). Check whichever one it set, not just .before.
		anchor := addSpanAnchor(path)
		for _, want := range []string{anchor.before, anchor.after} {
			if want != "" && !strings.Contains(raw, want) {
				problems = append(problems, "dead anchor in "+path+": "+want)
			}
		}
	}
	return problems
}

// checkSubstrSets verifies every line-drop/replace marker is present.
func checkSubstrSets(u trimUnit, uc *unitCheck, read func(string) (string, bool)) {
	check := func(path string, substrs []string) {
		raw, ok := read(path)
		if !ok {
			uc.Problems = append(uc.Problems, "missing file "+path)
			return
		}
		for _, sub := range substrs {
			if !strings.Contains(raw, sub) {
				uc.Problems = append(uc.Problems, "missing marker in "+path+": "+sub)
			}
		}
	}
	check(filepath.Join("cmd", "desktop", "main.go"), u.desktopDropLines)
	for _, ed := range u.extraDrops {
		check(ed.path, ed.substrs)
	}
	for _, rp := range u.replaces {
		check(rp.path, []string{rp.matchSubstr})
	}
}

// checkGoModDrops verifies the go.mod requires the trim would remove.
func checkGoModDrops(read func(string) (string, bool), u trimUnit, uc *unitCheck) {
	raw, ok := read("go.mod")
	if !ok {
		uc.Problems = append(uc.Problems, "missing file go.mod")
		return
	}
	for _, mod := range u.goModDrops {
		if !strings.Contains(raw, mod) {
			uc.Problems = append(uc.Problems, "missing go.mod require "+mod)
		}
	}
}

// looksLikeTemplate reports whether root has enough of the template's core
// layout to be a gogogo checkout at all.
//
// checkTree's per-unit results are meaningful against a template tree (the CI
// drift gate) or an already-trimmed one (every marker legitimately missing).
// Against an unrelated Go project they are not just useless, they are
// misleading: 68 CHECK-FAIL lines read as "this project is broken" when the
// truth is "this is not a gogogo checkout". Two cheap probes tell them apart.
func looksLikeTemplate(root string) bool {
	for _, probe := range []string{
		filepath.Join("cmd", "web", "main.go"),
		filepath.Join("internal", "installer", "run.go"),
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(probe))); err == nil {
			return true
		}
	}
	return false
}

// printCheck renders unitCheck results in grep-able lines:
// "CHECK-OK <id>", "CHECK-TRIMMED <id>" or "CHECK-FAIL <id> <problem>".
//
// Returns the count of units that are GENUINELY broken so the caller can decide
// the exit code. A non-template tree is reported as ONE actionable line rather
// than per-unit noise (see looksLikeTemplate).
//
// A deliberately trimmed tree is the third case, and it is not a failure:
// against a scaffolded project every marker the trim acted on is legitimately
// absent, so the strict verdict reported all of them as CHECK-FAIL with exit 1.
// A human knows to ignore that; an LLM agent reads exit 1 + FAIL and concludes
// the scaffold is broken. When the tree records its own trim (the removed: line
// in AGENTS.md), that is used to separate "you asked for this" from real drift.
func printCheck(w io.Writer, results []unitCheck, root string) int {
	if !looksLikeTemplate(root) {
		fmt.Fprintf(w, "CHECK-FAIL (tree) not a gogogo checkout: %s has no "+
			"cmd/web/main.go or internal/installer/run.go.\n"+
			"  Nothing to verify — run this against a gogogo template or a "+
			"project scaffolded from one.\n", root)
		return 1
	}
	// A recorded trim reclassifies the units it removed; a negative return
	// means there is no usable provenance, so fall through to strict below.
	if tp := readTrimProvenance(root); tp.found {
		if failed := explainTrim(w, results, tp); failed >= 0 {
			return failed
		}
	}
	failed := 0
	for _, uc := range results {
		if len(uc.Problems) == 0 {
			fmt.Fprintf(w, "CHECK-OK %s\n", uc.ID)
			continue
		}
		failed++
		for _, p := range uc.Problems {
			fmt.Fprintf(w, "CHECK-FAIL %s %s\n", uc.ID, p)
		}
	}
	return failed
}
