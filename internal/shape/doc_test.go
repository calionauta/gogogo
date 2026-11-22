package shape

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// writeShapeDoc lays down the tree's docs/shape.md with the given table body.
// The header and separator are written here so each case states only the rows
// it cares about, and a case about a MISSING header can pass an empty body.
func writeShapeDoc(t *testing.T, root, body string) {
	t.Helper()
	// The gate also checks that every entrypoint the enum promises EXISTS, so a
	// fixture has to look like a tree: without these, every case would fail on
	// six missing directories and the assertion would pass for the wrong reason.
	for _, s := range All {
		for _, ep := range legalEntrypoints[s] {
			if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(ep)), 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", ep, err)
			}
		}
	}
	const header = "| Shape | Entrypoints it may declare | What it means |\n" +
		"|---|---|---|\n"
	path := filepath.Join(root, filepath.FromSlash(DocPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("# App shape\n\n"+header+body), 0o600); err != nil {
		t.Fatalf("write %s: %v", DocPath, err)
	}
}

// docRows renders a full table from the enum itself, so a case that wants ONE
// deliberate disagreement can start from a doc that agrees about everything
// else. Without this every case would report six problems and the assertion
// would pass for the wrong reason.
func docRows() string {
	var b strings.Builder
	for _, s := range All {
		eps := legalEntrypoints[s]
		quoted := make([]string, len(eps))
		for i, ep := range eps {
			quoted[i] = "`" + ep + "`"
		}
		b.WriteString("| `" + string(s) + "` | " + strings.Join(quoted, ", ") + " | what it means |\n")
	}
	return b.String()
}

// TestDocProblemsPassesOnTheRealDoc is the only case that reads the repo's own
// page: the gate's first job is not to cry wolf on a consistent tree.
func TestDocProblemsPassesOnTheRealDoc(t *testing.T) {
	problems, err := DocProblems(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("DocProblems: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("the repository's own docs/shape.md disagrees with the enum: %v", problems)
	}
}

// dropFromRow rewrites ONE shape's row to omit the named entrypoint, leaving
// every other row alone. It matches by shape id and filters the parsed tokens,
// so the case does not depend on the order the enum happens to declare.
func dropFromRow(body string, s Shape, drop string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		cells := tableCells(line)
		if cells == nil || strings.Trim(cells[0], "`") != string(s) {
			continue
		}
		var keep []string
		for tok := range strings.SplitSeq(cells[1], ",") {
			tok = strings.TrimSpace(tok)
			if strings.Trim(tok, "`") == drop {
				continue
			}
			keep = append(keep, tok)
		}
		lines[i] = "| `" + string(s) + "` | " + strings.Join(keep, ", ") + " | what it means |"
	}
	return strings.Join(lines, "\n")
}

// setRow replaces one shape's entrypoint cell verbatim.
func setRow(body string, s Shape, cell string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		cells := tableCells(line)
		if cells == nil || strings.Trim(cells[0], "`") != string(s) {
			continue
		}
		lines[i] = "| `" + string(s) + "` | " + cell + " | what it means |"
	}
	return strings.Join(lines, "\n")
}

// TestDocProblemsCatchesAMissingEntrypoint is the defect that motivated the
// gate: an entrypoint added to the code and not to the table, which makes the
// landing page understate what a shape ships.
func TestDocProblemsCatchesAMissingEntrypoint(t *testing.T) {
	root := t.TempDir()
	writeShapeDoc(t, root, dropFromRow(docRows(), Web, "cmd/product-cli"))

	problems, err := DocProblems(root)
	if err != nil {
		t.Fatalf("DocProblems: %v", err)
	}
	if len(problems) != 1 {
		t.Fatalf("problems = %v, want exactly the web row", problems)
	}
	if !strings.Contains(problems[0], `"web"`) || !strings.Contains(problems[0], "cmd/product-cli") {
		t.Errorf("problem does not name the row and the missing entrypoint: %q", problems[0])
	}
}

// TestDocProblemsCatchesAShapeWithNoRow: a shape the page never mentions is a
// page and a scaffold disagreeing about what exists.
func TestDocProblemsCatchesAShapeWithNoRow(t *testing.T) {
	root := t.TempDir()
	var body strings.Builder
	for _, s := range All {
		if s == GUI {
			continue
		}
		eps := legalEntrypoints[s]
		quoted := make([]string, len(eps))
		for i, ep := range eps {
			quoted[i] = "`" + ep + "`"
		}
		body.WriteString("| `" + string(s) + "` | " + strings.Join(quoted, ", ") + " | x |\n")
	}
	writeShapeDoc(t, root, body.String())

	problems, err := DocProblems(root)
	if err != nil {
		t.Fatalf("DocProblems: %v", err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], `"gui"`) {
		t.Fatalf("problems = %v, want the undocumented gui shape", problems)
	}
}

// TestDocProblemsCatchesARowTheEnumDoesNotHave: the reverse direction. A page
// that documents a shape nobody can scaffold sends a reader to a dead end.
func TestDocProblemsCatchesARowTheEnumDoesNotHave(t *testing.T) {
	root := t.TempDir()
	writeShapeDoc(t, root, docRows()+"| `tablet` | `cmd/web`, `cmd/product-mcp` | invented |\n")

	problems, err := DocProblems(root)
	if err != nil {
		t.Fatalf("DocProblems: %v", err)
	}
	var found bool
	for _, p := range problems {
		if strings.Contains(p, `"tablet"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("problems = %v, want the invented `tablet` row", problems)
	}
}

// TestDocProblemsIgnoresOrder pins the deliberate leniency: the doc groups the
// entrypoints for a reader while the code groups them by kind, and demanding
// the same order would fail on a cosmetic edit.
func TestDocProblemsIgnoresOrder(t *testing.T) {
	root := t.TempDir()
	eps := append([]string(nil), legalEntrypoints[Web]...)
	slices.Reverse(eps)
	quoted := make([]string, len(eps))
	for i, ep := range eps {
		quoted[i] = "`" + ep + "`"
	}
	body := setRow(docRows(), Web, strings.Join(quoted, ", "))
	writeShapeDoc(t, root, body)

	problems, err := DocProblems(root)
	if err != nil {
		t.Fatalf("DocProblems: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("reordering a row must not be drift: %v", problems)
	}
}

// TestDocProblemsRejectsAMissingTable: an empty document must be an error, not
// an empty problem set — otherwise a page rewritten in another form would pass
// silently, which is the failure mode a gate exists to prevent.
func TestDocProblemsRejectsAMissingTable(t *testing.T) {
	root := t.TempDir()
	writeShapeDoc(t, root, "")

	if _, err := DocProblems(root); err == nil {
		t.Fatal("a doc with no shape table was accepted")
	}
}

// TestDocProblemsRejectsAnUnbacktickedCell: the parser only reads backticked
// `cmd/...` tokens. A cell that drops the backticks parses as "not a shape
// row", so the shape looks undocumented — the message must say WHICH shape, so
// the reader is not left guessing whether the table or the code is at fault.
func TestDocProblemsRejectsAnUnbacktickedCell(t *testing.T) {
	root := t.TempDir()
	// Backticks dropped: the cell no longer parses as a shape row.
	body := setRow(docRows(), Web, "cmd/web, cmd/product-mcp, cmd/product-cli")
	writeShapeDoc(t, root, body)

	problems, err := DocProblems(root)
	if err != nil {
		t.Fatalf("DocProblems: %v", err)
	}
	var found bool
	for _, p := range problems {
		if strings.Contains(p, `"web"`) && strings.Contains(p, "no row") {
			found = true
		}
	}
	if !found {
		t.Fatalf("problems = %v, want the web shape reported as undocumented", problems)
	}
}

// TestDocProblemsCatchesADuplicateRow: appending the corrected row and
// forgetting to delete the old is the natural editing accident. The page renders
// every row, so a duplicate is drift whichever one comes second — last-write-wins
// made the verdict depend on line order.
func TestDocProblemsCatchesADuplicateRow(t *testing.T) {
	root := t.TempDir()
	// A stale `web` row, then the correct one: the STALE one loses the map but
	// still renders on the page.
	stale := "| `web` | `cmd/web`, `cmd/product-mcp` | the browser app |\n"
	writeShapeDoc(t, root, docRows()+stale)

	_, err := DocProblems(root)
	if err == nil {
		t.Fatal("a shape named twice was accepted")
	}
	if !strings.Contains(err.Error(), `"web"`) || !strings.Contains(err.Error(), "more than one row") {
		t.Errorf("error does not name the duplicated shape: %v", err)
	}
}

// TestDocProblemsCatchesAMissingDirectory covers the blind spot both sides
// share: the table and `legalEntrypoints` are hand-written, so agreeing with
// each other proves nothing about the tree. A shape that promises an entrypoint
// nobody created must fail here, not later in a build.
func TestDocProblemsCatchesAMissingDirectory(t *testing.T) {
	root := t.TempDir()
	writeShapeDoc(t, root, docRows())
	// writeShapeDoc seeds every entrypoint so the sibling cases isolate their
	// own fault; here the fault IS the absence, so one directory is removed.
	if err := os.RemoveAll(filepath.Join(root, "cmd", "product-cli")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	problems, err := DocProblems(root)
	if err != nil {
		t.Fatalf("DocProblems: %v", err)
	}
	var sawProductCLI bool
	for _, p := range problems {
		if strings.Contains(p, "cmd/product-cli") && strings.Contains(p, "does not exist") {
			sawProductCLI = true
		}
	}
	if !sawProductCLI {
		t.Fatalf("problems = %v, want the missing cmd/product-cli reported", problems)
	}
	// One missing directory is one problem, not one per shape that lists it.
	n := 0
	for _, p := range problems {
		if strings.Contains(p, "cmd/product-cli") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("cmd/product-cli reported %d times, want once", n)
	}
}

// TestDocProblemsNamesTheLikelyCauseWhenNoRowParses: a table whose entrypoint
// column shifted and a table that is gone look identical to the parser. The
// message must name both, or the reader hunts for a missing table that is right
// there.
func TestDocProblemsNamesTheLikelyCauseWhenNoRowParses(t *testing.T) {
	root := t.TempDir()
	// Every row gains a Note column BEFORE the entrypoints.
	var b strings.Builder
	for line := range strings.SplitSeq(docRows(), "\n") {
		cells := tableCells(line)
		if len(cells) < 3 {
			b.WriteString(line + "\n")
			continue
		}
		b.WriteString("| " + cells[0] + " | note | " + cells[1] + " | " + cells[2] + " |\n")
	}
	writeShapeDoc(t, root, b.String())

	_, err := DocProblems(root)
	if err == nil {
		t.Fatal("a table with a shifted entrypoint column was accepted")
	}
	if !strings.Contains(err.Error(), "SECOND column") {
		t.Errorf("error blames the table's absence, not the shifted column: %v", err)
	}
}

// TestDocProblemsCatchesARepeatedToken covers the same-shape duplicate the SET
// comparison already rejects (a length mismatch), but whose message said only
// "lists [...]" — leaving the reader to spot the repeat by eye in a list of six.
func TestDocProblemsCatchesARepeatedToken(t *testing.T) {
	root := t.TempDir()
	writeShapeDoc(t, root, setRow(docRows(), Web,
		"`cmd/web`, `cmd/web`, `cmd/product-mcp`, `cmd/product-cli`"))

	problems, err := DocProblems(root)
	if err != nil {
		t.Fatalf("DocProblems: %v", err)
	}
	if len(problems) != 1 {
		t.Fatalf("problems = %v, want the web row", problems)
	}
	if !strings.Contains(problems[0], "twice") {
		t.Errorf("message does not name the repeated token: %q", problems[0])
	}
}
