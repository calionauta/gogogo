// Spike 2: recipe receipt and drift detection.
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
	ID string `yaml:"id"`
	// Summary is what the recipe does, in the author's words. Without it
	// discovery can only show the upstream repo path, which tells an agent
	// where the code came from and nothing about whether to use it.
	Summary  string   `yaml:"summary,omitempty"`
	Version  string   `yaml:"version,omitempty"`
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
	Tables  []string       `yaml:"tables,omitempty"`
	Routes  []Route        `yaml:"routes,omitempty"`
	Jobs    []string       `yaml:"jobs,omitempty"`
	Slots   []string       `yaml:"slots,omitempty"`
	Actions []InvokeAction `yaml:"actions,omitempty"`
	// Omitted lists mutating routes the author deliberately keeps out of the
	// headless surface. The reason is part of the declaration, so `recipe
	// check` can tell a decision from a forgotten route.
	Omitted []OmittedRoute `yaml:"omitted,omitempty"`
	// Metrics are the dashboard's labeled counts (one COUNT query per
	// entry, `?` bound to the viewing owner). Labels are the user's
	// words; the dashboard groups them per recipe.
	Metrics []Metric `yaml:"metrics,omitempty"`
	// Outcomes are the jobs this recipe claims to do, each declared as the
	// pair of counts that answers it (success over eligible). Optional and
	// additive: a receipt without them is valid and scores the same, which
	// is what keeps an older install on this build.
	Outcomes []Outcome `yaml:"outcomes,omitempty"`
	// Panel is the chat answer slice this recipe offers (optional — a
	// recipe with no list-shaped data declares none). The component
	// name, prompt and writable props live here so the dashboard,
	// /config and the model read the same words; Fill/Render still
	// live in code (they must call the page's own component).
	Panel *PanelSpec `yaml:"panel,omitempty"`
}

// Metric is one dashboard count a recipe declares: a label for the
// user, a COUNT(*) query, and the odometer signal name (default:
// derived from the label).
//
// OwnerBinds reports whether the query scopes to the viewing owner
// (one `?`). It defaults to TRUE (convention: a per-user dashboard);
// a recipe counting global rows (shared docs, instance totals) sets
// false. The rule is fail-fast: exactly one `?` when OwnerBinds, none
// otherwise.
type Metric struct {
	Label      string `yaml:"label"`
	Query      string `yaml:"query"`
	Signal     string `yaml:"signal,omitempty"`
	OwnerBinds *bool  `yaml:"ownerBinds,omitempty"`
}

// BindsOwner reports whether the metric is owner-scoped. nil means
// the convention default (true).
func (m Metric) BindsOwner() bool {
	if m.OwnerBinds == nil {
		return true
	}
	return *m.OwnerBinds
}

// WithoutOwner is the global-rows spelling (OwnerBinds=false).
func WithoutOwner() *bool {
	f := false
	return &f
}

// PanelSpec names the chat answer slice a recipe offers. Component is
// the model-facing name (snake_case); Values is the closed slice enum
// (e.g. [open done all]); Writable lists the props the MODEL may set —
// by convention exactly one, the slice prop. Fill/Render stay in code
// (they must call the page's own component); this is the name, the
// prompt, and the closed vocabulary the model is given.
type PanelSpec struct {
	Component string   `yaml:"component"`
	Prompt    string   `yaml:"prompt"`
	Writable  []string `yaml:"writable"`
	Values    []string `yaml:"values,omitempty"`
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
