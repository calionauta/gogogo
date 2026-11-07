// Installer engine: extension discovery for agents.
package installer

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"

	"github.com/calionauta/gogogo/internal/capabilities"
	"github.com/calionauta/gogogo/internal/recipe"
)

const (
	extExportAction    = "export"
	extRecipeKindName  = "recipe"
	extRecipeInstall   = "recipe install"
	extRecipeUpdate    = "recipe update"
	extRecipeStatus    = "recipe status"
	extRecipeUninstall = "recipe uninstall"
)

type extensionDescriptor struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Summary   string   `json:"summary"`
	Origin    string   `json:"origin"`
	Installed bool     `json:"installed"`
	Removed   bool     `json:"removed,omitempty"`
	Register  string   `json:"register,omitempty"`
	DependsOn []string `json:"dependsOn,omitempty"`
	Actions   []string `json:"actions,omitempty"`
}

func extensionDescriptors(dir string) ([]extensionDescriptor, error) {
	receiptIDs, err := recipe.ListReceipts(dir)
	if err != nil {
		return nil, &ExitError{code: 1, msg: "extension list: " + err.Error()}
	}
	receiptSet := map[string]bool{}
	receiptMap := map[string]recipe.Receipt{}
	for _, id := range receiptIDs {
		receiptSet[id] = true
		if r, lerr := recipe.LoadReceipt(dir, id); lerr == nil {
			receiptMap[id] = r
		}
	}
	out := make([]extensionDescriptor, 0, len(capabilities.All)+len(receiptIDs))
	seen := map[string]bool{}
	for _, cap := range capabilities.All {
		seen[cap.ID] = true
		desc := extensionDescriptor{
			ID: cap.ID, Kind: string(cap.Kind), Summary: cap.Summary,
			Origin: "capability", Installed: receiptSet[cap.ID],
			Register: cap.Register, DependsOn: append([]string(nil), cap.DependsOn...),
			Actions: []string{extExportAction, extRecipeInstall, extRecipeStatus, extRecipeUninstall},
		}
		if _, ok := receiptSet[cap.ID]; ok {
			desc.Origin = "capability+recipe"
			if r := receiptMap[cap.ID]; r.Removed != "" {
				desc.Removed = true
			}
		}
		out = append(out, desc)
	}
	for id := range receiptSet {
		if seen[id] {
			continue
		}
		r := receiptMap[id]
		desc := extensionDescriptor{
			ID: id, Kind: extRecipeKindName, Summary: r.Upstream.Repo,
			Origin: "recipe", Installed: true, Register: r.Register,
			Removed: r.Removed != "", Actions: []string{extRecipeInstall, extRecipeUpdate, extRecipeStatus, extRecipeUninstall},
		}
		out = append(out, desc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func runExtension(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return &ExitError{code: 1, msg: "extension needs a subcommand: list | describe"}
	}
	switch args[0] {
	case "list":
		return runExtensionList(args[1:], stdout)
	case "describe", "show":
		return runExtensionShow(args[1:], stdout)
	default:
		return &ExitError{code: 1, msg: fmt.Sprintf("unknown extension subcommand %q (want list|describe)", args[0])}
	}
}

func runExtensionList(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("extension list", flag.ContinueOnError)
	dir := fs.String("dir", ".", "target checkout directory")
	format := fs.String("format", planFormatText, "output format: text|json")
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return &ExitError{code: 1, msg: "extension list takes no positional arguments"}
	}
	if *format != planFormatText && *format != planFormatJSON {
		return &ExitError{code: 1, msg: fmt.Sprintf("unknown --format %q (want text|json)", *format)}
	}
	exts, err := extensionDescriptors(*dir)
	if err != nil {
		return err
	}
	if *format == planFormatJSON {
		return json.NewEncoder(stdout).Encode(exts)
	}
	for _, e := range exts {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", e.ID, e.Kind, e.Origin, e.Summary)
	}
	return nil
}

func runExtensionShow(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("extension describe", flag.ContinueOnError)
	dir := fs.String("dir", ".", "target checkout directory")
	format := fs.String("format", planFormatText, "output format: text|json")
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return &ExitError{code: 1, msg: "extension describe: " + err.Error()}
	}
	ids := fs.Args()
	if len(ids) != 1 {
		return &ExitError{code: 1, msg: "extension describe needs exactly one extension id"}
	}
	if *format != planFormatText && *format != planFormatJSON {
		return &ExitError{code: 1, msg: fmt.Sprintf("unknown --format %q (want text|json)", *format)}
	}
	exts, err := extensionDescriptors(*dir)
	if err != nil {
		return err
	}
	for _, e := range exts {
		if e.ID == ids[0] {
			if *format == planFormatJSON {
				return json.NewEncoder(stdout).Encode(e)
			}
			fmt.Fprintf(stdout, "%s\n  kind: %s\n  origin: %s\n  summary: %s\n", e.ID, e.Kind, e.Origin, e.Summary)
			if e.Register != "" {
				fmt.Fprintf(stdout, "  register: %s\n", e.Register)
			}
			if len(e.DependsOn) > 0 {
				fmt.Fprintf(stdout, "  depends on: %v\n", e.DependsOn)
			}
			fmt.Fprintf(stdout, "  lifecycle: %v\n", e.Actions)
			return nil
		}
	}
	return &ExitError{code: 1, msg: "extension " + ids[0] + " not found"}
}
