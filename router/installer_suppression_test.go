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

// TestInteractive_NonTTYSuppresses is the regression guard for the second half
// of the browser-tab bug: the env var alone was not enough, because two
// harnesses (cmd/web/smoke_test.go and scripts/smoke.mjs) spawned the binary
// without setting it. `go test` runs with a non-TTY stdin/stdout, so
// interactive() must report false here without any env var being set — which
// is exactly the condition under which a spawned test binary must not open a
// browser.
//
// Red-proof: invert the final return of interactive() (report true
// unconditionally) and this test fails on the `interactive()` assertion.
func TestInteractive_NonTTYSuppresses(t *testing.T) {
	t.Setenv(approuter.NoBrowserEnvForTest, "")
	if approuter.InteractiveForTest() {
		t.Fatal("interactive() = true under `go test` (non-TTY); a spawned test binary would open a browser")
	}
}

// TestInteractive_EnvOverride pins both explicit overrides so a refactor that
// drops them is caught.
func TestInteractive_EnvOverride(t *testing.T) {
	t.Setenv(approuter.NoBrowserEnvForTest, "1")
	if approuter.InteractiveForTest() {
		t.Fatal("GOGOGO_NO_BROWSER=1 must force non-interactive")
	}
	t.Setenv(approuter.NoBrowserEnvForTest, "0")
	if !approuter.InteractiveForTest() {
		t.Fatal("GOGOGO_NO_BROWSER=0 must force interactive (escape hatch for PTY-less automation)")
	}
}

// TestIsTerminal_NonTerminalsAreRejected guards the predicate itself. A regular
// file and /dev/null must both be rejected: /dev/null in particular is a
// character device, so a bare mode-bit check would call it a terminal and let a
// background daemon with `< /dev/null` open a browser.
func TestIsTerminal_NonTerminalsAreRejected(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "notatty")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer f.Close()
	if approuter.IsTerminalForTest(f) {
		t.Fatal("a regular file must not be reported as a terminal")
	}

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer devNull.Close()
	if approuter.IsTerminalForTest(devNull) {
		t.Fatalf("%s is a character device but NOT a terminal; treating it as one\n"+
			"lets a background daemon open a browser", os.DevNull)
	}
}
