// Metric and outcome declaration checks: the list, its ownership claim, and the
// one gap that needs the recipe's code rather than its declaration.
package recipe

import (
	"strings"
	"testing"
)

// validOutcome is the reference shape: an outcome with two counts over a
// declared table and one counterbalance reading, no invented target.
func validOutcome() Metric {
	return Metric{
		Kind:        MetricOutcome,
		ID:          "order_without_repair",
		Job:         "Receive the order you asked for without fixing a misread order",
		Beneficiary: "user",
		Success: &Count{
			Label: "Orders without repair",
			Query: "SELECT COUNT(*) FROM ext_kiosk_orders WHERE owner = ? AND corrected = 0",
		},
		Total: &Count{
			Label: "Orders confirmed",
			Query: "SELECT COUNT(*) FROM ext_kiosk_orders WHERE owner = ?",
		},
		Counterbalance: []Count{{
			Label: "Orders corrected",
			Query: "SELECT COUNT(*) FROM ext_kiosk_orders WHERE owner = ? AND corrected = 1",
		}},
		Window: "session",
	}
}

// validActivity is the reference activity metric: one count, no claim.
func validActivity() Metric {
	return Metric{Label: "Orders confirmed", Query: "SELECT COUNT(*) FROM ext_kiosk_orders WHERE owner = ?"}
}

// TestMetricProblemsAcceptsTheDeclaredShapes is the positive control: without
// it, "everything is a problem" would pass every negative case below.
func TestMetricProblemsAcceptsTheDeclaredShapes(t *testing.T) {
	d := Declaration{Tables: []string{"ext_kiosk_orders"}, Metrics: []Metric{validActivity(), validOutcome()}}
	if problems := MetricProblems(d); len(problems) != 0 {
		t.Fatalf("a well-formed declaration must pass, got %v", problems)
	}
	if problems := OutcomeTableProblems(d); len(problems) != 0 {
		t.Fatalf("a well-formed declaration must pass its table check, got %v", problems)
	}
	if len(d.Activity()) != 1 || len(d.Outcomes()) != 1 {
		t.Fatalf("kinds did not split: activity=%d outcomes=%d", len(d.Activity()), len(d.Outcomes()))
	}
}

// TestMetricProblemsPinEveryRule walks the failure modes one at a time, because
// the message is the affordance: each one has to name the metric and the field.
func TestMetricProblemsPinEveryRule(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Metric)
		want   string
	}{
		{"id is not snake_case", func(o *Metric) { o.ID = "OrderWithoutRepair" }, "must be snake_case"},
		{"job is empty", func(o *Metric) { o.Job = "  " }, "has an empty job"},
		{"beneficiary outside the closed set", func(o *Metric) { o.Beneficiary = "team" }, "must be one of"},
		{"window outside the closed set", func(o *Metric) { o.Window = "month" }, "window \"month\""},
		{"target above one", func(o *Metric) { v := 1.5; o.Target = &v }, "must be in (0,1]"},
		{"target at zero", func(o *Metric) { v := 0.0; o.Target = &v }, "must be in (0,1]"},
		{"success is not a count", func(o *Metric) {
			o.Success.Query = "SELECT id FROM ext_kiosk_orders WHERE owner = ?"
		}, "must select count"},
		{"success binds the wrong owners", func(o *Metric) {
			o.Success.Query = "SELECT COUNT(*) FROM ext_kiosk_orders"
		}, "must bind 1 owner parameter"},
		{"a missing half", func(o *Metric) { o.Total = nil }, "needs both success and total"},
		{"counterbalance is not a count", func(o *Metric) {
			o.Counterbalance[0].Query = "SELECT corrected FROM ext_kiosk_orders"
		}, "must select count"},
		{"unknown kind", func(o *Metric) { o.Kind = "ratio" }, "must be one of"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := validOutcome()
			tc.mutate(&o)
			problems := MetricProblems(Declaration{
				Tables: []string{"ext_kiosk_orders"}, Metrics: []Metric{o},
			})
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Fatalf("expected a problem containing %q, got %v", tc.want, problems)
			}
		})
	}
}

// TestActivityMetricPassesTheSameRules pins that the plain count is not a
// second-class citizen: same query rule, same signal rule.
func TestActivityMetricPassesTheSameRules(t *testing.T) {
	bad := validActivity()
	bad.Query = "SELECT id FROM ext_kiosk_orders WHERE owner = ?"
	if problems := MetricProblems(Declaration{Metrics: []Metric{bad}}); len(problems) == 0 {
		t.Fatal("an activity metric skipped the count rule")
	}
	bad = validActivity()
	bad.Signal = "not-an-ident"
	if problems := MetricProblems(Declaration{Metrics: []Metric{bad}}); len(problems) == 0 {
		t.Fatal("an activity metric skipped the signal rule")
	}
}

// TestOutcomeIDsAreUnique pins the second outcome that reuses an id: the
// dashboard keys a ratio by id, so two of them would collide silently.
func TestOutcomeIDsAreUnique(t *testing.T) {
	o := validOutcome()
	problems := MetricProblems(Declaration{
		Tables: []string{"ext_kiosk_orders"}, Metrics: []Metric{o, o},
	})
	if !strings.Contains(strings.Join(problems, "\n"), "duplicate outcome id") {
		t.Fatalf("expected a duplicate-id problem, got %v", problems)
	}
}

// TestOutcomeOverUndeclaredTableIsAnError is the enforced half of "is this
// instrumented": the query names the table, and the declaration must own it.
func TestOutcomeOverUndeclaredTableIsAnError(t *testing.T) {
	problems := OutcomeTableProblems(Declaration{Metrics: []Metric{validOutcome()}})
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, `reads table "ext_kiosk_orders"`) {
		t.Fatalf("a table outside the declaration must be refused, got %v", problems)
	}
}

// TestCounterbalanceOverUndeclaredTableIsAnError covers the reading the
// declaration added: it is a count like the other two, so it owes the same
// ownership proof.
func TestCounterbalanceOverUndeclaredTableIsAnError(t *testing.T) {
	o := validOutcome()
	o.Counterbalance[0].Query = "SELECT COUNT(*) FROM todos WHERE owner = ?"
	problems := OutcomeTableProblems(Declaration{
		Tables: []string{"ext_kiosk_orders"}, Metrics: []Metric{o},
	})
	if !strings.Contains(strings.Join(problems, "\n"), "counterbalance[0]") {
		t.Fatalf("a counterbalance over an undeclared table must be refused, got %v", problems)
	}
}

// TestOutcomeInstrumentationGapsNamesTheMissingState is the instrumentation
// half: the declaration is valid, the code records one counterbalance state and
// not the other, and the report names the gap `recipe check` blocks on.
func TestOutcomeInstrumentationGapsNamesTheMissingState(t *testing.T) {
	files := map[string]string{
		"features/kiosk/service/orders.go": "// Confirm marks corrected=1 on a repair.\n",
	}
	root := writeTree(t, files)
	o := validOutcome()
	o.Counterbalance = []Count{
		{Label: "Corrected", Query: "SELECT COUNT(*) FROM ext_kiosk_orders WHERE owner = ? AND corrected = 1"},
		{Label: "Abandoned", Query: "SELECT COUNT(*) FROM ext_kiosk_orders WHERE owner = ? AND abandoned = 1"},
	}
	gaps := OutcomeInstrumentationGaps(root, receiptFor(files, Declaration{
		Tables: []string{"ext_kiosk_orders"}, Metrics: []Metric{o},
	}))
	if len(gaps) != 1 || !strings.Contains(gaps[0], `"abandoned"`) {
		t.Fatalf("expected exactly the unmentioned state, got %v", gaps)
	}
}
