// SCOPE:layer=infra,removal=plugin — installer engine: strip whitespace hygiene
package installer

import (
	"go/format"
	"strings"
	"testing"
)

// TestStripFileCountedOutputIsGofmtClean guards a bug the real
// `gogogo --trim dagnats` path produced: a strip removes whole lines but not
// the blank line that separated them from their neighbours, so a removal whose
// surrounding blocks were already blank-separated left two or three consecutive
// empty lines — and the tail-strip left the file without a final newline.
// gofmt flags both, so every scaffolded project failed its own `make fmt` the
// moment it was created from a trimmed scaffold.
func TestStripFileCountedOutputIsGofmtClean(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"package main",
		"",
		"func main() {",
		"\tfirst()",
		"",
		"\t// bounded to the process lifetime",
		"\t// and documented across two lines",
		"\tctx, stop := lifecycle()",
		"\tdefer stop()",
		"",
		"\t// removal target",
		"\tengine(ctx)",
		"",
		"\tlast()",
		"}",
		"",
		"",
	}, "\n")

	dir := t.TempDir()
	writeFile(t, dir+"/main.go", src)

	tree, err := openTree(dir)
	if err != nil {
		t.Fatalf("openTree: %v", err)
	}
	defer func() { _ = tree.Close() }()

	rc := &UnitReceipt{}
	rules := []stripRule{{
		startMarker: "engine(ctx)",
		endMarker:   "engine(ctx)",
		alsoDeleteContains: []string{
			"ctx, stop := lifecycle()",
			"defer stop()",
			"// bounded to the process lifetime",
			"// and documented across two lines",
			"// removal target",
		},
	}}
	if stripErr := stripFileCounted(tree, dir+"/main.go", rules, rc); stripErr != nil {
		t.Fatalf("stripFileCounted: %v", stripErr)
	}
	if rc.StripsApplied != 1 {
		t.Fatalf("expected 1 strip applied, got %d", rc.StripsApplied)
	}

	got, readErr := tree.ReadFile(dir + "/main.go")
	if readErr != nil {
		t.Fatalf("read back: %v", readErr)
	}
	out := string(got)

	if strings.Contains(out, "engine(ctx)") || strings.Contains(out, "lifecycle()") {
		t.Errorf("strip left removed code behind:\n%s", out)
	}
	if strings.Contains(out, "\n\n\n") {
		t.Errorf("strip left a blank-line run:\n%q", out)
	}
	if !strings.HasSuffix(out, "}\n") {
		t.Errorf("output must end with exactly one newline, got %q", out[len(out)-8:])
	}
	if _, err := format.Source(got); err != nil {
		t.Errorf("stripped output is not gofmt-valid: %v\n%s", err, out)
	}
}

// TestCollapseBlankRuns pins the helper directly, including the EOF case that
// the strip path depends on.
func TestCollapseBlankRuns(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"no blanks", []string{"a", "b"}, []string{"a", "b"}},
		{"single blank kept", []string{"a", "", "b"}, []string{"a", "", "b"}},
		{"run collapsed", []string{"a", "", "", "", "b"}, []string{"a", "", "b"}},
		{"leading blanks dropped", []string{"", "", "a"}, []string{"a"}},
		{"trailing blanks dropped", []string{"a", "", "", ""}, []string{"a"}},
		{"whitespace-only counts as blank", []string{"a", "  ", "\t", "b"}, []string{"a", "", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := collapseBlankRuns(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %q, want %q", got, tc.want)
				}
			}
		})
	}
}
