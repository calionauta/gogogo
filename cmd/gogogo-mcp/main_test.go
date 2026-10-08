package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestToolsListCapabilities(t *testing.T) {
	res, _, err := handleCapabilitiesList(context.Background(), nil, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	text := toolText(t, res)
	var caps []map[string]any
	if err := json.Unmarshal([]byte(text), &caps); err != nil {
		t.Fatalf("capabilities_list is not valid JSON: %v", err)
	}
	if len(caps) == 0 {
		t.Fatal("empty capability list")
	}
	found := map[string]bool{}
	for _, c := range caps {
		if id, ok := c["id"].(string); ok {
			found[id] = true
		}
		if _, ok := c["kind"]; !ok {
			t.Errorf("capability missing kind: %v", c)
		}
	}
	for _, want := range []string{"dagnats", "whiteboard", "nats", "todo", "skins"} {
		if !found[want] {
			t.Errorf("capability %q missing from list", want)
		}
	}
}

func TestTrimApplyRequiresConfirm(t *testing.T) {
	_, _, err := handleTrimApply(context.Background(), nil, trimArgs{Name: "x", Dir: "/tmp"})
	if err == nil || !strings.Contains(err.Error(), "confirm:true") {
		t.Errorf("expected confirm:true refusal, got %v", err)
	}
}

func TestAddUnitRequiresConfirm(t *testing.T) {
	_, _, err := handleAddUnit(context.Background(), nil,
		addArgs{Unit: "landing", From: "/tmp", Dir: "/tmp"})
	if err == nil || !strings.Contains(err.Error(), "confirm:true") {
		t.Errorf("expected confirm:true refusal, got %v", err)
	}
}

func TestAddUnitValidatesFields(t *testing.T) {
	_, _, err := handleAddUnit(context.Background(), nil,
		addArgs{Unit: "landing", Confirm: true})
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("expected required-fields error, got %v", err)
	}
	_, _, err = handleAddUnit(context.Background(), nil,
		addArgs{Unit: "nats", From: "/tmp", Dir: "/tmp", Confirm: true})
	if err == nil {
		t.Errorf("expected error for non-offered unit nats")
	}
}

func TestCheckTreeRequiresDir(t *testing.T) {
	_, _, err := handleCheckTree(context.Background(), nil, dirArgs{})
	if err == nil {
		t.Errorf("expected dir-required error")
	}
}

func TestAdviseStackNeedsNothing(t *testing.T) { // The guidance tool is read-only: no dir, no confirm, no changes.
	res, _, err := handleAdvise(context.Background(), nil, adviseArgs{Need: "offline airplane mode"})
	if err != nil {
		t.Fatal(err)
	}
	text := toolText(t, res)
	var doc struct {
		Presets []struct {
			Name string `json:"name"`
		} `json:"presets"`
	}
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("advise_stack is not valid JSON: %v", err)
	}
	if len(doc.Presets) == 0 || doc.Presets[0].Name != "offline-first" {
		t.Errorf("offline need did not surface offline-first: %v", doc.Presets)
	}
}

func TestAdviseStackGenericWordsStayInGoScopes(t *testing.T) {
	// Over the MCP transport, like an agent would call it: generic English
	// words must never route outside Go scopes, and every answer must be
	// valid JSON with a scope the docs promise.
	for _, need := range []string{
		"native serialization fast path behind a C ABI, with a pure-Go fallback",
		"fast",
		"Zig kernel",
		"Django blog",
	} {
		res, _, err := handleAdvise(context.Background(), nil, adviseArgs{Need: need})
		if err != nil {
			t.Fatalf("%q: %v", need, err)
		}
		var doc struct {
			Scope string `json:"scope"`
		}
		if err := json.Unmarshal([]byte(toolText(t, res)), &doc); err != nil {
			t.Fatalf("%q is not valid JSON: %v", need, err)
		}
		switch doc.Scope {
		case "template", "go-standards", "exception-to-go":
		default:
			t.Errorf("%q → scope %q, want template/go-standards/exception-to-go", need, doc.Scope)
		}
	}
}

func TestProtocolListsCatalogTools(t *testing.T) {
	// End-to-end over in-memory transports: the served surface is
	// exactly the catalog, callable through the MCP protocol.
	ctx := context.Background()
	server := buildServer()
	clientTrans, serverTrans := mcp.NewInMemoryTransports()
	// Canonical SDK order: server connects first (synchronously), then
	// the client (see modelcontextprotocol/go-sdk server_example_test).
	if _, err := server.Connect(ctx, serverTrans, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, nil)
	session, err := client.Connect(ctx, clientTrans, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) != len(toolCatalog) {
		names := []string{}
		for _, tool := range res.Tools {
			names = append(names, tool.Name)
		}
		t.Fatalf("want %d tools, got %d: %v", len(toolCatalog), len(res.Tools), names)
	}
	call, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "capabilities_list"})
	if err != nil {
		t.Fatal(err)
	}
	if len(call.Content) == 0 {
		t.Fatal("empty call result")
	}
}

func toolText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("empty tool result")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("non-text content: %T", res.Content[0])
	}
	return tc.Text
}

func TestTrimApplyEndToEnd(t *testing.T) {
	// The mutating path for real: smallest compilable template tree in,
	// rename + REAL tidy/build proof out, machine-readable envelope back.
	// Breaks when arg mapping, apply, rename, or proof regresses.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cmd", "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module github.com/calionauta/gogogo\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd", "web", "main.go"),
		[]byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, _, err := handleTrimApply(context.Background(), nil, trimArgs{
		Name: "mcp-app", Owner: "mcporg", Dir: dir, Confirm: true, Format: "json",
	})
	if err != nil {
		t.Fatalf("trim_apply: %v", err)
	}
	var env struct {
		BuildOk bool `json:"buildOk"`
		Plan    struct {
			Module string `json:"module"`
		} `json:"plan"`
		Next struct {
			Dir string `json:"dir"`
		} `json:"next"`
	}
	if uerr := json.Unmarshal([]byte(toolText(t, res)), &env); uerr != nil {
		t.Fatalf("trim_apply is not valid JSON: %v", uerr)
	}
	if !env.BuildOk {
		t.Error("buildOk must be true for the compilable fixture")
	}
	if env.Plan.Module != "github.com/mcporg/mcp-app" {
		t.Errorf("module = %q, want the renamed path", env.Plan.Module)
	}
	if env.Next.Dir != dir {
		t.Errorf("next.dir = %q, want %q", env.Next.Dir, dir)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "module github.com/mcporg/mcp-app") {
		t.Errorf("go.mod was not renamed on disk:\n%s", raw)
	}
}

func TestMcpReadmeListsTools(t *testing.T) {
	// Docs stay truthful: every served tool must appear backticked in the
	// module README's tools table, or humans read about a surface the
	// docs never explain.
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range toolCatalog {
		if !strings.Contains(string(raw), "`"+d.name+"`") {
			t.Errorf("README.md never mentions tool %q", d.name)
		}
	}
}
