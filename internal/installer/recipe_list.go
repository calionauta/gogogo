// Installer CLI: `recipe list` in its two shapes.
package installer

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
)

func runRecipeList(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("recipe list", flag.ContinueOnError)
	dir := fs.String("dir", ".", "target checkout directory")
	format := fs.String("format", planFormatText, "output format: text|json")
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return &ExitError{code: 1, msg: "recipe list takes no positional arguments"}
	}
	if *format != planFormatText && *format != planFormatJSON {
		return &ExitError{code: 1, msg: fmt.Sprintf("unknown --format %q (want text|json)", *format)}
	}
	exts, err := recipeDescriptors(*dir)
	if err != nil {
		return err
	}
	applyRuntimeStates(*dir, exts)
	if *format == planFormatJSON {
		return json.NewEncoder(stdout).Encode(exts)
	}
	for _, e := range exts {
		line := fmt.Sprintf("%s\t%s\t%s\t%s", e.ID, e.Kind, e.Origin, e.Summary)
		line += listStateColumn(e.State)
		if e.Enabled != nil {
			line += "\t" + map[bool]string{true: recipeEnabledWord, false: recipeDisabledWord}[*e.Enabled]
		}
		if len(e.Invoke) > 0 {
			line += "\tinvoke: " + strings.Join(e.Invoke, ",")
		}
		fmt.Fprintln(stdout, line)
	}
	return nil
}
