// Installer CLI: `recipe receipts` — the hashes still describe the tree?
//
// WHY A SEPARATE VERB. `recipe check` deliberately treats a modified file as
// the user's (a fork edits its slice; that is the point of owning it), so it
// cannot fail on drift without punishing the workflow recipes exist for. But
// THIS repository is not a fork: every receipt here describes a slice the
// template ships, so a hash that no longer matches means the record rotted —
// and it rotted silently in 82 of 276 files before this gate existed.
//
// That rot is not cosmetic. `recipe install` copies by receipt, `--recipes`
// prunes by receipt, and `recipe test` derives its package scope from the
// receipt: a wrong hash makes a removal leave files behind and a test run
// silently skip them.
package installer

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/calionauta/gogogo/internal/recipe"
)

// receiptDriftRow is one recipe whose declared hashes no longer match the tree.
type receiptDriftRow struct {
	ID    string   `json:"id"`
	Stale []string `json:"stale"`
}

// runRecipeReceipts verifies every receipt against the files it claims. Exit 1
// when any hash differs, naming the file — the fix is `gogogo recipe receipts
// --write`, which re-hashes in place.
func runRecipeReceipts(args []string, stdout io.Writer) error {
	dir, format, write, err := parseReceiptsArgs(args)
	if err != nil {
		return err
	}
	root, aerr := filepath.Abs(dir)
	if aerr != nil {
		return &ExitError{code: 1, msg: "recipe receipts: " + aerr.Error()}
	}
	if !looksLikeTemplate(root) {
		return &ExitError{code: 1, msg: "recipe receipts: " + dir + " is not a gogogo checkout"}
	}
	ids, lerr := recipe.ListReceipts(root)
	if lerr != nil {
		return &ExitError{code: 1, msg: "recipe receipts: " + lerr.Error()}
	}
	sort.Strings(ids)

	var rows []receiptDriftRow
	var fixed []string
	for _, id := range ids {
		r, rerr := recipe.LoadReceipt(root, id)
		if rerr != nil {
			return &ExitError{code: 1, msg: "recipe receipts: " + rerr.Error()}
		}
		if r.Removed != "" {
			continue // a tombstone describes data, not bytes
		}
		stale := staleReceiptFiles(root, r)
		if len(stale) == 0 {
			continue
		}
		if write {
			if werr := rewriteReceiptHashes(root, r, stale); werr != nil {
				return &ExitError{code: 1, msg: "recipe receipts: " + werr.Error()}
			}
			fixed = append(fixed, id)
			continue
		}
		rows = append(rows, receiptDriftRow{ID: id, Stale: stale})
	}
	if write {
		return printReceiptsWritten(stdout, format, fixed)
	}
	return printReceiptDrift(stdout, format, rows)
}

// parseReceiptsArgs reads the flags. `--write` is the only mutating path.
func parseReceiptsArgs(args []string) (dir, format string, write bool, err error) {
	dir, format = ".", planFormatText
	fs := flag.NewFlagSet("recipe receipts", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&dir, "dir", ".", "checkout whose receipts to verify")
	fs.StringVar(&format, "format", planFormatText, "output format: text|json")
	fs.BoolVar(&write, "write", false, "re-hash the stale files in place (the fix)")
	if perr := fs.Parse(args); perr != nil {
		return "", "", false, &ExitError{code: 1, msg: "recipe receipts: " + perr.Error()}
	}
	if fs.NArg() != 0 {
		return "", "", false, &ExitError{code: 1, msg: "recipe receipts takes no positional arguments"}
	}
	if format != planFormatText && format != planFormatJSON {
		return "", "", false, &ExitError{code: 2, msg: `recipe receipts: unknown --format (want text|json)`}
	}
	return dir, format, write, nil
}

// encodeJSONIndent is the one machine shape every recipe verb emits.
func encodeJSONIndent(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// staleReceiptFiles lists the declared paths whose bytes no longer hash to what
// the receipt recorded. A path that is GONE is not stale here: that is a
// removal, which `recipe status` reports and `--write` must not paper over.
func staleReceiptFiles(root string, r recipe.Receipt) []string {
	var stale []string
	for path, want := range r.Files {
		got, ok := hashFileIn(root, path)
		if !ok {
			continue
		}
		if got != want {
			stale = append(stale, path)
		}
	}
	sort.Strings(stale)
	return stale
}

// hashFileIn reads one repo-relative path and hashes it. ok is false when the
// file is absent (a removal, which is not drift) or unreadable.
//
// The path comes from a RECEIPT in the checkout being verified — never from a
// request — and the walk is confined by os.Root below.
func hashFileIn(root, rel string) (string, bool) {
	tree, err := os.OpenRoot(root)
	if err != nil {
		return "", false
	}
	defer func() { _ = tree.Close() }()
	raw, err := tree.ReadFile(filepath.FromSlash(rel))
	if err != nil {
		return "", false
	}
	return recipe.Hash(raw), true
}

// receiptHashLine matches `  path: <64 hex>` inside a receipt's files: block.
// The digest may be YAML-quoted: `recipe.SaveReceipt` writes it bare, but a
// hand-written or older receipt often quotes it, and a matcher that only knew
// one spelling silently skipped the other.
var receiptHashLine = regexp.MustCompile(`^(\s+)(\S.*): "?([0-9a-f]{64})"?$`)

// rewriteReceiptHashes updates the stale lines in place, leaving every other
// byte of the receipt alone: it is a tracked, hand-reviewed file.
func rewriteReceiptHashes(root string, r recipe.Receipt, stale []string) error {
	path := filepath.Join(root, filepath.FromSlash(recipe.ReceiptPath(r.ID)))
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	want := make(map[string]bool, len(stale))
	for _, p := range stale {
		want[p] = true
	}
	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		m := receiptHashLine.FindStringSubmatch(line)
		if m == nil || !want[m[2]] {
			continue
		}
		got, ok := hashFileIn(root, m[2])
		if !ok {
			continue
		}
		// Keep whatever quote style the line already used.
		quote := ""
		if strings.Contains(line, `: "`) {
			quote = `"`
		}
		lines[i] = m[1] + m[2] + `: ` + quote + got + quote
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), sourceFileMode) //nolint:gosec // tracked receipt
}

// printReceiptDrift renders the verdict. Exit 1 when anything is stale.
func printReceiptDrift(w io.Writer, format string, rows []receiptDriftRow) error {
	if format == planFormatJSON {
		if err := encodeJSONIndent(w, rows); err != nil {
			return err
		}
	} else {
		for _, row := range rows {
			fmt.Fprintf(w, "recipe %s: %d stale hash(es)\n", row.ID, len(row.Stale))
			for _, path := range row.Stale {
				fmt.Fprintf(w, "  %s\n", path)
			}
		}
	}
	if len(rows) == 0 {
		if format == planFormatText {
			fmt.Fprintln(w, "recipe receipts: every declared hash matches the tree")
		}
		return nil
	}
	total := 0
	for _, row := range rows {
		total += len(row.Stale)
	}
	return &ExitError{code: 1, msg: fmt.Sprintf(
		"recipe receipts: %d stale hash(es) in %d recipe(s); re-hash with "+
			"`gogogo recipe receipts --write`", total, len(rows))}
}

// printReceiptsWritten reports the fix, never silently.
func printReceiptsWritten(w io.Writer, format string, fixed []string) error {
	if format == planFormatJSON {
		return encodeJSONIndent(w, fixed)
	}
	if len(fixed) == 0 {
		fmt.Fprintln(w, "recipe receipts: nothing to re-hash")
		return nil
	}
	fmt.Fprintf(w, "recipe receipts: re-hashed %s\n", strings.Join(fixed, ", "))
	return nil
}
