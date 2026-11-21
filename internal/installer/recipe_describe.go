// The describe printers: the human-readable half of `recipe describe`.
//
// Split from recipe_discovery.go for the file-size gate. Same descriptor, same
// caller — this is where the text is formatted, not where the data is read.
package installer

import (
	"fmt"
	"io"
	"strings"

	"github.com/calionauta/gogogo/internal/recipe"
)

// printInstrumentation renders the declared readings: what the recipe already
// measures, which an agent must read before proposing a second metric.
//
// An outcome metric prints as "success label / total label — job (beneficiary,
// window; counterMetrics: …)" so the pair is legible without a table.
func printInstrumentation(stdout io.Writer, e recipeDescriptor) {
	for _, m := range e.Metrics {
		if m.Kind != string(recipe.MetricOutcome) {
			fmt.Fprintf(stdout, "  metric: %s — %s\n", m.Label, m.Query)
			continue
		}
		line := fmt.Sprintf("  metric: %s — %s / %s — %s (%s",
			m.ID, m.SuccessLabel, m.TotalLabel, m.Job, m.Beneficiary)
		if m.Window != "" {
			line += ", " + m.Window
		}
		if len(m.CounterMetrics) > 0 {
			line += "; counterMetrics: " + strings.Join(m.CounterMetrics, ", ")
		}
		fmt.Fprintf(stdout, "%s)\n", line)
	}
}

// printDescribe renders one descriptor as text. Extracted so
// runRecipeDescribe stays under the complexity gate and the nestif gate.
// printCredit is the provenance half of describe: who wrote the slice and when
// the version shipped. Split out because describe is already at the complexity
// gate, and because discovery's other forms may want the same two lines.
func printCredit(stdout io.Writer, e recipeDescriptor) {
	if e.Author.Name != "" {
		credit := e.Author.Name
		if e.Author.Email != "" {
			credit += " <" + e.Author.Email + ">"
		}
		fmt.Fprintf(stdout, "  author: %s\n", credit)
	}
	if e.Released != "" {
		fmt.Fprintf(stdout, "  released: %s\n", e.Released)
	}
	if len(e.BasedOn) > 0 {
		fmt.Fprintf(stdout, "  based on: %v\n", e.BasedOn)
	}
	if e.Enabled != nil {
		state := recipeEnabledWord
		if !*e.Enabled {
			state = recipeDisabledWord
		}
		fmt.Fprintf(stdout, "  runtime: %s (takes effect on the next boot)\n", state)
	}
	if len(e.Components) > 0 {
		fmt.Fprintf(stdout, "  components: %v\n", e.Components)
	}
}

func printDescribe(stdout io.Writer, e recipeDescriptor) {
	fmt.Fprintf(stdout, "%s\n  kind: %s\n  origin: %s\n  state: %s\n",
		e.ID, e.Kind, e.Origin, e.State)
	fmt.Fprintf(stdout, "  summary: %s\n", e.Summary)
	if e.Version != "" {
		fmt.Fprintf(stdout, "  version: %s\n", e.Version)
	}
	printCredit(stdout, e)
	if e.Register != "" {
		fmt.Fprintf(stdout, "  register: %s\n", e.Register)
	}
	if len(e.DependsOn) > 0 {
		fmt.Fprintf(stdout, "  depends on: %v\n", e.DependsOn)
	}
	if len(e.Dependents) > 0 {
		fmt.Fprintf(stdout, "  dependents: %v\n", e.Dependents)
	}
	if len(e.Routes) > 0 {
		fmt.Fprintf(stdout, "  routes: %v\n", e.Routes)
	}
	if len(e.Tables) > 0 {
		fmt.Fprintf(stdout, "  tables: %v\n", e.Tables)
	}
	if len(e.Jobs) > 0 {
		fmt.Fprintf(stdout, "  jobs: %v\n", e.Jobs)
	}
	if len(e.Slots) > 0 {
		fmt.Fprintf(stdout, "  slots: %v\n", e.Slots)
	}
	if len(e.Invoke) > 0 {
		fmt.Fprintf(stdout, "  invoke: %v\n", e.Invoke)
	}
	if len(e.InvokeParams) > 0 {
		fmt.Fprintf(stdout, "  invokeParams: %v\n", e.InvokeParams)
	}
	printInstrumentation(stdout, e)
	if len(e.InvokePathParams) > 0 {
		fmt.Fprintf(stdout, "  invokePathParams: %v\n", e.InvokePathParams)
	}
	for _, omitted := range e.Omitted {
		fmt.Fprintf(stdout, "  omitted: %s\n", omitted)
	}
	printInvokeHelp(stdout, e)
	fmt.Fprintf(stdout, "  lifecycle: %v\n", e.Actions)
}

// printInvokeHelp renders the agent-facing half of discovery: what each
// declared verb does and what its params mean. A bare verb name forces the
// caller to read the handler, which is exactly what a headless agent cannot do.
func printInvokeHelp(stdout io.Writer, e recipeDescriptor) {
	if len(e.InvokeDescriptions) == 0 && len(e.InvokeParamHelp) == 0 {
		return
	}
	fmt.Fprintln(stdout, "  invoke help:")
	for _, verb := range e.Invoke {
		if desc := e.InvokeDescriptions[verb]; desc != "" {
			fmt.Fprintf(stdout, "    %s — %s\n", verb, desc)
		}
		pathParams := e.InvokePathParams[verb]
		if len(pathParams) > 0 {
			fmt.Fprintf(stdout, "      path: %s (uuid)\n", strings.Join(pathParams, ", "))
		}
		params := e.InvokeParams[verb]
		if len(params) > 0 {
			fmt.Fprintf(stdout, "      params: %s\n", strings.Join(params, ", "))
		}
		if query := e.InvokeQuery[verb]; len(query) > 0 {
			fmt.Fprintf(stdout, "      query: %s\n", strings.Join(query, ", "))
		}
		if raw := e.InvokeRawBody[verb]; raw != "" {
			fmt.Fprintf(stdout, "      body: %s (sent verbatim as application/json)\n", raw)
		}
		if page := e.InvokeBrowserPath[verb]; page != "" {
			fmt.Fprintf(stdout, "      page: %s (the same capability for a human)\n", page)
		}
		for _, p := range append(append(append([]string(nil), pathParams...), params...), e.InvokeQuery[verb]...) {
			if help := e.InvokeParamHelp[verb][p]; help != "" {
				fmt.Fprintf(stdout, "      %s: %s\n", p, help)
			}
		}
	}
}
