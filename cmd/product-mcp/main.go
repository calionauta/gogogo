// Command product-mcp is the CONSUMER MCP surface of a gogogo product.
//
// This is the half a user of the product runs: it answers "what can this
// product do" and "do it" over MCP stdio. It links internal/productapi and
// nothing else — no installer, no capabilities registry, no shape tooling. A
// product ships this with its own binary, so an agent driving the product never
// needs the evolution toolchain installed.
//
// The template's `cmd/gogogo-mcp` is the mirror image: it evolves a checkout.
// Same transport, opposite role. Keeping them separate is what makes each
// installable on its own.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/calionauta/gogogo/internal/productapi"
)

func main() {
	server := flag.String("server", firstNonEmpty(os.Getenv("GOGOGO_SERVER"), "http://127.0.0.1:8080"),
		"base URL of the running product (loopback only)")
	session := flag.String("session", os.Getenv("GOGOGO_SESSION"),
		"product session token (or GOGOGO_SESSION); never a password")
	flag.Parse()

	client := productapi.Client{BaseURL: *server, Session: *session}
	srv := newServer(client)
	if err := srv.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintf(os.Stderr, "product-mcp: %v\n", err)
		os.Exit(1)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// newServer wires the two tools that make a product self-describing and
// callable. Extraction exists so the tests drive the same wiring main does:
// main_test.go connects a real client to this server over an in-memory
// transport, so a tool registered here but undispatchable fails in CI rather
// than in an agent's first call.
func newServer(client productapi.Client) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "product",
		Version: version,
	}, &mcp.ServerOptions{
		Instructions: "Call product_describe first: it lists what this product " +
			"offers and where each param travels. Then product_invoke. The verbs " +
			"are the same routes the UI uses — not a wrapper.",
	})
	mcp.AddTool(srv, &mcp.Tool{
		Name: "product_describe",
		Description: "List what this product offers: its units and, per unit, the " +
			"verbs it answers with their HTTP method, whether each param travels in " +
			"the body or the URL query, and the page a human uses for the same " +
			"action. Read this before product_invoke.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, describeOut, error) {
		units, err := client.Descriptors(ctx)
		if err != nil {
			return nil, describeOut{}, err
		}
		return nil, describeOut{Units: summarise(units)}, nil
	})
	mcp.AddTool(srv, &mcp.Tool{
		Name: "product_invoke",
		Description: "Call one verb this product declares. Discovery from " +
			"product_describe is the source of truth: an unknown verb fails with " +
			"the list of what exists. Authorisation is the same as the UI's.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in invokeIn) (*mcp.CallToolResult, invokeOut, error) {
		body, err := client.Invoke(ctx, in.Unit, in.Verb, in.Params)
		if err != nil {
			return nil, invokeOut{}, err
		}
		return nil, invokeOut{Unit: in.Unit, Verb: in.Verb, Body: body}, nil
	})
	return srv
}

type noArgs struct{}

type describeOut struct {
	Units []unitSummary `json:"units"`
}

type unitSummary struct {
	ID    string        `json:"id"`
	Roles []string      `json:"roles,omitempty"`
	Verbs []verbSummary `json:"verbs"`
}

type verbSummary struct {
	Verb    string   `json:"verb"`
	Method  string   `json:"method"`
	Summary string   `json:"summary,omitempty"`
	Body    []string `json:"body,omitempty"`
	Query   []string `json:"query,omitempty"`
	RawBody string   `json:"rawBody,omitempty"`
	Page    string   `json:"page,omitempty"`
}

type invokeIn struct {
	Unit   string            `json:"unit"`
	Verb   string            `json:"verb"`
	Params map[string]string `json:"params,omitempty"`
}

type invokeOut struct {
	Unit string `json:"unit"`
	Verb string `json:"verb"`
	Body string `json:"body"`
}

// summarise projects the wire shape onto what a model needs to choose a verb.
// The route pattern is dropped: it describes the server's tree, not the call.
func summarise(units []productapi.Unit) []unitSummary {
	out := make([]unitSummary, 0, len(units))
	for _, u := range units {
		s := unitSummary{ID: u.ID, Roles: u.Roles, Verbs: make([]verbSummary, 0, len(u.Verbs))}
		for _, v := range u.Verbs {
			s.Verbs = append(s.Verbs, verbSummary{
				Verb:    v.Verb,
				Method:  v.Method,
				Summary: v.Summary,
				Body:    v.Params,
				Query:   v.Query,
				RawBody: v.RawBody,
				Page:    v.Page,
			})
		}
		sort.Slice(s.Verbs, func(i, j int) bool { return s.Verbs[i].Verb < s.Verbs[j].Verb })
		out = append(out, s)
	}
	return out
}
