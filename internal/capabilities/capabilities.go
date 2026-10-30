// SCOPE:plugin — Capability registry: single source
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

// Scaffold is the install-time choice axis: what the scaffold prompt offers
// and what it keeps by default. It is deliberately separate from Kind
// (removal class): a plugin can be default, opt-in or manual.
type Scaffold string

const (
	// ScaffoldCore is always in the tree and never offered.
	ScaffoldCore Scaffold = "core"
	// ScaffoldDefault is offered and kept unless the user drops it.
	ScaffoldDefault Scaffold = "default"
	// ScaffoldOptIn is offered and dropped unless the user names it.
	ScaffoldOptIn Scaffold = "opt-in"
	// ScaffoldManual is not offered: its removal is a hand edit, recorded in
	// Reason. A zero Scaffold means manual for non-core units.
	ScaffoldManual Scaffold = "manual"
)

// Offered reports whether the scaffold prompt may keep or drop this class.
func (s Scaffold) Offered() bool {
	return s == ScaffoldDefault || s == ScaffoldOptIn
}

// Capability is one removable-or-toggleable unit of the template.
// Dirs and Files are repo-relative paths owned by the capability; the
// conformance test fails when a package under internal/ or features/ is
// owned by nobody, and cmd/gogogo refuses unknown ids.
type Capability struct {
	ID         string   `json:"id"`
	Kind       Kind     `json:"kind"`
	Summary    string   `json:"summary"`
	RuntimeOff string   `json:"runtimeOff,omitempty"`
	Scaffold   Scaffold `json:"scaffold,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	DependsOn  []string `json:"dependsOn,omitempty"`
	// UISignal names the todo.Signals bool field that hides this
	// capability's UI ("" when the capability has no gated UI). The
	// conformance test asserts the field exists and at least one .templ
	// reads it: no dead buttons after trim, by construction.
	UISignal string   `json:"uiSignal,omitempty"`
	Dirs     []string `json:"dirs,omitempty"`
	Files    []string `json:"files,omitempty"`
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

// Class resolves the scaffold class. Core wins over the field (a core unit is
// never offered, even if a stray Scaffold says otherwise), and a non-core
// unit without a class is manual.
func (c Capability) Class() Scaffold {
	if c.Kind == KindCore {
		return ScaffoldCore
	}
	if c.Scaffold == "" {
		return ScaffoldManual
	}
	return c.Scaffold
}

// OfferedIDs returns the ids the installer may keep or drop, in registry
// order. Bundled capabilities (room, collab, notes, chat) appear here because
// their unit removes them too; the installer maps ids to its own units.
func OfferedIDs() []string {
	var ids []string
	for _, c := range All {
		if c.Class().Offered() {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// IDsByScaffold returns the capability ids in one scaffold class, in registry
// order. The installer uses it for the default and opt-in prompt groups.
func IDsByScaffold(s Scaffold) []string {
	var ids []string
	for _, c := range All {
		if c.Class() == s {
			ids = append(ids, c.ID)
		}
	}
	return ids
}
