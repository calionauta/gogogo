// SCOPE:plugin — installer engine: named trim presets (executable keep-sets)
package installer

import (
	"fmt"
	"slices"
	"strings"

	"github.com/calionauta/gogogo/internal/capabilities"
)

// trimPresets are named compositions over the existing planTrim(keep) API:
// curated DROP lists (unit ids from manifest_units.go) for the shapes
// users actually ask for. A preset is data only — no new machinery; the
// trim build gate (trim_build_test.go) covers whatever a preset drops.
//
// How to use: presetPlan(name) returns the same []trimUnit planTrim would,
// so `applyTrim(dir, presetPlan("minimal"), nil)` is a minimal tree.
//
// How to maintain (LLMs: read this when adding a unit):
//  1. Adding a manifest unit? Decide per preset below whether it drops.
//     TestTrimPresetsCoverNewUnits FAILS until the new unit is dropped by
//     at least one preset — that failure is the sync mechanism, not a bug.
//  2. Renaming/removing a unit? TestTrimPresetsReferenceKnownUnits fails
//     naming the stale id — update the preset lists.
//  3. The drop lists name installer units (unitDagnats, …), never raw
//     capability ids — except the whiteboard bundle, which already covers
//     whiteboard+collab+notes as one honest shape (see unitCaps).
//
// Trim preset names. Consts, not literals: the names are referenced from
// the manifest, the CLI/MCP surface, advise copy and tests — a typo in any
// one of those silently desyncs the preset (goconst enforces it).
const (
	presetMinimal  = "minimal"
	presetNoAI     = "no-ai"
	presetNoCollab = "no-collab"
)

var trimPresets = map[string]trimPreset{
	presetMinimal: {
		Idea: "core + todo + auth: the smallest tree that still serves " +
			"pages with sessions, queue and router.",
		Drop: []string{
			unitDagnats, unitGoAkt, unitWhiteboard, unitLanding,
			unitConfigView, unitCredits, unitSounds, unitSkinsExtra,
			unitGenUI, unitReview, unitSearch,
		},
		Note: "GET / 404s (landing owned the only root route); the navbar " +
			"brand retargets to /todo. Todo stays as the reference " +
			"implementation — delete features/todo/ manually when done reading it.",
	},
	presetNoAI: {
		Idea: "everything except model surface: drop the credits ledger " +
			"plus the Ask + chat + review demos.",
		Drop: []string{unitCredits, unitGenUI, unitReview, unitSearch},
		Note: "Todo AI Suggest still works with GOAI_API_KEY, unmetered. " +
			"The LLM client itself is runtime (unset GOAI_API_KEY hides the " +
			"button) — the preset deletes the demo surfaces, not the client.",
	},
	presetNoCollab: {
		Idea: "no CRDT canvas/docs: drop the whiteboard bundle.",
		Drop: []string{unitWhiteboard},
		Note: "Removes whiteboard+collab+notes (the only honest shape: " +
			"notes is the second collab consumer, so collab cannot trim " +
			"alone). Collab is NOT realtime: the SSE record streams, the SSE " +
			"hub and NATS cross-instance broadcast stay — only the Loro CRDT " +
			"transport (DocStore, sync workers, presence) goes. " +
			"Realtime and offline-sync have no preset on purpose: realtime " +
			"is runtime switches (NATS_ENABLED=false), offline-sync is " +
			"OFFLINE_SYNC_ENABLED=false — flags, not deletion.",
	},
}

// trimPreset is one named composition: what to drop and why.
type trimPreset struct {
	// Idea is one line for --help and plan output.
	Idea string
	// Drop lists installer unit ids (manifest_units.go constants).
	Drop []string
	// Note records the visible consequences (404s, unmetered paths).
	Note string
}

// presetPlan resolves a preset name to the drop plan planTrim would build
// for the same keep set. Unknown names return ok=false (caller prints the
// preset names as the recovery hint, same as advise's no-match path).
func presetPlan(name string) (drop []trimUnit, ok bool) {
	p, known := trimPresets[name]
	if !known {
		return nil, false
	}
	skip := make(map[string]bool, len(p.Drop))
	for _, d := range p.Drop {
		skip[d] = true
	}
	var keep []string
	for _, u := range manifestUnits {
		if !skip[u.id] {
			keep = append(keep, u.id)
		}
	}
	// Empty means keep-all in parseKeep's convention, so an empty keep
	// list must be spelled keepNone ("none") — otherwise the minimal
	// preset (drop everything) would silently keep everything.
	plugins, features := strings.Join(keep, ","), strings.Join(keep, ",")
	if len(keep) == 0 {
		plugins, features = keepNone, keepNone
	}
	return planTrim(parseKeep(plugins, features)), true
}

// presetKeepLists resolves a named trim preset to the --plugins/--features
// keep lists the rest of the pipeline consumes. Unknown names fail fast
// listing the valid presets (same contract as checkKeepIDs for ids).
func presetKeepLists(name string) (plugins, features string, err error) {
	p, ok := trimPresets[name]
	if !ok {
		return "", "", fmt.Errorf("unknown preset %q (valid: %s)",
			name, strings.Join(trimPresetNames(), ", "))
	}
	skip := make(map[string]bool, len(p.Drop))
	for _, d := range p.Drop {
		skip[d] = true
	}
	var keepP, keepF []string
	for _, u := range manifestUnits {
		if skip[u.id] {
			continue
		}
		switch u.meta().kind {
		case capabilities.KindPlugin:
			keepP = append(keepP, u.id)
		case capabilities.KindFeature:
			keepF = append(keepF, u.id)
		default:
			// Unknown kind: planTrim keeps it fail-safe, so list it in
			// both dimensions (empty means keep-all per dimension — a
			// fail-open here would drop it, hence the explicit default).
			keepP = append(keepP, u.id)
			keepF = append(keepF, u.id)
		}
	}
	// Empty means keep-all in parseKeep's convention, so a preset that
	// drops a whole dimension must spell keepNone ("none") — the same
	// trap presetPlan documents above.
	plugins, features = strings.Join(keepP, ","), strings.Join(keepF, ",")
	if plugins == "" {
		plugins = keepNone
	}
	if features == "" {
		features = keepNone
	}
	return plugins, features, nil
}

// resolvePreset turns --preset into keep lists. One source of keep truth:
// a preset and explicit keep lists together are refused rather than
// precedence-guessed. Lives with the presets (not loadOptions) for the
// gocyclo gate.
func resolvePreset(opt *options) error {
	if opt.preset == "" {
		return nil
	}
	if opt.plugins != "" || opt.features != "" {
		return fmt.Errorf("--preset %q conflicts with --plugins/--features (use one)", opt.preset)
	}
	plugins, features, err := presetKeepLists(opt.preset)
	if err != nil {
		return err
	}
	opt.plugins, opt.features = plugins, features
	return nil
}

// trimPresetNames lists preset names in stable order for help output.
func trimPresetNames() []string {
	names := make([]string, 0, len(trimPresets))
	for _, id := range []string{presetMinimal, presetNoAI, presetNoCollab} {
		if _, ok := trimPresets[id]; ok {
			names = append(names, id)
		}
	}
	for name := range trimPresets {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}
