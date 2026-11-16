// Recipe string translations: the declared label and its localized spelling.
//
// A metric label is DATA the recipe author wrote, so it cannot live in the app
// catalog (the slice may be uninstalled) and cannot be looked up by a core key
// (the wording is the author's). It travels with the declaration.
package recipe

import (
	"strings"
	"testing"
)

func translationsDecl() Declaration {
	return Declaration{
		Metrics: []Metric{
			{Kind: MetricActivity, Label: "Orders confirmed", Query: "SELECT COUNT(*) FROM t WHERE owner = ?"},
			{
				Kind: MetricOutcome, ID: "done", Job: "job", Beneficiary: "user",
				Success:        &Count{Label: "Orders without repair", Query: "SELECT COUNT(*) FROM t WHERE owner = ? AND ok = 1"},
				Total:          &Count{Label: "Orders confirmed", Query: "SELECT COUNT(*) FROM t WHERE owner = ?"},
				CounterMetrics: []Count{{Label: "Orders corrected", Query: "SELECT COUNT(*) FROM t WHERE owner = ? AND fixed = 1"}},
			},
		},
		Translations: []Translations{{
			Locale: "pt-BR",
			Metrics: map[string]string{
				"Orders confirmed":      "Pedidos confirmados",
				"Orders without repair": "Pedidos sem reparo",
				"Orders corrected":      "Pedidos corrigidos",
			},
		}},
	}
}

func TestLabelForPicksTheLocale(t *testing.T) {
	d := translationsDecl()
	if got := d.LabelFor("Orders confirmed", "pt-BR"); got != "Pedidos confirmados" {
		t.Errorf("pt-BR label = %q", got)
	}
	if got := d.LabelFor("Orders corrected", "PT-br"); got != "Pedidos corrigidos" {
		t.Errorf("tag match must be case-insensitive, got %q", got)
	}
}

// TestLabelForFallsBackToTheDeclaredWord: an untranslated locale renders the
// author's word, never a dotted key and never an empty cell.
func TestLabelForFallsBackToTheDeclaredWord(t *testing.T) {
	d := translationsDecl()
	if got := d.LabelFor("Orders confirmed", "de"); got != "Orders confirmed" {
		t.Errorf("missing locale = %q, want the declared label", got)
	}
	if got := d.LabelFor("Orders confirmed", ""); got != "Orders confirmed" {
		t.Errorf("empty locale = %q", got)
	}
}

// TestTranslationForAnUnknownLabelIsAnError: a stray key is a typo the reader
// would silently ignore (lookup is by label), so the declaration gate says it.
func TestTranslationForAnUnknownLabelIsAnError(t *testing.T) {
	d := translationsDecl()
	d.Translations[0].Metrics["Orders that do not exist"] = "x"
	problems := MetricProblems(d)
	if !hasProblem(problems, "which no declared metric shows") {
		t.Fatalf("unknown label accepted: %v", problems)
	}
}

func TestTranslationWithoutALocaleIsAnError(t *testing.T) {
	d := translationsDecl()
	d.Translations = append(d.Translations, Translations{Metrics: map[string]string{"Orders confirmed": "x"}})
	if problems := MetricProblems(d); !hasProblem(problems, "no locale") {
		t.Fatalf("locale-less translation accepted: %v", problems)
	}
}

func TestDuplicateTranslationLocaleIsAnError(t *testing.T) {
	d := translationsDecl()
	d.Translations = append(d.Translations, Translations{
		Locale: "PT-br", Metrics: map[string]string{"Orders confirmed": "x"},
	})
	if problems := MetricProblems(d); !hasProblem(problems, "declared twice") {
		t.Fatalf("duplicate locale accepted: %v", problems)
	}
}

func hasProblem(problems []string, substr string) bool {
	for _, p := range problems {
		if strings.Contains(p, substr) {
			return true
		}
	}
	return false
}
