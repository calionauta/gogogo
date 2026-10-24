// SCOPE:feature — HTTP chat slice (per-request answer surfaces).
//
// Split from chi.go for the file-size gate, and along a real seam: chi.go
// owns the routes and the page/stream/ask handlers, this file owns how the
// tab's surface choice travels from the composer form to the queue job.
// The composer sends one `surfaces` value per enabled toggle plus an
// always-present empty marker, so "every band off" arrives as a
// present-but-empty set (prose-only) while an old client without the
// toggles sends nothing at all (handler default).
package handlers

import (
	"net/http"
	"strings"

	comp "github.com/calionauta/gogogo/features/genui/components"
)

// chatJob is the queue payload: the prompt plus the tab's surface choice.
// Surfaces rides the job (not the handler) so each tab toggles its own
// vocabulary: the composer builds the CSV from its three signals and always
// sends the param — even empty (all off = prose-only). Absent means an old
// client that predates the toggles, which gets the handler default.
type chatJob struct {
	Prompt string `json:"prompt"`
	UserID string `json:"userId"`
	Idem   string `json:"idem"`
	// Surfaces is nil when the client sent none (use the default), empty
	// when the tab turned every band off (prose-only). No omitempty: the
	// worker must tell "absent" (null) from "all off" ([]) apart.
	Surfaces []string `json:"surfaces"`
}

// requestSurfaces reads the tab's surface choice from the ask form: the
// composer sends one `surfaces` value per enabled toggle (catalog, custom,
// native — the official surface names) plus an always-present empty marker,
// so "every band off" arrives as a present-but-empty set (prose-only) while
// an old client without the toggles sends nothing at all (nil = handler
// default). Unknown names are an error: the composer only sends names it
// toggles, so this fires on a hand-made request, never on UI traffic.
func requestSurfaces(r *http.Request) ([]string, error) {
	vals, present := r.Form["surfaces"]
	if !present {
		return nil, nil
	}
	var names []string
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			names = append(names, v)
		}
	}
	if len(names) == 0 {
		return []string{}, nil
	}
	set, err := comp.ParseSurfaces(strings.Join(names, ","), nil)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(set))
	for _, name := range set {
		out = append(out, string(name))
	}
	return out, nil
}
