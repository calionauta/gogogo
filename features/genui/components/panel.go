// SCOPE:plugin — system panels: the static catalog entry, plus the optional
// data provider a feature contributes.
package components

import (
	"context"
	"sort"
	"strings"
)

// panelSpec is a system panel the catalog knows: the prompt line that tells
// the model when to use it. Advertisement lives here, statically, so the chat
// prompt and the chat-only rule need no wiring.
//
// The DATA comes from a PanelProvider registered by the feature that owns it.
// That split is the point: a panel with no provider (a trimmed build, or a
// feature that never wired one) is dropped rather than rendered empty (see
// Hydrate), so the two halves cannot disagree about whether it exists.
type panelSpec struct {
	prompt string
}

// panels is the registry: one entry per system panel. Adding one is an entry
// here plus — when it shows stored data — a PanelProvider implementation, and
// nothing else: the chat prompt, the chat-only rule and Ask's router's
// candidate set all read this map.
var panels = map[string]panelSpec{
	todoListName: {
		prompt: "todo_list {filter: " + strings.Join(TodoListFilters, "|") + "} when the " +
			"request is about the user's OWN todos: it shows their real list, which " +
			"invented markup cannot",
	},
}

// PanelProvider fills one system panel's props from the app's own store. It
// lives at the consumer (this package needs rows, the store owns them, and
// nothing here learns what a todo is), and each provider names the component
// it serves, so Hydrate needs no switch when a panel is added.
type PanelProvider interface {
	// Component is the catalog name this provider fills ("todo_list").
	Component() string
	// Fill returns the props for one answer, or an error to drop the panel.
	Fill(ctx context.Context, owner string, props map[string]any) (map[string]any, error)
}

// IsPanel reports whether a catalog name is a registered system panel. Panels
// are chat's affordance: they show the app's own data, and Ask's prompt never
// names them (the router's candidates are derived from what Ask advertises,
// so a panel cannot leak into a classifier pick).
func IsPanel(name string) bool {
	_, ok := panels[name]
	return ok
}

// panelNames is the registry's names, sorted, so the prompt and the guidance
// sentence are stable across runs (map order is not).
func panelNames() []string {
	names := make([]string, 0, len(panels))
	for name := range panels {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
