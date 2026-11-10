// Capability registry: single source
// of truth for ids, kinds, runtime switches, and owned paths (consumed by
// cmd/gogogo, docs, and --help).
package capabilities

// Kind follows the servant principle, not the directory: a plugin serves
// other capabilities (nothing user-facing disappears, but something stops
// working — jobs, metering, sounds); a feature is a terminal user
// surface (a page or journey disappears — todo, whiteboard, landing,
// config). Directories do not decide kinds (features/credits is a plugin:
// it serves todo metering, not users directly).
type Kind string

const (
	KindCore    Kind = "core"
	KindPlugin  Kind = "plugin"
	KindFeature Kind = "feature"
)

// Capability is one removable-or-toggleable unit of the template.
// Dirs and Files are repo-relative paths owned by the capability; the
// conformance test fails when a package under internal/ or features/ is
// owned by nobody, and cmd/gogogo refuses unknown ids.
type Capability struct {
	ID         string   `json:"id"`
	Kind       Kind     `json:"kind"`
	Summary    string   `json:"summary"`
	RuntimeOff string   `json:"runtimeOff,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	DependsOn  []string `json:"dependsOn,omitempty"`
	// Recommended marks this recipe as advised at scaffold time. The
	// installer recommends it in the plan and in `advise`; installation
	// still requires explicit acceptance (--install-recommended or --install).
	// The catalog maintainer flips this over time; bundled recipes are
	// always shipped in the default set (they are the template), so
	// Recommended is an advisory, not a packaging switch.
	Recommended bool `json:"recommended,omitempty"`
	// Actions lists the invocable verb names the unit's Provider declares
	// (recipe.Declaration.Actions). Why it is data here: discovery is a
	// checkout-level read (installer + MCP) that must not link feature
	// handlers. It mirrors the Declaration by name only — routes stay in
	// the Declaration, and the server gate asserts the two agree.
	Actions []string `json:"actions,omitempty"`
	// ActionParams mirrors the closed body-param sets per verb, so discovery
	// shows what an agent may forward without reading the handler.
	// Gated against the Declaration the same way Actions is.
	ActionParams map[string][]string `json:"actionParams,omitempty"`
	// ActionPathParams mirrors the declared route placeholders per verb
	// (v1: only `id`, a UUID). Separate from ActionParams because the
	// dispatcher moves it into the URL instead of the form body.
	ActionPathParams map[string][]string `json:"actionPathParams,omitempty"`
	// UISignal names the todo.Signals bool field that hides this
	// capability's UI ("" when the capability has no gated UI). The
	// conformance test asserts the field exists and at least one .templ
	// reads it: no dead buttons after trim, by construction.
	UISignal string   `json:"uiSignal,omitempty"`
	Dirs     []string `json:"dirs,omitempty"`
	Files    []string `json:"files,omitempty"`
	// Register names the repo-relative package exporting
	// `Register(reg *recipe.Registry, deps register.Deps)`. A unit that sets it
	// mounts itself: the generated registration in internal/server calls it at
	// boot, so the composition root never names the unit's constructor. Empty
	// means the root still builds the unit itself (the path that a unit takes
	// until it is converted).
	//
	// Why this is data on the capability rather than a line in chi.go: the root
	// holding a unit's constructor is what makes the unit unremovable, and two
	// places knowing what a unit needs is how the deps of a removed unit get
	// "fixed" by hand.
	Register string   `json:"register,omitempty"`
	Warns    []string `json:"warnings,omitempty"`
	Note     string   `json:"note,omitempty"`
}

// depSounds is the shared soft dependency of UI pages: their .templ
// files import the sounds package for SoundAssets.
const depSounds = "sounds"

// depCollab is the shared transport dependency of collab consumers
// (whiteboard, notes): the server-owned Loro DocStore + sync workers.
const depCollab = "collab"

// ByID indexes All by capability id.
func ByID() map[string]Capability {
	m := make(map[string]Capability, len(All))
	for _, c := range All {
		m[c.ID] = c
	}
	return m
}
