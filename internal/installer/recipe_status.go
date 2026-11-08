// Installer CLI: `recipe status`.
package installer

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/calionauta/gogogo/internal/recipe"
)

// recipeLookupArgs is the read-only recipe surface shared by `status` and
// `outdated`: one recipe or every installed one, in one checkout, formatted
// a caller way, against an optional catalog.
type recipeLookupArgs struct {
	id      string
	dir     string
	catalog string
	format  string
}

// parseRecipeLookupArgs parses that shared surface; `command` names the
// caller in the messages so a refusal never names the wrong command.
func parseRecipeLookupArgs(command string, args []string) (recipeLookupArgs, error) {
	opt := recipeLookupArgs{}
	fs := flag.NewFlagSet("recipe "+command, flag.ContinueOnError)
	fs.StringVar(&opt.dir, "dir", ".", "checkout to inspect")
	fs.StringVar(&opt.catalog, "catalog", "", "catalog directory (default: <checkout>/catalog)")
	fs.StringVar(&opt.format, "format", planFormatText, "output format: text|json")
	fs.SetOutput(io.Discard)

	rest := args
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		opt.id = rest[0]
		rest = rest[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return opt, &ExitError{code: 1, msg: "recipe " + command + ": " + err.Error()}
	}
	if opt.id == "" && fs.NArg() > 0 {
		opt.id = fs.Arg(0)
	}
	if fs.NArg() > 1 {
		return opt, &ExitError{code: 1, msg: "recipe " + command + " takes at most one recipe id"}
	}
	if opt.format != planFormatText && opt.format != planFormatJSON {
		return opt, &ExitError{code: 1, msg: fmt.Sprintf("unknown --format %q (want text|json)", opt.format)}
	}
	return opt, nil
}

// runRecipeStatus reports the drift of one recipe (or every installed one): what
// is pristine, what the user edited, and whether the declaration still matches
// the copied code.
func runRecipeStatus(args []string, stdout io.Writer) error {
	opt, err := parseRecipeLookupArgs("status", args)
	if err != nil {
		return err
	}
	root, aerr := filepath.Abs(opt.dir)
	if aerr != nil {
		return &ExitError{code: 1, msg: "recipe status: " + aerr.Error()}
	}
	if !looksLikeTemplate(root) {
		return &ExitError{code: 1, msg: "recipe status: " + opt.dir + " is not a gogogo checkout"}
	}
	ids, ierr := statusIDs(root, opt.id)
	if ierr != nil {
		return ierr
	}
	if len(ids) == 0 {
		fmt.Fprintln(stdout, "recipe status: no receipts in "+recipe.ReceiptDir+
			"/ — nothing is installed yet")
		return nil
	}
	problems := 0
	latest, lerr := catalogLatest("status", resolveCatalogDir(root, opt.catalog))
	if lerr != nil {
		return lerr
	}
	for _, id := range ids {
		p, serr := runRecipeStatusOne(root, id, latest, stdout, opt.format)
		if serr != nil {
			return serr
		}
		problems += p
	}
	if problems > 0 {
		return &ExitError{code: 1, msg: fmt.Sprintf(
			"recipe status: %d declaration problem(s) — the receipt and the code disagree", problems)}
	}
	return nil
}

// runRecipeStatusOne reports one recipe: drift plus the version verdict.
// A removed receipt reports its ownership note and counts no problems.
func runRecipeStatusOne(root, id string, latest map[string]recipe.CatalogEntry, stdout io.Writer,
	format string,
) (int, error) {
	r, rerr := recipe.LoadReceipt(root, id)
	if rerr != nil {
		return 0, &ExitError{code: 1, msg: "recipe status: " + rerr.Error()}
	}
	if r.Removed != "" {
		fmt.Fprintf(stdout, "recipe %s: removed %s; the receipt stays for data ownership\n", id, r.Removed)
		return 0, nil
	}
	drift, derr := recipe.Check(root, r)
	if derr != nil {
		return 0, &ExitError{code: 1, msg: "recipe status: " + derr.Error()}
	}
	schema := checkRecipeSchema(root, id, r.Files)
	policy := checkRecipePolicy(root, id, r.Declaration)
	problems := len(drift.Problems) + len(schema) + len(policy)
	entry, ok := latest[id]
	row := versionRow(r, entry, ok)
	if format == planFormatJSON {
		return problems, printDriftJSON(stdout, drift, row, schema, policy)
	}
	var sb strings.Builder
	drift.Render(&sb)
	fmt.Fprintf(&sb, "  version %s\n", rowPhrase(row))
	for _, p := range schema {
		fmt.Fprintf(&sb, "  SCHEMA  %s\n", p)
	}
	for _, p := range policy {
		fmt.Fprintf(&sb, "  POLICY  %s\n", p)
	}
	fmt.Fprint(stdout, sb.String())
	return problems, nil
}

// statusIDs resolves "one recipe" or "every installed recipe" into ids.
func statusIDs(root, id string) ([]string, error) {
	if id != "" {
		if _, err := recipe.LoadReceipt(root, id); err != nil {
			return nil, &ExitError{code: 1, msg: "recipe status: " + err.Error()}
		}
		return []string{id}, nil
	}
	ids, err := recipe.ListReceipts(root)
	if err != nil {
		return nil, &ExitError{code: 1, msg: "recipe status: " + err.Error()}
	}
	sort.Strings(ids)
	return ids, nil
}

// driftReport is the machine shape of one recipe's drift. The schema mirror is
// reported beside the declaration problems rather than inside them: the first
// is a generated view lagging the code, the second is the code contradicting
// what the receipt claims. The version verdict rides alongside both so one
// read answers drift and freshness together.
type driftReport struct {
	recipe.Drift
	Version        string   `json:"version,omitempty"`
	Latest         string   `json:"latest,omitempty"`
	Status         string   `json:"status,omitempty"`
	MinCore        string   `json:"minCore,omitempty"`
	SchemaProblems []string `json:"schemaProblems,omitempty"`
	PolicyProblems []string `json:"policyProblems,omitempty"`
}

// printDriftJSON is the machine shape of one recipe's drift.
func printDriftJSON(w io.Writer, d recipe.Drift, row recipeVersionRow, schema, policy []string) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	rep := driftReport{Drift: d, SchemaProblems: schema, PolicyProblems: policy}
	rep.Version, rep.Latest, rep.Status, rep.MinCore = row.Installed, row.Latest, row.Status, row.MinCore
	return enc.Encode(rep)
}
