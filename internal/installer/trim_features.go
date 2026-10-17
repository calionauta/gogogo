// SCOPE:plugin — installer engine: the feature trim units (data only)
//
// Whiteboard, landing, config-view, credits and genui: each wires into the
// composition root, so their trims edit the shared signature, call site and
// composition test. Grouped here so manifest_units.go stays under the
// file-sizes gate; the grouping is by size, not by domain.

package installer

// 404-pin replacements shared by the route-removing units: without the
// route the mux contract test must assert absence ("not here" beats a dead
// route answering anything else). Consts, not literals — one per distinct
// shape the test pins (goconst enforces it).
const (
	muxWant404    = "want 404"
	muxGoneStatus = "http.StatusNotFound"
)

var featureTrimUnits = []trimUnit{
	{
		id: unitWhiteboard,
		// Bundle rationale lives in the collab capability entry: one
		// unit is the only honest shape (see internal/capabilities).
		extraDrops: []fileDrop{
			{
				path: chiGoFile,
				substrs: []string{
					// Single-argument lines: dropping removes exactly that
					// argument, and the signature loses the parameters in
					// replaces below, so both sides agree.
					"wbhandlers.NewHandler(queries, a, hub, persister, docs, cfg),",
					"noteshandlers.NewHandler(queries, a, hub, persister, docs, cfg),",
					`mux.Handle("/whiteboard", wbH.Routes())`,
					`mux.Handle("/whiteboard/", wbH.Routes())`,
					`mux.Handle("/api/whiteboard", wbH.Routes())`,
					`mux.Handle("/api/whiteboard/", wbH.Routes())`,
					`mux.Handle("/api/whiteboards/", wbH.Routes())`,
					`mux.Handle("/notes", notesH.Routes())`,
					`mux.Handle("/notes/", notesH.Routes())`,
					`mux.Handle("/api/notes", notesH.Routes())`,
					`mux.Handle("/api/notes/", notesH.Routes())`,
					// The feature's own static subtree (whiteboard.js, notes.js):
					// the core /static/ mount never served them, so each feature
					// owns its mount line and its import below.
					`mux.Handle("/static/wb/", resources.AssetHandler(whiteboard.StaticFS(), "/static/wb"))`,
					`mux.Handle("/static/notes/", resources.AssetHandler(notes.StaticFS(), "/static/notes"))`,
					`	"github.com/calionauta/gogogo/features/whiteboard"`,
					`	"github.com/calionauta/gogogo/features/notes"`,
					// The feature mounts above are the only use of the core
					// resources package in chi.go; without them the import goes.
					`	"github.com/calionauta/gogogo/web/resources"`,
					`	wbhandlers "github.com/calionauta/gogogo/features/whiteboard/handlers"`,
					`	noteshandlers "github.com/calionauta/gogogo/features/notes/handlers"`,
					`	"github.com/calionauta/gogogo/internal/collab"`,
					"wbH *wbhandlers.Handler,",
					"notesH *noteshandlers.Handler,",
				},
			},
			{
				// The server test's collab imports and handler constructions
				// go as line drops; the persister setup (two identical
				// blocks) goes as a strip below, since its `if perr` check
				// would survive drops.
				path: chiTestFile,
				substrs: []string{
					`"github.com/calionauta/gogogo/internal/collab"`,
					`wbhandlers "github.com/calionauta/gogogo/features/whiteboard/handlers"`,
					`noteshandlers "github.com/calionauta/gogogo/features/notes/handlers"`,
					"wbhandlers.NewHandler(",
					"noteshandlers.NewHandler(",
				},
			},
			{
				// The Service Worker intercepts every /api/* fetch: its
				// stream-exclusion list names the notes/whiteboard streams,
				// so the lines go with the unit — otherwise the shipped SW
				// watches routes nothing serves (and the route gate fails
				// the trimmed tree for exactly that). One-way (sw.js has
				// no add anchor): re-add by hand, see addFollowUps.
				path: "web/resources/static/sw.js",
				substrs: []string{
					`"/api/notes/", // notes`,
					`"/api/whiteboard/" // whiteboard`,
				},
			},
		},
		replaces: []fileReplace{
			{
				// nat feeds collab only; without the unit the JetStream
				// handle is all the desktop keeps.
				path:        "cmd/desktop/main.go",
				matchSubstr: "leaf-node transport (nat feeds collab only)",
				old:         "nat, js = h, h.JS",
				newStr:      "js = h.JS",
			},
			{
				path:        "cmd/desktop/main.go",
				matchSubstr: "embedded transport (nat feeds collab only)",
				old:         "nat, js = h, h.JS",
				newStr:      "js = h.JS",
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
		desktopDropLines: []string{`internal/collab"`, `"context"`, `"time"`, `nat *nats.Handle`},
		// No runtimeOff: the whiteboard has no boot-costly servers, so
		// deletion is the off switch (a flag would be config surface
		// with no benefit).
		extraStrips: []fileStrip{
			{
				path: navbarTempl,
				rules: []stripRule{
					{
						startMarker: `<a href="/whiteboard"`,
						endMarker:   `}>Collab whiteboard</a>`,
					},
					{
						startMarker: `<a href="/notes"`,
						endMarker:   `}>Collab notes</a>`,
					},
				},
			},
			{
				// The persister block: dropping its lines piecemeal leaves
				// the `if perr` check referencing nothing, so it goes as
				// one block from construction through wiring.
				path: chiGoFile,
				rules: []stripRule{
					{
						startMarker: "persister, perr := collab.SnapshotStore(cfg.Collab.Snapshots, queries)",
						endMarker:   "docs.SetPersister(persister)",
					},
				},
			},
			{
				// Same block in the composition test (two identical copies;
				// the rule matches every occurrence, so one rule covers
				// both call sites).
				path: chiTestFile,
				rules: []stripRule{
					{
						startMarker: "persister, perr := collab.NewTursoPersister(",
						endMarker:   "docs := collab.NewDocStore()",
					},
				},
			},
		},
	},
	{
		id: unitLanding,
		extraDrops: []fileDrop{
			{
				// One argument per line in both the call and the signature,
				// so each drop removes exactly its feature's line — and the
				// add path restores the call line from these same drops
				// (see routerCallLines).
				path: chiGoFile,
				substrs: []string{
					"landinghandlers.NewHandler(cfg, a),",
					"landingH *landinghandlers.Handler,",
					`landinghandlers "github.com/calionauta/gogogo/features/landing/handlers"`,
					`mux.Handle("/", landingH.Routes())`,
				},
			},
			{
				path: chiTestFile,
				substrs: []string{
					`landinghandlers "github.com/calionauta/gogogo/features/landing/handlers"`,
					"landinghandlers.NewHandler(cfg, a),",
				},
			},
		},
		replaces: []fileReplace{
			{
				path:        navbarTempl,
				matchSubstr: `class="app-nav-brand"`,
				old:         `href="/"`,
				newStr:      `href="/todo"`,
			},
			{
				// Landing owned the only root route: without it GET /
				// 404s (pinned here, warned in the capability). One-way
				// (add leaves the 404 pin): re-assert 200 by hand, see
				// addFollowUps.
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
		extraDrops: []fileDrop{
			{
				// One argument per line, same as landing: drops remove
				// exactly their feature's lines, and add restores them.
				path: chiGoFile,
				substrs: []string{
					"confighandlers.NewHandler(cfg, a, queries),",
					"configH *confighandlers.Handler,",
					`confighandlers "github.com/calionauta/gogogo/features/config/handlers"`,
					`mux.Handle("/config", configH.Routes())`,
					`mux.Handle("/config/", configH.Routes())`,
				},
			},
			{
				path: chiTestFile,
				substrs: []string{
					`confighandlers "github.com/calionauta/gogogo/features/config/handlers"`,
					"confighandlers.NewHandler(cfg, a, dbgen.New(sqlDB)),",
				},
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
		replaces: []fileReplace{
			{
				// The mux contract test pins anon /config as an auth
				// redirect (303). Without the unit the route must 404
				// (same rule as credits/landing: absence says "not
				// here"). One-way (add leaves the 404 pin): re-assert
				// 303 by hand, see addFollowUps.
				path:        chiTestFile,
				matchSubstr: "configRec.Code != http.StatusSeeOther",
				old:         "http.StatusSeeOther",
				newStr:      muxGoneStatus,
			},
			{
				path:        chiTestFile,
				matchSubstr: "mux anon /config status = %d, want 303",
				old:         "want 303",
				newStr:      muxWant404,
			},
		},
	},
	{
		id: unitCredits,
		extraDrops: []fileDrop{
			{
				path: chiGoFile,
				substrs: []string{
					`mux.Handle("/api/credits", creditsH.Routes())`,
					`mux.Handle("/api/credits/", creditsH.Routes())`,
					`mux.Handle("/api/ai/", creditsH.Routes())`,
					`mux.Handle("/api/byok/", creditsH.Routes())`,
					`"github.com/calionauta/gogogo/features/credits"`,
					`creditshandlers "github.com/calionauta/gogogo/features/credits/handlers"`,
					"creditsH *creditshandlers.Handler,",
					"creditsH,",
				},
			},
			{
				// The import goes as a line drop. The two constructions do
				// not: each carries buildChiMux's own closing paren, so a
				// drop would take it with it. Two identical replaces, one
				// per call site — each matches the next remaining
				// occurrence, so both land.
				path: chiTestFile,
				substrs: []string{
					`creditshandlers "github.com/calionauta/gogogo/features/credits/handlers"`,
				},
			},
		},
		replaces: []fileReplace{
			{
				// The two composition call sites construct the handler
				// inline, and each line carries buildChiMux's own closing
				// paren — so a drop would take it with it. Two identical
				// replaces, one per site: each matches the next remaining
				// occurrence, so both land. (The chi.go signature and call
				// lines are one-arg-per-line, so those go as drops above.)
				path:        chiTestFile,
				matchSubstr: creditsCallLine,
				old:         creditsCallLine,
				newStr:      ")",
			},
			{
				path:        chiTestFile,
				matchSubstr: creditsCallLine,
				old:         creditsCallLine,
				newStr:      ")",
			},
			{
				// The mux contract test pins the credits route present
				// (anon → 401 JSON). Without the unit the route must 404 —
				// a dead route answering anything else is worse than one
				// that says "not here" (same rule as the room-disabled
				// test). One-way (add leaves the 404 pin): re-assert 401
				// by hand, see addFollowUps.
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
		extraStrips: []fileStrip{
			{
				// The whole bootstrap helper plus its call block: nothing
				// calls it once the call line is dropped, and dead code
				// that does not compile still breaks the build.
				path: chiGoFile,
				rules: []stripRule{
					{
						startMarker:       "// bootCredits opens the Turso ledger",
						endIsClosingBrace: true,
						endBrace:          "}",
					},
					{
						startMarker:       "creditsH, shutdown, err := bootCredits(",
						endIsClosingBrace: true,
						endBrace:          "\t}",
					},
				},
			},
		},
		goModDrops: []string{"github.com/calionauta/ai-credits"},
	},
	{
		id: unitReview,
		// Bundle rationale lives in the unitCaps comment above: the
		// review page is genkit's only consumer, so trimming the page
		// plus `go mod tidy` is the whole removal (goModDrops below).
		extraDrops: []fileDrop{
			{
				// The boot helper's feature import: the strip below
				// removes its only consumer, so the import goes too
				// (same rule as the chi.go import drops).
				path: bootHelpersFile,
				substrs: []string{
					reviewImportSuffix,
				},
			},
			{
				path: chiGoFile,
				substrs: []string{
					// Construction is one line; the signature loses the
					// parameter in the bootReview strip below.
					"reviewH := bootReview(cfg, a, realLLM)",
					// One argument per line: each drop removes exactly
					// its feature's line, and add restores them.
					"reviewH,",
					`mux.Handle("/review", reviewH.Routes())`,
					`mux.Handle("/review/", reviewH.Routes())`,
					`mux.Handle("/api/review/", reviewH.Routes())`,
					reviewImportSuffix,
					"reviewH *reviewhandlers.Handler,",
				},
			},
			{
				// The composition test's import, helper call sites and
				// mux-table element go as line drops; the helper itself
				// goes as a strip below (its body would survive drops).
				path: chiTestFile,
				substrs: []string{
					reviewImportSuffix,
					`	"github.com/calionauta/gogogo/internal/llm"`,
					"newReviewHandlerForTest(cfg, a),",
					"\t\t\"/review\",",
				},
			},
		},
		extraStrips: []fileStrip{
			{
				path: navbarTempl,
				rules: []stripRule{
					{
						startMarker: `<a href="/review"`,
						endMarker:   `}>Review</a>`,
					},
				},
			},
			{
				// The whole boot helper (lives in boot_helpers.go for the
				// file-size gate): nothing calls it once the call line is
				// dropped (same shape as the bootCredits strip).
				path: bootHelpersFile,
				rules: []stripRule{
					{
						startMarker:       "// bootReview wires the Genkit review-loop slice",
						endIsClosingBrace: true,
						endBrace:          "}",
					},
				},
			},
			{
				// The composition-test helper: same rule, test file.
				path: chiTestFile,
				rules: []stripRule{
					{
						startMarker:       "// newReviewHandlerForTest builds the review handler",
						endIsClosingBrace: true,
						endBrace:          "}",
					},
				},
			},
		},
		// genkit goes with the page (review is its only consumer). go-jev
		// stays: the Ask router (genui/handlers) imports it too, so dropping
		// the require would leave `trim review` with an unresolvable import.
		goModDrops: []string{"github.com/firebase/genkit/go"},
	},
}
