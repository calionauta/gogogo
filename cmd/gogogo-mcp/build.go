// SCOPE:plugin — MCP tool: desktop/mobile build prerequisites.
//
// Split from main.go for the file-sizes gate. The text lives in
// internal/installer, so the CLI help and this tool cannot drift.
package main

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/calionauta/gogogo/internal/installer"
)

type buildArgs struct {
	Platform string `json:"platform,omitempty" jsonschema:"desktop or mobile (empty returns both)"`
}

func handleBuildPrerequisites(
	_ context.Context, _ *mcp.CallToolRequest, args buildArgs,
) (*mcp.CallToolResult, any, error) {
	switch strings.ToLower(strings.TrimSpace(args.Platform)) {
	case "desktop":
		return textResult(installer.DesktopPrerequisites), nil, nil
	case "mobile", "android":
		return textResult(installer.MobilePrerequisites), nil, nil
	default:
		return textResult(installer.DesktopPrerequisites + "\n\n" + installer.MobilePrerequisites), nil, nil
	}
}
