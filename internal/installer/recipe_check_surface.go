// Installer CLI: the dashboard/panel half of `recipe check`.
package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/calionauta/gogogo/internal/recipe"
)

// checkRecipeMetricsAndPanel cross-checks the declared dashboard and
// chat surfaces against the tree. It fails fast on obvious contract
// breaks and warns when the conventions that make a recipe reusable
// are missing.
func checkRecipeMetricsAndPanel(root string, receipt recipe.Receipt, report RecipeCheckReport) RecipeCheckReport {
	decl := receipt.Declaration
	seen := map[string]bool{}
	for _, m := range decl.Metrics {
		table := metricTable(m.Query)
		if table != "" && !slices.Contains(decl.Tables, table) && !recipe.IsCoreTable(table) {
			report = reportWithWarnings(report, fmt.Sprintf(
				"metric %q reads table %q, which the declaration's Tables "+
					"do not claim", m.Label, table))
			report.Suggestions = appendUnique(report.Suggestions,
				"declare owned tables in Tables (core reads stay allowed for bundled units)")
		}
		if seen[m.Label] {
			report = reportWithWarnings(report, fmt.Sprintf("metric label %q is duplicated", m.Label))
		}
		seen[m.Label] = true
	}
	if len(decl.Metrics) == 0 {
		report = reportWithWarnings(report, "no dashboard metrics declared")
		report.Suggestions = appendUnique(report.Suggestions,
			"declare Metrics with labels and COUNT queries (ownerBinds defaults to true)")
	}
	if decl.Panel == nil {
		report = reportWithWarnings(report, "no genui panel declared")
		report.Suggestions = appendUnique(report.Suggestions,
			"if the recipe has a list slice, declare PanelSpec and add genui_panel.go on comp.SlicePanel")
		return report
	}
	code := panelSourceUnder(root, receipt)
	if code == "" {
		report = reportWithErrors(report, fmt.Sprintf(
			"panel %s is declared, but no provider source ships with the recipe",
			decl.Panel.Component))
		return report
	}
	if !strings.Contains(code, fmt.Sprintf("%q", decl.Panel.Component)) &&
		!strings.Contains(code, decl.Panel.Component) {
		report = reportWithWarnings(report, fmt.Sprintf(
			"provider source does not name panel component %q", decl.Panel.Component))
	}
	for _, w := range decl.Panel.Writable {
		if !strings.Contains(code, fmt.Sprintf("%q", w)) {
			report = reportWithWarnings(report, fmt.Sprintf("provider source does not name writable prop %q", w))
		}
	}
	return report
}

var sqlFromTable = regexp.MustCompile(`(?i)\bFROM\s+([a-zA-Z_][a-zA-Z0-9_]*)`)

func metricTable(query string) string {
	m := sqlFromTable.FindStringSubmatch(query)
	if len(m) < 2 {
		return ""
	}
	return strings.Trim(m[1], "\"'`[]")
}

func panelSourceUnder(root string, receipt recipe.Receipt) string {
	paths := make([]string, 0, len(receipt.Files))
	for p := range receipt.Files {
		if strings.HasSuffix(p, ".go") && (strings.Contains(p, "genui") || strings.Contains(p, "panel")) {
			paths = append(paths, p)
		}
	}
	sortStrings(paths)
	var b strings.Builder
	for _, rel := range paths {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err == nil {
			b.WriteString(string(raw))
			b.WriteByte('\n')
		}
	}
	if receipt.Register != "" {
		b.WriteString(goSourceUnder(filepath.Join(root, filepath.FromSlash(receipt.Register))))
	}
	return b.String()
}

func sortStrings(xs []string) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}
