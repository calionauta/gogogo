// SCOPE:layer=feature,removal=plugin — Wire the AI credits + BYOK feature
// into the router. To remove: delete this file + the block in router.go that
// calls registerCredits + delete features/credits/ + config Credits fields
// + go.mod require/replace.
//
// registerCredits builds the credits service from config and returns it so
// router.Init can RegisterRoutes. Returns nil (and no-ops) when
// CREDITS_ENABLED=false or the lib fails to init — the binary boots
// billing-free either way.
package router

import (
	"context"
	"log/slog"

	"github.com/pocketbase/pocketbase/core"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/credits"
	"github.com/calionauta/gogogo/features/todo/handlers"
)

func registerCredits(cfg *config.Config) *credits.Service {
	if !cfg.Credits.Enabled {
		return nil
	}
	svc, err := credits.New(cfg)
	if err != nil {
		slog.Error("credits: init disabled", "err", err)
		return nil
	}
	return svc
}

// registerCreditsRoutes builds the credits service (if enabled), mounts its
// routes over the router, and returns the service so caller-init can wire the
// llm Biller onto the todo suggest worker. Returns nil when disabled/failed.
func registerCreditsRoutes(cfg *config.Config, se *core.ServeEvent) *credits.Service {
	svc := registerCredits(cfg)
	if svc == nil {
		return nil
	}
	svc.RegisterRoutes(se)

	// Background workers (payments retry + settlement outbox) are bound to a
	// context cancelled on app termination, so a server shutdown stops them
	// instead of leaking goroutines that outlive the process tree.
	ctx, cancel := context.WithCancel(context.Background())
	se.App.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
		cancel()
		return e.Next()
	})
	svc.Start(ctx)

	return svc
}

// wireCredits mounts the credits routes (if enabled) and, when the service
// exists, installs it as the llm.Biller on the todo Client(s) so the real
// Suggest LLM calls are metered. Extracted from router.Init to keep Init's
// cognitive complexity under the ceiling. Delete with features/credits/ +
// router/credits.go: drop the wireCredits call line from Init.
func wireCredits(cfg *config.Config, se *core.ServeEvent, todoH *handlers.TodoHandler) {
	svc := registerCreditsRoutes(cfg, se)
	if svc == nil || todoH == nil {
		return
	}
	todoH.SetLLMMeter(svc)
}
