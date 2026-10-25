// SCOPE:core - DO NOT REMOVE - standalone DaisyUI linter (the product half).
//
// `cmd/daisyui-lint` is the reusable surface of the rules in `internal/daisyui`:
// any project can run it, not just gogogo. Install with
//
//	go install github.com/calionauta/gogogo/cmd/daisyui-lint@latest
//
// The discipline is the same one `datastar-lint` established, because a linter an
// agent has to trust needs it: stable diagnostic codes (UNKNOWN_CLASS,
// PART_WITHOUT_PARENT, HOST_MISMATCH, RETIRED_CLASS, DUPLICATE_MODIFIER,
// ARBITRARY_VALUE), a `--version` that names the DaisyUI release the vocabulary
// was parsed from, and a vocabulary DERIVED from the vendored skill rather than
// hand-maintained — so a DaisyUI bump widens the checks instead of leaving a
// second copy behind.
//
// Severity is the policy knob: an off-scale arbitrary value is a WARNING by
// default (a design decision, not a bug) and an ERROR under --strict, and a
// project accepts specific tokens in `.daisyui-allow` (one per line, `#` for
// comments) rather than silencing a rule.
//
// Exit codes: 0 clean, 1 findings at error severity, 2 usage/setup error.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/calionauta/gogogo/internal/daisyui"
)

// Version is the binary's own version; it moves independently of DaisyUI.
const Version = "0.1.0"

// skipDirs are never linted: fixtures intentionally contain broken markup, and
// vendored trees are not the project's code.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, ".git": true,
	"testdata": true, "site": true, "dist": true,
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main with an exit code, so the CLI is testable without a subprocess.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("daisyui-lint", flag.ContinueOnError)
	fs.SetOutput(stderr)
	skillPath := fs.String("skill", "", "path to the vendored daisyUI skill (default: search upward)")
	allowPath := fs.String("allow", "", "allowlist of accepted arbitrary-value tokens (default: .daisyui-allow)")
	strict := fs.Bool("strict", false, "promote warnings to errors")
	cssGlobs := fs.String("css", "",
		"comma-separated globs of YOUR stylesheets: classes they define are not judged as DaisyUI")
	format := fs.String("format", "text", "output format: text or json")
	showVersion := fs.Bool("version", false, "print the tool version and the daisyUI release it validates")
	fs.Usage = func() {
		fmt.Fprintf(stderr, `usage: daisyui-lint [flags] [paths...]

Checks DaisyUI usage against the vendored skill: declared classes, parts and
their containers, form-control hosts, retired v4 names, duplicate modifiers, and
off-scale arbitrary values.

  --skill PATH   vendored skill (SKILL.md / llms.txt); default: search upward
                 for .agents/skills/daisyui/SKILL.md or skills/daisyui/SKILL.md
  --allow PATH   accepted arbitrary-value tokens, one per line
  --strict       warnings become errors (recommended in CI of an app)
  --format       text (default) or json
  --version      tool version + the daisyUI release the skill declares

Exit codes: 0 clean, 1 findings at error severity, 2 usage/setup error.
`)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	paths := fs.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}

	skill := *skillPath
	if skill == "" {
		found, err := daisyui.FindSkill(paths[0])
		if err != nil {
			fmt.Fprintf(stderr, "daisyui-lint: %v\n", err)
			return 2
		}
		skill = found
	}
	if *showVersion {
		fmt.Fprintf(stdout, "daisyui-lint %s\nskill: %s\ndaisyUI: %s\n",
			Version, skill, daisyui.SkillVersion(skill))
		return 0
	}

	vocab, err := daisyui.LoadSkillFile(skill)
	if err != nil {
		fmt.Fprintf(stderr, "daisyui-lint: %v\n", err)
		return 2
	}
	opts := daisyui.Options{Strict: *strict}
	if *cssGlobs != "" {
		owned, oerr := ownClasses(*cssGlobs)
		if oerr != nil {
			fmt.Fprintf(stderr, "daisyui-lint: %v\n", oerr)
			return 2
		}
		opts.RepoOwned = owned
	}
	if *allowPath != "" {
		if opts.Allow, err = daisyui.LoadAllow(*allowPath); err != nil {
			fmt.Fprintf(stderr, "daisyui-lint: %v\n", err)
			return 2
		}
	}

	files, err := collectFiles(paths)
	if err != nil {
		fmt.Fprintf(stderr, "daisyui-lint: %v\n", err)
		return 2
	}
	findings := lintFiles(vocab, files, opts)

	if *format == "json" {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(findings)
		return exitCode(findings)
	}
	for _, f := range findings {
		fmt.Fprintln(stdout, f)
	}
	if len(findings) == 0 {
		fmt.Fprintf(stdout, "✅ %d files clean against %s (%s)\n", len(files), skill, daisyui.SkillVersion(skill))
		return 0
	}
	fmt.Fprintf(stdout, "\n%d finding(s) in %d file(s)\n", len(findings), filesWith(findings))
	return exitCode(findings)
}

// ownClasses loads the project's own stylesheets so a local class that shares a
// DaisyUI prefix (`chat-layout`) is not judged against DaisyUI's `chat`. Without
// --css the ambiguity is reported, which is the honest default: the linter
// cannot know your vocabulary unless you point at it.
func ownClasses(globs string) (map[string]bool, error) {
	out := map[string]bool{}
	for glob := range strings.SplitSeq(globs, ",") {
		glob = strings.TrimSpace(glob)
		if glob == "" {
			continue
		}
		matches, err := filepath.Glob(glob)
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			// #nosec G304 G703 -- local dev tool; the path is the caller's glob.
			raw, rerr := os.ReadFile(m)
			if rerr != nil {
				return nil, rerr
			}
			for class := range daisyui.CSSClasses(string(raw)) {
				out[class] = true
			}
		}
	}
	return out, nil
}

// lintFiles runs the rules over every file and sorts deterministically, so two
// runs of the same tree print the same thing (agents diff output).
func lintFiles(vocab daisyui.Vocabulary, files []string, opts daisyui.Options) []daisyui.Finding {
	out := make([]daisyui.Finding, 0)
	for _, rel := range files {
		// #nosec G304 G703 -- local dev tool; rel comes from the caller's paths.
		raw, err := os.ReadFile(rel)
		if err != nil {
			fmt.Fprintf(os.Stderr, "daisyui-lint: %v\n", err)
			continue
		}
		out = append(out, daisyui.LintFile(vocab, string(raw), filepath.ToSlash(rel), opts)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// collectFiles expands the paths into lintable markup.
func collectFiles(paths []string) ([]string, error) {
	var out []string
	for _, root := range paths {
		st, err := os.Stat(root)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			out = append(out, root)
			continue
		}
		err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if skipDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			switch filepath.Ext(path) {
			case ".templ", ".html":
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}

func exitCode(findings []daisyui.Finding) int {
	for _, f := range findings {
		if f.Severity == daisyui.Error {
			return 1
		}
	}
	return 0
}

func filesWith(findings []daisyui.Finding) int {
	seen := map[string]bool{}
	for _, f := range findings {
		seen[f.File] = true
	}
	return len(seen)
}
