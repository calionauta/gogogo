// SCOPE:plugin — the action catalog: what a model-authored component may offer
// to DO in this app.
//
// WHY THIS EXISTS. plan_cards used to carry a hardcoded `@post('/api/todos')`.
// Making the action a validated prop removed the feature knowledge from the
// shared leaf, but it left a hole: the model has to write a URL, and it cannot
// know which routes exist. Told nothing, it invents a plausible path
// (`/api/todos/add`, `/todos/create`) and the button 404s — a regression
// dressed as progress, and worse than the hardcode it replaced.
//
// So the features DECLARE their actions and the prompt advertises them. That
// keeps knowledge with the owner (todo knows its own route) while giving the
// model a closed, correct list, and it makes the failure mode visible: an
// action nobody declared cannot be emitted, because the prompt never mentions
// it and validation rejects it (see checkDeclaredAction).
//
// The seam is optional and additive, exactly like PanelSummarizer: a feature
// with actions implements ActionProvider, and a feature without one contributes
// nothing. Nothing in this package knows what a todo is.
package components

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ActionSpec is one action a feature offers to model-authored components.
//
// It is deliberately the same SHAPE as the Action the model emits (label,
// method, url, fields): one vocabulary means the prompt can print the spec and
// the validator can compare the two field by field, instead of translating
// between two near-identical structs.
type ActionSpec struct {
	// Label is the button text the model should use.
	Label string
	// Method is POST or GET. Same-origin only, like every action.
	Method string
	// URL is the literal path. Declared by the feature, never by the model.
	URL string
	// Fields names the payload keys, with a short description of each. The
	// DESCRIPTION is for the model (what to put in the field); the KEY is what
	// validation checks a model-supplied action against.
	Fields map[string]string
	// When is the one-line instruction for picking this action, in the same
	// spirit as PanelProvider.Prompt: the only place the model learns the
	// action exists, so it names the request shape it answers.
	When string
}

// ActionProvider is the OPTIONAL half of a panel's contract: a feature that
// offers model-authored components something to DO implements the method.
//
// Optional on purpose, and asserted rather than required — the same mechanism
// as PanelSummarizer. A feature that only renders data has no actions, and
// making the method mandatory would force every provider to write an empty one.
type ActionProvider interface {
	// Actions returns this feature's offerable actions. An empty slice is
	// valid (the feature has nothing to offer) and contributes no prompt text.
	Actions() []ActionSpec
}

// actionRegistry holds the declared actions, keyed by URL.
//
// Keyed by URL because that is what identifies an action for validation: the
// model may paraphrase the label (it is a display string) but the URL is the
// thing that gets called, so a mismatch there is the one that matters. Two
// features declaring the same URL is a COLLISION (below) rather than a merge:
// silently keeping one would make the other's fields unvalidatable.
type actionRegistry struct {
	mu        sync.RWMutex
	byURL     map[string]ActionSpec
	installed bool
}

var actions = &actionRegistry{}

// InstallActionRegistry replaces the declared actions and validates them.
//
// Process-scope and install-once, like InstallPanelRegistry: the set is a boot
// decision, and the concurrency story is the same one lock.
func InstallActionRegistry(providers []ActionProvider) error {
	specs, err := validateActions(providers)
	if err != nil {
		return err
	}
	byURL := make(map[string]ActionSpec, len(specs))
	for _, spec := range specs {
		byURL[spec.URL] = spec
	}
	actions.mu.Lock()
	defer actions.mu.Unlock()
	actions.byURL = byURL
	actions.installed = true
	return nil
}

// validateActions flattens the providers and checks every spec.
//
// Split from the install so the invariants are testable without mutating
// process state — the same reason validatePanelProviders is split, and it is
// what lets the guard tests stay read-only.
func validateActions(providers []ActionProvider) ([]ActionSpec, error) {
	var specs []ActionSpec
	seen := make(map[string]string, len(providers))
	for _, p := range providers {
		for _, spec := range p.Actions() {
			if err := validateActionSpec(spec); err != nil {
				return nil, fmt.Errorf("genui: action %q: %w", spec.URL, err)
			}
			if owner, dup := seen[spec.URL]; dup {
				return nil, fmt.Errorf("genui: %q declares action %q already declared by %q",
					fmt.Sprintf("%T", p), spec.URL, owner)
			}
			seen[spec.URL] = fmt.Sprintf("%T", p)
			specs = append(specs, spec)
		}
	}
	// Sorted for a stable prompt: map iteration would reshuffle the advertised
	// action list between boots, which reads as the vocabulary changing.
	sort.Slice(specs, func(i, j int) bool { return specs[i].URL < specs[j].URL })
	return specs, nil
}

// validateActionSpec checks one declaration. A declaration is trusted code, so
// this is a boot-time assertion (fail the boot, loudly) rather than a
// per-request check — but it applies the SAME rules the model's action must
// satisfy, which is what keeps the two from drifting apart.
func validateActionSpec(spec ActionSpec) error {
	if strings.TrimSpace(spec.Label) == "" {
		return errors.New("empty label")
	}
	if strings.TrimSpace(spec.When) == "" {
		return errors.New("empty When (the model needs to know when to offer it)")
	}
	return checkAction(Action{Label: spec.Label, Method: spec.Method, URL: spec.URL})
}

// DeclaredActions returns the declared actions, sorted by URL.
func DeclaredActions() []ActionSpec {
	actions.mu.RLock()
	defer actions.mu.RUnlock()
	out := make([]ActionSpec, 0, len(actions.byURL))
	for _, spec := range actions.byURL {
		out = append(out, spec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URL < out[j].URL })
	return out
}

// checkDeclaredAction rejects a model action that does not match a declaration
// exactly, field for field.
//
// This is the substitution that makes the prop approach safe. Without it a
// model could aim a card's button at ANY same-origin path — `checkAction` alone
// permits `/api/todos/delete-all` if such a route existed — and the card would
// become a way for an answer to trigger an endpoint the feature never offered
// to the model. Requiring the URL to be declared, and the field KEYS to be
// exactly the declared ones, makes the model's freedom be "which of the offered
// actions, with what content" instead of "which endpoint".
//
// Extra or missing fields are rejected rather than ignored: a missing field is
// a request the handler would treat as absent (silently doing something other
// than what the card says), and an extra one is the model trying to set
// something the declaration never exposed as model-writable.
func checkDeclaredAction(a Action) error {
	actions.mu.RLock()
	spec, ok := actions.byURL[a.URL]
	actions.mu.RUnlock()
	if !ok {
		return fmt.Errorf("genui: %q is not an action this app offers", a.URL)
	}
	if a.Method != spec.Method {
		return fmt.Errorf("genui: action %q must use %s, not %s", a.URL, spec.Method, a.Method)
	}
	if len(a.Fields) != len(spec.Fields) {
		return fmt.Errorf("genui: action %q takes exactly %d field(s) (%s), got %d",
			a.URL, len(spec.Fields), fieldKeys(spec.Fields), len(a.Fields))
	}
	for key := range a.Fields {
		if _, ok := spec.Fields[key]; !ok {
			return fmt.Errorf("genui: action %q has no field %q (it takes %s)",
				a.URL, key, fieldKeys(spec.Fields))
		}
	}
	return nil
}

// fieldKeys renders a field map's keys for an error message, sorted.
func fieldKeys(fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// actionsFragment is the prompt section advertising the declared actions, or ""
// when none are installed.
//
// Printed in the same shape the model must emit, so "what exists" and "what to
// write" are one reading. The method is included because a model that guesses
// GET for a POST declaration gets a rejected answer (see checkDeclaredAction),
// and the cheapest way not to guess is to be told.
func actionsFragment() string {
	specs := DeclaredActions()
	if len(specs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Actions you may attach to a component (an action's url, method " +
		"and field NAMES must match a row below exactly; put your own content in the fields): ")
	for i, spec := range specs {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(spec.When)
		b.WriteString(" -> ")
		fmt.Fprintf(&b, "{label:%q, method:%s, url:%s, fields:{%s}}",
			spec.Label, spec.Method, spec.URL, fieldHint(spec.Fields))
	}
	b.WriteString(". ")
	return b.String()
}

// fieldHint renders a declaration's fields as the prompt shows them: the key
// with its description, which doubles as the instruction for what to put there.
func fieldHint(fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if desc := strings.TrimSpace(fields[k]); desc != "" {
			parts = append(parts, k+": "+desc)
			continue
		}
		parts = append(parts, k)
	}
	return strings.Join(parts, ", ")
}

// actionAllowed reports whether the action surface may be advertised at all.
// Tied to the catalog band: actions only appear on catalog components, so an
// answer with no catalog cannot carry one.
func actionAllowed(surfaces []Surface) bool {
	return HasSurfaces(surfaces, SurfaceCatalog) && len(DeclaredActions()) > 0
}

// actionsFragmentIfAllowed gates the actions advert on the surface set, so a
// leg configured without the catalog is never told about actions it cannot
// attach to anything.
func actionsFragmentIfAllowed(surfaces []Surface) string {
	if !actionAllowed(surfaces) {
		return ""
	}
	return actionsFragment()
}
