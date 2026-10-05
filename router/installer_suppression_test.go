// SCOPE:layer=infra,removal=core — tests that the PocketBase first-run
// installer can be suppressed in non-interactive environments.
package router_test

import (
	"os"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"

	approuter "github.com/calionauta/gogogo/router"
)

// TestSuppressInstaller_ClearsInstallerFunc is the regression guard for the
// browser tab that popped open on every live-server test run.
//
// PocketBase's default installer (apis.DefaultInstallerFunc) mints a pbinstall
// token and calls osutils.LaunchURL — it OPENS A BROWSER on the machine
// running the process. A test boots the real binary with a throwaway
// DATA_DIR, so the installer fired every time and the tab raced the listener
// ("127.0.0.1 refused to connect"). suppressInstaller clears
// ServeEvent.InstallerFunc, which apis/serve.go checks before LaunchURL.
//
// Red-proof: delete the body of suppressInstaller and InstallerFunc stays
// non-nil here.
func TestSuppressInstaller_ClearsInstallerFunc(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:  t.TempDir(),
		HideStartBanner: true,
	})
	approuter.SuppressInstallerForTest(app)

	se := &core.ServeEvent{App: app}
	// Mirror apis/serve.go: the default is assigned BEFORE the hook fires.
	se.InstallerFunc = func(core.App, *core.Record, string) error { return nil }

	if err := app.OnServe().Trigger(se, func(e *core.ServeEvent) error { return e.Next() }); err != nil {
		t.Fatalf("trigger OnServe: %v", err)
	}

	if se.InstallerFunc != nil {
		t.Fatal("suppressInstaller did not clear InstallerFunc — the installer would open a browser")
	}
}

// TestNoBrowserEnv_IsTheDocumentedKey ties the constant to the env var the
// live-server test sets, so a rename cannot silently split them.
func TestNoBrowserEnv_IsTheDocumentedKey(t *testing.T) {
	if approuter.NoBrowserEnvForTest != "GOGOGO_NO_BROWSER" {
		t.Fatalf("noBrowserEnv = %q, want GOGOGO_NO_BROWSER", approuter.NoBrowserEnvForTest)
	}
	os.Unsetenv(approuter.NoBrowserEnvForTest)
}
