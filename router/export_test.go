// SCOPE:layer=feature,removal=plugin — test-only export surface for the router
// package. Go test files are excluded from the SCOPE linter, but this one
// carries the annotation anyway so the file's purpose is obvious at a glance.
//
// Only symbols that a *_test.go in package router_test needs. Nothing here
// ships in the binary: export_test.go is compiled exclusively under `go test`.
package router

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// MountDagNatsDashboardForTest exposes mountDagNatsDashboard so the proxy's
// routing can be asserted from an external test package. The function is
// unexported because it is wiring detail, not API.
func MountDagNatsDashboardForTest(se *core.ServeEvent, upstream string) {
	mountDagNatsDashboard(se, upstream)
}

// RewriteDagNatsPathsForTest exposes the response-body rewriter so its
// Content-Length contract (header must equal the rewritten byte length) can be
// asserted directly.
func RewriteDagNatsPathsForTest(resp *http.Response) error {
	return rewriteDagNatsPaths(resp)
}
