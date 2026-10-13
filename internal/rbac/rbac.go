// SCOPE:core — RBAC with Casbin (embedded model + policy)
//
// The Casbin model (.conf) and policy (.csv) are compiled into the binary
// with //go:embed, so no `.conf`/`.csv` file is ever read from disk at
// runtime. A single binary carries its own authorization rules.
//
// Authoring guide: docs/rbac.md (lifecycle, file map, row anatomy,
// checklists). Role vocabulary lives in roles.go; every policy invariant
// an LLM can break is pinned by a red-proof test in rbac_test.go
// (shape, flat, dedupe, coverage, viewer matrix) enforced in CI and by
// the rbac-policy pre-commit hook. There is no upstream Casbin policy
// linter for Go (only the online editor and load-time parse errors),
// so those tests ARE this repo's Casbin lint.
package rbac

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
)

//go:embed casbin/model.conf
var modelConf string

//go:embed casbin/policy.csv
var policyCSV string

// NewEnforcer builds a Casbin enforcer from the embedded model and policy.
// Both the request/policy/role definitions and the default rules come from
// the binary — there is no adapter and no filesystem access.
func NewEnforcer() (*casbin.Enforcer, error) {
	m, err := model.NewModelFromString(modelConf)
	if err != nil {
		return nil, fmt.Errorf("rbac: parse embedded model: %w", err)
	}
	lineNo := 0
	for line := range strings.SplitSeq(policyCSV, "\n") {
		lineNo++
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// A malformed policy line must fail closed: ignoring it would leave
		// the enforcer running with fewer rules than the operator wrote.
		if perr := persist.LoadPolicyLine(line, m); perr != nil {
			return nil, fmt.Errorf("rbac: parse embedded policy line %d: %w", lineNo, perr)
		}
	}
	e, err := casbin.NewEnforcer(m)
	if err != nil {
		return nil, fmt.Errorf("rbac: new enforcer: %w", err)
	}
	return e, nil
}

// EnforceWith runs the decision against an already-built enforcer — the hot
// path for a request middleware, which builds once and reuses. There is
// deliberately no per-call Enforce(sub, obj, act) helper that builds an
// enforcer internally: parsing model + policy per request is pure waste,
// and its presence invites the next LLM to call it from a handler.
// Tests build once with NewEnforcer and call e.Enforce directly.
func EnforceWith(e *casbin.Enforcer, sub, obj, act string) (bool, error) {
	return e.Enforce(sub, obj, act)
}

// Model returns the embedded model text (exposed for tests and tooling).
func Model() string { return modelConf }

// Policy returns the embedded policy text (exposed for tests and tooling).
func Policy() string { return policyCSV }
