// SCOPE:core — README page index generated from the site manifest.
//
// The README's page index duplicates the docs site manifest. It drifted twice
// before this file existed (it covered 23 of 43 pages, and still claimed to
// mirror `site/build.mjs` after the manifest moved to the site config), because
// nothing recomputed it: a new page was added to the manifest, published, and
// never linked from the one page a GitHub visitor reads first.
//
// Rule: the index is a generated fact, like the counts. The manifest is the
// source; `-write` regenerates the block between the markers and the check
// fails on any other edit.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	indexBegin = "<!-- docs-index:begin -->"
	indexEnd   = "<!-- docs-index:end -->"
)

// siteConfig is the only file that names docs pages since the site engine
// became a vendored package (site/build.mjs is the engine, not the map).
const siteConfig = "site/docsite.config.mjs"

// manifestPageRE matches one manifest entry. `\s*` spans the newlines that
// make the entries readable in the config (`dagnats` puts `file:` on its own
// line), and the manifest is cut out of the file first so the `releases:`
// block — which also has `file:` — never matches.
var manifestPageRE = regexp.MustCompile(
	`(?s)\{\s*group:\s*"([^"]*)",\s*slug:\s*"([^"]*)",\s*file:\s*"([^"]*)",\s*desc:\s*"([^"]*)"`)

// sitePage is one manifest entry.
type sitePage struct {
	Group string
	Slug  string
	File  string
	Desc  string
}

// manifestPages reads the manifest in config order.
func manifestPages(root string) ([]sitePage, error) {
	raw, err := os.ReadFile(filepath.Join(root, siteConfig))
	if err != nil {
		return nil, err
	}
	text := string(raw)
	start := strings.Index(text, "manifest: [")
	if start < 0 {
		return nil, fmt.Errorf("%s: manifest not found", siteConfig)
	}
	var pages []sitePage
	for _, m := range manifestPageRE.FindAllStringSubmatch(text[start:], -1) {
		pages = append(pages, sitePage{Group: m[1], Slug: m[2], File: m[3], Desc: m[4]})
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("%s: manifest has no parsable page entry", siteConfig)
	}
	return pages, nil
}

// renderIndex renders the generated block, groups in config order.
func renderIndex(pages []sitePage) string {
	var b strings.Builder
	b.WriteString(indexBegin + "\n")
	group := ""
	for _, p := range pages {
		if p.Group != group {
			if group != "" {
				b.WriteString("\n")
			}
			b.WriteString("**" + p.Group + "**\n")
			group = p.Group
		}
		fmt.Fprintf(&b, "- [%s](docs/%s) — %s\n", p.Slug, p.File, p.Desc)
	}
	b.WriteString(indexEnd + "\n")
	return b.String()
}

// readmeIndex returns the current block, markers included.
func readmeIndex(root string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		return "", err
	}
	text := string(raw)
	start := strings.Index(text, indexBegin)
	end := strings.Index(text, indexEnd)
	if start < 0 || end < start {
		return "", fmt.Errorf("README.md: page index markers %s / %s missing", indexBegin, indexEnd)
	}
	return text[start : end+len(indexEnd)+1], nil
}

// checkReadmeIndex reports a stale or missing index.
func checkReadmeIndex(root string) []string {
	pages, err := manifestPages(root)
	if err != nil {
		return []string{err.Error()}
	}
	got, err := readmeIndex(root)
	if err != nil {
		return []string{err.Error()}
	}
	if got != renderIndex(pages) {
		return []string{"README.md: page index does not match " + siteConfig +
			" — run `make docs-index` (the index is generated, never hand-edited)"}
	}
	return nil
}

// writeReadmeIndex replaces the block in place, so `make docs-index` is the
// one path that updates it.
func writeReadmeIndex(root string) error {
	pages, err := manifestPages(root)
	if err != nil {
		return err
	}
	rel := "README.md"
	full := filepath.Join(root, rel)
	raw, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	// Keep whatever mode the file already has: this rewrites a tracked file, and
	// a fresh permission of its own would show up as a mode change in git.
	info, err := os.Stat(full)
	if err != nil {
		return err
	}
	text := string(raw)
	start := strings.Index(text, indexBegin)
	end := strings.Index(text, indexEnd)
	if start < 0 || end < start {
		return fmt.Errorf("%s: page index markers %s / %s missing", rel, indexBegin, indexEnd)
	}
	updated := text[:start] + renderIndex(pages) + text[end+len(indexEnd)+1:]
	if updated == text {
		return nil
	}
	//nolint:gosec // G703: the only relative name reaching this call is the
	// constant README.md below; `root` is the module root this process ran in.
	return os.WriteFile(full, []byte(updated), info.Mode().Perm())
}
