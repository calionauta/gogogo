// SCOPE:core — DB-backed Casbin policy source (persist.Adapter).
//
// With RBAC_POLICY_SOURCE=db the enforcer loads its rules from the
// casbin_rule table (migration 0009) instead of the embedded policy.csv,
// so an operator edits the policy at runtime. On first use the embedded
// policy seeds the table, so flipping the source does not change any
// decision until a row is inserted or deleted; the shape/coverage tests
// still pin the embedded policy that seeds it.
//
// This is a teaching seam as much as a feature: the adapter methods show
// exactly where Casbin reads and writes policy, which is what a product
// extends for a richer store (a per-tenant table, an audit log, a remote
// service). See docs/rbac.md ("Dynamic permissions").
package rbac

import (
	"context"
	"fmt"
	"strings"

	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
)

// DBAdapter implements Casbin's persist.Adapter over the casbin_rule
// table. Build with NewDBAdapter and pass to NewEnforcerWithAdapter.
type DBAdapter struct {
	q *dbgen.Queries
}

var _ persist.Adapter = (*DBAdapter)(nil)

// NewDBAdapter wraps the sqlc queries as a Casbin adapter.
func NewDBAdapter(q *dbgen.Queries) *DBAdapter { return &DBAdapter{q: q} }

// LoadPolicy reads every row and feeds it to the model. Called by Casbin
// at enforcer construction.
func (a *DBAdapter) LoadPolicy(m model.Model) error {
	rows, err := a.q.ListCasbinRules(context.Background())
	if err != nil {
		return fmt.Errorf("rbac: load db policy: %w", err)
	}
	for _, r := range rows {
		line := ruleLine(r)
		if line == "" {
			continue
		}
		if err := persist.LoadPolicyLine(line, m); err != nil {
			return fmt.Errorf("rbac: parse db rule %q: %w", line, err)
		}
	}
	return nil
}

// SavePolicy rewrites the whole table from the model (clear + insert).
// Casbin's interface method carries no context, so it uses a background
// one; SeedEmbeddedPolicy takes the boot context and calls savePolicy.
func (a *DBAdapter) SavePolicy(m model.Model) error {
	return a.savePolicy(context.Background(), m)
}

func (a *DBAdapter) savePolicy(ctx context.Context, m model.Model) error {
	if err := a.q.DeleteAllCasbinRules(ctx); err != nil {
		return fmt.Errorf("rbac: clear db policy: %w", err)
	}
	for _, sec := range []string{"p", "g"} {
		for ptype, ast := range m[sec] {
			for _, rule := range ast.Policy {
				if err := a.insert(ctx, ptype, rule); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// AddPolicy appends one rule (Casbin's runtime AddPolicy path).
func (a *DBAdapter) AddPolicy(_ string, ptype string, rule []string) error {
	return a.insert(context.Background(), ptype, rule)
}

// RemovePolicy deletes one exact rule.
func (a *DBAdapter) RemovePolicy(_ string, ptype string, rule []string) error {
	v := padRule(rule)
	if err := a.q.DeleteCasbinRule(context.Background(), dbgen.DeleteCasbinRuleParams{
		Ptype: ptype, V0: v[0], V1: v[1], V2: v[2], V3: v[3], V4: v[4], V5: v[5],
	}); err != nil {
		return fmt.Errorf("rbac: delete db rule: %w", err)
	}
	return nil
}

// RemoveFilteredPolicy deletes every rule of ptype whose values match
// fieldValues starting at fieldIndex. Filtered in Go (the table is small;
// runtime mutation is rare), then deleted by row id.
func (a *DBAdapter) RemoveFilteredPolicy(_ string, ptype string, fieldIndex int, fieldValues ...string) error {
	if len(fieldValues) == 0 {
		return nil
	}
	rows, err := a.q.ListCasbinRules(context.Background())
	if err != nil {
		return fmt.Errorf("rbac: list db policy: %w", err)
	}
	for _, r := range rows {
		if r.Ptype != ptype {
			continue
		}
		if fieldMatches(ruleValues(r), fieldIndex, fieldValues) {
			if err := a.q.DeleteCasbinRuleByID(context.Background(), r.ID); err != nil {
				return fmt.Errorf("rbac: delete db rule %d: %w", r.ID, err)
			}
		}
	}
	return nil
}

func (a *DBAdapter) insert(ctx context.Context, ptype string, rule []string) error {
	v := padRule(rule)
	if err := a.q.InsertCasbinRule(ctx, dbgen.InsertCasbinRuleParams{
		Ptype: ptype, V0: v[0], V1: v[1], V2: v[2], V3: v[3], V4: v[4], V5: v[5],
	}); err != nil {
		return fmt.Errorf("rbac: insert db rule: %w", err)
	}
	return nil
}

// SeedEmbeddedPolicy fills an empty casbin_rule table from the embedded
// policy.csv. A non-empty table is left untouched, so a runtime edit is
// never overwritten by a reboot. Idempotent.
func SeedEmbeddedPolicy(ctx context.Context, q *dbgen.Queries) error {
	n, err := q.CountCasbinRules(ctx)
	if err != nil {
		return fmt.Errorf("rbac: count db policy: %w", err)
	}
	if n > 0 {
		return nil
	}
	m, err := newModel()
	if err != nil {
		return err
	}
	if err := loadEmbeddedPolicy(m); err != nil {
		return err
	}
	return NewDBAdapter(q).savePolicy(ctx, m)
}

// ruleLine renders one DB row as a Casbin policy line ("p, admin, /*, .*"),
// trimming trailing empty columns Casbin would reject.
func ruleLine(r dbgen.CasbinRule) string {
	return casbinLine(r.Ptype, r.V0, r.V1, r.V2, r.V3, r.V4, r.V5)
}

func casbinLine(ptype string, vals ...string) string {
	end := len(vals)
	for end > 0 && vals[end-1] == "" {
		end--
	}
	if ptype == "" || end == 0 {
		return ""
	}
	return ptype + ", " + strings.Join(vals[:end], ", ")
}

func padRule(rule []string) [6]string {
	var v [6]string
	copy(v[:], rule)
	return v
}

func ruleValues(r dbgen.CasbinRule) []string {
	return []string{r.V0, r.V1, r.V2, r.V3, r.V4, r.V5}
}

func fieldMatches(vals []string, fieldIndex int, fieldValues []string) bool {
	if fieldIndex < 0 || fieldIndex+len(fieldValues) > len(vals) {
		return false
	}
	for i, fv := range fieldValues {
		if vals[fieldIndex+i] != fv {
			return false
		}
	}
	return true
}
