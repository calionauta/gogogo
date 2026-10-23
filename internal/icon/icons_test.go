// SCOPE:core — Icon catalog guards.
//
// The catalog is invisible when it is wrong: an unknown name renders an
// empty <svg>, and a module whose Icon is missing renders a blank rail row.
// Both tests below make that loud instead of visual.
package icon_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/icon"
	"github.com/calionauta/gogogo/internal/ui"
)

// TestIconCatalogCoversModules pins the dynamic call site: shell.templ
// renders @Icon(m.Icon) for every registry entry, so every Modules[].Icon
// must resolve. A new module with an unvendored glyph fails here, not as a
// blank sidebar row.
func TestIconCatalogCoversModules(t *testing.T) {
	for _, m := range ui.Modules {
		var buf bytes.Buffer
		if err := icon.Icon(m.Icon, "test").Render(context.Background(), &buf); err != nil {
			t.Fatalf("render %q: %v", m.Icon, err)
		}
		if !strings.Contains(buf.String(), "<path") {
			t.Errorf("module %q icon %q renders no glyph — vendor it in internal/icon/icons.go", m.ID, m.Icon)
		}
	}
}

// TestIconRendersSVG pins the wrapper contract: a 1em currentColor svg,
// decorative (aria-hidden), with the caller's class passed through. An
// unknown name renders the empty shell — never a broken tag.
func TestIconRendersSVG(t *testing.T) {
	var buf bytes.Buffer
	if err := icon.Icon("material-symbols:menu", "app-rail-icon").Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()
	for _, want := range []string{
		`<svg`, `width="1em"`, `fill="currentColor"`, `class="app-rail-icon"`, `aria-hidden="true"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("icon markup missing %q:\n%s", want, html)
		}
	}

	buf.Reset()
	if err := icon.Icon("material-symbols:no-such-glyph", "x").Render(context.Background(), &buf); err != nil {
		t.Fatalf("render unknown: %v", err)
	}
	if strings.Contains(buf.String(), "<path") {
		t.Error("unknown glyph must render an empty svg, not a path")
	}
}

// TestIconLiteralsResolve scans every .templ for Icon("...") literals and
// requires each to exist in the catalog. The dynamic site (m.Icon) is
// covered by TestIconCatalogCoversModules; this covers the hand-written
// ones (drawer toggle, demo button, theme/sound toggles, delete buttons),
// where a typo would otherwise render an empty button.
//
// red: an Icon("material-symbols:menuu") typo renders an empty toggle.
func TestIconLiteralsResolve(t *testing.T) {
	root := filepath.Join("..", "..")
	var templPaths []string
	for _, dir := range []string{"features", "web", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".templ") {
				return err
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			templPaths = append(templPaths, rel)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	// Read outside the walk callback: gosec G122 forbids filesystem
	// operations on walked paths inside it (symlink TOCTOU).
	checked := 0
	for _, rel := range templPaths {
		raw, rerr := os.ReadFile(filepath.Join(root, rel))
		if rerr != nil {
			t.Fatalf("read %s: %v", rel, rerr)
		}
		for line := range strings.SplitSeq(string(raw), "\n") {
			rest := line
			for {
				i := strings.Index(rest, `Icon("material-symbols:`)
				if i < 0 {
					break
				}
				name := "material-symbols:" + rest[i+len(`Icon("material-symbols:`):]
				end := strings.Index(name, `"`)
				if end < 0 {
					break
				}
				name = name[:end]
				rest = rest[i+len(`Icon("`)+end:]
				checked++
				var buf bytes.Buffer
				if err := icon.Icon(name, "test").Render(context.Background(), &buf); err != nil {
					t.Fatalf("render %q: %v", name, err)
				}
				if !strings.Contains(buf.String(), "<path") {
					t.Errorf("%s uses Icon(%q) with no catalog entry — vendor it or fix the typo", rel, name)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no Icon(\"...\") literals found — the scan matched nothing to check")
	}
}
