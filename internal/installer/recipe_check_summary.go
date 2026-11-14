// The machine shape of a checked recipe's declared metrics.
//
// Split from recipe_check.go so the scoring rules and the projection stay under
// the file budget, and because the projection is the half a reader consults when
// an agent asks what a recipe measures.
package installer

import "github.com/calionauta/gogogo/internal/recipe"

// summariseMetrics projects a declaration's metrics into the report's two
// shapes. Split out of checkOneRecipe for the complexity gate: the projection is
// mechanical, and mixing it with the scoring rules made one function do both.
func summariseMetrics(d recipe.Declaration) ([]checkMetricSummary, []checkOutcomeSummary) {
	activity := d.Activity()
	metrics := make([]checkMetricSummary, 0, len(activity))
	for _, m := range activity {
		metrics = append(metrics, checkMetricSummary{
			Label: m.Label, Query: m.Query, Signal: m.Signal, Owner: m.OwnerBinds,
		})
	}
	outcomes := d.Outcomes()
	summaries := make([]checkOutcomeSummary, 0, len(outcomes))
	for _, o := range outcomes {
		if o.Success == nil || o.Total == nil {
			continue
		}
		labels := make([]string, 0, len(o.Counterbalance))
		for _, c := range o.Counterbalance {
			labels = append(labels, c.Label)
		}
		summaries = append(summaries, checkOutcomeSummary{
			ID: o.ID, Job: o.Job, Success: o.Success.Query, Total: o.Total.Query,
			Counterbalance: labels,
		})
	}
	return metrics, summaries
}
