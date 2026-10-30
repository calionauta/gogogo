# Security model

This page is the map, not the manual. It names the control, where it is
enforced, and the page that owns the details. The code and the named tests are
the source of truth.

## Controls at a glance

| Area | Control | Enforced in |
|---|---|---|
| **Passwords and sessions** | argon2id hashes, opaque session tokens (only the SHA-256 reaches the DB), `HttpOnly` + `SameSite=Lax` cookie, `Secure` in production (cleared by the desktop shell, whose webview loads loopback HTTP), absolute seven-day TTL, optional idle timeout | `internal/auth`; [Admin & Dashboard](admin-dashboard.md#app-session-cookie-why-one) |
| **Authorization** | Casbin route + verb policy, a handler session check on every private route, owner-scoped SQL for records, always-on operator fences for `/config`, `/console`, `/dagnats`, `/debug/statsviz` and `/debug/pprof` | `internal/rbac`, `features/*/handlers`; [RBAC](rbac.md) |
| **Browser edge** | per-request CSP nonce (no `unsafe-inline`/`unsafe-eval` for scripts), HSTS outside dev, COOP/CORP, `X-Content-Type-Options`, `Referrer-Policy`, `X-Frame-Options`, same-origin guard on unsafe writes, no-store on auth responses | `internal/server/edge.go`; [Architecture](architecture.md#security-boundary) |
| **Credential changes** | password change, TOTP disable and sign-out-everywhere revoke sessions and pending magic links; recovery rotation invalidates the old batch. Sign-out-everywhere uses the password when set, a current authenticator/recovery code when TOTP is enrolled, or the session only when the account has no factor. TOTP disable and recovery rotation require the password or a current code on OAuth-only rows | `internal/auth/account.go`, `features/account` |
| **Admin mutations** | role changes and session revocation require the signed-in admin's password when set, or a current authenticator/recovery code on a passwordless admin; a failed proof is audited and the mutation is refused | `features/config`, `internal/auth/account.go` |
| **Email-bound links** | links are built from `APP_URL`, never the request `Host`; single-use purpose-scoped tokens; magic link GET is read-only and POST confirms; `MAGIC_LINK_BIND_BROWSER=true` is the strict same-browser opt-in | `internal/auth/email_lifecycle.go`, `magiclink.go` |
| **Desktop distribution modes** | `LICENSE_MODE=none\|license\|account` gates the app until an Ed25519 token verifies offline (bad stored token = gated boot, never half-open); `AUTH_MODE=open` (no login) refuses a non-loopback bind without `AUTH_OPEN_ALLOW_REMOTE=true`; the embedded NATS refuses a non-loopback listen without `NATS_TOKEN`, the cloud connection lives in `<DATA_DIR>/sync.json` (0600), never in the database, an explicit `NATS_LEAFNODE_URL` wins over it, `CLOUD_SYNC_ALLOW` can pin hosts, and device tokens are signed server-side with a bounded lifetime (offline revocation is impossible by construction) | `internal/license`, `internal/server/license_gate.go`, `features/license`, `features/account`, `internal/cloudcfg`; [Desktop & Mobile](desktop-mobile.md#activation-distribution-gate) |
| **Secrets** | project-scoped age file at `~/.secrets/<APP_NAME>.env.age`; deploy renders `~/.secrets/<APP_NAME>.env` (600); empty SMTP closes email flows | `internal/secrets`, `config`, deploy workflow; [Deploy](deploy.md#email-delivery-verification-reset-magic-links) |
| **Audit trail** | `security_events` rows for rejected/successful auth, credential changes, session revocation and admin mutations; 30-day retention pruned at boot and daily in 1000-row batches; admin panel and filtered CSV | `internal/auth/securitylog.go`, `internal/server/boot_helpers.go`, `features/config`; [Admin & Dashboard](admin-dashboard.md) |
| **CSP reporting** | `report-uri /api/security/csp-report`, 64 KiB cap, control-character stripping and query/fragment removal before logging | `internal/server/csp_report.go`, `edge.go` |
| **Rate limiting** | per-session in-process limiter for authenticated traffic; auth endpoints have their own budgets; the edge proxy owns fleet-wide and IP limits | `internal/server/ratelimit.go`; [Configuration](configuration.md) |
| **Supply chain** | `go.sum`, `govulncheck`, `gitleaks`, pinned toolchain, SBOM + Grype image scan on a schedule | [Supply-chain scanning](research/supply-chain-scanning-2026.md) |

## Production defaults

- `RBAC_ENABLED=true` in production; development starts open for single-tenant work.
- `SIGNUP_ENABLED=false` and `DEMO_ACCOUNTS_ENABLED=false` in production. A public demo opts in explicitly; the publishable demo pair can be enabled without opening signup.
- `APP_URL` is required for any link that leaves the process; a missing value fails closed, not back to `Host`.
- `ADMIN_EMAILS` is empty by default, which closes `/console`, `/dagnats` and the opt-in `/debug/statsviz` + `/debug/pprof`. `/config` still needs an admin role.
- Empty `SMTP_ADDR`/`SMTP_FROM` leaves verification, reset and magic-link delivery at 503.

## Residual risks and deliberate boundaries

- **The in-process limiter is per-session, not per-IP.** Behind a tunnel every request arrives from the proxy, so the edge owns IP/fleet throttling and login throttling.
- **The CSP report endpoint is anonymous by design.** Browsers post without a session; the edge should bound the request rate. The body and logged fields are capped, but the endpoint is still a write sink.
- **Audit retention is age-only.** A sudden event volume can grow the table inside the 30-day window; a max-row cap is a future belt-and-suspenders.
- **Mailpit is a local capture sink.** Its loopback-only inbox contains live magic-link/reset tokens; it must never be published. The app has no visitor-facing inbox.
- **Name-scoped secrets are not a sandbox.** Anything running as the same OS user can read `~/.secrets`; isolate untrusted projects with another user or container.
- **No real email in the public demo.** Magic, verification and reset stay closed until a provider and domain are configured; password login is the demo path.
- **A passwordless account with no TOTP signs out with the session alone.** There is no password and no factor to prove; TOTP-enrolled accounts must supply the current code for sign-out-everywhere.

## Where to change what

- New route, handler or record access: [RBAC](rbac.md) + the invariants below.
- Session, password, TOTP or magic-link behaviour: `internal/auth` and [Admin & Dashboard](admin-dashboard.md).
- Headers, CSP, origin guard, limiter: `internal/server/edge.go` and `internal/server/ratelimit.go`.
- Secrets, SMTP and deploy variables: [Deploy](deploy.md#email-delivery-verification-reset-magic-links) and [Configuration](configuration.md).
- Audit store, panel, CSV, retention: [Admin & Dashboard](admin-dashboard.md) and the migration/query files under `internal/db`.
- Tests and gates: [Local CI](local-ci.md); security changes need RED/GREEN/PROVE evidence.

## Change checklist

The normative checklist lives in [AGENTS.md](../AGENTS.md#security-invariants-agent-gate). The short version:

- **Every route**: session check, a direct `user` policy row, and an `adminOnlyRoutes`/`publicRoutes` decision.
- **Every record**: owner-scoped SQL, with deletes through `canDeleteDoc`/`canDeleteRow`.
- **Every sink**: Templ escaping, sqlc parameters, `os.Root` for paths, no request-derived URL/command.
- **Every inline script**: the per-request nonce and `data-nonce`.
- **Every password rotation, TOTP removal, and sign-out-everywhere**: revoke
  sessions and pending links. Require a fresh proof for destructive account actions.
- **Every admin mutation**: verify the admin's password or current code before
  changing a role or revoking sessions; audit a failed proof and refuse.
- **Every new dependency**: pass [dependency principles](dependency-principles.md), pin it, run `govulncheck`, and record the trade-off.
