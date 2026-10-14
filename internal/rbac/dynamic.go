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

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/util"
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

// pathMatches is keyMatch2 with the same shape Casbin's built-in would
// have — kept as a named function so model.conf reads as one vocabulary.
func pathMatches(path, pattern string) bool {
	return util.KeyMatch2(path, pattern)
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
