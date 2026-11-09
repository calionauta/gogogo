// The credits slice mounts itself through the recipe seam.
package handlers

import (
	"context"

	"github.com/calionauta/gogogo/features/credits"
	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/recipe/register"
)

// Register builds the credits handler: ledger DB + engine (nil when the
// feature is disabled — routes answer 503), workers, and shutdown. Ledger
// errors degrade to the nil-engine path only on construction failure of
// the engine; ledger open failure is a boot error this function cannot
// return, so it panics through NewHandler's dependency check shape instead
// of silently dropping billing.
func Register(reg *recipe.Registry, d register.Deps) {
	ledgerDB, err := OpenTursoLedger(d.Cfg.DataDir)
	if err != nil {
		panic("credits: ledger open: " + err.Error())
	}
	if d.OnShutdown != nil {
		d.OnShutdown(func() { _ = ledgerDB.Close() })
	}
	svc, err := credits.NewOnDB(ledgerDB, d.Cfg)
	if err != nil {
		panic("credits: engine: " + err.Error())
	}
	if svc != nil {
		svc.Start(context.Background())
	}
	reg.Add(NewHandler(d.Auth, svc).Recipe())
}
