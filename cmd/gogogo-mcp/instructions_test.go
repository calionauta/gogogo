package main

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// instructionsBudget is the ceiling the text must stay under. Claude Code
// truncates the server `instructions` field at 2048 characters, so a rule
// pushed past this line is a rule most clients never read. The budget is the
// reason the file is short: it is a route, not a manual.
const instructionsBudget = 2048

// TestServerInstructionsAreServed pins the delivery path: the text is on the
// server (so the client gets it at session start, with no skill install) and
// arrives in the initialize result over the real protocol.
func TestServerInstructionsAreServed(t *testing.T) {
	text := serverOptions().Instructions
	if strings.TrimSpace(text) == "" {
		t.Fatal("server carries no instructions — the client learns the order from nowhere")
	}
	if n := len(text); n > instructionsBudget {
		t.Errorf("instructions are %d chars, over the %d budget — clients truncate the tail, "+
			"so the last rule is silently dropped", n, instructionsBudget)
	}

	ctx := context.Background()
	server := buildServer()
	clientTrans, serverTrans := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverTrans, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, nil)
	session, err := client.Connect(ctx, clientTrans, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	served := ""
	if res := session.InitializeResult(); res != nil {
		served = res.Instructions
	}
	if strings.TrimSpace(served) == "" {
		t.Fatal("initialize result carries no instructions — the field never left the server")
	}
	if served != text {
		t.Errorf("served instructions differ from the loaded prompt:\n%s", served)
	}
}

// TestServerInstructionsCarryTheNonNegotiables pins the five rules that no
// tool description can enforce by itself. Each one is a rule this project has
// paid for: a scaffold with an invented name, a landing page built in a
// product binary, an agent that reads "confirm:true" as "the user said yes",
// a port presented as an install, and skills nobody re-registered.
func TestServerInstructionsCarryTheNonNegotiables(t *testing.T) {
	text := serverOptions().Instructions
	for _, want := range []struct{ name, needle string }{
		{"ask before scaffolding", "ASK THE USER"},
		{"a landing page is out of scope", "landing"},
		{"confirm is not consent", "confirm=true"},
		{"port, do not scaffold, a foreign tree", "export_unit"},
		{"skills are not re-registered by the scaffold", ".agents/skills/"},
	} {
		if !strings.Contains(text, want.needle) {
			t.Errorf("instructions never say %q (missing %q)", want.name, want.needle)
		}
	}
}
