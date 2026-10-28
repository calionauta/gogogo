// SCOPE:plugin — the two answer shapes a live model produced for "what are my
// tasks?", and the one shape that is not recoverable.
//
// Reported transcript: the model answered the user's own-data question with
// `{"components":[{"todo_list":{...}}]}` (the envelope read as a map of names),
// and the chat bubble said "The model asked for an unnamed component here,
// which isn't available." The second sampled shape inlined props beside the
// type. Both are SHAPE slips — the catalog still validates every name and
// field — so they are lifted, never guessed at.
package components

import (
	"strings"
	"testing"
)

// TestParseDirectivesLiftsTypeAsKey pins the recovered shape: the component
// name is the key and its value is the props. Red-proof: delete nameAsKey and
// the parse fails with `unregistered component ""` — the live message.
func TestParseDirectivesLiftsTypeAsKey(t *testing.T) {
	t.Parallel()
	dirs, err := ParseDirectives(`{"components":[{"todo_list":{"filter":"open"}}]}`)
	if err != nil {
		t.Fatalf("ParseDirectives(type as key): %v", err)
	}
	if len(dirs) != 1 || dirs[0].Component != "todo_list" {
		t.Fatalf("dirs = %+v, want one todo_list", dirs)
	}
	if got, _ := dirs[0].Props["filter"].(string); got != "open" {
		t.Fatalf("filter = %q, want open (props were not lifted)", got)
	}
}

// TestParseDirectivesLiftsInlineProps pins the second sampled shape: props
// beside the type instead of under "props". The catalog's strict prop decoder
// still runs, so this recovers the envelope and rejects unknown fields.
func TestParseDirectivesLiftsInlineProps(t *testing.T) {
	t.Parallel()
	dirs, err := ParseDirectives(`{"components":[{"type":"todo_list","filter":"done"}]}`)
	if err != nil {
		t.Fatalf("ParseDirectives(inline props): %v", err)
	}
	if len(dirs) != 1 || dirs[0].Component != "todo_list" {
		t.Fatalf("dirs = %+v, want one todo_list", dirs)
	}
	if got, _ := dirs[0].Props["filter"].(string); got != "done" {
		t.Fatalf("filter = %q, want done", got)
	}
}

// TestParseDirectivesRejectsUnknownNameAsKey is the fail-closed half: lifting
// a name is a spelling fix, not permission to invent a component. An unknown
// key stays unknown, and the parser still refuses the answer.
func TestParseDirectivesRejectsUnknownNameAsKey(t *testing.T) {
	t.Parallel()
	if _, err := ParseDirectives(`{"components":[{"mystery":{"a":1}}]}`); err == nil {
		t.Fatal("an unregistered type-as-key was accepted")
	}
	// A key that is not on the catalog must not be turned into props either:
	// the answer still has no type.
	if _, err := ParseDirectives(`{"components":[{"filter":"open"}]}`); err == nil {
		t.Fatal("a props-only object was accepted as a component")
	}
}

// TestParseLenientRecoversTypeAsKey pins that the lenient front door agrees
// with the strict one: both read the same shape and both still validate.
// Red-proof: leave rawComponents on the plain struct decode and the strict
// reader recovers the answer while the lenient one reports a placeholder.
func TestParseLenientRecoversTypeAsKey(t *testing.T) {
	t.Parallel()
	dirs, errs := ParseLenient(`{"components":[{"todo_list":{"filter":"all"}}]}`)
	if len(errs) != 0 {
		t.Fatalf("ParseLenient errs = %v, want none", errs)
	}
	if len(dirs) != 1 || dirs[0].Component != "todo_list" {
		t.Fatalf("dirs = %+v, want one todo_list", dirs)
	}
}

// TestConvertLenientReportsUnnamedComponent pins the reported bubble out of
// existence: an unnamed directive is a FAILURE to repair, never a text_note
// that says "the model asked for an unnamed component". A named unknown type
// keeps its catch-all placeholder (the existing partial-render contract).
func TestConvertLenientReportsUnnamedComponent(t *testing.T) {
	t.Parallel()
	raw, err := parseStrictRaw(`{"components":[{"props":{"filter":"open"}}]}`)
	if err != nil {
		t.Fatalf("parseStrictRaw: %v", err)
	}
	dirs, errs := ConvertLenient(raw)
	if len(dirs) != 0 {
		t.Fatalf("unnamed component rendered %+v, want nothing", dirs)
	}
	if DroppedCount(errs) != 1 || !strings.Contains(errs[0].Error(), `has no "type"`) {
		t.Fatalf("errs = %v, want one actionable missing-type failure", errs)
	}

	raw, err = parseStrictRaw(`{"components":[{"type":"mystery","props":{"a":1}}]}`)
	if err != nil {
		t.Fatalf("parseStrictRaw: %v", err)
	}
	dirs, errs = ConvertLenient(raw)
	if len(dirs) != 1 || dirs[0].Component != "text_note" {
		t.Fatalf("named unknown = %+v, want the catch-all placeholder", dirs)
	}
	if DroppedCount(errs) != 0 {
		t.Fatalf("named catch-all counted as dropped: %v", errs)
	}
}
