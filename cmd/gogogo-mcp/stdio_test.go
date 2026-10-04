package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestStdioRoundTrip spawns the real binary over stdio (the exact path an
// LLM client takes via CommandTransport) and drives ListTools + one CallTool.
// The in-memory protocol test bypasses framing; this one does not.
func TestStdioRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain required to build the probe binary")
	}
	bin := filepath.Join(t.TempDir(), "gogogo-mcp-probe")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build probe binary: %v\n%s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "stdio-probe", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: exec.Command(bin)}, nil)
	if err != nil {
		t.Fatalf("connect over stdio: %v", err)
	}
	defer session.Close()

	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools over stdio: %v", err)
	}
	if len(res.Tools) != len(toolCatalog) {
		t.Fatalf("stdio tools = %d, want %d", len(res.Tools), len(toolCatalog))
	}

	call, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "advise_stack",
		Arguments: map[string]any{"need": "offline airplane mode"},
	})
	if err != nil {
		t.Fatalf("advise_stack over stdio: %v", err)
	}
	if len(call.Content) == 0 {
		t.Fatal("empty advise_stack result over stdio")
	}
	tc, ok := call.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("non-text content over stdio: %T", call.Content[0])
	}
	if !strings.Contains(tc.Text, "offline-first") {
		t.Errorf("stdio advise did not surface offline-first:\n%.500s", tc.Text)
	}

	// Omitted handler-required fields must fail with the handler's
	// friendly error, not a schema wall: check_tree without dir. (Tool
	// failures surface as error content, not a Go error — assert there.)
	res2, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "check_tree",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("transport error, want tool-level refusal: %v", err)
	}
	if len(res2.Content) == 0 {
		t.Fatal("empty check_tree refusal")
	}
	tc2, ok := res2.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(tc2.Text, "dir is required") {
		t.Errorf("expected the handler dir-required error, got %#v", res2.Content)
	}
}
