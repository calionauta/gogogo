// Spike 7: the catalog entry, as a format rather than a service.
//
// The catalog is a reviewed set of behaviors, not a directory of everything that
// exists. This package publishes the artifact the review produces — one YAML per
// version — and the rules an entry must pass before a project may rely on it.
// It deliberately stops at validation: resolution, download and hash comparison
// are the install path's job, and the entry is what that path consumes.
package recipe

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// catalogIDRE is the recipe id shape the receipt and the catalog share.
var catalogIDRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// catalogVersionRE is the version shape a reviewed artifact carries: a v-prefixed
// semver, optionally with a pre-release suffix.
var catalogVersionRE = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

// catalogHashRE accepts the two spellings a review produces: the bare hex digest
// the receipt stores, and the `sha256:` prefixed form the catalog example uses.
var catalogHashRE = regexp.MustCompile(`^(sha256:)?[a-f0-9]{64}$`)

// CatalogEntry is one reviewed artifact: catalog/<id>/<version>.yaml.
//
// The fields are the review's claims. Two of them are deliberate pointers or
// lists rather than free text, because the evidence says the operator pays for
// what is not declared: UpstreamDeps names the module closure (Spike 7 measured
// tens of megabytes), and MinCore names the compatibility claim (without it an
// upgrade is advisory by accident).
type CatalogEntry struct {
	ID      string            `yaml:"id"`
	Version string            `yaml:"version"`
	Source  Upstream          `yaml:"source"`
	Files   map[string]string `yaml:"files"`
	License string            `yaml:"license"`
	MinCore string            `yaml:"minCore"`
	// Recommended marks this catalog recipe as advised at scaffold time.
	// The installer recommends it in the plan/advise; installation still
	// requires explicit acceptance. The catalog maintainer flips this over
	// time without code changes.
	Recommended bool `yaml:"recommended,omitempty"`
	// Register is the repo-relative package exporting `Register`, when the
	// entry's code mounts through the seam. Empty is a data-only recipe.
	Register string `yaml:"register,omitempty"`
	// Module is the Go module the code was written against; empty means the
	// source repo path, which is how Go modules are usually named.
	Module    string   `yaml:"module,omitempty"`
	WorksWith []string `yaml:"worksWith,omitempty"`
	Slots     []string `yaml:"slots,omitempty"`
	Routes    []Route  `yaml:"routes,omitempty"`
	Tables    []string `yaml:"tables,omitempty"`
	Jobs      []string `yaml:"jobs,omitempty"`
	// UpstreamDeps is a pointer so "absent" and "empty" are different claims:
	// absent is a missing declaration, empty is "this recipe needs nothing".
	UpstreamDeps    *[]string `yaml:"upstreamDeps"`
	ReviewedAt      string    `yaml:"reviewedAt"`
	ReviewedBy      string    `yaml:"reviewedBy"`
	ReviewedAgainst string    `yaml:"reviewedAgainst"`
	Notes           string    `yaml:"notes,omitempty"`
}

// IsValidVersion reports whether v is a version a reviewed artifact carries:
// a v-prefixed semver. It is the one shape validator shared by the catalog
// entry and the installable manifest, so the two halves never disagree on
// what a version is.
func IsValidVersion(v string) bool { return catalogVersionRE.MatchString(v) }

// LoadCatalogEntry reads and validates one entry. Validation problems are
// returned as an error so a broken artifact fails at load, not at install.
func LoadCatalogEntry(file string) (CatalogEntry, error) {
	var e CatalogEntry
	raw, err := os.ReadFile(file)
	if err != nil {
		return e, err
	}
	if uerr := yaml.Unmarshal(raw, &e); uerr != nil {
		return e, fmt.Errorf("recipe: parse %s: %w", file, uerr)
	}
	if problems := e.Validate(); len(problems) > 0 {
		return e, fmt.Errorf("recipe: invalid catalog entry %s:\n  - %s", file, strings.Join(problems, "\n  - "))
	}
	return e, nil
}

// Validate applies the review rules. It returns every problem, sorted, so one
// run fixes all of them instead of one per attempt. Split into four groups for
// the complexity gate; the order inside surfaceProblems matters (see there).
func (e CatalogEntry) Validate() []string {
	out := e.identityProblems()
	out = append(out, e.fileProblems()...)
	out = append(out, e.surfaceProblems()...)
	out = append(out, e.reviewProblems()...)
	sort.Strings(out)
	return out
}

// identityProblems covers the fields that say what this artifact is.
func (e CatalogEntry) identityProblems() []string {
	var out []string
	if !catalogIDRE.MatchString(e.ID) {
		out = append(out, fmt.Sprintf(
			"id %q must be lower-case letters/digits/dashes and start with a letter", e.ID))
	}
	if !catalogVersionRE.MatchString(e.Version) {
		out = append(out, fmt.Sprintf(
			"version %q must be vMAJOR.MINOR.PATCH (optionally with a pre-release suffix)", e.Version))
	}
	if e.Source.Repo == "" {
		out = append(out, "source.repo is empty: the pristine baseline must be locatable")
	}
	if e.Source.Ref == "" {
		out = append(out, "source.ref is empty: an entry without a ref installs an unpinned tree")
	}
	if e.License == "" {
		out = append(out, "license is required: a recipe the operator cannot legally use is not usable")
	}
	if e.MinCore == "" {
		out = append(out, "minCore is required: without it the upgrade claim is advisory by accident")
	}
	return out
}

// fileProblems covers the copied file set and its hashes.
func (e CatalogEntry) fileProblems() []string {
	var out []string
	if len(e.Files) == 0 {
		out = append(out, "files is empty: a recipe with nothing to copy is not installable")
	}
	for p, hash := range e.Files {
		if !safeRelPath(p) {
			out = append(out, fmt.Sprintf("files key %q must be a clean repo-relative path", p))
		}
		if !catalogHashRE.MatchString(hash) {
			out = append(out, fmt.Sprintf("files[%s] is not a sha256 digest (%q)", p, hash))
		}
	}
	return out
}

// surfaceProblems covers what the declaration claims. A core table is reported
// FIRST and independently of the prefix rule: a core claim is a boundary
// violation, not a naming nit, and ordering it second would mask it.
func (e CatalogEntry) surfaceProblems() []string {
	var out []string
	for _, t := range e.Tables {
		if IsCoreTable(t) {
			out = append(out, fmt.Sprintf(
				"table %q is a core table: a recipe may not own a core object", t))
			continue
		}
		if !strings.HasPrefix(t, "ext_"+e.ID) {
			out = append(out, fmt.Sprintf(
				"table %q must be prefixed ext_%s: the engine's namespace is what makes "+
					"uninstall and migrations safe", t, e.ID))
		}
	}
	for _, j := range e.Jobs {
		if !HasRecipePrefix(j, e.ID) {
			out = append(out, fmt.Sprintf("job %q must be prefixed ext:%s:", j, e.ID))
		}
	}
	// The review refuses an unhosted slot as well as an unknown one: a reviewed
	// recipe claiming content this build does not draw would install cleanly and
	// render nothing.
	out = append(out, SlotProblems(e.Slots)...)
	for _, w := range e.WorksWith {
		if w == "" {
			out = append(out, "worksWith holds an empty set name")
		}
	}
	if e.Register != "" {
		if !safeRelPath(e.Register) {
			out = append(out, fmt.Sprintf(
				"register %q must be a clean repo-relative package path", e.Register))
		} else if !catalogHasDir(e.Files, e.Register) {
			out = append(out, fmt.Sprintf(
				"register names %q, but files holds nothing under it: the generated "+
					"registration would import a package the entry does not ship", e.Register))
		}
	}
	return out
}

// catalogHasDir reports whether the entry ships code under dir.
func catalogHasDir(files map[string]string, dir string) bool {
	prefix := strings.TrimSuffix(dir, "/") + "/"
	for p := range files {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// reviewProblems covers the claims that make an entry a review rather than a
// listing: who reviewed it, against what, and what it costs to install.
func (e CatalogEntry) reviewProblems() []string {
	var out []string
	if e.UpstreamDeps == nil {
		out = append(out, "upstreamDeps is required: an entry that does not name its dependency "+
			"closure hands the operator a surprise (an empty list is a valid no-dependency claim)")
	}
	if e.ReviewedAt == "" || e.ReviewedBy == "" || e.ReviewedAgainst == "" {
		out = append(out, "reviewedAt, reviewedBy and reviewedAgainst are required: an entry is a "+
			"review, not a licence to exist")
	}
	return out
}

// ScanCatalog reads every reviewed entry under dir, grouped by id, in the
// layout an entry is committed: catalog/<id>/<version>.yaml. A directory that
// does not exist is not an error — it is a project with no catalog, and every
// installed recipe reports unknown — but a present entry that fails validation
// is: an unreadable review must fail at scan time, not at install. Which
// entry is the latest is the caller's job; this only loads, sorted per id so
// the answer is deterministic.
func ScanCatalog(dir string) (map[string][]CatalogEntry, error) {
	out := map[string][]CatalogEntry{}
	ids, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if !id.IsDir() {
			continue
		}
		if err := scanCatalogID(dir, id.Name(), out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// scanCatalogID loads every YAML file under one id directory.
func scanCatalogID(dir, id string, out map[string][]CatalogEntry) error {
	files, err := os.ReadDir(filepath.Join(dir, id))
	if err != nil {
		return err
	}
	names := []string{}
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		switch filepath.Ext(f.Name()) {
		case ".yaml", ".yml":
			names = append(names, f.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		file := filepath.Join(dir, id, name)
		entry, err := LoadCatalogEntry(file)
		if err != nil {
			return err
		}
		if entry.ID != id {
			return fmt.Errorf("recipe: catalog entry %s claims id %q (directory %q)",
				file, entry.ID, id)
		}
		out[id] = append(out[id], entry)
	}
	return nil
}

// safeRelPath reports whether p is a clean, repo-relative path: no absolute
// prefix, no parent traversal, no backslashes.
func safeRelPath(p string) bool {
	if p == "" || path.IsAbs(p) || strings.Contains(p, `\`) {
		return false
	}
	clean := path.Clean(p)
	return clean == p && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}
