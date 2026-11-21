// The config view's headless read: what is configured, never the value.
//
// The HTML page masks in the template. A JSON view cannot lean on that, so this
// handler serialises PageData — the same rows BuildPageData already put through
// maskValue — instead of reading cfg field by field. The mask is therefore
// structural: there is no second place where a secret-shaped field could be
// forgotten, and a new config field is masked by whatever policy already
// governs its group/key.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/config/components"
	"github.com/calionauta/gogogo/internal/observability"
)

// configAPIPath is the read surface. It sits under /api/ because the page route
// stays HTML: an agent asks for the view as data, a browser asks for the page.
const configAPIPath = "/api/config"

// configPayload is the wire shape. Rows keep their Env name so an operator can
// correlate a row with an env var without reading the code, and Masked/BoolOnly
// are explicit so a caller never has to guess whether a Display is real.
type configPayload struct {
	AppName    string             `json:"appName"`
	BuildLabel string             `json:"buildLabel"`
	RBACOn     bool               `json:"rbacEnabled"`
	Groups     []configGroup      `json:"groups"`
	Status     []configStatusItem `json:"status"`
}

type configGroup struct {
	Group string      `json:"group"`
	Rows  []configRow `json:"rows"`
}

type configRow struct {
	Group    string `json:"group"`
	Key      string `json:"key"`
	Env      string `json:"env"`
	Display  string `json:"display"`
	Masked   bool   `json:"masked"`
	BoolOnly bool   `json:"boolOnly"`
	Set      bool   `json:"set"`
}

// configStatusItem is components.StatusItem with wire tags. Converting is safe
// because the field sets are identical; a new field on either side fails to
// compile rather than silently dropping out of the read.
type configStatusItem components.StatusItem

// readRoute resolves the session and hands the read a value, which is what keeps
// the payload builder testable without a live auth stack.
func (h *Handler) readRoute(w http.ResponseWriter, r *http.Request) {
	if _, err := h.auth.Session(r); err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	writeConfigJSON(r.Context(), w, configReadBody(h.cfg))
}

// configReadBody maps PageData onto the wire shape. It takes the already-masked
// PageData, so a caller cannot accidentally pass a raw cfg through a path that
// skips maskValue — the only way to build a payload is from masked rows.
func configReadBody(cfg *config.Config) configPayload {
	data := components.BuildPageData(cfg)
	out := configPayload{
		AppName:    data.AppName,
		BuildLabel: data.BuildLabel,
		RBACOn:     data.RBACEnabled,
		Groups:     make([]configGroup, 0, len(data.Groups)),
		Status:     make([]configStatusItem, 0, len(data.Status)),
	}
	for _, g := range data.Groups {
		grp := configGroup{Group: g.Name, Rows: make([]configRow, 0, len(g.Rows))}
		for _, r := range g.Rows {
			grp.Rows = append(grp.Rows, configRow{
				Group:    r.Group,
				Key:      r.Key,
				Env:      r.Env,
				Display:  r.Display,
				Masked:   r.Masked,
				BoolOnly: r.BoolOnly,
				Set:      r.Set,
			})
		}
		out.Groups = append(out.Groups, grp)
	}
	for _, s := range data.Status {
		out.Status = append(out.Status, configStatusItem(s))
	}
	return out
}

func writeConfigJSON(ctx context.Context, w http.ResponseWriter, payload configPayload) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		observability.Error(ctx, "config: encode read",
			observability.WithComponent("config-view", "core"), "error", err)
		http.Error(w, "could not encode the config view", http.StatusInternalServerError)
	}
}
