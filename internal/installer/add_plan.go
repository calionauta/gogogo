// SCOPE:plugin — installer engine: the `add` preview (dry-run plan)
package installer

import (
	"encoding/json"
	"fmt"
	"io"
)

// printAddPlan previews an add: unit metadata plus what would change.
func printAddPlan(w io.Writer, format string, u trimUnit, from, dir string) {
	if format == planFormatJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		manual := addManualWiring(u)
		if unitPathsPresent(dir, u) {
			manual = nil
		}
		_ = enc.Encode(map[string]any{
			"unit": u.id, "from": from, "dir": dir,
			"summary": unitOneLiner(u), "dryRun": true,
			"manual": manual,
		})
		return
	}
	m := u.meta()
	fmt.Fprintf(w, "gogogo: add %s (%s) from %s into %s\n", u.id, m.kind, from, dir)
	fmt.Fprintf(w, "  copies %d path(s), %d wiring strip(s) to restore\n",
		len(m.dirs)+len(m.files),
		len(u.mainStrips)+len(u.desktopStrips)+len(u.extraStrips))
	// Present units restore nothing (idempotent re-add): listing their
	// manual lines here would read as work left to do.
	if !unitPathsPresent(dir, u) {
		for _, line := range addManualWiring(u) {
			fmt.Fprintf(w, "  manual: %s\n", line)
		}
	}
	for _, warn := range addFollowUps(u) {
		fmt.Fprintf(w, "  manual follow-up: %s\n", warn)
	}
}
