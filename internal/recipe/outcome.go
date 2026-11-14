// Outcome checks that need the whole declaration: the tables an outcome reads
// must be tables the same declaration claims.
//
// Split from metric.go because this is the one rule that cannot be applied to a
// metric in isolation — it compares a metric against `Declaration.Tables`. A
// query over a table the recipe does not own proves the declaration is
// describing somebody else's data, which is the failure a shape-only check
// cannot see.
package recipe

import (
	"fmt"
	"regexp"
	"strings"
)

// queryFromTableRE extracts the single table a COUNT query reads. It is
// deliberately not a SQL parser: the contract already limits counts to
// `SELECT COUNT(*) ... WHERE ...`, so `FROM <ident>` is the whole surface the
// ownership check has to read.
var queryFromTableRE = regexp.MustCompile(`(?i)\bfrom\s+([a-z_][a-z0-9_]*)`)

// OutcomeTableProblems checks every count in the declaration against the tables
// it claims. It runs on boot, in `recipe check` and in the catalog review.
func OutcomeTableProblems(d Declaration) []string {
	if len(d.Metrics) == 0 {
		return nil
	}
	declared := make(map[string]bool, len(d.Tables))
	for _, t := range d.Tables {
		declared[strings.ToLower(t)] = true
	}
	var out []string
	for _, m := range d.Metrics {
		switch m.ResolvedKind() {
		case MetricActivity:
			// An activity count is not a claim about a result, so an undeclared
			// table is a catalog-staleness warning the dashboard already absorbs
			// (a failed query omits the number). Only an outcome owes the
			// ownership proof: it is the number a reader acts on.
		case MetricOutcome:
			if m.Success != nil {
				out = append(out, countTableProblems("outcome "+m.ID+" success", *m.Success, declared)...)
			}
			if m.Total != nil {
				out = append(out, countTableProblems("outcome "+m.ID+" total", *m.Total, declared)...)
			}
			for i, c := range m.Counterbalance {
				out = append(out, countTableProblems(
					fmt.Sprintf("outcome %s counterbalance[%d]", m.ID, i), c, declared)...)
			}
		}
	}
	return out
}

// countTableProblems checks one count's FROM clause against the claimed set.
func countTableProblems(prefix string, c Count, declared map[string]bool) []string {
	if strings.TrimSpace(c.Query) == "" {
		return nil // shape problems are MetricProblems' job
	}
	table, ok := queryFromTable(c.Query)
	if !ok {
		return []string{prefix + ": query must select from one table"}
	}
	if !declared[table] {
		return []string{fmt.Sprintf(
			"%s reads table %q, which the declaration does not list: a reading over "+
				"a table the recipe does not own cannot be attributed to it",
			prefix, table)}
	}
	return nil
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

// OutcomeInstrumentationGaps reports counterbalance readings whose state the
// recipe's code never records.
//
// The declaration is well-formed, but a counterbalance count over a column no
// write path ever sets is a number nobody can read, and the fix is either
// recording it or dropping the claim. `recipe check` grades these as errors.
// Boot never runs this check — a legacy receipt must keep booting — so the gate
// lives where the declaration is judged, not in the engine.
func OutcomeInstrumentationGaps(root string, r Receipt) []string {
	outcomes := r.Declaration.Outcomes()
	if len(outcomes) == 0 {
		return nil
	}
	code, ok := recipeCode(root, r)
	if !ok {
		return nil
	}
	blob := strings.Join(code, "\n")
	var out []string
	for _, o := range outcomes {
		for _, c := range o.Counterbalance {
			if token := counterToken(c.Query); token != "" && !strings.Contains(blob, token) {
				out = append(out, fmt.Sprintf(
					"outcome %q declares counterbalance %q that the recipe's code never mentions",
					o.ID, token))
			}
		}
	}
	return out
}

// counterToken is the column a counterbalance query filters on, which is the
// state the code must record.
//
// The owner binding (`owner = ?`) is skipped: it is the scoping every count
// carries, not the state that distinguishes this reading. A query with no
// diminishing filter (a plain total) has no token and contributes nothing.
func counterToken(query string) string {
	const where = " where "
	i := strings.Index(strings.ToLower(query), where)
	if i < 0 {
		return ""
	}
	cond := query[i+len(where):]
	// Split the filter on AND so `owner = ? AND corrected = 1` yields both
	// sides, and take the first one that is not the owner binding.
	for part := range strings.SplitSeq(strings.ToLower(cond), " and ") {
		part = strings.TrimSpace(part)
		end := strings.IndexAny(part, " =<>!(),")
		if end <= 0 {
			continue
		}
		col := strings.TrimSpace(part[:end])
		if col == "" || col == "owner" {
			continue
		}
		return col
	}
	return ""
}
