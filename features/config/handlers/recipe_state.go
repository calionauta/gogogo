// Admin recipe switch: the runtime enable/disable endpoint.
//
// Split from chi.go for the file-size gate. The decision is boot-time on
// purpose — a disabled recipe must not start its workers — so the redirect
// says "takes effect on the next boot" rather than pretending it is instant.
package handlers

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/calionauta/gogogo/features/config/components"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/capabilities"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/events"
	"github.com/calionauta/gogogo/internal/recipestate"
)

// eventRecipeToggled names the core domain fact a recipe switch publishes: the
// state changed, and a hook sees who changed it. It is not the audit row —
// auth.RecordAudit stays the durable operator record.
const eventRecipeToggled = "recipe.toggled"

// applyRecipeStates loads the runtime switches and separates "disabled" from
// "removed" on the Units table. Extracted from handleIndex for the complexity
// gate; it is one concern with one source (recipe_state).
func (h *Handler) applyRecipeStates(ctx context.Context, data *components.PageData) {
	// An unreadable table renders every row enabled: that is the same default
	// the boot switch takes, and the alternative — showing a state the page
	// could not read — would be worse than the permissive one.
	states, _ := recipestate.Load(ctx, h.queries)
	data.RecipeStates = states
	if h.providers != nil {
		mounted := make(map[string]bool, len(h.providers.Providers()))
		for _, p := range h.providers.Providers() {
			mounted[p.ID()] = true
		}
		components.MarkRemovedUnits(data, mounted)
	}
	h.appendInstalledRows(data)
	for i := range data.Units {
		for j := range data.Units[i].Rows {
			row := &data.Units[i].Rows[j]
			if enabled, ok := data.RecipeStates[row.ID]; ok && !enabled {
				row.Status = components.UnitStatusDisabled
			}
		}
	}
}

// appendInstalledRows adds the recipes the generated registration knows but
// the capability registry does not (an out-of-tree `recipe install`). They are
// feature rows on the same Units table, so the runtime switch reaches them.
func (h *Handler) appendInstalledRows(data *components.PageData) {
	if len(h.installed) == 0 {
		return
	}
	known := map[string]bool{}
	for _, group := range data.Units {
		for _, row := range group.Rows {
			known[row.ID] = true
		}
	}
	var extra []components.UnitRow
	for _, id := range h.installed {
		if known[id] {
			continue
		}
		extra = append(extra, components.UnitRow{
			ID: id, Summary: "installed recipe", Status: components.UnitStatusIn,
			Kind: capabilities.KindFeature,
		})
	}
	if len(extra) == 0 {
		return
	}
	slices.SortFunc(extra, func(a, b components.UnitRow) int { return strings.Compare(a.ID, b.ID) })
	label := capabilities.KindGroupLabel(capabilities.KindFeature)
	for i := range data.Units {
		if data.Units[i].Label == label {
			data.Units[i].Rows = append(data.Units[i].Rows, extra...)
			return
		}
	}
	data.Units = append(data.Units, components.UnitGroup{Label: label, Rows: extra})
}

// handleRecipeToggle flips the runtime switch for one feature recipe. It is an
// admin mutation like a role change: step-up proof, audit row, and a redirect
// that says the change applies on the next boot.
func (h *Handler) handleRecipeToggle(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.adminSession(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectConfigTab(w, r, "error", "Invalid form submission.")
		return
	}
	id := strings.TrimSpace(r.PostFormValue("id"))
	if !h.toggleable(id) {
		redirectConfigTab(w, r, "error", "Only feature recipes can be toggled.")
		return
	}
	enabled := r.PostFormValue("enabled") == "true"
	if !h.verifyAdminProof(w, r, sess, "recipe toggle", id, id) {
		return
	}
	var flag int64
	if enabled {
		flag = 1
	}
	if err := h.queries.SetRecipeState(r.Context(), dbgen.SetRecipeStateParams{
		ID: id, Enabled: flag, UpdatedBy: sess.User.Email, UpdatedAt: dbschema.Now(),
	}); err != nil {
		redirectConfigTab(w, r, "error", "Could not update the recipe state.")
		return
	}
	state := "enabled=false"
	if enabled {
		state = "enabled=true"
	}
	h.auth.RecordAudit(r.Context(), auth.EventAdminRecipeToggled, state, id, id, sess.User.ID)
	h.bus.Emit(r.Context(), events.Event{
		Name: eventRecipeToggled,
		Fields: map[string]any{
			"recipe":  id,
			"enabled": enabled,
			"actor":   sess.User.ID,
		},
	})
	redirectConfigTab(w, r, "notice",
		"Recipe "+id+" updated; it takes effect on the next boot.")
}

// toggleable reports whether id is a feature recipe this build installed. A
// bundled/scaffolded unit is in the registry; an out-of-tree install is in the
// generated registration (including one that is disabled and not mounted).
func (h *Handler) toggleable(id string) bool {
	if cap, found := capabilities.ByID()[id]; found {
		return cap.Kind == capabilities.KindFeature
	}
	return slices.Contains(h.installed, id)
}
