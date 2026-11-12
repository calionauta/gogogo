package handlers

import (
	"context"

	"github.com/calionauta/gogogo/features/chat/store"
	comp "github.com/calionauta/gogogo/features/genui/components"
	"github.com/calionauta/gogogo/internal/observability"
)

// catalogCompatible reconciles one stored answer with the catalog that is
// serving it now. An answer stamped with a different vocabulary fingerprint was
// validated against components this build may no longer have; rendering it
// through RenderAll would fail on the retired name and blank the WHOLE message
// (a half answer is worse than none). Filtering first keeps every component the
// catalog still knows and drops the rest, which is the honest degradation.
//
// The stamp is what makes this targeted: an unstamped row (written before the
// field existed) and a matching row both render as stored, so nothing is
// filtered on a guess. Chat is the only writer of stored directives, so the
// filter runs with chat's own default surface set.
func catalogCompatible(ctx context.Context, m store.Message) []comp.Directive {
	current := comp.CatalogVersion()
	if m.CatalogVersion == "" || m.CatalogVersion == current {
		return m.Components
	}
	kept := comp.FilterDirectives(m.Components, comp.DefaultSurfacesChat)
	if len(kept) == len(m.Components) {
		return kept
	}
	observability.Warn(ctx, "chat: stored answer predates the catalog, dropped unknown components",
		append(observability.WithComponent("chat", "feature"),
			"stored", m.CatalogVersion, "current", current,
			"dropped", len(m.Components)-len(kept))...)
	return kept
}
