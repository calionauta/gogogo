// SCOPE:core — Request-scoped extension destinations for the shell's rail.
//
// A recipe contributes navigation through the `nav.sidebar` slot, and the rail
// is rendered by every page, so the composition root resolves the list once per
// page request and seeds it here. The shell therefore never imports the recipe
// registry, and a page template never asks which recipe an entry came from —
// the same rule the slots follow.
package ui

import (
	"context"

	"github.com/calionauta/gogogo/internal/rbac"
)

// NavEntry is one extension destination: a label and a same-origin path.
//
// The type carries no recipe id on purpose. The rail renders what it is given;
// attribution lives in the receipt, where an operator asking "where did this
// link come from?" looks, not in the markup.
type NavEntry struct {
	Label string
	Href  string
	// Admin keeps the destination out of a non-admin rail. The built-in
	// modules carry the same flag; without it an extension's operator-only
	// page would render a link that 403s.
	Admin bool
}

type navKey struct{}

// WithNav seeds the rail's extension destinations for one request.
func WithNav(ctx context.Context, entries []NavEntry) context.Context {
	if len(entries) == 0 {
		return ctx
	}
	return context.WithValue(ctx, navKey{}, entries)
}

// Nav returns the extension destinations to render. A page rendered outside
// the composition root (a component test, a trimmed build) reads nil and the
// rail simply has no extension group.
func Nav(ctx context.Context) []NavEntry {
	entries, _ := ctx.Value(navKey{}).([]NavEntry)
	return entries
}

// visibleNav drops the entries the role may not open. The rule mirrors
// Visible for the built-in modules so an extension cannot be the one
// destination in the rail that answers 403 when tapped.
func visibleNav(ctx context.Context, role string) []NavEntry {
	entries := Nav(ctx)
	if len(entries) == 0 {
		return nil
	}
	out := make([]NavEntry, 0, len(entries))
	for _, e := range entries {
		if e.Admin && !rbac.IsAdmin(role) {
			continue
		}
		out = append(out, e)
	}
	return out
}
