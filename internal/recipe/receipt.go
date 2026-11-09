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

	"github.com/calionauta/gogogo/internal/dbschema"
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
	// Metrics are the dashboard's labeled counts (one COUNT query per
	// entry, `?` bound to the viewing owner). Labels are the user's
	// words; the dashboard groups them per recipe.
	Metrics []Metric `yaml:"metrics,omitempty"`
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
	// Slots are the third surface a declaration can claim out of thin air: the
	// code can be byte-pristine and still contribute nothing to the slot it
	// declared, or declare a slot this build has no page for.
	for _, slot := range r.Declaration.Slots {
		if offer := SlotProblems([]string{slot}); len(offer) > 0 {
			problems = append(problems, offer...)
			continue
		}
		if !slotContributed(blob, slot) {
			problems = append(problems,
				fmt.Sprintf("declared slot %q is not contributed by the recipe's code", slot))
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
	// The action table is the invocable surface: every verb must target a
	// route this same declaration claims. Pure data, checked alongside the
	// other declaration halves so a drifted table is refused at boot.
	problems = append(problems, actionProblems(r.Declaration)...)
	problems = append(problems, MetricProblems(r.Declaration)...)
	if ps := PanelProblems(r.Declaration); len(ps) > 0 {
		problems = append(problems, ps...)
	}
	return problems
}

// MetricProblems applies the one metric rule: query counts, owner-scoped,
// single placeholder. Exported so registration, the catalog and the
// dashboard executor share one definition of a valid metric.
func MetricProblems(d Declaration) []string {
	var out []string
	for _, m := range d.Metrics {
		if strings.TrimSpace(m.Label) == "" {
			out = append(out, "metric with an empty label")
		}
		if err := validCount(m.Query, m.BindsOwner()); err != nil {
			out = append(out, fmt.Sprintf("metric %q: %v", m.Label, err))
		}
	}
	return out
}

// PanelProblems applies the panel rule: snake_case component, prompt,
// at least one writable prop (the model must choose a slice).
func PanelProblems(d Declaration) []string {
	if d.Panel == nil {
		return nil
	}
	var out []string
	if !validPanelName(d.Panel.Component) {
		out = append(out, fmt.Sprintf("panel component %q must be snake_case", d.Panel.Component))
	}
	if strings.TrimSpace(d.Panel.Prompt) == "" {
		out = append(out, fmt.Sprintf("panel %q has an empty prompt", d.Panel.Component))
	}
	if len(d.Panel.Writable) == 0 {
		out = append(out, fmt.Sprintf("panel %q declares no writable prop", d.Panel.Component))
	}
	for _, w := range d.Panel.Writable {
		if strings.TrimSpace(w) == "" {
			out = append(out, fmt.Sprintf("panel %q declares an empty writable prop", d.Panel.Component))
		}
	}
	if len(d.Panel.Writable) > 1 {
		out = append(out, fmt.Sprintf(
			"panel %q declares %d writable props; by convention it is exactly one (the slice)",
			d.Panel.Component, len(d.Panel.Writable)))
	}
	if len(d.Panel.Values) > 0 {
		for _, v := range d.Panel.Values {
			if !strings.EqualFold(v, "all") && !isClosedToken(v) {
				out = append(out, fmt.Sprintf("panel %q value %q is not a closed token", d.Panel.Component, v))
			}
		}
	}
	return out
}

// IsCoreTable reports whether a name belongs to the core schema. Exported for
// the catalog validator, which applies the same boundary to a reviewed entry.
func IsCoreTable(table string) bool { return isCoreTable(table) }

// validCount reports whether query is a single COUNT(*) with one `?`
// (owner-scoped) or none (global). Anything else fails fast at
// registration, not as a broken SQL string at render time.
func validCount(query string, ownerBinds bool) error {
	q := strings.TrimSpace(strings.ToLower(query))
	if !strings.HasPrefix(q, "select count") {
		return fmt.Errorf("query must select count(*), got %q", query)
	}
	n := strings.Count(query, "?")
	want := 1
	if !ownerBinds {
		want = 0
	}
	if n != want {
		return fmt.Errorf("query must bind %d owner parameter (?), got %d", want, n)
	}
	return nil
}

// isClosedToken reports whether v is one word a prompt may offer the
// model (no spaces, no punctuation — the slice enum must be copyable
// from the sentence into props).
func isClosedToken(v string) bool {
	if strings.TrimSpace(v) == "" {
		return false
	}
	for _, r := range v {
		lower := (r >= 'a' && r <= 'z')
		upper := (r >= 'A' && r <= 'Z')
		digit := (r >= '0' && r <= '9')
		if lower || upper || digit || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

// validPanelName reports whether name is snake_case (lowercase letters,
// digits, underscores, no leading/trailing underscore).
func validPanelName(name string) bool {
	if name == "" || strings.HasPrefix(name, "_") || strings.HasSuffix(name, "_") {
		return false
	}
	for _, r := range name {
		lower := (r >= 'a' && r <= 'z')
		digit := (r >= '0' && r <= '9')
		if lower || digit || r == '_' {
			continue
		}
		return false
	}
	return true
}

// isCoreTable reports whether a name belongs to the core schema. The rule the
// recipe model depends on: a recipe never owns a core object, because two
// owners of one table is what makes uninstall unsafe and migrations
// unmergeable.
//
// The set comes from dbschema, which owns it and gates it against the migrated
// schema in both directions. The inline list this replaces was a stale copy in
// the wrong package: it named two tables that do not exist and missed nine that
// do, so `notes` and `boards` were claimable.
func isCoreTable(table string) bool {
	name := strings.ToLower(table)
	for _, core := range dbschema.CoreTables() {
		if name == core || strings.HasPrefix(name, core+"_") {
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
