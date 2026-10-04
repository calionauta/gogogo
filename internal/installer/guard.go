// SCOPE:layer=infra,removal=plugin — installer engine: server-side safety rails (read-only mode, dir confinement)
package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Guardrails for shared/gateway deployments (Bifrost): an agent with tool
// access has the CLI's full power, so the operator — not the caller —
// sets the boundaries via environment. Unset means local-CLI behavior
// (open), so existing flows never notice these exist.
const (
	// envReadonly refuses every mutation (apply/add) when "1"/"true".
	// Read paths (advise, plan, check) keep working.
	envReadonly = "GOGOGO_READONLY"
	// envRoots confines --dir/--from to these roots
	// (os.PathListSeparator-separated absolute paths). Empty = anywhere.
	envRoots = "GOGOGO_ALLOWED_ROOTS"
)

// guardMutable refuses what the rails forbid before anything is read,
// cloned, or planned against. dirs[0] is the target, the rest are
// additional inputs (e.g. add --from).
func guardMutable(dirs ...string) error {
	if v := strings.ToLower(strings.TrimSpace(os.Getenv(envReadonly))); v == "1" || v == "true" {
		return &ExitError{code: 1, msg: "gogogo: refusing — read-only mode " +
			"(" + envReadonly + "=1). Plan, advise, and check still work."}
	}
	roots := allowedRoots()
	if len(roots) == 0 {
		return nil
	}
	for _, d := range dirs {
		abs, err := filepath.Abs(d)
		if err != nil {
			return &ExitError{code: 1, msg: "gogogo: cannot resolve " + d}
		}
		inside := false
		for _, r := range roots {
			if abs == r || strings.HasPrefix(abs, r+string(os.PathSeparator)) {
				inside = true
				break
			}
		}
		if !inside {
			return &ExitError{code: 1, msg: fmt.Sprintf(
				"gogogo: %s is outside the allowed roots (%s)",
				d, strings.Join(roots, string(os.PathListSeparator)))}
		}
	}
	return nil
}

// allowedRoots parses envRoots into cleaned absolute dirs. Unset or blank
// means open (local CLI default); malformed entries are skipped, never fatal.
func allowedRoots() []string {
	raw := os.Getenv(envRoots)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for r := range strings.SplitSeq(raw, string(os.PathListSeparator)) {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		abs, err := filepath.Abs(r)
		if err != nil {
			continue
		}
		out = append(out, abs)
	}
	return out
}
