// SCOPE:core — Dynamic / ABAC decision inputs for the RBAC enforcer.
//
// The base policy decides on the coarse role subject + path + method
// (policy.csv). This file adds the two dynamic inputs the static policy
// alone cannot express:
//
//  1. ABAC identity attributes — a request may carry an Object (owner,
//     subject id, tenant, IP) and the matcher's attrOK clause enforces
//     ownership when an owner is present. The middleware passes an
//     Object; tests and simple callers may keep passing a path string.
//  2. Code-defined matcher functions — pathMatch/attrOK are plain Go, so a
//     product adds any conditional it needs by registering another
//     function here and naming it in model.conf. That is the "define
//     permissions from code, variables and DB data" seam.
//
// Everything is a no-op for a bare path string: existing string callers
// keep identical decisions until an Object carries an owner. Worked
// example + how to extend: docs/rbac.md ("Dynamic permissions").
package rbac

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/casbin/casbin/v3"
)

// Object is the ABAC form of a request object. It upgrades the plain path
// string with the attributes a per-record decision needs. Its zero value
// is a path-only request, so an enforcer driven with a bare path behaves
// exactly as before.
type Object struct {
	Path      string
	SubjectID string // the authenticated identity (users.id), NOT the coarse role
	Owner     string // the record's owner id; empty disables the ownership clause
	Tenant    string // reserved for a future multi-tenant rule
	IP        string // client address, for a code-defined IP condition
}

// RegisterDynamic installs the matcher functions model.conf names
// (pathMatch, attrOK) on a freshly built enforcer. Call once, right after
// building the enforcer (finishEnforcer does). Casbin's AddFunction has
// no error return; a bad arity surfaces as a matcher error at Enforce
// time, which the middleware fails closed on.
func RegisterDynamic(e *casbin.Enforcer) {
	e.AddFunction("pathMatch", func(args ...any) (any, error) {
		if len(args) != 2 {
			return false, fmt.Errorf("pathMatch: want 2 args, got %d", len(args))
		}
		pat, _ := args[1].(string)
		return pathMatches(objPath(args[0]), pat), nil
	})
	e.AddFunction("attrOK", func(args ...any) (any, error) {
		if len(args) != 2 {
			return false, fmt.Errorf("attrOK: want 2 args, got %d", len(args))
		}
		sub, _ := args[0].(string)
		return attrsOK(sub, args[1]), nil
	})
}

// objPath extracts the path keyMatch2 runs on: the Object's Path or the
// bare string, whichever the caller passed.
func objPath(obj any) string {
	o, ok := asObject(obj)
	if ok {
		return o.Path
	}
	if s, ok := obj.(string); ok {
		return s
	}
	return ""
}

// pathMatches decides whether a request path is covered by a policy
// object. It keeps keyMatch2's SHAPE — `*` spans `/`, `:name` matches one
// segment — with one deliberate difference: every literal run is escaped.
//
// Casbin's util.KeyMatch2 rewrites the object into a regexp and leaves
// metacharacters live, so `p, anonymous, /sw.js, GET` also allows
// `/swXjs` and a future `p, user, /api/v1.0/*, .*` would allow
// `/api/v1X0/...`. That is a silent over-grant nobody can see in the CSV.
// Escaping the literals makes the object mean exactly what it reads.
//
// Compilation is cached: the enforcer consults the same handful of
// patterns on every request, and Casbin's built-in recompiles per call.
func pathMatches(path, pattern string) bool {
	re, err := patternRegexp(pattern)
	if err != nil {
		// An uncompilable object is a policy bug. Fail closed: the caller
		// (attrOK/pathMatch) returns false and the middleware denies.
		return false
	}
	return re.MatchString(path)
}

// patternCache memoizes compiled policy objects. Keys are the objects
// themselves; the value is a *regexp.Regexp. Untrusted input never reaches
// this map — only the embedded or DB-loaded policy does.
// policyFields is the arity of a "p, sub, obj, act" CSV row.
const policyFields = 4

var patternCache sync.Map // map[string]*regexp.Regexp

// patternRegexp compiles one policy object into an anchored regexp:
//
//   - -> .*   (spans segments, keyMatch2 semantics)
//     :name  -> [^/]+ (one segment)
//     other  -> QuoteMeta(other)
//
// Segments are split on `/` so escaping never swallows a separator.
func patternRegexp(pattern string) (*regexp.Regexp, error) {
	if cached, ok := patternCache.Load(pattern); ok {
		if re, ok := cached.(*regexp.Regexp); ok {
			return re, nil
		}
	}
	segments := strings.Split(pattern, "/")
	for i, seg := range segments {
		switch {
		case seg == "*":
			segments[i] = ".*"
		case strings.HasPrefix(seg, ":"):
			segments[i] = "[^/]+"
		default:
			segments[i] = regexp.QuoteMeta(seg)
		}
	}
	re, err := regexp.Compile("^" + strings.Join(segments, "/") + "$")
	if err != nil {
		return nil, fmt.Errorf("rbac: policy object %q: %w", pattern, err)
	}
	patternCache.Store(pattern, re)
	return re, nil
}

// attrsOK is the ownership clause. A bare string has no attributes, so it
// always passes (the base policy decides). An Object with no Owner also
// passes. Only an Object carrying an Owner restricts: admin passes, and
// otherwise the subject id must equal the owner. Adding a tenant or IP
// rule means extending this function — the model already calls it.
func attrsOK(sub string, obj any) bool {
	o, ok := asObject(obj)
	if !ok || o.Owner == "" {
		return true
	}
	if sub == RoleAdmin {
		return true
	}
	return o.SubjectID != "" && o.SubjectID == o.Owner
}

func asObject(obj any) (Object, bool) {
	switch v := obj.(type) {
	case Object:
		return v, true
	case *Object:
		if v != nil {
			return *v, true
		}
	}
	return Object{}, false
}

// ValidatePolicyObjects is the authoring guard for policy.csv: every
// object must be a plain path, a `*` segment, or a `:name` segment.
//
// It exists because pathMatches escapes literals, so a metacharacter is
// no longer an over-grant risk — but it silently becomes a LITERAL dot or
// plus, which is never what an author meant. Catching it at
// enforcer-build time (NewEnforcer, NewEnforcerWithAdapter) turns a
// silent semantic change into a boot failure naming the row. A `.` is
// allowed (real paths carry file extensions: /sw.js); every other regex
// metacharacter is refused, because a path never needs one.
//
// Deliberately not a regexp-shape check: the point is that objects are
// paths, not patterns. A reviewer reading the CSV can trust that.
func ValidatePolicyObjects(policyCSV string) error {
	lineNo := 0
	for line := range strings.SplitSeq(policyCSV, "\n") {
		lineNo++
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) != policyFields {
			continue // the shape test owns arity; nothing to validate here
		}
		obj := strings.TrimSpace(parts[2])
		for seg := range strings.SplitSeq(obj, "/") {
			if seg == "*" || strings.HasPrefix(seg, ":") {
				continue
			}
			if strings.ContainsAny(seg, "+()[]{}|^$?\\") {
				return fmt.Errorf(
					"rbac: policy line %d object %q contains regex metacharacter in segment %q "+
						"(objects are paths: use `*` for a whole segment, `:name` for a placeholder)",
					lineNo, obj, seg)
			}
		}
	}
	return nil
}
