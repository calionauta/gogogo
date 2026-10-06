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
| `LOG_LEVEL` | `INFO` | slog level |
| `DATA_DIR` | `data` | Root for runtime data |
| `DATABASE_PATH` | `data/data.db` | SQLite file the credits ledger shares with PocketBase |

### Skin

| Variable | Default | Purpose |
|---|---|---|
| `UI_SKIN` | `daisyui` | Active UI skin: `daisyui`, `basecoat`, or `morpheus`. Can also be overridden per request with `?skin=`. Unknown values fall back to DaisyUI with a warning. |

### Realtime and workflows

| Variable | Default | Purpose |
|---|---|---|
| `NATS_ENABLED` | `true` | Boot NATS JetStream for multi-instance broadcast. `false` disables the engine; consumers handle nil. |
| `NATS_STORE_DIR` | under `DATA_DIR` | JetStream storage directory |
| `NATS_LEAFNODE_URL` | unset | When set, the desktop boots as a NATS Leaf Node replicating against your central server. Offline edits replay on reconnect. |
| `DAGNATS_ENABLED` | `true` | Boot the DagNats durable workflow engine |
| `DAGNATS_HTTP_ADDR` | `127.0.0.1:8090` | DagNats HTTP API + console |
| `DAGNATS_NATS_PORT` | `4222` | Shared embedded JetStream port (DagNats boots it; the whiteboard SyncWorker attaches) |
| `DAGNATS_STORE_DIR` | under `DATA_DIR` | DagNats store directory |
| `DAGNATS_TRIGGER_BOOTSTRAP` | `true` | Seed one disabled placeholder trigger when the trigger bucket is empty. **Workaround for an upstream DagNats v0.0.24 bug** — without it the trigger console cannot create the first trigger. See [the workaround page](dagnats-bootstrap-workaround.md) before disabling or removing. |
| `DAGNATS_GREET_PACING` | `1500ms` | How long the onboarding **greet** step pauses before completing. Exists only so a human watching the stepper can read the "greeting" phase — it is deliberate product latency, so tune it rather than delete it. A non-positive or unparseable value falls back to `1500ms`. Set `0`-ish (e.g. `1ms`) for automated runs. |

### Persistence

| Variable | Default | Purpose |
|---|---|---|
| `ENTITY_STORE` | `pb` | `pb` (PocketBase records, admin UI works) or `crdt` (Loro per-owner doc + JetStream cross-instance transport). Same `EntityStore[T]` interface either way. |
| `OFFLINE_SYNC_ENABLED` | `true` | Hybrid offline sync. `false` disables the NATS CRUD proxy and the Service Worker offline queue — zero code paths traversed. |

### LLM

| Variable | Default | Purpose |
|---|---|---|
| `GOAI_API_KEY` | unset | Any OpenAI-compatible provider key. Unset → the AI suggest route is not registered and the button is hidden. |
| `GOAI_BASE_URL` | `https://api.openai.com/v1` | OpenAI-compatible base URL |
| `GOAI_MODEL` | `gpt-4o-mini` | Default model |
| `SIMULATE_LLM` | anything but `false` | In-process fake LLM scripting 500 → retry → slow → 200, so you can watch the retry toasts without a provider key. Enabled unless explicitly set to `false` — including in production |

### AI credits + BYOK

All dormant unless `CREDITS_ENABLED=true`.

| Variable | Default | Purpose |
|---|---|---|
| `CREDITS_ENABLED` | `false` | Master switch for the ai-credits plugin |
| `CREDITS_MONTHLY_CREDITS` | `0` | Monthly entitlement per user |
| `CREDITS_DEFAULT_MODE` | `explicit` | Billing mode applied when a request does not name one |
| `CREDITS_ENC_KEY` | unset | 64 hex chars (`openssl rand -hex 32`) or a legacy raw 32-byte value. Encrypts user BYOK keys at rest. |
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

> The relay trusts the authenticated PocketBase user stamped by the app. **Do
> not expose `X-Auth-User` from an external proxy.**

### Auth and admin

| Variable | Default | Purpose |
|---|---|---|
| `ENCRYPTION_KEY` | — | PocketBase encryption key |
| `ADMIN_UNLOCK_TOKEN` | unset | Master-password token for the "Clear all" form. When present (in the age-encrypted secrets file), the todo UI shows the form; the handler compares constant-time. |

### Build metadata

| Variable | Default | Purpose |
|---|---|---|
| `BUILD_LABEL` | `dev` | Git tag baked in via `-ldflags="-X main.Version=..."`; shown on the navbar version badge |
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
| `BuildLabel` | `config/config.go` | `dev` | Git tag on the navbar badge |
| `BuildCommit` | `config/config.go` | *(empty)* | Short SHA on the navbar badge |
| `DefaultBaseURL` (GoAI) | `internal/llm/goai.go` | `https://api.openai.com/v1` | OpenAI-compatible base URL |
| `DefaultModel` (GoAI) | `internal/llm/goai.go` | `gpt-4o-mini` | Default LLM model |
| `DEFAULT_VOLUME` | `web/resources/static/cuelume.js` | `0.4` | Sound playback volume (cuelume's mixer runs hotter than libraries tuned for 30%) |

## Related

- [Deploy](deploy.md) — the CI build pipeline and its ldflags.
- [Async layers](async-layers.md) — what each opt-out actually disables.