// SCOPE:core — Config types: the struct Load fills.
//
// Split from config.go so the env-loading code and the type surface stay
// under the file-size gate; the package is one source of truth either way.
package config

import "time"

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
		// Listen is host:port for the embedded server. Empty keeps a random
		// loopback port (the default, unreachable from outside). A
		// non-loopback listen requires Token.
		Listen string
		// Token authenticates clients when the listener is exposed. Empty
		// means no auth, which the guard allows only on loopback.
		Token string
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
		// MagicLinkBindBrowser forces same-browser magic links.
		MagicLinkBindBrowser bool
		// RequireEmailVerification gates password login.
		RequireEmailVerification bool
		// SecureCookies marks the session cookie Secure (HTTPS-only).
		// Tracks !Dev and is cleared by the desktop shell, whose webview
		// loads loopback HTTP where WebKit drops a Secure cookie.
		SecureCookies bool
		// SessionIdleTimeout ends inactive sessions; zero = absolute TTL.
		SessionIdleTimeout time.Duration
		// SMTP configures verification/reset delivery.
		SMTP SMTPConfig
		// AppURL is the browser-facing origin for out-of-band links.
		AppURL string
	}
	// LicenseMode is the distribution gate: none (default), license or
	// account. Non-none blocks the app until a signed token is activated.
	LicenseMode string
	// LicensePublicKey is the Ed25519 key (hex/base64) that verifies tokens.
	LicensePublicKey string
	// LicenseFile is where the activated token lives (0600).
	LicenseFile string
	// LicenseGate enables the activation gate. Only the desktop shell sets
	// it: a web deploy in account mode issues tokens, it is not gated.
	LicenseGate bool
	// LicensePrivateKey (server only) mints device tokens for signed-in
	// users; the desktop ships only the public key.
	LicensePrivateKey string
	// CloudSyncURL is the NATS endpoint embedded in issued device tokens.
	CloudSyncURL string
	// DeviceTokenDays bounds an issued device token (0 = perpetual).
	DeviceTokenDays int
	// CloudSyncAllow optionally restricts which hosts /account may connect
	// to (comma list, host or host:port). Empty allows any valid NATS URL.
	CloudSyncAllow []string
	// AuthMode is local (login) or open (one local owner, no login).
	AuthMode string
	// AllowOpenRemote permits AUTH_MODE=open beyond loopback.
	AllowOpenRemote bool

	// Delivery picks the transport for external channels (email today).
	// The inbox is not a channel here: in_app is written by the feature.
	Delivery DeliveryConfig
	// Turso holds the Turso Database connection settings — the Rust
	// SQLite-compatible engine, driven from Go by turso.tech/database/tursogo.
	// Consumed by the sqlc-backed data layer. An empty URL means a local file
	// at DBPath.
	Turso struct {
		URL       string // turso:// or file://
		AuthToken string
		DBPath    string // local file used when URL is empty
	}

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

	// ChatRouter opts Chat into that same jev-compatible
	// endpoint as a component router (JEV_ROUTER_* env, off by default).
	// See config/chat_router.go and docs/configuration.md.
	ChatRouter ChatRouterConfig

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
