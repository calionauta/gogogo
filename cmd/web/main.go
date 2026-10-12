// SCOPE:core - DO NOT REMOVE - Server entry point. This is the main binary.
// Package main wires the binary's startup sequence: the flat chi stack
// (internal/server), the goqite-backed chat/genui queues it owns, optional
// NATS JetStream realtime, and the DagNats durable workflow engine (always
// compiled; opt out at runtime via DAGNATS_ENABLED).
//
// Boot order is a graph, not a line: BootChi builds everything engine-free
// (and returns the todo handler the onboarding worker writes through),
// DagNats then owns the embedded NATS :4222, startNATS attaches to it, and
// AttachJetStream + Serve finish the stack. run() blocks until SIGINT or
// SIGTERM, so every deferred cleanup runs in reverse order on the way out.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/server"
)

// Build metadata — overwritten at build time via LDFLAGS
// (-ldflags="-X main.Version=... -X main.CommitHash=... -X main.BuildTime=...").
// Surfaced on the navbar version badge so a tester can verify which binary
// is running by visual inspection. Defaults to "dev" / "unknown" / "" so a
// `go run` build never claims to be a tagged release.
var (
	Version    = "dev"
	CommitHash = "unknown"
	BuildTime  = ""
)

// ShortCommit returns the first 7 chars of CommitHash for compact display.
func ShortCommit() string {
	const shortHashLen = 7
	if len(CommitHash) >= shortHashLen {
		return CommitHash[:7]
	}
	return CommitHash
}

// listenAddr resolves the flat-stack listen address: the ChiAddr
// override when set, Host:Port otherwise. Pure so the contract is
// unit-testable; both callers (here and, via StartChi, cmd/desktop)
// must agree on what "the address" means.
func listenAddr(cfg *config.Config) string {
	if cfg.ChiAddr != "" {
		return cfg.ChiAddr
	}
	return fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
}

func main() {
	// `health` is a scratch-compatible healthcheck subcommand: it does an
	// internal GET to /health and exits 0 on 200, 1 otherwise. The image
	// is scratch (no shell, no wget/curl), so the compose HEALTHCHECK uses
	// `CMD ["/app", "health"]` (exec form) rather than a shell command.
	if len(os.Args) > 1 && os.Args[1] == "health" {
		if err := runHealthcheck(); err != nil {
			log.Fatalf("healthcheck failed: %v", err)
		}
		return
	}
	if err := run(); err != nil {
		log.Fatalf("startup failed: %v", err)
	}
}

func runHealthcheck() error {
	cfg := config.Get()
	cfg.BuildLabel = Version
	cfg.BuildCommit = ShortCommit()
	url := fmt.Sprintf("http://127.0.0.1:%d/health", cfg.Port)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil) // #nosec G107
	if err != nil {
		return fmt.Errorf("build healthcheck request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	return nil
}

func run() error {
	cfg := config.Get()
	cfg.BuildLabel = Version
	cfg.BuildCommit = ShortCommit()

	// Boot the engine-free stack first: it owns the todo handler the
	// DagNats onboarding worker writes through, and DagNats must be up
	// before JetStream can exist (it owns the embedded NATS).
	stack, err := server.BootChi(cfg)
	if err != nil {
		return err
	}

	// lifecycleCtx bounds background boot work (workflow registration, DagNats
	// watchdogs) to the process lifetime: a shutdown cancels it, so a retry loop
	// stops promptly instead of burning its own timeouts.
	lifecycleCtx, stopLifecycle := context.WithCancel(context.Background())
	defer stopLifecycle()

	// DagNats owns the embedded NATS on :4222 and must boot before
	// startNATS so the realtime broadcaster can attach to it. It's always
	// compiled; when DAGNATS_ENABLED=false it no-ops.
	startDagNats(lifecycleCtx, cfg, stack.TodoHandler)
	defer shutdownDagNats()

	// GoAkt owns the room actor system (standalone, no network). It's
	// always compiled; when GOAKT_ENABLED=false it no-ops.
	// NOTE: intentionally NOT lifecycleCtx-bound (unlike DagNats): the
	// standalone boot performs no retries and returns promptly, so there
	// is no loop for shutdown to bound — and a self-contained span keeps
	// `add goakt` position-independent (any anchor works).
	startGoAkt(context.Background(), cfg)
	defer shutdownGoAkt()

	js := startNATS(cfg)

	stack.AttachJetStream(lifecycleCtx, js)

	// ChiAddr, when set, overrides Host:Port (same override cmd/desktop
	// uses for its webview proxy target). Empty keeps the default, so a
	// stock env never changes behaviour.
	addr := listenAddr(cfg)
	stop, err := stack.Serve(addr)
	if err != nil {
		return err
	}
	defer stop()
	log.Printf("listening on %s", addr)

	// WORKAROUND (upstream DagNats v0.0.24 bug): the trigger console cannot
	// create the FIRST trigger because listing an empty bucket returns
	// ErrNoKeysFound, which it reports as a 500 before writing anything.
	// Seeding one disabled placeholder makes the bucket non-empty, so the
	// console works. Runs after startNATS because that is what connects to
	// the engine's NATS. Never fatal: a failure here means the trigger UI
	// stays broken, not that the app is unusable. Set
	// DAGNATS_TRIGGER_BOOTSTRAP=false to skip, and see
	// docs/dagnats-bootstrap-workaround.md for removal steps.
	if cfg.DagNats.Enabled && cfg.DagNats.TriggerBootstrap {
		ensureTriggerBootstrap()
	}

	// The process is the server now (there is no framework run loop
	// underneath): block on signals so the deferred cleanups above —
	// stop() draining HTTP, engine stops, lifecycle cancel — run in
	// reverse order instead of being skipped at process exit.
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	<-ctx.Done()
	log.Printf("shutting down")
	return nil
}
