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
				path: chiGoFile,
				rules: []stripRule{
					{
						startMarker:       "// mountOnboarding registers the onboarding routes",
						endIsClosingBrace: true,
						endBrace:          "}",
					},
					{
						// The whole bootstrap helper: nothing calls it once
						// the call block below is dropped, and dead code that
						// does not compile still breaks the build.
						startMarker:       "// bootOnboarding wires the DagNats onboarding slice",
						endIsClosingBrace: true,
						endBrace:          "}",
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
				addBefore:         "\t// Phase 2: wire the CRDTStore JetStream transport when the chosen",
			},
		},
		goModDrops: []string{"github.com/danmestas/dagnats"},
	},
}
