package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRemoveModuleEntryDropsBothShapes covers the two entry layouts the module
// registry uses and the viewer lookup beside it: an uninstall that misses one
// leaves the rail linking to a page whose package is gone.
func TestRemoveModuleEntryDropsBothShapes(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `package ui

var Modules = []Module{
	{
		ID:    "notifications", Title: "Inbox", Pinned: true,
		Icon:  "material-symbols:notifications", URL: "/notifications",
	},
	{ID: "todo", Title: "Todo", Group: GroupCollab, URL: "/todo"},
	{ID: "notes", Title: "Notes", Group: GroupCollab, URL: "/notes"},
}

var viewerModules = map[string]bool{
	"todo":  true,
	"notes": true,
}
`
	if err := os.WriteFile(filepath.Join(root, "internal", "ui", "modules.go"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	changed, err := removeModuleEntry(root, "todo")
	if err != nil {
		t.Fatalf("removeModuleEntry: %v", err)
	}
	if !changed {
		t.Fatal("removeModuleEntry reported no change")
	}
	got, err := os.ReadFile(filepath.Join(root, "internal", "ui", "modules.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if strings.Contains(text, "\"todo\"") {
		t.Errorf("todo entry survived:\n%s", text)
	}
	for _, want := range []string{"notifications", "notes"} {
		if !strings.Contains(text, "\""+want+"\"") {
			t.Errorf("%s was removed with it:\n%s", want, text)
		}
	}

	// A multi-line entry goes too.
	changed, err = removeModuleEntry(root, "notifications")
	if err != nil {
		t.Fatalf("removeModuleEntry multiline: %v", err)
	}
	if !changed {
		t.Fatal("multi-line entry was not removed")
	}
	got, _ = os.ReadFile(filepath.Join(root, "internal", "ui", "modules.go"))
	if strings.Contains(string(got), "notifications") {
		t.Errorf("notification entry survived:\n%s", got)
	}
}

// TestUninstallDropsTheCardAndTheNavEntry is the "is it spread anywhere else"
// answer: install writes a package, a receipt, a sidebar line and a domain
// card, so uninstall must undo all four or the next commit is red.
func TestUninstallDropsTheCardAndTheNavEntry(t *testing.T) {
	root := newRecipeCheckout(t)
	dir := newNavRecipeDir(t, true)
	if out, err := runRecipeCLI(t, "install", dir, "--dir", root, "--yes"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "domain"), 0o755); err != nil {
		t.Fatal(err)
	}
	card := filepath.Join(root, "docs", "domain", "timer.md")
	cardBody := "## Owns\n\nthe timer\n\n## Does not own\n\nnothing\n\n" +
		"## Language\n\nclock\n\n## Invariants\n\nruns once\n"
	if err := os.WriteFile(card, []byte(cardBody), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runRecipeCLI(t, "uninstall", "timer", "--dir", root, "--yes")
	if err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	if _, err := os.Stat(card); !os.IsNotExist(err) {
		t.Errorf("domain card survived the uninstall (check-domains would refuse it): %v", err)
	}
	if !strings.Contains(out, "card:") {
		t.Errorf("uninstall did not report the card removal:\n%s", out)
	}
}
