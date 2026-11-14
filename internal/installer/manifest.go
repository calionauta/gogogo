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

// The strip engine that applied manifests survives only as the export
// index: builtinUnit strip data is read by the port bundle (export_wiring.go)
// and is never applied to a tree anymore.
