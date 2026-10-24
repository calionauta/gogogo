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
//	UI_SKIN              (default: "daisyui") — active UI skin (one ships; see web/skins/)
//	                       (see web/skins/)
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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/calionauta/gogogo/internal/secrets"
)

type Config struct {
	// AppName is used as the scope for the secrets file
	// (~/.secrets/<AppName>.env.age) and the project name in logs.
	// Derived from APP_NAME env or the binary name; never empty.
	AppName string

	// BuildLabel is the human-readable build identifier (e.g.
	// "v0.21.0" or "dev"). Surfaced on the navbar version badge so
	// a tester can verify which binary is running by visual
	// inspection. Set via BUILD_LABEL env var (overwritten by the
	// Makefile via -ldflags="-X main.Version" at build time).
	BuildLabel string

	// BuildCommit is the short git commit hash. Surfaced alongside
	// BuildLabel on the version badge. Set via BUILD_COMMIT env var
	// (overwritten by -ldflags="-X main.CommitHash").
	BuildCommit string

	Host     string
	Port     int
	LogLevel string
	Dev      bool

	// HTTPAccessLog emits one structured line per request at the edge.
	// Opt-in: a busy SSE page logs a line per stream close, which is
	// signal in production and noise while developing.
	HTTPAccessLog bool

	DataDir string

	// AppURL is the public origin used to build links that leave the
	// process (the magic-link email). It must be the browser-facing URL,
	// not the listen address: behind a TLS-terminating proxy the request
	// Host is either http or an attacker-supplied value, so auth refuses
	// to build a magic link without this. Empty disables magic links.
	AppURL string

	// DemoAccounts seeds demo1/demo2 with published credentials and fixed
	// roles. It defaults to development only: a production deploy must opt in
	// explicitly, because those credentials are public and demo1 is an admin.
	DemoAccounts bool

	// SignupEnabled opens public account creation: the password signup
	// route and first-login OAuth provisioning. It defaults to true in
	// development and false in production. Magic links never provision
	// accounts (they only log existing users in).
	SignupEnabled bool

	// RateLimitRPM bounds dynamic requests per session (or per client IP
	// when signed out) in a sliding minute. 0 disables the in-process
	// limiter. The edge remains the primary throttle for a fleet; this is
	// the single-box default so a stock deployment is not unbounded.
	RateLimitRPM int

	// AdminToken, when non-empty, unlocks the admin endpoints (e.g. the
	// Todo "clear all" via token). Loaded from the age-decrypted
	// secrets file, NOT from the host environment directly. This is the
	// canonical example of "use a real secret in the demo app" — see
	// README's "Admin unlock" section.
	AdminToken string

	NATS struct {
		Enabled  bool
		StoreDir string
		// LeafNodeURL, when set, makes this instance a NATS Leaf Node that
		// syncs with a central NATS server (e.g. the demo server). Used by
		// the desktop/mobile edge to replicate JetStream streams offline
		// and replay on reconnect. Empty = standalone embedded NATS.
		LeafNodeURL string
	}

	// DagNats holds the DagNats durable-workflow engine settings. It is
	// always compiled; set DAGNATS_ENABLED=false to no-op it at runtime.
	// DagNats reuses the embedded NATS JetStream that the realtime build
	// already starts, so it needs no extra infra.
	DagNats struct {
		Enabled  bool
		HTTPAddr string // HTTP/API/console listen addr (separate port from the app)
		NATSPort int    // NATS port the engine owns (default 4222; the realtime broadcaster connects here)
		StoreDir string

		// GreetPacing is how long the onboarding-greet step pauses before
		// completing. Its ONLY purpose is human-visible pacing: the onboarding
		// stepper advances one step at a time, and a greet that returns
		// instantly makes the "greeting" phase flash by unread.
		//
		// It is therefore product latency, not a neutral demo detail — it sits
		// in front of every real onboarding run. Configured rather than
		// hard-coded so a slow device, an automated run, or a test can dial it
		// down without editing production code (tests substitute their own
		// handler, but the knob documents the intent). Default 1500ms, or
		// DAGNATS_GREET_PACING to override (e.g. "0s", "250ms").
		GreetPacing time.Duration
	}

	// GoAkt holds the GoAkt entity-actor settings. It is always
	// compiled; set GOAKT_ENABLED=false to no-op it at runtime.
	// Standalone mode needs no network, discovery, or cluster: rooms
	// live in this process only.
	GoAkt struct {
		Enabled bool
	}

	// OfflineSync controls the hybrid offline-sync-online strategy.
	// When enabled (default), the system works offline and syncs when
	// online via Service Worker (web) + NATS CRUD proxy (desktop/edge).
	// When disabled, all requests go directly to the database with no
	// offline caching or background sync — the simplest path for
	// always-online deployments.
	//
	// Toggle via OFFLINE_SYNC_ENABLED=false in the environment.
	// Default: true (opt-out). When disabled, no Service Worker is
	// registered, no NATS CRUD stream is created, and no unnecessary
	// code paths are traversed.
	OfflineSync struct {
		Enabled bool
	}

	// Collab selects the CRDT snapshot store behind the whiteboard and
	// notes (internal/collab Persister). "turso" persists snapshots in
	// doc_snapshots (survive restarts, shared across instances via the
	// database); "memory" keeps them process-local (no snapshot writes,
	// docs start empty after a restart — the draw-without-persistence
	// mode). Unknown values fail closed at boot.
	//
	// Toggle via COLLAB_SNAPSHOTS=memory in the environment.
	// Default: turso.
	Collab struct {
		Snapshots string
	}

	// RBACEnabled turns on the opt-in Casbin RBAC middleware
	// (RBAC_ENABLED, default false): false is a no-op pass-through, true
	// binds the enforcer built from internal/rbac/casbin and denies any
	// request the policy does not allow. See docs/rbac.md.
	RBACEnabled bool

	// RBACPolicySource selects the RBAC rule source (RBAC_POLICY_SOURCE,
	// default "embedded"): "embedded" reads policy.csv, "db" loads the
	// casbin_rule table (seeded on first use). Anything else → embedded.
	RBACPolicySource string

	// RBACPermissive is audit-before-enforce (RBAC_PERMISSIVE, default
	// false): would-be denies are warn-logged and allowed through. One
	// deploy only, after adding a feature's paths. See docs/rbac.md.
	RBACPermissive bool

	// AdminEmails promotes existing users to the admin role at boot
	// (ADMIN_EMAILS, comma-separated, matched lowercase). Promotion only,
	// never provisioning: the address must already own a row (sign up or
	// first OAuth login first), and public signup must stay closed while
	// the list is set — anyone can claim an unowned address. Empty
	// disables promotion entirely.
	AdminEmails []string

	// ViewerEmails promotes existing users to the restricted viewer role
	// at boot (VIEWER_EMAILS, same comma-separated contract). Viewers
	// read, create and toggle todos; deletes and paid actions deny with
	// 403 under RBAC enforcement and render disabled with a message.
	// Ties lose to AdminEmails (admin promotion runs last). Empty
	// disables promotion entirely; demo1/demo2 stay full users unless
	// listed here.
	ViewerEmails []string

	// Auth holds owned-auth settings.
	Auth struct {
		// TOTPEncKey seals TOTP secrets (AUTH_ENC_KEY, 64 hex chars).
		// Empty falls back to the auto-created key file. Malformed is
		// dropped (fail-safe); set-but-malformed fails the boot instead.
		TOTPEncKey []byte
	}
	// Turso holds the Turso Database connection settings — the Rust
	// SQLite-compatible engine, driven from Go by turso.tech/database/tursogo.
	// Consumed by the sqlc-backed data layer. An empty URL means a local file
	// at DBPath.
	Turso struct {
		URL       string // turso:// or file://
		AuthToken string
		DBPath    string // local file used when URL is empty
	}

	// Skin selects the active UI skin. One ships ("daisyui"), and it is the
	// template's core theme; the registry in web/skins/ exists so adding another
	// is a docs page, not a refactor. Visual variants are DaisyUI themes, not
	// skins (docs/ui-skins.md). Resolved at runtime; no build tag.
	Skin string

	// ChiAddr overrides the flat-stack listen address (owned auth +
	// sqlc todo slice + every ported feature). Empty uses Host:Port,
	// the shape every other frontend binds. cmd/desktop sets it so the
	// webview proxy and the backend agree on one address.
	ChiAddr string

	GoAI GoAIConfig

	// Review holds the jev-compatible judge settings for features/review
	// (JEV_* env, keyless SimpleJev demo by default). See config/review.go
	// and docs/review.md for the customization contract.
	Review ReviewConfig

	// AskRouter opts the Ask AI page into that same jev-compatible
	// endpoint as a component router (JEV_ROUTER_* env, off by default).
	// See config/ask_router.go and docs/configuration.md.
	AskRouter AskRouterConfig

	// GenUI holds the per-leg AI answer surfaces (GENUI_*_SURFACES env).
	// See config/genui.go.
	GenUI GenUIConfig

	// Credits gates the AI credits + BYOK plugin (lib calionauta/ai-credits).
	// When Credits.Enabled is false the plugin registers no routes and
	// credits.New is never built — the binary runs billing-free.
	Credits CreditsConfig

	// Search selects the todo search layer (SEARCH_MODE). The plugin is
	// removable; the default (substring) is the layer that survives its
	// removal. See config/search.go and docs/configuration.md.
	Search SearchConfig
}

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
		AppURL:        strings.TrimRight(getEnv("APP_URL", ""), "/"),
		DemoAccounts:  envBool("DEMO_ACCOUNTS_ENABLED", dev),
		SignupEnabled: envBool("SIGNUP_ENABLED", dev),
		RateLimitRPM:  int(envInt("RATE_LIMIT_RPM", defaultRateLimitRPM)),
		AdminToken:    os.Getenv("ADMIN_UNLOCK_TOKEN"),
		GoAI: GoAIConfig{
			APIKey: os.Getenv("GOAI_API_KEY"),
		},
	}

	cfg.NATS.Enabled = envBool("NATS_ENABLED", defaultNATSEnabled())
	cfg.NATS.StoreDir = getEnv("NATS_STORE_DIR", "data/nats")
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
	cfg.loadSearchEnv()
	cfg.Turso.URL = getEnv("TURSO_DATABASE_URL", "")
	cfg.Turso.AuthToken = getEnv("TURSO_AUTH_TOKEN", "")
	cfg.Turso.DBPath = getEnv("TURSO_DB_PATH", "data/app.db")
	cfg.ChiAddr = getEnv("CHI_ADDR", "")
	cfg.Skin = getEnv("UI_SKIN", "daisyui")
	cfg.loadReviewEnv()
	cfg.loadAskRouterEnv()
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

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envBool reads a boolean env var, falling back to def when unset or
// unparseable. The fallback lets a config file or env var (e.g.
// NATS_ENABLED=false) override the default at runtime; every feature is
// always compiled, so there is no build tag to flip.
func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

// envInt reads an integer env var, falling back to def when unset or
// unparseable.
func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// envDuration reads a duration env var ("250ms", "2s"), falling back to def
// when unset or unparseable. A non-positive override is treated as unset:
// zero would silently delete intentional pacing rather than tune it.
func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

// defaultDagNatsNATSPort is the conventional NATS port the DagNats engine
// owns. The realtime broadcaster connects here (single-NATS convention).
const defaultDagNatsNATSPort = 4222

// defaultGreetPacing is the onboarding-greet pause. It exists purely so a human
// watching the stepper can read the "greeting" phase; see DagNats.GreetPacing.
// Long enough to be perceptible, short enough not to feel sluggish.
const defaultGreetPacing = 1500 * time.Millisecond

// defaultAppName falls back to the binary name when APP_NAME is unset
// so the secrets file scope tracks whatever the project owner actually
// compiled. Uses os.Args[0] (binary path) trimmed to base name; if that
// fails (e.g. tests), it returns a hard-coded stable name.
func defaultAppName() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return "gogogo"
	}
	base := exe
	for i := len(exe) - 1; i >= 0; i-- {
		if exe[i] == '/' {
			base = exe[i+1:]
			break
		}
	}
	if base == "" {
		return "gogogo"
	}
	return base
}
