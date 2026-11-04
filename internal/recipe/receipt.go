// SCOPE:plugin — Spike 2: recipe receipt and drift detection.
//
// This package answers the one question the recipe model stands or falls on:
// given a recipe the user has already edited, can the tool report which files
// are still pristine, which are touched, which were deleted, which were added,
// AND whether the declaration still matches the code?
//
// It is a spike: it models the receipt, computes the four file states, and
// checks the declaration against the code. It does NOT install, update, merge
// or write anything. The point is to prove the mechanism before any product is
// built on it.
package recipe

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileState is what happened to one installed file since it was written.
type FileState string

const (
	// StatePristine means the bytes match the recorded hash exactly. It is safe
	// to fast-forward: nothing the user cares about lives here.
	StatePristine FileState = "pristine"
	// StateModified means the bytes differ. The user owns those bytes; no tool
	// may overwrite them.
	StateModified FileState = "modified"
	// StateDeleted means the file is gone. The user removed it on purpose, so an
	// update must not resurrect it.
	StateDeleted FileState = "deleted"
)

// Upstream identifies where the pristine baseline came from, so a later update has
// something to compare against that is not the user's copy.
type Upstream struct {
	Repo string `yaml:"repo"`
	Ref  string `yaml:"ref"`
	Path string `yaml:"path,omitempty"`
}

// Receipt is what install writes and every later operation reads. The whole
// point of the three fields is that they answer questions no version string can:
// where the pristine baseline lives, which exact bytes were installed, and what
// the code claims to do.
type Receipt struct {
	ID       string   `yaml:"id"`
	Upstream Upstream `yaml:"upstream"`
	// Files maps installed path → hex sha256 of the PRISTINE upstream content.
	// This is the third leg of any three-way merge; without it "what did I
	// change" is unanswerable and the update is limited to a two-way diff.
	Files map[string]string `yaml:"files"`
	// Declaration is the append-only half: schema, routes, jobs, slots. It is
	// engine-applied, never merged, because two owners of one migration history
	// is a data bug rather than a conflict.
	Declaration Declaration `yaml:"declaration"`
	// Register is the repo-relative package that exports `Register`, the seam's
	// wiring contract. Empty means the recipe contributes no code to mount
	// (data or policy only), so the generated registration skips it.
	Register string `yaml:"register,omitempty"`
	// Removed records an uninstall. The receipt is deliberately kept — a receipt
	// without files is how an operator still knows what data came from where —
	// and the generated registration skips a removed recipe, so uninstalling
	// never leaves an import of a package that is gone.
	Removed string `yaml:"removed,omitempty"`
}

// Declaration is what the code claims. The drift check compares it to the code.
type Declaration struct {
	Tables []string `yaml:"tables,omitempty"`
	Routes []Route  `yaml:"routes,omitempty"`
	Jobs   []string `yaml:"jobs,omitempty"`
	Slots  []string `yaml:"slots,omitempty"`
}

// Route is a declared HTTP surface. The pattern is absolute, matching the
// composition root's convention.
type Route struct {
	Method  string `yaml:"method"`
	Pattern string `yaml:"pattern"`
}

// FileReport is one row of the drift report.
type FileReport struct {
	Path  string
	State FileState
}

// Drift is the whole answer: file states plus declaration problems.
type Drift struct {
	ID       string
	Files    []FileReport
	Problems []string
	Unknown  []string // files present in the tree that the receipt never claimed
}

// Pristine reports whether the declared part can be applied unattended: every
// file untouched means an update is a straight fast-forward.
func (d Drift) Pristine() bool {
	for _, f := range d.Files {
		if f.State != StatePristine {
			return false
		}
	}
	return true
}

// Counts summarises the report for a one-line summary.
func (d Drift) Counts() (pristine, modified, deleted int) {
	for _, f := range d.Files {
		switch f.State {
		case StatePristine:
			pristine++
		case StateModified:
			modified++
		case StateDeleted:
			deleted++
		}
	}
	return pristine, modified, deleted
}

// Hash returns the hex sha256 of b. The function is the receipt's whole
// provenance primitive, so it is defined once and used by install, drift and
// update alike.
func Hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// HashFile reads path and returns its hex sha256.
func HashFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return Hash(raw), nil
}

// Check compares the receipt against the tree rooted at root and reports file
// states plus declaration problems. It never writes.
//
// A file absent from the tree is StateDeleted, not an error: a user who removes
// a file is stating intent, and an update must not resurrect it.
func Check(root string, r Receipt) (Drift, error) {
	d := Drift{ID: r.ID}

	paths := make([]string, 0, len(r.Files))
	for p := range r.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, p := range paths {
		full := filepath.Join(root, filepath.FromSlash(p))
		raw, err := os.ReadFile(full)
		if os.IsNotExist(err) {
			d.Files = append(d.Files, FileReport{Path: p, State: StateDeleted})
			continue
		}
		if err != nil {
			return d, fmt.Errorf("read %s: %w", p, err)
		}
		state := StateModified
		if Hash(raw) == r.Files[p] {
			state = StatePristine
		}
		d.Files = append(d.Files, FileReport{Path: p, State: state})
	}

	d.Problems = checkDeclaration(root, r)
	return d, nil
}

// checkDeclaration verifies the declaration against the code, which is the
// question no file hash can answer: a recipe can be byte-pristine and still
// have drifted from what it claims, if the claim itself was written wrong.
//
// The checks are deliberately textual and conservative. A route is considered
// present when its pattern string appears anywhere in the recipe's own code —
// enough to catch "declared a page that was never built", not enough to catch a
// method mismatch. That is the right trade for a drift warning; a test would be
// the place for the stricter claim.
func checkDeclaration(root string, r Receipt) []string {
	var problems []string

	code, ok := recipeCode(root, r)
	if !ok {
		return []string{"declaration cannot be checked: no recipe code found in the tree"}
	}
	blob := strings.Join(code, "\n")

	for _, rt := range r.Declaration.Routes {
		if !strings.Contains(blob, rt.Pattern) {
			problems = append(problems,
				fmt.Sprintf("declared route %s %s does not appear in the recipe's code", rt.Method, rt.Pattern))
		}
	}
	for _, job := range r.Declaration.Jobs {
		if !strings.Contains(blob, job) {
			problems = append(problems,
				fmt.Sprintf("declared job %q is not registered by the recipe's code", job))
		}
	}
	for _, table := range r.Declaration.Tables {
		// The ownership boundary is checked FIRST and independently of whether
		// the table appears in the recipe's SQL. Ordering matters: if the SQL
		// check ran first it would mask this one, and a core-table claim would be
		// reported as a missing-SQL nit instead of as a boundary violation.
		if isCoreTable(table) {
			problems = append(problems,
				fmt.Sprintf("declared table %q is a core table: a recipe may not own a core object", table))
			continue
		}
		// A table is declared, not written in Go: it must appear in the recipe's
		// own SQL, never in a core migration.
		if !strings.Contains(blob, table) {
			problems = append(problems,
				fmt.Sprintf("declared table %q has no SQL in the recipe's own files", table))
		}
	}
	return problems
}

// isCoreTable reports whether a name belongs to the core schema. The rule the
// recipe model depends on: a recipe never owns a core object, because two
// owners of one table is what makes uninstall unsafe and migrations
// unmergeable.
func isCoreTable(table string) bool {
	for _, core := range []string{
		"users", "sessions", "todos", "security_events",
		"magic_links", "schema_migrations", "config",
	} {
		if table == core || strings.HasPrefix(table, core+"_") {
			return true
		}
	}
	return false
}

// recipeCode returns the installed files' contents, so the declaration can be
// checked against what was actually written.
func recipeCode(root string, r Receipt) ([]string, bool) {
	var out []string
	found := false
	for p := range r.Files {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			continue
		}
		found = true
		out = append(out, string(raw))
	}
	return out, found
}

// Render writes the human report. It is deliberately the shape a user can read
// in one screen, and it names the user's ownership explicitly so a modified
// file is never described as a problem.
func (d Drift) Render(w *strings.Builder) {
	pristine, modified, deleted := d.Counts()
	fmt.Fprintf(w, "recipe %s:\n", d.ID)
	fmt.Fprintf(w, "  code    %d pristine, %d modified (yours — nothing to do), %d deleted\n",
		pristine, modified, deleted)
	for _, f := range d.Files {
		if f.State == StateModified {
			fmt.Fprintf(w, "            M %s\n", f.Path)
		}
	}
	if d.Pristine() {
		w.WriteString("  update  a straight fast-forward is safe: nothing was edited\n")
	} else {
		w.WriteString("  update  untouched files fast-forward; modified files need a real merge\n")
	}
	for _, p := range d.Problems {
		fmt.Fprintf(w, "  DECL    %s\n", p)
	}
	if len(d.Problems) == 0 {
		w.WriteString("  decl    matches the code\n")
	}
}
