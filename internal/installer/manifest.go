// SCOPE:layer=infra,removal=plugin — installer engine: trim/add unit mechanics (strips, drops, anchors)
package installer

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

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

// planTrim returns the units to DROP given the keep set. Unknown ids in the
// keep set are ignored so a future unit never breaks an old command line.
// Matching is by kind: plugin units read the plugins dimension, feature
// units the features dimension — the two prompt questions stay independent.
func planTrim(keep keepSet) []trimUnit {
	var drop []trimUnit
	for _, u := range manifestUnits {
		switch u.meta().kind {
		case capabilities.KindPlugin:
			if !keep.plugins[u.id] {
				drop = append(drop, u)
			}
		case capabilities.KindFeature:
			if !keep.features[u.id] {
				drop = append(drop, u)
			}
		default:
			// Unknown kind: keep (fail safe — never delete on confusion).
		}
	}
	return drop
}

func dropIDs(units []trimUnit) []string {
	ids := make([]string, 0, len(units))
	for _, u := range units {
		ids = append(ids, u.id)
	}
	return ids
}

// UnitReceipt records what one dropped unit actually changed. Agents use
// it to verify the trim (missed markers = manifest drift) and to resume
// idempotently: re-running on a trimmed tree misses every marker and
// changes nothing.
type UnitReceipt struct {
	ID            string   `json:"id"`
	DirsRemoved   int      `json:"dirsRemoved"`
	FilesRemoved  int      `json:"filesRemoved"`
	StripsApplied int      `json:"stripsApplied"`
	StripsMissed  []string `json:"stripsMissed"`
}

// Receipt aggregates UnitReceipts for one apply run.
type Receipt struct {
	Units []UnitReceipt `json:"units"`
}

// applyTrim deletes dirs/files and strips wiring blocks for each dropped unit.
// It is idempotent: missing paths are skipped, missing markers are skipped
// (and recorded in rc when non-nil). Unknown unit ids fail fast: a typo in
// --plugins must not silently keep everything.
func applyTrim(root string, drop []trimUnit, rc *Receipt) error {
	byID := capabilities.ByID()
	for _, u := range drop {
		for _, id := range u.caps() {
			if _, ok := byID[id]; !ok {
				return &ExitError{code: 1, msg: "unknown capability " + id}
			}
		}
		unit := UnitReceipt{ID: u.id, StripsMissed: []string{}}
		if err := applyUnit(root, u, &unit); err != nil {
			return err
		}
		if rc != nil {
			rc.Units = append(rc.Units, unit)
		}
	}
	return nil
}

func applyUnit(root string, u trimUnit, rc *UnitReceipt) error {
	m := u.meta()
	for _, d := range m.dirs {
		p := filepath.Join(root, filepath.FromSlash(d))
		if _, err := os.Lstat(p); err == nil {
			rc.DirsRemoved++
		}
		_ = os.RemoveAll(p)
	}
	for _, f := range m.files {
		p := filepath.Join(root, filepath.FromSlash(f))
		if _, err := os.Lstat(p); err == nil {
			rc.FilesRemoved++
		}
		_ = os.Remove(p)
	}
	if err := applyWiringStrips(root, u, rc); err != nil {
		return err
	}
	return applyExtraFiles(root, u, rc)
}

// applyWiringStrips removes web-main and desktop wiring blocks.
// router/router.go needs no block strips: Init is a flat list of one call
// per capability, so trimming is a single-line drop via extraDrops.
func applyWiringStrips(root string, u trimUnit, rc *UnitReceipt) error {
	if len(u.mainStrips) > 0 {
		mp := filepath.Join(root, "cmd", "web", "main.go")
		if err := stripFileCounted(mp, u.mainStrips, rc); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if u.id == unitDagnats {
		// startDagNats was the only todoH use in main.go; keep the
		// server.Run shape stable with an explicit blank use.
		mp := filepath.Join(root, "cmd", "web", "main.go")
		_ = insertAfterLine(mp, "\tdefer shutdown()",
			"\t_ = todoH // dagnats removed: handler stays wired via router")
	}
	if len(u.desktopStrips) > 0 {
		dp := filepath.Join(root, "cmd", "desktop", "main.go")
		if err := stripFileCounted(dp, u.desktopStrips, rc); err != nil && !os.IsNotExist(err) {
			return err
		}
		for _, sub := range u.desktopDropLines {
			_ = dropLineContaining(dp, sub)
		}
	}
	return nil
}

// applyExtraFiles handles .templ/navbar/skin edits, go.mod drops, and the
// per-unit extras. Missing files are skipped: a sibling unit may have
// deleted them first.
func applyExtraFiles(root string, u trimUnit, rc *UnitReceipt) error {
	for _, es := range u.extraStrips {
		p := filepath.Join(root, filepath.FromSlash(es.path))
		if err := stripFileCounted(p, es.rules, rc); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for _, ed := range u.extraDrops {
		p := filepath.Join(root, filepath.FromSlash(ed.path))
		for _, sub := range ed.substrs {
			if err := dropLineContaining(p, sub); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	for _, rp := range u.replaces {
		p := filepath.Join(root, filepath.FromSlash(rp.path))
		if err := replaceInLine(p, rp.matchSubstr, rp.old, rp.newStr); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for _, mod := range u.goModDrops {
		_ = dropGoModRequire(filepath.Join(root, "go.mod"), mod)
	}
	return nil
}

// stripFileCounted removes whole lines from each rule's start through end,
// recording per-rule receipts: a rule whose start marker never matches is a
// missed strip (manifest drift), not an error, so re-runs stay idempotent.
func stripFileCounted(path string, rules []stripRule, rc *UnitReceipt) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(raw), "\n")
	kill := make([]bool, len(lines))
	for _, r := range rules {
		if applyStripRule(lines, kill, r) {
			rc.StripsApplied++
		} else {
			rc.StripsMissed = append(rc.StripsMissed, shortPath(path)+": "+r.startMarker)
		}
	}
	kept := lines[:0]
	for i, l := range lines {
		if !kill[i] {
			kept = append(kept, l)
		}
	}
	// NOTE: a dagnats-specific blank use (`_ = todoH`) is inserted by
	// applyTrim (dagnats unit), not here — stripFileCounted stays generic.
	out := strings.Join(kept, "\n")
	//nolint:gosec // G306 scaffolded repo files are 0644 tracked sources, same as a git checkout.
	return os.WriteFile(path, []byte(out), scaffoldFileMode)
}

// shortPath keeps receipts readable: last two path segments at most.
func shortPath(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	return strings.Join(parts, "/")
}

// applyStripRule marks whole lines from each rule start through its end,
// returning true when the rule matched at least one start.
func applyStripRule(lines []string, kill []bool, r stripRule) bool {
	matched := false
	for i := range len(lines) {
		if kill[i] || !strings.Contains(lines[i], r.startMarker) {
			continue
		}
		end := stripEnd(lines, i, r)
		if end == -1 {
			continue
		}
		matched = true
		for j := i; j <= end; j++ {
			kill[j] = true
		}
		for _, extra := range r.alsoDeleteContains {
			for j := range lines {
				if strings.Contains(lines[j], extra) {
					kill[j] = true
				}
			}
		}
	}
	return matched
}

// stripEnd finds the closing line index for a rule starting at from,
// or -1 when the end marker is absent (rule skipped, file untouched).
func stripEnd(lines []string, from int, r stripRule) int {
	if r.endIsClosingBrace {
		for j := from; j < len(lines); j++ {
			if lines[j] == r.endBrace {
				return j
			}
		}
		return -1
	}
	for j := from; j < len(lines); j++ {
		if strings.Contains(lines[j], r.endMarker) {
			return j
		}
	}
	return -1
}

// replaceInLine replaces old with newStr inside the first line containing
// matchSubstr. No-op when no line matches (idempotent).
func replaceInLine(path, matchSubstr, old, newStr string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(raw), "\n")
	for i, l := range lines {
		if strings.Contains(l, matchSubstr) {
			lines[i] = strings.Replace(l, old, newStr, 1)
			break
		}
	}
	//nolint:gosec // G306 scaffolded repo files are 0644 tracked sources.
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), scaffoldFileMode)
}

// insertAfterLine inserts text as a new line directly after the first line
// exactly equal to anchor. No-op when the anchor is missing or the text is
// already present (idempotent).
func insertAfterLine(path, anchor, text string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(raw), "\n")
	if slices.Contains(lines, text) {
		return nil
	}
	for i, l := range lines {
		if l == anchor {
			lines = append(lines[:i+1], append([]string{text}, lines[i+1:]...)...)
			//nolint:gosec // G306 scaffolded repo files are 0644 tracked sources, same as a git checkout.
			return os.WriteFile(path, []byte(strings.Join(lines, "\n")), scaffoldFileMode)
		}
	}
	return nil
}

func dropLineContaining(path, substr string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(raw), "\n")
	kept := lines[:0]
	for _, l := range lines {
		if strings.Contains(l, substr) {
			continue
		}
		kept = append(kept, l)
	}
	//nolint:gosec // G306 scaffolded repo files are 0644 tracked sources, same as a git checkout.
	return os.WriteFile(path, []byte(strings.Join(kept, "\n")), scaffoldFileMode)
}

func dropGoModRequire(goModPath, module string) error {
	raw, err := os.ReadFile(goModPath)
	if err != nil {
		return err
	}
	lines := strings.Split(string(raw), "\n")
	kept := lines[:0]
	for _, l := range lines {
		if isGoModRequireLine(l, module) {
			continue
		}
		kept = append(kept, l)
	}
	//nolint:gosec // G306 scaffolded repo files are 0644 tracked sources, same as a git checkout.
	return os.WriteFile(goModPath, []byte(strings.Join(kept, "\n")), scaffoldFileMode)
}

// isGoModRequireLine reports whether a go.mod line requires the module,
// either as a single-line require or as an entry inside a require block.
func isGoModRequireLine(line, module string) bool {
	if !strings.Contains(line, module) {
		return false
	}
	trimmed := strings.TrimSpace(line)
	return strings.Contains(line, "require") ||
		strings.HasPrefix(trimmed, module) ||
		strings.Contains(line, "\t"+module+" ") ||
		strings.Contains(line, " "+module+" ")
}
