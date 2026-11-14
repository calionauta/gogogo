// Command prompt-eval reports what a candidate prompt tree changes.
//
// WHY IT IS NOT A MODEL HARNESS. The obvious shape for "eval" is to call a model
// and score answers. That is not what an optimizer loop needs first, and it is
// not what this template can do honestly: the repo has no real-LLM tests (a
// standing rule), the answer quality is judged by whoever runs the loop, and a
// fake model would score a fake number. What the loop DOES need before spending
// a token is the diff between the library and the candidate tree, with the one
// fact that makes two runs comparable: the version.
//
// So this command answers, for a candidate tree:
//
//   - which prompts changed, added, removed or kept;
//   - whether a changed body bumped its `version` (an unbumped rewrite makes the
//     previous evaluation's result incomparable, which is the reason the field
//     exists);
//   - whether the candidate keeps the same `variables` (a call site passes
//     exactly those names, so a rename is a broken render, not an experiment);
//   - whether the tree is well-formed at all (the same contract the embedded
//     library obeys).
//
// A non-zero exit means "do not run the loop": either the tree would not load,
// or a result against it would be uncomparable. It is the cheap gate before the
// expensive one, and it prints a table a human reads.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/calionauta/gogogo/internal/prompts"
)

// status is one prompt's verdict against the library.
type status string

const (
	statusUnchanged status = "unchanged"
	statusChanged   status = "changed"
	statusAdded     status = "added"
	statusRemoved   status = "removed"
	statusInvalid   status = "invalid"
)

// row is one line of the report.
type row struct {
	ID      string   `json:"id"`
	Status  status   `json:"status"`
	Version int      `json:"version,omitempty"`
	Base    int      `json:"baseVersion,omitempty"`
	Notes   []string `json:"notes,omitempty"`
}

// report is the whole answer, so `--format json` is one object.
type report struct {
	Dir     string `json:"dir"`
	Changed int    `json:"changed"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Invalid int    `json:"invalid"`
	Rows    []row  `json:"rows"`
}

func main() {
	dir := flag.String("dir", "", "candidate prompt tree (default: $"+prompts.OverrideDirEnv+")")
	format := flag.String("format", "text", "text or json")
	flag.Parse()

	if *dir == "" {
		*dir = os.Getenv(prompts.OverrideDirEnv)
	}
	if *dir == "" {
		fmt.Fprintf(os.Stderr,
			"prompt-eval: no candidate tree — pass -dir or set %s\n", prompts.OverrideDirEnv)
		os.Exit(2)
	}

	rep, err := evaluate(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "prompt-eval: "+err.Error())
		os.Exit(2)
	}
	if *format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintln(os.Stderr, "prompt-eval: "+err.Error())
			os.Exit(2)
		}
	} else {
		printReport(os.Stdout, rep)
	}

	// Exit 3 is "the tree would not evaluate comparably" — a verdict, not a
	// crash, so a looping script can tell it from a usage error.
	if rep.Invalid > 0 {
		os.Exit(3)
	}
}

// evaluate compares the candidate tree with the embedded library.
func evaluate(dir string) (report, error) {
	rep := report{Dir: dir}
	base, err := prompts.All()
	if err != nil {
		return rep, fmt.Errorf("read embedded library: %w", err)
	}
	known := make(map[string]prompts.Prompt, len(base))
	for _, p := range base {
		known[p.ID] = p
	}

	// The walk goes through os.Root: a candidate tree is still a tree this
	// program did not write.
	root, rerr := os.OpenRoot(dir)
	if rerr != nil {
		return rep, rerr
	}
	defer func() { _ = root.Close() }()

	seen := map[string]bool{}
	walkErr := fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".prompt") {
			return nil
		}
		id := strings.TrimSuffix(filepath.ToSlash(path), ".prompt")
		seen[id] = true
		r, problems := compareOne(root, path, id, known)
		rep.Rows = append(rep.Rows, r)
		if len(problems) > 0 {
			rep.Rows[len(rep.Rows)-1].Notes = append(rep.Rows[len(rep.Rows)-1].Notes, problems...)
			rep.Invalid++
		}
		switch r.Status {
		case statusChanged:
			rep.Changed++
		case statusAdded:
			rep.Added++
		}
		return nil
	})
	if walkErr != nil {
		return rep, walkErr
	}

	// A prompt the library has and the candidate dropped is not an error — the
	// tree may hold only what it changes — but it is worth naming: an optimizer
	// that silently deleted a prompt would be reported as "no diff" otherwise.
	for _, p := range base {
		if seen[p.ID] {
			continue
		}
		rep.Rows = append(rep.Rows, row{ID: p.ID, Status: statusRemoved, Base: p.Version})
		rep.Removed++
	}
	sort.Slice(rep.Rows, func(i, j int) bool { return rep.Rows[i].ID < rep.Rows[j].ID })
	return rep, nil
}

// compareOne reads one candidate and classifies it against the library.
func compareOne(root *os.Root, path, id string, known map[string]prompts.Prompt) (row, []string) {
	r := row{ID: id}
	base, exists := known[id]
	if exists {
		r.Base = base.Version
	}
	raw, rerr := fs.ReadFile(root.FS(), path)
	if rerr != nil {
		r.Status = statusInvalid
		return r, []string{rerr.Error()}
	}
	p, err := prompts.Parse(id, raw)
	if err != nil {
		r.Status = statusInvalid
		return r, []string{err.Error()}
	}
	r.Version = p.Version
	if !exists {
		// A new file has no call site, so nothing loads it. The gate refuses it
		// as "added, but nothing references it" rather than a clean addition.
		r.Status = statusAdded
		return r, []string{"candidate invents an id the library does not have; no call site loads it"}
	}
	if p.Body == base.Body {
		r.Status = statusUnchanged
		return r, nil
	}
	r.Status = statusChanged
	var problems []string
	if p.Version <= base.Version {
		problems = append(problems, fmt.Sprintf(
			"body changed but version stayed %d (library %d): an evaluation is not comparable across versions",
			p.Version, base.Version))
	}
	if strings.Join(p.Variables, ",") != strings.Join(base.Variables, ",") {
		problems = append(problems, fmt.Sprintf(
			"variables changed: call sites pass %v, candidate declares %v",
			base.Variables, p.Variables))
	}
	return r, problems
}

// printReport renders the table a human reads.
func printReport(w io.Writer, rep report) {
	fmt.Fprintf(w, "prompt-eval: %s\n", rep.Dir)
	for _, r := range rep.Rows {
		line := fmt.Sprintf("  %-10s %s", r.Status, r.ID)
		if r.Status == statusChanged {
			line += fmt.Sprintf("  v%d -> v%d", r.Base, r.Version)
		}
		fmt.Fprintln(w, line)
		for _, n := range r.Notes {
			fmt.Fprintf(w, "             ! %s\n", n)
		}
	}
	fmt.Fprintf(w, "\n  %d changed, %d added, %d removed, %d invalid\n",
		rep.Changed, rep.Added, rep.Removed, rep.Invalid)
	if rep.Invalid > 0 {
		fmt.Fprintln(w, "  do not run the loop: fix the tree (or bump the version) first")
	}
}
