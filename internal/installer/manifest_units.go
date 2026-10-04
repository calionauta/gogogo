// SCOPE:layer=infra,removal=plugin — installer engine: unit ids, capability map, trim manifest
package installer

// Unit ids, the capability map and the per-unit trim manifest. Split out of
// manifest.go so the data and the machinery that applies it can be read
// separately.

import (
	"github.com/calionauta/gogogo/internal/capabilities"
)

// Unit ids offered by the installer. Most map 1:1 to a capability;
// skins-extra maps to the skins capability (partial removal) and
// whiteboard covers whiteboard+collab (the only honest shape).
const (
	unitDagnats    = "dagnats"
	unitWhiteboard = "whiteboard"
	unitLanding    = "landing"
	unitConfigView = "config-view"
	unitCredits    = "credits"
	unitSounds     = "sounds"
	unitSkinsExtra = "skins-extra"
)

// unitCaps maps installer units to registry capability ids.
var unitCaps = map[string][]string{
	unitDagnats:    {"dagnats"},
	unitWhiteboard: {"whiteboard", "collab"},
	unitLanding:    {"landing"},
	unitConfigView: {"config-view"},
	unitCredits:    {"credits"},
	unitSounds:     {"sounds"},
	unitSkinsExtra: {"skins"},
}

// scaffoldFileMode matches a git checkout: scaffolded repo files are 0644
// tracked sources, not secrets (G306 would prefer 0600).
const scaffoldFileMode = 0o644

// trimUnit is one installer unit: registry capabilities plus the
// mechanical wiring edits that delete them. All metadata (kind, warns,
// owned paths) comes from internal/capabilities via meta(); this struct
// holds only the mechanics.
type trimUnit struct {
	id string
	// mainStrips are applied to cmd/web/main.go in order.
	mainStrips []stripRule
	// desktopStrips are applied to cmd/desktop/main.go in order (desktop is
	// a separate target, but `go mod tidy` at the root still parses it).
	desktopStrips []stripRule
	// desktopDropLines are substrings; any import line containing one is
	// removed from cmd/desktop/main.go after desktopStrips run.
	desktopDropLines []string
	// goModDrops are module paths removed from go.mod (then `go mod tidy`).
	goModDrops []string
	// extraStrips apply stripRules to additional files (navbar links,
	// desktop demo). Missing files are skipped.
	extraStrips []fileStrip
	// extraDrops remove matching lines from additional files (.templ
	// call sites, skin blank imports). Missing files are skipped.
	extraDrops []fileDrop
	// replaces rewrites one substring inside the first matching line of
	// additional files (navbar brand retarget). Missing files skipped.
	replaces []fileReplace
}

// unitMeta aggregates registry metadata across the unit's capabilities.
// Primary capability (caps[0]) provides kind/note/runtimeOff; dirs, files
// and warns union across all caps so the whiteboard bundle reports both.
type unitMeta struct {
	kind       capabilities.Kind
	dirs       []string
	files      []string
	warns      []string
	note       string
	runtimeOff string
}

func (u trimUnit) meta() unitMeta {
	byID := capabilities.ByID()
	m := unitMeta{
		dirs:  []string{},
		files: []string{},
		warns: []string{},
	}
	var warns []string
	var dirs, files []string
	for i, id := range u.caps() {
		c, ok := byID[id]
		if !ok {
			continue
		}
		if i == 0 {
			m.kind = c.Kind
			m.note = c.Note
			m.runtimeOff = c.RuntimeOff
		}
		dirs = append(dirs, c.Dirs...)
		files = append(files, c.Files...)
		warns = append(warns, c.Warns...)
	}
	m.dirs = dirs
	m.files = files
	m.warns = warns
	return m
}

func (u trimUnit) caps() []string {
	if caps, ok := unitCaps[u.id]; ok {
		return caps
	}
	return []string{u.id}
}

// stripRule removes whole lines from startMarker through endMarker inclusive.
// When endIsClosingBrace is set, endMarker is ignored: the strip runs from
// the startMarker line through the next line that equals endBrace exactly
// (used for `if ... {` blocks whose closing brace has stable indentation).
// addBefore/addAfter optionally pin where `gogogo add` re-inserts the
// extracted span. Empty means the path default in addSpans.
type stripRule struct {
	startMarker        string
	endMarker          string
	endIsClosingBrace  bool
	endBrace           string
	addBefore          string
	addAfter           string
	alsoDeleteContains []string
}

// fileStrip applies stripRules to one repo-relative file, skipping the
// file when a sibling unit already deleted it.
type fileStrip struct {
	path  string
	rules []stripRule
}

// fileDrop removes whole lines containing any substr from one
// repo-relative file, skipping the file when it does not exist.
type fileDrop struct {
	path    string
	substrs []string
}

// fileReplace replaces old with new inside the first line containing
// matchSubstr in one repo-relative file.
type fileReplace struct {
	path        string
	matchSubstr string
	old         string
	newStr      string
}

// soundsDropLines are the .templ lines removed from every page layout
// when the sounds plugin is trimmed: component calls plus the Go import.
// data-cuelume-* attributes are intentionally left behind (inert without
// the JS runtime).
var soundsDropLines = []string{
	`@sounds.SoundAssets()`,
	soundsImportSuffix,
}

// soundsToggleLines extends soundsDropLines for the navbar host, the only
// layout rendering the mute toggle.
var soundsToggleLines = []string{
	`@sounds.SoundAssets()`,
	`@sounds.SoundToggle()`,
	soundsImportSuffix,
}

// navbarTempl owns the brand + section links retargeted by trim units.
const navbarTempl = "features/auth/views.templ"

// routerGoFile is the shared route-wiring file. Phase 2 Init is a flat
// list of one call per capability, so trims here are single-line drops.
const routerGoFile = "router/router.go"

// Skin import suffixes dropped from handler dispatch + blank imports when
// skins-extra is trimmed.
const (
	skinBasecoatImport = `web/skins/basecoat"`
	skinMorpheusImport = `web/skins/morpheus"`
	soundsImportSuffix = `features/sounds"`
)

var manifestUnits = []trimUnit{
	{
		id: unitDagnats,
		extraDrops: []fileDrop{
			{
				path:    routerGoFile,
				substrs: []string{"registerOnboarding(app, q, se, broadcaster, todoH, cfg)"},
			},
		},
		mainStrips: []stripRule{
			{
				startMarker: "startDagNats(cfg, pb, todoH)",
				endMarker:   "defer shutdownDagNats()",
				addAfter:    mainCallsAnchor,
			},
			{
				startMarker:       "// WORKAROUND (upstream DagNats v0.0.24 bug)",
				endIsClosingBrace: true,
				endBrace:          "\t}",
				addBefore:         "\t// Phase 2: wire the CRDTStore JetStream transport when the chosen",
			},
		},
		goModDrops: []string{"github.com/danmestas/dagnats"},
	},
	{
		id: unitWhiteboard,
		// Bundle rationale lives in the collab capability entry: one
		// unit is the only honest shape (see internal/capabilities).
		extraDrops: []fileDrop{
			{
				path:    routerGoFile,
				substrs: []string{"registerWhiteboardStack(se, q, cfg)"},
			},
		},
		desktopStrips: []stripRule{
			{
				startMarker: "// Edge sync (Phase C): publish local " +
					"Loro updates on app.sync.<docID>.",
				endIsClosingBrace: true,
				endBrace:          "\t}",
			},
		},
		desktopDropLines: []string{`internal/collab"`, `"context"`, `"time"`},
		// No runtimeOff: the whiteboard has no boot-costly servers, so
		// deletion is the off switch (a flag would be config surface
		// with no benefit).
		extraStrips: []fileStrip{
			{
				path: navbarTempl,
				rules: []stripRule{
					{
						startMarker: `<a href="/whiteboard"`,
						endMarker:   `}>Whiteboard</a>`,
					},
				},
			},
		},
	},
	{
		id: unitLanding,
		extraDrops: []fileDrop{
			{
				path:    routerGoFile,
				substrs: []string{`features/landing"`, "landing.New(cfg).RegisterRoutes(se)"},
			},
		},
		replaces: []fileReplace{
			{
				path:        "features/auth/views.templ",
				matchSubstr: `class="app-nav-brand"`,
				old:         `href="/"`,
				newStr:      `href="/todo"`,
			},
		},
	},
	{
		id: unitConfigView,
		extraDrops: []fileDrop{
			{
				path:    routerGoFile,
				substrs: []string{`features/config"`, "cfgfeature.New(cfg).RegisterRoutes(se)"},
			},
		},
		extraStrips: []fileStrip{
			{
				path: navbarTempl,
				rules: []stripRule{
					{
						startMarker: `<a href="/config"`,
						endMarker:   `}>Config</a>`,
					},
				},
			},
		},
	},
	{
		id: unitCredits,
		extraDrops: []fileDrop{
			{
				path:    routerGoFile,
				substrs: []string{"wireCredits(cfg, se, todoH)"},
			},
		},
		goModDrops: []string{"github.com/calionauta/ai-credits"},
	},
	{
		id: unitSounds,
		extraDrops: []fileDrop{
			{path: navbarTempl, substrs: soundsToggleLines},
			{path: "features/todo/components/layout.templ", substrs: soundsDropLines},
			{path: "features/config/views.templ", substrs: soundsDropLines},
			{path: "features/landing/views.templ", substrs: soundsDropLines},
			{path: "features/whiteboard/components.templ", substrs: soundsDropLines},
		},
	},
	{
		id: unitSkinsExtra,
		extraDrops: []fileDrop{
			{
				path:    "features/todo/components/skin_imports.go",
				substrs: []string{skinBasecoatImport, skinMorpheusImport},
			},
			{
				path:    todoGoFile,
				substrs: []string{skinBasecoatImport, skinMorpheusImport},
			},
			{
				path:    todoRepoFile,
				substrs: []string{skinBasecoatImport, skinMorpheusImport},
			},
		},
		extraStrips: []fileStrip{
			{
				path: todoGoFile,
				rules: []stripRule{
					{
						startMarker:       "\tif skinName == SkinMorpheus {",
						endIsClosingBrace: true,
						endBrace:          "\t}",
					},
					{
						startMarker:       "\tif skinName == SkinBasecoat {",
						endIsClosingBrace: true,
						endBrace:          "\t}",
					},
				},
			},
			{
				path: todoRepoFile,
				rules: []stripRule{
					{
						startMarker: "\tcase SkinMorpheus:",
						endMarker:   "return morpheus.TodoListRegion(signals)",
					},
					{
						startMarker: "\tcase SkinBasecoat:",
						endMarker:   "return basecoat.TodoListRegion(signals)",
					},
				},
			},
		},
	},
}
