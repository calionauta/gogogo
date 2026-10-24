// SCOPE:plugin — lenient conversion tests (partial render + catch-all).
package components

import (
	"strings"
	"testing"
)

// TestConvertLenientRendersPartialEnvelope pins per-directive partial
// rendering: valid components render, undecodable props are dropped with
// their error, and the answer is still usable. Red-proof: fail the whole
// list on one bad component and the valid sibling never renders.
func TestConvertLenientRendersPartialEnvelope(t *testing.T) {
	t.Parallel()
	raw, err := parseStrictRaw(`{"components":[{"type":"text_note","props":{"text":"ok"}},` +
		`{"type":"plan_cards","props":{"plans":"nope"}}]}`)
	if err != nil {
		t.Fatalf("parseStrictRaw: %v", err)
	}
	dirs, errs := ConvertLenient(raw)
	if len(dirs) != 1 || dirs[0].Component != "text_note" {
		t.Fatalf("dirs = %+v, want the one valid card", dirs)
	}
	if DroppedCount(errs) != 1 {
		t.Fatalf("errs = %v, want 1 dropped", errs)
	}
	if !strings.Contains(errs[0].Error(), "plan_cards") {
		t.Fatalf("error %q does not name the failed component path", errs[0])
	}
}

// TestConvertLenientCatchAllUnknownType pins the useDefaultRenderTool
// contract: unknown names become a named text_note placeholder instead of
// an error, so one invented name never sinks the valid siblings.
func TestConvertLenientCatchAllUnknownType(t *testing.T) {
	t.Parallel()
	raw, err := parseStrictRaw(`{"components":[{"type":"mystery","props":{"a":1}},` +
		`{"type":"text_note","props":{"text":"kept"}}]}`)
	if err != nil {
		t.Fatalf("parseStrictRaw: %v", err)
	}
	dirs, errs := ConvertLenient(raw)
	if len(dirs) != 2 {
		t.Fatalf("dirs = %+v, want placeholder + kept note", dirs)
	}
	text, _ := dirs[0].Props["text"].(string)
	if dirs[0].Component != "text_note" || !strings.Contains(text, "mystery") {
		t.Fatalf("placeholder = %+v, want it to name the requested type", dirs[0])
	}
	if DroppedCount(errs) != 0 {
		t.Fatalf("errs = %v, want the catch-all to count as rendered", errs)
	}
}

// TestConvertLenientFailsWithoutAnythingUsable pins the floor: no valid
// components means no answer, and the errors say so.
func TestConvertLenientFailsWithoutAnythingUsable(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"components":[]}`,
		`{"components":[{"type":"plan_cards","props":{"plans":"nope"}}]}`,
		`just prose, no envelope`,
	} {
		raw, rerr := rawComponents(body)
		var dirs []Directive
		var errs []error
		if rerr == nil {
			dirs, errs = ConvertLenient(raw)
		} else {
			errs = []error{rerr}
		}
		if len(dirs) != 0 {
			t.Fatalf("body %q rendered %+v, want nothing", body, dirs)
		}
		if len(errs) == 0 {
			t.Fatalf("body %q reported no error", body)
		}
		if body == "just prose, no envelope" && DroppedCount(errs) != 0 {
			t.Fatalf("prose counted as dropped: %v", errs)
		}
	}
}

// TestParseLenientMatchesStrictOnValidAnswers pins that the two readers
// agree: a valid envelope converts identically through both.
func TestParseLenientMatchesStrictOnValidAnswers(t *testing.T) {
	t.Parallel()
	raw := `{"components":[{"type":"text_note","props":{"text":"hi"}},` +
		`{"type":"diagram","props":{"source":"flowchart TD\n A-->B"}}]}`
	strict, err := ParseDirectives(raw)
	if err != nil {
		t.Fatalf("ParseDirectives: %v", err)
	}
	dirs, errs := ParseLenient(raw)
	if len(errs) != 0 {
		t.Fatalf("ParseLenient errs = %v, want none", errs)
	}
	if len(dirs) != len(strict) {
		t.Fatalf("lenient = %d dirs, strict = %d", len(dirs), len(strict))
	}
	for i := range dirs {
		if dirs[i].Component != strict[i].Component {
			t.Fatalf("dir %d = %s, strict = %s", i, dirs[i].Component, strict[i].Component)
		}
	}
}
