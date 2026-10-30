// SCOPE:plugin — MCP tool: desktop-sync prerequisites.
//
// Split from main.go for the file-sizes gate. The text itself lives in
// internal/installer, so the CLI help and this tool cannot drift.
package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/calionauta/gogogo/internal/installer"
)

// handleSyncPrerequisites returns the shared guidance string. No arguments:
// the answer is the same for every project, and a path would only invite the
// caller to think this inspects a tree.
func handleSyncPrerequisites(
	_ context.Context, _ *mcp.CallToolRequest, _ struct{},
) (*mcp.CallToolResult, any, error) {
	return textResult(installer.SyncPrerequisites), nil, nil
}
