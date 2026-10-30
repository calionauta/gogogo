// SCOPE:core — Credits engine boot, split from the composition root.
package server

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/credits"
	creditshandlers "github.com/calionauta/gogogo/features/credits/handlers"
	"github.com/calionauta/gogogo/internal/auth"
)

// bootCredits opens the Turso ledger, builds the engine (nil when the
// feature is disabled — routes answer 503), starts its workers and
// registers its shutdown. Split from BootChi to keep both under the
// function-length gate.
func bootCredits(
	ctx context.Context, cfg *config.Config, shutdown []func(),
	sqlDB *sql.DB, cancelConsumer context.CancelFunc, a *auth.Auth,
) (*creditshandlers.Handler, []func(), error) {
	ledgerDB, lerr := creditshandlers.OpenTursoLedger(cfg.DataDir)
	if lerr != nil {
		cancelConsumer()
		_ = sqlDB.Close()
		return nil, shutdown, fmt.Errorf("server: chi ledger: %w", lerr)
	}
	shutdown = append(shutdown, func() { _ = ledgerDB.Close() })
	//nolint:contextcheck // engine constructor takes no context (same as the
	// previous stack's wiring); cancellation starts at Service.Start.
	creditsSvc, cerr := credits.NewOnDB(ledgerDB, cfg)
	if cerr != nil {
		cancelConsumer()
		_ = sqlDB.Close()
		return nil, shutdown, fmt.Errorf("server: chi credits: %w", cerr)
	}
	if creditsSvc == nil {
		return creditshandlers.NewHandler(a, nil), shutdown, nil
	}
	creditsSvc.Start(ctx)
	return creditshandlers.NewHandler(a, creditsSvc), shutdown, nil
}
