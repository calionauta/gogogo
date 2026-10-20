# Review loop

Paste text or a diff on `/review` — a writer and a critic iterate (bounded),
then a classifier judge delivers the verdict. Text is the default tab
(no code literacy needed, and classifier judges are most accurate on
prose); code is the second tab.

## Where this sits (plugin, not layer)

The scope taxonomy has no "layer" axis — the directory is the layer
(`features/` = product) and the annotation is the removal class. In
those terms: `review` is a **feature** (terminal user surface, trim it
and `/review` is gone) and `genkit` is a **plugin capability** (serves
the feature; nothing user-facing disappears without it, but review
stops working). Genkit is not a framework (proven by
`cmd/check-flatdeps`), not a second LLM client (zero provider plugins;
generation reaches providers only through `llm.Client`), and not core
(trim + `go mod tidy` removes it from the build).

## How it runs

Synchronous in the request scope: one POST streams phased progress over
ephemeral SSE and patches a single verdict fragment. Disconnect cancels
the run; `MaxRunDuration` (90s) bounds it either way. A 15s heartbeat
holds the stream open between phases so proxies never kill it idle
(a dead stream used to leave the Run button disabled with no error).
Nothing persists —
no queue jobs, no DagNats runs, no database writes. The billing ledger
is the only write (generative calls via `llm.Client`'s meter when wired;
the judge call is recorded once when a judge meter is wired).

The loop needs a real LLM (`GOAI_API_KEY`): without one the page shows a
setup hint and the run API answers 503. There is deliberately no
simulated path — the template's AI surface is real-LLM only.

## What the verdict card shows

A run has two halves and the card shows both, because one without the
other is a decision nobody can audit:

- **The exchange** — the writer's proposal, and (when the critic asked
  for changes) the critic's own request, rendered verbatim.
- **The decision** — verdict badge, confidence, round count, and a
  rationale that names *both* what the critic concluded and what the
  judge decided.

Three rules keep the card honest, and each fixes a reported failure:

- **A no-op is not an approval.** When the writer replies
  `NO CHANGE NEEDED`, or the proposal is byte-identical to the input,
  the run stops before the judge and returns `needs-human` with
  "the editor proposed no change". Otherwise a trivial input came back
  as `approve` (confidence 1.00) with the input echoed under
  "Proposal", which reads as a broken rewrite.
- **`APPROVE` means the draft is an improvement**, not merely
  "no complaint" — the critic prompt requires the comparison. A
  criticism-free pass over an unedited draft is exactly the case above.
- **Each run replaces the previous verdict, and a failed run replaces
  it too.** The result region renders a pending card before the loop
  starts and an error card on failure; all three states carry
  `id="review-result"` because that is the patch target (see
  `docs/code-quality.md`, the outer-morph rule).

## The three roles

- **Genkit orchestrates** (`github.com/firebase/genkit/go`, no provider
  plugins): typed `ReviewInput`/`Verdict` flows, per-phase progress,
  per-step OTel spans, Dev UI + evals. One shared instance per process.
- **goai generates** (`internal/llm.Client` via the `gogogo/llm` model
  action): writer + critic. Retry stays in goai's native layer only —
  never add a Genkit retry/billing middleware (double reserve).
- **jev judges** (terminal, once per run): `mattn/go-jev` against any
  jev-compatible API. Below `JEV_CONFIDENCE_THRESHOLD` the verdict
  escalates to a human instead of auto-accepting (confidence cascade).
  The judge is only consulted for a proposal that actually changed the
  input — there is nothing to accept on a no-op (see above).

## Pointing at another jev-compatible API

The flow never names a provider — endpoint and model ride env, and the
contract is just `POST {state,model,questions}`:

| Env | Default (keyless SimpleJev demo) | Custom example |
|---|---|---|
| `JEV_API_URL` | `https://simple-jev-demo-api.featherless.ai/v1/classifier` | `https://api.typesafe.ai/v1/systemone`, `http://127.0.0.1:8000/v1/classifier` (self-hosted), bare `host:port` (expands to `/v1/systemone`) |
| `JEV_MODEL` | `featherless-ai/Qwen3.6-35B-A3B-classifier` | `jev-latest`, any served ID |
| `JEV_API_KEY` | empty (no `Authorization` header) | operator key in the age-encrypted secrets file |
| `JEV_TIMEOUT` | `10s` | per-attempt HTTP timeout |
| `JEV_CONFIDENCE_THRESHOLD` | `0.7` | recalibrate (below) when changing backends |

The demo endpoint is rate-limited (2k context, 2 RPS) — fine for trying,
not for production. Production wants a `featherless.ai` account (or the
open-source self-host) plus the calibration below.

## Calibrating the confidence threshold

Before trusting auto-accept on a new backend: label ~20 cases by hand
(10 publish / 10 revise), run the judge, measure agreement (target ≥70%
on the binary decision), freeze the threshold in `JEV_CONFIDENCE_THRESHOLD`.
Re-run when the backend, model, or rubric changes. An uncalibrated judge
is noise with confidence attached.

## Removal

the `review` installer unit (dropped by the `minimal` and `no-ai` presets), or
by hand: delete `features/review/` + the `bootReview` helper, `reviewH`
lines and `/review` mounts in `internal/server/chi.go` (+ the
`newReviewHandlerForTest` fixture in its test) + `go mod tidy`.

## Why no LLM gateway (Bifrost) yet

An LLM gateway ([Bifrost](https://github.com/maximhq/bifrost),
Apache-2.0) was evaluated and **deferred — not rejected, and not
implemented**. One provider, provider-aware retry already in `goai`,
budgets already in ai-credits: a gateway would add a control plane, its
own log store, and a large dependency for zero current need.

Verified facts (scratch probe + upstream docs, 2026-10-13; `core`
v1.12.0 — do not confuse with the host's `bifrost-stdio` service, which
is BB's MCP gateway, a different project sharing the name):

- **Embeds cleanly**: `go get github.com/maximhq/bifrost/core` →
  `bifrost.Init` + `Shutdown` (exit 0), `CGO_ENABLED=0` builds, so the
  zero-CGO rule holds. 140 modules in the graph, **~+32MB dev / ~+24MB
  stripped (2x the Genkit delta)**, carrying AWS + Azure SDKs, gRPC,
  sonic, fasthttp and mcp-go even when only OpenRouter is used.
- **OpenRouter is a first-class provider** (chat + stream) — the current
  setup enters directly, no adapter.
- **Observability is opt-in**: without the logging plugin there are
  **zero writes** (zero-artifact holds by default). With it:
  SQLite/Postgres, async (<0.1ms), `x-request-id` becomes the log ID,
  `x-bf-lh-*` headers become metadata, `attempt_trail` records every
  attempt and key rotation. OTel / Prometheus / Datadog are connectors.
  No UI in embedded mode (that belongs to gateway mode).
- **MCP is mechanism, not a feature**: in-process client via `MCPConfig`
  exists, but each tool is permission + blast radius (allowlist, RBAC,
  audit, prompt-injection via tool output) — a separate feature with
  its own threat model.
- **Keys stay ours in embedded mode**: our `Account` implementation
  reads age-secrets/env and hands keys to Bifrost, which holds them in
  memory only. (Persisted key storage is a gateway-mode concept.)
- **Budgets are operator-side**: USD caps and rate limits per
  provider/key (customer→team→key hierarchy) answer "how much did the
  system spend" — ai-credits keeps the per-user wallet (grants,
  reserve/settle). Different functions that can coexist; the bug to
  avoid is per-user charging in both, or two different deny errors for
  one call.
- **If adopted, it sits *under* `llm.Client`** as smart transport, never
  beside or above it: one retry layer only (Bifrost's, with
  `attempt_trail` — goai's native retry switches off on that path),
  billing untouched, Genkit unaware.

Reconsider when: a second provider/key needs failover (e.g. OpenRouter
free quota killing the demo), the first tool-using feature arrives, or
measured cache-hit savings justify the weight. Next step then is a
one-day spike with real calls (usage mapping, streaming, fallback with
one bad key) — not a blind integration.
