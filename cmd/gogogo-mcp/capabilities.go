// The capabilities_list tool: the registry, and — with a checkout — each
// feature's domain-card boundary.
//
// It lives in its own file because the tool is the one place the MCP surface
// reads the tree: the registry answers from anywhere, the cards need a path.
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/calionauta/gogogo/internal/capabilities"
)

// capabilitiesArgs takes an optional checkout. Without one the registry is
// static and answers from any directory; with one, each feature unit also
// carries the one-line `owns` its domain card states, so a client choosing a
// unit reads the boundary instead of guessing it from the id.
type capabilitiesArgs struct {
	Dir string `json:"dir"`
}

// capabilityEntry is a registry unit plus, for a feature, its card's boundary.
type capabilityEntry struct {
	capabilities.Capability
	Owns string `json:"owns,omitempty"`
}

func handleCapabilitiesList(
	_ context.Context, _ *mcp.CallToolRequest, args capabilitiesArgs,
) (*mcp.CallToolResult, any, error) {
	units, err := capabilitiesWithBoundaries(args.Dir)
	if err != nil {
		return nil, nil, err
	}
	raw, err := json.MarshalIndent(units, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return textResult(string(raw)), nil, nil
}

// capabilitiesWithBoundaries is the one place the two answers differ: the
// registry alone, or the registry with each feature's card summary. A dir with
// no cards is a caller mistake worth reporting — silently returning the bare
// registry would look like every slice lost its boundary.
func capabilitiesWithBoundaries(dir string) ([]capabilityEntry, error) {
	if dir == "" {
		units := make([]capabilityEntry, 0, len(capabilities.All))
		for _, c := range capabilities.All {
			units = append(units, capabilityEntry{Capability: c})
		}
		return units, nil
	}
	units := make([]capabilityEntry, 0, len(capabilities.All))
	cards := 0
	for _, c := range capabilities.All {
		e := capabilityEntry{Capability: c}
		if c.Kind == capabilities.KindFeature {
			if card, cardErr := capabilities.ReadCard(dir, c.ID); cardErr == nil {
				e.Owns = card.Owns
				cards++
			}
		}
		units = append(units, e)
	}
	if cards == 0 {
		return nil, fmt.Errorf("no domain cards under %s/%s — is %s a gogogo checkout?",
			dir, capabilities.CardDir, dir)
	}
	return units, nil
}
