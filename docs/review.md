# Review loop

Paste text or a diff on `/review` — a writer and a critic iterate (bounded),
then a classifier judge delivers the verdict. Text is the default tab
(no code literacy needed, and classifier judges are most accurate on
prose); code is the second tab.

## How it runs

Synchronous in the request scope: one POST streams phased progress over
ephemeral SSE and patches a single verdict fragment. Disconnect cancels
the run; `MaxRunDuration` (90s) bounds it either way. Nothing persists —
no queue jobs, no DagNats runs, no database writes. The billing ledger
is the only write (generative calls via `llm.Client`'s meter when wired;
the judge call is recorded once when a judge meter is wired).

The loop needs a real LLM (`GOAI_API_KEY`): without one the page shows a
setup hint and the run API answers 503. There is deliberately no
simulated path — the template's AI surface is real-LLM only.

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

`gogogo trim review` (dropped by the `minimal` and `no-ai` presets), or
by hand: delete `features/review/` + the `bootReview` helper, `reviewH`
lines and `/review` mounts in `internal/server/chi.go` (+ the
`newReviewHandlerForTest` fixture in its test) + `go mod tidy`.
