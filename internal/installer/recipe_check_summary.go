// The machine shape of a checked recipe's declared metrics.
//
// Split from recipe_check.go so the scoring rules and the projection stay under
// the file budget, and because the projection is the half a reader consults when
// an agent asks what a recipe measures.
package installer

import "github.com/calionauta/gogogo/internal/recipe"

// summariseMetrics projects a declaration's single metrics list into the
// report's one list. An activity metric carries its label/query; an outcome
// metric additionally carries the job, its two halves and the counterMetrics
// labels. There is no second list to merge.
func summariseMetrics(d recipe.Declaration) []checkMetricSummary {
	out := make([]checkMetricSummary, 0, len(d.Metrics))
	for _, m := range d.Metrics {
		item := checkMetricSummary{
			Kind:   string(m.ResolvedKind()),
			Label:  m.Label,
			Query:  m.Query,
			Signal: m.Signal,
			Owner:  m.OwnerBinds,
		}
		if m.ResolvedKind() == recipe.MetricOutcome {
			if m.Success == nil || m.Total == nil {
				continue
			}
			item.ID = m.ID
			item.Job = m.Job
			item.Beneficiary = m.Beneficiary
			item.Window = m.Window
			item.Success = m.Success.Query
			item.Total = m.Total.Query
			for _, c := range m.CounterMetrics {
				item.CounterMetrics = append(item.CounterMetrics, c.Label)
			}
		}
		out = append(out, item)
	}
	return out
}
