// Installer CLI: `recipe check`.
//
// A read-only evaluator for humans and LLMs. It reuses the existing
// drift, schema, policy, declaration, and registry checks, then adds
// source-quality signals (complexity, wiring, tests, docs) that the
// install path deliberately does not own. The report is deterministic:
// the same tree always produces the same score and suggestions.
package installer

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/calionauta/gogogo/internal/capabilities"
	"github.com/calionauta/gogogo/internal/recipe"
)

// RecipeCheckReport is the machine representation of one recipe check.
type RecipeCheckReport struct {
	ID            string                `json:"id"`
	Healthy       bool                  `json:"healthy"`
	Score         int                   `json:"score"`
	Errors        []string              `json:"errors,omitempty"`
	Warnings      []string              `json:"warnings,omitempty"`
	Suggestions   []string              `json:"suggestions,omitempty"`
	Complexity    recipeCheckComplexity `json:"complexity"`
	Declaration   recipe.Declaration    `json:"declaration"`
	ProviderFiles []string              `json:"providerFiles,omitempty"`
	Metrics       []checkMetricSummary  `json:"metrics,omitempty"`
	Panel         *recipe.PanelSpec     `json:"panel,omitempty"`
}

// checkMetricSummary is the checker-facing shape of one dashboard metric.
type checkMetricSummary struct {
	Label  string `json:"label"`
	Query  string `json:"query"`
	Signal string `json:"signal"`
	Owner  *bool  `json:"owner,omitempty"`
}

// runRecipeCheck evaluates installed recipes without mutating the tree.
func runRecipeCheck(args []string, stdout io.Writer) error {
	opt, err := parseRecipeLookupArgs("check", args)
	if err != nil {
		return err
	}
	root, aerr := filepath.Abs(opt.dir)
	if aerr != nil {
		return &ExitError{code: 1, msg: "recipe check: " + aerr.Error()}
	}
	if !looksLikeTemplate(root) {
		return &ExitError{code: 1, msg: "recipe check: " + opt.dir + " is not a gogogo checkout"}
	}
	ids, err := statusIDs("check", root, opt.id)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		fmt.Fprintln(stdout, "recipe check: no receipts in "+recipe.ReceiptDir+"/ — nothing to check")
		return nil
	}
	reports := make([]RecipeCheckReport, 0, len(ids))
	blockers := 0
	for _, id := range ids {
		report := checkOneRecipe(root, id)
		if !report.Healthy {
			blockers++
		}
		reports = append(reports, report)
	}
	if opt.format == planFormatJSON {
		if len(reports) == 1 {
			return json.NewEncoder(stdout).Encode(reports[0])
		}
		return json.NewEncoder(stdout).Encode(reports)
	}
	for _, report := range reports {
		printRecipeCheck(stdout, report)
	}
	if blockers > 0 {
		return &ExitError{code: 1, msg: fmt.Sprintf("recipe check: %d recipe(s) need attention", blockers)}
	}
	return nil
}

// checkOneRecipe is the pure evaluator. Split from runRecipeCheck so
// tests can pin the scoring rules without the CLI.
func checkOneRecipe(root, id string) RecipeCheckReport {
	report := RecipeCheckReport{ID: id}
	receipt, err := recipe.LoadReceipt(root, id)
	if err != nil {
		return reportWithErrors(report, "receipt: "+err.Error())
	}
	report.Declaration = receipt.Declaration
	report.Panel = receipt.Declaration.Panel
	for _, m := range receipt.Declaration.Metrics {
		report.Metrics = append(report.Metrics, checkMetricSummary{
			Label: m.Label, Query: m.Query, Signal: m.Signal, Owner: m.OwnerBinds,
		})
	}
	if receipt.Removed != "" {
		return reportWithErrors(report, "recipe is removed; restore or uninstall it")
	}
	drift, derr := recipe.Check(root, receipt)
	if derr != nil {
		report = reportWithErrors(report, "drift: "+derr.Error())
	} else {
		report = reportWithErrors(report, drift.Problems...)
		if len(drift.Files) == 0 && len(receipt.Files) == 0 {
			report = reportWithWarnings(report, "receipt has an empty file set")
		}
		for _, f := range drift.Files {
			if f.State == recipe.StateDeleted {
				report = reportWithWarnings(report, "missing file "+f.Path)
			}
		}
	}
	metrics := recipe.MetricProblems(receipt.Declaration)
	panelProblems := recipe.PanelProblems(receipt.Declaration)
	report.Errors = append(report.Errors, metrics...)
	report.Errors = append(report.Errors, panelProblems...)
	if len(metrics)+len(panelProblems) > 0 {
		report.Suggestions = append(report.Suggestions,
			"fix the declaration first: metrics must be COUNT queries "+
				"bound to owner (or ownerBinds:false), and a panel needs a "+
				"snake_case component, prompt, and exactly one writable prop")
	}
	report = checkRecipeRegistration(root, receipt, report)
	report = checkRecipeMetricsAndPanel(root, receipt, report)
	report, complexity := checkRecipeSource(root, receipt, report)
	report.Complexity = complexity
	if complexity.LargestFuncLines > 100 {
		report = reportWithWarnings(report, fmt.Sprintf(
			"function %s is %d lines (repo limit 100); extract helpers",
			complexity.LargestFunction, complexity.LargestFuncLines))
	}
	if complexity.LargestFileLines > 500 {
		report = reportWithWarnings(report, fmt.Sprintf(
			"file %s is %d lines (repo limit 500); split by concern", complexity.LargestFile, complexity.LargestFileLines))
	}
	if !recipeHasTests(root, receipt) {
		report = reportWithWarnings(report, "no Go test files are installed with this recipe")
		report.Suggestions = appendUnique(report.Suggestions, "add a red-proofed contract test before publishing")
	}
	if !recipeDocumented(root, receipt.ID) {
		report = reportWithWarnings(report, "recipe id is not named in docs")
		report.Suggestions = appendUnique(
			report.Suggestions,
			"mention the recipe in docs/unit-contracts.md or docs/features.md during the catalog PR")
	}
	report.Score = recipeCheckScore(report)
	report.Healthy = len(report.Errors) == 0
	return report
}

func reportWithErrors(report RecipeCheckReport, msgs ...string) RecipeCheckReport {
	report.Errors = appendUnique(report.Errors, msgs...)
	return report
}

func reportWithWarnings(report RecipeCheckReport, msgs ...string) RecipeCheckReport {
	report.Warnings = appendUnique(report.Warnings, msgs...)
	return report
}

func appendUnique(in []string, msgs ...string) []string {
	out := append([]string(nil), in...)
	for _, msg := range msgs {
		if strings.TrimSpace(msg) != "" && !slices.Contains(out, msg) {
			out = append(out, msg)
		}
	}
	return out
}

// recipeCheckScore is deterministic: every error costs 10 points
// and every warning costs 3, never below zero.
func recipeCheckScore(report RecipeCheckReport) int {
	score := 100 - 10*len(report.Errors) - 3*len(report.Warnings)
	if score < 0 {
		return 0
	}
	return score
}

func printRecipeCheck(stdout io.Writer, report RecipeCheckReport) {
	verdict := "ready"
	if !report.Healthy {
		verdict = "blocked"
	} else if len(report.Warnings) > 0 {
		verdict = "needs work"
	}
	fmt.Fprintf(stdout, "recipe %s: %s (score %d)\n", report.ID, verdict, report.Score)
	printCheckComplexity(stdout, report)
	for _, e := range report.Errors {
		fmt.Fprintf(stdout, "  error: %s\n", e)
	}
	for _, w := range report.Warnings {
		fmt.Fprintf(stdout, "  warning: %s\n", w)
	}
	for _, s := range report.Suggestions {
		fmt.Fprintf(stdout, "  next: %s\n", s)
	}
}

func printCheckComplexity(stdout io.Writer, report RecipeCheckReport) {
	if report.Complexity == (recipeCheckComplexity{}) {
		return
	}
	fmt.Fprintf(stdout, "  complexity: %d files, %d Go lines, largest %s "+
		"(%d lines), largest file %s (%d lines), %d test files, "+
		"%d branch points\n",
		report.Complexity.GoFiles, report.Complexity.GoLines,
		report.Complexity.LargestFunction, report.Complexity.LargestFuncLines,
		report.Complexity.LargestFile, report.Complexity.LargestFileLines,
		report.Complexity.GoTestFiles, report.Complexity.BranchPoints)
}

// checkRecipeRegistration verifies the self-mounting contract without importing
// user code: a Register path exists, it is shipped, and its source arms the
// generated registration through the seam.
func checkRecipeRegistration(root string, receipt recipe.Receipt, report RecipeCheckReport) RecipeCheckReport {
	id := receipt.ID
	if receipt.Register == "" {
		report = reportWithWarnings(report, "no Register package: the recipe is data-only or missing its seam entry")
		report.Suggestions = appendUnique(report.Suggestions, "add a Register package that calls reg.Add(handler.Recipe())")
		return report
	}
	dir := filepath.Join(root, filepath.FromSlash(receipt.Register))
	if !dirExists(dir) {
		report = reportWithErrors(report, fmt.Sprintf(
			"Register path %q has no directory", receipt.Register))
		report.Suggestions = appendUnique(report.Suggestions,
			"restore the Register package or clear the receipt's Register field")
		return report
	}
	code := goSourceUnder(dir)
	if code == "" {
		report = reportWithErrors(report, fmt.Sprintf("Register package %q has no Go source", receipt.Register))
		return report
	}
	if !strings.Contains(code, "reg.Add(") && !strings.Contains(code, "recipe.Provider") {
		report = reportWithWarnings(report,
			"Register source does not obviously call reg.Add "+
				"(the generated registration would mount nothing)")
	}
	if !strings.Contains(code, "Declaration()") && !strings.Contains(code, "recipe.Declaration{") {
		report = reportWithWarnings(report,
			"Register source does not obviously declare the recipe "+
				"(code and receipt can drift)")
	}
	if _, exists := capabilities.ByID()[id]; !exists {
		report = reportWithWarnings(report, "no capabilities row names this recipe")
		report.Suggestions = appendUnique(report.Suggestions,
			"add a Capability row with Register set, then regenerate units.gen.go")
	}
	return report
}

func dirExists(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

func goSourceUnder(dir string) string {
	rootFS, err := os.OpenRoot(dir)
	if err != nil {
		return ""
	}
	defer func() { _ = rootFS.Close() }()
	var b strings.Builder
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return nil
		}
		file, oerr := rootFS.Open(filepath.ToSlash(rel))
		if oerr != nil {
			return nil
		}
		defer func() { _ = file.Close() }()
		raw, rerr := io.ReadAll(file)
		if rerr == nil {
			b.WriteString(string(raw))
			b.WriteByte('\n')
		}
		return nil
	})
	return b.String()
}

func recipeHasTests(root string, receipt recipe.Receipt) bool {
	for p := range receipt.Files {
		if strings.HasSuffix(p, "_test.go") {
			return true
		}
	}
	if receipt.Register != "" {
		entries := readDirEntries(filepath.Join(root, filepath.FromSlash(receipt.Register)))
		for _, name := range entries {
			if strings.HasSuffix(name, "_test.go") {
				return true
			}
		}
	}
	return false
}

func readDirEntries(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

func recipeDocumented(root, id string) bool {
	for _, rel := range []string{"docs/unit-contracts.md", "docs/features.md", "docs/scope-taxonomy.md"} {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err == nil && strings.Contains(string(raw), id) {
			return true
		}
	}
	return false
}

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
