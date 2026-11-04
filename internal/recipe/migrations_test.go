package recipe

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

// migratingProvider is a declaration plus a filesystem: the shape a recipe with
// its own DDL hands back, without needing a real recipe package.
type migratingProvider struct {
	fakeProvider
	files fs.FS
}

func (m migratingProvider) Migrations() fs.FS { return m.files }

func mapFS(files map[string]string) fs.FS {
	out := fstest.MapFS{}
	for name, body := range files {
		out[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return out
}

func TestValidateSchema(t *testing.T) {
	const id = "pomodoro"

	cases := []struct {
		name   string
		tables []string
		files  map[string]string
		// want is the set of problem lines expected. Empty means the schema
		// must validate; a non-empty set must match line-for-line in count, so
		// an extra problem cannot hide behind a substring match.
		want []string
	}{
		{
			name:   "matching declaration and create",
			tables: []string{"ext_pomodoro_cards"},
			files: map[string]string{
				"0001_init.sql": "CREATE TABLE IF NOT EXISTS ext_pomodoro_cards (id TEXT PRIMARY KEY);",
			},
		},
		{
			name:   "declared without create",
			tables: []string{"ext_pomodoro_cards"},
			files: map[string]string{
				"0001_init.sql": "SELECT 1;",
			},
			want: []string{`recipe "pomodoro" declares table ext_pomodoro_cards but no migration creates it`},
		},
		{
			name:   "create without declaration",
			tables: nil,
			files: map[string]string{
				"0001_init.sql": "CREATE TABLE ext_pomodoro_extra (id TEXT);",
			},
			want: []string{`recipe "pomodoro" migration creates table ext_pomodoro_extra that the declaration does not list`},
		},
		{
			// The commented-out ghosts are documentation of a dropped idea; a
			// scanner that counts them would pass a declaration nothing creates.
			name:   "commented-out create is ignored",
			tables: []string{"ext_pomodoro_cards"},
			files: map[string]string{
				"0001_init.sql": "-- CREATE TABLE ext_pomodoro_ghost (id TEXT);\n" +
					"/*\nCREATE TABLE ext_pomodoro_also_ghost (id TEXT);\n*/\n" +
					"CREATE TABLE ext_pomodoro_cards (id TEXT);",
			},
		},
		{
			name:   "unprefixed table is refused",
			tables: []string{"cards"},
			files: map[string]string{
				"0001_init.sql": "CREATE TABLE cards (id TEXT);",
			},
			want: []string{`recipe "pomodoro" migration creates table cards without the ext_pomodoro prefix`},
		},
		{
			name:   "core table is refused",
			tables: []string{"todos"},
			files: map[string]string{
				"0001_init.sql": "CREATE TABLE IF NOT EXISTS todos (id TEXT);",
			},
			want: []string{`recipe "pomodoro" migration creates core table todos`},
		},
		{
			// Quoted identifiers and a subdirectory are the two shapes a real
			// migration set uses; both must be read, not skipped.
			name:   "quoted identifiers in a subdirectory",
			tables: []string{"ext_pomodoro_notes", "ext_pomodoro_tags"},
			files: map[string]string{
				"migrations/0001_init.sql": `CREATE TABLE IF NOT EXISTS "ext_pomodoro_notes" (id TEXT);`,
				"migrations/0002_tags.sql": "CREATE TABLE IF NOT EXISTS `ext_pomodoro_tags` (id TEXT);",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSchema(id, Declaration{Tables: tc.tables}, mapFS(tc.files))
			if len(tc.want) == 0 {
				if err != nil {
					t.Fatalf("schema must validate, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected %v, got no error", tc.want)
			}
			got := err.Error()
			for _, line := range tc.want {
				if !strings.Contains(got, line) {
					t.Errorf("missing problem line %q in:\n%s", line, got)
				}
			}
			if lines := strings.Count(got, "\n") + 1; lines != len(tc.want) {
				t.Errorf("expected %d problem line(s), got %d:\n%s", len(tc.want), lines, got)
			}
		})
	}
}

// TestValidateSchemaProblemsAreSorted pins the stability claim: a report whose
// order depends on map iteration is unreadable in CI and unstable in a diff.
func TestValidateSchemaProblemsAreSorted(t *testing.T) {
	err := ValidateSchema("pomodoro",
		Declaration{Tables: []string{"ext_pomodoro_b", "ext_pomodoro_a"}},
		mapFS(map[string]string{"0001_init.sql": "SELECT 1;"}))
	if err == nil {
		t.Fatal("expected two missing tables")
	}
	lines := strings.Split(err.Error(), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "ext_pomodoro_a") || !strings.Contains(lines[1], "ext_pomodoro_b") {
		t.Fatalf("problems must be sorted, got:\n%s", err.Error())
	}
}

func TestMigrationSetsOnlyForMigrators(t *testing.T) {
	reg := &Registry{}
	reg.Add(fakeProvider{id: "zeta"}) // declaration only, no DDL
	noop := mapFS(map[string]string{"0001_init.sql": "SELECT 1;"})
	beta := migratingProvider{fakeProvider{id: "beta"}, noop}
	alpha := migratingProvider{fakeProvider{id: "alpha"}, noop}
	reg.Add(beta)
	reg.Add(alpha)

	sets := reg.MigrationSets()
	if len(sets) != 2 {
		t.Fatalf("expected one set per migrator, got %d", len(sets))
	}
	if sets[0].ID != "alpha" || sets[1].ID != "beta" {
		t.Fatalf("sets must be id-ordered, got %q then %q", sets[0].ID, sets[1].ID)
	}
	if sets[0].Dir != "" {
		t.Errorf("a recipe set is read from its filesystem root, got dir %q", sets[0].Dir)
	}
}
