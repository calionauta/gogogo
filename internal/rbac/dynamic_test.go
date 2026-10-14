// SCOPE:core — Dynamic RBAC tests: ABAC ownership + DB-backed policy.
//
// These pin the two dynamic inputs added in dynamic.go / adapter.go:
// the ownership clause that reads an Object's attributes, and the
// RBAC_POLICY_SOURCE=db path that loads (and seeds) the casbin_rule
// table. The static policy invariants stay pinned in rbac_test.go.
package rbac

import (
	"context"
	"testing"

	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

func openRBACDB(t *testing.T) *dbgen.Queries {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/rbac-dyn.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("db migrate: %v", merr)
	}
	return dbgen.New(sqlDB)
}

// TestRBACABACOwnership pins the dynamic ownership clause: once an Object
// carries an owner, a plain user reaches the record only as its owner;
// admin reaches anything; an ownerless Object is unaffected. Red-proof:
// make attrsOK always return true and the deny cases go red.
func TestRBACABACOwnership(t *testing.T) {
	e, err := NewEnforcer()
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	cases := []struct {
		name string
		sub  string
		obj  Object
		want bool
	}{
		{"owner reaches own record", RoleUser, Object{Path: "/api/todos/abc", SubjectID: "u1", Owner: "u1"}, true},
		{"user denied another's record", RoleUser, Object{Path: "/api/todos/abc", SubjectID: "u1", Owner: "u2"}, false},
		{"admin reaches any record", RoleAdmin, Object{Path: "/api/todos/abc", SubjectID: "u1", Owner: "u2"}, true},
		{"ownerless object keeps the base decision", RoleUser, Object{Path: "/api/todos", SubjectID: "u1"}, true},
	}
	for _, c := range cases {
		got, gerr := EnforceWith(e, c.sub, c.obj, "GET")
		if gerr != nil {
			t.Fatalf("%s: %v", c.name, gerr)
		}
		if got != c.want {
			t.Errorf("%s: Enforce(%s) = %v, want %v", c.name, c.sub, got, c.want)
		}
	}
	// A bare path string is the same as an ownerless Object: string
	// callers (tests, the middleware default) keep identical decisions.
	ok, serr := EnforceWith(e, RoleUser, "/api/todos/abc", "GET")
	if serr != nil || !ok {
		t.Fatalf("bare string path must keep the base allow: ok=%v err=%v", ok, serr)
	}
}

// TestRBACDBPolicySource pins the DB-backed source end to end: seeding an
// empty table from the embedded policy is idempotent, the DB enforcer
// decides exactly like the embedded one, and a row inserted at runtime
// changes the decision on the next build — the "policy is data now"
// payoff. Red-proof: make LoadPolicy skip rows and the new grant fails.
func TestRBACDBPolicySource(t *testing.T) {
	q := openRBACDB(t)
	ctx := context.Background()
	if err := SeedEmbeddedPolicy(ctx, q); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Idempotent: a second seed must not duplicate the rules.
	n1, err := q.CountCasbinRules(context.Background())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if serr := SeedEmbeddedPolicy(ctx, q); serr != nil {
		t.Fatalf("seed twice: %v", serr)
	}
	n2, err := q.CountCasbinRules(context.Background())
	if err != nil {
		t.Fatalf("count twice: %v", err)
	}
	if n1 != n2 {
		t.Errorf("seed not idempotent: %d rows, then %d", n1, n2)
	}

	embedded, err := NewEnforcer()
	if err != nil {
		t.Fatalf("embedded enforcer: %v", err)
	}
	fromDB, err := NewEnforcerWithAdapter(NewDBAdapter(q))
	if err != nil {
		t.Fatalf("NewEnforcerWithAdapter: %v", err)
	}
	// Seeded with the embedded policy, the two must decide identically.
	for _, c := range []struct{ sub, obj, act string }{
		{RoleUser, "/todo", "GET"},
		{RoleUser, "/config", "GET"},
		{RoleAdmin, "/config", "GET"},
		{RoleAnonymous, "/todo", "GET"},
		{RoleViewer, "/api/todos", "GET"},
		{RoleViewer, "/api/todos/abc/delete", "POST"},
	} {
		want, werr := EnforceWith(embedded, c.sub, c.obj, c.act)
		if werr != nil {
			t.Fatalf("embedded enforce %v: %v", c, werr)
		}
		got, gerr := EnforceWith(fromDB, c.sub, c.obj, c.act)
		if gerr != nil {
			t.Fatalf("db enforce %v: %v", c, gerr)
		}
		if got != want {
			t.Errorf("db source Enforce(%v) = %v, embedded = %v", c, got, want)
		}
	}

	// Runtime edit: a new rule in the table takes effect on the next build.
	if ierr := q.InsertCasbinRule(context.Background(), dbgen.InsertCasbinRuleParams{
		Ptype: "p", V0: RoleUser, V1: "/reports", V2: "GET",
	}); ierr != nil {
		t.Fatalf("insert rule: %v", ierr)
	}
	rebuilt, err := NewEnforcerWithAdapter(NewDBAdapter(q))
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if ok, eerr := EnforceWith(rebuilt, RoleUser, "/reports", "GET"); eerr != nil || !ok {
		t.Errorf("runtime row must grant /reports GET to user: ok=%v err=%v", ok, eerr)
	}
	if base, _ := EnforceWith(embedded, RoleUser, "/reports", "GET"); base {
		t.Error("premise broken: the embedded policy already allowed /reports")
	}
}
