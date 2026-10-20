// SCOPE:core — RBAC coverage lint: every routed method has a policy decision.

package rbac

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// adminOnlyRoutes declares the routes the policy deliberately keeps away from
// a plain `user`. It is the ONE place that decision is written down, so a
// reviewer sees it in a diff instead of inferring it from an absent policy row.
var adminOnlyRoutes = map[string]bool{
	"/config":   true,
	"/config/":  true,
	"/config/*": true,
}

// publicRoutes declares the routes a signed-out visitor may reach. An
// accidentally-public route (reachable through a wildcard nobody meant to
// widen) fails here instead of shipping.
var publicRoutes = map[string]bool{
	"/":                           true,
	"/login":                      true,
	"/logout":                     true,
	"/health":                     true,
	"/sw.js":                      true,
	"/api/auth/signup/credential": true,
	"/api/auth/signin/credential": true,
	"/api/auth/signout":           true,
	"/api/auth/magic":             true,
	"/api/auth/magic/request":     true,
	"/api/session/stream":         true,
}

// TestRBACCoverageMatchesRoutes is the coverage lint that replaced the
// hand-maintained path list. It reads every registered route straight from
// the source (`r.Get("/path")` and friends in the feature routers) and
// asserts a policy decision exists for each method.
//
// Reading source instead of importing the handlers is deliberate: it keeps
// this package free of feature dependencies, so the check works in any
// trimmed tree (internal/installer drops whole features, and an import would
// drag a missing package into the trimmed build) and never needs a manifest
// line. The older coverage test enumerated paths BY HAND, which is how a
// realtime probe once shipped with no policy row at all.
//
// Red-proof: delete the `p, user, /api/session/stream, GET` row from
// policy.csv and this test names the route.
func TestRBACCoverageMatchesRoutes(t *testing.T) {
	e, err := NewEnforcer()
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	routes := registeredRoutes(t)
	if len(routes) < 30 {
		t.Fatalf("found only %d routes — the source scanner is broken, not the tree small", len(routes))
	}
	for _, rt := range routes {
		if rt.pattern == "" {
			continue
		}
		for _, method := range rt.methods {
			allowUser, uerr := e.Enforce(RoleUser, rt.pattern, method)
			if uerr != nil {
				t.Fatalf("enforce user %s %s: %v", method, rt.pattern, uerr)
			}
			allowAdmin, aerr := e.Enforce(RoleAdmin, rt.pattern, method)
			if aerr != nil {
				t.Fatalf("enforce admin %s %s: %v", method, rt.pattern, aerr)
			}
			if adminOnlyRoutes[rt.pattern] {
				if allowUser {
					t.Errorf("%s %s: route is in adminOnlyRoutes but the policy grants it to user", method, rt.pattern)
				}
				if !allowAdmin {
					t.Errorf("%s %s: admin-only route is denied for admin too — unreachable", method, rt.pattern)
				}
				continue
			}
			// The strict half: a non-admin-only route must allow `user` ON ITS
			// OWN. Testing user||admin lets `p, admin, /*, .*` mask a missing
			// user row — which is how a realtime probe once shipped broken:
			// admin reached it, so nothing looked wrong, while every
			// signed-in non-admin got a 403 on it.
			if !allowUser {
				t.Errorf("%s %s: no `user` policy row reaches this route "+
					"(admin's /* masks it, which is not a decision for users). "+
					"Add rows to casbin/policy.csv and, only if the route must stay "+
					"admin-only, to adminOnlyRoutes.", method, rt.pattern)
			}
			if !allowAdmin {
				t.Errorf("%s %s: admin cannot reach a non-admin-only route — check the /* row", method, rt.pattern)
			}
		}
	}
}

// TestRBACPublicRoutesArePublic is the other direction: every declared-public
// route must really be reachable without a session.
func TestRBACPublicRoutesArePublic(t *testing.T) {
	e, err := NewEnforcer()
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	declared := make([]string, 0, len(publicRoutes))
	for p := range publicRoutes {
		declared = append(declared, p)
	}
	slices.Sort(declared)
	for _, path := range declared {
		ok, eerr := e.Enforce(RoleAnonymous, path, "GET")
		if eerr != nil {
			t.Fatalf("enforce anonymous %s: %v", path, eerr)
		}
		if !ok {
			t.Errorf("%q is declared public but is not reachable anonymously", path)
		}
	}
}

type routeEntry struct {
	pattern string
	methods []string
}

// routeMethodRE matches the route-registration idiom every feature router uses:
//
//	r.Get("/notes", h.handleIndex)
//	r.Post("/api/notes/{docID}/op", h.handleOp)
var routeMethodRE = regexp.MustCompile(`r\.(Get|Post|Put|Patch|Delete|Head)\(["\x60](/[^"\x60]*)["\x60]`)

// registeredRoutes scans the feature routers for registered routes and the
// core server mounts for the paths it serves directly.
func registeredRoutes(t *testing.T) []routeEntry {
	t.Helper()
	root := repoRoot(t)
	byPath := map[string]map[string]bool{}
	add := func(path, method string) {
		if byPath[path] == nil {
			byPath[path] = map[string]bool{}
		}
		byPath[path][method] = true
	}

	// Feature routers: one chi.Router per feature package. Read through
	// os.OpenRoot: a WalkDir callback acting on a bare path is a symlink
	// TOCTOU (gosec G122), which is the same reason
	// internal/capabilities' scanner does it this way.
	rootFS, rerr := os.OpenRoot(root)
	if rerr != nil {
		t.Fatalf("open repo root: %v", rerr)
	}
	defer func() { _ = rootFS.Close() }()
	err := filepath.WalkDir(filepath.Join(root, "features"), func(p string, d fs.DirEntry, werr error) error {
		if werr != nil || d.IsDir() || d.Name() != "chi.go" {
			return werr
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		f, openErr := rootFS.Open(filepath.ToSlash(rel))
		if openErr != nil {
			return openErr
		}
		raw, readErr := io.ReadAll(f)
		_ = f.Close()
		if readErr != nil {
			return readErr
		}
		for _, m := range routeMethodRE.FindAllStringSubmatch(string(raw), -1) {
			method := strings.ToUpper(m[1])
			if method == "HEAD" {
				continue // HEAD rides GET in every router here
			}
			add(samplePath(m[2]), method)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk feature routers: %v", err)
	}

	// Core mounts: routes the server serves outside a feature router. Read
	// from chi.go/static.go so a rename shows up here instead of rotting.
	for _, rel := range []string{"internal/server/chi.go", "internal/server/static.go"} {
		raw, rerr := os.ReadFile(filepath.Join(root, rel))
		if rerr != nil {
			continue // a trimmed tree may drop one of them
		}
		for _, m := range coreMountRE.FindAllStringSubmatch(string(raw), -1) {
			path := m[1]
			if strings.HasSuffix(path, "/") && path != "/" {
				// A PREFIX mount ("/api/whiteboards/"): the subtree's real
				// routes come from the feature router scan above (every chi
				// router registers absolute paths). Synthesising a sample
				// here would invent a route nothing serves.
				continue
			}
			add(path, "GET")
		}
	}

	out := make([]routeEntry, 0, len(byPath))
	for path, methods := range byPath {
		ms := make([]string, 0, len(methods))
		for m := range methods {
			ms = append(ms, m)
		}
		slices.Sort(ms)
		out = append(out, routeEntry{pattern: path, methods: ms})
	}
	slices.SortFunc(out, func(a, b routeEntry) int { return strings.Compare(a.pattern, b.pattern) })
	return out
}

// coreMountRE matches the literal paths the core mux registers directly:
//
//	mux.Handle("/api/session/stream", a.SessionStreamHandler())
//	mux.HandleFunc("/sw.js", ...)
var coreMountRE = regexp.MustCompile(`mux\.(?:Handle|HandleFunc)\(["\x60](/[^"\x60]*)["\x60]`)

// samplePath turns a chi pattern into a concrete request path: `{x}` and `*`
// both become a literal segment, which is what a real request carries.
func samplePath(pattern string) string {
	pattern = strings.ReplaceAll(pattern, "/*", "/x")
	for {
		start := strings.Index(pattern, "{")
		if start < 0 {
			break
		}
		end := strings.Index(pattern[start:], "}")
		if end < 0 {
			break
		}
		pattern = pattern[:start] + "x" + pattern[start+end+1:]
	}
	return pattern
}

// repoRoot walks up from the test's working directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, serr := os.Stat(filepath.Join(dir, "go.mod")); serr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}
