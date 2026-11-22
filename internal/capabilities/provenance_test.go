package capabilities

import (
	"os"
	"path/filepath"
	"testing"
)

// The record exists so a gate can tell "this tree deliberately dropped chat"
// apart from "chat is missing". Both halves of that distinction are pinned
// here, because a reader that answered "trimmed" too eagerly would silence a
// real removal.

func TestParseTrimProvenanceReadsBothRecords(t *testing.T) {
	text := `# AGENTS

## Trim provenance

scaffolded-from: calionauta/gogogo@v1
removed: chat, kiosk
removed-paths: features/chat, features/kiosk, internal/server/genui_stubs_test.go

## Next section

removed: this-is-not-provenance
`
	got := ParseTrimProvenance(text)
	if !got.Recorded {
		t.Fatal("section present but not recorded")
	}
	for _, id := range []string{"chat", "kiosk"} {
		if !got.Removed[id] {
			t.Errorf("removed: lost %q (%v)", id, got.Removed)
		}
	}
	// A record that belongs to a LATER section must not leak in: the reader
	// stops at the next `## ` heading, so a `removed:` line elsewhere in a long
	// AGENTS.md cannot widen the skip set.
	if got.Removed["this-is-not-provenance"] {
		t.Error("a later section's `removed:` line leaked into the record")
	}
	for _, p := range []string{"features/chat", "internal/server/genui_stubs_test.go"} {
		if !got.Covers(p) {
			t.Errorf("removed-paths: lost %q (%v)", p, got.Paths)
		}
	}
}

func TestParseTrimProvenanceAbsentSectionIsStrict(t *testing.T) {
	got := ParseTrimProvenance("# AGENTS\n\n## Architecture\n\nremoved: chat\n")
	if got.Recorded {
		t.Error("no `## Trim provenance` section must not be recorded")
	}
	if got.Covers("features/chat") {
		t.Error("a tree with no record is checked strictly: nothing is covered")
	}
}

// TestCoversDirectoryCoversItsBeneath is the rule the gate relies on: the
// record names `features/chat`, the registry names `features/chat/handlers`.
func TestCoversDirectoryCoversItsBeneath(t *testing.T) {
	got := ParseTrimProvenance("## Trim provenance\n\nremoved-paths: features/chat\n")
	for _, beneath := range []string{"features/chat", "features/chat/handlers", "features/chat/x/y.go"} {
		if !got.Covers(beneath) {
			t.Errorf("%q should be covered by features/chat", beneath)
		}
	}
	// The prefix rule must not become a substring rule: chatty is not chat.
	for _, other := range []string{"features/chatty", "features/chat-extra"} {
		if got.Covers(other) {
			t.Errorf("%q must not be covered by features/chat", other)
		}
	}
}

func TestCoversAllNeedsEveryDirectory(t *testing.T) {
	got := ParseTrimProvenance("## Trim provenance\n\nremoved-paths: features/room\n")
	if got.CoversAll([]string{"features/room", "internal/goakt"}) {
		t.Error("one surviving directory means the slice is still here")
	}
	if !got.CoversAll([]string{"features/room"}) {
		t.Error("every directory removed means the slice is gone")
	}
	// A unit with no directories is not covered by an empty loop.
	if got.CoversAll(nil) {
		t.Error("a unit owning no directories cannot be covered")
	}
}

func TestReadTrimProvenanceReadsTheTree(t *testing.T) {
	dir := t.TempDir()
	agents := "## Trim provenance\n\nremoved: chat\nremoved-paths: features/chat\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(agents), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ReadTrimProvenance(dir); !got.Covers("features/chat") {
		t.Fatalf("record not read from disk: %+v", got)
	}
	// No AGENTS.md at all is strict, not an error: a hand-made tree has no
	// record and must still be checkable.
	if got := ReadTrimProvenance(t.TempDir()); got.Recorded || got.Covers("features/chat") {
		t.Errorf("a tree with no AGENTS.md must be strict, got %+v", got)
	}
}
