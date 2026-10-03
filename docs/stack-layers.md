# Stack in layers, not silos

Everything you need to build a modern web app, in a single binary. This page
explains **why each choice is here**; for the layered async topology see
[Six async layers](async-layers.md).

| Layer | Choice | Why |
|-------|--------|-----|
| **Language** | Go 1.27 | Fast compilation, easy deploy, lean runtime |
| **Database + Auth + API** | [PocketBase](https://pocketbase.io) (embedded, on `ncruces/go-sqlite3`) | Zero-config auth, REST, admin UI at `/_/`, file storage — all in SQLite |
| **Templating** | [Templ](https://templ.guide) | Type-safe Go components, generated at build time |
| **Reactive UI** | [Datastar](https://data-star.dev) (SSE) | Server-rendered over SSE, single ~12 KiB client. CSS built once via the Tailwind v4 CLI; no JS framework build step. |
| **CSS / UI skin** | [DaisyUI v5](https://daisyui.com) (default) + TailwindCSS; pluggable: [BasecoatUI](https://basecoatui.com), [Morpheus](https://github.com/romshark/morpheus) | DaisyUI ~34 kB; Basecoat shadcn-style OKLCH tokens; Morpheus vendorized bundle. See [UI skins](ui-skins.md). |
| **Task queue** | [goqite](https://github.com/maragudk/goqite) + SSE Hub | Background jobs streamed to the browser, no Redis |
| **Retries** | [avast/retry-go v4](https://github.com/avast/retry-go) | Exponential backoff with jitter, no boilerplate |
| **Durable workflows** | [DagNats](https://github.com/danmestas/dagnats) | Multi-step durable workflows as declarative JSON over NATS JetStream |
| **LLM SDK** | [GoAI](https://github.com/zendev-sh/goai) | Any provider: OpenAI, Anthropic, Groq, Ollama…; provider-aware retry and streaming |
| **AI credits + BYOK** | [ai-credits](https://github.com/calionauta/ai-credits) | Optional SQLite ledger: managed LLM reserve/settle billing, monthly entitlements, Stripe top-ups, encrypted per-user BYOK relay metering |
| **Real-time** | [NATS JetStream](https://nats.io) | Multi-user realtime, cross-instance broadcast |
| **Collaboration (CRDT)** | [loro-go](https://github.com/aholstenson/loro-go) | Conflict-free merging of whiteboard/notes state; converges offline edits with no last-write-wins data loss |
| **Hand-drawn canvas** | [Rough.js](https://roughjs.com) (embedded) | Minimalist sketchy whiteboard rendering, embedded in the binary so it is removed with the whiteboard feature |
| **Secrets** | [age](https://age-encryption.org) + `~/.secrets/` | Local encryption, no vault, no cloud |
| **IDs** | [google/uuid](https://github.com/google/uuid) | Stable request/job IDs |
| **Live reload** | [Air](https://github.com/air-verse/air) | `make dev` regenerates templ and restarts the binary |
| **Native window (PoC)** | [gogpu/ui](https://github.com/gogpu/ui) (pure Go, zero CGO) | `cmd/gui`: a second frontend over the same PocketBase + `EntityStore` — no HTTP, no webview. See [Desktop & Mobile](desktop-mobile.md). |
| **Linting** | [golangci-lint](https://golangci-lint.run) + [datastar-lint](https://github.com/calionauta/datastar-lint) | 27 linters: `govet`, `staticcheck`, `gosec`, `revive`, `gocritic`, `errcheck`, `ineffassign`, `unused`, `errorlint`, `nilerr`, `bodyclose`, `contextcheck`, `containedctx`, `sloglint`, `thelper`, `testifylint`, `gocyclo`, `gocognit`, `funlen`, `noctx`, `goconst`, `dupl`, `lll`, `mnd`, `tagliatelle`, `modernize`, `nolintlint` (see `.golangci.yml`); `datastar-lint` catches Datastar attribute/signal/expression mistakes (run via `make datastar-lint`) |
| **CI/CD** | GitHub Actions | `ci.yml` (lint + test + build) + `deploy.yml` (multi-arch Docker to ghcr.io, runs on `master`) |

## Why `ncruces/go-sqlite3`?

It is the pure-Go (no cgo) SQLite engine this template standardizes on and the
always-on driver. `db/pocketbase.go` registers it as the `sqlite3`
`database/sql` driver that every query uses. PocketBase also bundles
`modernc.org/sqlite`, but that registers itself as `sqlite` and stays unused,
so **no build tag is required** and a plain `go build` just works.

Being cgo-free means clean cross-compilation for the multi-arch Docker image
(linux/amd64 + arm64) and the Wails desktop/mobile builds.

## Why no frontend framework?

Datastar gives you reactivity with no client-side framework and no build step.
The client is a single ~12 KiB script; state lives in signals that the server
reads and writes over SSE. You get the ergonomics of a reactive UI without a
`node_modules` tree, a bundler, or hydration — and the HTML that arrives over
the wire is the same HTML the server rendered, so there is no second source of
truth to drift.

CSS is the one build step, and it is a build step you run explicitly
(`make css`), not one your users pay for at runtime: the compiled
`app.min.css` is embedded into the binary with `//go:embed`.

## How the pieces combine

The layers are not independent choices — they are designed to work together:

- A handler returns a **Templ** component; **Datastar** decides whether the
  response is a full page or an SSE patch.
- Background work goes to **goqite**; the worker streams a **toast** back to
  the originating browser through the **SSE Hub**, addressed by `clientID`.
- Multi-step processes go to **DagNats**, whose state is durable on **JetStream**.
- Collaborative state goes through **Loro CRDT**, so offline edits merge
  without last-write-wins data loss.
- Record changes fan out through **PocketBase realtime**, scoped per user by
  the collection's own access rules.

Related: [Features](features.md), [Async layers](async-layers.md),
[Code quality for LLM agents](code-quality.md).