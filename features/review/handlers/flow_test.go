// SCOPE:feature — Review flow pure-unit tests (red-proof).
package handlers

import (
	"strings"
	"testing"

	"github.com/calionauta/gogogo/config"
)

// TestTruncateRunes_Boundaries pins the demo guardrail: runes, not
// bytes. Red-proof: slice bytes instead of runes and the multibyte
// case goes red (split UTF-8); drop the cap and the over-long case
// goes red.
func TestTruncateRunes_Boundaries(t *testing.T) {
	if got := truncateRunes("", 10); got != "" {
		t.Errorf("empty = %q, want empty", got)
	}
	if got := truncateRunes("hi", 10); got != "hi" {
		t.Errorf("short = %q, want uncut", got)
	}
	exact := strings.Repeat("a", 10)
	if got := truncateRunes(exact, 10); got != exact {
		t.Errorf("exact-length cut, want uncut")
	}
	if got := truncateRunes(strings.Repeat("b", 11), 10); len([]rune(got)) != 10 {
		t.Errorf("over-long = %d runes, want 10", len([]rune(got)))
	}
	multi := "héllo世界🙂"
	if got := truncateRunes(multi, 5); got != "héllo" {
		t.Errorf("multibyte trunc = %q, want %q", got, "héllo")
	}
	if got := truncateRunes(multi, 100); got != multi {
		t.Errorf("multibyte short = %q, want uncut", got)
	}
}

// TestRolePrompts_UnknownTabFallsBackToText pins the closed-set
// default: only "code" selects the code prompts; anything else
// (empty, typo, future tab) is the text showcase — the safe default,
// since the text loop is what the demo calibrates. Red-proof: default
// the else-branch to the code prompts and this goes red.
func TestRolePrompts_UnknownTabFallsBackToText(t *testing.T) {
	textMaker, textCritic := rolePrompts(TabText)
	for _, tab := range []string{"", "bogus", "TEXT", "code "} {
		maker, critic := rolePrompts(tab)
		if maker != textMaker || critic != textCritic {
			t.Errorf("tab %q did not fall back to the text prompts", tab)
		}
	}
	codeMaker, _ := rolePrompts(TabCode)
	if codeMaker == textMaker {
		t.Error("code prompts must differ from the text prompts")
	}
}

// TestIsApproval_Contract pins the critic contract: approvals start
// with APPROVE (case/space-insensitive prefix). Anything else revises.
// Red-proof: switch to exact equality and the padded/lowercase cases
// go red; switch to Contains and the REVISE case goes red.
func TestIsApproval_Contract(t *testing.T) {
	for _, ok := range []string{"APPROVE", "  approve  ", "Approve: ready", "APPROVE with notes"} {
		if !isApproval(ok) {
			t.Errorf("isApproval(%q) = false, want true", ok)
		}
	}
	for _, no := range []string{"", "REVISE: fix this", "I approve this", "DISAPPROVE"} {
		if isApproval(no) {
			t.Errorf("isApproval(%q) = true, want false", no)
		}
	}
}

// TestRunnerThreshold_DefaultsWhenUnset pins the zero-means-default
// rule so flow tests need no config. Red-proof: return r.Threshold
// directly and the unset cases go red (0.0 auto-accepts nothing and
// escalates everything).
func TestRunnerThreshold_DefaultsWhenUnset(t *testing.T) {
	if got := (&Runner{}).threshold(); got != config.DefaultReviewConfidenceThreshold {
		t.Errorf("zero threshold = %v, want default %v", got, config.DefaultReviewConfidenceThreshold)
	}
	if got := (&Runner{Threshold: -1}).threshold(); got != config.DefaultReviewConfidenceThreshold {
		t.Errorf("negative threshold = %v, want default", got)
	}
	if got := (&Runner{Threshold: 0.9}).threshold(); got != 0.9 {
		t.Errorf("set threshold = %v, want 0.9", got)
	}
}
