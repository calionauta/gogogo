// The candidate-tree half of the prompt override seam.
//
// GOGOGO_PROMPTS_DIR points the loader at a prompt tree on disk (what an
// optimizer rewrites). The loader validates a candidate when it reads it, which
// is too late to compare it with the library — so the gate checks the tree
// against the embedded copy before anything runs:
//
//  1. every file parses under its path-derived id, with the same contract the
//     embedded tree obeys;
//  2. the id already exists in the library (a candidate cannot invent a prompt
//     no call site loads);
//  3. `variables` are unchanged — a call site passes exactly those names, so a
//     renamed variable is a broken render, not an experiment;
//  4. a body that differs carries a higher `version`, because an evaluation
//     result against an unchanged version is not comparable with the previous
//     one. That is the whole reason the field exists.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/calionauta/gogogo/internal/prompts"
)

// checkOverrideTree reports the candidate tree's problems and how many files it
// validated. The walk and every read go through os.Root (a candidate tree is
// still a tree we did not write); a broken directory is one problem, not a
// panic, because the gate must answer in CI.
func checkOverrideTree(dir string) ([]string, int) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return []string{"check-prompts: override: " + err.Error()}, 0
	}
	defer func() { _ = root.Close() }()

	base, err := prompts.All()
	if err != nil {
		return []string{"check-prompts: override: embedded library: " + err.Error()}, 0
	}
	known := make(map[string]prompts.Prompt, len(base))
	for _, p := range base {
		known[p.ID] = p
	}

	var paths []string
	walkErr := fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".prompt") {
			paths = append(paths, p)
		}
		return nil
	})
	var problems []string
	if walkErr != nil {
		problems = append(problems, "check-prompts: override: "+walkErr.Error())
	}

	checked := 0
	for _, rel := range paths {
		fileProblems, ok := checkCandidate(root, rel, known)
		problems = append(problems, fileProblems...)
		if ok {
			checked++
		}
	}
	return problems, checked
}

// checkCandidate validates one file of the candidate tree. The second return
// value says the file parsed, which is what the success line counts.
func checkCandidate(root *os.Root, rel string, known map[string]prompts.Prompt) ([]string, bool) {
	id := strings.TrimSuffix(filepath.ToSlash(rel), ".prompt")
	raw, err := root.ReadFile(rel)
	if err != nil {
		return []string{"check-prompts: override " + rel + ": " + err.Error()}, false
	}
	cand, err := prompts.Parse(id, raw)
	if err != nil {
		return []string{"check-prompts: override " + rel + ": " + err.Error()}, false
	}
	embedded, ok := known[id]
	if !ok {
		return []string{fmt.Sprintf(
			"check-prompts: override %s: no embedded prompt with id %q "+
				"(a candidate must override a file a call site loads)", rel, id)}, true
	}
	return compareOverride(id, cand, embedded), true
}

// compareOverride reports the ways a candidate disagrees with the library in a
// way an evaluation cannot absorb: a different variable list, or a changed body
// that kept the old version.
func compareOverride(id string, cand, embedded prompts.Prompt) []string {
	var problems []string
	if got, want := strings.Join(cand.Variables, ","), strings.Join(embedded.Variables, ","); got != want {
		problems = append(problems, fmt.Sprintf(
			"check-prompts: override: %s: variables [%s] differ from the library's [%s] — "+
				"a call site passes exactly the library's names", id, got, want))
	}
	if cand.Body != embedded.Body && cand.Version <= embedded.Version {
		problems = append(problems, fmt.Sprintf(
			"check-prompts: override: %s: body differs but version stayed %d — "+
				"bump it, or the result is not comparable with the previous evaluation",
			id, cand.Version))
	}
	return problems
}
