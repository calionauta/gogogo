// Declared metrics: the one list a recipe uses to say what it measures.
//
// A metric is a reading. Two kinds cover what a recipe can honestly claim from
// its own tables:
//
//	activity — how much happened (one count, no claim).
//	outcome  — did the job get done, as a success count against the eligible
//	           count, plus the counterMetrics readings that stop the success
//	           number being read as health.
//
// WHY ONE LIST, NOT TWO FIELDS. The previous shape had `metrics` and `outcomes`
// as siblings, which made the reader guess which one answers "is it working".
// They are the same question at different strengths, so they are one list with
// a `kind`, and the dashboard renders them in declaration order.
//
// WHY COUNTERBALANCE, NOT "COUNTER SIGNALS". A bare token (`corrected`) was a
// note to the author that nothing could query. A reading is a labeled COUNT like
// the success half, so the tension the declaration claims is visible on the
// page: success 8/10 beside "corrected 2" is a fact, not a guess.
//
// WHY NO EVENT STREAM AND NO LOG SOURCE. The query must be COUNT(*) over a table
// the recipe already owns. A reading that needs history means the recipe is
// missing a column, not that it needs a pipeline — and a reading over log text
// would need a second query engine (DuckDB and friends) for a number the same
// table can answer. Landing the fact in a table is the mechanism; that is why
// `security_events` exists.
package recipe

import (
	"fmt"
	"slices"
	"strings"
)

// MetricKind is what a metric claims. The zero value is Activity so a
// declaration written before this field existed still reads as a plain count.
type MetricKind string

const (
	// MetricUnset is the zero value: an entry written before `kind` existed.
	// Spelled as a constant so "did this declare a kind?" is a comparison of
	// two constants, never of a type against "".
	MetricUnset MetricKind = ""
	// MetricActivity counts activity. No denominator, no claim.
	MetricActivity MetricKind = "activity"
	// MetricOutcome answers a job as success over eligible, with the
	// counterMetrics readings that qualify it.
	MetricOutcome MetricKind = "outcome"
)

// MetricKinds is the closed set, in the order the docs present them.
var MetricKinds = []MetricKind{MetricActivity, MetricOutcome}

// metricWord is the one spelling a conflict and a count error use.
const metricWord = "metric"

// beneficiary is whose job an outcome serves. A closed set: the operator's
// number and the user's number must not be the same declaration, or optimizing
// against the person becomes indistinguishable from serving them.
var outcomeBeneficiaries = []string{"user", "operator"}

// outcomeWindows is the period an outcome pair is read over. Empty is the
// honest default (cumulative).
var outcomeWindows = []string{"session", "day", "none"}

// Count is one labeled COUNT(*) query: the only primitive a metric is built
// from. `?` binds the viewing owner when OwnerBinds, and the query must read a
// table the same declaration claims.
type Count struct {
	Label string `yaml:"label"`
	Query string `yaml:"query"`
	// AllQuery is the same reading without the owner filter, for the admin
	// dashboard. It must be a COUNT(*) with no `?`. Empty means the reading is
	// owner-only: the admin dashboard then shows no number rather than a sum of
	// per-user counts, which would double-count shared rows and leak one user's
	// data shape.
	AllQuery string `yaml:"allQuery,omitempty"`
	// Signal is the dashboard counter's JS identifier. Empty derives it from
	// the label.
	Signal string `yaml:"signal,omitempty"`
	// OwnerBinds reports whether the query scopes to the viewing owner (one
	// `?`). nil means the convention default (true).
	OwnerBinds *bool `yaml:"ownerBinds,omitempty"`
}

// BindsOwner reports whether the count is owner-scoped.
func (c Count) BindsOwner() bool {
	if c.OwnerBinds == nil {
		return true
	}
	return *c.OwnerBinds
}

// WithoutOwner is the global-rows spelling (OwnerBinds=false).
func WithoutOwner() *bool {
	f := false
	return &f
}

// Metric is one reading. Kind decides which fields are required: an activity
// metric uses the top-level Label/Query, an outcome uses ID/Job/Success/Total
// plus its CounterMetrics readings.
//
// A flat union rather than two list fields: the reader sees one ordered list,
// and `MetricProblems` is the single place that decides whether a given kind has
// the fields it needs. The alternative (two structs and a custom YAML
// discriminator) would make every consumer switch on a wrapper type to reach
// data it already knows how to render.
type Metric struct {
	Kind MetricKind `yaml:"kind,omitempty"`

	// activity: the count itself.
	Label      string `yaml:"label,omitempty"`
	Query      string `yaml:"query,omitempty"`
	AllQuery   string `yaml:"allQuery,omitempty"`
	Signal     string `yaml:"signal,omitempty"`
	OwnerBinds *bool  `yaml:"ownerBinds,omitempty"`

	// outcome: the job and its two halves.
	ID             string   `yaml:"id,omitempty"`
	Job            string   `yaml:"job,omitempty"`
	Beneficiary    string   `yaml:"beneficiary,omitempty"`
	Success        *Count   `yaml:"success,omitempty"`
	Total          *Count   `yaml:"total,omitempty"`
	CounterMetrics []Count  `yaml:"counterMetrics,omitempty"`
	Window         string   `yaml:"window,omitempty"`
	Target         *float64 `yaml:"target,omitempty"`
}

// Translations is the localized spelling of the labels this declaration shows.
//
// A metric's label is DATA the recipe author wrote, so it cannot live in the
// app catalog (the recipe may be uninstalled) and it cannot be looked up by a
// core key (the wording is the author's). It travels WITH the declaration, one
// entry per locale tag, and the reader picks the one the request asks for. A
// tag with no entry falls back to the declared label, which is why a partial
// translation renders instead of showing a dotted key.
type Translations struct {
	// Locale is the tag (`pt-BR`, `en`), matched case-insensitively.
	Locale string `yaml:"locale"`
	// Metrics maps a declared metric label to its spelling in this locale.
	Metrics map[string]string `yaml:"metrics,omitempty"`
}

// ResolvedKind is the kind this metric declares, with the zero value read as
// Activity (the pre-kind spelling).
func (m Metric) ResolvedKind() MetricKind {
	if m.Kind == MetricUnset {
		return MetricActivity
	}
	return m.Kind
}

// AsCount views an activity metric through the Count shape, so one executor
// serves both kinds.
func (m Metric) AsCount() Count {
	return Count{
		Label: m.Label, Query: m.Query, AllQuery: m.AllQuery,
		Signal: m.Signal, OwnerBinds: m.OwnerBinds,
	}
}

// Activity returns the activity metrics in declaration order.
func (d Declaration) Activity() []Metric {
	out := make([]Metric, 0, len(d.Metrics))
	for _, m := range d.Metrics {
		if m.ResolvedKind() == MetricActivity {
			out = append(out, m)
		}
	}
	return out
}

// OutcomeMetrics returns the metrics whose kind is outcome, in declaration
// order. They are metrics like any other; the name says which projection this
// is, never that an outcome is a second entity.
func (d Declaration) OutcomeMetrics() []Metric {
	out := make([]Metric, 0, len(d.Metrics))
	for _, m := range d.Metrics {
		if m.ResolvedKind() == MetricOutcome {
			out = append(out, m)
		}
	}
	return out
}

// MetricProblems applies the metric rules to every entry, dispatching by kind.
// Exported so registration, the catalog and the dashboard executor share one
// definition of a valid metric.
func MetricProblems(d Declaration) []string {
	return append(metricListProblems(d.Metrics), translationProblems(d)...)
}

// translationProblems checks the localized spellings: a translation must name a
// locale, name only labels the declaration actually shows, and not repeat a
// locale. A translation for a label nobody renders is a typo that would never
// surface (the reader looks up by label, so the stray key is simply ignored).
func translationProblems(d Declaration) []string {
	shown := map[string]bool{}
	for _, m := range d.Metrics {
		for _, label := range metricLabels(m) {
			shown[label] = true
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, t := range d.Translations {
		tag := strings.TrimSpace(t.Locale)
		if tag == "" {
			out = append(out, "translation entry has no locale")
			continue
		}
		if seen[strings.ToLower(tag)] {
			out = append(out, fmt.Sprintf("translation for %q is declared twice", tag))
		}
		seen[strings.ToLower(tag)] = true
		for label := range t.Metrics {
			if !shown[label] {
				out = append(out, fmt.Sprintf(
					"translation for %q names %q, which no declared metric shows", tag, label))
			}
		}
	}
	return out
}

// metricLabels is every human label one metric renders.
func metricLabels(m Metric) []string {
	if m.ResolvedKind() == MetricActivity {
		return []string{m.Label}
	}
	out := make([]string, 0, 2+len(m.CounterMetrics))
	if m.Success != nil {
		out = append(out, m.Success.Label)
	}
	if m.Total != nil {
		out = append(out, m.Total.Label)
	}
	for _, c := range m.CounterMetrics {
		out = append(out, c.Label)
	}
	return out
}

func metricListProblems(metrics []Metric) []string {
	out := make([]string, 0, len(metrics))
	seenIDs := make(map[string]bool, len(metrics))
	for _, m := range metrics {
		switch m.ResolvedKind() {
		case MetricActivity:
			out = append(out, countProblems("", m.AsCount())...)
		case MetricOutcome:
			out = append(out, outcomeProblems(m, seenIDs)...)
		default:
			out = append(out, fmt.Sprintf(
				"metric %q kind %q must be one of %v", metricName(m), m.Kind, MetricKinds))
		}
	}
	return out
}

// metricName is what an error message calls a metric: its id when it has one,
// its label otherwise.
func metricName(m Metric) string {
	if m.ID != "" {
		return m.ID
	}
	return m.Label
}

// outcomeProblems covers one outcome: identity, the claim, both halves and the
// counterMetrics readings.
func outcomeProblems(m Metric, seen map[string]bool) []string {
	var out []string
	if !validPanelName(m.ID) {
		out = append(out, fmt.Sprintf("outcome %q must be snake_case", m.ID))
	}
	if seen[m.ID] {
		out = append(out, fmt.Sprintf("duplicate outcome id %q", m.ID))
	}
	seen[m.ID] = true
	if strings.TrimSpace(m.Job) == "" {
		out = append(out, fmt.Sprintf(
			"outcome %q has an empty job: a ratio without the job it measures is a number nobody can disagree with", m.ID))
	}
	if !slices.Contains(outcomeBeneficiaries, m.Beneficiary) {
		out = append(out, fmt.Sprintf(
			"outcome %q beneficiary %q must be one of %v", m.ID, m.Beneficiary, outcomeBeneficiaries))
	}
	if m.Window != "" && !slices.Contains(outcomeWindows, m.Window) {
		out = append(out, fmt.Sprintf(
			"outcome %q window %q must be one of %v", m.ID, m.Window, outcomeWindows))
	}
	if m.Target != nil && (*m.Target <= 0 || *m.Target > 1) {
		out = append(out, fmt.Sprintf(
			"outcome %q target %v must be in (0,1]: a rate outside that range is a different unit", m.ID, *m.Target))
	}
	if m.Success == nil || m.Total == nil {
		out = append(out, fmt.Sprintf("outcome %q needs both success and total counts", m.ID))
		return out
	}
	out = append(out, countProblems("outcome "+m.ID+" success", *m.Success)...)
	out = append(out, countProblems("outcome "+m.ID+" total", *m.Total)...)
	for i, c := range m.CounterMetrics {
		out = append(out, countProblems(fmt.Sprintf("outcome %s counterMetrics[%d]", m.ID, i), c)...)
	}
	return out
}

// countProblems is the ONE definition of a valid count, shared by an activity
// metric and by every half of an outcome. A second spelling of "a valid count"
// is how the surfaces drift apart.
func countProblems(prefix string, c Count) []string {
	var out []string
	name := c.Label
	if prefix != "" {
		name = prefix
	}
	if strings.TrimSpace(c.Label) == "" {
		out = append(out, "count with an empty label")
	}
	if err := validCount(c.Query, c.BindsOwner()); err != nil {
		out = append(out, fmt.Sprintf("%s: %v", metricLabel(name), err))
	}
	if c.AllQuery != "" {
		if err := validCount(c.AllQuery, false); err != nil {
			out = append(out, fmt.Sprintf("%s: allQuery: %v", metricLabel(name), err))
		}
	}
	// The signal drives the dashboard's counter: it lands inside a `$name`
	// expression in the page's script, so a name that is not an identifier is
	// a syntax error there. It fails here instead.
	if c.Signal != "" && !IsJSIdent(c.Signal) {
		out = append(out, fmt.Sprintf(
			"%s: signal %q is not a JavaScript identifier", metricLabel(name), c.Signal))
	}
	return out
}

// metricLabel is the human handle in a count error.
func metricLabel(name string) string {
	if name == "" {
		return metricWord
	}
	return fmt.Sprintf("metric %q", name)
}

// LabelFor returns the declared label's spelling in one locale, falling back to
// the declared label itself.
//
// The fallback is the contract: a third-party recipe that ships no translation
// must not have its wording replaced by a key, and a partial translation must
// render the English word instead of a dotted identifier.
func (d Declaration) LabelFor(label, locale string) string {
	if label == "" || locale == "" {
		return label
	}
	for _, t := range d.Translations {
		if !strings.EqualFold(t.Locale, locale) {
			continue
		}
		if v, ok := t.Metrics[label]; ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return label
}
