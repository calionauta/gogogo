// SCOPE:core - DO NOT REMOVE - class integrity gate (pre-commit + CI).
// Package main implements `make check-classes`, the executable half of the UI
// rules in docs/ui-shell.md and docs/ui-compositions.md.
//
// Why it exists: a design rule with no gate is a rule until the next commit.
// Three failures shipped through review because nothing checked them —
//
//   - `.ui-field-group` was used in four pages and defined in NO stylesheet
//     (an "invented component renders unstyled" case that looked fine in the
//     diff because the surrounding markup had other classes);
//   - `docs/ui-shell.md` kept documenting `.ui-btn`, `.ui-field`, `.ui-badge`,
//     `.ui-table` and `.ui-callout` after they were deleted — the migration
//     commit declared them gone and the table was never read again;
//   - the account row carried `badge` while an override stripped every badge
//     property: a custom component wearing a component library's class name.
//
// So the gate checks three directions of the same invariant (a class that
// exists in one place must exist in the other) plus the DaisyUI part rules
// that are invisible to a compiler:
//
//  1. USED      — every app-*/ui-*/is-* class in a class attribute of a
//     .templ file is defined in web/resources/static/chrome.css.
//  2. DOCUMENTED— every app-*/ui-* class in a kit table row of
//     docs/ui-shell.md is defined in chrome.css (a table nobody
//     verifies rots in a week). Prose is exempt on purpose: a page
//     must be able to explain a class it removed.
//  3. DEFINED   — every app-*/ui-*/is-* selector in chrome.css is mentioned
//     somewhere: markup, a static script, or docs. Dead CSS piles
//     up exactly where a redesign removed the last consumer.
//  4. PARTS     — DaisyUI components are protocols, not primitives: `toggle`
//     (and checkbox/radio) belongs on an <input>, `join-item`
//     needs a `join` container, `dropdown-content` needs a
//     `dropdown`. Each is one wrong string away from a control
//     that looks right and behaves wrong.
//
// Deliberately NOT checked: DaisyUI/utility classes against app.min.css.
// Tailwind emits what it scans, so a class used in markup is in the bundle by
// construction; a stale bundle is `make css-check`'s job, and matching escaped
// selectors (`w-\[3ch\]`) here would produce noise, not signal.
//
// The tool is a developer gate, not part of the runtime binary.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/calionauta/daisyui-lint/daisyui"

	"github.com/calionauta/gogogo/internal/uiregistry"
)

const (
	chromeCSS = "web/resources/static/chrome.css"
	shellDoc  = "docs/ui-shell.md"
	// allowFile is the project's own list of accepted arbitrary-value tokens.
	allowFile = ".daisyui-allow"
)

// finding is one broken invariant, already formatted for a human.
type finding struct {
	file string
	line int
	msg  string
}

func (f finding) String() string {
	if f.line > 0 {
		return fmt.Sprintf("%s:%d: %s", f.file, f.line, f.msg)
	}
	return fmt.Sprintf("%s: %s", f.file, f.msg)
}

// definedRE pulls `.class` selectors out of the stylesheet (comments already
// stripped, so a mention in prose does not count as a definition).
// mentionRE is the permissive "did anyone at all name this class" probe used
// by the dead-CSS direction: comments, docs and scripts all count, because a
// class named only by a comment is still somebody's intent.
var mentionRE = regexp.MustCompile(`\b(?:app|ui|is)-[a-z0-9-]+`)

// docClassRE finds `.app-x` / `.ui-x` in the docs.
var docClassRE = regexp.MustCompile(`\.((?:app|ui)-[a-z0-9-]+)`)

func main() {
	strict := flag.Bool("strict", false,
		"require the vendored skill, the registry and the repo kit (the maintainer's mode)")
	flag.Parse()
	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}
	findings, err := checkTree(root, *strict)
	if err != nil {
		fmt.Fprintf(os.Stderr, "check-classes: %v\n", err)
		os.Exit(1)
	}
	if len(findings) == 0 {
		fmt.Fprintln(os.Stdout, "✅ classes: used/live match the stylesheet, DaisyUI parts respected")
		return
	}
	fmt.Fprintln(os.Stderr, "❌ class integrity: the UI rules are not executable here —")
	for _, f := range findings {
		fmt.Fprintf(os.Stderr, "  %s\n", f)
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  Fix the markup, the stylesheet, or the doc this names.")
	fmt.Fprintln(os.Stderr, "  Approved compositions: docs/ui-compositions.md")
	fmt.Fprintln(os.Stderr, "  Or ask the registry: go run ./cmd/ui-registry list|show <id>")
	os.Exit(1)
}

// loadOptions assembles the DaisyUI rules' policy: the repo's own vocabulary
// (so local prefixes win over DaisyUI's), and the arbitrary-value allowlist a
// project keeps in .daisyui-allow (absent by default: a new off-scale value is a
// question, not a default).
func loadOptions(root string, defined, alive map[string]bool) (daisyui.Options, error) {
	opts := daisyui.Options{RepoOwned: repoOwned(defined, alive)}
	allow, err := daisyui.LoadAllow(filepath.Join(root, allowFile))
	if err != nil && !os.IsNotExist(err) {
		return opts, err
	}
	opts.Allow = allow
	return opts, nil
}

// repoOwned is the set of class names the repo styles itself: its chrome kit
// plus everything any shipped stylesheet defines. DaisyUI's namespace yields to
// it, so a local `chat-layout` is never judged against DaisyUI's `chat`.
func repoOwned(sets ...map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, s := range sets {
		for k := range s {
			out[k] = true
		}
	}
	return out
}

// notice prints a degradation line on stderr: the gate keeps working, but the
// reader knows which direction it did not run.
func notice(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "check-classes: note: "+format+"\n", args...)
}

// isLocalKit reports whether a class belongs to this repo's own vocabulary, so
// the failure can point at chrome.css instead of the generic stylesheet rule.
func isLocalKit(name string) bool {
	return strings.HasPrefix(name, "app-") || strings.HasPrefix(name, "ui-") || strings.HasPrefix(name, "is-")
}

// checkTree runs every direction over one checkout root.
//
// `strict` is the maintainer's mode: the vendored skill, the registry and the
// repo kit must be present, because losing any of them would silently narrow the
// gate. A consumer of the template may legitimately trim them, so the default
// DEGRADES: the direction that needs the missing input prints one line and the
// rest of the gate keeps working. The Makefile passes --strict.
func checkTree(root string, strict bool) ([]finding, error) {
	defined, err := loadDefined(root)
	if err != nil {
		if strict {
			return nil, err
		}
		notice("repo kit checks skipped: %v", err)
		defined = map[string]bool{}
	}
	vocab := daisyui.Vocabulary{}
	if skillPath, ferr := daisyui.FindSkill(root); ferr != nil {
		if strict {
			return nil, ferr
		}
		notice("daisyUI checks skipped: %v", ferr)
	} else if vocab, err = daisyui.LoadSkillFile(skillPath); err != nil {
		if strict {
			return nil, err
		}
		notice("daisyUI checks skipped: %v", err)
		vocab = daisyui.Vocabulary{}
	}
	alive, err := loadHandwritten(root)
	if err != nil {
		if strict {
			return nil, err
		}
		notice("hand-written stylesheets not read: %v", err)
		alive = map[string]bool{}
	}
	opts, err := loadOptions(root, defined, alive)
	if err != nil {
		return nil, err
	}
	markup, err := templFiles(root)
	if err != nil {
		return nil, err
	}
	findings, mentioned, err := markupFindings(root, markup, defined, vocab, opts)
	if err != nil {
		return nil, err
	}
	addMentions(root, mentioned)
	findings = append(findings, deadSelectorFindings(defined, mentioned)...)
	findings = append(findings, docTableFindings(root, defined)...)
	if reg, rerr := uiregistry.Load(root); rerr == nil {
		findings = append(findings, registryFindings(root, reg)...)
	} else if strict {
		return nil, rerr
	} else {
		notice("registry checks skipped: %v", rerr)
	}
	sortFindings(findings)
	return findings, nil
}

// markupFindings walks the markup: direction 1 (used-but-undefined), the
// DaisyUI part rules, and the loose mention index the dead-CSS direction needs.
func markupFindings(root string, markup []string, defined map[string]bool,
	vocab daisyui.Vocabulary, opts daisyui.Options,
) ([]finding, map[string]bool, error) {
	var findings []finding
	mentioned := map[string]bool{}
	for _, rel := range markup {
		// #nosec G703 -- local dev tool; rel comes from the walk of the repo.
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return nil, nil, err
		}
		src := string(raw)
		for _, tok := range daisyui.TokensInMarkup(src, rel) {
			if defined[tok.Name] || !isLocalKit(tok.Name) {
				continue
			}
			findings = append(findings, finding{file: tok.File, line: tok.Line, msg: fmt.Sprintf(
				"class %q is used but not defined in %s (an invented component renders unstyled)",
				tok.Name, chromeCSS)})
		}
		for _, f := range daisyui.LintFile(vocab, src, rel, opts) {
			findings = append(findings, finding{
				file: f.File, line: f.Line,
				msg: fmt.Sprintf("%s: %s", f.Code, f.Message),
			})
		}
		markMentions(src, mentioned)
	}
	return findings, mentioned, nil
}

// deadSelectorFindings is direction 3: a rule nothing references anymore.
func deadSelectorFindings(defined, mentioned map[string]bool) []finding {
	dead := make([]string, 0, len(defined))
	for name := range defined {
		if !mentioned[name] {
			dead = append(dead, name)
		}
	}
	sort.Strings(dead)
	findings := make([]finding, 0, len(dead))
	for _, name := range dead {
		findings = append(findings, finding{file: chromeCSS, msg: fmt.Sprintf(
			"selector .%s is defined but used nowhere (dead CSS: drop it, or name it in the markup)", name)})
	}
	return findings
}

// docTableFindings is direction 2: the kit table promises classes the CSS does
// not ship. Only table rows count — prose may name a class it explains away.
func docTableFindings(root string, defined map[string]bool) []finding {
	// #nosec G703 -- local dev tool; the doc path is a package constant.
	raw, err := os.ReadFile(filepath.Join(root, shellDoc))
	if err != nil {
		return nil
	}
	var findings []finding
	seen := map[string]bool{}
	for line := range strings.SplitSeq(string(raw), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		for _, m := range docClassRE.FindAllStringSubmatch(line, -1) {
			name := m[1]
			if defined[name] || seen[name] {
				continue
			}
			seen[name] = true
			findings = append(findings, finding{file: shellDoc, msg: fmt.Sprintf(
				"documented class .%s is not defined in %s (the doc promises a kit the CSS does not ship)",
				name, chromeCSS)})
		}
	}
	return findings
}

// addMentions widens the mention index with the non-markup places a class can
// legitimately live: the static scripts that toggle state classes, and docs.
func addMentions(root string, into map[string]bool) {
	var files []string
	for _, dir := range []string{"web/resources/static", "docs"} {
		base := filepath.Join(root, dir)
		// A trimmed checkout may have no docs dir at all; that is not a finding.
		// #nosec G703 -- local dev tool; base is built from the caller's own root.
		_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			switch filepath.Ext(path) {
			case ".js", ".mjs", ".md":
				files = append(files, path)
			}
			return nil
		})
	}
	for _, path := range files {
		// #nosec G304 G703 -- local dev tool; the paths come from the repo walk.
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		markMentions(string(raw), into)
	}
}

func markMentions(src string, into map[string]bool) {
	for _, m := range mentionRE.FindAllString(src, -1) {
		into[m] = true
	}
}

func sortFindings(findings []finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].file != findings[j].file {
			return findings[i].file < findings[j].file
		}
		return findings[i].line < findings[j].line
	})
}

// templFiles lists the markup this gate inspects (features/, internal/, web/).
func templFiles(root string) ([]string, error) {
	var out []string
	for _, dir := range []string{"features", "internal", "web"} {
		base := filepath.Join(root, dir)
		// #nosec G703 -- local dev tool; base is built from the caller's own root.
		if _, err := os.Stat(base); err != nil {
			continue
		}
		// #nosec G703 -- local dev tool; base is built from the caller's own root.
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Ext(path) != ".templ" {
				return nil
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			out = append(out, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}
