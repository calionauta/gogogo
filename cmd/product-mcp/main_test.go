package main

// These tests drive the SAME wiring main() builds — that is why newServer is
// extracted. They run the tools through a real mcp.Server against a fake
// product, so a tool that main registers but the server cannot dispatch (a bad
// schema, a handler signature that does not match its input type) fails here
// instead of in an agent's first call.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/calionauta/gogogo/internal/productapi"
)

// fakeProduct serves the two routes product-mcp depends on, reproducing the
// wire shape a real product emits (a bare array, session-gated).
func fakeProduct(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ext/descriptors", func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie("gogogo_auth"); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"todo","verbs":[
			{"verb":"create","method":"POST","pattern":"/todo","summary":"add a row",
			 "params":["title"],"page":"/todo"},
			{"verb":"read","method":"GET","pattern":"/todo"}
		]}]`))
	})
	mux.HandleFunc("/api/ext/invoke", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ID     string            `json:"id"`
			Verb   string            `json:"verb"`
			Params map[string]string `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if in.ID != "todo" || in.Verb != "create" {
			http.Error(w, "unknown verb", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"title":"` + in.Params["title"] + `"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// connect builds the server main builds and returns a client session over an
// in-memory transport — the real MCP dispatch path, no network listener.
func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	product := fakeProduct(t)
	client := productapi.New(product.URL, "test-session")

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ss, err := newServer(client).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("the server main builds does not accept a connection: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "dev"}, nil).
		Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// TestRegistersExactlyTheTwoProductTools pins the surface. A third tool means
// the product MCP grew a capability the docs and the boundary check do not
// describe; a missing one means an agent cannot drive the product at all.
func TestRegistersExactlyTheTwoProductTools(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	for _, want := range []string{"product_describe", "product_invoke"} {
		if !got[want] {
			t.Errorf("%s is not registered — an agent cannot drive the product", want)
		}
	}
	if len(got) != 2 {
		t.Errorf("registered %d tools (%v), want exactly the describe/invoke pair", len(got), got)
	}
}

// TestDescribeReturnsWhatTheProductServes runs product_describe end to end: the
// handler, the client, the wire, and the projection onto unitSummary.
func TestDescribeReturnsWhatTheProductServes(t *testing.T) {
	cs := connect(t)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "product_describe",
	})
	if err != nil {
		t.Fatalf("product_describe: %v", err)
	}
	if res.IsError {
		t.Fatalf("product_describe returned an error result: %+v", res.Content)
	}
	if len(res.Content) == 0 {
		t.Fatal("product_describe returned no content")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, want text", res.Content[0])
	}
	var out describeOut
	if err := json.Unmarshal([]byte(text.Text), &out); err != nil {
		t.Fatalf("describe output is not the documented JSON: %v\n%s", err, text.Text)
	}
	if len(out.Units) != 1 || out.Units[0].ID != "todo" {
		t.Fatalf("units = %+v, want one named todo", out.Units)
	}
	if len(out.Units[0].Verbs) != 2 {
		t.Fatalf("verbs = %+v, want create and read", out.Units[0].Verbs)
	}
	create := out.Units[0].Verbs[0]
	if create.Verb != "create" || create.Method != http.MethodPost {
		t.Errorf("first verb = %+v, want create POST", create)
	}
	if len(create.Body) != 1 || create.Body[0] != "title" {
		t.Errorf("body params = %v, want [title] — the agent would not know where title goes", create.Body)
	}
	if create.Page != "/todo" {
		t.Errorf("page = %q, want /todo — the agent loses the human-facing route", create.Page)
	}
}

// TestInvokeCallsTheVerb drives product_invoke through the same path and
// asserts the product's own answer comes back unmodified.
func TestInvokeCallsTheVerb(t *testing.T) {
	cs := connect(t)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "product_invoke",
		Arguments: map[string]any{
			"unit":   "todo",
			"verb":   "create",
			"params": map[string]string{"title": "buy milk"},
		},
	})
	if err != nil {
		t.Fatalf("product_invoke: %v", err)
	}
	if res.IsError {
		t.Fatalf("product_invoke returned an error result: %+v", res.Content)
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, want text", res.Content[0])
	}
	if !strings.Contains(text.Text, "buy milk") {
		t.Errorf("the product's answer did not survive the round trip: %s", text.Text)
	}
}

// TestInvokeSurfacesTheProductsRefusal proves a rejected call reaches the agent
// as an error rather than a silent success with an empty body. An agent that
// cannot tell refusal from success retries forever.
func TestInvokeSurfacesTheProductsRefusal(t *testing.T) {
	cs := connect(t)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "product_invoke",
		Arguments: map[string]any{"unit": "todo", "verb": "nonexistent"},
	})
	if err == nil && (res == nil || !res.IsError) {
		t.Fatal("a refused verb came back as a successful call")
	}
}

// TestSummariseKeepsWhereEachParamTravels is the projection's own contract. The
// distinction between body, query and raw body is the whole reason describe
// exists: an agent that sends a query param in the body gets a 400 it cannot
// explain from the tool description alone.
func TestSummariseKeepsWhereEachParamTravels(t *testing.T) {
	units := []productapi.Unit{{
		ID: "todo",
		Verbs: []productapi.Verb{{
			Verb:    "search",
			Method:  "GET",
			Pattern: "/todo",
			Params:  []string{"title"},
			Query:   []string{"filter"},
			RawBody: "application/json",
			Page:    "/todo",
		}},
	}}
	got := summarise(units)
	if len(got) != 1 || len(got[0].Verbs) != 1 {
		t.Fatalf("summarise dropped a unit or verb: %+v", got)
	}
	v := got[0].Verbs[0]
	if len(v.Body) != 1 || v.Body[0] != "title" {
		t.Errorf("body = %v, want [title]", v.Body)
	}
	if len(v.Query) != 1 || v.Query[0] != "filter" {
		t.Errorf("query = %v, want [filter]", v.Query)
	}
	if v.RawBody != "application/json" {
		t.Errorf("rawBody = %q, want application/json", v.RawBody)
	}
}
