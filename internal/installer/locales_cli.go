// Installer CLI: `gogogo locales` — read and write the product's language set.
//
// The operator answers once ("English only, or more?"), the answer is recorded
// in a tracked file, and every later question (scaffold a feature, check a
// recipe, grade a catalog) reads that record instead of guessing a default.
// Env still wins at runtime: a deployment overrides without editing the tree.
package installer

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/calionauta/gogogo/internal/projectlocales"
)

type localesArgs struct {
	dir    string
	set    string
	format string
}

// runLocales is `gogogo locales [--set en,pt-BR]`. No flag reads; `--set`
// writes and prints what was recorded.
func runLocales(args []string, stdout io.Writer) error {
	opt := localesArgs{dir: ".", format: planFormatText}
	fs := flag.NewFlagSet("locales", flag.ContinueOnError)
	fs.StringVar(&opt.dir, "dir", ".", "checkout whose locales.yml to read or write")
	fs.StringVar(&opt.set, "set", "", "comma-separated locale tags to record (e.g. en,pt-BR,es)")
	fs.StringVar(&opt.format, "format", planFormatText, "output format: text|json")
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return &ExitError{code: 1, msg: "locales: " + err.Error()}
	}
	if fs.NArg() != 0 {
		return &ExitError{code: 1, msg: "locales takes no positional arguments"}
	}

	if strings.TrimSpace(opt.set) != "" {
		rec, err := projectlocales.Write(opt.dir, splitIDs(opt.set))
		if err != nil {
			return &ExitError{code: 1, msg: "locales: " + err.Error()}
		}
		return printLocales(stdout, opt.format, rec.Locales, projectlocales.File, true)
	}

	rec, ok, err := projectlocales.Load(opt.dir)
	if err != nil {
		return &ExitError{code: 1, msg: "locales: " + err.Error()}
	}
	if ok {
		return printLocales(stdout, opt.format, rec.Locales, projectlocales.File, false)
	}
	tags, source := projectlocales.Resolve(opt.dir, "")
	return printLocales(stdout, opt.format, tags, source, false)
}

// printLocales renders the set and where it came from: an operator reading
// "en, pt-BR" needs to know whether that is a decision or a default.
func printLocales(w io.Writer, format string, tags []string, source string, written bool) error {
	if format == planFormatJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Locales []string `json:"locales"`
			Source  string   `json:"source"`
			Written bool     `json:"written,omitempty"`
		}{Locales: tags, Source: source, Written: written})
	}
	verb := "locales"
	if written {
		verb = "recorded"
	}
	fmt.Fprintf(w, "gogogo: %s %s (from %s)\n", verb, strings.Join(tags, ", "), source)
	if !written && source != projectlocales.File {
		fmt.Fprintln(w, "  record a decision with: gogogo locales --set "+strings.Join(tags, ","))
	}
	return nil
}
