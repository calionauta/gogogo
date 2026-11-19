// Installer engine: the feature built-in units (data only)
//
// Whiteboard, config-view, credits and genui: each is a
// self-mounting bundled unit (Capability.Register, registry-driven).
// Trimming one deletes its directory (via the capability Dirs), drops
// its nav entry and 404-pins its routes in the mux test; the generated
// registration refreshes from the registry during the trim proof. The
// only composition-root wiring left for whiteboard+notes is the shared
// collab wiring in BootChi feeding Deps.

package installer

// 404-pin replacements shared by the route-removing units: without the
// route the mux contract test must assert absence ("not here" beats a dead
// route answering anything else). Consts, not literals — one per distinct
// shape the test pins (goconst enforces it).
const (
	muxWant404    = "want 404"
	muxGoneStatus = "http.StatusNotFound"
)

// collabImportSuffix is the transport's import line as it appears in a source
// file, and the substring a trim removes. Named because three declarations match
// on the same text (the server wiring and the two tests that build the bundle),
// and a literal repeated three times drifts the moment one is edited.
const collabImportSuffix = `internal/collab"`

var featureBuiltinUnits = []builtinUnit{
	{
		id: unitWhiteboard,
		// Bundle rationale lives in the collab capability entry: one
		// unit is the only honest shape (see internal/capabilities).
		// Self-mounting now: the handlers register through units.gen.go.
		extraDrops: []fileDrop{
			{
				path: chiGoFile,
				substrs: []string{
					// The shared collab wiring fed into Deps.
					"persister, docs, perr := bootCollab(cfg, queries)",
					// The branch that handles bootCollab's error is NOT dropped
					// here: a line-level match takes the `return` and leaves the
					// `if perr != nil {` head, whose brace then eats a neighbour.
					// blockStrips removes the whole branch.
					// The transport import and the struct field that holds it.
					collabImportSuffix,
					"federated := bootPresence(hub)",
					"Collab:  collab.Handoff{Persister: persister, Docs: docs, Presence: federated},",
					"// Collaborator presence: ONE local seam for both features (the actor when",
					"// GoAkt runs, the in-process PeerSet otherwise), so a deployment cannot end",
					"// up with two rosters that disagree about the same tab. It is federated",
					"// across instances in AttachPresenceFederation once NATS exists.",
					"presence:             federated,",
					// The ChiStack field the wiring above assigned.
					"presence *collab.FederatedPresence",
					"// presence is the roster seam both collaborative features share (created",
					"// at boot, federated by AttachPresenceFederation). Nil = features trimmed.",
					"upgradePresenceToActor(federated, appgoakt.NewDocPresence(roomHost))",
					`MountSubtree(r, whiteboard.AssetPath, whiteboard.StaticHandler())`,
					`MountSubtree(r, notes.AssetPath, notes.StaticHandler())`,
					"// Features that embed their OWN assets (whiteboard.js + rough.min.js,",
					"// notes.js) register their subtree here; the core /static/ mount never",
					"// serves them. A feature trim drops its line and its import.",
					`	"github.com/calionauta/gogogo/features/whiteboard"`,
					`	"github.com/calionauta/gogogo/features/notes"`,
				},
			},
			{
				path: bootHelpersFile,
				substrs: []string{
					collabImportSuffix,
				},
			},
		},
		// The `if perr != nil { ... }` branch of the collab boot leaves as a
		// block, head and closing brace together. Dropping only its `return`
		// left the head behind and the file failed with "undefined: perr" — the
		// symptom blamed on the operator's first build instead of this list.
		blockStrips: []stripRule{
			{
				path:              chiGoFile,
				startMarker:       "if perr != nil {",
				endIsClosingBrace: true,
				endBrace:          "\t}",
			},
		},
		mainStrips: []stripRule{
			{
				startMarker:       "// The FRAMES crossing instances (above) are not enough for the \"X online\"",
				endIsClosingBrace: true,
				endBrace:          "\t}",
			},
			{
				// bootCollab RETURNS collab types, so unlike a caller it cannot
				// survive the package's removal: its signature names them. Absent
				// from this list, the tree compiled right up to "undefined: collab"
				// in a file nobody suspected.
				startMarker:       "// one helper keeps the two lines that must agree in one place.",
				endIsClosingBrace: true,
				endBrace:          "}", // top-level func: its brace sits in column 0
			},
		},
		desktopStrips: []stripRule{
			{
				startMarker: "// Edge sync (Phase C): a demo publish keeps the edge path exercised.",
				endMarker:   "defer publishDemoCollab(nat, js)()",
			},
			{
				startMarker:       "// publishDemoCollab exercises the edge-sync publish paths end to end",
				endIsClosingBrace: true,
				endBrace:          "}",
				addBefore:         "// desktopConfig loads config and applies the shell's overrides. Split out of",
			},
		},
		desktopDropLines: []string{collabImportSuffix, `"time"`},
		// Tests that build the collab bundle travel with it: they import the
		// package by name, so `go mod tidy` parses them and reports "finding
		// module for .../internal/collab" for a tree that builds fine without
		// them — the error that made a clean scaffold look broken.
		testDrops: []fileDrop{
			{path: "internal/server/chi_test.go", substrs: []string{collabImportSuffix}},
			{path: "internal/server/mux_fixture_test.go", substrs: []string{collabImportSuffix}},
		},
		// loro-go is the CGO binding the transport is built on: with collab gone,
		// nothing imports it, and `go mod tidy` will not drop a requirement on its
		// own. Left pinned, every target keeps needing a C toolchain — the exact
		// defect that shipped empty GUI releases (#454). The fork's replace line
		// goes with it.
		goModDrops: []string{"github.com/aholstenson/loro-go", "github.com/calionauta/loro-go"},
		extraStrips: []fileStrip{
			{
				path: modulesFile,
				rules: []stripRule{
					{startMarker: `{ID: "whiteboard"`, endMarker: "},"},
					{startMarker: `{ID: "notes"`, endMarker: "},"},
				},
			},
			{
				path: chiGoFile,
				rules: []stripRule{
					{
						startMarker: "persister, docs, perr := bootCollab(cfg, queries)",
						endMarker:   "\t}",
					},
				},
			},
			{
				path: bootHelpersFile,
				rules: []stripRule{
					{
						startMarker:       "// bootCollab builds",
						endIsClosingBrace: true,
						endBrace:          "}",
					},
				},
			},
			{
				path: chiGoFile,
				rules: []stripRule{
					{
						startMarker: "\t// presence is the roster seam both collaborative features share",
						endMarker:   "\tpresence *collab.FederatedPresence",
					},
				},
			},
		},
	},
	{
		id: unitConfigView,
		extraStrips: []fileStrip{
			{
				path:  modulesFile,
				rules: []stripRule{{startMarker: `{ID: "config"`, endMarker: "},"}},
			},
			{
				path: "features/authviews/controls.templ",
				rules: []stripRule{{
					startMarker: "<!-- trim:config-view removes this entry",
					endMarker:   "}",
				}},
			},
		},
		replaces: []fileReplace{
			{
				path:        chiTestFile,
				matchSubstr: "configRec.Code != http.StatusForbidden",
				old:         "http.StatusForbidden",
				newStr:      muxGoneStatus,
			},
			{
				path:        chiTestFile,
				matchSubstr: "mux anon /config status = %d, want 403",
				old:         "want 403",
				newStr:      muxWant404,
			},
		},
	},
	{
		id: unitCredits,
		replaces: []fileReplace{
			{
				path:        chiTestFile,
				matchSubstr: "creditsRec.Code != http.StatusUnauthorized",
				old:         "http.StatusUnauthorized",
				newStr:      muxGoneStatus,
			},
			{
				path:        chiTestFile,
				matchSubstr: "want 401 JSON",
				old:         "want 401 JSON",
				newStr:      muxWant404,
			},
		},
		goModDrops: []string{"github.com/calionauta/ai-credits"},
	},
	{
		id: unitReview,
		extraStrips: []fileStrip{
			{
				path:  modulesFile,
				rules: []stripRule{{startMarker: `{ID: "review"`, endMarker: "},"}},
			},
		},
	},
}
