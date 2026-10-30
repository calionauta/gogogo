// SCOPE:plugin — installer engine: unit ids, capability map, trim manifest
package installer

// Unit ids, the capability map and the per-unit trim manifest. Split out of
// manifest.go so the data and the machinery that applies it can be read
// separately.

import (
	"github.com/calionauta/gogogo/internal/capabilities"
)

// Unit ids offered by the installer. Most map 1:1 to a capability;
// whiteboard covers whiteboard+collab+notes (the only honest shape:
// notes is the second collab consumer, so collab cannot trim alone).
const (
	unitDagnats    = "dagnats"
	unitGoAkt      = "goakt"
	unitWhiteboard = "whiteboard"
	unitLanding    = "landing"
	unitConfigView = "config-view"
	unitCredits    = "credits"
	unitSounds     = "sounds"
	unitGenUI      = "genui"
	unitReview     = "review"
	unitSearch     = "search"
)

// unitCaps maps installer units to registry capability ids.
var unitCaps = map[string][]string{
	unitDagnats:    {"dagnats"},
	unitGoAkt:      {"goakt", "room"},
	unitWhiteboard: {"whiteboard", "collab", "notes"},
	unitLanding:    {"landing"},
	unitConfigView: {"config-view"},
	unitCredits:    {"credits"},
	unitSounds:     {"sounds"},
	unitGenUI:      {"genui", "chat"},
	// Bundle rationale mirrors whiteboard+collab: genkit is the review
	// loop's only consumer, so one unit is the honest shape. genkit owns
	// no dirs (external dep); trimming the unit plus `go mod tidy`
	// drops it from the build.
	unitReview: {"review", "genkit"},
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
	// call sites). Missing files are skipped.
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
	scaffold   capabilities.Scaffold
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
			m.scaffold = c.Class()
			m.note = c.Note
			m.runtimeOff = c.RuntimeOff
		}
		dirs = append(dirs, c.Dirs...)
		files = append(files, c.Files...)
		warns = append(warns, c.Warns...)
	}
	m.dirs = dirs
	m.files = files
	if warns == nil {
		warns = []string{}
	}
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

// soundsPageLines is for layouts that carry the assets (in their head templ)
// plus the mute control. The control sits inside a one-line templ wrapper on
// purpose — dropping that line leaves an empty wrapper that still satisfies the
// shell's slot, whereas passing the call inline would put it on the same line as
// the Shell invocation and dropping that line would break the call.
//
// The conformance check verifies the call is really in the file, so a page
// that drops it without touching the manifest fails CI instead of shipping a
// trim that misses its line.
func soundsPageLines() []string {
	return []string{`sounds.SoundAssets()`, soundsPanelControl, soundsImportSuffix}
}

const soundsPanelControl = `sounds.SoundSwitch()` // the shell's mute control

// modulesFile is the shell's navigation registry: one line per module, which
// is what trim units drop when they remove a destination. The brand lives in
// shellTempl, and that is what the landing unit retargets.
const (
	modulesFile = "internal/ui/modules.go"
	shellTempl  = "internal/ui/shell.templ"
)

// chiGoFile is the shared route-wiring file: BootChi composes one handler per
// capability and buildChiMux mounts one route subtree per capability,
// so trims here are line drops (both files live in this one source file).
const chiGoFile = "internal/server/chi.go"

// bootHelpersFile holds the BootChi helpers (seed, RBAC, LLM, review)
// that chi.go would otherwise outgrow: the file-size gate caps every
// .go file at 500 lines, so wiring helpers live here.
const bootHelpersFile = "internal/server/boot_helpers.go"

// Repeated trim targets, hoisted so a typo in a path is a compile error
// rather than a trim that silently matches nothing.
const (
	// chiTestFile is the composition test every unit edits instead of
	// deleting: it builds one handler per feature, so trimming any of
	// them means editing its imports and call sites.
	chiTestFile = "internal/server/chi_test.go"
	// creditsCallLine is the inlined credits construction both composition
	// call sites share, closing paren included (a drop would take it).
	creditsCallLine = "creditshandlers.NewHandler(a, nil))"
)

// chiMuxAnchor ends buildChiMux; capability route lines land above it.
const chiMuxAnchor = "\treturn r"

// Import suffixes dropped from handler dispatch when their unit trims.
const (
	soundsImportSuffix = `features/sounds"`
	// reviewImportSuffix is the review feature import dropped from every
	// composition file wiring the handler (route root, boot helper,
	// composition test): one const so the repeated literal is not a
	// goconst finding, mirroring soundsImportSuffix.
	reviewImportSuffix = `features/review/handlers"`
)

// manifestUnits is every offered unit in canonical menu order. The order is
// load-bearing: TestUnitMenusFollowRegistryKinds pins it, and the file
// split (dagNats / feature / base) is by size, not by domain — so the order
// is reconstructed here explicitly rather than by concatenating the slices.
var manifestUnits = buildManifestUnits()

func buildManifestUnits() []trimUnit {
	byID := make(map[string]trimUnit)
	for _, u := range dagNatsTrimUnit {
		byID[u.id] = u
	}
	for _, u := range featureTrimUnits {
		byID[u.id] = u
	}
	for _, u := range genuiTrimUnits {
		byID[u.id] = u
	}
	for _, u := range searchTrimUnit {
		byID[u.id] = u
	}
	for _, u := range baseTrimUnits {
		byID[u.id] = u
	}
	order := []string{
		unitDagnats, unitGoAkt, unitWhiteboard, unitLanding,
		unitConfigView, unitCredits, unitSounds,
		unitGenUI, unitReview, unitSearch,
	}
	out := make([]trimUnit, 0, len(order))
	for _, id := range order {
		u, ok := byID[id]
		if !ok {
			panic("installer: manifest unit missing: " + id)
		}
		out = append(out, u)
	}
	return out
}

var baseTrimUnits = []trimUnit{
	{
		id: unitGoAkt,
		// Bundle rationale, same as whiteboard+collab: the room demo
		// without the engine does not compile, and the engine without
		// the demo is unproven surface — one unit is the honest shape.
		extraDrops: []fileDrop{
			{
				path: chiGoFile,
				substrs: []string{
					"roomH, roomHost := bootRoomActor(bootCtx, cfg, a, hub)",
					// One argument per line: each drop removes exactly its
					// feature's line, and add restores them.
					"roomH *roomhandlers.Handler,",
					"roomH,",
					`r.MountFeature("room", roomH.Routes())`,
					`appgoakt "github.com/calionauta/gogogo/internal/goakt"`,
					`roomhandlers "github.com/calionauta/gogogo/features/room/handlers"`,
				},
			},
			{
				// The server test's room call sites go as line drops; the
				// multi-line fakeRooms fixture goes as a block strip below.
				// The mux table's "/room" element goes too (one element per
				// line, so combined trims stay order-independent).
				path: chiTestFile,
				substrs: []string{
					`appgoakt "github.com/calionauta/gogogo/internal/goakt"`,
					`roomhandlers "github.com/calionauta/gogogo/features/room/handlers"`,
					"nil, // roomHandler: engine disabled",
					"roomhandlers.NewHandler(cfg, a, fakeRooms{}, queue.NewSSEHub()),",
					"\t\t\"/room\",",
				},
			},
		},

		mainStrips: []stripRule{
			{
				startMarker: "startGoAkt(context.Background(), cfg)",
				endMarker:   "defer shutdownGoAkt()",
				addAfter:    mainCallsAnchor,
				alsoDeleteContains: []string{
					"// GoAkt owns the room actor system (standalone, no network).",
					"// always compiled; when GOAKT_ENABLED=false it no-ops.",
					"// NOTE: intentionally NOT lifecycleCtx-bound (unlike DagNats): the",
					"// standalone boot performs no retries and returns promptly, so there",
					"// is no loop for shutdown to bound — and a self-contained span keeps",
					"// `add goakt` position-independent (any anchor works).",
				},
			},
		},
		goModDrops: []string{"github.com/tochemey/goakt/v4"},
		extraStrips: []fileStrip{
			{
				path:  modulesFile,
				rules: []stripRule{{startMarker: `{ID: "room"`, endMarker: "},"}},
			},
			{
				// The shutdown hook block and the two actor helpers:
				// dropping an `if` line alone would leave the closing brace
				// behind, so each goes as a block. The feature mount itself
				// is a single line in extraDrops.
				path: chiGoFile,
				rules: []stripRule{
					{
						startMarker:       "if roomHost != nil {",
						endIsClosingBrace: true,
						endBrace:          "\t}",
					},
					{
						startMarker:       "// bootRoomActor starts the GoAkt actor system",
						endIsClosingBrace: true,
						endBrace:          "}",
					},
					{
						startMarker:       "// stopRoomActor is the shutdown hook",
						endIsClosingBrace: true,
						endBrace:          "}",
					},
				},
			},
			{
				// The multi-line fakeRooms fixture: line drops would orphan
				// the method bodies.
				path: chiTestFile,
				rules: []stripRule{
					{
						startMarker: "// fakeRooms is the deterministic grain stand-in",
						endMarker:   "func (fakeRooms) Tell(_ context.Context, _ string, _ any) error { return nil }",
					},
				},
			},
		},
	},
	{
		id: unitSounds,
		extraDrops: []fileDrop{
			{path: "features/todo/components/layout.templ", substrs: soundsPageLines()},
			{path: "features/config/components/page.templ", substrs: soundsPageLines()},
			{path: "features/landing/components/index.templ", substrs: soundsPageLines()},
			{path: "features/whiteboard/components/board.templ", substrs: soundsPageLines()},
			{path: "features/notes/components/notes.templ", substrs: soundsPageLines()},
			{path: "features/chat/components/chat.templ", substrs: soundsPageLines()},
			{path: "features/review/components/review.templ", substrs: soundsPageLines()},
			{path: "features/notifications/components/page.templ", substrs: soundsPageLines()},
			{path: "features/account/components/page.templ", substrs: soundsPageLines()},
			{path: "features/authviews/login.templ", substrs: soundsDropLines},
			{path: "features/authviews/signup.templ", substrs: soundsDropLines},
		},
	},
}
