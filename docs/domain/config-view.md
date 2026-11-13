# config-view

**Owns.** The read-only operator page: the safe view of configuration, the capability table (which units are in, mounted and removable), the security-event audit view, and the CSV export (`/config`, `/config/audit.csv`).
**Does not own.** Writing secrets (`secrets`), storing RBAC policy (`rbac`), or mutating units — removal is an installer operation, not a page action.

## Language

| term | means | not |
|---|---|---|
| safe view | config values with secrets masked | the raw environment |
| unit row | one capability with its kind, status and removal class | an enabled/disabled flag |
| security event | one audited rejection or mutation, with subject and actor | a log line |

## Invariants

- Every route here is admin-only, enforced by policy before the handler runs.
- A secret is never rendered, logged, or exported — the view shows presence, not values.
- The capability table reads `capabilities.All`; it cannot drift from the registry.
- Audit rows carry event, reason, subject and actor; never a password, token or code.
