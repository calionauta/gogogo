// The schema half of the seam: a recipe's DDL is its own file,
// applied through the core ledger, and checked against its declaration.
//
// Why this is a second, optional interface instead of a fourth method on
// Provider: most recipes own no storage, and widening Provider would make every
// one of them implement a method that returns an empty filesystem. An optional
// interface keeps the seam's small promise intact and lets the gates ask a
// question only of the units that answer it.
package recipe

import (
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"

	"github.com/calionauta/gogogo/internal/dbschema"
)

// Migrator is what a recipe with its own DDL implements. The files land in the
// shared ledger under the recipe id, so two recipes may both number their files
// from 0001 and a boot applies them in one pass (dbschema.MigrateWith).
type Migrator interface {
	// Migrations returns the recipe's DDL. The usual shape is an embed.FS whose
	// `migrations/` subdirectory holds the .sql files — the convention the
	// installer mirrors for sqlc and the authoring skill documents — and a
	// filesystem rooted AT its migrations is accepted too. Anything else is read
	// as nothing, which is why migrationDirFor looks instead of assuming.
	Migrations() fs.FS
}

// migrationDir is the subdirectory a recipe's DDL normally lives in. The name is
// shared with the installer's own convention on purpose: `recipe install`
// mirrors these files for sqlc, and a runtime that read a different directory
// would apply a schema codegen never saw.
const migrationDir = "migrations"

// migrationDirFor picks the directory to read inside a recipe's filesystem: the
// `migrations/` subdirectory when it exists, otherwise the filesystem's root.
//
// Getting this wrong is silent, not loud: the ledger simply records no versions
// and the recipe's table is never created, while boot logs look healthy. The
// reference recipe shipped its DDL in `migrations/` and hit exactly that — the
// sets were built with an empty Dir, so the app booted with no table.
func migrationDirFor(fsys fs.FS) string {
	if fsys == nil {
		return ""
	}
	if info, err := fs.Stat(fsys, migrationDir); err == nil && info.IsDir() {
		return migrationDir
	}
	return ""
}

// MigrationSets is the registry's view of the optional schema half: one set per
// provider that keeps its own DDL, sorted by id so a boot is deterministic.
func (r *Registry) MigrationSets() []dbschema.MigrationSet {
	var sets []dbschema.MigrationSet
	for _, p := range r.Providers() {
		m, ok := p.(Migrator)
		if !ok {
			continue
		}
		fsys := m.Migrations()
		sets = append(sets, dbschema.MigrationSet{ID: p.ID(), FS: fsys, Dir: migrationDirFor(fsys)})
	}
	// Providers() is already id-ordered; sorting here keeps the boot order a
	// property of this function rather than an assumption about its caller.
	sort.Slice(sets, func(i, j int) bool { return sets[i].ID < sets[j].ID })
	return sets
}

// SchemaError carries one problem per line, sorted, so a failing gate reads as
// a report instead of "first error wins".
type SchemaError struct {
	Problems []string
}

func (e *SchemaError) Error() string { return strings.Join(e.Problems, "\n") }

// ValidateSchema checks one recipe's DDL against the tables it declared.
//
// The comparison is bidirectional on purpose. A declared table without DDL is a
// declaration the schema cannot answer; a CREATE without a declaration is a
// table that would arrive with no policy row, no uninstall story and no owner
// the ledger can name. Both are boot-time facts here, not review findings.
//
// Comment stripping runs first: a commented-out CREATE is documentation, and a
// scanner that counts it would let a table look declared-and-created while
// nothing creates it.
func ValidateSchema(id string, d Declaration, fsys fs.FS) error {
	created, err := createdTables(fsys)
	if err != nil {
		return fmt.Errorf("recipe %q migrations: %w", id, err)
	}
	writes, err := coreTableWrites(id, fsys)
	if err != nil {
		return fmt.Errorf("recipe %q migrations: %w", id, err)
	}

	declared := make(map[string]string, len(d.Tables))
	for _, table := range d.Tables {
		declared[strings.ToLower(table)] = table
	}

	problems := writes
	for key, asDeclared := range declared {
		if !created[key] {
			problems = append(problems,
				fmt.Sprintf("recipe %q declares table %s but no migration creates it", id, asDeclared))
		}
	}
	for _, name := range sortedNames(created) {
		switch {
		case isCoreTable(name):
			problems = append(problems, fmt.Sprintf("recipe %q migration creates core table %s", id, name))
			continue
		case !HasRecipePrefix(name, id):
			problems = append(problems,
				fmt.Sprintf("recipe %q migration creates table %s without the ext_%s prefix", id, name, id))
		}
		if _, ok := declared[name]; !ok {
			problems = append(problems,
				fmt.Sprintf("recipe %q migration creates table %s that the declaration does not list", id, name))
		}
	}

	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return &SchemaError{Problems: problems}
}

// createdTables reads every .sql file under fsys and returns the tables it
// creates. A nil filesystem creates nothing, which is exactly what a recipe
// declaring no table should hand back.
func createdTables(fsys fs.FS) (map[string]bool, error) {
	bodies, err := sqlBodies(fsys)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, body := range bodies {
		for _, name := range scanCreateTable(body) {
			out[name] = true
		}
	}
	return out, nil
}

// sqlBodies returns every .sql file under fsys, comment-stripped, in sorted
// order. One walk feeds both scans, so the two cannot disagree about which
// statements a recipe's migrations contain.
func sqlBodies(fsys fs.FS) ([]string, error) {
	if fsys == nil {
		return nil, nil
	}

	var files []string
	err := fs.WalkDir(fsys, ".", func(p string, entry fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(p), ".sql") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)

	out := make([]string, 0, len(files))
	for _, file := range files {
		b, rerr := fs.ReadFile(fsys, file)
		if rerr != nil {
			return nil, rerr
		}
		out = append(out, stripSQLComments(string(b)))
	}
	return out, nil
}

// coreTableWrites reports each statement that MODIFIES a core table.
//
// createTableRE already refuses a CREATE of a core table, but that is one verb:
// the ledger would have applied an ALTER, a DROP, an index or a seed write to
// core data just as silently, and the declaration check never sees the SQL text.
//
// The boundary is target-based, not mention-based. A recipe may reference a
// core table — a foreign key to users is a dependency and the correct way to
// scope a recipe's rows to an owner — and may alter or index its OWN table. What
// it may not do is act ON a core object.
func coreTableWrites(id string, fsys fs.FS) ([]string, error) {
	bodies, err := sqlBodies(fsys)
	if err != nil {
		return nil, err
	}

	var problems []string
	seen := map[string]bool{}
	for _, body := range bodies {
		for _, stmt := range splitSQLStatements(body) {
			for _, rule := range coreWriteRules {
				m := rule.re.FindStringSubmatch(stmt)
				if m == nil {
					continue
				}
				name := tableFromMatch(m)
				if name == "" || !isCoreTable(name) {
					break
				}
				line := fmt.Sprintf("recipe %q migration %s core table %s — a recipe may not own a core object",
					id, rule.verb, name)
				if !seen[line] {
					seen[line] = true
					problems = append(problems, line)
				}
				break
			}
		}
	}
	return problems, nil
}

// coreWriteRules pairs each modifying verb with the phrase it reports. The
// table is the LAST capture group set: an index or trigger names its own object
// first, so identOnly skips that name without capturing it.
var coreWriteRules = []struct {
	verb string
	re   *regexp.Regexp
}{
	{"alters", regexp.MustCompile(`(?is)^\s*alter\s+table\s+(?:if\s+exists\s+)?` + identGroup)},
	{"drops", regexp.MustCompile(`(?is)^\s*drop\s+table\s+(?:if\s+exists\s+)?` + identGroup)},
	{"indexes", regexp.MustCompile(`(?is)^\s*create\s+(?:unique\s+)?index\s+(?:if\s+not\s+exists\s+)?` +
		identOnly + `\s+on\s+` + identGroup)},
	{"inserts into", regexp.MustCompile(`(?is)^\s*insert\s+(?:or\s+\w+\s+)?into\s+` + identGroup)},
	{"updates", regexp.MustCompile(`(?is)^\s*update\s+(?:or\s+\w+\s+)?` + identGroup)},
	{"deletes from", regexp.MustCompile(`(?is)^\s*delete\s+from\s+` + identGroup)},
	{"truncates", regexp.MustCompile(`(?is)^\s*truncate\s+(?:table\s+)?` + identGroup)},
	{"creates a trigger on", regexp.MustCompile(`(?is)^\s*create\s+(?:temp\s+|temporary\s+)?trigger\s+` +
		`(?:if\s+not\s+exists\s+)?` + identOnly + `.*?\bon\s+` + identGroup)},
}

// identGroup matches one SQL identifier and captures it; identOnly matches one
// without capturing it. The quote alternatives come first so a quoted name is
// not read as the bare tail of a qualified name.
const (
	identGroup = "(?:`([^`]+)`|\"([^\"]+)\"|\\[([^\\]]+)\\]|([A-Za-z0-9_$.]+))"
	identOnly  = "(?:`[^`]+`|\"[^\"]+\"|\\[[^\\]]+\\]|[A-Za-z0-9_$.]+)"
)

// tableFromMatch returns the identifier a rule captured, or "" when the match
// named none (which happens only for a malformed statement).
func tableFromMatch(match []string) string {
	for _, group := range match[1:] {
		if group != "" {
			return tableName(group)
		}
	}
	return ""
}

// splitSQLStatements splits on semicolons that are not inside a single-quoted
// literal, so a default value containing ";" does not cut a statement in half.
func splitSQLStatements(src string) []string {
	var out []string
	start := 0
	inQuote := false
	for i := 0; i < len(src); i++ {
		switch c := src[i]; {
		case c == '\'':
			if inQuote && i+1 < len(src) && src[i+1] == '\'' {
				i++ // an escaped quote stays inside the literal
				continue
			}
			inQuote = !inQuote
		case c == ';' && !inQuote:
			out = append(out, src[start:i])
			start = i + 1
		}
	}
	return append(out, src[start:])
}

// createTableRE matches the forms a hand-written migration actually uses. The
// quote alternatives come first so a quoted identifier is not read as the bare
// tail of a qualified name.
var createTableRE = regexp.MustCompile(
	"(?is)\\bcreate\\s+(?:temp\\s+|temporary\\s+)?table\\s+(?:if\\s+not\\s+exists\\s+)?" +
		"(?:`([^`]+)`|\"([^\"]+)\"|\\[([^\\]]+)\\]|([A-Za-z0-9_$.]+))")

func scanCreateTable(stmt string) []string {
	var out []string
	for _, match := range createTableRE.FindAllStringSubmatch(stmt, -1) {
		for _, group := range match[1:] {
			if group == "" {
				continue
			}
			if name := tableName(group); name != "" {
				out = append(out, name)
			}
			break
		}
	}
	return out
}

// tableName normalizes one captured identifier: the last dotted segment, no
// quoting, lower-cased, because SQL identifiers compare case-insensitively.
func tableName(raw string) string {
	if i := strings.LastIndexByte(raw, '.'); i >= 0 {
		raw = raw[i+1:]
	}
	raw = strings.Trim(strings.TrimSpace(raw), "\"`[]")
	return strings.ToLower(raw)
}

// stripSQLComments removes -- line comments and /* */ block comments while
// leaving string literals intact, so a default value containing "--" survives.
func stripSQLComments(src string) string {
	var b strings.Builder
	for i := 0; i < len(src); {
		switch {
		case strings.HasPrefix(src[i:], "--"):
			if j := strings.IndexByte(src[i:], '\n'); j >= 0 {
				i += j // the newline itself is emitted by the default branch
			} else {
				i = len(src)
			}
		case strings.HasPrefix(src[i:], "/*"):
			if j := strings.Index(src[i+2:], "*/"); j >= 0 {
				i += j + 4
			} else {
				i = len(src)
			}
			b.WriteByte(' ')
		case src[i] == '\'':
			b.WriteByte(src[i])
			i++
			for i < len(src) {
				c := src[i]
				b.WriteByte(c)
				i++
				if c != '\'' {
					continue
				}
				if i < len(src) && src[i] == '\'' { // escaped quote
					b.WriteByte(src[i])
					i++
					continue
				}
				break
			}
		default:
			b.WriteByte(src[i])
			i++
		}
	}
	return b.String()
}

func sortedNames(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
