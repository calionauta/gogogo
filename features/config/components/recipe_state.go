// Runtime recipe state on the config page: the switch predicate the template
// renders and the correction that separates "disabled" from "removed".
//
// Split from safe_view.go for the file-size gate; the data still lives on
// PageData, so the template reads one source.
package components

import "github.com/calionauta/gogogo/internal/capabilities"

// RecipeEnabled reports the runtime switch for one recipe. A missing row is
// enabled: installing a recipe must not require a second write.
func (d PageData) RecipeEnabled(id string) bool {
	enabled, ok := d.RecipeStates[id]
	return !ok || enabled
}

// MarkRemovedUnits corrects the registry's default for what the binary
// actually mounted: a recipe with no provider in this build is not "in" —
// it was uninstalled (its registry row stays as removal-class data) or
// switched off at boot. Switch-gated rows keep their opt-out text: for
// them, unmounted is the documented off state, not a removal. Features only —
// Core/Servants are always wired when the row exists.
func MarkRemovedUnits(data *PageData, mounted map[string]bool) {
	for i := range data.Units {
		for j := range data.Units[i].Rows {
			row := &data.Units[i].Rows[j]
			if row.Kind == capabilities.KindFeature && !mounted[row.ID] && row.Status == UnitStatusIn {
				row.Status = UnitStatusRemoved
			}
		}
	}
}
