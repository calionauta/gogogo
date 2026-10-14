// SCOPE:core — RBAC with Casbin (embedded model + policy, or a DB source)
//
// The Casbin model (.conf) is compiled into the binary with //go:embed.
// The default policy (.csv) is embedded too, so a clone carries its own
// authorization rules with no config file to read. With
// RBAC_POLICY_SOURCE=db the rules come from the casbin_rule table instead
// (see adapter.go), so an operator can edit the policy at runtime.
//
// The model matcher names two functions registered in dynamic.go
// (pathMatch, attrOK): they accept a bare path string OR an ABAC Object,
// so the same model serves the string hot path and per-record decisions.
//
// Authoring guide: docs/rbac.md (lifecycle, file map, row anatomy,
// checklists, dynamic permissions). Role vocabulary lives in roles.go;
// every policy invariant an LLM can break is pinned by a red-proof test
// in rbac_test.go (shape, flat, dedupe, coverage, viewer matrix)
// enforced in CI and by the rbac-policy pre-commit hook. There is no
// upstream Casbin policy linter for Go (only the online editor and
// load-time parse errors), so those tests ARE this repo's Casbin lint.
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

// newModel parses the embedded model text.
func newModel() (model.Model, error) {
	m, err := model.NewModelFromString(modelConf)
	if err != nil {
		return nil, fmt.Errorf("rbac: parse embedded model: %w", err)
	}
	return m, nil
}

// loadEmbeddedPolicy fills a model from the embedded policy.csv. A
// malformed policy line must fail closed: ignoring it would leave the
// enforcer running with fewer rules than the operator wrote.
func loadEmbeddedPolicy(m model.Model) error {
	lineNo := 0
	for line := range strings.SplitSeq(policyCSV, "\n") {
		lineNo++
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if perr := persist.LoadPolicyLine(line, m); perr != nil {
			return fmt.Errorf("rbac: parse embedded policy line %d: %w", lineNo, perr)
		}
	}
	return nil
}

// NewEnforcer builds a Casbin enforcer from the embedded model and policy.
// No adapter and no filesystem access: the binary carries its own rules.
// The dynamic matcher functions (pathMatch, attrOK) are registered here,
// so every enforcer enforces the same model.
func NewEnforcer() (*casbin.Enforcer, error) {
	m, err := newModel()
	if err != nil {
		return nil, err
	}
	if err := loadEmbeddedPolicy(m); err != nil {
		return nil, err
	}
	return finishEnforcer(m)
}

// NewEnforcerWithAdapter builds the enforcer from a persist.Adapter — the
// DB-backed policy source (internal/rbac/adapter.go). Same model, same
// dynamic functions; only the rule source differs.
func NewEnforcerWithAdapter(ad persist.Adapter) (*casbin.Enforcer, error) {
	m, err := newModel()
	if err != nil {
		return nil, err
	}
	return finishEnforcer(m, ad)
}

// finishEnforcer builds the enforcer (optionally with an adapter that
// loads the policy) and registers the dynamic matcher functions.
func finishEnforcer(m model.Model, ad ...persist.Adapter) (*casbin.Enforcer, error) {
	params := make([]any, 0, 1+len(ad))
	params = append(params, m)
	for _, a := range ad {
		params = append(params, a)
	}
	e, err := casbin.NewEnforcer(params...)
	if err != nil {
		return nil, fmt.Errorf("rbac: new enforcer: %w", err)
	}
	RegisterDynamic(e)
	return e, nil
}

// EnforceWith runs the decision against an already-built enforcer — the hot
// path for a request middleware, which builds once and reuses. obj is a
// path string or an Object (ABAC); the matcher's pathMatch/attrOK accept
// both. There is deliberately no per-call Enforce(sub, obj, act) helper
// that builds an enforcer internally: parsing model + policy per request
// is pure waste, and its presence invites the next LLM to call it from a
// handler. Tests build once with NewEnforcer and call e.Enforce directly.
func EnforceWith(e *casbin.Enforcer, sub string, obj any, act string) (bool, error) {
	return e.Enforce(sub, obj, act)
}

// Model returns the embedded model text (exposed for tests and tooling).
func Model() string { return modelConf }

// Policy returns the embedded policy text (exposed for tests and tooling).
func Policy() string { return policyCSV }
