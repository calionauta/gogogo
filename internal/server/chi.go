// SCOPE:core — Flat-stack chi server (staged-migration seam)
//
// StartChi serves the limen-auth + sqlc-todo chi router alongside
// PocketBase while the migration is staged. It is a no-op unless both
// CHI_ADDR and LIMEN_SECRET are set, so the default boot behaves exactly
// as before. Removed at step 7, when the chi router becomes the server.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/todo/chi"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/queue"
)

// shutdownTimeout bounds chi-stack shutdown: in-flight SSE streams get a
// few seconds to finish before the process exits.
const shutdownTimeout = 5 * time.Second

// StartChi boots the flat stack and serves it in the background. The
// returned func stops the server; callers defer it. A nil return with a
// nil error means "seam disabled" (no CHI_ADDR or no LIMEN_SECRET) —
// logged, never fatal, so a stock env never breaks.
func StartChi(cfg *config.Config) (stop func(), err error) {
	if cfg.ChiAddr == "" {
		return nil, nil
	}
	if len(cfg.AuthSecret) != auth.SecretLen {
		slog.Warn("server: CHI_ADDR set but LIMEN_SECRET is not 32 bytes; chi stack disabled")
		return nil, nil
	}

	sqlDB, err := db.Open(cfg)
	if err != nil {
		return nil, fmt.Errorf("server: chi db open: %w", err)
	}
	if err := db.Migrate(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("server: chi db migrate: %w", err)
	}

	a, aerr := auth.New(sqlDB, cfg.AuthSecret, !cfg.Dev)
	if aerr != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("server: chi auth: %w", aerr)
	}

	hub := queue.NewSSEHub()
	todoH := chi.NewHandler(dbgen.New(sqlDB), a, hub, cfg.Skin)

	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Handler())
	mux.Handle("/api/todos", todoH.Routes())
	mux.Handle("/api/todos/", todoH.Routes())

	srv := &http.Server{
		Addr:              cfg.ChiAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if serr := srv.ListenAndServe(); serr != nil && serr != http.ErrServerClosed {
			slog.Error("server: chi stack stopped", "error", serr)
		}
	}()
	slog.Info("server: chi flat stack listening", "addr", cfg.ChiAddr)
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = srv.Shutdown(ctx)
		_ = sqlDB.Close()
	}, nil
}
