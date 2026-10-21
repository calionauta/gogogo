# Auth libraries: limen, and why the commodity part is owned

Reviewed: 2026-10-21

**Question.** Should password login, sessions and the auth cookie come from a
library, or be owned?

**Evaluated: `github.com/thecodearcher/limen`.** A composable auth library:
sessions, cookies, CSRF and rate limiting as the base, with magic link, OAuth and
2FA as plugins. It fits Turso through its `database/sql` adapter, so the engine
was never the obstacle.

## What it gives

Every auth flow an app eventually wants, behind one dependency: session and
cookie handling, CSRF, rate limiting, plus the flows that usually mean writing
your own — magic link, one-time codes, social login.

## Why it is not the template's default

Three costs, in the order they bite.

- **Pre-1.0 across three version-skewed modules.** Three surfaces to track, in
  every generated project, on a library still finding its shape.
- **A second schema.** Five tables with its own columns and `INTEGER` ids,
  against the template's `TEXT` + UUIDv7 convention. Migrations and
  `dbschema.NewID()` stop being the single answer, and two id spaces is the kind
  of split that surfaces later as a join that cannot be written.
- **Its breadth needs infrastructure anyway.** Magic link needs an email sender,
  TOTP needs enrollment state, OAuth needs client ids. A library cannot remove
  that work, only the syntax around it.

## Decision

Own the commodity part. `internal/auth` is roughly 300 lines: argon2id hashes
(`golang.org/x/crypto`, already a dependency), random 32-byte session tokens of
which only the SHA-256 reaches the `sessions` table, and one `gogogo_auth`
cookie. No auth dependency, no third schema, nothing to rotate.

## Revisit trigger

An app that needs magic link, TOTP or OAuth across several projects, where
tracking a young library costs less than owning those flows, and after the
schema and id-convention question is settled or bridged. Until then limen stays
the documented upgrade plugin for exactly that moment, not the default.

## Related

- [Architecture decision: auth](../architecture-decision.md#auth-owned-argon2id-with-the-library-path-written-down)
- [Auth plugins the template owns](../architecture-decision.md#auth-plugins-owned-magic-link-totp-oauth) — magic link, TOTP and OAuth ship in the template, so the trigger above is about a library doing them better, not about needing them at all.
