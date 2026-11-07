// Installer engine: the generated AGENTS.md must keep the pin.
package installer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGeneratedAgentsKeepsTheToolchainPin is the regression test for a proof
// that fails on every real scaffold. writeAgents REPLACES the template's
// AGENTS.md, and internal/toolchain_pin_test.go requires that file to name the
// Go version go.mod pins. The generated file said nothing about the toolchain,
// so the proof ran a test its own output had just broken — and the failure read
// as "the trim broke the build", which is the false positive Spike 7 recorded.
func TestGeneratedAgentsKeepsTheToolchainPin(t *testing.T) {
	const version = "1.27.3"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module example.com/app\n\ngo "+version+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeAgents(context.Background(), dir, "my-app", nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	// The pin test asks for the MINOR form ("Go 1.27", the level AGENTS.md
	// speaks elsewhere); a full patch contains it. Assert the same form, so
	// this test fails for exactly the reason the proof would.
	minor := version[:strings.LastIndex(version, ".")]
	if !strings.Contains(string(raw), "Go "+minor) {
		t.Fatalf("generated AGENTS.md does not name Go %s.\n"+
			"internal/toolchain_pin_test.go fails a tree whose AGENTS.md omits the "+
			"pin, so every real scaffold would fail its own proof.\n---\n%s", minor, raw)
	}
}

// TestGeneratedAgentsWithoutGoMod pins the fixture path: a synthetic tree has no
// go.mod, and writeAgents must still succeed rather than fail the scaffold over a
// line it cannot know.
func TestGeneratedAgentsWithoutGoMod(t *testing.T) {
	dir := t.TempDir()
	if err := writeAgents(context.Background(), dir, "my-app", nil); err != nil {
		t.Fatalf("a tree without go.mod must still get AGENTS.md: %v", err)
	}
}
