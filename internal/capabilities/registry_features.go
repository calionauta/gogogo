// Feature units: the slices a project may switch off, install or remove.
//
// Split from registry.go for the file-size gate. They are appended to
// baseUnits at package init so every reader still sees one slice — the split
// is about where the literals live, not about a second registry.
package capabilities

var featureUnits = []Capability{
	{
		ID:       "todo",
		Kind:     KindFeature,
		Summary:  "Todo MVC demo (reference implementation)",
		Dirs:     []string{"features/todo"},
		Register: "features/todo/handlers",
		Actions:  []string{verbCreate, verbToggle, verbDelete, verbClear},
		ActionParams: map[string][]string{
			verbCreate: {"title", "idem_key", "visibility"},
		},
		ActionPathParams: map[string][]string{
			verbToggle: {"id"},
			verbDelete: {"id"},
		},
		Reason: "Reference implementation — remove manually later per docs/scope-taxonomy.md.",
	},
	{
		ID:        "chat",
		Kind:      KindFeature,
		Summary:   "conversation demo (persisted history + catalog answers on genui)",
		DependsOn: []string{idGenui},
		Dirs:      []string{"features/chat"},
		Register:  "features/chat/handlers",
		Actions:   []string{"ask", "clear", "history"},
		ActionParams: map[string][]string{
			"ask": {"prompt", "surfaces"},
		},
		ActionQuery: map[string][]string{
			"history": {"limit"},
		},
		Reason: "Bundled into the genui unit: chat imports genui components " +
			"(no-compile without it) — one unit is the honest shape, same as " +
			"whiteboard+collab+notes.",
	},
	{
		ID:        "review",
		Kind:      KindFeature,
		Summary:   "review loop demo (writer → critic → classifier judge, streamed verdict)",
		DependsOn: []string{"genkit"},
		Dirs:      []string{"features/review"},
		Register:  "features/review/handlers",
		Actions:   []string{"run", "verdict"},
		ActionParams: map[string][]string{
			"run":     {paramTab, paramContent},
			"verdict": {paramTab, paramContent},
		},
		Warns: []string{
			"The Review sidebar entry is stripped automatically.",
			"ci-local smoke: export SMOKE_FEATURES without review " +
				"(e.g. SMOKE_FEATURES=todo,room,login) or the harness fails on the removed route.",
		},
		Note: "No UISignal claim by design: the run UI gates on the " +
			"todo.Signals ReviewEnabled field at runtime (keyless hides " +
			"it), but the only .templ reader ships inside features/review " +
			"— claiming it would fail the removed-tree conformance, the " +
			"same reason SimulatedLLM is unclaimed. Removal drops the " +
			"sidebar entry, so no dead UI either way. " +
			"DependsOn lists genkit only: llm is never removed " +
			"(runtime-off is blessed), so declaring it would break " +
			"a bundle restore (no unit provides the dep) " +
			"for zero mechanical gain — the LLM requirement rides the " +
			"Summary and docs instead.",
	},
	{
		ID:         "goakt",
		Kind:       KindServant,
		Summary:    "entity actors (GoAkt: room lease + collaborative presence)",
		RuntimeOff: "GOAKT_ENABLED=false",
		Dirs:       []string{"internal/goakt", "features/room"},
		Files: []string{
			// Owned by no dir: the engine boot in the web entry point.
			// internal/server/chi.go is EDITED (line drops), not deleted.
			"cmd/web/goakt.go",
			"cmd/web/goakt_test.go",
			// The presence wiring's own test: it imports internal/goakt to boot
			// a real actor host and assert the seam routes to it, so a tree
			// with the engine deleted cannot compile it. Deleted with the
			// unit, which is honest — the claim it guards stops existing.
			"internal/server/presence_test.go",
		},
		Warns: []string{
			"Runtime alternative (no deletion): GOAKT_ENABLED=false.",
		},
		Note: "Deletion removes the engine + room demo; collaborative presence " +
			"(whiteboard AND notes) falls back to the in-process PeerSet behind the " +
			"same seam, so the cross-instance roster still works. The Room sidebar " +
			"entry is stripped automatically; main.go boot lines go with the unit's " +
			"mainStrips.",
	},
	{
		ID:      "room",
		Kind:    KindFeature,
		Summary: "room presence demo (one grain per room, crash hook included)",
		DependsOn: []string{
			depSounds,
		},
		Dirs:     []string{"features/room"},
		Register: "features/room/handlers",
		Actions:  []string{verbAcquire, "join", "leave", "release"},
		ActionQuery: map[string][]string{
			verbAcquire: {paramClientID},
			"join":      {paramClientID},
			"leave":     {paramClientID},
			"release":   {paramClientID},
		},
		Reason: "Delete the dir, the routes it mounts through the seam, and the Room entry in internal/ui/modules.go.",
		Warns: []string{
			"The Room sidebar entry is removed by hand with the unit.",
		},
	},
	{
		ID:      "whiteboard",
		Kind:    KindFeature,
		Summary: "collaborative canvas (Loro + Rough.js + presence)",
		DependsOn: []string{
			depCollab,
			depSounds,
		},
		Dirs:     []string{"features/whiteboard"},
		Register: "features/whiteboard/handlers",
		Actions:  []string{verbCreate, verbDelete, verbSnap, verbUpdate},
		ActionParams: map[string][]string{
			verbUpdate: {paramBody},
		},
		ActionPathParams: map[string][]string{
			verbDelete: {paramDocID},
			verbSnap:   {paramDocID},
			verbUpdate: {paramDocID},
		},
		ActionQuery: map[string][]string{
			verbUpdate: {paramClientID},
		},
		ActionRawBody:     map[string]string{verbUpdate: paramBody},
		ActionBrowserPath: map[string]string{verbCreate: "/whiteboard/new"},
		// The presence wiring is whiteboard+notes infrastructure: with both
		// gone there is no seam to build, no roster to federate, and no
		// collab import left in the server package. `AlsoWhenDropped` is what
		// makes that sentence true — without it this file was removed with
		// whiteboard alone, leaving notes (which still wires presence) failing
		// to build against a package that had just been deleted.
		Files:           []string{"internal/server/presence.go"},
		AlsoWhenDropped: []string{"notes"},
		Reason: "Delete the dir plus internal/server/presence.go and the " +
			"Whiteboard entry in internal/ui/modules.go (notes falls " +
			"back to the in-process PeerSet presence seam). presence.go goes " +
			"only when notes is also absent: it is the shared seam, so removing " +
			"it while notes survives breaks the build.",
		Warns: []string{
			"The Whiteboard sidebar entry is removed by hand with the unit.",
			"ci-local smoke: export SMOKE_FEATURES without whiteboard " +
				"(e.g. SMOKE_FEATURES=todo,room,login) or the harness fails on the removed route.",
		},
	},
	{
		ID:      "notes",
		Kind:    KindFeature,
		Summary: "shared plain-text notes (server-owned Loro Text + SSE)",
		DependsOn: []string{
			depCollab,
			depSounds,
		},
		Dirs:     []string{"features/notes"},
		Register: "features/notes/handlers",
		Actions:  []string{verbCreate, verbDelete, verbEdit, verbRead},
		ActionParams: map[string][]string{
			verbEdit: {paramBody},
		},
		ActionPathParams: map[string][]string{
			verbDelete: {paramDocID},
			verbEdit:   {paramDocID},
			verbRead:   {paramDocID},
		},
		ActionQuery: map[string][]string{
			verbEdit: {paramClientID},
		},
		ActionRawBody:     map[string]string{verbEdit: paramBody},
		ActionBrowserPath: map[string]string{verbCreate: "/notes/new"},
		Reason: "Delete the dir, the routes it mounts through the seam, and the " +
			"Notes entry in internal/ui/modules.go.",
		Warns: []string{
			"The Notes sidebar entry is removed by hand with the unit.",
		},
	},
}
