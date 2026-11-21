// Page index inside the curated agent entry point.
//
// TWO AGENT INDEXES EXISTED WITH NOTHING COMPARING THEM. llms.txt at the repo
// root is the file an agent is told to read FIRST (AGENTS.md points here), and
// it is written by hand: start-here, by-task, invariants. site/llms.txt is
// generated from the site MANIFEST. A page could be published for months and
// never be reachable from the index agents actually open, and the reverse — a
// curated entry naming a page that was renamed — failed only if someone read it.
//
// The fix is not a second hand-maintained list: it is a generated block, on the
// same contract as every other generated thing here (counts, unit tables, MCP
// tool tables). The markers below are the boundary; `go run ./cmd/check-docs
// -write` regenerates what is between them, and the guard fails on drift.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	pageIndexBegin = "<!-- page-index:begin -->"
	pageIndexEnd   = "<!-- page-index:end -->"
	pageIndexFile  = "llms.txt"
)

var baseRE = regexp.MustCompile(`(?m)^\s*base:\s*"([^"]+)"`)

// siteBase reads the absolute site URL from the config, so a generated link is
// the URL a reader (or an agent) can actually open.
func siteBase(root string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, siteConfig))
	if err != nil {
		return "", err
	}
	m := baseRE.FindStringSubmatch(string(raw))
	if m == nil {
		return "", fmt.Errorf("%s: no base URL", siteConfig)
	}
	return strings.TrimSuffix(m[1], "/"), nil
}

// renderPageIndex renders every published page, grouped in config order. One
// line per page: the title, the URL it lives at, the file that is its source,
// and the one-sentence description the manifest already carries.
func renderPageIndex(pages []sitePage, base string) string {
	var b strings.Builder
	b.WriteString(pageIndexBegin + "\n")
	b.WriteString("## Every published page\n\n")
	b.WriteString("Generated from " + siteConfig + " by `make docs-index`; the guard fails on drift.\n")
	b.WriteString("`docs/<file>` is the source, the URL is where it renders, and `docs/<file>.md`-style\n")
	b.WriteString("markdown copies are published beside each page for an agent that wants one page\n")
	b.WriteString("without the whole site.\n")
	group := ""
	for _, p := range pages {
		if p.Group != group {
			if group != "" {
				b.WriteString("\n")
			}
			b.WriteString("\n**" + p.Group + "**\n")
			group = p.Group
		}
		fmt.Fprintf(&b, "- [%s](%s/docs/%s/) — %s (`docs/%s`)\n", p.Slug, base, p.Slug, p.Desc, p.File)
	}
	b.WriteString(pageIndexEnd + "\n")
	return b.String()
}

// currentPageIndex returns the block, markers included; "" when llms.txt has
// none (a project that does not keep a curated index).
func currentPageIndex(root string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, pageIndexFile))
	if err != nil {
		return "", err
	}
	text := string(raw)
	start := strings.Index(text, pageIndexBegin)
	end := strings.Index(text, pageIndexEnd)
	if start < 0 || end < start {
		return "", nil
	}
	return text[start : end+len(pageIndexEnd)+1], nil
}

// checkPageIndex reports a page index that drifted from the manifest.
func checkPageIndex(root string) []string {
	pages, err := manifestPages(root)
	if err != nil {
		return []string{err.Error()}
	}
	base, err := siteBase(root)
	if err != nil {
		return []string{err.Error()}
	}
	got, err := currentPageIndex(root)
	if err != nil {
		return []string{err.Error()}
	}
	if got == "" {
		return nil
	}
	if got != renderPageIndex(pages, base) {
		return []string{pageIndexFile + ": the page index does not match " + siteConfig +
			" — run `make docs-index` (the block is generated, never hand-edited)"}
	}
	return nil
}

// writePageIndex replaces the block in place.
func writePageIndex(root string) error {
	pages, err := manifestPages(root)
	if err != nil {
		return err
	}
	base, err := siteBase(root)
	if err != nil {
		return err
	}
	full := filepath.Join(root, pageIndexFile)
	raw, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	info, err := os.Stat(full)
	if err != nil {
		return err
	}
	text := string(raw)
	start := strings.Index(text, pageIndexBegin)
	end := strings.Index(text, pageIndexEnd)
	if start < 0 || end < start {
		return nil
	}
	updated := text[:start] + renderPageIndex(pages, base) + text[end+len(pageIndexEnd)+1:]
	if updated == text {
		return nil
	}
	//nolint:gosec // G703: the only relative name reaching this call is the
	// constant llms.txt above; `root` is the module root this process ran in.
	return os.WriteFile(full, []byte(updated), info.Mode().Perm())
}
