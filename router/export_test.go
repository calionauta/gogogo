// SCOPE:layer=feature,removal=plugin — test-only export surface for the router
// package. Go test files are excluded from the SCOPE linter, but this one
// carries the annotation anyway so the file's purpose is obvious at a glance.
//
// Only symbols that a *_test.go in package router_test needs. Nothing here
// ships in the binary: export_test.go is compiled exclusively under `go test`.
package router

import (
	"net/http"
	"os"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
)

// SuppressInstallerForTest exposes suppressInstaller so the installer
// suppression can be asserted with a bare app, without booting a full
// router.Init (which needs a config, queue and handlers).
func SuppressInstallerForTest(app *pocketbase.PocketBase) {
	suppressInstaller(app)
}

// NoBrowserEnvForTest exposes the env var name so a test does not hardcode it.
const NoBrowserEnvForTest = noBrowserEnv

// InteractiveForTest exposes interactive so the non-TTY suppression decision
// can be asserted without booting a server.
func InteractiveForTest() bool {
	return interactive()
}

// IsTerminalForTest exposes isTerminal so the predicate that gates
// browser-launching can be asserted directly.
func IsTerminalForTest(f *os.File) bool {
	return isTerminal(f)
}

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
