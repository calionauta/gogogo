// SCOPE:core — The Client weight table is measured, and every page that quotes
// a weight quotes it from there.
//
// docs/features.md is the one place a size lives. Those numbers were
// hand-maintained once and drifted three ways at the same time (a 28 KB row for
// a 36 KB file, a "shared core" row missing two scripts every page loads, and a
// two-file row measured as one) because prose is the one place the build never
// reached.
//
// So the numbers are generated, not checked against a second hand copy:
//
//	-write  measures each row's files and rewrites the table cells, then fills
//	        the landing spans that quote them (data-weight="<row label>")
//	check   measures again and reports any cell or span that disagrees
//
// Gzip is Go's compress/gzip. It is not byte-identical to GNU `gzip -nc`, which
// is why the table states the tool rather than pretending both agree: one
// implementation owns the number, and the same code writes and checks it.
package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// clientWeightDoc owns the measured table; the landing quotes it.
const (
	clientWeightDoc = "docs/features.md"
	landingDoc      = "site/index.html"
)

// bytesPerKB is the unit the table rounds up to.
const bytesPerKB = 1024

// fileMode is what the generator writes with: repo prose, never executable.
const fileMode = 0o600

// clientWeightRowRE matches one measured row: label | files | N KB | N KB.
// Headers and the separator carry no KB cell and do not match.
var clientWeightRowRE = regexp.MustCompile(
	`^\|\s*([^|]+?)\s*\|\s*([^|]+?)\s*\|\s*(\d+)\s*KB\s*\|\s*(\d+)\s*KB\s*\|\s*$`)

// weightSpanRE matches a generated landing number: an empty <p class="tech">
// tagged with the row whose weight it quotes.
var weightSpanRE = regexp.MustCompile(`(<p class="tech" data-weight="([^"]+)">)([^<]*)(</p>)`)

// assetTokenRE matches the two shapes a row may name: a script, or a directory
// of them (trailing slash). A backticked span that is neither is prose.
var assetTokenRE = regexp.MustCompile(
	`^(?:[A-Za-z0-9_.-]+/)*[A-Za-z0-9_.-]+\.js$|^(?:[A-Za-z0-9_.-]+/)*[A-Za-z0-9_.-]+/$`)

// assetRoots are where a bare script name is looked for, in order.
var assetRoots = []string{"web/resources/static/", "features/*/static/"}

// weightRow is one measured row, as written and as measured.
type weightRow struct {
	label  string
	files  string
	raw    int
	gzip   int
	line   int
	rawKB  int
	gzipKB int
}

// weightRows parses the table and measures every row it can resolve.
func weightRows(root string) ([]weightRow, error) {
	lines, err := fileLines(root, clientWeightDoc)
	if err != nil {
		return nil, err
	}
	var rows []weightRow
	for i, line := range lines {
		m := clientWeightRowRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		raw, gz, names, measureErr := measureRow(root, m[2])
		if measureErr != nil {
			return nil, fmt.Errorf("%s:%d: %w", clientWeightDoc, i+1, measureErr)
		}
		if len(names) == 0 {
			// A row that names no asset must state 0 KB; anything else is a
			// number nothing measured.
			rows = append(rows, weightRow{
				label: m[1], files: m[2], line: i,
				rawKB: mustAtoi(m[3]), gzipKB: mustAtoi(m[4]),
			})
			continue
		}
		rows = append(rows, weightRow{
			label:  m[1],
			files:  m[2],
			raw:    roundUp(raw),
			gzip:   roundUp(gz),
			line:   i,
			rawKB:  mustAtoi(m[3]),
			gzipKB: mustAtoi(m[4]),
		})
	}
	return rows, nil
}

// checkClientWeight reports a table cell or a landing span that no longer
// matches the bytes, and a span that quotes a row the table does not have.
func checkClientWeight(root string) []string {
	rows, err := weightRows(root)
	if err != nil {
		return []string{err.Error()}
	}
	var out []string
	byLabel := make(map[string]weightRow, len(rows))
	for _, row := range rows {
		byLabel[row.label] = row
		if row.rawKB != row.raw || row.gzipKB != row.gzip {
			out = append(out, fmt.Sprintf(
				"%s:%d: client weight is %d KB / %d KB gzip, the table says %d KB / %d KB"+
					" — run `make docs-index` (or `go run ./cmd/check-docs -write`)",
				clientWeightDoc, row.line+1, row.raw, row.gzip, row.rawKB, row.gzipKB))
		}
	}
	spans, err := landingSpans(root)
	if err != nil {
		return append(out, err.Error())
	}
	for _, span := range spans {
		row, ok := byLabel[span.label]
		if !ok {
			out = append(out, fmt.Sprintf(
				"%s: data-weight=%q quotes a row %s does not have",
				landingDoc, span.label, clientWeightDoc))
			continue
		}
		if want := spanText(row); span.text != want {
			out = append(out, fmt.Sprintf(
				"%s: data-weight=%q reads %q, the table measures %q — run `make docs-index`",
				landingDoc, span.label, span.text, want))
		}
	}
	return out
}

// writeClientWeight rewrites every measured table cell and landing span. It is
// the only writer: a number that is not produced here does not belong in prose.
func writeClientWeight(root string) error {
	lines, err := fileLines(root, clientWeightDoc)
	if err != nil {
		return err
	}
	rows, err := weightRows(root)
	if err != nil {
		return err
	}
	byLabel := make(map[string]weightRow, len(rows))
	for _, row := range rows {
		byLabel[row.label] = row
		if row.rawKB == row.raw && row.gzipKB == row.gzip {
			continue
		}
		lines[row.line] = fmt.Sprintf("| %s | %s | %d KB | %d KB |",
			row.label, row.files, row.raw, row.gzip)
	}
	if writeErr := os.WriteFile(filepath.Join(root, filepath.FromSlash(clientWeightDoc)),
		[]byte(strings.Join(lines, "\n")), fileMode); writeErr != nil {
		return writeErr
	}

	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(landingDoc)))
	if err != nil {
		return err
	}
	var missing []string
	out := weightSpanRE.ReplaceAllStringFunc(string(raw), func(span string) string {
		m := weightSpanRE.FindStringSubmatch(span)
		row, ok := byLabel[m[2]]
		if !ok {
			missing = append(missing, m[2])
			return span
		}
		return m[1] + spanText(row) + m[4]
	})
	if len(missing) > 0 {
		return fmt.Errorf("%s: data-weight names rows %s does not have: %s",
			landingDoc, clientWeightDoc, strings.Join(missing, ", "))
	}
	landingPath := filepath.Join(root, filepath.FromSlash(landingDoc))
	// landingDoc is a constant in this package; root is the module root.
	if writeErr := os.WriteFile(landingPath, []byte(out), fileMode); writeErr != nil { //nolint:gosec // G703
		return writeErr
	}
	return nil
}

// weightSpan is one generated number in the landing page.
type weightSpan struct {
	label string
	text  string
}

func landingSpans(root string) ([]weightSpan, error) {
	lines, err := fileLines(root, landingDoc)
	if err != nil {
		return nil, err
	}
	var out []weightSpan
	for _, line := range lines {
		if m := weightSpanRE.FindStringSubmatch(line); m != nil {
			out = append(out, weightSpan{label: m[2], text: m[3]})
		}
	}
	return out, nil
}

// spanText is the copy the landing shows for a measured row.
func spanText(row weightRow) string {
	return fmt.Sprintf("~%d KB raw · ~%d KB gzip", row.raw, row.gzip)
}

// measureRow sums every asset the row's file cell names, returning raw bytes,
// gzipped bytes and the names it resolved (empty when the cell names no asset).
func measureRow(root, cell string) (int64, int64, []string, error) {
	var raw, gz int64
	var names []string
	for _, span := range tickRE.FindAllStringSubmatch(cell, -1) {
		token := strings.TrimSpace(span[1])
		if !assetTokenRE.MatchString(token) {
			continue
		}
		files, err := assetFiles(root, token)
		if err != nil {
			return 0, 0, nil, err
		}
		for _, file := range files {
			data, readErr := os.ReadFile(file)
			if readErr != nil {
				return 0, 0, nil, readErr
			}
			raw += int64(len(data))
			gz += int64(gzipLen(data))
		}
		names = append(names, token)
	}
	return raw, gz, names, nil
}

// assetFiles resolves a token to files on disk: a repo-relative path when the
// token carries one, otherwise the same basename under the asset roots.
func assetFiles(root, token string) ([]string, error) {
	if strings.HasSuffix(token, "/") {
		return dirScripts(root, token)
	}
	if strings.Contains(token, "/") {
		full := filepath.Join(root, filepath.FromSlash(token))
		if _, err := os.Stat(full); err != nil {
			return nil, fmt.Errorf("client weight names %q, which does not exist", token)
		}
		return []string{full}, nil
	}
	for _, pattern := range assetRoots {
		matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern), token))
		if err != nil {
			return nil, err
		}
		if len(matches) > 0 {
			return matches, nil
		}
	}
	return nil, fmt.Errorf("client weight names %q, which no asset directory holds", token)
}

// dirScripts lists every .js under a directory token, recursively: a vendored
// library ships as a module graph, and the page fetches all of it.
func dirScripts(root, token string) ([]string, error) {
	dir := filepath.Join(root, filepath.FromSlash(strings.TrimSuffix(token, "/")))
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() && strings.HasSuffix(p, ".js") {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("client weight names %q, which does not exist", token)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("client weight names %q, which holds no .js", token)
	}
	return out, nil
}

// gzipLen is the wire size of data, with the compressor this package also uses
// when it writes the number.
func gzipLen(data []byte) int {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return len(data)
	}
	if err := w.Close(); err != nil {
		return len(data)
	}
	return buf.Len()
}

func roundUp(bytes int64) int {
	return int((bytes + bytesPerKB - 1) / bytesPerKB)
}

// mustAtoi parses a digit group the row regex already constrained.
func mustAtoi(digits string) int {
	n, err := strconv.Atoi(digits)
	if err != nil {
		return 0
	}
	return n
}
