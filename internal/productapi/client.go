// The product's consumer client: call a running gogogo product headless.
//
// This is the "use the product" half of the two surfaces. It knows NOTHING
// about a checkout — no .recipes/, no receipts, no capability registry. It asks
// the server what it offers (GET /api/ext/descriptors) and calls it
// (POST /api/ext/invoke), which is exactly what any third-party agent would do.
//
// The evolution client (`internal/installer`) is the mirror image: it reads and
// rewrites a tree and never needs a running server to know what a verb means.
// Keeping them apart is what lets a generated product ship this file without
// dragging the installer along.
package productapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// SessionCookie is the cookie the product authenticates a browser (and an
// invoke caller) with. It travels as a value, never as a query parameter.
const SessionCookie = "gogogo_auth"

// Timeouts and caps for one call. Generous enough for a real query, tight
// enough that a hung product does not hang the caller.
const (
	dialTimeout = 30 * time.Second
	bodyLimit   = 1 << 20
	errBodyCap  = 512
)

// Unit is one unit the product offers, as the server describes it.
type Unit struct {
	ID      string   `json:"id"`
	Roles   []string `json:"roles,omitempty"`
	Verbs   []Verb   `json:"verbs"`
	Omitted []struct {
		Route struct {
			Method  string `json:"method"`
			Pattern string `json:"pattern"`
		} `json:"route"`
		Reason string `json:"reason"`
	} `json:"omitted,omitempty"`
}

// Verb is one callable action. Where a param travels is part of the contract:
// Query travels in the URL, Params and RawBody in the form body.
type Verb struct {
	Verb    string            `json:"verb"`
	Method  string            `json:"method"`
	Pattern string            `json:"pattern"`
	Summary string            `json:"summary,omitempty"`
	Params  []string          `json:"params,omitempty"`
	Query   []string          `json:"query,omitempty"`
	RawBody string            `json:"rawBody,omitempty"`
	Page    string            `json:"page,omitempty"`
	Help    map[string]string `json:"help,omitempty"`
}

// Client calls one running product.
type Client struct {
	// BaseURL is the product's origin. The client refuses anything but
	// loopback until a deployment has an identity story (see docs/mcp.md).
	BaseURL string
	// Session is the bearer this client presents — the same value a browser
	// holds in the session cookie.
	Session string
	// HTTP is the transport; nil means a bounded default.
	HTTP *http.Client
}

// LoopbackOnly is the containment rule, exported so a caller sees the reason in
// the error rather than guessing.
// New builds a client for a running product.
func New(baseURL, session string) Client {
	return Client{BaseURL: baseURL, Session: session}
}

// ValidateBaseURL applies the loopback rule: http/https only, loopback host
// only. A product is reached through SSH or Tailscale in v1, never a public
// URL, so a public address is refused before any dial.
func ValidateBaseURL(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("a base URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("base URL %q must be http/https", raw)
	}
	if !loopbackHost(u.Hostname()) {
		return "", fmt.Errorf("base URL %q is not loopback; reach a remote "+
			"product through SSH or Tailscale (v1)", raw)
	}
	return u.String(), nil
}

// loopbackHost is the ONE loopback rule in this package. ValidateBaseURL and
// Client.validate applied it as two hand-written copies with different host
// sets ("" vs Hostname()), so a name one accepted the other refused. A dev
// server is reached as localhost or a loopback IP; anything else goes over SSH
// or Tailscale in v1.
func loopbackHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (c Client) validate() (*url.URL, error) {
	if strings.TrimSpace(c.Session) == "" {
		return nil, errors.New("no session: pass the product's session token " +
			"(the cookie value), never a password")
	}
	raw := strings.TrimSpace(c.BaseURL)
	if raw == "" {
		return nil, errors.New("no server: pass the product's base URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("server URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("server URL must be http or https, got %q", u.Scheme)
	}
	if !loopbackHost(u.Hostname()) {
		return nil, fmt.Errorf("server %q is not loopback: reach a remote product "+
			"through SSH or Tailscale, not this client", u.Hostname())
	}
	return u, nil
}

func (c Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Descriptors asks the product what it offers. One round trip, the whole call
// surface: an agent that caches this does not need the product's docs to know
// which verb exists or where each param travels.
func (c Client) Descriptors(ctx context.Context) ([]Unit, error) {
	base, err := c.validate()
	if err != nil {
		return nil, err
	}
	endpoint := base.JoinPath("/api/ext/descriptors")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.AddCookie(&http.Cookie{
		Name:     SessionCookie,
		Value:    c.Session,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("describe: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("describe: %s", statusText(resp))
	}
	var out []Unit
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode descriptors: %w", err)
	}
	return out, nil
}

// Invoke calls one declared verb. It validates the verb against the product's
// OWN descriptors first, so a typo fails with the list of what exists instead
// of an opaque 400 from the dispatcher.
func (c Client) Invoke(ctx context.Context, id, verb string, params map[string]string) (string, error) {
	base, err := c.validate()
	if err != nil {
		return "", err
	}
	units, err := c.Descriptors(ctx)
	if err != nil {
		return "", err
	}
	action, err := findVerb(units, id, verb)
	if err != nil {
		return "", err
	}
	if err := checkParams(action, params); err != nil {
		return "", err
	}
	return c.send(ctx, base, id, verb, params)
}

// Call sends one verb WITHOUT a discovery round trip. It exists for a caller
// that already validated the verb against the checkout — the evolution client
// reads the receipt, so a second read of the wire is a wasted request and a new
// failure mode with no new information. A consumer has no checkout and uses
// Invoke.
func (c Client) Call(ctx context.Context, id, verb string, params map[string]string) (string, error) {
	base, err := c.validate()
	if err != nil {
		return "", err
	}
	return c.send(ctx, base, id, verb, params)
}

func (c Client) send(
	ctx context.Context, base *url.URL, id, verb string, params map[string]string,
) (string, error) {
	payload, err := json.Marshal(map[string]any{"id": id, "verb": verb, "params": params})
	if err != nil {
		return "", err
	}
	endpoint := base.JoinPath("/api/ext/invoke")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:     SessionCookie,
		Value:    c.Session,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("invoke %s %s: %w", id, verb, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, bodyLimit))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("invoke %s %s: %s: %s", id, verb, resp.Status, strings.TrimSpace(string(body)))
	}
	return string(body), nil
}

// findVerb locates one verb, and on a miss lists what the unit does offer —
// the error an agent can act on.
func findVerb(units []Unit, id, verb string) (Verb, error) {
	for _, u := range units {
		if u.ID != id {
			continue
		}
		available := make([]string, 0, len(u.Verbs))
		for _, v := range u.Verbs {
			if v.Verb == verb {
				return v, nil
			}
			available = append(available, v.Verb)
		}
		if len(available) == 0 {
			return Verb{}, fmt.Errorf("unit %q offers no invocable verb", id)
		}
		slices.Sort(available)
		return Verb{}, fmt.Errorf("unit %q has no verb %q; it offers %s",
			id, verb, strings.Join(available, ", "))
	}
	return Verb{}, fmt.Errorf("unit %q is not offered by this product", id)
}

// checkParams refuses a call that is malformed before it reaches the wire.
// The declaration says where each field travels; sending it elsewhere is the
// difference between a handler that sees the value and one that sees nothing.
func checkParams(action Verb, params map[string]string) error {
	declared := make(map[string]string, len(action.Params)+len(action.Query))
	for _, p := range action.Params {
		declared[p] = "body"
	}
	for _, q := range action.Query {
		declared[q] = "query"
	}
	if len(declared) == 0 {
		return nil
	}
	for name := range params {
		where, ok := declared[name]
		if !ok {
			known := make([]string, 0, len(declared))
			for k := range declared {
				known = append(known, k+" ("+declared[k]+")")
			}
			slices.Sort(known)
			return fmt.Errorf("verb %q has no param %q; it takes %s",
				action.Verb, name, strings.Join(known, ", "))
		}
		if where == "query" {
			return fmt.Errorf("param %q belongs in the URL query of verb %q, "+
				"not the invoke body; this client sends it as a body field",
				name, action.Verb)
		}
	}
	return nil
}

func statusText(resp *http.Response) string {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, errBodyCap))
	text := strings.TrimSpace(string(body))
	if text == "" {
		return resp.Status
	}
	return resp.Status + ": " + text
}
