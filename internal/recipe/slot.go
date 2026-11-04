// SCOPE:plugin — Spike 5: slots, with declared shapes.
//
// The question: can a recipe render content inside a core page without the core
// page knowing which recipe it is, and without the recipe touching that page's
// logic?
//
// The kill criterion is precise and worth restating, because it is the whole
// design: if filling a slot requires the owning page to know the recipe's ID, the
// abstraction has failed. The types below are arranged so that a page receives a
// list of renderable cells and never a name.
package recipe

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// SlotID names a location in a shell or a page. The set is closed: a page can
// only offer slots it knows how to lay out, because a slot without a container
// contract is where layout rots.
type SlotID string

const (
	// SlotDashboardCard is a cell in the dashboard grid.
	SlotDashboardCard SlotID = "dashboard.card"
	// SlotSettingsSection is a section inside the settings page.
	SlotSettingsSection SlotID = "settings.section"
	// SlotNavSidebar is a destination in the sidebar. An entry, not a fragment.
	SlotNavSidebar SlotID = "nav.sidebar"
	// SlotRecordRowActions is declarative actions on a row. Deliberately NOT a
	// component: it touches another recipe's entity, so its shape must be data.
	SlotRecordRowActions SlotID = "record.row-actions"
)

// Shape is the container contract a slot declares. This is the part that keeps a
// slot from meaning "somewhere in this page": a recipe knows the width it will
// get, and the page knows the width it must reserve.
type Shape string

const (
	// ShapeCell is one grid cell, narrow. Two cells share a row.
	ShapeCell Shape = "cell"
	// ShapeWide is a full-width row in the grid.
	ShapeWide Shape = "wide"
	// ShapeSection is a block inside a stacked section column.
	ShapeSection Shape = "section"
	// ShapeEntries is a list of navigation destinations.
	ShapeEntries Shape = "entries"
	// ShapeActions is a list of declarative row actions.
	ShapeActions Shape = "actions"
)

// ShapeFor is the closed mapping from slot to its container contract. A page
// lays out per slot id, and this table is the single place that says how much
// room a filler gets.
func ShapeFor(s SlotID) (Shape, bool) {
	switch s {
	case SlotDashboardCard:
		return ShapeCell, true
	case SlotSettingsSection:
		return ShapeSection, true
	case SlotNavSidebar:
		return ShapeEntries, true
	case SlotRecordRowActions:
		return ShapeActions, true
	default:
		return "", false
	}
}

// Fragment is what a recipe contributes to a content slot. It carries no recipe
// id: the page renders fragments, and the arrangement is the page's business.
type Fragment struct {
	// Title is shown by the owning page's own chrome, so a fragment never draws
	// its own card frame.
	Title string
	// Render returns the content. Kept as a func so this package does not depend
	// on templ in a spike; the real surface would carry templ.Component.
	Render func() string
}

// Entry is a navigation destination contributed by a recipe.
type Entry struct {
	Label string
	Href  string
}

// Action is a declarative row action. Data, not a component, because it operates
// on another recipe's entity.
type Action struct {
	Label   string
	Href    string
	Verb    string // "get" or "post"
	Confirm string
}

// Contribution is one recipe's declared content for slots. It is the only thing
// the slot registry knows about a recipe.
type Contribution struct {
	Fragments map[SlotID]Fragment
	Entries   map[SlotID][]Entry
	Actions   map[SlotID][]Action
}

// SlotRegistry collects contributions. Registration happens from the same
// generated file that registers providers, so the installed set stays a build
// fact.
type SlotRegistry struct {
	mu   sync.RWMutex
	frag map[SlotID][]Fragment
	ent  map[SlotID][]Entry
	act  map[SlotID][]Action
}

// NewSlotRegistry returns an empty registry.
func NewSlotRegistry() *SlotRegistry {
	return &SlotRegistry{
		frag: map[SlotID][]Fragment{},
		ent:  map[SlotID][]Entry{},
		act:  map[SlotID][]Action{},
	}
}

// Add registers one recipe's contribution. An unknown slot is refused: offering
// content to a slot the shell does not lay out would be silently dropped, which
// is worse than an error at registration.
func (s *SlotRegistry) Add(c Contribution) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for slot := range c.Fragments {
		if _, ok := ShapeFor(slot); !ok {
			return fmt.Errorf("slot %q is not offered by this build", slot)
		}
	}
	for slot := range c.Entries {
		if _, ok := ShapeFor(slot); !ok {
			return fmt.Errorf("slot %q is not offered by this build", slot)
		}
	}
	for slot := range c.Actions {
		if _, ok := ShapeFor(slot); !ok {
			return fmt.Errorf("slot %q is not offered by this build", slot)
		}
	}
	for slot, f := range c.Fragments {
		s.frag[slot] = append(s.frag[slot], f)
	}
	for slot, es := range c.Entries {
		for _, e := range es {
			if e.Label == "" || !isSameOriginPath(e.Href) {
				return fmt.Errorf("slot %q entry %q must carry a label and a root-relative path, got %q",
					slot, e.Label, e.Href)
			}
		}
		s.ent[slot] = append(s.ent[slot], es...)
	}
	for slot, as := range c.Actions {
		for _, a := range as {
			if err := validateAction(slot, a); err != nil {
				return err
			}
		}
		s.act[slot] = append(s.act[slot], as...)
	}
	return nil
}

// isSameOriginPath is the rail's security invariant: an extension destination is
// a path this binary serves — never an absolute URL, never a scheme a browser
// would execute, and never protocol-relative (`//host`), which would leave the
// origin without looking like it.
func isSameOriginPath(href string) bool {
	return strings.HasPrefix(href, "/") && !strings.HasPrefix(href, "//")
}

// RowIDPlaceholder is the one token a row action may substitute: the core page
// replaces it with the row's own id. It is spelled here so the page and the
// validator cannot disagree about the contract.
const RowIDPlaceholder = "{id}"

// validateAction holds a row action to the same origin rule as a nav entry, plus
// one more: the destination is a template with exactly one row placeholder,
// because an action that cannot name its row would act on "some row".
//
// Verbs are a closed set. `get` is for an action that navigates (open the row in
// the recipe's own page); anything that changes state is `post`, so a prefetch
// or a crawler cannot fire it.
func validateAction(slot SlotID, a Action) error {
	if a.Label == "" {
		return fmt.Errorf("slot %q action has no label", slot)
	}
	if a.Verb != "get" && a.Verb != "post" {
		return fmt.Errorf("slot %q action %q has verb %q, want get or post", slot, a.Label, a.Verb)
	}
	if !isSameOriginPath(a.Href) {
		return fmt.Errorf("slot %q action %q must be a root-relative path, got %q", slot, a.Label, a.Href)
	}
	if n := strings.Count(a.Href, RowIDPlaceholder); n != 1 {
		return fmt.Errorf("slot %q action %q must name the row exactly once with %s, got %q",
			slot, a.Label, RowIDPlaceholder, a.Href)
	}
	return nil
}

// Fragments returns the content for a content slot, in stable order. A page calls
// this and renders what it gets; it never asks who contributed.
func (s *SlotRegistry) Fragments(slot SlotID) []Fragment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]Fragment(nil), s.frag[slot]...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out
}

// Entries returns navigation destinations for a slot.
func (s *SlotRegistry) Entries(slot SlotID) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]Entry(nil), s.ent[slot]...)
	// Label order, like Fragments: the rail renders what it is given, so the
	// registry is the only place that can make a boot's order deterministic.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

// Actions returns declarative row actions for a slot.
func (s *SlotRegistry) Actions(slot SlotID) []Action {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Action(nil), s.act[slot]...)
}

// RenderSlot is what an owning page calls. Deliberately the whole contract: a slot
// id in, rendered content out. There is no recipe id in this signature, and that
// absence IS the abstraction.
func (s *SlotRegistry) RenderSlot(slot SlotID, render func(Fragment) string) string {
	var out strings.Builder
	for _, f := range s.Fragments(slot) {
		out.WriteString(render(f))
	}
	return out.String()
}
