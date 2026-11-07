// Installer engine: unit manifest mechanics (strips, drops, anchors).
//
// The strip engine below still serves the port tool only: it lists the
// wiring lines a bundle carries. Scaffold no longer applies it.
package installer

func dropIDs(units []builtinUnit) []string {
	ids := make([]string, 0, len(units))
	for _, u := range units {
		ids = append(ids, u.id)
	}
	return ids
}

// UnitReceipt records what one dropped unit actually changed. Agents use
// it to verify the trim (missed markers = manifest drift) and to resume
// idempotently: re-running on a trimmed tree misses every marker and
// changes nothing.
type UnitReceipt struct {
	ID            string   `json:"id"`
	DirsRemoved   int      `json:"dirsRemoved"`
	FilesRemoved  int      `json:"filesRemoved"`
	StripsApplied int      `json:"stripsApplied"`
	StripsMissed  []string `json:"stripsMissed"`
}

// Receipt aggregates UnitReceipts for one apply run.
type Receipt struct {
	Units []UnitReceipt `json:"units"`
}

// The strip engine that applied manifests survives only as the export
// index: builtinUnit strip data is read by the port bundle (export_wiring.go)
// and is never applied to a tree anymore.
