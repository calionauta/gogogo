// SCOPE:plugin — installer CLI: `recipe status`.
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

// recipeStatusArgs is the parsed CLI surface for status.
type recipeStatusArgs struct {
	id     string
	dir    string
	format string
}

// runRecipeStatus reports the drift of one recipe (or every installed one): what
// is pristine, what the user edited, and whether the declaration still matches
// the copied code.
func runRecipeStatus(args []string, stdout io.Writer) error {
	opt, err := parseRecipeStatusArgs(args)
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
	for _, id := range ids {
		r, rerr := recipe.LoadReceipt(root, id)
		if rerr != nil {
			return &ExitError{code: 1, msg: "recipe status: " + rerr.Error()}
		}
		if r.Removed != "" {
			fmt.Fprintf(stdout, "recipe %s: removed %s; the receipt stays for data ownership\n", id, r.Removed)
			continue
		}
		drift, derr := recipe.Check(root, r)
		if derr != nil {
			return &ExitError{code: 1, msg: "recipe status: " + derr.Error()}
		}
		problems += len(drift.Problems)
		if opt.format == planFormatJSON {
			if encErr := printDriftJSON(stdout, drift); encErr != nil {
				return encErr
			}
			continue
		}
		var sb strings.Builder
		drift.Render(&sb)
		fmt.Fprint(stdout, sb.String())
	}
	if problems > 0 {
		return &ExitError{code: 1, msg: fmt.Sprintf(
			"recipe status: %d declaration problem(s) — the receipt and the code disagree", problems)}
	}
	return nil
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

// parseRecipeStatusArgs accepts the optional id before or after the flags.
func parseRecipeStatusArgs(args []string) (recipeStatusArgs, error) {
	opt := recipeStatusArgs{}
	fs := flag.NewFlagSet("recipe status", flag.ContinueOnError)
	fs.StringVar(&opt.dir, "dir", ".", "checkout to inspect")
	fs.StringVar(&opt.format, "format", planFormatText, "output format: text|json")
	fs.SetOutput(io.Discard)

	rest := args
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		opt.id = rest[0]
		rest = rest[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return opt, &ExitError{code: 1, msg: "recipe status: " + err.Error()}
	}
	if opt.id == "" && fs.NArg() > 0 {
		opt.id = fs.Arg(0)
	}
	if fs.NArg() > 1 {
		return opt, &ExitError{code: 1, msg: "recipe status takes at most one recipe id"}
	}
	if opt.format != planFormatText && opt.format != planFormatJSON {
		return opt, &ExitError{code: 1, msg: fmt.Sprintf("unknown --format %q (want text|json)", opt.format)}
	}
	return opt, nil
}

// printDriftJSON is the machine shape of one recipe's drift.
func printDriftJSON(w io.Writer, d recipe.Drift) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(d)
}
