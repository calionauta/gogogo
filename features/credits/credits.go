// SCOPE:layer=feature,removal=plugin — AI credits + BYOK (lib ai-credits).
// To remove: delete features/credits/ + router/credits.go + config fields.
//
// Wires the calionauta/ai-credits library (immutable ledger + pricing +
// reservations + monthly grants + reconcile + BYOK relay) into the app's
// PocketBase SQLite. The lib takes a *sql.DB; the app exposes neither via
// app.DB() (it returns an opaque dbx.Builder hiding the concrete *sql.DB),
// so — like the goqite queue (internal/queue) — we open a second connection
// to the same DB file. WAL + busy_timeout make cross-connection writes safe.
// Billing is fully opt-in: when CREDITS_ENABLED=false the plugin builds
// nothing and registers no routes (see router/credits.go).
package credits

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/calionauta/ai-credits/credits"
	paymentcore "github.com/calionauta/ai-credits/payments"
	stripecredits "github.com/calionauta/ai-credits/stripe"

	"github.com/calionauta/gogogo/config"
)

// Service bundles the credits engine plus the app-facing deps handlers need.
type Service struct {
	Credits  *credits.Service
	Store    *credits.CredentialStore
	Relay    *credits.ByokRelay
	Cfg      *config.CreditsConfig
	Model    string // default managed model id
	AppKey   string // app-level GOAI_API_KEY for managed mode
	BaseURL  string // app-level GOAI_BASE_URL for managed mode
	Now      func() time.Time
	Payments *paymentcore.Service
	Stripe   *stripecredits.Adapter

	// started guards Start against double-launch; the workers are
	// long-lived and must be started exactly once per process.
	started sync.Once
}

// New builds the credits engine on the app's SQLite DB file (a fresh
// connection to cfg.DBPath), applies the lib-owned schema idempotently, and
// constructs the BYOK credential store + relay. Returns (nil, nil) when the
// feature is disabled — handlers then no-op and the binary boots
// billing-free.
func New(cfg *config.Config) (*Service, error) {
	if !cfg.Credits.Enabled {
		return nil, nil
	}
	// The credits ledger lives in the SAME SQLite file PocketBase uses
	// (default DataDir/data.db) so users and balances share one atomic
	// backup; the lib opens its own connection (the PB builder is opaque).
	dbPath := cfg.DBPath
	if dbPath == "" {
		dbPath = "data/data.db"
	}
	db, err := credits.OpenSQLite("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}
	svc, err := credits.New(db, credits.Config{
		DefaultBillingMode: cfg.Credits.DefaultMode,
		MonthlyCredits:     cfg.Credits.MonthlyCredits,
		ReservationTimeout: defaultReservationTimeout,
		PricingReader:      pricingReader(cfg.Credits.PricingFile),
		Now:                time.Now,
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if err2 := svc.EnsureSchema(context.Background()); err2 != nil {
		_ = db.Close()
		return nil, err2
	}

	var store *credits.CredentialStore
	switch {
	case len(cfg.Credits.EncKey) == keyLen32:
		var k [keyLen32]byte
		copy(k[:], cfg.Credits.EncKey)
		store = svc.NewCredentialStore(k)
	case len(cfg.Credits.ByokProviders) == 0:
		// No BYOK relays requested → the enc key is simply not needed.
		slog.Debug("credits: BYOK store not created (no CREDITS_ENC_KEY, no ByokProviders)")
	default:
		// Billing enabled AND the operator requested BYOK providers, but
		// CREDITS_ENC_KEY isn't a 32-byte key — the relay would silently
		// 404. Fail closed at boot instead of shipping dead routes.
		return nil, fmt.Errorf(
			"credits: CREDITS_ENC_KEY must be 32 bytes to serve BYOK providers %v",
			cfg.Credits.ByokProviders)
	}

	var relay *credits.ByokRelay
	if store != nil {
		relay = svc.NewByokRelay(store, cfg.Credits.ByokProviders, slog.Default())
	}

	catalog := map[string]paymentcore.CatalogItem{
		"topup-small": {Credits: 500, Currency: "usd", AmountMinor: 500},
		"topup-large": {Credits: 2500, Currency: "usd", AmountMinor: 2000},
	}
	paymentSvc, err := paymentcore.New(db, svc, catalog)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	var stripeSvc *stripecredits.Adapter
	if cfg.Credits.StripeSecretKey != "" && cfg.Credits.StripeWebhookSecret != "" {
		stripeSvc, err = stripecredits.New(paymentSvc, stripecredits.Config{
			SecretKey: cfg.Credits.StripeSecretKey, WebhookSecret: cfg.Credits.StripeWebhookSecret,
			SuccessURL: cfg.Credits.StripeSuccessURL, CancelURL: cfg.Credits.StripeCancelURL,
		})
		if err != nil {
			_ = db.Close()
			return nil, err
		}
	}

	svcOut := &Service{
		Credits:  svc,
		Store:    store,
		Relay:    relay,
		Cfg:      &cfg.Credits,
		Model:    cfg.Credits.Model,
		AppKey:   cfg.GoAI.APIKey,
		BaseURL:  defaultBaseURL,
		Now:      time.Now,
		Payments: paymentSvc,
		Stripe:   stripeSvc,
	}
	return svcOut, nil
}

// Start launches the background workers (payments retry + settlement outbox).
// It is deliberately separate from New so the CALLER owns the lifetime: the
// router passes a context cancelled by the app's OnTerminate hook, so shutdown
// stops both loops instead of leaking goroutines bound to context.Background().
//
// Both workers honour ctx cancellation — payments.Worker.Run selects on
// ctx.Done(), and the settlement ticker below does the same. Safe to call more
// than once; only the first call starts the workers.
func (s *Service) Start(ctx context.Context) {
	s.started.Do(func() {
		if s.Payments != nil {
			go func() {
				w := paymentcore.NewWorker(s.Payments,
					paymentcore.WorkerConfig{Interval: 10 * time.Second})
				w.Run(ctx)
			}()
		}
		if s.Credits != nil {
			go s.runSettlementOutbox(ctx)
		}
	})
}

// settlementInterval is the cadence of the settlement-outbox drain. Tests pass
// their own interval to settlementLoop, so this stays a const (no mutable
// package global).
const settlementInterval = time.Minute

// runSettlementOutbox drains the settlement outbox on a ticker until ctx is
// cancelled. Split from Start so tests can drive the exact production loop.
func (s *Service) runSettlementOutbox(ctx context.Context) {
	settlementLoop(ctx, settlementInterval, func(ctx context.Context) {
		_ = s.Credits.ProcessSettlementOutbox(ctx, 100)
	})
}

// settlementLoop runs tick every interval until ctx is cancelled, calling
// drain on each tick. The select on ctx.Done() is what makes shutdown prompt;
// without it the goroutine outlives the process that started it. Kept free of
// Service fields so testing/synctest can drive it with virtual time.
func settlementLoop(ctx context.Context, tick time.Duration, drain func(context.Context)) {
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			drain(ctx)
		}
	}
}

// defaultBaseURL matches the internal/llm default when GOAI_BASE_URL is unset.
const defaultBaseURL = "https://api.openai.com/v1"

// keyLen32 is the XChaCha20-Poly1305 key size (32 bytes) the BYOK credential
// store requires.
const keyLen32 = 32

// defaultReservationTimeout is the lib default (5m) mirrored here so the
// plugin is self-documenting without coupling to lib internals.
const defaultReservationTimeout = 5 * time.Minute

// pricingReader returns the configured pricing JSON reader, or nil for the
// lib's built-in defaults.
func pricingReader(path string) io.Reader {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		slog.Warn("credits: open pricing file", "path", path, "err", err)
		return nil
	}
	return f
}
