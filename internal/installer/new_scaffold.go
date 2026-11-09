// Installer engine: `gogogo new` sources and wiring edits.
//
// The templates are the skeleton the todo slice would give you if it were
// writable: a chi handler with an auth-gated page and a JSON probe, the page
// itself, plus the self-mounting wiring (Register + Recipe + receipt) so a
// scaffolded feature mounts like every bundled unit. Everything is plain
// string insertion at anchors that already exist, so a moved anchor is a
// refusal before any file is written — never a half-wired tree.
package installer

import (
	"go/parser"
	"go/token"
	"strings"
)

// newHandlerSource renders features/<name>/handlers/chi.go. module is the
// target project's module path: every real project is renamed, so a hardcoded
// template path would import the upstream module instead of its own packages.
func newHandlerSource(name, module string) string {
	return strings.NewReplacer(
		"{{name}}", name,
		"{{module}}", module,
	).Replace(newHandlerTemplate)
}

// newPageSource renders features/<name>/components/page.templ. The page renders
// a body inside internal/ui.Shell, so it imports no feature package and needs
// no trimmed-tree variant: the shell owns the sidebar, assets and sound toggle.
func newPageSource(name, module string) string {
	return strings.NewReplacer(
		"{{name}}", name,
		"{{module}}", module,
	).Replace(newPageTemplate)
}

// newRegisterSource renders features/<name>/handlers/register.go: the
// self-mounting entry the generated registration calls.
func newRegisterSource(name, module string) string {
	return strings.NewReplacer(
		"{{name}}", name,
		"{{module}}", module,
	).Replace(newRegisterTemplate)
}

// newRecipeSource renders features/<name>/handlers/recipe.go: the seam
// declaration (routes the page + probe own).
func newRecipeSource(name, module string) string {
	return strings.NewReplacer(
		"{{name}}", name,
		"{{module}}", module,
	).Replace(newRecipeTemplate)
}

// newReceiptSource renders .recipes/<name>.yml: the install receipt for a
// scaffolded feature. Files are empty at scaffold time (the installer writes
// hashes on the next `recipe install`/`status` pass); the declaration is
// complete so drift is checked from the first boot.
func newReceiptSource(name, module string) string {
	return strings.NewReplacer(
		"{{name}}", name,
		"{{module}}", module,
	).Replace(newReceiptTemplate)
}

// editChiGo is kept for backward compatibility with the old manual mount.
// New scaffolds are self-mounting and need no chi.go edit: the registry's
// Register entry plus units.gen.go regen do the wiring. This function is a
// no-op (still validates) so old tests that call it continue to prove the
// shape instead of refusing.
func editChiGo(src, _, _ string) (string, error) {
	if perr := validateGo("internal/server/chi.go", src); perr != nil {
		return "", perr
	}
	return src, nil
}

// editRegistry appends one feature unit inside the []Capability literal, so
// it lands in the slice regardless of how All is wrapped (plain literal or
// append-wrapped across files). Inserting before the literal's close keeps a
// moved shape a refusal before any file is written — never a half-wired tree.
func editRegistry(src, name string) (string, error) {
	entry := newRegistryEntry(name)
	ins, err := registryInsertPoint(src)
	if err != nil {
		return "", err
	}
	out := src[:ins] + entry + src[ins:]
	if err := validateGo("internal/capabilities/registry.go", out); err != nil {
		return "", err
	}
	return out, nil
}

// registryInsertPoint returns the byte offset just before the closing brace
// of the []Capability literal: before "\n}\n" for a plain literal, before
// "\n}, " for the append-wrapped registry (var All = append([]Capability{…
// }, append(…))). Anything else is refused.
func registryInsertPoint(src string) (int, error) {
	if strings.Contains(src, "var All = ") && strings.Contains(src, "[]Capability{") {
		if idx := strings.LastIndex(src, "\n}\n"); idx >= 0 && idx == len(src)-3 {
			return idx + 1, nil
		}
		if idx := strings.Index(src, "\n}, "); idx >= 0 {
			return idx + 1, nil
		}
	}
	return 0, &ExitError{code: 1, msg: "internal/capabilities/registry.go: cannot find the end of " +
		"`var All = []Capability{…}` — refusing to guess (the registry shape moved)"}
}

// newRegistryEntry is the unit metadata a scaffolded feature starts with.
// Self-mounting: Register names the handlers package so the generated
// registration mounts it like every bundled unit (no chi.go edit).
func newRegistryEntry(name string) string {
	return "\t{\n" +
		"\t\tID:      \"" + name + "\",\n" +
		"\t\tKind:    KindFeature,\n" +
		"\t\tSummary: \"" + name + " feature (scaffolded by `gogogo new`)\",\n" +
		"\t\tDirs:    []string{\"features/" + name + "\"},\n" +
		"\t\tRegister: \"features/" + name + "/handlers\",\n" +
		"\t\tReason:  \"Scaffolded: no built-in unit yet — removal is manual until you add the strips.\",\n" +
		"\t},\n"
}

// appendPolicyRows adds the two user rows the RBAC coverage lint demands for
// the scaffolded routes. Literal paths (no wildcard) keep the policy shape
// gate quiet: widening a subtree must stay deliberate.
func appendPolicyRows(src, name string) string {
	if strings.Contains(src, "p, user, /"+name+", ") {
		return src
	}
	if !strings.HasSuffix(src, "\n") {
		src += "\n"
	}
	return src +
		"# `gogogo new`: " + name + " (page + probe; add a row per route as it grows)\n" +
		"p, user, /" + name + ", .*\n" +
		"p, user, /api/" + name + "/ping, .*\n"
}

// editRegistry appends one feature unit inside the []Capability literal, so
// it lands in the slice regardless of how All is wrapped (plain literal or
// append-wrapped across files). Inserting before the literal's close keeps a
// moved shape a refusal before any file is written — never a half-wired tree.
// validateGo parses a rendered source before it is written, so a template
// mistake is a refusal in the plan phase rather than a broken tree after it.
func validateGo(rel, src string) error {
	if _, err := parser.ParseFile(token.NewFileSet(), rel, src, parser.AllErrors); err != nil {
		return &ExitError{code: 1, msg: "generated " + rel + " does not parse: " + err.Error()}
	}
	return nil
}

// newHandlerTemplate is the handler skeleton. {{name}} and {{var}} are
// substituted; the removal class is the capability row, not a file header.
const newHandlerTemplate = `// {{name}} feature (scaffolded by ` + "`gogogo new`" + `)
//
// Two routes: an auth-gated page and a JSON probe. Wiring lives in
// internal/server/chi.go (import, construction, one MountFeature call) and the
// RBAC rows in internal/rbac/casbin/policy.csv. Grow the feature by adding
// methods to Routes and a ` + "`p, user, <path>, .*`" + ` row for each new path.
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"{{module}}/config"
	"{{module}}/features/{{name}}/components"
	"{{module}}/internal/auth"
)

// Handler serves the {{name}} feature on chi.
type Handler struct {
	auth *auth.Auth
	cfg  *config.Config
}

// NewHandler builds the chi handler. Nil deps panic: wire errors surface at
// boot, not per request.
func NewHandler(a *auth.Auth, cfg *config.Config) *Handler {
	if a == nil {
		panic("{{name}} chi: NewHandler with nil auth")
	}
	if cfg == nil {
		panic("{{name}} chi: NewHandler with nil config")
	}
	return &Handler{auth: a, cfg: cfg}
}

// Routes returns the chi router with the page and the JSON probe.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/{{name}}", h.handlePage)
	r.Get("/api/{{name}}/ping", h.handlePing)
	return r
}

// handlePage renders the page for a signed-in user; guests bounce to /login.
func (h *Handler) handlePage(w http.ResponseWriter, r *http.Request) {
	sess, err := h.auth.Session(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	email, role := "", ""
	if sess.User != nil {
		email = sess.User.Email
		role = sess.Role
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = components.Page(email, role, h.cfg.BuildLabel, h.cfg.BuildCommit).Render(r.Context(), w)
}

// handlePing is the cheapest end-to-end check that the route is mounted.
// It checks the session itself (401 JSON, never a redirect): RBAC_ENABLED
// defaults to false, so a policy row alone never guards a route.
func (h *Handler) handlePing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if _, err := h.auth.Session(r); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "auth required"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{
		"feature": "{{name}}",
		"status":  "ok",
	})
}
`

// newPageTemplate is the page skeleton. It loads the same core scripts the
// landing page does and reuses Tailwind classes already present in the
// bundle, so `make css-check` stays green without a rebuild.
//
// The asset URLs are fingerprinted (resources.StaticURL) like every other
// page in this repo: the query names the bytes, so the browser and any CDN
// in front of the app cache them immutably instead of revalidating on every
// navigation. A scaffolded page must not be the only one that pays the
// round trip.
const newPageTemplate = `// {{name}} page (scaffolded by ` + "`gogogo new`" + `)
package components

import "{{module}}/internal/ui"

// Page renders the {{name}} page inside the shared shell. The handler
// redirects guests before rendering, so email/role are the signed-in path;
// the shell owns the sidebar, header, assets and theme scripts.
templ Page(email string, role string, buildLabel string, buildCommit string) {
	@ui.Shell(ui.Data{
		Title:       "{{name}} — gogogo",
		Module:      "{{name}}",
		Heading:     "{{name}}",
		Context:     "scaffolded",
		Email:       email,
		Role:        role,
		BuildLabel:  buildLabel,
		BuildCommit: buildCommit,
	}, nil, nil, nil, body())
}

templ body() {
	<div class="ui-page ui-stack">
		<section class="ui-card">
			<div class="ui-card-body">
				<h2 class="ui-title">{{name}}</h2>
				<p class="opacity-70">Edit features/{{name}}/components/page.templ and reload.</p>
			</div>
		</section>
	</div>
}
`

// newRegisterTemplate is the self-mounting entry for a scaffolded feature.
// The generated registration calls it once; it builds the handler and
// hands its provider to the seam (no root edit).
const newRegisterTemplate = `// The {{name}} slice mounts itself through the recipe seam.
package handlers

import (
	"{{module}}/internal/recipe"
	"{{module}}/internal/recipe/register"
)

// Register builds the {{name}} handler and hands its declaration to the seam.
func Register(reg *recipe.Registry, d register.Deps) {
	reg.Add(NewHandler(d.Auth, d.Cfg).Recipe())
}
`

// newRecipeTemplate is the seam declaration for a scaffolded feature.
const newRecipeTemplate = `// The {{name}} unit declares its surface to the recipe seam.
package handlers

import (
	"github.com/go-chi/chi/v5"

	"{{module}}/internal/recipe"
)

// RecipeID is the recipe id the {{name}} unit declares itself under.
const RecipeID = "{{name}}"

// Recipe returns the declaration the composition root mounts through the
// recipe seam. It lives beside the routes it describes so the two cannot drift
// without one edit.
func (h *Handler) Recipe() recipe.Provider { return recipeProvider{h: h} }

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return RecipeID }

func (p recipeProvider) Declaration() recipe.Declaration {
	return recipe.Declaration{
		Routes: []recipe.Route{
			{Method: "GET", Pattern: "/{{name}}"},
			{Method: "GET", Pattern: "/api/{{name}}/ping"},
		},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
`

// newReceiptTemplate is the install receipt a scaffolded feature starts
// with. Files is empty until `recipe status` hashes the tree; the
// declaration is complete so drift is checked from the first boot.
const newReceiptTemplate = `id: {{name}}
version: v0.1.0
upstream:
  repo: github.com/calionauta/gogogo
  ref: master
  path: features/{{name}}
files: {}
declaration:
  routes:
    - { method: GET, pattern: /{{name}} }
    - { method: GET, pattern: /api/{{name}}/ping }
register: features/{{name}}/handlers
`
