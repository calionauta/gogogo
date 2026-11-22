package shape

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DocPath is the page that DECLARES the enum in the table the landing page is
// generated from. It is the second half of every shape definition: a shape is a
// line in `legalEntrypoints` and a row here, and the two drifting apart is a
// real defect — `site/build-landing.mjs` reads that table to render the shape
// cards, so an entrypoint added only to the code produces a page that claims a
// shape ships less than it does.
const DocPath = "docs/shape.md"

// DocProblems reports every disagreement between the enum in this package and
// the table in docs/shape.md, in both directions.
//
// A missing row is an undocumented shape; a missing entrypoint in a row is a
// page and a scaffold that disagree about what ships. Neither is expressible as
// "the doc is stale" — the reader needs to know which cell.
func DocProblems(root string) ([]string, error) {
	rows, err := docEntrypoints(root)
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, s := range All {
		row, ok := rows[string(s)]
		if !ok {
			problems = append(problems,
				fmt.Sprintf("%s: the %q shape has no row (every shape is a row there)", DocPath, s))
			continue
		}
		code, ok := legalEntrypoints[s]
		if !ok {
			problems = append(problems,
				fmt.Sprintf("internal: %q is in All but has no legal entrypoint set", s))
			continue
		}
		// Compared as a SET: the doc groups the entrypoints for a reader
		// (`web` first, the shells after), while legalEntrypoints groups them by
		// what each one is. Order carries no meaning in either, so demanding it
		// match would fail on a cosmetic edit and train people to ignore the gate.
		if !sameEntrypoints(row, code) {
			problems = append(problems,
				fmt.Sprintf("%s: the %q row lists [%s], the enum ships [%s]",
					DocPath, s, strings.Join(sortedCopy(row), ", "), strings.Join(sortedCopy(code), ", ")))
		}
	}
	for id := range rows {
		if _, ok := legalEntrypoints[Shape(id)]; !ok {
			problems = append(problems,
				fmt.Sprintf("%s: the %q row names a shape the enum does not have", DocPath, id))
		}
	}
	return problems, nil
}

// sameEntrypoints compares two entrypoint sets ignoring order.
func sameEntrypoints(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]bool, len(a))
	for _, s := range a {
		seen[s] = true
	}
	for _, s := range b {
		if !seen[s] {
			return false
		}
	}
	return true
}

// sortedCopy is the deterministic rendering the problem message uses, so the
// same disagreement always reads the same way.
func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// docEntrypoints reads the "| Shape | Entrypoints it may declare | …" table.
// The first two cells are the contract; the prose gloss is the third and is not
// compared, so rewording a description is not drift.
func docEntrypoints(root string) (map[string][]string, error) {
	path := filepath.Join(root, filepath.FromSlash(DocPath))
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", DocPath, err)
	}
	defer func() { _ = f.Close() }()

	out := map[string][]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		cells := tableCells(sc.Text())
		if cells == nil {
			continue
		}
		eps, ok := entrypointCell(cells)
		if !ok {
			continue
		}
		out[strings.Trim(cells[0], "`")] = eps
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", DocPath, err)
	}
	if len(out) == 0 {
		// An empty set would make every shape "undocumented", which is a
		// misleading verdict when the real fault is that the table moved.
		return nil, fmt.Errorf("%s: no shape table found (expected a | Shape | Entrypoints | row)", DocPath)
	}
	return out, nil
}

// tableCells splits a markdown table row into trimmed cells, or nil when the
// line is not a row.
func tableCells(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") {
		return nil
	}
	parts := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
	if len(parts) < 3 {
		return nil
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// entrypointCell extracts the backticked paths from a row's entrypoint cell.
// It rejects the header and the separator row by content, not by position, so a
// table that grows an intro line still parses.
func entrypointCell(cells []string) ([]string, bool) {
	if cells[0] == "Shape" || strings.HasPrefix(cells[0], "-") {
		return nil, false
	}
	var eps []string
	for tok := range strings.SplitSeq(cells[1], ",") {
		tok = strings.TrimSpace(tok)
		if !strings.HasPrefix(tok, "`cmd/") || !strings.HasSuffix(tok, "`") {
			return nil, false
		}
		eps = append(eps, strings.Trim(tok, "`"))
	}
	return eps, len(eps) > 0
}
