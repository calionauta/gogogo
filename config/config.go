// SCOPE:core - DO NOT REMOVE - Server configuration (env vars, secrets).
//
// ── Env vars (all optional, see Load() for defaults) ──
//
//	PORT                (default: 8080)          — HTTP listen port
//	HOST                (default: "0.0.0.0")     — HTTP listen address
//	ENVIRONMENT         (default: "development") — set to "production" for prod mode
//	APP_NAME            (default: binary name)   — project name (secrets scope)
//	LOG_LEVEL           (default: "INFO")
//	HTTP_ACCESS_LOG     (default: false) — one structured line per request
//	                     (opt-in; a stream logs when it closes)
//	DATA_DIR            (default: "data")
//	APP_URL             (default: "") — public origin for out-of-band links
//	                     (magic links). Required when a magic sender is wired.
//	SIGNUP_ENABLED      (default: true in dev, false in production) — public
//	                     account creation (password + first OAuth login).
//	DEMO_ACCOUNTS_ENABLED (default: true in dev, false in production) — seed
//	                     the published demo1/demo2 credentials.
//	RATE_LIMIT_RPM      (default: 1200) — per-session dynamic requests/minute
//	                     (0 disables the in-process limiter; edge is primary)
//	ADMIN_UNLOCK_TOKEN  (default: "") — master password for admin endpoints
//	GOAI_API_KEY        (default: "") — LLM provider API key
//	GOAI_BASE_URL       (default: "https://api.openai.com/v1") // LLM base URL
//	GOAI_MODEL          (default: "gpt-4o-mini")  — LLM model ID
//	SIMULATE_LLM        (default: "true" in dev) — enable simulated LLM
//	NATS_ENABLED        (default: true)  — enable NATS JetStream
//	NATS_STORE_DIR      (default: "data/nats")
//	NATS_LEAFNODE_URL   (default: "")    — connect as NATS Leaf Node
//	DAGNATS_ENABLED     (default: true)  — enable DagNats workflows
//	GOAKT_ENABLED       (default: true)  — enable GoAkt room actors
//	DAGNATS_HTTP_ADDR   (default: "127.0.0.1:8090")
//	DAGNATS_NATS_PORT   (default: 4222)
//	DAGNATS_GREET_PACING (default: 1500ms) — onboarding-greet demo pause
//	CREDITS_ENABLED     (default: false) — enable ai-credits managed/BYOK billing
//	CREDITS_ENC_KEY     (default: "") — 32 raw bytes or 64 hex chars for BYOK key storage
//	BYOK_PROVIDERS      (default: "") — provider=url pairs for the BYOK relay
//	DAGNATS_STORE_DIR   (default: "data/dagnats")
//	OFFLINE_SYNC_ENABLED (default: true) — toggle hybrid offline sync
//	SEARCH_MODE          (default: "substring") — todo search layer:
//	                       "substring" | "fuzzy" | "bm25"; unknown values
//	                       fall back to substring (see config/search.go)
//	RBAC_ENABLED         (default: false) — opt-in Casbin RBAC middleware;
//	                       enforces the embedded policy in internal/rbac/casbin
//	JEV_API_URL          (default: SimpleJev demo) — jev-compatible judge endpoint
//	JEV_MODEL            (default: demo classifier) — judge model ID
//	JEV_API_KEY          (default: "") — keyless demo when empty (age secrets in prod)
//	JEV_TIMEOUT          (default: 10s) — per-attempt judge HTTP timeout
//	JEV_CONFIDENCE_THRESHOLD (default: 0.7) — escalate to human below this
//
// ── Runtime constants (tune in config.go, consumed across packages) ──
//
//	DefaultReplayBufferSize     (64)  — SSEHub per-client ring-buffer
//	DefaultClientQueueSize      (64)  — SSEHub per-client channel buffer
//	DefaultSSEHeartbeatInterval (15s) — SSE heartbeat ticker interval
package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/calionauta/gogogo/internal/secrets"
)

// GoAIConfig holds the LLM client settings. Currently just an
// APIKey + a model; expanded in internal/llm as more knobs
// (GOAI_BASE_URL, GOAI_MODEL, etc.) are read from env.
type GoAIConfig struct {
	APIKey string
}

// Load builds the Config. Order matters: secrets must be decrypted
// BEFORE reading the rest of the env so admin/LLM/NATS values can
// come from the encrypted file.
// cached is the global config singleton, loaded once.
var (
	cached *Config
	once   sync.Once
)

// BuildTag identifies this binary for staleness checks: tabs open
// across a deploy compare it (via the X-Gogogo-Build header on
// /health) and reload instead of talking to a server whose element IDs
// and signals no longer match their DOM. In dev both parts are static
// ("dev/"), so the check is inert exactly where rebuilds are constant.
func (c *Config) BuildTag() string {
	if c == nil {
		return "dev/"
	}
	return c.BuildLabel + "/" + c.BuildCommit
}

// Get returns the cached config singleton, loading it on first call.
func Get() *Config {
	once.Do(func() {
		cached = Load()
	})
	return cached
}

func Load() *Config {
	appName := os.Getenv("APP_NAME")
	if appName == "" {
		appName = defaultAppName()
	}

	// Decrypt ~/.secrets/<appName>.env.age into the process env. Silent
	// skip when AGE_SECRET_KEY or the secrets file is missing.
	secrets.Load(appName)

	dev := os.Getenv("ENVIRONMENT") != "production"

	port := 8080
	if p := os.Getenv("PORT"); p != "" {
		parsed, err := strconv.Atoi(p)
		if err != nil {
			slog.Warn("config: invalid PORT, using default", "port", p, "default", port, "error", err)
		} else {
			port = parsed
		}
	}

	cfg := &Config{
		AppName:       appName,
		BuildLabel:    getEnv("BUILD_LABEL", "dev"),
		BuildCommit:   getEnv("BUILD_COMMIT", ""),
		Host:          getEnv("HOST", "0.0.0.0"),
		Port:          port,
		LogLevel:      getEnv("LOG_LEVEL", "INFO"),
		HTTPAccessLog: envBool("HTTP_ACCESS_LOG", false),
		Dev:           dev,
		DataDir:       getEnv("DATA_DIR", "data"),
		DemoAccounts:  envBool("DEMO_ACCOUNTS_ENABLED", dev),

		SignupEnabled: envBool("SIGNUP_ENABLED", dev),
		PerfEnabled:   envBool("PERF_ENABLED", dev),
		RateLimitRPM:  int(envInt("RATE_LIMIT_RPM", defaultRateLimitRPM)),
		AdminToken:    os.Getenv("ADMIN_UNLOCK_TOKEN"),
		GoAI: GoAIConfig{
			APIKey: os.Getenv("GOAI_API_KEY"),
		},
	}

	cfg.Auth.AppURL = strings.TrimRight(getEnv("APP_URL", ""), "/")
	cfg.Auth.SecureCookies = !dev
	cfg.LicenseMode = strings.ToLower(getEnv("LICENSE_MODE", LicenseNone))
	cfg.LicensePublicKey = getEnv("LICENSE_PUBLIC_KEY", "")
	cfg.LicenseFile = getEnv("LICENSE_FILE", filepath.Join(cfg.DataDir, "license.token"))
	cfg.LicensePrivateKey = getEnv("LICENSE_PRIVATE_KEY", "")
	cfg.CloudSyncURL = getEnv("CLOUD_SYNC_URL", "")
	cfg.DeviceTokenDays = int(envInt("DEVICE_TOKEN_DAYS", defaultDeviceTokenDays))
	cfg.CloudSyncAllow = splitCSV(getEnv("CLOUD_SYNC_ALLOW", ""))
	cfg.AuthMode = strings.ToLower(getEnv("AUTH_MODE", AuthLocal))
	cfg.AllowOpenRemote = envBool("AUTH_OPEN_ALLOW_REMOTE", false)
	cfg.NATS.Enabled = envBool("NATS_ENABLED", defaultNATSEnabled())
	cfg.NATS.StoreDir = getEnv("NATS_STORE_DIR", "data/nats")
	cfg.NATS.Listen = getEnv("NATS_LISTEN", "")
	cfg.NATS.Token = os.Getenv("NATS_TOKEN")
	cfg.NATS.LeafNodeURL = getEnv("NATS_LEAFNODE_URL", "")

	cfg.DagNats.Enabled = envBool("DAGNATS_ENABLED", defaultDagNatsEnabled())
	cfg.DagNats.HTTPAddr = getEnv("DAGNATS_HTTP_ADDR", "127.0.0.1:8090")
	cfg.DagNats.NATSPort = envInt("DAGNATS_NATS_PORT", defaultDagNatsNATSPort)
	cfg.DagNats.StoreDir = getEnv("DAGNATS_STORE_DIR", "data/dagnats")
	cfg.DagNats.GreetPacing = envDuration("DAGNATS_GREET_PACING", defaultGreetPacing)

	cfg.GoAkt.Enabled = envBool("GOAKT_ENABLED", true)

	cfg.OfflineSync.Enabled = envBool("OFFLINE_SYNC_ENABLED", true)
	cfg.Collab.Snapshots = getEnv("COLLAB_SNAPSHOTS", "turso")
	cfg.loadRBACEnv(dev)
	cfg.loadAuthEnv()
	cfg.loadDeliveryEnv()
	cfg.loadSearchEnv()
	cfg.Turso.URL = getEnv("TURSO_DATABASE_URL", "")
	cfg.Turso.AuthToken = getEnv("TURSO_AUTH_TOKEN", "")
	cfg.Turso.DBPath = getEnv("TURSO_DB_PATH", "data/app.db")
	cfg.ChiAddr = getEnv("CHI_ADDR", "")
	cfg.loadReviewEnv()
	cfg.loadChatRouterEnv()
	cfg.loadGenUIEnv()

	cfg.Credits.Enabled = envBool("CREDITS_ENABLED", false)
	cfg.Credits.DefaultMode = getEnv("CREDITS_DEFAULT_MODE", "explicit")
	cfg.Credits.MonthlyCredits = int64(envInt("CREDITS_MONTHLY_CREDITS", 0))
	cfg.Credits.PricingFile = getEnv("CREDITS_PRICING_FILE", "")
	cfg.Credits.Model = getEnv("CREDITS_MODEL", getEnv("GOAI_MODEL", "gpt-4o-mini"))
	cfg.Credits.ByokProviders = parseProviders(getEnv("BYOK_PROVIDERS", ""))
	if k := os.Getenv("CREDITS_ENC_KEY"); k != "" {
		cfg.Credits.EncKey = parseCreditsEncKey(k)
	}
	cfg.Credits.StripeSecretKey = os.Getenv("STRIPE_SECRET_KEY")
	cfg.Credits.StripeWebhookSecret = os.Getenv("STRIPE_WEBHOOK_SECRET")
	cfg.Credits.StripeSuccessURL = getEnv("STRIPE_SUCCESS_URL", "http://localhost:3000/credits?status=success")
	cfg.Credits.StripeCancelURL = getEnv("STRIPE_CANCEL_URL", "http://localhost:3000/credits?status=cancelled")

	return cfg
}
