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

// Capability is one removable-or-toggleable unit of the template.
// Dirs and Files are repo-relative paths owned by the capability; the
// conformance test fails when a package under internal/ or features/ is
// owned by nobody, and cmd/gogogo refuses unknown ids.
type Capability struct {
	ID         string   `json:"id"`
	Kind       Kind     `json:"kind"`
	Summary    string   `json:"summary"`
	RuntimeOff string   `json:"runtimeOff,omitempty"`
	Offered    bool     `json:"offered"`
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

// OfferedIDs returns the ids the installer may delete, in registry order.
func OfferedIDs() []string {
	var ids []string
	for _, c := range All {
		if c.Offered {
			ids = append(ids, c.ID)
		}
	}
	return ids
}
