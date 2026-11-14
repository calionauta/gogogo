// Bare repo paths in the index pages.
//
// The backtick rule (checkPaths) only sees a path a writer marked as code, and
// that is exactly how a reference to a deleted file survived in llms.txt for
// weeks: `internal/installer/trim_presets.go` was written as plain prose, the
// file had been deleted with the trim operation, and nothing looked at it. An
// agent reading the index then chased a file that does not exist.
//
// A page that is a path index makes the opposite bet: every repo path on it is
// an address, not an example, so it is checked whether or not it is backticked.
// The list is deliberately short — those pages are indexes, and prose elsewhere
// may legitimately name a path that only exists after a build or in a
// scaffolded tree.
package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// indexPages are the pages whose repo paths are addresses: the agent index and
// the checkout's own instructions.
var indexPages = []string{"llms.txt", "AGENTS.md"}

// codeSpanRE strips an inline code span, so a backticked path is checked once
// (by checkPaths, which also applies the trimmed/generated exemptions).
var codeSpanRE = regexp.MustCompile("`[^`\n]*`")

// barePathRE matches a repo FILE path written without backticks. The leading
// boundary keeps it from matching the tail of a URL or a word, and requiring a
// tracked extension keeps prose like "docs/prose" or "web/desktop" out of the
// rule: a file is an address, a slash-joined phrase is not.
//
// Built from the same two lists the backtick rule uses, so the two cannot drift
// on what counts as a directory or a tracked file type.
var barePathRE = regexp.MustCompile(
	`(?:^|[\s(,])((?:` + strings.Join(repoRoots, "|") + `|skills)/` +
		`[A-Za-z0-9_./-]+\.(?:` + trackedFileExts + `))`,
)

// bareBundleDirRE matches a bundle directory written without backticks:
// `catalog/<id>` or `features/<id>`. Those roots
// roots are address spaces — a name under them is a thing that exists, not a
// phrase — which is what separates `catalog/kiosk` (a claim, and a false one)
// from `docs/prose` (prose). Only two segments, so a longer example path with a
// placeholder is left to the file rule.
var bareBundleDirRE = regexp.MustCompile(
	`(?:^|[\s(,])((?:catalog|features)/[A-Za-z0-9_-]+)(?:[\s,;.)]|$)`,
)

// checkBarePaths reports a dead repo path on an index page that was not written
// as inline code. Only indexPages are scanned: elsewhere the bare-path form is
// normally an example, and flagging it would train writers to ignore the gate.
func checkBarePaths(root string, files []string, trimmed map[string]bool) []string {
	var out []string
	for _, rel := range files {
		if !slices.Contains(indexPages, rel) {
			continue
		}
		lines, err := fileLines(root, rel)
		if err != nil {
			continue
		}
		for i, line := range lines {
			bare := codeSpanRE.ReplaceAllString(line, " ")
			seen := map[string]bool{}
			for _, pattern := range []*regexp.Regexp{barePathRE, bareBundleDirRE} {
				for _, m := range pattern.FindAllStringSubmatch(bare, -1) {
					bad := deadPath(root, m[1], trimmed)
					if bad == "" || seen[bad] {
						continue
					}
					seen[bad] = true
					out = append(out, fmt.Sprintf(
						"%s:%d: dead path %s (write it in backticks, or delete the reference)",
						rel, i+1, bad))
				}
			}
		}
	}
	return out
}
