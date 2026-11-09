// Installer engine: extension discovery for agents.
package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

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

type recipeDescriptor struct {
	ID           string              `json:"id"`
	Version      string              `json:"version,omitempty"`
	Kind         string              `json:"kind"`
	Summary      string              `json:"summary"`
	Origin       string              `json:"origin"`
	Installed    bool                `json:"installed"`
	Removed      bool                `json:"removed,omitempty"`
	Register     string              `json:"register,omitempty"`
	DependsOn    []string            `json:"dependsOn,omitempty"`
	Dependents   []string            `json:"dependents,omitempty"`
	State        string              `json:"state"`
	Routes       []string            `json:"routes,omitempty"`
	Tables       []string            `json:"tables,omitempty"`
	Jobs         []string            `json:"jobs,omitempty"`
	Slots        []string            `json:"slots,omitempty"`
	Invoke       []string            `json:"invoke,omitempty"`
	InvokeParams map[string][]string `json:"invokeParams,omitempty"`
	Actions      []string            `json:"actions,omitempty"`
}

func descriptorState(installed, removed bool, origin string) string {
	if removed {
		return "removed"
	}
	if installed {
		return "installed"
	}
	if origin == "capability" {
		return "builtin"
	}
	return "unknown"
}

func applyReceiptSurface(desc *recipeDescriptor, r recipe.Receipt) {
	desc.Version = r.Version
	desc.Tables = append([]string(nil), r.Declaration.Tables...)
	desc.Jobs = append([]string(nil), r.Declaration.Jobs...)
	desc.Slots = append([]string(nil), r.Declaration.Slots...)
	for _, rt := range r.Declaration.Routes {
		desc.Routes = append(desc.Routes, rt.Method+" "+rt.Pattern)
	}
	// The installed variant's verbs win when it declares any; an empty
	// receipt never blanks the registry mirror of the in-tree unit.
	if len(r.Declaration.Actions) > 0 {
		desc.Invoke = desc.Invoke[:0]
		desc.InvokeParams = map[string][]string{}
		for _, a := range r.Declaration.Actions {
			desc.Invoke = append(desc.Invoke, a.Verb)
			if len(a.Params) > 0 {
				desc.InvokeParams[a.Verb] = append([]string(nil), a.Params...)
			}
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
			Origin: "capability", Installed: receiptSet[cap.ID],
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
		desc.State = descriptorState(desc.Installed, desc.Removed, desc.Origin)
		out = append(out, desc)
	}
	for id := range receiptSet {
		if seen[id] {
			continue
		}
		r := receiptMap[id]
		desc := recipeDescriptor{
			ID: id, Kind: extRecipeKindName, Summary: r.Upstream.Repo,
			Origin: "recipe", Installed: true, Register: r.Register,
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
	if *format == planFormatJSON {
		return json.NewEncoder(stdout).Encode(exts)
	}
	for _, e := range exts {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", e.ID, e.Kind, e.Origin, e.Summary)
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

// printDescribe renders one descriptor as text. Extracted so
// runRecipeDescribe stays under the complexity gate and the nestif gate.
func printDescribe(stdout io.Writer, e recipeDescriptor) {
	fmt.Fprintf(stdout, "%s\n  kind: %s\n  origin: %s\n  state: %s\n",
		e.ID, e.Kind, e.Origin, e.State)
	fmt.Fprintf(stdout, "  summary: %s\n", e.Summary)
	if e.Version != "" {
		fmt.Fprintf(stdout, "  version: %s\n", e.Version)
	}
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
	fmt.Fprintf(stdout, "  lifecycle: %v\n", e.Actions)
}

type invokeParams []string

func (p *invokeParams) String() string { return strings.Join(*p, ",") }

func (p *invokeParams) Set(v string) error {
	*p = append(*p, v)
	return nil
}

// runRecipeInvoke runs one declared verb against the operator-supplied
// session on a running server: `recipe invoke <id> <verb> --server
// <loopback-url> --session <token> --param k=v ...`. The server URL is
// operator config (flag or env), restricted to loopback for the headless
// client — a remote URL is a different trust decision and a future phase.
// The id is validated against the checkout's capabilities + receipts
// (unknown id fails before any dial); the verb is validated against the
// receipt's Actions when those are visible, otherwise the server's
// registry is the source of truth and decides.
func runRecipeInvoke(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("recipe invoke", flag.ContinueOnError)
	dir := fs.String("dir", ".", "target checkout directory")
	server := fs.String("server", strEnv("GOGOGO_SERVER"), "running server base URL (loopback only)")
	session := fs.String("session", strEnv("GOGOGO_SESSION"), "operator session token (or GOGOGO_SESSION)")
	var params invokeParams
	fs.Var(&params, "param", "form field k=v (repeatable)")
	format := fs.String("format", planFormatText, "output format: text|json")
	fs.SetOutput(io.Discard)
	// Leading positionals are id + verb; ones after the flags are accepted
	// too. Matches the documented shape (`invoke <id> <verb> --server ...`),
	// which the flag parser cannot handle after the first positional.
	ids := []string{}
	rest := args
	for len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		ids = append(ids, rest[0])
		rest = rest[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return &ExitError{code: 1, msg: "recipe invoke: " + err.Error()}
	}
	ids = append(ids, fs.Args()...)
	if len(ids) != 2 {
		return &ExitError{code: 1, msg: "recipe invoke needs exactly a recipe id and a verb"}
	}
	if *session == "" {
		return &ExitError{code: 1, msg: "recipe invoke needs a session (--session or GOGOGO_SESSION)"}
	}
	if _, err := invokeTarget(*dir, ids[0], ids[1]); err != nil {
		return err
	}
	endpoint, err := invokeURL(*server)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"id":     ids[0],
		"verb":   ids[1],
		"params": stringMap(params),
	})
	if err != nil {
		return &ExitError{code: 1, msg: "recipe invoke: " + err.Error()}
	}
	return invokeCall(ctx, endpoint, *session, payload, *format, stdout)
}

// invokeTarget resolves the checkout-side descriptor and validates the
// verb the same way the run measures: receipt-visible actions are
// checked locally; server-side registries (built-ins) are authoritative
// and checked by the server.
func invokeTarget(dir, id, verb string) (*recipeDescriptor, error) {
	exts, err := recipeDescriptors(dir)
	if err != nil {
		return nil, err
	}
	for i := range exts {
		if exts[i].ID != id {
			continue
		}
		target := &exts[i]
		if len(target.Invoke) > 0 && !slices.Contains(target.Invoke, verb) {
			return nil, &ExitError{code: 1, msg: "recipe " + id + " declares no invocable action " + verb}
		}
		return target, nil
	}
	return nil, &ExitError{code: 1, msg: "recipe " + id + " not found"}
}

// invokeCall sends the payload to the validated loopback endpoint and
// renders the answer. Extracted from runRecipeInvoke for the
// complexity gate.
func invokeCall(
	ctx context.Context, endpoint, session string, payload []byte, format string, stdout io.Writer,
) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return &ExitError{code: 1, msg: "recipe invoke: " + err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:     "gogogo_auth",
		Value:    session,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return &ExitError{code: 1, msg: "recipe invoke: " + err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &ExitError{code: 1, msg: "recipe invoke: " + err.Error()}
	}
	if resp.StatusCode >= http.StatusMultipleChoices {
		return &ExitError{code: 1, msg: "recipe invoke: server answered " +
			resp.Status + ": " + strings.TrimSpace(string(body))}
	}
	if format == planFormatJSON {
		return json.NewEncoder(stdout).Encode(map[string]any{"status": resp.StatusCode, "body": string(body)})
	}
	_, err = fmt.Fprintln(stdout, string(body))
	return err
}

func strEnv(key string) string { return os.Getenv(key) }

// invokeURL validates the operator's server URL for the headless client:
// http/https only, loopback host only. Anything else is refused before
// any dial — the SSRF guard for v1.
func invokeURL(raw string) (string, error) {
	if raw == "" {
		return "", &ExitError{code: 1, msg: "recipe invoke needs --server (or GOGOGO_SERVER)"}
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", &ExitError{code: 1, msg: "recipe invoke: --server must be http/https"}
	}
	host := u.Hostname()
	if host != "localhost" && net.ParseIP(host) == nil {
		return "", &ExitError{code: 1, msg: "recipe invoke: --server must be loopback (v1)"}
	}
	if ip := net.ParseIP(host); ip != nil && !ip.IsLoopback() {
		return "", &ExitError{code: 1, msg: "recipe invoke: --server must be loopback (v1)"}
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/api/ext/invoke"
	return u.String(), nil
}

func stringMap(p invokeParams) map[string]string {
	out := map[string]string{}
	for _, kv := range p {
		k, v, _ := strings.Cut(kv, "=")
		out[k] = v
	}
	return out
}
