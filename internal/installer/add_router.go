// SCOPE:plugin — installer engine: add router call restoration
package installer

import (
	"path/filepath"
	"strings"
)

func addRouterCalls(t *treeFS, root string, u trimUnit, rc *AddReceipt) error {
	calls := routerCallLines(u)
	if len(calls) == 0 {
		return nil
	}
	p := filepath.Join(root, filepath.FromSlash(chiGoFile))
	raw, err := t.ReadFile(p)
	if err != nil {
		return err
	}
	lines := strings.Split(string(raw), "\n")
	anchor := -1
	for i, l := range lines {
		if l == chiMuxAnchor {
			anchor = i
			break
		}
	}
	if anchor == -1 {
		return &ExitError{code: 1, msg: "cannot locate insertion anchor in " + chiGoFile + " (not a scaffolded buildChiMux?)"}
	}
	var insert []string
	for _, c := range calls {
		dup := false
		for _, l := range lines {
			if strings.TrimSpace(l) == strings.TrimSpace(c) {
				dup = true
				break
			}
		}
		if !dup {
			insert = append(insert, "\t"+c)
		}
	}
	if len(insert) == 0 {
		return nil
	}
	lines = append(lines[:anchor], append(insert, lines[anchor:]...)...)
	rc.LinesInserted += len(insert)
	rc.touch(chiGoFile)
	return t.WriteFile(p, []byte(strings.Join(lines, "\n")), scaffoldFileMode)
}

// isRestorableRouteCall reports whether a chi.go drop line is re-created by
// addRouterCalls (a route registration at the composition root), so the add
// path does not also report it as manual wiring. Both shapes count: the
// current MountFeature/MountSubtree calls, and the mux.Handle lines a tree
// scaffolded before the chi root still carries.
func isRestorableRouteCall(sub string) bool {
	return strings.HasPrefix(sub, "mux.Handle") ||
		strings.Contains(sub, ".MountFeature(") ||
		strings.Contains(sub, ".MountSubtree(") ||
		strings.Contains(sub, ".MountPath(") ||
		strings.HasPrefix(sub, "r.NotFound(") ||
		strings.HasPrefix(sub, "r.Get(")
}

// routerCallLines are the exact Init call lines a unit owns: explicit
// routerCalls plus router.go line-drops that are calls, not imports.
// Import-lookalikes (ending in a quote) are restored by addImports instead.
func routerCallLines(u trimUnit) []string {
	var out []string
	seen := map[string]bool{}
	add := func(c string) {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			return
		}
		seen[c] = true
		out = append(out, c)
	}
	for _, ed := range u.extraDrops {
		if ed.path != chiGoFile {
			continue
		}
		for _, sub := range ed.substrs {
			if strings.HasSuffix(sub, `"`) {
				continue
			}
			add(sub)
		}
	}
	return out
}
