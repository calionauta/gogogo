// SCOPE:plugin — installer engine: the genui/chat trim unit (data only)
//
// Split out of trim_features.go for the file-size gate. genui is the AI
// bundle: chat imports genui components (no-compile without it), so the two
// are one unit (see the genui capability entry). It grew past its
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
	chathandlersImport = `chathandlers "github.com/calionauta/gogogo/features/chat/handlers"`
	chatstoreImport    = `chatstore "github.com/calionauta/gogogo/features/chat/store"`
	genuiComponentsImp = `comp "github.com/calionauta/gogogo/features/genui/components"`
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
						startMarker:       "// bootAI wires the Chat demo",
						endIsClosingBrace: true,
						endBrace:          "}",
					},
				},
			},
			{
				// The sidebar renders []Module, so the trimmed Chat
				// destination is one dropped entry.
				path:  modulesFile,
				rules: []stripRule{{startMarker: `{ID: "chat"`, endMarker: "},"}},
			},
		},
		extraDrops: []fileDrop{
			{
				path: chiGoFile,
				substrs: []string{
					// The helper call (one line) is dropped as a whole; the
					// helper's body (including the RegisterHandlers call it
					// now owns) goes via the strip rule above, because a
					// multi-line construction cannot be removed by substring.
					"chatH, err := bootAI(cfg, a, chiQ, hub, realLLM, simulatedLLM, queries)",
					// One argument per line: each drop removes exactly its
					// feature's line, and add restores them.
					"chatH,",
					`r.MountFeature("chat", chatH.Routes())`,
					chathandlersImport,
					// chatstore and comp are NOT listed for chi.go: BootChi never
					// imported them (bootAI owns those references), and a marker
					// that no longer exists in the file is drift the sync test
					// fails on. Their boot_helpers.go drops are in the fileDrop
					// below.
					"chatH *chathandlers.Handler,",
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
					genuiComponentsImp,
				},
			},
			{
				// The server test's chat imports and handler constructions
				// go as line drops; the mux table's "/chat" element goes too
				// (one element per line, so combined trims stay
				// order-independent). The model stubs go with the unit's Files
				// entry (a file delete, not a strip — strips on chi_test.go
				// have no add anchor).
				path: chiTestFile,
				substrs: []string{
					chathandlersImport,
					chatstoreImport,
					"chathandlers.NewHandler(",
					"stubResponder{raw: \"{}\"}),",
					"\t\t\"/chat\",",
				},
			},
		},
	},
}
