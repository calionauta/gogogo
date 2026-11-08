// Shared desktop/gui boot helpers — one bootstrap, every native shell.
//
// cmd/desktop (Wails) and cmd/gui (gogpu/ui) previously duplicated the
// same boot sequence: core + todo migrations, auth.New + TOTP seal key,
// demo seed, plus the desktop-only instance-id / single-instance /
// webview-URL helpers. This package is the single home for that logic;
// shells keep only what is truly shell-specific (opening the window,
// starting NATS, serving HTTP). A native shell (MyGo today, anything
// tomorrow) boots through here instead of copy-pasting the sequence.
package desktopboot

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/todo"
	todohandlers "github.com/calionauta/gogogo/features/todo/handlers"
	"github.com/calionauta/gogogo/internal/auth"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/demo"
	"github.com/calionauta/gogogo/internal/instance"
)

// MigrateAll runs the core ledger migration, then the todo unit's schema.
func MigrateAll(sqlDB *sql.DB) error {
	if err := dbschema.Migrate(sqlDB); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	todoSet := dbschema.MigrationSet{ID: todohandlers.RecipeID, FS: todo.Migrations, Dir: "migrations"}
	if err := dbschema.MigrateWith(sqlDB, todoSet); err != nil {
		return fmt.Errorf("todo migrate: %w", err)
	}
	return nil
}

// NewAuth builds owned auth over sqlDB and wires the TOTP seal key:
// env key wins, malformed AUTH_ENC_KEY fails the boot, else the
// auto-created file, else keyless with enrollment failing closed.
func NewAuth(sqlDB *sql.DB, cfg *config.Config) (*auth.Auth, error) {
	a, err := auth.New(dbgen.New(sqlDB), cfg.Auth.SecureCookies)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	key := cfg.Auth.TOTPEncKey
	if raw := os.Getenv("AUTH_ENC_KEY"); raw != "" && len(key) == 0 {
		return nil, errors.New("auth: malformed AUTH_ENC_KEY (want 64 hex chars)")
	}
	if len(key) == 0 {
		if fk, ferr := auth.LoadOrCreateKeyFile(cfg.DataDir); ferr != nil {
			slog.Warn("totp key file unavailable, TOTP enrollment disabled", "error", ferr)
		} else {
			key = fk[:]
		}
	}
	if len(key) > 0 {
		if err := a.SetTOTPEncKey(key); err != nil {
			return nil, fmt.Errorf("auth: %w", err)
		}
	}
	return a, nil
}

// EnsureDemo seeds the demo accounts exactly as the web stack does.
func EnsureDemo(ctx context.Context, a *auth.Auth, sqlDB *sql.DB) {
	demo.Ensure(ctx, a, dbgen.New(sqlDB))
}

// ScopeInstanceIdentity pins APP_INSTANCE_ID to the desktop profile
// (hash of the DB path) unless the operator set it explicitly. Must run
// before the backend boots: instance.ID() caches on first use.
func ScopeInstanceIdentity(cfg *config.Config) {
	if os.Getenv(instance.EnvVar) != "" {
		return
	}
	sum := sha256.Sum256([]byte(cfg.Turso.DBPath))
	_ = os.Setenv(instance.EnvVar, fmt.Sprintf("%s-desktop-%x", cfg.AppName, sum[:8]))
}

// SingleInstanceID scopes the single-instance lock to app + database, so
// a second profile still opens while a duplicate of the same profile is
// refused.
func SingleInstanceID(appName, dbPath string) string {
	sum := sha256.Sum256([]byte(dbPath))
	return fmt.Sprintf("%s.desktop.%x", appName, sum[:8])
}

// PortOf parses the port out of a bound listener address.
func PortOf(addr string) (int, error) {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, fmt.Errorf("port %q: %w", portStr, err)
	}
	return port, nil
}

// WebviewURL is the loopback URL a webview shell loads. A wildcard listen
// is not a dial target, so it maps to 127.0.0.1 — the same process serves
// there. Works for Wails, MyGo (URL loopback, no frontendDist needed) or
// any WKWebView/WebView2 shell.
func WebviewURL(host string, port int) string {
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/"
}
