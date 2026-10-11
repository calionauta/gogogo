// SCOPE:core — Internal tests for the opt-in RBAC middleware
package router

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

// TestRBACSubjectAnonymous pins the no-session mapping: an unauthenticated
// request must be the "anonymous" subject, never a privileged one. Invert
// the expectation and the test fails.
func TestRBACSubjectAnonymous(t *testing.T) {
	if got := rbacSubject(nil); got != "anonymous" {
		t.Fatalf("rbacSubject(nil) = %q, want %q", got, "anonymous")
	}
	if got := rbacSubject(&core.RequestEvent{}); got != "anonymous" {
		t.Fatalf("rbacSubject(no auth) = %q, want %q", got, "anonymous")
	}
}
