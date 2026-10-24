# Configuration

Every environment variable and runtime constant lives in one place:
`config/config.go`. Change it once, every feature picks up the new value.

## Environment variables

Secrets can come from the environment directly, or — for local development —
from an age-encrypted file in `~/.secrets/` decrypted into the process
environment at boot. Production does not use age: the secrets file is rendered
from GitHub Actions secrets on every deploy. Full explanation, including the
two-file layout (`.env.age` is read, `.env` is the plaintext working copy):
[Getting started → Secrets setup](getting-started.md#secrets-setup).

### Core

| Variable | Default | Purpose |
|---|---|---|
| `PORT` | `8080` | HTTP listen port |
| `HOST` | `0.0.0.0` | HTTP bind address |
| `ENVIRONMENT` | `development` | Anything other than `production` selects dev mode |
| `APP_NAME` | binary name | Project name (secrets scope); never empty |
| `APP_INSTANCE_ID` | hostname | Stable per-instance identity for cross-instance fan-out: names the JetStream CRUD durable and tags published events so an instance ignores its own echo. Set it when the hostname changes across restarts (StatefulSet, recreated containers) and you want the durable cursor to survive |
| `LOG_LEVEL` | `INFO` | slog level |
| `HTTP_ACCESS_LOG` | `false` | Emit one structured `http: request` line per request (method, path, status, bytes, duration, stream). A stream is logged when it closes, with its real duration. Off by default: a busy SSE page logs a line per stream. The write is flusher-preserving, so enabling it never breaks an SSE handler. |
| `DATA_DIR` | `data` | Root for runtime data |
| `TURSO_DB_PATH` | `data/app.db` | Turso Database file for the flat stack (used when `TURSO_DATABASE_URL` is empty) |
| `CHI_ADDR` | unset | Listen-address override for the flat stack. Unset = `HOST:PORT`. `cmd/web` honors it since the flat-stack port (it used to be the staged-seam flag); `cmd/desktop` sets it itself from `HOST`/`PORT` |

| `SEARCH_MODE` | `substring` | **Default** todo search layer: `substring`, `fuzzy` or `bm25` (see [Search](search.md)). **All three are always wired** and the search box's `Match` picker switches between them per request, so this value only decides which one a tab starts on. `bm25` therefore always writes an index (one postings row per distinct term per mutation); set `SEARCH_MODE=substring` and leave the picker alone if you want searches that cost no writes. An unknown value logs a warning and falls back to `substring` rather than failing the boot — a typo must never make the server unbootable. On upgrade, a boot-time heal re-indexes rows that predate the index, because postings are a cache of a Go stemming function and SQL cannot rebuild them. |

### Realtime and workflows

| Variable | Default | Purpose |
|---|---|---|
| `NATS_ENABLED` | `true` | Boot NATS JetStream for multi-instance broadcast. `false` disables the engine; consumers handle nil. |
| `NATS_STORE_DIR` | under `DATA_DIR` | JetStream storage directory |
| `NATS_LEAFNODE_URL` | unset | When set, the desktop boots as a NATS Leaf Node replicating against your central server. Offline edits replay on reconnect. |
| `DAGNATS_ENABLED` | `true` | Boot the DagNats durable workflow engine |
| `DAGNATS_HTTP_ADDR` | `127.0.0.1:8090` | DagNats HTTP API + console (loopback-only; browsers reach the console through the `/console/` reverse proxy on the app mux) |
| `DAGNATS_NATS_PORT` | `4222` | Shared embedded JetStream port (DagNats boots it; the whiteboard SyncWorker attaches) |
| `DAGNATS_STORE_DIR` | under `DATA_DIR` | DagNats store directory |
| `DAGNATS_GREET_PACING` | `1500ms` | How long the onboarding **greet** step pauses before completing. Exists only so a human watching the stepper can read the "greeting" phase — it is deliberate product latency, so tune it rather than delete it. A non-positive or unparseable value falls back to `1500ms`. Set `0`-ish (e.g. `1ms`) for automated runs. |

### Persistence

| Variable | Default | Purpose |
|---|---|---|

| `OFFLINE_SYNC_ENABLED` | `true` | Hybrid offline sync. `false` disables the NATS CRUD proxy and the Service Worker offline queue — zero code paths traversed. |
| `COLLAB_SNAPSHOTS` | `turso` | CRDT snapshot store behind whiteboard/notes. `memory` keeps snapshots process-local (draw without persistence — docs start empty after restart). Unknown values fail closed at boot. |

### LLM

| Variable | Default | Purpose |
|---|---|---|
| `GOAI_API_KEY` | unset | Any OpenAI-compatible provider key. Unset → the AI suggest route is not registered and the button is hidden. |
| `GOAI_BASE_URL` | `https://api.openai.com/v1` | OpenAI-compatible base URL |
| `GOAI_MODEL` | `gpt-4o-mini` | Default model |
| `SIMULATE_LLM` | anything but `false` | In-process fake LLM scripting 500 → retry → slow → 200, so you can watch the retry toasts without a provider key. Enabled unless explicitly set to `false` — including in production |

### Review loop (jev judge)

| Variable | Default | Purpose |
|---|---|---|
| `JEV_API_URL` | SimpleJev keyless demo (`.../v1/classifier`) | Any jev-compatible endpoint (`POST {state,model,questions}`): TypeSafe, self-hosted SimpleJev, tensai. Bare hosts expand to `/v1/systemone` |
| `JEV_MODEL` | SimpleJev demo classifier | Judge model ID served by the endpoint |
| `JEV_API_KEY` | unset | Judge credential (age-encrypted secrets file). Empty = keyless demo, no `Authorization` header |
| `JEV_TIMEOUT` | `10s` | Per-attempt judge HTTP timeout |
| `JEV_CONFIDENCE_THRESHOLD` | `0.7` | Below this the verdict escalates to a human; recalibrate per backend (see [Review loop](review.md)) |
| `JEV_ROUTER_ENABLED` | `false` | Let the same jev-compatible endpoint choose which component a Chat answer leads with. Off = byte-identical prompt to the router-less one |
| `JEV_ROUTER_CONFIDENCE_THRESHOLD` | `0.7` | Below this the pick is ignored and the whole catalog is offered |
| `GENUI_CHAT_SURFACES` | `catalog,custom` | Chat's answer surfaces: CSV of `catalog,custom,native`. `custom` is `html_block` (model-invented, sanitized markup). `native` (the app's own panels) is always added and cannot be turned off. Absent = the same value. `catalog` alone is the strict showcase (unknown types fail closed). |

The router (`JEV_ROUTER_*`) is opt-in and reuses the endpoint above —
one jev-compatible API serves the judge and the router, for BOTH AI legs.
Turning it on adds a leading hint to the prompt; it never removes a
component from what the model may emit, and every failure (endpoint down,
low confidence, a label
outside the catalog) falls back to the full catalog, so a leg answers
exactly as it did before. See [Chat](features.md).

### AI credits + BYOK

All dormant unless `CREDITS_ENABLED=true`.

| Variable | Default | Purpose |
|---|---|---|
| `CREDITS_ENABLED` | `false` | Master switch for the ai-credits plugin |
| `CREDITS_MONTHLY_CREDITS` | `0` | Monthly entitlement per user |
| `CREDITS_DEFAULT_MODE` | `explicit` | Billing mode applied when a request does not name one |
| `CREDITS_ENC_KEY` | unset | 64 hex chars (`openssl rand -hex 32`). Encrypts user BYOK keys at rest. Anything else fails closed at BYOK configuration. |
| `CREDITS_MODEL` | `GOAI_MODEL`, else `gpt-4o-mini` | Model used for priced managed calls |
| `CREDITS_PRICING_FILE` | unset | Path to a pricing table |
| `BYOK_PROVIDERS` | unset | `openai=https://api.openai.com/v1,groq=https://api.groq.com/openai/v1` |
| `STRIPE_SECRET_KEY` | unset | Stripe top-ups |
| `STRIPE_WEBHOOK_SECRET` | unset | Stripe webhook signature verification |
| `STRIPE_SUCCESS_URL` / `STRIPE_CANCEL_URL` | unset | Redirect targets after top-up |

With `CREDITS_ENABLED=true`, authenticated users get `GET /api/credits`, the
real Todo **Suggest** path is metered in managed mode, and the app exposes
`POST /api/ai/request`. When both BYOK vars are present,
`POST /api/byok/{provider}/{path...}` injects the user's encrypted key
server-side and records upstream JSON/SSE token usage with
`billing_mode=byok` and `credits_charged=0`.

> The relay trusts the authenticated session user stamped by the app. **Do
> not expose `X-Auth-User` from an external proxy.**

### Auth and admin

| Variable | Default | Purpose |
|---|---|---|
| `AUTH_ENC_KEY` | auto-created `totp_seal.key` | 64 hex chars (`openssl rand -hex 32`). Seals TOTP secrets at rest (XChaCha20-Poly1305). Unset auto-creates a 0600 key file in the data dir (no operator action; backups must handle it like any secret). Set-but-malformed fails the boot. |
| `ADMIN_UNLOCK_TOKEN` | unset | Master-password token for the "Clear all" form. When present (in the age-encrypted secrets file), the todo UI shows the form; the handler compares constant-time. |
| `RBAC_ENABLED` | `false` | Enforce the embedded Casbin policy on every request (deny = 403 + warn log). `false` is pass-through with identical wiring. When on: anonymous reaches only the public surface (landing, login, auth endpoints, static, health); users reach everything except `/config`; admins reach all. |
| `RBAC_PERMISSIVE` | `false` | Audit-before-enforce rollout: would-be denies are warn-logged (`permissive=1`) and the request proceeds. Set for one deploy after adding a feature's paths, then turn off. Only meaningful with `RBAC_ENABLED=true`. See [RBAC](rbac.md). |
| `RBAC_POLICY_SOURCE` | `embedded` | Where the Casbin rules come from: `embedded` reads the compiled-in `policy.csv`; `db` loads the `casbin_rule` table (seeded from the embedded policy on first boot, then editable at runtime). Any other value falls back to `embedded`. |
| `ADMIN_EMAILS` | unset | Comma-separated addresses promoted to `admin` at boot (existing rows only — never provisions; keep public signup closed while set, anyone can claim an unowned address). Required to reach `/config` with `RBAC_ENABLED=true`. |
| `VIEWER_EMAILS` | unset | Comma-separated addresses promoted to the restricted `viewer` role at boot (same existing-rows-only contract). Viewers read, create and toggle todos; deletes and paid actions deny with 403 under enforcement and render disabled with a message. An address in both lists ends `admin`. See the RBAC demo in [Admin & Dashboard](admin-dashboard.md). |

### Build metadata

| Variable | Default | Purpose |
|---|---|---|
| `BUILD_LABEL` | `dev` | Git tag baked in via `-ldflags="-X main.Version=..."`; shown on the sidebar build badge |
| `BUILD_COMMIT` | *(empty)* | Short SHA baked in via `-ldflags="-X main.CommitHash=..."`; shown next to the label |

The Makefile sets these automatically. See
[Deploy](deploy.md#build-pipeline-and-version-badge-metadata) for the
`docker buildx` version.

## Runtime constants

Not every tunable belongs in `config.go`. Runtime constants that are
implementation details of a single package stay in that package to keep
cohesion — `config/config.go` documents every env var and the most commonly
tuned runtime constants.

| Constant | Location | Default | Purpose |
|----------|----------|---------|---------|
| `DefaultReplayBufferSize` | `config/config.go` | `64` | Per-client SSE replay ring-buffer length |
| `DefaultClientQueueSize` | `config/config.go` | `64` | Per-client SSE channel buffer |
| `DefaultSSEHeartbeatInterval` | `config/config.go` | `15s` | SSE heartbeat to detect disconnection |
| `BuildLabel` | `config/config.go` | `dev` | Git tag on the sidebar build badge |
| `BuildCommit` | `config/config.go` | *(empty)* | Short SHA on the sidebar build badge |
| `DefaultBaseURL` (GoAI) | `internal/llm/goai.go` | `https://api.openai.com/v1` | OpenAI-compatible base URL |
| `DefaultModel` (GoAI) | `internal/llm/goai.go` | `gpt-4o-mini` | Default LLM model |
| `DEFAULT_VOLUME` | `web/resources/static/cuelume.js` | `0.4` | Sound playback volume (cuelume's mixer runs hotter than libraries tuned for 30%) |

## Related

- [Deploy](deploy.md) — the CI build pipeline and its ldflags.
- [Async layers](async-layers.md) — what each opt-out actually disables.