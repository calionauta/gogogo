// SCOPE:core — reference links inside skills (part of the skill-frontmatter gate).
//
// A skill's body names files by relative path, and the important ones cross
// skill boundaries: gogogo-ui's "MUST read" list points at
// `../gogogo-coding-standards/references/datastar.md`, and daisyui-templ.md
// points back at it. The registry check only proves the .agents/skills symlinks
// resolve; nothing verified the files those instructions name. A rename would
// leave three skills telling the reader to open a file that is not there — and
// an agent cannot fall back to a URL, because a skill's references only resolve
// as relative paths.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// [text](target) — a markdown link.
	mdLinkRE = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	// `path/to/file.md` — how a skill tells a reader to open a file. These are
	// not links, so a markdown-link checker would miss exactly the paths that
	// matter most here.
	codePathRE = regexp.MustCompile("`([^`\\s]+\\.md)`")
	// Fenced code blocks hold examples, not references.
	fenceRE = regexp.MustCompile("(?s)```.*?```")
)

// checkLinks reports every skill-relative markdown path that does not resolve to
// a file, trying the file's own directory first and the repo root second (both
// conventions appear in skills: `references/x.md` and `docs/x.md`).
//
// Vendored skills are skipped: a directory carrying UPSTREAM_SHA is a
// byte-identical copy whose links describe the upstream layout, and editing it
// here would make the pin a lie. (stelow-workflow-coding-standards links to
// ../stelow-workflow-entry/SKILL.md, a sibling that exists upstream and was not
// vendored — a real defect, but one to fix upstream and refresh, not here.)
func checkLinks(root string, skills []string) []problem {
	var problems []problem
	for _, skillPath := range skills {
		if isVendored(filepath.Dir(skillPath)) {
			continue
		}
		skillDir := filepath.Dir(skillPath)
		for _, file := range skillMarkdownFiles(skillPath) {
			// #nosec G304 -- local dev tool; files come from the skills walk.
			raw, err := os.ReadFile(file)
			if err != nil {
				problems = append(problems, problem{path: file, msg: err.Error()})
				continue
			}
			for _, link := range skillLinks(string(raw)) {
				if !resolves(root, skillDir, filepath.Dir(file), link) {
					problems = append(problems, problem{
						path: file,
						msg:  fmt.Sprintf("link %q does not resolve (missing file)", link),
					})
				}
			}
		}
	}
	return problems
}

// isVendored reports whether a skill directory carries the UPSTREAM_SHA marker
// written by the refresh scripts.
func isVendored(dir string) bool {
	// #nosec G703 -- local dev tool; dir comes from the repo's own skills walk.
	_, err := os.Stat(filepath.Join(dir, "UPSTREAM_SHA"))
	return err == nil
}

// skillMarkdownFiles is SKILL.md plus its references/*.md.
func skillMarkdownFiles(skillPath string) []string {
	out := []string{skillPath}
	refs, err := filepath.Glob(filepath.Join(filepath.Dir(skillPath), "references", "*.md"))
	if err == nil {
		out = append(out, refs...)
	}
	return out
}

// skillLinks returns the relative paths a skill body points at, in either form
// (markdown link or backticked path), with code fences removed first.
func skillLinks(src string) []string {
	src = fenceRE.ReplaceAllString(src, "")
	// Capacity hint derived from the text instead of a magic number: markdown
	// links are the common form, backticked paths the rest.
	out := make([]string, 0, strings.Count(src, "]("))
	for _, m := range mdLinkRE.FindAllStringSubmatch(src, -1) {
		out = append(out, m[1])
	}
	for _, m := range codePathRE.FindAllStringSubmatch(src, -1) {
		out = append(out, m[1])
	}
	return out
}

// resolves reports whether link names something that exists. Three bases are
// tried, because all three conventions appear in skills: the file's own
// directory (`../daisyui/SKILL.md`), the skill root (a references/ file naming
// `references/other.md`), and the repo root (`docs/ui.md`). External URLs,
// anchors, globs and template placeholders are not local references and pass.
func resolves(root, skillDir, fileDir, link string) bool {
	if i := strings.IndexByte(link, '#'); i >= 0 {
		link = link[:i]
	}
	if link == "" || strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") ||
		strings.HasPrefix(link, "mailto:") || strings.HasPrefix(link, "#") {
		return true
	}
	if strings.ContainsAny(link, "*{}<>$") {
		return true // a glob or a placeholder in an example
	}
	link = strings.TrimPrefix(link, "/")
	for _, base := range []string{fileDir, skillDir, root} {
		// #nosec G703 -- local dev tool; link comes from the repo's own markdown.
		if _, err := os.Stat(filepath.Join(base, link)); err == nil {
			return true
		}
	}
	return false
}
