package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/recipe"
)

// staticProvider is a Provider over a bare router, for tests that exercise the
// mount itself rather than a unit's declaration. It exists because MountFeature
// requires a Provider now: a test that wants "some routes on the tree" has to
// say what they are, which is the same discipline production follows.
type staticProvider struct {
	id    string
	decl  recipe.Declaration
	route chi.Router
}

func (p staticProvider) ID() string                      { return p.id }
func (p staticProvider) Declaration() recipe.Declaration { return p.decl }
func (p staticProvider) Routes() chi.Router              { return p.route }

// mountRoutes is the shorthand those tests want: a named unit with routes.
func mountRoutes(r *Router, id string, h chi.Router) {
	r.MountFeature(staticProvider{id: id, route: h})
}
