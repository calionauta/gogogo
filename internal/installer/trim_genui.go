// SCOPE:plugin — installer engine: the genui/chat trim unit (data only)
//
// Split out of trim_features.go for the file-size gate. genui is the AI
// bundle: chat imports genui components (no-compile without it), so the two
// legs are one unit (see the genui capability entry). It grew past its
// neighbours because a bad answer surface must fail the BOOT, which means
// the unit owns a boot helper (bootAI) rather than a handful of lines.

package installer

// genuiTrimUnits holds the one AI unit. A slice (not a bare literal) so
// manifest_units.go appends it the same way it appends featureTrimUnits.
//
// The import lines are consts because the unit drops them from three files
// (chi.go, chi_test.go and boot_helpers.go) and goconst is right that one
// spelling beats three literals: a rename that missed one would leave a
// dangling import in the trimmed tree.
const (
	chathandlersImport  = `chathandlers "github.com/calionauta/gogogo/features/chat/handlers"`
	chatstoreImport     = `chatstore "github.com/calionauta/gogogo/features/chat/store"`
	genuihandlersImport = `genuihandlers "github.com/calionauta/gogogo/features/genui/handlers"`
	genuiComponentsImp  = `comp "github.com/calionauta/gogogo/features/genui/components"`
)

var genuiTrimUnits = []trimUnit{
	{
		id: unitGenUI,
		// Bundle rationale lives in the genui capability entry: chat imports
		// genui components (no-compile without it), so one unit is the only
		// honest shape (see internal/capabilities).
		extraStrips: []fileStrip{
			{
				// bootAI's whole body goes with the unit: nothing calls it once
				// the drop below removes the call line, and leaving it would
				// keep every genui/chat import alive. Same shape as
				// bootReview's strip, in the same file (boot_helpers.go) —
				// which is also why BootChi no longer imports llm/comp/chatstore
				// at all in a trimmed tree.
				path: bootHelpersFile,
				rules: []stripRule{
					{
						startMarker:       "// bootAI wires both AI legs",
						endIsClosingBrace: true,
						endBrace:          "}",
					},
				},
			},
			{
				path: navbarTempl,
				rules: []stripRule{
					{
						startMarker: `<a href="/genui"`,
						endMarker:   `}>Ask AI</a>`,
					},
				},
			},
		},
		extraDrops: []fileDrop{
			{
				path: chiGoFile,
				substrs: []string{
					// The helper call (one line) is dropped as a whole; the
					// helper's body (including the RegisterHandlers calls it
					// now owns) goes via the strip rule above, because a
					// multi-line construction cannot be removed by substring.
					"chatH, genuiH, err := bootAI(cfg, a, chiQ, hub, realLLM, simulatedLLM, queries)",
					// One argument per line: each drop removes exactly its
					// feature's line, and add restores them.
					"chatH,",
					"genuiH,",
					`mux.Handle("/chat", chatH.Routes())`,
					`mux.Handle("/chat/", chatH.Routes())`,
					`mux.Handle("/api/chat/", chatH.Routes())`,
					`mux.Handle("/genui", genuiH.Routes())`,
					`mux.Handle("/genui/", genuiH.Routes())`,
					`mux.Handle("/api/genui/", genuiH.Routes())`,
					chathandlersImport,
					genuihandlersImport,
					// chatstore and comp are NOT listed for chi.go: BootChi never
					// imported them (bootAI owns those references), and a marker
					// that no longer exists in the file is drift the sync test
					// fails on. Their boot_helpers.go drops are in the fileDrop
					// below.
					"chatH *chathandlers.Handler,",
					"genuiH *genuihandlers.Handler,",
				},
			},
			{
				// bootAI's signature and body are the last users of these imports
				// in boot_helpers.go, and the strip rule above removes the body —
				// so the imports go by line. Without this the trimmed tree keeps
				// importing packages the unit deleted.
				path: bootHelpersFile,
				substrs: []string{
					chathandlersImport,
					chatstoreImport,
					genuihandlersImport,
					genuiComponentsImp,
				},
			},
			{
				// The server test's genui imports and handler constructions
				// go as line drops (both call sites each); the mux table's
				// "/chat" and "/genui" elements go too (one element per
				// line, so combined trims stay order-independent). The
				// model stubs go with the unit's Files entry (a file
				// delete, not a strip — strips on chi_test.go have no
				// add anchor).
				path: chiTestFile,
				substrs: []string{
					chathandlersImport,
					chatstoreImport,
					genuihandlersImport,
					"chathandlers.NewHandler(",
					"genuihandlers.NewHandler(",
					"stubResponder{raw: \"{}\"}),",
					"\t\t\"/chat\",",
					"\t\t\"/genui\",",
				},
			},
		},
	},
}
