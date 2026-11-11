// Installer engine: the feature built-in units (data only)
//
// Whiteboard, landing, config-view, credits and genui: each is a
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
					"federated := bootPresence(hub)",
					"Persister: persister, DocStore: docs, Presence: federated,",
					"// Collaborator presence: ONE local seam for both features (the actor when",
					"// GoAkt runs, the in-process PeerSet otherwise), so a deployment cannot end",
					"// up with two rosters that disagree about the same tab. It is federated",
					"// across instances in AttachPresenceFederation once NATS exists.",
					"presence:             federated,",
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
					`	"github.com/calionauta/gogogo/internal/collab"`,
				},
			},
		},
		mainStrips: []stripRule{
			{
				startMarker:       "// The FRAMES crossing instances (above) are not enough for the \"X online\"",
				endIsClosingBrace: true,
				endBrace:          "\t}",
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
		desktopDropLines: []string{`internal/collab"`, `"time"`},
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
		id: unitLanding,
		replaces: []fileReplace{
			{
				path:        shellTempl,
				matchSubstr: `class="app-rail-brand-link"`,
				old:         `href="/"`,
				newStr:      `href="/todo"`,
			},
			{
				path:        chiTestFile,
				matchSubstr: "landingRec.Code != http.StatusOK",
				old:         "http.StatusOK",
				newStr:      muxGoneStatus,
			},
			{
				path:        chiTestFile,
				matchSubstr: "mux GET / status = %d, want 200",
				old:         "want 200",
				newStr:      muxWant404,
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
