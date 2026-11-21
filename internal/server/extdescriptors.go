// Discovery: what THIS product offers, so a consuming agent can find its verbs
// without reading the checkout's .recipes/ files.
//
// The evolution CLI reads receipts because it edits a tree. A product's caller
// has no tree: it has a running server. `GET /api/ext/descriptors` answers from
// the same Registry that `/api/ext/invoke` dispatches through, so "what can I
// call" and "what happens when I call it" cannot disagree.
package server

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/recipe"
)

// descriptorsPath is the discovery surface. It sits beside the invoke route
// because they are one contract: discovery is the read half of invoking.
const descriptorsPath = "/api/ext/descriptors"

// unitDescriptor is one unit as a caller sees it: its id, its roles, and the
// verbs it answers. Deliberately NOT the receipt — no file hashes, no owner
// notes, no evolution fields. A product's caller needs the call surface.
type unitDescriptor struct {
	ID      string                `json:"id"`
	Roles   []string              `json:"roles,omitempty"`
	Verbs   []verbDescriptor      `json:"verbs"`
	Omitted []recipe.OmittedRoute `json:"omitted,omitempty"`
}

type verbDescriptor struct {
	Verb    string            `json:"verb"`
	Method  string            `json:"method"`
	Pattern string            `json:"pattern"`
	Summary string            `json:"summary,omitempty"`
	Params  []string          `json:"params,omitempty"`
	Query   []string          `json:"query,omitempty"`
	RawBody string            `json:"rawBody,omitempty"`
	Page    string            `json:"page,omitempty"`
	Help    map[string]string `json:"help,omitempty"`
}

// mountExtDescriptors registers the discovery route next to invoke.
func mountExtDescriptors(r *Router) {
	r.Claim("core", "GET", descriptorsPath)
	r.Get(descriptorsPath, extDescriptorsHandler(r.recipes))
}

// extDescriptorsHandler answers the call surface of the running product.
//
// It is session-gated like invoke: the verb list names what an authenticated
// caller may reach, and handing it to an anonymous request would describe the
// product's internals to anyone who can reach the port.
func extDescriptorsHandler(reg *recipe.Registry) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie(auth.CookieName); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if reg == nil {
			http.Error(w, "no recipes mounted", http.StatusServiceUnavailable)
			return
		}
		out := descriptorsOf(reg)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := json.NewEncoder(w).Encode(out); err != nil {
			http.Error(w, "could not encode descriptors", http.StatusInternalServerError)
		}
	})
}

// descriptorsOf projects the registry onto the wire shape. Sorted by id so the
// answer is stable — an agent comparing two deployments should not see a
// reordering as a change.
func descriptorsOf(reg *recipe.Registry) []unitDescriptor {
	providers := reg.Providers()
	out := make([]unitDescriptor, 0, len(providers))
	for _, p := range providers {
		d := p.Declaration()
		u := unitDescriptor{
			ID:      p.ID(),
			Roles:   recipe.Roles(p),
			Verbs:   make([]verbDescriptor, 0, len(d.Actions)),
			Omitted: d.Omitted,
		}
		for _, a := range d.Actions {
			u.Verbs = append(u.Verbs, verbDescriptor{
				Verb:    a.Verb,
				Method:  a.Route.Method,
				Pattern: a.Route.Pattern,
				Summary: a.Description,
				Params:  a.Params,
				Query:   a.Query,
				RawBody: a.RawBody,
				Page:    a.BrowserPath,
				Help:    a.ParamHelp,
			})
		}
		sort.Slice(u.Verbs, func(i, j int) bool { return u.Verbs[i].Verb < u.Verbs[j].Verb })
		out = append(out, u)
	}
	return out
}
