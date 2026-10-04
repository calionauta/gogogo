package main

import (
	"context"
	"encoding/json"
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

func TestProtocolListsFiveTools(t *testing.T) {
	// End-to-end over in-memory transports: the served surface is
	// exactly five tools, callable through the MCP protocol.
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
	if len(res.Tools) != 5 {
		names := []string{}
		for _, tool := range res.Tools {
			names = append(names, tool.Name)
		}
		t.Fatalf("want 5 tools, got %d: %v", len(res.Tools), names)
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
