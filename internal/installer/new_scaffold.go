// SCOPE:plugin — installer engine: `gogogo new` sources and wiring edits.
//
// The templates are the skeleton the todo slice would give you if it were
// writable: a chi handler with an auth-gated page and a JSON probe, the page
// itself, and the three edits the composition root needs (import, parameter,
// mount) plus the registry unit and the RBAC rows. Everything is plain string
// insertion at anchors that already exist, so a moved anchor is a refusal
// before any file is written — never a half-wired tree.
package installer

import (
	"fmt"
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

// newPageSource renders features/<name>/components/page.templ. Sounds is
// optional because a scaffolded project may have trimmed the sounds unit;
// importing a missing package would fail the proof build.
func newPageSource(name, module string, withSounds bool) string {
	src := strings.NewReplacer(
		"{{name}}", name,
		"{{module}}", module,
	).Replace(newPageTemplate)
	if withSounds {
		return src
	}
	src = strings.ReplaceAll(src, "\t\""+module+"/features/sounds\"\n", "")
	return strings.ReplaceAll(src, "\t\t\t@sounds.SoundAssets()\n", "")
}

// editChiGo adds the feature to the composition root: one import and one
// self-contained block in BootChi (construction + mounts). Mounting in
// BootChi instead of threading a parameter through buildChiMux keeps the
// scaffold independent of every trim unit — credits, landing and their
// signature lines are all removable.
func editChiGo(src, name, module string) (string, error) {
	alias := name + "handlers"
	// The import anchor is module-relative: every real checkout is renamed,
	// so a hardcoded template path would refuse on the first `new`.
	importAnchor := "\tconfighandlers \"" + module + "/features/config/handlers\"\n"
	var err error
	if src, err = insertAfter(src, importAnchor,
		"\t"+alias+" \""+module+"/features/"+name+"/handlers\"\n"); err != nil {
		return "", err
	}
	if src, err = insertAfter(src, chiBootAnchor, "\n"+newMountBlock(name)); err != nil {
		return "", err
	}
	if perr := validateGo("internal/server/chi.go", src); perr != nil {
		return "", perr
	}
	return src, nil
}

// chiBootAnchor is the one fixed anchor `gogogo new` relies on. It is the
// console mount line, which is core in every trimmed tree and sits inside
// BootChi, where the composition router and the auth handle are in scope. The
// import anchor is derived from the target module path instead (see
// editChiGo).
const chiBootAnchor = "\tmountConsoleProxy(mux, a, cfg)"

// newMountBlock is the construction plus the one mount a scaffolded feature
// needs: MountFeature flattens the feature's own router (page, subtree and
// JSON API all declare their absolute paths there), so the root never
// re-states a URL.
func newMountBlock(name string) string {
	return "\t// " + name + ": scaffolded by `gogogo new`; delete these lines to unmount.\n" +
		"\t" + name + "H := " + name + "handlers.NewHandler(a)\n" +
		"\tmux.MountFeature(\"" + name + "\", " + name + "H.Routes())\n"
}

// editRegistry appends one feature unit to capabilities.All. The entry is
// written before the closing brace of the slice, at the end of the file.
func editRegistry(src, name string) (string, error) {
	idx := strings.LastIndex(src, "\n}\n")
	if idx < 0 || idx != len(src)-3 {
		return "", &ExitError{code: 1, msg: "internal/capabilities/registry.go: cannot find the end of " +
			"`var All = []Capability{…}` — refusing to guess (the registry shape moved)"}
	}
	entry := newRegistryEntry(name)
	out := src[:idx+1] + entry + src[idx+1:]
	if err := validateGo("internal/capabilities/registry.go", out); err != nil {
		return "", err
	}
	return out, nil
}

// newRegistryEntry is the unit metadata a scaffolded feature starts with.
func newRegistryEntry(name string) string {
	return "\t{\n" +
		"\t\tID:      \"" + name + "\",\n" +
		"\t\tKind:    KindFeature,\n" +
		"\t\tSummary: \"" + name + " feature (scaffolded by `gogogo new`)\",\n" +
		"\t\tDirs:    []string{\"features/" + name + "\"},\n" +
		"\t\tReason:  \"Scaffolded: no trim unit yet — removal is manual until you add the strips.\",\n" +
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

// insertAfter inserts add after the first occurrence of anchor.
func insertAfter(src, anchor, add string) (string, error) {
	idx := strings.Index(src, anchor)
	if idx < 0 {
		return "", anchorError(anchor)
	}
	at := idx + len(anchor)
	return src[:at] + add + src[at:], nil
}

// anchorError names the missing anchor instead of leaving a bare "not found".
func anchorError(anchor string) error {
	first, _, _ := strings.Cut(strings.TrimSpace(anchor), "\n")
	return &ExitError{code: 1, msg: fmt.Sprintf(
		"wiring anchor not found (%q) — this checkout's composition root moved; "+
			"scaffold by hand from features/todo and add the unit to internal/capabilities", first)}
}

// validateGo parses a rendered source before it is written, so a template
// mistake is a refusal in the plan phase rather than a broken tree after it.
func validateGo(rel, src string) error {
	if _, err := parser.ParseFile(token.NewFileSet(), rel, src, parser.AllErrors); err != nil {
		return &ExitError{code: 1, msg: "generated " + rel + " does not parse: " + err.Error()}
	}
	return nil
}

// newHandlerTemplate is the handler skeleton. {{name}} and {{var}} are
// substituted; the file carries its SCOPE line so check-scope is green.
const newHandlerTemplate = `// SCOPE:feature — {{name}} feature (scaffolded by ` + "`gogogo new`" + `)
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

	"{{module}}/features/{{name}}/components"
	"{{module}}/internal/auth"
)

// Handler serves the {{name}} feature on chi.
type Handler struct {
	auth *auth.Auth
}

// NewHandler builds the chi handler. Nil deps panic: wire errors surface at
// boot, not per request.
func NewHandler(a *auth.Auth) *Handler {
	if a == nil {
		panic("{{name}} chi: NewHandler with nil auth")
	}
	return &Handler{auth: a}
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
	_ = components.Page(email, role).Render(r.Context(), w)
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
const newPageTemplate = `// SCOPE:feature — {{name}} page (scaffolded by ` + "`gogogo new`" + `)
package components

import (
	"{{module}}/features/authviews"
	"{{module}}/features/sounds"
	"{{module}}/web/resources"
)

// Page renders the {{name}} page. The handler redirects guests before
// rendering, so userEmail/userRole are the signed-in path; the navbar uses
// the role for its admin-only links.
templ Page(userEmail string, userRole string) {
	<!DOCTYPE html>
	<html lang="en" { authviews.ThemeRootAttrs(ctx, "")... }>
		<head>
			<meta charset="utf-8"/>
			<meta name="viewport" content="width=device-width, initial-scale=1"/>
			<title>{{name}}</title>
			<link rel="stylesheet" href={ resources.StaticURL("app.min.css") }/>
			<link rel="stylesheet" href={ resources.StaticURL("app.css") }/>
			@authviews.ThemeHead()
			// Theme + Datastar are the two core scripts every page loads.
			<script defer type="module" src={ resources.StaticURL("theme.js") }></script>
			<script defer type="module" src={ resources.StaticURL("datastar.js") }></script>
			@sounds.SoundAssets()
		</head>
		<body class="min-h-screen bg-base-100">
			@authviews.Navbar(userEmail, userRole, "{{name}}", "", "")
			<main class="container mx-auto p-4">
				<h1 class="text-2xl font-bold">{{name}}</h1>
				<p class="opacity-70">Edit features/{{name}}/components/page.templ and reload.</p>
			</main>
		</body>
	</html>
}
`
