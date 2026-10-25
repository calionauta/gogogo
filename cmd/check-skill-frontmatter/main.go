// SCOPE:core — SKILL.md frontmatter linter (pre-commit + CI gate).
//
// Package main implements the check-skill-frontmatter linter referenced by the
// Makefile (`make check-skill-frontmatter`) and the lefthook pre-commit
// pipeline. It exists because a skill's YAML frontmatter is parsed by the
// *consumer* (Claude Code, pi, any Agent Skills host) and NOT by this repo's
// build — so a syntax error ships silently and only shows up as
// "Error in user YAML: mapping values are not allowed in this context" in the
// host UI, where nobody is looking.
//
// That exact failure happened: the gogogo-coding-standards description was an
// unquoted scalar containing "Triggers when: " — the ": " starts a YAML
// mapping, so the whole frontmatter failed to parse. Quoting the value fixed
// it, and this check is what stops the next unquoted ": " from shipping.
//
// It parses with a real YAML parser (gopkg.in/yaml.v3, already in the build
// graph via DagNats) rather than a regex, because the failure mode is a parser
// rejection — a regex would only catch the one shape already seen.
//
// Checks, per SKILL.md:
//
//  1. Frontmatter exists and is delimited by a leading `---` / closing `---`.
//  2. It parses as a YAML mapping (catches unquoted ": ", tabs, bad indent).
//  3. `name` is present and non-empty.
//  4. `description` is present and non-empty.
//  5. `description` stays within the Agent Skills limit (1024 chars).
//  6. No unknown top-level keys (typos like `descripton:` would otherwise be
//     silently ignored by hosts).
//  7. Every relative markdown path in the body (a link or a backticked path)
//     resolves — including the cross-skill references like
//     `../gogogo-coding-standards/references/datastar.md`, which no other gate
//     reads and an agent cannot fall back to a URL for.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// maxDescriptionLen is the Agent Skills frontmatter limit for `description`.
// Exceeding it makes hosts truncate or reject the skill, which reads as "the
// skill never triggers" — the hardest kind of bug to attribute.
const maxDescriptionLen = 1024

// allowedKeys is the closed set of frontmatter keys this repo uses. Unknown
// keys are rejected rather than ignored: a typo (`descripton`) would otherwise
// parse fine and silently disable the skill's triggering.
var allowedKeys = map[string]bool{
	"name":          true,
	"description":   true,
	"license":       true,
	"allowed-tools": true,
	"metadata":      true,
	"compatibility": true,
	"version":       true,
}

type problem struct {
	path string
	msg  string
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}

	skills, err := findSkills(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "check-skill-frontmatter: %v\n", err)
		os.Exit(1)
	}

	var problems []problem
	for _, path := range skills {
		if p, ok := checkSkill(path); !ok {
			problems = append(problems, problem{path: path, msg: p})
		}
	}
	problems = append(problems, checkRegistry(root, skills)...)
	problems = append(problems, checkLinks(root, skills)...)

	if len(problems) == 0 {
		fmt.Fprintf(os.Stdout, "✅ SKILL.md frontmatter valid, registered, references resolve (%d file(s))\n", len(skills))
		return
	}
	for _, p := range problems {
		fmt.Fprintf(os.Stdout, "❌ %s\n   %s\n", p.path, p.msg)
	}
	os.Exit(2)
}

// findSkills walks root for SKILL.md files, skipping vendor/build trees.
func findSkills(root string) ([]string, error) {
	var out []string
	// #nosec G703 -- local dev tool; root comes from the developer's own shell.
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			isRoot := filepath.Clean(path) == filepath.Clean(root)
			if !isRoot && (name == "vendor" || name == "node_modules" ||
				name == "tmp" || name == "site" || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "SKILL.md" {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

// checkSkill returns a message and false when path has a frontmatter problem.
func checkSkill(path string) (string, bool) {
	// #nosec G304 G703 -- local dev tool; path comes from the walk of the repo.
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("read: %v", err), false
	}
	fm, ok := extractFrontmatter(string(raw))
	if !ok {
		return "no frontmatter: file must start with `---` and have a closing `---`", false
	}

	var doc map[string]any
	if err := yaml.Unmarshal([]byte(fm), &doc); err != nil {
		// Surface the parser's own message: it names the line/column, which is
		// exactly what the host UI showed and what a human needs to fix it.
		return "frontmatter is not valid YAML — " + err.Error() +
			"\n   (an unquoted value containing \": \" starts a mapping; quote the whole value)", false
	}
	if doc == nil {
		return "frontmatter parsed as empty", false
	}

	for key := range doc {
		if !allowedKeys[key] {
			return fmt.Sprintf("unknown frontmatter key %q (allowed: %s)",
				key, strings.Join(sortedKeys(allowedKeys), ", ")), false
		}
	}

	name, _ := doc["name"].(string)
	if strings.TrimSpace(name) == "" {
		return "`name` is missing or empty", false
	}

	desc, _ := doc["description"].(string)
	if strings.TrimSpace(desc) == "" {
		return "`description` is missing or empty", false
	}
	if len(desc) > maxDescriptionLen {
		return fmt.Sprintf("`description` is %d chars, over the %d limit — hosts truncate it and the skill stops triggering",
			len(desc), maxDescriptionLen), false
	}

	return "", true
}

// checkRegistry asserts every skill under skills/ is registered with the
// host's skill loader at .agents/skills/<name>.
//
// A skill is inert until a host can find it BY NAME. Nothing in this repo
// pointed any agent at skills/, so gogogo-coding-standards and
// stelow-workflow-coding-standards were read as prose at best and skipped at
// worst — while the rules they carry (patch targets, size gates, test shape)
// were exactly the ones a change needed. .agents/skills/<name> is that
// registration: agents that resolve skills from the project directory load
// them by name, and the symlink keeps one copy of the content.
//
// Drift is checked in both directions: an unregistered skill never loads, and
// a registry entry with no skill behind it (or a dangling link) advertises a
// skill that is not there.
func checkRegistry(root string, skills []string) []problem {
	regDir := filepath.Join(root, ".agents", "skills")
	entries, err := os.ReadDir(regDir)
	if err != nil {
		if len(skills) == 0 {
			return nil
		}
		return []problem{{path: regDir, msg: "no host registry: run `mkdir -p .agents/skills` and link each skill " +
			"(`ln -sfn ../../skills/<name> .agents/skills/<name>`) so hosts load it by name"}}
	}
	registered := map[string]bool{}
	for _, e := range entries {
		registered[e.Name()] = true
	}
	names := map[string]bool{}
	var problems []problem
	for _, path := range skills {
		name := filepath.Base(filepath.Dir(path))
		names[name] = true
		if !registered[name] {
			problems = append(problems, problem{path: regDir, msg: fmt.Sprintf(
				"%s is not registered — run `ln -sfn ../../skills/%s .agents/skills/%s`, or hosts will never load it by name",
				name, name, name)})
			continue
		}
		// A link that does not resolve advertises a skill that is not there.
		// #nosec G304 G703 -- local dev tool; the path is built from a walk of the repo.
		if _, err := os.Stat(filepath.Join(regDir, name, "SKILL.md")); err != nil {
			problems = append(problems, problem{
				path: filepath.Join(regDir, name),
				msg:  fmt.Sprintf("does not resolve to %s/SKILL.md (%v) — repair or drop the entry", name, err),
			})
		}
	}
	for name := range registered {
		if !names[name] {
			problems = append(problems, problem{
				path: filepath.Join(regDir, name),
				msg:  "registered but no skills/" + name + " behind it — remove the stale entry",
			})
		}
	}
	return problems
}

// extractFrontmatter returns the text between the leading `---` and the next
// `---` line, and whether both delimiters were found.
func extractFrontmatter(s string) (string, bool) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" {
		return "", false
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " \t") == "---" {
			return strings.Join(lines[1:i], "\n"), true
		}
	}
	return "", false
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Insertion sort: the set is tiny and this avoids importing sort for one call.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
