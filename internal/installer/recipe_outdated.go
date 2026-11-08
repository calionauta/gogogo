// Installer CLI: `recipe outdated`.
//
// Answers the one question `recipe status` cannot: is the installed version of
// each recipe behind the latest version the catalog has reviewed? The catalog
// is a local directory (catalog/<id>/<version>.yaml, the same layout an
// entry is committed in) — never a phone-home, never a background check.
// Absence is honest, not an error: no catalog (or no entry for a recipe, or
// a receipt recorded without a version) reports unknown, because guessing an
// order from a branch name is worse than saying so.
//
// Exit 3 when any recipe is behind — the same verdict code `--check-update`
// uses, for the same reason: a read-only advisory must not fail a pipeline,
// and 0 has to keep meaning "nothing to do".
package installer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/calionauta/gogogo/internal/recipe"
)

// recipeVersionRow is one recipe's version verdict: what is installed,
// what the catalog latest is, how they compare, and what core the latest
// needs. Latest is empty when the catalog has nothing to compare against.
type recipeVersionRow struct {
	ID        string `json:"id"`
	Installed string `json:"installed,omitempty"`
	Latest    string `json:"latest,omitempty"`
	Status    string `json:"status"`
	MinCore   string `json:"minCore,omitempty"`
}

// outdatedReport is the machine shape of the whole answer.
type outdatedReport struct {
	Checked  int                `json:"checked"`
	Outdated int                `json:"outdated"`
	Catalog  string             `json:"catalog"`
	Results  []recipeVersionRow `json:"results"`
}

// runRecipeOutdated compares every installed recipe (or one, by id) with the
// catalog. Read-only: nothing is written, nothing is resolved at run time.
func runRecipeOutdated(args []string, stdout io.Writer) error {
	opt, err := parseRecipeLookupArgs("outdated", args)
	if err != nil {
		return err
	}
	root, aerr := filepath.Abs(opt.dir)
	if aerr != nil {
		return &ExitError{code: 1, msg: "recipe outdated: " + aerr.Error()}
	}
	if !looksLikeTemplate(root) {
		return &ExitError{code: 1, msg: "recipe outdated: " + opt.dir + " is not a gogogo checkout"}
	}
	ids, ierr := statusIDs(root, opt.id)
	if ierr != nil {
		return outdatedIDsError(ierr)
	}
	if len(ids) == 0 {
		fmt.Fprintln(stdout, "recipe outdated: no receipts in "+recipe.ReceiptDir+
			"/ — nothing is installed yet")
		return nil
	}
	latest, lerr := catalogLatest("outdated", resolveCatalogDir(root, opt.catalog))
	if lerr != nil {
		return lerr
	}
	rows, removed := outdatedRows(root, ids, latest)
	rep := outdatedReport{
		Checked: len(rows), Outdated: countBehind(rows),
		Catalog: resolveCatalogDir(root, opt.catalog), Results: rows,
	}
	if opt.format == planFormatJSON {
		if jerr := printOutdatedJSON(stdout, rep); jerr != nil {
			return jerr
		}
	} else {
		printOutdatedText(stdout, rep, removed)
	}
	if rep.Outdated > 0 {
		return &ExitError{code: 3}
	}
	return nil
}

// outdatedIDsError rewraps a status-ids failure under this command's name.
func outdatedIDsError(err error) error {
	if ee, ok := errors.AsType[*ExitError](err); ok {
		return &ExitError{code: ee.code, msg: "recipe outdated: " + strings.TrimPrefix(ee.msg, "recipe status: ")}
	}
	return err
}

// outdatedRows loads every receipt, skips the removed ones (data ownership,
// not an install), and compares the rest with the catalog.
func outdatedRows(root string, ids []string, latest map[string]recipe.CatalogEntry) ([]recipeVersionRow, []string) {
	rows := []recipeVersionRow{}
	removed := []string{}
	for _, id := range ids {
		r, rerr := recipe.LoadReceipt(root, id)
		if rerr != nil || r.Removed != "" {
			removed = append(removed, id)
			continue
		}
		entry, ok := latest[id]
		rows = append(rows, versionRow(r, entry, ok))
	}
	return rows, removed
}

// versionRow compares one receipt with its catalog entry. A receipt with no
// recorded version, or no catalog entry to compare against, is unknown — the
// one answer that never pretends an order it does not have.
func versionRow(r recipe.Receipt, entry recipe.CatalogEntry, ok bool) recipeVersionRow {
	row := recipeVersionRow{ID: r.ID, Installed: r.Version, Status: statusUnknown}
	if !ok {
		return row
	}
	row.Latest, row.MinCore = entry.Version, entry.MinCore
	row.Status = versionStatus(r.Version, entry.Version)
	return row
}

// countBehind counts rows whose verdict is behind.
func countBehind(rows []recipeVersionRow) int {
	n := 0
	for _, row := range rows {
		if row.Status == statusBehind {
			n++
		}
	}
	return n
}

// resolveCatalogDir is the one catalog convention: `<checkout>/catalog`
// unless `--catalog` names another directory. No env, no config file —
// the layout an entry is committed in is the layout that is scanned.
func resolveCatalogDir(root, catalog string) string {
	if catalog != "" {
		if abs, err := filepath.Abs(catalog); err == nil {
			return abs
		}
		return catalog
	}
	return filepath.Join(root, "catalog")
}

// catalogLatest scans one catalog directory and keeps, per id, the entry
// with the highest version. `command` names the caller in the refusal, so a
// scan failure from `recipe status` never reads as `recipe outdated`.
func catalogLatest(command, catalogDir string) (map[string]recipe.CatalogEntry, error) {
	grouped, err := recipe.ScanCatalog(catalogDir)
	if err != nil {
		return nil, &ExitError{code: 1, msg: "recipe " + command + ": " + err.Error()}
	}
	out := map[string]recipe.CatalogEntry{}
	for id, entries := range grouped {
		if latest, ok := latestEntry(entries); ok {
			out[id] = latest
		}
	}
	return out, nil
}

// latestEntry picks the newest catalog entry under the version ordering the
// update check uses everywhere else. Catalog versions are validated at scan
// time, so an unorderable entry is skipped, never guessed.
func latestEntry(entries []recipe.CatalogEntry) (recipe.CatalogEntry, bool) {
	var best recipe.CatalogEntry
	found := false
	for _, entry := range entries {
		if !found {
			best, found = entry, true
			continue
		}
		if cmp, ok := versionOrder(entry.Version, best.Version); ok && cmp > 0 {
			best = entry
		}
	}
	return best, found
}

// printOutdatedJSON renders the machine shape of the whole answer.
func printOutdatedJSON(w io.Writer, rep outdatedReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}

// printOutdatedText renders one line per recipe. Deliberately short — a
// version check that needs reading is a version check nobody reads.
func printOutdatedText(w io.Writer, rep outdatedReport, removed []string) {
	for _, id := range removed {
		fmt.Fprintf(w, "recipe %s: removed; the receipt stays for data ownership\n", id)
	}
	for _, row := range rep.Results {
		fmt.Fprintf(w, "recipe %s: %s\n", row.ID, rowPhrase(row))
	}
	if rep.Outdated > 0 {
		fmt.Fprintln(w, "gogogo: exit 3 = an update exists (a verdict, not an error)")
	}
}

// rowPhrase is the one verdict phrase every render shares: installed →
// latest, or the honest unknown when one side is missing. Status output
// prefixes it with "version", outdated with "recipe <id>:".
func rowPhrase(row recipeVersionRow) string {
	installed := row.Installed
	if installed == "" {
		installed = "unknown"
	}
	if row.Latest == "" {
		return installed + " (no catalog entry to compare)"
	}
	if row.Installed == "" {
		return installed + " -> " + row.Latest + " " + row.Status + " (installed without a version)"
	}
	out := installed + " -> " + row.Latest + " " + row.Status
	if row.MinCore != "" {
		out += " (needs core " + row.MinCore + ")"
	}
	return out
}
