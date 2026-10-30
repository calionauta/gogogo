# Desktop distribution: license gate, accounts, and sync

Reviewed: 2026-10-30

The desktop shell today is local-first: its own Turso file, its own accounts,
and no relational replication. Cross-instance convergence exists only as events
(todo CRUD on `app.crud.todo.>`, Loro docs on `app.sync.<docID>`), and
`bridge.tenantOf` **skips** any op whose owner id is absent locally.

That raises a product question: should the product owner decide whether a
distributed build requires activation, and how does "connect to the server"
fit without forking identity? This page records what the 2026 landscape offers,
what fits gogogo, and the staged path.

## What the ecosystem settled on

**Offline licensing is a solved pattern.** Keygen, Cryptlex and LicenseSpring
all converge on the same shape: a **signed license file** verified locally, so
activation needs no network after the fact. Keygen recommends Ed25519 (or
ECDSA P-256 for FIPS); air-gapped installs use request/response files. The
taxonomy to pick from is perpetual, timed, floating, node-locked and feature
licenses.

**Device pairing is the standard "log in once elsewhere" flow.** RFC 8628
(device authorization grant) is what TVs and CLIs use: the device shows a short
code, the user approves it on a browser, the device polls for a token and then
works offline. Caveat from 2025–26: device-code phishing is an active attack
class, so show what is being authorized and require explicit confirmation.

**Local-first is mainstream; "sync engine" is not "local DB".** Turso Sync
(embedded replicas / push-pull), PowerSync, Electric and Zero all ship sync as a
product. Turso's own Go SDK (`tursogo`) exposes push/pull sync, which matters
here because it is already the driver in this repo.

**Enforcement on a user-controlled machine is deterrence.** A signed license
raises the cost of copying; it is not DRM. Price the gate accordingly.

## Options

| model | works offline after activation | revocation | multi-device | owner can choose it |
|---|---|---|---|---|
| no gate (open local) | n/a | none | unlimited | yes |
| license key → signed offline license | yes | weak (no phone home) | per license policy | yes |
| account + device token (pairing) | yes (cached token) | strong | seat-bound | yes |
| cloud-only features (sync needs account) | local free | strong | yes | yes |
| remote DB as the only source | no | strong | yes | no (kills offline) |

## Recommendation

**Two axes, not a mode switch.** The mistake to avoid is "local login *or*
server login": two identity sources produce the skip bug above.

- **Axis 1 — distribution gate**, chosen by the owner per release:
  `LICENSE_MODE=none | license | account`. It gates *features*, never local
  accounts; a local user row always exists.
- **Axis 2 — cloud sync**, optional and explicit. Connecting must adopt the
  server's `user.id` (or migrate local rows to it), because ops from an unknown
  owner are discarded by design.

Staged implementation:

1. **`none`** — today, plus the first-run signup bootstrap already shipped.
2. **`license`** — Ed25519-signed license file, public key compiled in, stored
   `0600`, optional expiry and optional device binding. Verified offline;
   never phone home silently.
3. **`account`** — RFC 8628 pairing: the desktop shows a code, the server
   returns the canonical user id, the local row is created with it, the token
   lives in the OS keychain.
4. **sync** — leaf node for the event wires, with identity aligned. Evaluate
   Turso Sync as the alternative: it replicates the whole database (users
   included), which removes the owner-id problem instead of aligning it.

## Where the UI goes

Not `/config`: it is operator-gated by `ADMIN_EMAILS` and unreachable on a fresh
desktop. Server connection belongs in app settings (or a first-run wizard),
requiring the local password before writing, storing credentials in the
keychain or an age file — never in the web UI's database. A URL typed by a user
is an outbound destination: validate scheme/host and show it before saving.

## What shipped (2026-10-30)

- **Axis 1 — `LICENSE_MODE`** `none | license | account`, enforced by a gate
  before the router: pages redirect to `/activate`, APIs get a JSON 403, static
  + health stay open. Tokens are Ed25519-signed (`gogogo1.<payload>.<sig>`,
  optional `exp`, optional `sync`), verified locally in `internal/license` and
  minted by `cmd/license keygen|mint|verify`. A stored token that fails
  verification boots gated, never half-open.
- **`account` activation adopts identity**: the local user row is created with
  the token's `sub` and the response signs that user in, which is the supported
  way to make later CRUD sync join instead of being skipped by `tenantOf`.
- **`AUTH_MODE=open`**: no login at all, one fixed local owner; refused beyond
  loopback without `AUTH_OPEN_ALLOW_REMOTE=true`.
- **Axis 2 — cloud connection** at `/account` (not `/config`): URL + device
  token → `<DATA_DIR>/sync.json` (0600), applied by the desktop at the next
  launch as the NATS leaf node; an explicit `NATS_LEAFNODE_URL` wins.

**Gap decisions (2026-10-30).** Each remaining gap is either closed or
deliberately deferred with the reason:

| gap | decision |
|---|---|
| Server-side issuance | **closed** — `/account` → *Device token* signs a token for the signed-in user (`LICENSE_PRIVATE_KEY`, `CLOUD_SYNC_URL`); no owner CLI needed. |
| Transport to a public cloud | **closed for the safe path** — `NATS_LISTEN`/`NATS_TOKEN` with a guard, tested over a private overlay; public exposure still needs TLS + per-device credentials (documented, not enabled). |
| Revocation | **bounded, not instant** — `DEVICE_TOKEN_DAYS` + re-issue; offline verification cannot consult a live list. |
| Password after activation | **closed** — the activation form accepts an optional local password (min 8). |
| Open-mode leftovers | **closed** — `/login` and `/login` POST redirect to `/`; account security cards are hidden. |
| Arbitrary sync host | **closed** — `CLOUD_SYNC_ALLOW` allowlist, checked on save. |
| Mode-switch orphans | **documented** — activation happens before use; switching after use requires a deliberate export/import. |
| Multi-user / workspaces | **deferred** — a product phase (invites, roles, shared rows), not a sync bug. Convergence already needs identical ids. |
| Turso Sync | **deferred** — beta; a spike must measure conflicts before replacing the event wires. |
| Signing / notarization / mobile | **prepared, not wired** — portable transport/token; mobile needs Keystore + deep-link activation + WebView verification. No SDK is installed, so no spike can claim support yet. |

Still open, in the order they block real use:

1. **Device pairing UX (RFC 8628)** — today an account token is delivered
   out-of-band and pasted. Pairing removes the copy-paste and rotates tokens.
2. **Reaching the central NATS from the internet** — the leaf node assumes a
   trusted path. A public cloud needs TLS + per-device credentials (NKeys/JWT)
   or an HTTP sync edge. This is the largest unbuilt piece.
3. **Turso Sync spike** — it would replicate users too and remove the
   identity-alignment constraint; still beta, needs conflict measurements.
4. Trial mode, seats, and license rotation/revocation.

## Open questions

- Trial mode before a license, or straight to activation?
- Local multi-account in the no-sync case, or one profile per machine?
- Turso Sync is still beta (offline writes were a private beta): a spike has to
  measure conflict behavior before it replaces the event wires.

## Sources

- [Keygen — offline licenses](https://keygen.sh/docs/choosing-a-licensing-model/offline-licenses/)
- [Cryptlex — offline licenses](https://cryptlex.com/docs/licensing-models/offline-licenses)
- [LicenseSpring — offline licensing](https://licensespring.com/blog/tutorials/offline-licensing)
- [RFC 8628 — OAuth 2.0 Device Authorization Grant](https://www.rfc-editor.org/info/rfc8628/)
- [Microsoft — device code flow abuse](https://techcommunity.microsoft.com/blog/coreinfrastructureandsecurityblog/device-code-flow-the-gift-that-keeps-on-giving-%E2%80%94-to-attackers/4540949)
- [Turso — local-first sync guide](https://turso.tech/blog/building-local-first-apps-the-complete-guide-to-offline-first-database-sync)
- [Turso — Go SDK reference (tursogo sync)](https://docs.turso.tech/sdk/go/reference)
- [PowerSync — local-first software](https://docs.powersync.com/resources/local-first-software)
