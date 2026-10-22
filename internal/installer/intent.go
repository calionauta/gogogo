// SCOPE:plugin — installer engine: advise routing (which command runs next)
package installer

import (
	"slices"
	"strings"
)

// The routing intents. advise answers "which units fit a use case", but the
// question an agent usually receives is "what do I do now": keep this project
// current, port a capability into an app that already exists, restore a unit I
// trimmed, add a feature. Those are different commands, and naming the wrong
// one costs a refuse-and-retry cycle — which is why the answer carries an
// ordered route for the intent, and never a command that would refuse in the
// tree it just probed.
//
// The intent is an INPUT first (--intent / the MCP `intent` arg): the model
// knows what the user asked for better than any keyword list. Detection from
// the need text is the convenience path, and the answer always says which one
// it used, so "I detected update from the wording" is distinguishable from
// "you told me update".
const (
	intentNone       = ""
	intentUpdate     = "update"
	intentPort       = "port"
	intentAddUnit    = "add-unit"
	intentNewFeature = "new-feature"
	intentScaffold   = "scaffold"
)

// intentNames is the accepted vocabulary, in the order --help prints it.
var intentNames = []string{intentUpdate, intentPort, intentAddUnit, intentNewFeature, intentScaffold}

// intentSignals is the detection table: the FIRST intent with a hit wins, so
// the order is the tie-break when a need names two ("update my gogogo project"
// is an update, not a scaffold).
var intentSignals = []struct {
	intent  string
	signals []string
}{
	// "behind"/"stale"/"current" are how people describe an update without
	// saying "update"; "migrate" is the command they half-remember; "up to
	// date" is the phrase, matched as a phrase.
	{intentUpdate, []string{
		"update", "upgrade", "behind", "stale", "outdated",
		"migrate", "sync", "current", "up to date", "out of date",
	}},
	// Porting is named by the TREE ("my existing app"), never by a capability
	// name, so the signal has to be the phrase people use for a codebase that
	// predates gogogo.
	{intentPort, []string{"existing", "brownfield", "adopt", "retrofit", "already"}},
	{intentAddUnit, []string{"restore", "readd", "bring"}},
	{intentNewFeature, []string{"feature", "slice", "endpoint"}},
	{intentScaffold, []string{"scaffold", "greenfield", "template", "bootstrap", "start"}},
}

// validIntent reports whether want names a route.
func validIntent(want string) bool {
	return want == intentNone || slices.Contains(intentNames, want)
}

// resolveIntent picks the route and records where it came from: what the
// caller said ("given"), what the wording implies ("detected"), or — on a path
// that is not a gogogo checkout — porting ("default"), because that is the
// only route that can work there. The source is reported, so an agent can tell
// "the user said update" from "the words looked like update".
func resolveIntent(need, given, tree string) (intent, source string) {
	if given != "" {
		return given, "given"
	}
	if d := detectIntent(need); d != "" {
		return d, "detected"
	}
	if tree == treeNotCheckout {
		return intentPort, "default"
	}
	return intentNone, ""
}

// detectIntent classifies a need by its wording. Empty need means "nobody
// said", never "guess": guessing an intent from silence is how an agent gets
// sent to the wrong command with confidence.
//
// Single words go through wordHit (the same prefix rules presets use, which is
// what stops "back" from matching "backend"). Phrases go through the
// normalized need as substrings, exactly like the stdlib signals: "up to date"
// tokenizes to three words that must not match anything on their own.
func detectIntent(need string) string {
	if strings.TrimSpace(need) == "" {
		return intentNone
	}
	words := needWords(need)
	joined := needWordsJoined(need)
	for _, row := range intentSignals {
		for _, sig := range row.signals {
			if isPhrase(sig) {
				if strings.Contains(joined, sig) {
					return row.intent
				}
				continue
			}
			if wordHit(words, sig) {
				return row.intent
			}
		}
	}
	return intentNone
}

// isPhrase reports whether a signal must be matched as a phrase instead of a
// single word (the stdlib-signal rule, shared so the two tables cannot drift).
func isPhrase(sig string) bool {
	return strings.Contains(sig, " ") || strings.Contains(sig, "-")
}

// routeFor resolves the ordered steps for one intent in one probed tree.
//
// The tree decides which commands are LEGAL, not merely preferable: `add` and
// `new` refuse a non-checkout, `export` writes a scratch bundle and leaves the
// target alone. A route that names a command the tree would refuse is worse
// than no route, so every (intent, tree) pair below is one of the three states
// it can actually be in. dir is substituted into the command when the caller
// gave one, so the line is copy-pasteable instead of a template.
func routeFor(intent, tree, dir string) []adviseStep {
	where := dir
	if where == "" {
		where = "<dir>"
	}
	checkout := tree == treeCheckout || tree == treeUnknown
	switch intent {
	case intentUpdate:
		if tree == treeNotCheckout {
			return []adviseStep{{Why: "this path records no scaffolded-from: line, so there " +
				"is nothing to compare — a tree you ported into by hand has no base to be " +
				"behind. Note the upstream tag you copied from in your own README; " +
				"--check-update only speaks for a scaffolded checkout."}}
		}
		return []adviseStep{
			{
				Run: "gogogo --check-update --dir " + where,
				Why: "read-only verdict on the tool AND the project; exit 3 means an " +
					"update exists, which is a verdict, not an error",
			},
			{
				Run: "gogogo migrate --dir " + where,
				Why: "every release between the recorded base and the latest, plus the " +
					"curated notes and the gates",
			},
			{
				Run: "gogogo --check --dir " + where,
				Why: "after applying: does the tree still match the unit manifest",
			},
		}
	case intentPort, intentAddUnit:
		if !checkout {
			return portRoute()
		}
		return []adviseStep{
			{
				Run: "gogogo add <unit> --from <template> --dir " + where + " --yes",
				Why: "add restores the unit mechanically in a gogogo checkout (copy + " +
					"rewire + build proof); on a tree that is NOT one it refuses — point " +
					"--dir at it and this answer switches to the export route instead",
			},
			{
				Run: "gogogo --check --dir " + where,
				Why: "confirms the tree matches the manifest again",
			},
		}
	case intentNewFeature:
		if !checkout {
			return []adviseStep{
				{Why: "new scaffolds INTO a gogogo checkout (it rewires the router and " +
					"the capability registry), so it refuses a foreign tree"},
				{
					Run: "gogogo export todo --dir <template> --to /tmp/port-todo",
					Why: "take the reference slice instead, and copy its shape: handler + " +
						"page + store + test, wired by hand",
				},
			}
		}
		return []adviseStep{
			{
				Run: "gogogo new feature <name> --dir " + where + " --dry-run --format json",
				Why: "preview the plan: handler + page, wiring, capability unit, RBAC rows",
			},
			{
				Run: "gogogo new feature <name> --dir " + where + " --yes",
				Why: "apply, then the built-in proof runs templ, docs, check-scope, " +
					"flatdeps and the build",
			},
		}
	case intentScaffold:
		return []adviseStep{
			{
				Run: "gogogo advise --need \"<your need>\" --format json",
				Why: "the unit list this answer already gives you, machine-readable",
			},
			{
				Run: "gogogo --name my-app --plugins <ids> --features <ids> " +
					"--no-tui --dry-run --format json --dir ./my-app",
				Why: "preview the trim plan with every consequence; nothing is written",
			},
			{
				Run: "gogogo --name my-app --no-tui --yes --dir ./my-app",
				Why: "apply: clone, trim, rename, tidy, build proof",
			},
		}
	}
	return nil
}

// portRoute is the foreign-tree route: the only three steps that can work where
// nothing installs.
func portRoute() []adviseStep {
	return []adviseStep{
		{
			Run: "gogogo export <unit> --dir <template> --to /tmp/port-<unit>",
			Why: "copies the unit's owned files plus the exact wiring, import rewrites " +
				"and proof into a scratch dir; your tree is never touched",
		},
		{Why: "then docs/porting-into-an-existing-app.md — the wiring is yours to " +
			"write (chi mount, session check, module entry, RBAC row)"},
		{
			Run: "go build ./...",
			Why: "the proof is YOUR app's build, not the template's",
		},
	}
}
