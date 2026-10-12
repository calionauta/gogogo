// SCOPE:plugin — installer engine: the feature trim units (data only)
//
// Whiteboard, landing, config-view and credits: each wires into the flat
// composition root, so their trims edit the shared signature, call site and
// composition test. Grouped here so manifest_units.go stays under the
// file-sizes gate; the grouping is by size, not by domain.

package installer

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
					"wbchi.NewHandler(queries, a, hub, persister, docs, cfg),",
					"notesachi.NewHandler(queries, a, hub, persister, docs, cfg),",
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
					`	wbchi "github.com/calionauta/gogogo/features/whiteboard/chi"`,
					`	notesachi "github.com/calionauta/gogogo/features/notes/chi"`,
					`	"github.com/calionauta/gogogo/internal/collab"`,
					"wbH *wbchi.Handler,",
					"notesH *notesachi.Handler,",
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
					`wbchi "github.com/calionauta/gogogo/features/whiteboard/chi"`,
					`notesachi "github.com/calionauta/gogogo/features/notes/chi"`,
					"wbchi.NewHandler(",
					"notesachi.NewHandler(",
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
						startMarker: "persister, perr := collab.NewTursoPersister(queries)",
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
					"landingchi.NewHandler(cfg, a),",
					"landingH *landingchi.Handler,",
					`landingchi "github.com/calionauta/gogogo/features/landing/chi"`,
					`mux.Handle("/", landingH.Routes())`,
				},
			},
			{
				path: chiTestFile,
				substrs: []string{
					`landingchi "github.com/calionauta/gogogo/features/landing/chi"`,
					"landingchi.NewHandler(cfg, a),",
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
					"configchi.NewHandler(cfg, a),",
					"configH *configchi.Handler,",
					`configchi "github.com/calionauta/gogogo/features/config/chi"`,
					`mux.Handle("/config", configH.Routes())`,
					`mux.Handle("/config/", configH.Routes())`,
				},
			},
			{
				path: chiTestFile,
				substrs: []string{
					`configchi "github.com/calionauta/gogogo/features/config/chi"`,
					"configchi.NewHandler(cfg, a),",
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
					`creditschi "github.com/calionauta/gogogo/features/credits/chi"`,
					"creditsH *creditschi.Handler,",
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
					`creditschi "github.com/calionauta/gogogo/features/credits/chi"`,
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
}
