// SCOPE:core — Tests for the measured Client weight rule and its writer.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestClientWeightRowMatches pins what the row regex accepts: a measured row,
// never the header, the separator, or prose that happens to have pipes.
func TestClientWeightRowMatches(t *testing.T) {
	for _, tc := range []struct {
		line   string
		label  string
		files  string
		wantOK bool
	}{
		{"| Shared notes | `notes.js` | 36 KB | 13 KB |", "Shared notes", "`notes.js`", true},
		{
			"| Landing, Config | inline only (no extra JS) | 0 KB | 0 KB |",
			"Landing, Config", "inline only (no extra JS)", true,
		},
		{"| Feature | Files | Raw | Gzip |", "", "", false},
		{"|---|---|---|---|", "", "", false},
		{"A row without a KB cell is prose, not a measurement.", "", "", false},
	} {
		m := clientWeightRowRE.FindStringSubmatch(tc.line)
		if !tc.wantOK {
			if m != nil {
				t.Errorf("clientWeightRowRE(%q) = %v, want no match", tc.line, m)
			}
			continue
		}
		if m == nil {
			t.Errorf("clientWeightRowRE(%q) did not match", tc.line)
			continue
		}
		if m[1] != tc.label || m[2] != tc.files {
			t.Errorf("clientWeightRowRE(%q) = label %q files %q, want %q / %q",
				tc.line, m[1], m[2], tc.label, tc.files)
		}
	}
}

// TestMeasureRow sums what a cell names and ignores spans that are prose. A
// directory token counts the module graph it holds, because the page fetches
// every file in it.
func TestMeasureRow(t *testing.T) {
	root := t.TempDir()
	write := func(rel string, size int) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("web/resources/static/a.js", 1000)
	write("web/resources/static/lib/one.js", 2048)
	write("web/resources/static/lib/two.js", 1)
	write("web/resources/static/lib/readme.md", 999)
	write("features/notes/static/b.js", 25)

	for _, tc := range []struct {
		cell  string
		bytes int64
		names int
	}{
		{"`a.js` + `b.js` (two roots)", 1025, 2},
		{"`web/resources/static/lib/` (the whole module graph)", 2049, 1},
		{"inline only (no extra JS)", 0, 0},
		{"`internal/auth` is prose here, not a script", 0, 0},
	} {
		raw, gz, names, err := measureRow(root, tc.cell)
		if err != nil {
			t.Fatalf("measureRow(%q): %v", tc.cell, err)
		}
		if raw != tc.bytes {
			t.Errorf("measureRow(%q) raw = %d bytes, want %d", tc.cell, raw, tc.bytes)
		}
		if gz <= 0 && tc.bytes > 0 {
			t.Errorf("measureRow(%q) gzip = %d, want a positive size", tc.cell, gz)
		}
		if len(names) != tc.names {
			t.Errorf("measureRow(%q) resolved %d tokens, want %d", tc.cell, len(names), tc.names)
		}
	}
}

// TestCheckClientWeightFlagsDrift is the red proof for both quotes: a table
// cell that disagrees with the files and a landing span that disagrees with the
// table are each reported, and `-write` clears both.
func TestCheckClientWeightFlagsDrift(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"web/resources/static/notes.js": strings.Repeat("x", 36*bytesPerKB),
		"docs/features.md": "## Client weight\n\n| Feature | Files | Raw | Gzip |\n|---|---|---|---|\n" +
			"| Shared notes | `notes.js` | 28 KB | 10 KB |\n",
		"site/index.html": "<div class=\"grid-2\">\n" +
			"  <p class=\"step\">Shared notes</p>\n" +
			"  <p class=\"tech\" data-weight=\"Shared notes\">~1 KB raw · ~1 KB gzip</p>\n" +
			"</div>\n",
	}
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	problems := checkClientWeight(root)
	if len(problems) != 2 {
		t.Fatalf("checkClientWeight with stale quotes = %v, want two problems", problems)
	}
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"the table says 28 KB", "data-weight=\"Shared notes\""} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems %q do not name %q", joined, want)
		}
	}

	if err := writeClientWeight(root); err != nil {
		t.Fatalf("writeClientWeight: %v", err)
	}
	if problems := checkClientWeight(root); len(problems) != 0 {
		t.Errorf("after -write, checkClientWeight = %v, want none", problems)
	}
	page, err := os.ReadFile(filepath.Join(root, "site", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "~36 KB raw") {
		t.Errorf("the landing span was not regenerated:\n%s", page)
	}
}

// TestAssetFilesRejectsUnknown keeps a typo loud: a script name no asset
// directory holds is a dead reference, not a zero-weight row.
func TestAssetFilesRejectsUnknown(t *testing.T) {
	root := t.TempDir()
	if _, err := assetFiles(root, "missing.js"); err == nil {
		t.Error("assetFiles(missing.js) = nil error, want a failure")
	}
	if _, err := assetFiles(root, "web/resources/static/gone/"); err == nil {
		t.Error("assetFiles(gone/) = nil error, want a failure")
	}
}
