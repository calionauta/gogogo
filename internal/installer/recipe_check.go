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
	Outcomes      []checkOutcomeSummary `json:"outcomes,omitempty"`
	Panel         *recipe.PanelSpec     `json:"panel,omitempty"`
	// Omitted is information, not a warning: the author explained why the
	// route is not headless, and the report shows that decision.
	Omitted []recipe.OmittedRoute `json:"omitted,omitempty"`
}

// checkMetricSummary is the checker-facing shape of one dashboard metric.
type checkMetricSummary struct {
	Label  string `json:"label"`
	Query  string `json:"query"`
	Signal string `json:"signal"`
	Owner  *bool  `json:"owner,omitempty"`
}

// checkOutcomeSummary is the checker-facing shape of one declared outcome: the
// job and the two counts, so a reader can see the ratio without opening the
// receipt.
type checkOutcomeSummary struct {
	ID             string   `json:"id"`
	Job            string   `json:"job"`
	Success        string   `json:"success"`
	Total          string   `json:"total"`
	CounterSignals []string `json:"counterSignals,omitempty"`
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
	report.Omitted = append([]recipe.OmittedRoute(nil), receipt.Declaration.Omitted...)
	for _, m := range receipt.Declaration.Metrics {
		report.Metrics = append(report.Metrics, checkMetricSummary{
			Label: m.Label, Query: m.Query, Signal: m.Signal, Owner: m.OwnerBinds,
		})
	}
	for _, o := range receipt.Declaration.Outcomes {
		report.Outcomes = append(report.Outcomes, checkOutcomeSummary{
			ID: o.ID, Job: o.Job, Success: o.Success.Query, Total: o.Total.Query,
			CounterSignals: o.CounterSignals,
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
	outcomeProblems := recipe.OutcomeProblems(receipt.Declaration)
	report.Errors = append(report.Errors, metrics...)
	report.Errors = append(report.Errors, panelProblems...)
	report.Errors = append(report.Errors, outcomeProblems...)
	if len(metrics)+len(panelProblems)+len(outcomeProblems) > 0 {
		report.Suggestions = append(report.Suggestions,
			"fix the declaration first: metrics must be COUNT queries "+
				"bound to owner (or ownerBinds:false), a panel needs a "+
				"snake_case component, prompt, and exactly one writable prop, "+
				"and an outcome is a success/total pair of such counts over a declared table")
	}
	report = checkOutcomeInstrumentation(root, receipt, report)
	report = checkRecipeRegistration(root, receipt, report)
	report = checkRecipeHeadless(receipt, report)
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

// checkOutcomeInstrumentation blocks an outcome whose counter signal no write
// path records. The token is the state the ratio is limited by, so a success
// count nobody can move is a number nobody can read. Boot never applies this
// rule (an older receipt must keep running), which is why the check — the place
// the declaration is judged — is where it becomes an error.
func checkOutcomeInstrumentation(root string, receipt recipe.Receipt, report RecipeCheckReport) RecipeCheckReport {
	gaps := recipe.OutcomeInstrumentationGaps(root, receipt)
	if len(gaps) == 0 {
		return report
	}
	report = reportWithErrors(report, gaps...)
	report.Suggestions = appendUnique(report.Suggestions,
		"record each counter signal in the recipe's write path (the column the "+
			"success query filters), or drop the signal from the declaration")
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
	for _, o := range report.Omitted {
		fmt.Fprintf(stdout, "  omitted: %s %s — %s\n", o.Route.Method, o.Route.Pattern, o.Reason)
	}
	for _, o := range report.Outcomes {
		fmt.Fprintf(stdout, "  outcome: %s — %s\n", o.ID, o.Job)
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
	return report
}

// checkRecipeHeadless is the headless half of the evaluator: a mutating
// route with no action is invisible to an agent, and an action with no
// description is a name the agent cannot choose between. These are
// warnings, not errors: an existing recipe must keep evaluating while its
// author fills the surface (the catalog review is where it becomes required).
func checkRecipeHeadless(receipt recipe.Receipt, report RecipeCheckReport) RecipeCheckReport {
	decl := receipt.Declaration
	docProblems := recipe.ActionDocProblems(decl)
	for _, problem := range docProblems {
		report = reportWithWarnings(report, problem)
	}
	if len(docProblems) > 0 {
		report.Suggestions = appendUnique(report.Suggestions,
			"describe each action in one sentence and document its params (paramHelp) so an agent can choose it")
	}
	// A bundled unit already has a capability Summary; only an installed
	// recipe can fall back to its repo path in discovery.
	if _, bundled := capabilities.ByID()[receipt.ID]; !bundled && strings.TrimSpace(receipt.Summary) == "" {
		report = reportWithWarnings(report,
			"no summary: discovery shows the upstream repo instead of what the recipe does")
		report.Suggestions = appendUnique(report.Suggestions,
			"add `summary: <what the recipe does>` to .recipe.yml and reinstall")
	}
	invisible := headlessInvisibleRoutes(decl)
	if len(invisible) == 0 {
		return report
	}
	const maxInvisibleShown = 4
	shown := invisible
	if len(shown) > maxInvisibleShown {
		shown = append(slices.Clone(shown[:maxInvisibleShown]),
			fmt.Sprintf("+%d more", len(invisible)-maxInvisibleShown))
	}
	report = reportWithWarnings(report,
		"mutating routes with no headless action: "+strings.Join(shown, ", "))
	report.Suggestions = appendUnique(report.Suggestions,
		"declare an InvokeAction (verb, description, params) for each mutating route, "+
			"or omit it deliberately (auth, stream, async/demo, destructive-only)")
	return report
}

// headlessInvisibleRoutes names the mutating routes an agent cannot call.
// Reads are excluded: a page GET is not an affordance. Streaming routes and
// auth endpoints stay exempt because they cannot be invoked headless at all;
// `{id}` no longer is — it is callable now, so a `{id}` route must be either
// an action or a declared omission like any other mutation.
func headlessInvisibleRoutes(d recipe.Declaration) []string {
	acted := map[recipe.Route]bool{}
	for _, a := range d.Actions {
		acted[a.Route] = true
	}
	omitted := map[recipe.Route]bool{}
	for _, o := range d.Omitted {
		omitted[o.Route] = true
	}
	var out []string
	for _, rt := range d.Routes {
		if acted[rt] || omitted[rt] || !mutatingMethod(rt.Method) {
			continue
		}
		if strings.HasSuffix(rt.Pattern, "/stream") ||
			strings.HasSuffix(rt.Pattern, "/events") ||
			isAuthRoute(rt.Pattern) {
			continue
		}
		out = append(out, rt.Method+" "+rt.Pattern)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func mutatingMethod(method string) bool {
	switch method {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	return false
}

func isAuthRoute(pattern string) bool {
	switch pattern {
	case "/login", "/signup", "/logout":
		return true
	}
	return strings.HasPrefix(pattern, "/api/auth/")
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
