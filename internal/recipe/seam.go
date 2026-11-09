// Spike 1: the declaration seam.
//
// The question this package answers: can a recipe's DECLARATION alone drive the
// composition root — routes, nav entries, policy rows, jobs — so that installing
// or removing a recipe never edits a core file except the generated registration?
//
// It is a spike, and it is deliberately narrow: it models registration, collision
// detection and mounting, and stops before persistence. The properties it must
// prove are:
//
//   - a declaration is data, so the surface can be checked before the build;
//   - two recipes claiming one route fail with both owners named, BEFORE boot;
//   - mounting is mechanical, so the root does not restate any URL;
//   - a recipe declaring a core object is refused.
package recipe

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
)

// Provider is what a recipe's copied code implements. The code lives in the
// user's tree; this interface is the only contract between it and the engine.
//
// Note what is NOT here: schema and policy rows are declared in the receipt, not
// returned by the code, because they are the append-only half — the engine
// applies them, and a recipe never writes an editable migration.
//
// A unit that is disabled at runtime returns a nil Provider from its Recipe
// method (room does this when the actor engine is off). Add ignores nil, so
// registration is a no-op and the route gate sees no claim: absence of code is
// absence of declaration, not a declaration the composed tree cannot honour.
type Provider interface {
	// ID is the recipe identifier, matching the receipt.
	ID() string
	// Declaration is the surface this recipe claims. It must match the receipt;
	// the drift check in receipt.go is what enforces that.
	Declaration() Declaration
	// Routes returns a chi router with ABSOLUTE patterns, the convention the
	// composition root already flattens with MountFeature.
	Routes() chi.Router
}

// Mounter is the composition root's seam, narrowed to one method so this package
// does not import internal/server (which would be an import cycle for anything
// that ever moves to a public tree).
type Mounter interface {
	MountFeature(owner string, h chi.Router)
}

// Registry holds the declared providers. Registration happens from a generated
// file so the installed set is a build fact, not a runtime discovery.
type Registry struct {
	mu        sync.RWMutex
	providers []Provider
}

// Add registers a provider. It is called from the generated registration file,
// once per installed recipe. A nil provider is a disabled unit (see Provider),
// not an error: the caller cannot know before boot whether the engine that
// owns the routes started, and a no-op is what the hardcoded mount did.
func (r *Registry) Add(p Provider) {
	if p == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = append(r.providers, p)
}

// Providers returns the registered providers in ID order, so mounting is
// deterministic and a boot log reads in a stable order.
func (r *Registry) Providers() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := append([]Provider(nil), r.providers...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

// Conflict names one collision: an object claimed by two owners.
type Conflict struct {
	Kind  string // "route", "table", "job", "slot", "recipe"
	Key   string
	Owner []string
}

func (c Conflict) String() string {
	return fmt.Sprintf("%s %q claimed by %s", c.Kind, c.Key, strings.Join(c.Owner, " and "))
}

// Validate checks the whole declared set BEFORE anything is mounted or built.
//
// This is the property that makes the model safe for third-party recipes: a
// collision is a build-time report naming both owners, not a boot panic in
// production and not a silently overwritten handler. The existing router panic is
// the last line of defence; this is the first, and it can run on declarations the
// code has not even been compiled against yet.
func (r *Registry) Validate() []Conflict {
	owners := map[string]map[string][]string{}
	claim := func(kind, key, owner string) {
		if owners[kind] == nil {
			owners[kind] = map[string][]string{}
		}
		owners[kind][key] = append(owners[kind][key], owner)
	}

	var conflicts []Conflict

	for _, p := range r.Providers() {
		d := p.Declaration()
		claim("recipe", p.ID(), p.ID())

		for _, rt := range d.Routes {
			claim("route", rt.Method+" "+rt.Pattern, p.ID())
		}
		for _, job := range d.Jobs {
			claim("job", job, p.ID())
			if !HasRecipePrefix(job, p.ID()) {
				conflicts = append(conflicts, Conflict{
					Kind: "job", Key: job, Owner: []string{p.ID(), "(no recipe prefix)"},
				})
			}
		}
		for _, table := range d.Tables {
			claim("table", table, p.ID())
			if isCoreTable(table) {
				conflicts = append(conflicts, Conflict{
					Kind: "table", Key: table, Owner: []string{p.ID(), "core"},
				})
			}
		}
		for _, slot := range d.Slots {
			claim("slot", slot, p.ID())
		}
		for _, m := range MetricProblems(d) {
			conflicts = append(conflicts, Conflict{
				Kind: "metric", Key: p.ID(), Owner: []string{p.ID(), m},
			})
		}
		for _, m := range PanelProblems(d) {
			conflicts = append(conflicts, Conflict{
				Kind: "panel", Key: p.ID(), Owner: []string{p.ID(), m},
			})
		}
	}

	for kind, keys := range owners {
		for key, who := range keys {
			if len(who) > 1 {
				conflicts = append(conflicts, Conflict{Kind: kind, Key: key, Owner: who})
			}
		}
	}
	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].Kind != conflicts[j].Kind {
			return conflicts[i].Kind < conflicts[j].Kind
		}
		return conflicts[i].Key < conflicts[j].Key
	})
	return conflicts
}

// Mount mounts every recipe's routes through the composition seam. It refuses to
// mount anything when the declared set has a conflict, so a bad set fails at
// build time in a test rather than at boot in production.
func (r *Registry) Mount(m Mounter) error {
	if conflicts := r.Validate(); len(conflicts) > 0 {
		msgs := make([]string, len(conflicts))
		for i, c := range conflicts {
			msgs[i] = c.String()
		}
		return fmt.Errorf("recipe set is invalid: %s", strings.Join(msgs, "; "))
	}
	for _, p := range r.Providers() {
		m.MountFeature(p.ID(), p.Routes())
	}
	return nil
}

// MountWith is a test and demo helper: build a mounter over a plain chi router
// so the seam can be exercised without the full server.
func MountWith(reg *Registry, root chi.Router) error {
	return reg.Mount(chiMounter{root: root})
}

type chiMounter struct{ root chi.Router }

func (c chiMounter) MountFeature(_ string, h chi.Router) {
	if h == nil {
		return
	}
	_ = chi.Walk(h, func(method, pattern string, handler http.Handler, _ ...func(http.Handler) http.Handler) error {
		c.root.Method(method, pattern, handler)
		return nil
	})
}

// HasRecipePrefix reports whether an object name belongs to the recipe, which is
// what makes uninstall and drain possible: an unprefixed job type or table cannot
// be attributed to its owner.
func HasRecipePrefix(name, recipeID string) bool {
	return strings.HasPrefix(name, "ext_"+recipeID) || strings.HasPrefix(name, "ext:"+recipeID+":")
}
