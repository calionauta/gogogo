// SCOPE:layer=infra,removal=plugin — installer engine: read-only drift gate over manifest markers
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
	var out []unitCheck
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

// checkRuleSets verifies every strip rule's start marker is present.
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
		}
	}
	check(filepath.Join("cmd", "web", "main.go"), u.mainStrips)
	check(filepath.Join("cmd", "desktop", "main.go"), u.desktopStrips)
	for _, es := range u.extraStrips {
		check(es.path, es.rules)
	}
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

// printCheck renders unitCheck results in grep-able lines:
// "CHECK-OK <id>" or "CHECK-FAIL <id> <problem>".
func printCheck(w io.Writer, results []unitCheck) int {
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
