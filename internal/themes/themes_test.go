// SCOPE:core — Theme catalog contract.
//
// The catalog here, the `themes:` list compiled by src/css/input.css and the
// picker are three views of one fact. The CSS side is the one that silently
// wins: a name in the catalog that the bundle does not compile renders as the
// default palette, so the selector looks fine and changes nothing. These tests
// make that failure a red build instead of a visual shrug.
package themes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// compiledThemes parses the `themes:` list out of the Tailwind/DaisyUI plugin
// block — the only place a theme becomes real.
func compiledThemes(t *testing.T) []string {
	t.Helper()
	path := filepath.Join("..", "..", "src", "css", "input.css")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	const marker = "themes:"
	i := strings.Index(string(body), marker)
	if i < 0 {
		t.Fatalf("%s: no %q list in the daisyui plugin block", path, marker)
	}
	rest := string(body)[i+len(marker):]
	end := strings.IndexAny(rest, ";\n")
	if end < 0 {
		end = len(rest)
	}
	var names []string
	for part := range strings.SplitSeq(rest[:end], ",") {
		name := strings.TrimSpace(strings.Fields(strings.TrimSpace(part))[0])
		if name == "" {
			continue
		}
		// `light --default` / `dark --prefersdark` carry flags, not more names.
		if j := strings.Index(name, "--"); j > 0 {
			name = name[:j]
		}
		names = append(names, name)
	}
	return names
}

// TestCatalogMatchesCompiledThemes is the drift guard in both directions: a
// theme in the catalog but not in the bundle is a dead option; a theme in the
// bundle but not in the catalog is unreachable from the UI.
func TestCatalogMatchesCompiledThemes(t *testing.T) {
	compiled := compiledThemes(t)
	have := map[string]bool{}
	for _, name := range compiled {
		have[name] = true
	}
	for _, th := range All {
		if !have[th.Name] {
			t.Errorf("theme %q is in the catalog but src/css/input.css does not compile it "+
				"(it would render as the default palette)", th.Name)
		}
	}
	known := map[string]bool{}
	for _, th := range All {
		known[th.Name] = true
	}
	for _, name := range compiled {
		if !known[name] {
			t.Errorf("src/css/input.css compiles %q but internal/themes does not list it "+
				"(no picker option reaches it)", name)
		}
	}
}

// TestCatalogIsWellFormed pins what the picker and the JS bootstrap rely on.
func TestCatalogIsWellFormed(t *testing.T) {
	if len(All) == 0 {
		t.Fatal("empty theme catalog")
	}
	seen := map[string]bool{}
	for _, th := range All {
		if th.Name == "" || th.Label == "" {
			t.Errorf("theme %+v needs both a Name and a Label", th)
		}
		if strings.ContainsAny(th.Name, " \t\"'") {
			t.Errorf("theme name %q must be a bare identifier (it becomes data-theme and a "+
				"sessionStorage value)", th.Name)
		}
		if seen[th.Name] {
			t.Errorf("theme %q listed twice", th.Name)
		}
		seen[th.Name] = true
		if th.Dark != IsDark(th.Name) {
			t.Errorf("IsDark(%q) disagrees with the catalog entry", th.Name)
		}
		if !Is(th.Name) {
			t.Errorf("Is(%q) = false for a listed theme", th.Name)
		}
	}
	if Is("nope") || IsDark("nope") {
		t.Error("unknown theme names must not resolve")
	}
}

// TestDarkNamesMatchesCatalog keeps the browser's copy honest: data-theme-dark
// is the only place the JS learns which palettes flip the chrome.
func TestDarkNamesMatchesCatalog(t *testing.T) {
	got := strings.Fields(DarkNames())
	var want []string
	for _, th := range All {
		if th.Dark {
			want = append(want, th.Name)
		}
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("DarkNames() = %v, want %v", got, want)
	}
	for _, name := range got {
		if !IsDark(name) {
			t.Errorf("DarkNames() lists %q but IsDark(%q) is false", name, name)
		}
	}
}
