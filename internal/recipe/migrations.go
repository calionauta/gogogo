// SCOPE:plugin — the schema half of the seam: a recipe's DDL is its own file,
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
	// Migrations returns the recipe's .sql files, at the root of the filesystem
	// or in subdirectories; every one of them is read in filename order.
	Migrations() fs.FS
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
		sets = append(sets, dbschema.MigrationSet{ID: p.ID(), FS: m.Migrations()})
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

	declared := make(map[string]string, len(d.Tables))
	for _, table := range d.Tables {
		declared[strings.ToLower(table)] = table
	}

	var problems []string
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
	out := map[string]bool{}
	if fsys == nil {
		return out, nil
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

	for _, file := range files {
		b, rerr := fs.ReadFile(fsys, file)
		if rerr != nil {
			return nil, rerr
		}
		for _, name := range scanCreateTable(stripSQLComments(string(b))) {
			out[name] = true
		}
	}
	return out, nil
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
