# landing

**Owns.** The public entry page: what this build is, the stack it ships, and the links into the app (`/`).
**Does not own.** Authentication pages (`authviews`), the docs site (`site/`), or any record — the page reads no table and writes nothing.

## Language

| term | means | not |
|---|---|---|
| landing | the page a visitor sees before signing in | the dashboard |
| stack | the modules this build actually mounts | a hardcoded feature list |
| entry | one link into a mounted surface | a menu of everything upstream ships |

## Invariants

- The page renders without a session and never queries a user-scoped table.
- What it advertises comes from the mounted units, so a trimmed build cannot link to a route it dropped.
- It performs no writes.
