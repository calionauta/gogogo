# Troubleshooting

Symptoms and where they actually come from. The most common gogogo issues are
**processes orphans segurando portas** and **assets stale** — not logic bugs.

## Tests fail with "failed to register onboarding workflow"

An old `go run` / `web` binary is still holding `:18099` or `:4224`.

```bash
pkill -x web
# or
lsof -ti :18099 | xargs kill
```

Then re-run. `make test` serializes packages with `-p 1` for DagNats engine
stability, but a leftover process on the port defeats that.

## The commit was aborted but every hook job showed green

The lefthook jobs run in parallel and the summary prints **last**. A non-zero
exit with all-green checkmarks means one job failed in the middle of the
output. The most common cause is `mod-tidy`: `go get <mod>@<version>` leaves
stale lines in `go.sum`.

```bash
go mod tidy && git add go.mod go.sum && git commit
```

## CSS looks stale / a style change did nothing

Editing a `.templ` without regenerating leaves `web/resources/static/app.min.css`
out of date.

```bash
make templ && make css
```

The pre-commit hook does this for you, but `css-check` can pass by inertia when
nobody rebuilt — so if you bypassed the hooks, rebuild explicitly.

> Do not "touch" a class in test data or prose to make a check pass.
> Tailwind's content scanner reads `.templ`, `.go`, and anything matched by the
> `@import "tailwindcss" source(...)` directive — a `class="…p-1…"` accidentally
> emitted as a utility creates spurious CSS and fails `css-check`.

## Realtime events arrive for other users, or not at all

Two separate mechanisms, and mixing them up is the usual cause:

- **Record mutations** flow through PocketBase's native `/api/realtime`, scoped
  by the collection's `ListRule`/`ViewRule`. If delivery is wrong, check the
  rule — `@request.auth.id != '' && owner = @request.auth.id`.
- **Ephemeral signals** (toasts, client count, workflow progress) flow through
  the SSE Hub.

If realtime is silently dropped, check that the client has **both** cookies —
see [Admin & Dashboard](admin-dashboard.md#app-session-cookie-vs-pb_auth-why-two).
Without `pb_auth` alongside `gogogo_auth`, PB's per-subscriber access check
rejects the SSE channel and record events vanish with no error.

## "I changed the UI and nothing happened"

Most likely one of:

1. **Stale binary.** `pkill -x web` and `make dev` again.
2. **Stale templ.** `make templ` — editing a `.templ` does nothing until the
   `_templ.go` is regenerated.
3. **Stale CSS.** `make css`.
4. **Datastar patch with no target.** A `PatchElements` whose top-level element
   lacks an `id`, or has no `WithSelector`, throws
   `PatchElementsNoTargetsFound` in the browser console. Pair
   `internal/datastar.RenderAndPatch` with an explicit selector.

## `go build -tags …` succeeded but the feature is missing at runtime

This is the silent-drift failure mode of the unified-build era. There are no
build tags any more. If you are passing `-tags jetstream dagnats`, you are
building a stale configuration that no longer reflects what ships. `make build`
is just `go build ./cmd/web` and includes everything.

## The DagNats workflow is stuck in `running`

Kill the server mid-run and restart — the engine resumes at the last incomplete
step, which is the whole point of durable state on JetStream. If it does not
resume, DagNats ≥ v0.0.20 fixed a bug where grouped-step retries never fired
and left a run stuck forever; make sure you are on a current version (see
`go.mod`).

If you are on a current version and it still sticks, inspect the run at the
console — see [Admin & Dashboard](admin-dashboard.md#dagnats-console).

## The deploy says green but the live app is old

Byte-diff an embedded asset:

```bash
diff <(curl -s https://<host>/static/app.min.css) \
     <(git show HEAD:web/resources/static/app.min.css)
```

For the version badge, compare what the navbar renders against the tag you
built. There is no `/api/version` endpoint — a request for one returns 404, so
do not use it as a health check:

```bash
# what the repo says the current tag is
VERSION=$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')
# what the running box reports: read BuildLabel/BuildCommit off the navbar,
# or grep the served HTML
curl -s https://<host>/ | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1
```

A mismatch means the deploy did not run or the tunnel is serving a cached
response. Static assets are served with `Cache-Control: public, max-age=0,
must-revalidate` and a content-hash ETag, so a stale CSS in the browser means a
stale deploy, not a cache.

## The deploy workflow fails on the server

Two known gotchas:

- **Permission denied writing the container dir.** The `deploy` user is not
  root — grant access with `setfacl`/`chmod`, never `chown`.
- **`git pull --ff-only` aborts.** Never `scp` into the server's repo clone; the
  deploy workflow pushes to a fresh checkout instead.

## Related

- [Local CI](local-ci.md) — the gate and its tiers.
- [Configuration](configuration.md) — every env var and its default.