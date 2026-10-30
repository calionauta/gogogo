// SCOPE:core — HTTP server construction and listener lifecycle.
package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
)

func serveChiOnListener(ln net.Listener, srv *http.Server, stack *ChiStack) func() {
	go func() {
		if serr := srv.Serve(ln); serr != nil && serr != http.ErrServerClosed {
			slog.Error("server: chi stack stopped", "error", serr)
		}
	}()
	slog.Info("server: chi flat stack listening", "addr", ln.Addr())
	return func() {
		stack.cancelConsumer()
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = srv.Shutdown(ctx)
		for _, stop := range stack.shutdown {
			stop()
		}
		_ = stack.sqlDB.Close()
	}
}
