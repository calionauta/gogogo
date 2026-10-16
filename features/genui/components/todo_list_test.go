// SCOPE:plugin — the app's own panel component (system component) rules.
package components

import (
	"slices"
	"testing"
)

// slicesContain keeps the assertion readable without pulling in a helper
// package for one call.
func slicesContain(haystack []string, needle string) bool {
	return slices.Contains(haystack, needle)
}

// TestTodoListValidatesTheFilter pins the closed set: the model chooses
// WHICH slice to show, and anything outside the enum fails closed, so a bad
// filter is a corrected answer rather than a query the store cannot answer.
func TestTodoListValidatesTheFilter(t *testing.T) {
	t.Parallel()
	for _, filter := range todoListFilters {
		raw := `{"components":[{"type":"todo_list","props":{"filter":"` + filter + `"}}]}`
		dirs, err := ParseDirectives(raw)
		if err != nil {
			t.Fatalf("filter %q rejected: %v", filter, err)
		}
		if len(dirs) != 1 || dirs[0].Component != todoListName {
			t.Fatalf("filter %q parsed to %+v", filter, dirs)
		}
	}
	raw := `{"components":[{"type":"todo_list","props":{"filter":"mine"}}]}`
	if _, err := ParseDirectives(raw); err == nil {
		t.Error("an unknown filter parsed, want it rejected")
	}
}

// TestTodoListRejectsUnknownProps is the point of a system component: the
// panel shows the APP's state, so the model picks a slice and nothing else.
// Rows are accepted ONLY because hydration fills them (TodoListProps) — a
// field the render shape does not name is rejected outright, so an answer
// that tries to supply its own shape fails closed instead of rendering.
func TestTodoListRejectsUnknownProps(t *testing.T) {
	t.Parallel()
	hydrated := `{"components":[{"type":"todo_list","props":{"filter":"open",` +
		`"rows":[{"id":"1","title":"real row from the store","done":false}]}}]}`
	if _, err := ParseDirectives(hydrated); err != nil {
		t.Fatalf("the hydrated shape was rejected: %v", err)
	}
	invented := `{"components":[{"type":"todo_list","props":{"filter":"open",` +
		`"todos":["invented by the model"]}}]}`
	if _, err := ParseDirectives(invented); err == nil {
		t.Error("a prop outside the render shape parsed, want it rejected")
	}
}

// TestChatOnlyComponentsStayOutOfAsk pins the advertisement boundary: the
// escape hatch and the app's panels are chat's alone, so Ask's router can
// never offer a label Ask's own prompt does not name.
func TestChatOnlyComponentsStayOutOfAsk(t *testing.T) {
	t.Parallel()
	for _, name := range []string{htmlBlockName, todoListName} {
		if !chatOnlyComponents(name) {
			t.Errorf("%s is not marked chat-only", name)
		}
		if got := CatalogComponents(); slicesContain(got, name) {
			t.Errorf("Ask's router offers chat-only %s: %v", name, got)
		}
		if got := PromptForCatalogLead(name); got != PromptForCatalog() {
			t.Errorf("Ask's prompt changed for chat-only %s", name)
		}
	}
}
