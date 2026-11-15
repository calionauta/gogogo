// Installer engine: extension discovery for agents.
package installer

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"maps"
	"sort"
	"strings"

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

	// originCapability and originRecipe are the two provenances a descriptor
	// can have. Named here because descriptorState compares them.
	originCapability = "capability"
	originRecipe     = "recipe"
)

type recipeDescriptor struct {
	ID       string        `json:"id"`
	Version  string        `json:"version,omitempty"`
	Author   recipe.Author `json:"author,omitzero"`
	Released string        `json:"released,omitempty"`
	BasedOn  []string      `json:"basedOn,omitempty"`
	// Components are the recipe's own UI files, so an agent looking for a
	// reusable shape can find one without walking the tree.
	Components []string `json:"components,omitempty"`
	// Enabled is the durable runtime switch. Nil means no recipe_state row
	// (the recipe is enabled by default).
	Enabled          *bool               `json:"enabled,omitempty"`
	Kind             string              `json:"kind"`
	Summary          string              `json:"summary"`
	Origin           string              `json:"origin"`
	Installed        bool                `json:"installed"`
	Removed          bool                `json:"removed,omitempty"`
	Register         string              `json:"register,omitempty"`
	DependsOn        []string            `json:"dependsOn,omitempty"`
	Dependents       []string            `json:"dependents,omitempty"`
	State            RecipeState         `json:"state"`
	Routes           []string            `json:"routes,omitempty"`
	Tables           []string            `json:"tables,omitempty"`
	Jobs             []string            `json:"jobs,omitempty"`
	Slots            []string            `json:"slots,omitempty"`
	Invoke           []string            `json:"invoke,omitempty"`
	InvokeParams     map[string][]string `json:"invokeParams,omitempty"`
	InvokePathParams map[string][]string `json:"invokePathParams,omitempty"`
	// Omitted names the mutating routes the author kept out of the headless
	// surface, with the reason. Discovery shows the decision instead of
	// implying the recipe forgot them.
	Omitted []string `json:"omitted,omitempty"`
	// InvokeDescriptions and InvokeParamHelp are the discovery half that
	// makes a verb choosable without reading the handler. They ride the
	// receipt/manifest and are additive so older consumers keep working.
	InvokeDescriptions map[string]string            `json:"invokeDescriptions,omitempty"`
	InvokeParamHelp    map[string]map[string]string `json:"invokeParamHelp,omitempty"`
	Actions            []string                     `json:"actions,omitempty"`
	// Metrics is the instrumentation half of the declaration, one list.
	// Discovery shows it because an agent asked to measure a recipe must find
	// what is already counted before declaring a second definition — and
	// because a job sentence is the operator's decision, not the agent's.
	Metrics []metricDescriptor `json:"metrics,omitempty"`
}

// metricDescriptor is one declared reading. Kind is empty for activity (the
// pre-kind spelling) and "outcome" for a ratio; the outcome fields are only
// present on that kind. One struct, one list: a caller never has to merge two
// arrays to learn what a recipe measures.
type metricDescriptor struct {
	Kind           string   `json:"kind,omitempty"`
	Label          string   `json:"label,omitempty"`
	Query          string   `json:"query,omitempty"`
	ID             string   `json:"id,omitempty"`
	Job            string   `json:"job,omitempty"`
	Beneficiary    string   `json:"beneficiary,omitempty"`
	Window         string   `json:"window,omitempty"`
	CounterMetrics []string `json:"counterMetrics,omitempty"`
	SuccessLabel   string   `json:"successLabel,omitempty"`
	TotalLabel     string   `json:"totalLabel,omitempty"`
}

// RecipeState is where a recipe sits in this tree. It is the JSON `state`
// field, so the spellings are a wire contract: constants, never literals at
// the comparison site.
//
//	A recipe receipt says installed or removed; a bundled unit that never
//	needs a receipt says builtin; anything else is unknown (never a guess).
type RecipeState string

const (
	StateInstalledRecipe RecipeState = "installed"
	StateRemovedRecipe   RecipeState = "removed"
	StateBuiltinRecipe   RecipeState = "builtin"
	StateUnknownRecipe   RecipeState = "unknown"
)

// descriptorState derives the state from the three facts that decide it.
func descriptorState(installed, removed bool, origin string) RecipeState {
	switch {
	case removed:
		return StateRemovedRecipe
	case installed:
		return StateInstalledRecipe
	case origin == originCapability:
		return StateBuiltinRecipe
	default:
		return StateUnknownRecipe
	}
}

func applyReceiptSurface(desc *recipeDescriptor, r recipe.Receipt) {
	desc.Version = r.Version
	desc.Author = r.Author
	desc.Released = r.Released
	desc.BasedOn = append([]string(nil), r.BasedOn...)
	desc.Components = recipeComponents(r)
	desc.Tables = append([]string(nil), r.Declaration.Tables...)
	desc.Jobs = append([]string(nil), r.Declaration.Jobs...)
	desc.Slots = append([]string(nil), r.Declaration.Slots...)
	for _, rt := range r.Declaration.Routes {
		desc.Routes = append(desc.Routes, rt.Method+" "+rt.Pattern)
	}
	// The installed variant's verbs win when it declares any; an empty
	// receipt never blanks the registry mirror of the in-tree unit.
	if len(r.Declaration.Actions) > 0 {
		applyReceiptActions(desc, r.Declaration.Actions)
	}
	desc.Omitted = desc.Omitted[:0]
	for _, o := range r.Declaration.Omitted {
		desc.Omitted = append(desc.Omitted, o.Route.Method+" "+o.Route.Pattern+" — "+o.Reason)
	}
	applyReceiptInstrumentation(desc, r.Declaration)
}

// applyReceiptInstrumentation copies the declared metrics onto the descriptor
// in one list. Kept beside applyReceiptSurface so the discovery surface stays
// one flat list of assignments, and it COPIES: a descriptor must not alias the
// receipt the caller still holds.
func applyReceiptInstrumentation(desc *recipeDescriptor, d recipe.Declaration) {
	desc.Metrics = desc.Metrics[:0]
	for _, m := range d.Metrics {
		item := metricDescriptor{
			Kind:  string(m.ResolvedKind()),
			Label: m.Label,
			Query: m.Query,
		}
		if m.ResolvedKind() == recipe.MetricOutcome {
			if m.Success == nil || m.Total == nil {
				continue
			}
			item.ID = m.ID
			item.Job = m.Job
			item.Beneficiary = m.Beneficiary
			item.Window = m.Window
			item.SuccessLabel = m.Success.Label
			item.TotalLabel = m.Total.Label
			for _, c := range m.CounterMetrics {
				item.CounterMetrics = append(item.CounterMetrics, c.Label)
			}
		}
		desc.Metrics = append(desc.Metrics, item)
	}
}

// recipeComponents lists the recipe's UI files under a components/ directory.
// The receipt already has the path set, so discovery never walks the tree: one
// source, the same hashes the install recorded.
func recipeComponents(r recipe.Receipt) []string {
	var out []string
	for path := range r.Files {
		if strings.Contains(path, "/components/") {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

// applyReceiptActions replaces the descriptor's verbs with the receipt's
// declaration. Kept separate from applyReceiptSurface so the discovered
// surface stays one flat list of assignments instead of a nested condition.
func applyReceiptActions(desc *recipeDescriptor, actions []recipe.InvokeAction) {
	desc.Invoke = desc.Invoke[:0]
	desc.InvokeParams = map[string][]string{}
	desc.InvokePathParams = map[string][]string{}
	desc.InvokeDescriptions = map[string]string{}
	desc.InvokeParamHelp = map[string]map[string]string{}
	for _, a := range actions {
		desc.Invoke = append(desc.Invoke, a.Verb)
		if len(a.Params) > 0 {
			desc.InvokeParams[a.Verb] = append([]string(nil), a.Params...)
		}
		if len(a.PathParams) > 0 {
			desc.InvokePathParams[a.Verb] = append([]string(nil), a.PathParams...)
		}
		if a.Description != "" {
			desc.InvokeDescriptions[a.Verb] = a.Description
		}
		if len(a.ParamHelp) > 0 {
			help := make(map[string]string, len(a.ParamHelp))
			maps.Copy(help, a.ParamHelp)
			desc.InvokeParamHelp[a.Verb] = help
		}
	}
}

func recipeDescriptors(dir string) ([]recipeDescriptor, error) {
	receiptIDs, err := recipe.ListReceipts(dir)
	if err != nil {
		return nil, &ExitError{code: 1, msg: "recipe list: " + err.Error()}
	}
	receiptSet := map[string]bool{}
	receiptMap := map[string]recipe.Receipt{}
	for _, id := range receiptIDs {
		receiptSet[id] = true
		if r, lerr := recipe.LoadReceipt(dir, id); lerr == nil {
			receiptMap[id] = r
		}
	}
	out := make([]recipeDescriptor, 0, len(capabilities.All)+len(receiptIDs))
	seen := map[string]bool{}
	dependents := map[string][]string{}
	for _, cap := range capabilities.All {
		for _, dep := range cap.DependsOn {
			dependents[dep] = append(dependents[dep], cap.ID)
		}
	}
	for _, cap := range capabilities.All {
		seen[cap.ID] = true
		desc := recipeDescriptor{
			ID: cap.ID, Kind: string(cap.Kind), Summary: cap.Summary,
			Origin: originCapability, Installed: receiptSet[cap.ID],
			Register: cap.Register, DependsOn: append([]string(nil), cap.DependsOn...),
			Dependents: append([]string(nil), dependents[cap.ID]...),
			Invoke:     append([]string(nil), cap.Actions...),
			Actions:    []string{extExportAction, extRecipeInstall, extRecipeStatus, extRecipeUninstall},
		}
		if _, ok := receiptSet[cap.ID]; ok {
			desc.Origin = "capability+recipe"
			r := receiptMap[cap.ID]
			if r.Removed != "" {
				desc.Removed = true
			}
			applyReceiptSurface(&desc, r)
		}
		// Built-in units publish params through the registry mirror;
		// receipt installations publish theirs through the receipt.
		desc.InvokeParams = map[string][]string{}
		for verb, ps := range cap.ActionParams {
			desc.InvokeParams[verb] = append([]string(nil), ps...)
		}
		desc.InvokePathParams = map[string][]string{}
		for verb, ps := range cap.ActionPathParams {
			desc.InvokePathParams[verb] = append([]string(nil), ps...)
		}
		desc.State = descriptorState(desc.Installed, desc.Removed, desc.Origin)
		out = append(out, desc)
	}
	for id := range receiptSet {
		if seen[id] {
			continue
		}
		r := receiptMap[id]
		summary := r.Summary
		if summary == "" {
			// Older receipts carry no summary; the repo is the honest
			// fallback, but `recipe check` asks the author to replace it.
			summary = r.Upstream.Repo
		}
		desc := recipeDescriptor{
			ID: id, Kind: extRecipeKindName, Summary: summary,
			Origin: originRecipe, Installed: true, Register: r.Register,
			Removed: r.Removed != "", Actions: []string{extRecipeInstall, extRecipeUpdate, extRecipeStatus, extRecipeUninstall},
		}
		applyReceiptSurface(&desc, r)
		desc.State = descriptorState(desc.Installed, desc.Removed, desc.Origin)
		out = append(out, desc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

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

func runRecipeDescribe(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("recipe describe", flag.ContinueOnError)
	dir := fs.String("dir", ".", "target checkout directory")
	format := fs.String("format", planFormatText, "output format: text|json")
	fs.SetOutput(io.Discard)
	// A leading positional is the id; one after the flags is accepted too,
	// but two ids is an error either way (the documented shape is
	// `describe <id> --dir <proj>`, which the flag parser cannot handle
	// after the first positional).
	ids := []string{}
	rest := args
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		ids = append(ids, rest[0])
		rest = rest[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return &ExitError{code: 1, msg: "recipe describe: " + err.Error()}
	}
	ids = append(ids, fs.Args()...)
	if len(ids) != 1 {
		return &ExitError{code: 1, msg: "recipe describe needs exactly one recipe id"}
	}
	id := ids[0]
	if *format != planFormatText && *format != planFormatJSON {
		return &ExitError{code: 1, msg: fmt.Sprintf("unknown --format %q (want text|json)", *format)}
	}
	exts, err := recipeDescriptors(*dir)
	if err != nil {
		return err
	}
	applyRuntimeStates(*dir, exts)
	for _, e := range exts {
		if e.ID != id {
			continue
		}
		if *format == planFormatJSON {
			return json.NewEncoder(stdout).Encode(e)
		}
		printDescribe(stdout, e)
		return nil
	}
	return &ExitError{code: 1, msg: "recipe " + id + " not found"}
}

// printInstrumentation renders the declared readings: what the recipe already
// measures, which an agent must read before proposing a second metric.
//
// An outcome metric prints as "success label / total label — job (beneficiary,
// window; counterMetrics: …)" so the pair is legible without a table.
func printInstrumentation(stdout io.Writer, e recipeDescriptor) {
	for _, m := range e.Metrics {
		if m.Kind != string(recipe.MetricOutcome) {
			fmt.Fprintf(stdout, "  metric: %s — %s\n", m.Label, m.Query)
			continue
		}
		line := fmt.Sprintf("  metric: %s — %s / %s — %s (%s",
			m.ID, m.SuccessLabel, m.TotalLabel, m.Job, m.Beneficiary)
		if m.Window != "" {
			line += ", " + m.Window
		}
		if len(m.CounterMetrics) > 0 {
			line += "; counterMetrics: " + strings.Join(m.CounterMetrics, ", ")
		}
		fmt.Fprintf(stdout, "%s)\n", line)
	}
}

// printDescribe renders one descriptor as text. Extracted so
// runRecipeDescribe stays under the complexity gate and the nestif gate.
// printCredit is the provenance half of describe: who wrote the slice and when
// the version shipped. Split out because describe is already at the complexity
// gate, and because discovery's other forms may want the same two lines.
func printCredit(stdout io.Writer, e recipeDescriptor) {
	if e.Author.Name != "" {
		credit := e.Author.Name
		if e.Author.Email != "" {
			credit += " <" + e.Author.Email + ">"
		}
		fmt.Fprintf(stdout, "  author: %s\n", credit)
	}
	if e.Released != "" {
		fmt.Fprintf(stdout, "  released: %s\n", e.Released)
	}
	if len(e.BasedOn) > 0 {
		fmt.Fprintf(stdout, "  based on: %v\n", e.BasedOn)
	}
	if e.Enabled != nil {
		state := recipeEnabledWord
		if !*e.Enabled {
			state = recipeDisabledWord
		}
		fmt.Fprintf(stdout, "  runtime: %s (takes effect on the next boot)\n", state)
	}
	if len(e.Components) > 0 {
		fmt.Fprintf(stdout, "  components: %v\n", e.Components)
	}
}

func printDescribe(stdout io.Writer, e recipeDescriptor) {
	fmt.Fprintf(stdout, "%s\n  kind: %s\n  origin: %s\n  state: %s\n",
		e.ID, e.Kind, e.Origin, e.State)
	fmt.Fprintf(stdout, "  summary: %s\n", e.Summary)
	if e.Version != "" {
		fmt.Fprintf(stdout, "  version: %s\n", e.Version)
	}
	printCredit(stdout, e)
	if e.Register != "" {
		fmt.Fprintf(stdout, "  register: %s\n", e.Register)
	}
	if len(e.DependsOn) > 0 {
		fmt.Fprintf(stdout, "  depends on: %v\n", e.DependsOn)
	}
	if len(e.Dependents) > 0 {
		fmt.Fprintf(stdout, "  dependents: %v\n", e.Dependents)
	}
	if len(e.Routes) > 0 {
		fmt.Fprintf(stdout, "  routes: %v\n", e.Routes)
	}
	if len(e.Tables) > 0 {
		fmt.Fprintf(stdout, "  tables: %v\n", e.Tables)
	}
	if len(e.Jobs) > 0 {
		fmt.Fprintf(stdout, "  jobs: %v\n", e.Jobs)
	}
	if len(e.Slots) > 0 {
		fmt.Fprintf(stdout, "  slots: %v\n", e.Slots)
	}
	if len(e.Invoke) > 0 {
		fmt.Fprintf(stdout, "  invoke: %v\n", e.Invoke)
	}
	if len(e.InvokeParams) > 0 {
		fmt.Fprintf(stdout, "  invokeParams: %v\n", e.InvokeParams)
	}
	printInstrumentation(stdout, e)
	if len(e.InvokePathParams) > 0 {
		fmt.Fprintf(stdout, "  invokePathParams: %v\n", e.InvokePathParams)
	}
	for _, omitted := range e.Omitted {
		fmt.Fprintf(stdout, "  omitted: %s\n", omitted)
	}
	printInvokeHelp(stdout, e)
	fmt.Fprintf(stdout, "  lifecycle: %v\n", e.Actions)
}

// printInvokeHelp renders the agent-facing half of discovery: what each
// declared verb does and what its params mean. A bare verb name forces the
// caller to read the handler, which is exactly what a headless agent cannot do.
func printInvokeHelp(stdout io.Writer, e recipeDescriptor) {
	if len(e.InvokeDescriptions) == 0 && len(e.InvokeParamHelp) == 0 {
		return
	}
	fmt.Fprintln(stdout, "  invoke help:")
	for _, verb := range e.Invoke {
		if desc := e.InvokeDescriptions[verb]; desc != "" {
			fmt.Fprintf(stdout, "    %s — %s\n", verb, desc)
		}
		pathParams := e.InvokePathParams[verb]
		if len(pathParams) > 0 {
			fmt.Fprintf(stdout, "      path: %s (uuid)\n", strings.Join(pathParams, ", "))
		}
		params := e.InvokeParams[verb]
		if len(params) > 0 {
			fmt.Fprintf(stdout, "      params: %s\n", strings.Join(params, ", "))
		}
		for _, p := range append(append([]string(nil), pathParams...), params...) {
			if help := e.InvokeParamHelp[verb][p]; help != "" {
				fmt.Fprintf(stdout, "      %s: %s\n", p, help)
			}
		}
	}
}
