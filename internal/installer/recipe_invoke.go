// Installer CLI: `recipe invoke`, the developer client.
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
	"strings"
	"time"
)

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
