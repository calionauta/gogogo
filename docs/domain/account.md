# account

**Owns.** The caller's own settings surface: profile, password change, session list with revoke and revoke-all, device tokens, and the cloud connection (`/account`, `/account/*`).
**Does not own.** Credential verification and session issuance (`auth`), roles and policy rows (`rbac`), or the shell's theme preference (`ui`).

## Language

| term | means | not |
|---|---|---|
| session | one signed-in device, revoked individually or all at once | a login attempt |
| device token | a long-lived credential bound to one device | an API key for integrations |
| step-up | re-proving the current password before a sensitive change | a role check |

## Invariants

- The subject is always the session's user; no route takes a user id from the request.
- Password changes and sign-out-everywhere require a step-up proof and write an audit event.
- Revoking a session invalidates it immediately, including its SSE streams.
- The page never renders a secret — tokens are shown once, at creation.
