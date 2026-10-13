// SCOPE:plugin — installer engine: the DagNats trim unit (data only)
//
// Split from manifest_units.go because it is by far the largest unit: the
// flat stack embeds onboarding-adjacent wiring in the composition root, so
// trimming the engine edits the boot block, the mount helper, the route
// lines and the import. One unit one file keeps that readable, and the
// file-sizes gate is right to insist on the split.

package installer

var dagNatsTrimUnit = []trimUnit{
	{
		id: unitDagnats,
		extraDrops: []fileDrop{
			{
				path: chiGoFile,
				substrs: []string{
					"mountOnboarding(mux, onboardingH)",
					// The engine package is gone with the unit: its import
					// goes too.
					`"github.com/calionauta/gogogo/features/onboarding"`,
				},
			},
		},
		// NOTE: no replaces needed. The mount-after-composition pattern keeps
		// buildChiMux's signature stable, so neither the signature line, the
		// call site, nor the composition test names the onboarding handler.
		// (If a future optional surface adds a buildChiMux parameter again,
		// prefer extending the mount pattern over reintroducing replaces.)
		extraStrips: []fileStrip{
			{
				// The "Run durable workflow" entry button in both skins: the
				// only /api/onboarding refs in any kept template. Without the
				// engine each would be a dead button firing a dead endpoint,
				// so both go with the unit. The gated panels/tabs/hints around
				// them stay, mirroring runtime-disabled (DAGNATS_ENABLED=false
				// renders the same pages without the engine) — the trim only
				// removes what can actually fire.
				path: "features/todo/components/todo_list.templ",
				rules: []stripRule{
					{
						startMarker: "<!-- trim:dagnats removes this entry button",
						endMarker:   "</button>",
					},
				},
			},
			{
				path: "web/skins/basecoat/todo_basecoat.templ",
				rules: []stripRule{
					{
						startMarker: "<!-- trim:dagnats removes this entry button",
						endMarker:   "</button>",
					},
				},
			},
			{
				path: chiGoFile,
				rules: []stripRule{
					{
						startMarker:       "// mountOnboarding registers the onboarding routes",
						endIsClosingBrace: true,
						endBrace:          "}",
						// Anchor on a function header no unit trims, using the WHOLE line
						// (findAnchor compares exact lines). The marker must survive both
						// the dagnats and the goakt trim, which remove the helpers around
						// it; an anchor inside a removable span disappears with it.
						addBefore: "func srvHandler(mux http.Handler) *http.Server {",
					},
					{
						// The whole bootstrap helper: nothing calls it once
						// the call block below is dropped, and dead code that
						// does not compile still breaks the build.
						startMarker:       "// bootOnboarding wires the DagNats onboarding slice",
						endIsClosingBrace: true,
						endBrace:          "}",
						// Top-level anchor that survives every trim (see the mountOnboarding
						// rule). Whole line: findAnchor compares exact lines.
						addBefore: "func srvHandler(mux http.Handler) *http.Server {",
					},
					{
						// One contiguous block: the call and the hook append.
						// The start marker is the call line itself (not the
						// comment above it) so the rule also matches minimal
						// fixtures that only carry the wiring lines. The
						// comment describing the block goes with it, or the
						// trim leaves prose about absent code.
						startMarker: "onboardingH, onbShutdown := bootOnboarding(",
						endMarker:   "shutdown = append(shutdown, onbShutdown...)",
						// Re-inserted just below the shutdown-hook declaration, which is
						// what it appends to. NOT bootRoomActor: the goakt trim removes
						// that, so anchoring there loses the spot on exactly the trees
						// that need `add dagnats` to work. The declaration is the
						// single-hub line (not the old `var shutdown []func()`).
						addAfter: "\tshutdown := []func(){chiQ.Close}",
						alsoDeleteContains: []string{
							"// Onboarding wires before the actor boot",
						},
					},
				},
			},
		},
		mainStrips: []stripRule{
			{
				startMarker: "startDagNats(lifecycleCtx, cfg, stack.TodoHandler)",
				endMarker:   "defer shutdownDagNats()",
				addAfter:    mainCallsAnchor,
				// The lifecycle context exists ONLY to bound DagNats boot work,
				// so removing the engine leaves its declaration unused and the
				// build fails on `declared and not used: lifecycleCtx`. Drop the
				// declaration, its defer, and the comment block that documents
				// them — otherwise the trim leaves an orphaned three-line comment
				// explaining a variable that no longer exists.
				alsoDeleteContains: []string{
					"// DagNats owns the embedded NATS on :4222 and must boot before",
					"// startNATS so the realtime broadcaster can attach to it. It's always",
					"// compiled; when DAGNATS_ENABLED=false it no-ops.",
				},
			},
			{
				startMarker:       "// WORKAROUND (upstream DagNats v0.0.24 bug)",
				endIsClosingBrace: true,
				endBrace:          "\t}",
				addBefore:         "\t// The process is the server now (there is no framework run loop",
			},
		},
		goModDrops: []string{"github.com/danmestas/dagnats"},
	},
}
