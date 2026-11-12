// Declared outcomes: the job a recipe claims to do, as two counts.
//
// A metric answers "how much happened". An outcome answers "did the job get
// done, for whom, and how often did it go wrong" — which is a ratio, not a
// score. The pair is deliberately made of two Metrics instead of a second query
// language: the same SELECT COUNT(*) rule that guards a dashboard count guards
// both halves, so there is one definition of a valid query and the engine's only
// new arithmetic is one division with a zero check.
//
// Why not an event stream: see docs/research/recipe-outcomes-2026-11.md. A
// denominator that the feature already stores does not need a collection
// pipeline, a retention policy, or an opt-in toggle.
package recipe

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Outcome is one job a recipe claims to perform, declared as the counts that
// answer it: how many cases reached the desired result, and how many were
// eligible at all.
//
// The ratio is what a reader interprets. Nothing here is rolled into a single
// quality number: the counter signals sit BESIDE the success count so a rise in
// corrections cannot be averaged away by a rise in volume.
type Outcome struct {
	ID string `yaml:"id"`
	// Job is the outcome in the user's own words. It is required because a
	// ratio without a sentence is a number nobody can disagree with.
	Job string `yaml:"job"`
	// Beneficiary says whose job this is. A closed set: the operator's number
	// and the user's number must not be the same declaration, or optimization
	// against the person becomes indistinguishable from serving them.
	Beneficiary string `yaml:"beneficiary"`
	// Success counts the cases that reached the result.
	Success Metric `yaml:"success"`
	// Total counts the eligible cases (the denominator).
	Total Metric `yaml:"total"`
	// CounterSignals names the states that limit how the success count may be
	// read: a correction, an abandonment, a retry. Tokens only — they are a
	// note to the author and to `recipe check`, never a query of their own.
	CounterSignals []string `yaml:"counterSignals,omitempty"`
	// Window is the period the pair is read over. Empty means the pair is
	// cumulative (the honest default for a dashboard count).
	Window string `yaml:"window,omitempty"`
	// Target is the operator's own goal in (0,1]. nil means nobody agreed on
	// one, which is the honest default: an invented target is a decision the
	// agent made on the operator's behalf.
	Target *float64 `yaml:"target,omitempty"`
}

// outcomeBeneficiaries and outcomeWindows are the closed sets. Membership is
// checked, not the spelling: a synonym is a typo with good intentions.
var (
	outcomeBeneficiaries = []string{"user", "operator"}
	outcomeWindows       = []string{"session", "day", "none"}
)

// queryFromTableRE extracts the single table a COUNT query reads. It is
// deliberately not a SQL parser: the contract already limits metrics to
// `SELECT COUNT(*) ... WHERE ...`, so `FROM <ident>` is the whole surface the
// ownership check has to read.
var queryFromTableRE = regexp.MustCompile(`(?i)\bfrom\s+([a-z_][a-z0-9_]*)`)

// OutcomeProblems applies the outcome rules. Pure data in, strings out — the
// same shape as MetricProblems, so boot, `recipe check` and the catalog review
// can run it without a filesystem or a handler.
func OutcomeProblems(d Declaration) []string {
	if len(d.Outcomes) == 0 {
		return nil
	}
	declaredTables := make(map[string]bool, len(d.Tables))
	for _, t := range d.Tables {
		declaredTables[strings.ToLower(t)] = true
	}
	var out []string
	seen := make(map[string]bool, len(d.Outcomes))
	for _, o := range d.Outcomes {
		out = append(out, outcomeIDProblems(o, seen)...)
		out = append(out, outcomeClaimProblems(o)...)
		out = append(out, outcomeQueryProblems(o, declaredTables)...)
		out = append(out, outcomeSignalProblems(o)...)
	}
	return out
}

// outcomeIDProblems covers identity: a snake_case id, unique in the file.
func outcomeIDProblems(o Outcome, seen map[string]bool) []string {
	var out []string
	if !validPanelName(o.ID) {
		out = append(out, fmt.Sprintf("outcome %q must be snake_case", o.ID))
	}
	if seen[o.ID] {
		out = append(out, fmt.Sprintf("duplicate outcome id %q", o.ID))
	}
	seen[o.ID] = true
	return out
}

// outcomeClaimProblems covers the sentence, the beneficiary and the window.
func outcomeClaimProblems(o Outcome) []string {
	var out []string
	if strings.TrimSpace(o.Job) == "" {
		out = append(out, fmt.Sprintf(
			"outcome %q has an empty job: a ratio without the job it measures is a number nobody can disagree with", o.ID))
	}
	if !slices.Contains(outcomeBeneficiaries, o.Beneficiary) {
		out = append(out, fmt.Sprintf(
			"outcome %q beneficiary %q must be one of %v", o.ID, o.Beneficiary, outcomeBeneficiaries))
	}
	if o.Window != "" && !slices.Contains(outcomeWindows, o.Window) {
		out = append(out, fmt.Sprintf(
			"outcome %q window %q must be one of %v", o.ID, o.Window, outcomeWindows))
	}
	if o.Target != nil && (*o.Target <= 0 || *o.Target > 1) {
		out = append(out, fmt.Sprintf(
			"outcome %q target %v must be in (0,1]: a rate outside that range is a different unit", o.ID, *o.Target))
	}
	return out
}

// outcomeQueryProblems is the instrumented half: both halves must be valid
// counts, and they must read a table the same declaration claims. This is the
// one part of "is it actually instrumented" that is checkable without guessing —
// a query over a table the recipe does not own proves the declaration is
// describing something else.
func outcomeQueryProblems(o Outcome, declaredTables map[string]bool) []string {
	var out []string
	for _, half := range []struct {
		name string
		m    Metric
	}{{"success", o.Success}, {"total", o.Total}} {
		if problems := metricProblems(half.m); len(problems) > 0 {
			for _, p := range problems {
				out = append(out, fmt.Sprintf("outcome %q %s: %s", o.ID, half.name, p))
			}
			continue
		}
		table, ok := queryFromTable(half.m.Query)
		if !ok {
			out = append(out, fmt.Sprintf(
				"outcome %q %s: query must select from one table", o.ID, half.name))
			continue
		}
		if !declaredTables[table] {
			out = append(out, fmt.Sprintf(
				"outcome %q %s reads table %q, which the declaration does not list: "+
					"an outcome over a table the recipe does not own cannot be attributed to it",
				o.ID, half.name, table))
		}
	}
	return out
}

// outcomeSignalProblems covers the counter signals: closed tokens, no repeats.
func outcomeSignalProblems(o Outcome) []string {
	var out []string
	seen := make(map[string]bool, len(o.CounterSignals))
	for _, s := range o.CounterSignals {
		if !isClosedToken(s) {
			out = append(out, fmt.Sprintf(
				"outcome %q counter signal %q is not a closed token", o.ID, s))
			continue
		}
		if seen[s] {
			out = append(out, fmt.Sprintf(
				"outcome %q declares counter signal %q twice", o.ID, s))
		}
		seen[s] = true
	}
	return out
}

// queryFromTable returns the table a COUNT query reads, lower-cased, and
// whether one could be read at all.
func queryFromTable(query string) (string, bool) {
	m := queryFromTableRE.FindStringSubmatch(query)
	if m == nil {
		return "", false
	}
	return strings.ToLower(m[1]), true
}

// OutcomeInstrumentationGaps reports counter signals the recipe's code never
// mentions. The declaration is well-formed, but the token is the state the ratio
// is limited by: if no write path records it, the declared outcome is a number
// nobody can read, and the fix is either recording it or dropping the claim.
//
// `recipe check` grades these as errors. Boot never runs this check — a legacy
// receipt must keep booting — so the gate lives where the declaration is judged,
// not in the engine.
func OutcomeInstrumentationGaps(root string, r Receipt) []string {
	if len(r.Declaration.Outcomes) == 0 {
		return nil
	}
	code, ok := recipeCode(root, r)
	if !ok {
		return nil
	}
	blob := strings.Join(code, "\n")
	var out []string
	for _, o := range r.Declaration.Outcomes {
		for _, signal := range o.CounterSignals {
			if !strings.Contains(blob, signal) {
				out = append(out, fmt.Sprintf(
					"outcome %q declares counter signal %q that the recipe's code never mentions",
					o.ID, signal))
			}
		}
	}
	return out
}
