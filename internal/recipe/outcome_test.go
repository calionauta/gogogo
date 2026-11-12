// Outcome declaration checks: the pair, its ownership claim, and the one gap
// that needs the recipe's code rather than its declaration.
package recipe

import (
	"strings"
	"testing"
)

// validOutcome is the reference shape: two counts over a declared table, one
// counter signal, no invented target.
func validOutcome() Outcome {
	return Outcome{
		ID:          "order_without_repair",
		Job:         "Receive the coffee you asked for without fixing a misread order",
		Beneficiary: "user",
		Success: Metric{
			Label: "Orders without repair",
			Query: "SELECT COUNT(*) FROM ext_coffee_orders WHERE owner = ? AND corrected = 0",
		},
		Total: Metric{
			Label: "Orders confirmed",
			Query: "SELECT COUNT(*) FROM ext_coffee_orders WHERE owner = ?",
		},
		CounterSignals: []string{"corrected"},
		Window:         "session",
	}
}

// TestOutcomeProblemsAcceptsTheDeclaredShape is the positive control: without
// it, "everything is a problem" would pass every negative case below.
func TestOutcomeProblemsAcceptsTheDeclaredShape(t *testing.T) {
	d := Declaration{Tables: []string{"ext_coffee_orders"}, Outcomes: []Outcome{validOutcome()}}
	if problems := OutcomeProblems(d); len(problems) != 0 {
		t.Fatalf("a well-formed outcome must pass, got %v", problems)
	}
}

// TestOutcomeProblemsPinEveryRule walks the failure modes one at a time, because
// the message is the affordance: each one has to name the outcome and the field.
func TestOutcomeProblemsPinEveryRule(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Outcome)
		want   string
	}{
		{"id is not snake_case", func(o *Outcome) { o.ID = "OrderWithoutRepair" }, "must be snake_case"},
		{"job is empty", func(o *Outcome) { o.Job = "  " }, "has an empty job"},
		{"beneficiary outside the closed set", func(o *Outcome) { o.Beneficiary = "team" }, "must be one of"},
		{"window outside the closed set", func(o *Outcome) { o.Window = "month" }, "window \"month\""},
		{"target above one", func(o *Outcome) { v := 1.5; o.Target = &v }, "must be in (0,1]"},
		{"target at zero", func(o *Outcome) { v := 0.0; o.Target = &v }, "must be in (0,1]"},
		{"success is not a count", func(o *Outcome) {
			o.Success.Query = "SELECT id FROM ext_coffee_orders WHERE owner = ?"
		}, "must select count"},
		{"success binds the wrong owners", func(o *Outcome) {
			o.Success.Query = "SELECT COUNT(*) FROM ext_coffee_orders"
		}, "must bind 1 owner parameter"},
		{"total reads an undeclared table", func(o *Outcome) {
			o.Total.Query = "SELECT COUNT(*) FROM todos WHERE owner = ?"
		}, "which the declaration does not list"},
		{"counter signal is not a token", func(o *Outcome) {
			o.CounterSignals = []string{"the user corrected it"}
		}, "is not a closed token"},
		{"counter signal repeats", func(o *Outcome) {
			o.CounterSignals = []string{"corrected", "corrected"}
		}, "twice"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := validOutcome()
			tc.mutate(&o)
			problems := OutcomeProblems(Declaration{
				Tables: []string{"ext_coffee_orders"}, Outcomes: []Outcome{o},
			})
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Fatalf("expected a problem containing %q, got %v", tc.want, problems)
			}
		})
	}
}

// TestOutcomeIDsAreUnique pins the second outcome that reuses an id: the
// dashboard keys a ratio by id, so two of them would collide silently.
func TestOutcomeIDsAreUnique(t *testing.T) {
	o := validOutcome()
	problems := OutcomeProblems(Declaration{
		Tables: []string{"ext_coffee_orders"}, Outcomes: []Outcome{o, o},
	})
	if !strings.Contains(strings.Join(problems, "\n"), "duplicate outcome id") {
		t.Fatalf("expected a duplicate-id problem, got %v", problems)
	}
}

// TestOutcomeOverUndeclaredTableIsAnError is the enforced half of "is this
// instrumented": the query names the table, and the declaration must own it.
func TestOutcomeOverUndeclaredTableIsAnError(t *testing.T) {
	problems := OutcomeProblems(Declaration{Outcomes: []Outcome{validOutcome()}})
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, `reads table "ext_coffee_orders"`) {
		t.Fatalf("a table outside the declaration must be refused, got %v", problems)
	}
}

// TestOutcomeInstrumentationGapsNamesTheMissingSignal is the instrumentation
// half: the declaration is valid, the code mentions one counter signal and not
// the other, and the report names the gap `recipe check` blocks on.
func TestOutcomeInstrumentationGapsNamesTheMissingSignal(t *testing.T) {
	files := map[string]string{
		"features/coffee/service/orders.go": "// Confirm marks corrected=1 on a repair.\n",
	}
	root := writeTree(t, files)
	o := validOutcome()
	o.CounterSignals = []string{"corrected", "abandoned"}
	gaps := OutcomeInstrumentationGaps(root, receiptFor(files, Declaration{
		Tables: []string{"ext_coffee_orders"}, Outcomes: []Outcome{o},
	}))
	if len(gaps) != 1 || !strings.Contains(gaps[0], `"abandoned"`) {
		t.Fatalf("expected exactly the unmentioned signal, got %v", gaps)
	}
}
