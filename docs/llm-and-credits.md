# LLM and AI credits

The AI Suggest and Queue/Retry demos are wired through
[GoAI](https://github.com/zendev-sh/goai) and read their configuration from the
environment (or your age-encrypted secrets file). The optional
[`ai-credits`](https://github.com/calionauta/ai-credits) plugin adds billing on
top.

## Configuring the LLM

There are two paths.

### 1. A real OpenAI-compatible provider (recommended for production)

Set `GOAI_API_KEY`, point `GOAI_BASE_URL` at the provider's `/v1` endpoint, and
pick a `GOAI_MODEL`. Any OpenAI-compatible endpoint works — we do not hardcode
a provider, you choose. With a key present, the Todo UI shows the **Suggest**
button.

```bash
GOAI_API_KEY=sk-...
GOAI_BASE_URL=https://api.groq.com/openai/v1
GOAI_MODEL=llama-3.3-70b-versatile
```

### 2. Keyless simulated LLM (on by default in dev)

`SIMULATE_LLM` is enabled automatically in dev (no API key needed). It spins up
an in-process fake GoAI client that scripts a realistic failure (500 → retry →
slow → 200) so you can watch the retry feedback toasts end-to-end. The UI shows
a "Suggest (simulated)" button with the same `goqite` job + SSE feedback flow
as a real suggestion; its retries stay at the worker level so every attempt is
visible in the demo.

If neither `GOAI_API_KEY` is set nor `SIMULATE_LLM` is enabled, the AI suggest
route is **not registered** and the UI button is hidden. The Todo example keeps
working — AI is opt-in, not required.

> `internal/llm` wraps GoAI behind an injectable interface. It calls a **remote**
> provider API — it is not a local-model runtime. In tests, inject a stub rather
> than calling a real provider.

## AI credits & BYOK (optional)

[`ai-credits`](https://github.com/calionauta/ai-credits) keeps AI billing in the
same SQLite file as PocketBase: an immutable ledger with a materialized balance,
pricing by actual token usage, conservative reserve/settle for unknown-output
calls, lazy monthly entitlements, Stripe top-ups, and a reconciler. It stays
completely dormant unless enabled.

```bash
# Managed mode: Todo AI Suggest reserves before the call and settles at GoAI's
# actual usage. Insufficient balance blocks the call before it reaches a provider.
CREDITS_ENABLED=true
CREDITS_MONTHLY_CREDITS=1000

# Optional BYOK: user keys are encrypted at rest; calls pass through the
# OpenAI-compatible relay and are metered but never charged credits.
CREDITS_ENC_KEY="$(openssl rand -hex 32)"   # 64 hex chars = 32 key bytes
BYOK_PROVIDERS="openai=https://api.openai.com/v1,groq=https://api.groq.com/openai/v1"
```

With `CREDITS_ENABLED=true`:

- authenticated users get `GET /api/credits`,
- the real Todo **Suggest** path is metered in managed mode,
- the app exposes `POST /api/ai/request` for managed OpenAI-compatible calls.

When both BYOK vars are present, `POST /api/byok/{provider}/{path...}` injects
the user's encrypted key server-side and records upstream JSON/SSE token usage
with `billing_mode=byok` and `credits_charged=0`.

`CREDITS_ENC_KEY` accepts the normal `openssl rand -hex 32` representation (or a
legacy raw 32-byte value).

> The relay trusts the authenticated PocketBase user stamped by the app. **Do
> not expose `X-Auth-User` from an external proxy.**

For the library contract, schema, and security model see
[`ai-credits/docs/architecture.md`](https://github.com/calionauta/ai-credits/blob/main/docs/architecture.md).

## Related

- [Configuration](configuration.md) — every credits/BYOK variable.
- [The Todo example](todo-example.md#ai-suggest) — how the suggest button works.