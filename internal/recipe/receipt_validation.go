// Declaration checks: receipt claims against the code and the schema.
package recipe

import (
	"fmt"
	"strings"

	"github.com/calionauta/gogogo/internal/dbschema"
)

// present when its pattern string appears anywhere in the recipe's own code —
// enough to catch "declared a page that was never built", not enough to catch a
// method mismatch. That is the right trade for a drift warning; a test would be
// the place for the stricter claim.
func checkDeclaration(root string, r Receipt) []string {
	var problems []string

	code, ok := recipeCode(root, r)
	if !ok {
		return []string{"declaration cannot be checked: no recipe code found in the tree"}
	}
	blob := strings.Join(code, "\n")

	for _, rt := range r.Declaration.Routes {
		if !strings.Contains(blob, rt.Pattern) {
			problems = append(problems,
				fmt.Sprintf("declared route %s %s does not appear in the recipe's code", rt.Method, rt.Pattern))
		}
	}
	for _, job := range r.Declaration.Jobs {
		if !strings.Contains(blob, job) {
			problems = append(problems,
				fmt.Sprintf("declared job %q is not registered by the recipe's code", job))
		}
	}
	// Slots are the third surface a declaration can claim out of thin air: the
	// code can be byte-pristine and still contribute nothing to the slot it
	// declared, or declare a slot this build has no page for.
	for _, slot := range r.Declaration.Slots {
		if offer := SlotProblems([]string{slot}); len(offer) > 0 {
			problems = append(problems, offer...)
			continue
		}
		if !slotContributed(blob, slot) {
			problems = append(problems,
				fmt.Sprintf("declared slot %q is not contributed by the recipe's code", slot))
		}
	}
	for _, table := range r.Declaration.Tables {
		// The ownership boundary is checked FIRST and independently of whether
		// the table appears in the recipe's SQL. Ordering matters: if the SQL
		// check ran first it would mask this one, and a core-table claim would be
		// reported as a missing-SQL nit instead of as a boundary violation.
		if isCoreTable(table) {
			problems = append(problems,
				fmt.Sprintf("declared table %q is a core table: a recipe may not own a core object", table))
			continue
		}
		// A table is declared, not written in Go: it must appear in the recipe's
		// own SQL, never in a core migration.
		if !strings.Contains(blob, table) {
			problems = append(problems,
				fmt.Sprintf("declared table %q has no SQL in the recipe's own files", table))
		}
	}
	// The action table is the invocable surface: every verb must target a
	// route this same declaration claims. Pure data, checked alongside the
	// other declaration halves so a drifted table is refused at boot.
	problems = append(problems, actionProblems(r.Declaration)...)
	problems = append(problems, OmissionProblems(r.Declaration)...)
	problems = append(problems, MetricProblems(r.Declaration)...)
	problems = append(problems, OutcomeProblems(r.Declaration)...)
	if ps := PanelProblems(r.Declaration); len(ps) > 0 {
		problems = append(problems, ps...)
	}
	return problems
}

// MetricProblems applies the one metric rule: query counts, owner-scoped,
// single placeholder. Exported so registration, the catalog and the
// dashboard executor share one definition of a valid metric.
func MetricProblems(d Declaration) []string {
	out := make([]string, 0, len(d.Metrics))
	for _, m := range d.Metrics {
		out = append(out, metricProblems(m)...)
	}
	return out
}

// metricProblems is the single metric's rules, shared by a dashboard count and
// by each half of an outcome. One function, because a second spelling of "a
// valid count" is how the two surfaces drift apart.
func metricProblems(m Metric) []string {
	var out []string
	if strings.TrimSpace(m.Label) == "" {
		out = append(out, "metric with an empty label")
	}
	if err := validCount(m.Query, m.BindsOwner()); err != nil {
		out = append(out, fmt.Sprintf("metric %q: %v", m.Label, err))
	}
	// The signal drives the dashboard's counter: it lands inside a
	// `$name` expression in the page's script, so a name that is not an
	// identifier is a syntax error there. It fails here instead.
	if m.Signal != "" && !IsJSIdent(m.Signal) {
		out = append(out, fmt.Sprintf(
			"metric %q: signal %q is not a JavaScript identifier", m.Label, m.Signal))
	}
	return out
}

// IsJSIdent reports whether s is a plain JavaScript identifier: a letter or
// underscore followed by letters, digits or underscores.
//
// One definition, two callers: the declaration check above (a metric whose
// signal is malformed is refused at boot) and the dashboard's odometer (a
// malformed signal gets no count-up trigger instead of a broken script).
func IsJSIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// PanelProblems applies the panel rule: snake_case component, prompt,
// at least one writable prop (the model must choose a slice).
func PanelProblems(d Declaration) []string {
	if d.Panel == nil {
		return nil
	}
	var out []string
	if !validPanelName(d.Panel.Component) {
		out = append(out, fmt.Sprintf("panel component %q must be snake_case", d.Panel.Component))
	}
	if strings.TrimSpace(d.Panel.Prompt) == "" {
		out = append(out, fmt.Sprintf("panel %q has an empty prompt", d.Panel.Component))
	}
	if len(d.Panel.Writable) == 0 {
		out = append(out, fmt.Sprintf("panel %q declares no writable prop", d.Panel.Component))
	}
	for _, w := range d.Panel.Writable {
		if strings.TrimSpace(w) == "" {
			out = append(out, fmt.Sprintf("panel %q declares an empty writable prop", d.Panel.Component))
		}
	}
	if len(d.Panel.Writable) > 1 {
		out = append(out, fmt.Sprintf(
			"panel %q declares %d writable props; by convention it is exactly one (the slice)",
			d.Panel.Component, len(d.Panel.Writable)))
	}
	if len(d.Panel.Values) > 0 {
		for _, v := range d.Panel.Values {
			if !strings.EqualFold(v, "all") && !isClosedToken(v) {
				out = append(out, fmt.Sprintf("panel %q value %q is not a closed token", d.Panel.Component, v))
			}
		}
	}
	return out
}

// IsCoreTable reports whether a name belongs to the core schema. Exported for
// the catalog validator, which applies the same boundary to a reviewed entry.
func IsCoreTable(table string) bool { return isCoreTable(table) }

// validCount reports whether query is a single COUNT(*) with one `?`
// (owner-scoped) or none (global). Anything else fails fast at
// registration, not as a broken SQL string at render time.
func validCount(query string, ownerBinds bool) error {
	q := strings.TrimSpace(strings.ToLower(query))
	if !strings.HasPrefix(q, "select count") {
		return fmt.Errorf("query must select count(*), got %q", query)
	}
	n := strings.Count(query, "?")
	want := 1
	if !ownerBinds {
		want = 0
	}
	if n != want {
		return fmt.Errorf("query must bind %d owner parameter (?), got %d", want, n)
	}
	return nil
}

// isClosedToken reports whether v is one word a prompt may offer the
// model (no spaces, no punctuation — the slice enum must be copyable
// from the sentence into props).
func isClosedToken(v string) bool {
	if strings.TrimSpace(v) == "" {
		return false
	}
	for _, r := range v {
		lower := (r >= 'a' && r <= 'z')
		upper := (r >= 'A' && r <= 'Z')
		digit := (r >= '0' && r <= '9')
		if lower || upper || digit || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

// validPanelName reports whether name is snake_case (lowercase letters,
// digits, underscores, no leading/trailing underscore).
func validPanelName(name string) bool {
	if name == "" || strings.HasPrefix(name, "_") || strings.HasSuffix(name, "_") {
		return false
	}
	for _, r := range name {
		lower := (r >= 'a' && r <= 'z')
		digit := (r >= '0' && r <= '9')
		if lower || digit || r == '_' {
			continue
		}
		return false
	}
	return true
}

// isCoreTable reports whether a name belongs to the core schema. The rule the
// recipe model depends on: a recipe never owns a core object, because two
// owners of one table is what makes uninstall unsafe and migrations
// unmergeable.
//
// The set comes from dbschema, which owns it and gates it against the migrated
// schema in both directions. The inline list this replaces was a stale copy in
// the wrong package: it named two tables that do not exist and missed nine that
// do, so `notes` and `boards` were claimable.
func isCoreTable(table string) bool {
	name := strings.ToLower(table)
	for _, core := range dbschema.CoreTables() {
		if name == core || strings.HasPrefix(name, core+"_") {
			return true
		}
	}
	return false
}

// recipeCode returns the installed files' contents, so the declaration can be
