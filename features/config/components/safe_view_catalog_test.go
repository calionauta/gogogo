// SCOPE:feature — the /config answer-catalog wiring (genui-owned).
//
// This file is listed in the genui trim unit: without the generative
// catalog the config page has no components section to wire, so the file
// goes with the unit instead of asserting against a trimmed tree.
package components

import (
	"testing"

	"github.com/calionauta/gogogo/config"
)

// TestBuildPageDataWiresStatusAndCatalog pins that the page data carries
// the strip and the answer catalog together: a strip without data, or a
// catalog section without components, is a heading over nothing.
func TestBuildPageDataWiresStatusAndCatalog(t *testing.T) {
	t.Parallel()
	pd := BuildPageData(config.Load())
	if len(pd.Status) == 0 {
		t.Error("PageData carries no status strip")
	}
	if len(pd.AnswerCatalog) == 0 {
		t.Error("PageData carries no answer catalog")
	}
	for _, info := range pd.AnswerCatalog {
		if info.Kind == "native" {
			t.Errorf("AnswerCatalog leaks native panel %q (belongs in AnswerPanels)", info.Name)
		}
	}
}

// TestStatusBadgeClassMapsEveryEmittedTone pins the badge vocabulary: every
// tone StatusSummary emits maps to a real DaisyUI badge, and nothing else
// maps to success — a missing key must read as missing.
func TestStatusBadgeClassMapsEveryEmittedTone(t *testing.T) {
	t.Parallel()
	for _, tone := range []string{"success", "warning", "ghost", "info", "error"} {
		if got := StatusBadgeClass(tone); got == "" {
			t.Errorf("tone %q maps to nothing", tone)
		}
	}
	if got := StatusBadgeClass("bogus"); got != "badge-neutral" {
		t.Errorf("unknown tone maps to %q, want the muted badge", got)
	}
}
