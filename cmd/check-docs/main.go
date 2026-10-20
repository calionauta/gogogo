// SCOPE:core — Docs claim guard: no drifting counts, dead paths, or invented commands.
//
// Prose is the one place the repo's generated docs do not reach. Three drifts
// shipped and were only caught by hand: "24 capabilities" (the registry had
// 35), "all nine slices" (there were ten), and `gogogo trim <unit>` (a
// subcommand that never existed). None of them breaks a build, so only a
// mechanical scan keeps them out.
//
// The rules, each cheap and deterministic:
//
//  1. No hand-written counts of capabilities/slices/units — the generated
//     docs/unit-contracts.md#registry-at-a-glance is the only place counts
//     live, and its freshness test pins them.
//  2. Every concrete repo path in backticks must exist (a moved package
//     otherwise keeps being recommended for months).
//  3. Every `gogogo <subcommand>` in backticks must be a real subcommand.
//  4. Every docs/*.md must be published by the site MANIFEST (or it silently
//     never reaches the docs site).
//
// Decision records (docs/architecture-decision.md) are exempt from rule 2:
// they exist to record what was removed, so naming dead paths is their job.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// rootDocs are the prose entrypoints outside docs/.
var rootDocs = []string{
	"README.md", "AGENTS.md", "ARCHITECTURE.md", "CONTRIBUTING.md", "llms.txt",
	".github/PULL_REQUEST_TEMPLATE.md",
}

// generatedDocs carry computed counts; their freshness tests pin them.
var generatedDocs = []string{"docs/api-routes.md", "docs/unit-contracts.md"}

// historyDocs may name removed paths and commands on purpose.
var historyDocs = []string{"docs/architecture-decision.md"}

// externalDocs describe another project's tree (DagNats internals), so their
// `internal/...` paths are not claims about this repo.
var externalDocs = []string{"docs/dagnats.md"}

// skipPathFiles exempts decision records from the existence rule.
func skipPathFiles(rel string) bool { return slices.Contains(historyDocs, rel) }

// countRE matches a number immediately followed by a PLURAL drifting noun.
// Singular phrases ("restores one unit") are ordinary English; the drift that
// shipped was "24 capabilities" and "nine slices".
var countRE = regexp.MustCompile(
	`(?i)\b(\d+|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|dozen)\s+(capabilities|slices|units)\b`)

// tickRE extracts inline code spans.
var tickRE = regexp.MustCompile("`([^`\n]+)`")

// repoRoots are the top-level directories a concrete path can start with.
var repoRoots = []string{"features", "internal", "cmd", "web", "config", "scripts", "bin", "deploy", "site"}

// pathSegmentRE is one directory segment (a dot means package.Symbol
// notation, e.g. internal/datastar.RenderAndPatch, not a path).
var pathSegmentRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// fileSegmentRE is a last segment that may be a file, but only of a type this
// repo actually tracks (a hypothetical app.min.css.map is not a claim).
var fileSegmentRE = regexp.MustCompile(`^[A-Za-z0-9_-]+\.(go|md|sh|sql|templ|js|css|json|ya?ml|toml)$`)

// generatedPath reports paths that exist only after a build (site output is
// gitignored), so a clean checkout does not have them and docs may still
// name them.
func generatedPath(path string) bool {
	if path == "site/docs" || strings.HasPrefix(path, "site/docs/") {
		return true
	}
	switch path {
	case "site/llms.txt", "site/llms-full.txt", "site/sitemap.xml":
		return true
	}
	return false
}

// exampleSegments mark illustrative paths (`internal/foo/`) that need not exist.
var exampleSegments = map[string]bool{
	"foo": true, "bar": true, "baz": true, "x": true, "y": true,
	"name": true, "example": true, "yourpkg": true, "mypkg": true,
}

// commandRE matches an invocation of the installer CLI inside backticks,
// including the `go run ./cmd/gogogo …` form (the path puts a slash before the
// binary name).
var commandRE = regexp.MustCompile(`(?:^|[\s/])gogogo\s+([a-z][a-z-]*)`)

// subcommandRE extracts the dispatch cases from the installer entrypoint, so
// the allowed list cannot drift when a subcommand is added or renamed.
var subcommandRE = regexp.MustCompile(`case "([a-z][a-z-]*)":`)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "check-docs:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	files, err := docFiles(root)
	if err != nil {
		return err
	}
	subs, err := knownSubcommands(root)
	if err != nil {
		return err
	}
	problems := checkCounts(root, files)
	problems = append(problems, checkPaths(root, files, trimmedPaths(root))...)
	problems = append(problems, checkCommands(root, files, subs)...)
	problems = append(problems, checkStaleScopeDialect(root, files)...)
	problems = append(problems, checkManifest(root)...)
	if len(problems) > 0 {
		return fmt.Errorf("docs claims drifted (%d):\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
	fmt.Fprintln(os.Stdout, "check-docs: counts are generated, paths and commands exist")
	return nil
}

// docFiles lists the prose files the guard owns, repo-relative.
func docFiles(root string) ([]string, error) {
	var out []string
	for _, rel := range rootDocs {
		if _, err := os.Stat(filepath.Join(root, rel)); err == nil {
			out = append(out, rel)
		}
	}
	patterns := []string{"docs/*.md", "cmd/*/README.md", "skills/*/SKILL.md"}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			rel, relErr := filepath.Rel(root, m)
			if relErr != nil {
				return nil, relErr
			}
			rel = filepath.ToSlash(rel)
			// Vendored skills are upstream-owned (never edit them here).
			if strings.HasPrefix(rel, "skills/stelow-") {
				continue
			}
			out = append(out, rel)
		}
	}
	slices.Sort(out)
	return out, nil
}

// fileLines reads rel under root and returns its lines.
func fileLines(root, rel string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, err
	}
	return strings.Split(string(raw), "\n"), nil
}

// checkCounts rejects hand-written counts in prose. Generated docs are
// excluded: their numbers are computed and pinned by a freshness test.
func checkCounts(root string, files []string) []string {
	var out []string
	for _, rel := range files {
		if slices.Contains(generatedDocs, rel) {
			continue
		}
		lines, err := fileLines(root, rel)
		if err != nil {
			out = append(out, rel+": "+err.Error())
			continue
		}
		for i, line := range lines {
			if m := countRE.FindString(line); m != "" {
				out = append(out, fmt.Sprintf(
					"%s:%d: hand-written count %q — link docs/unit-contracts.md#registry-at-a-glance instead",
					rel, i+1, m))
			}
		}
	}
	return out
}

// checkPaths rejects concrete repo paths that do not exist. Paths a scaffold
// removed on purpose are exempt: a generated project's docs still describe
// the template's units, and AGENTS.md records what was trimmed.
func checkPaths(root string, files []string, trimmed map[string]bool) []string {
	var out []string
	for _, rel := range files {
		if skipPathFiles(rel) || slices.Contains(externalDocs, rel) {
			continue
		}
		lines, err := fileLines(root, rel)
		if err != nil {
			continue
		}
		for i, line := range lines {
			for _, tick := range tickRE.FindAllStringSubmatch(line, -1) {
				if bad := deadPath(root, tick[1], trimmed); bad != "" {
					out = append(out, fmt.Sprintf("%s:%d: dead path `%s`", rel, i+1, bad))
				}
			}
		}
	}
	return out
}

// deadPath returns the path when it is a concrete repo path that does not
// exist; empty when the token is not a path or is an illustrative example.
func deadPath(root, token string, trimmed map[string]bool) string {
	token = strings.TrimSpace(token)
	if strings.ContainsAny(token, "<>{}$*") || strings.Contains(token, "..") {
		return ""
	}
	path := strings.TrimSuffix(token, "/")
	segments := strings.Split(path, "/")
	if len(segments) < 2 || !slices.Contains(repoRoots, segments[0]) {
		return ""
	}
	if exampleSegments[segments[1]] || exampleSegments[segments[len(segments)-1]] {
		return ""
	}
	for i, seg := range segments[1:] {
		last := i == len(segments)-2
		if last && fileSegmentRE.MatchString(seg) {
			continue
		}
		if !pathSegmentRE.MatchString(seg) {
			return ""
		}
	}
	if trimmedPath(trimmed, path) || generatedPath(path) {
		return ""
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
		return token
	}
	return ""
}

// trimmedPath reports whether path (or a parent of it) was deliberately
// removed by the scaffold. Same provenance the registry conformance tests
// read, so docs and registry agree about a trimmed tree.
func trimmedPath(trimmed map[string]bool, path string) bool {
	for t := range trimmed {
		if path == t || strings.HasPrefix(path, t+"/") || strings.HasPrefix(t, path+"/") {
			return true
		}
	}
	return false
}

// trimmedPaths reads AGENTS.md's trim provenance ("removed-paths:" inside the
// "## Trim provenance" section). No section means the full template: strict.
func trimmedPaths(root string) map[string]bool {
	out := map[string]bool{}
	raw, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		return out
	}
	inSection := false
	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "## ") {
			inSection = line == "## Trim provenance"
			continue
		}
		if !inSection {
			continue
		}
		rest, ok := strings.CutPrefix(line, "removed-paths:")
		if !ok {
			continue
		}
		for p := range strings.SplitSeq(strings.TrimSpace(rest), ",") {
			if p = strings.TrimSpace(p); p != "" && p != "none" {
				out[filepath.ToSlash(p)] = true
			}
		}
	}
	return out
}

// checkCommands rejects `gogogo <word>` invocations naming a subcommand the
// CLI does not dispatch.
func checkCommands(root string, files, known []string) []string {
	var out []string
	for _, rel := range files {
		if slices.Contains(historyDocs, rel) {
			continue
		}
		lines, err := fileLines(root, rel)
		if err != nil {
			continue
		}
		for i, line := range lines {
			for _, tick := range tickRE.FindAllStringSubmatch(line, -1) {
				span := tick[1]
				if !strings.Contains(span, "gogogo") {
					continue
				}
				m := commandRE.FindStringSubmatch(span)
				if m == nil {
					continue
				}
				if !slices.Contains(known, m[1]) {
					out = append(out, fmt.Sprintf(
						"%s:%d: `gogogo %s` is not a subcommand (known: %s)",
						rel, i+1, m[1], strings.Join(known, ", ")))
				}
			}
		}
	}
	return out
}

// knownSubcommands reads the dispatch list from the installer entrypoint.
func knownSubcommands(root string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "internal", "installer", "run.go"))
	if err != nil {
		return nil, fmt.Errorf("read installer dispatch: %w", err)
	}
	var out []string
	for _, m := range subcommandRE.FindAllStringSubmatch(string(raw), -1) {
		out = append(out, m[1])
	}
	if len(out) == 0 {
		return nil, errors.New("no subcommands found in internal/installer/run.go — the dispatch parser broke")
	}
	slices.Sort(out)
	return out, nil
}

// staleScopeRE matches the two-axis SCOPE dialect that was rejected; docs
// that still teach it send readers to an annotation `make check-scope`
// refuses.
var staleScopeRE = regexp.MustCompile(`SCOPE:layer=`)

// checkStaleScopeDialect rejects the retired annotation form in prose.
func checkStaleScopeDialect(root string, files []string) []string {
	var out []string
	for _, rel := range files {
		lines, err := fileLines(root, rel)
		if err != nil {
			continue
		}
		for i, line := range lines {
			if staleScopeRE.MatchString(line) {
				out = append(out, fmt.Sprintf(
					"%s:%d: retired two-axis SCOPE dialect — use `// SCOPE:<core|plugin|feature>`",
					rel, i+1))
			}
		}
	}
	return out
}

// checkManifest proves every docs/*.md is published by the site build: an
// orphan page exists in git and nowhere a reader can reach it.
func checkManifest(root string) []string {
	raw, err := os.ReadFile(filepath.Join(root, "site", "build.mjs"))
	if err != nil {
		return []string{"site/build.mjs: " + err.Error()}
	}
	published := map[string]bool{}
	for _, m := range regexp.MustCompile(`file:\s*"([^"]+)"`).
		FindAllStringSubmatch(string(raw), -1) {
		published[m[1]] = true
	}
	matches, err := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	if err != nil {
		return []string{"docs: " + err.Error()}
	}
	var out []string
	for _, m := range matches {
		base := filepath.Base(m)
		if !published[base] {
			out = append(out, "site/build.mjs: docs/"+base+" is not in the MANIFEST (never published)")
		}
	}
	return out
}

// moduleRoot walks up from the working directory to the module root.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}
