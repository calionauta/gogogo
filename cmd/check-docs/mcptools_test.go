// Tests for the generated MCP tool tables.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMCPToolsAreReadFromTheCatalog pins the reader: the tool set comes from
// the catalog the server serves, with its name, description, parameters and
// safety verdict — the four facts the documentation tables carry.
func TestMCPToolsAreReadFromTheCatalog(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	tools, err := mcpTools(root)
	if err != nil {
		t.Fatalf("read the catalog: %v", err)
	}
	if len(tools) < 10 {
		t.Fatalf("read %d tools from the catalog, want the served surface", len(tools))
	}
	seen := map[string]bool{}
	for _, tool := range tools {
		if tool.name == "" || tool.desc == "" || tool.params == "" || tool.effect == "" {
			t.Errorf("tool %q is under-described: %+v", tool.name, tool)
		}
		if seen[tool.name] {
			t.Errorf("duplicate tool %q", tool.name)
		}
		seen[tool.name] = true
	}
	// A description written as concatenated literals must be read whole: the
	// first piece alone would be a different sentence than the tool serves.
	for _, tool := range tools {
		if tool.name == "recipe_describe" && !strings.Contains(tool.desc, "metrics/outcomes") {
			t.Errorf("recipe_describe description was truncated: %q", tool.desc)
		}
	}
}

// TestMCPToolsTableIsCurrent is the gate itself: the tables in the docs and
// in the module README must match the catalog. A tool that changed its
// description and left the prose behind is exactly the drift this exists for.
func TestMCPToolsTableIsCurrent(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	if problems := checkMCPTools(root); len(problems) > 0 {
		t.Errorf("the generated MCP tables are stale: %v", problems)
	}
}

// TestMCPToolsDetectsAStaleTable proves the gate fails when the table and
// the catalog disagree — the red half, so the green above means something.
func TestMCPToolsDetectsAStaleTable(t *testing.T) {
	root := t.TempDir()
	catalog := `package main

var toolCatalog = []toolDef{
	{
		name:   "one_tool",
		desc:   "The first sentence. And the second.",
		params: "dir",
		effect: "No",
		add:    nil,
	},
}
`
	writeFixture(t, root, "cmd/gogogo-mcp/catalog.go", catalog)
	for _, doc := range mcpToolsDocs {
		body := mcpToolsStart + "\n| Tool | Changes anything? | Params | What it does |\n|---|---|---|---|\n" +
			"| `one_tool` | No | `dir` | The first sentence. |\n" + mcpToolsEnd + "\n"
		writeFixture(t, root, doc.file, body)
	}
	problems := checkMCPTools(root)
	if len(problems) == 0 {
		t.Fatal("a table that lost half a description must be reported as stale")
	}
	for _, p := range problems {
		if !strings.Contains(p, "stale") {
			t.Errorf("problem %q does not name the drift", p)
		}
	}
}

// writeFixture writes one file under a root, creating the directories.
func writeFixture(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
