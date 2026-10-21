# Troubleshooting

Symptoms and where they actually come from. The most common gogogo issues are
**processes orphans segurando portas** and **assets stale** — not logic bugs.

## An open tab reloads itself once right after a deploy

Expected, not a bug. A tab open across a deploy holds a DOM rendered by the
old binary; the new server's element IDs and signals may no longer match it,
so morphs would land nowhere. Authenticated pages compare the server's build
tag (`X-Gogogo-Build` on `/health`) on load and on return-to-foreground and
reload once when it changes. In dev the tag is constant, so Air rebuilds never
trigger it. Single-binary deploys only: alternating builds (rolling deploy
without affinity) would reload on every flip.

## A test binary seems to hang at boot (no browser, no output)

The flat binary opens no browser and takes no CLI subcommands — address +
data dir ride env (`HOST`/`PORT`, `TURSO_DB_PATH`, `DATA_DIR`). A spawned
binary that never becomes healthy is almost always missing env: it binds
`0.0.0.0:8080` and opens `data/` relative to the *child's* cwd. Pin both
to a temp dir in the test (see `cmd/web/smoke_test.go`).

Keep `GOGOGO_NO_BROWSER=1` set explicitly in the child env anyway (built
from scratch, not `append(os.Environ(), …)` — an inherited `=0` would win):
cheap insurance if a launcher ever returns.

## Tests fail with "failed to register onboarding workflow"

An old `go run` / `web` binary is still holding `:18099` or `:4224`.

```bash
pkill -x web
# or
lsof -ti :18099 | xargs kill
```

Then re-run. Two things can put a stale listener on `:18099`/`:4224`: a
leftover `go run`/`web` binary, or **two packages binding the same fixed port**
under `-p N`. The suite no longer has the second problem — every DagNats test
binds an ephemeral HTTP port now (see `docs/local-ci.md`), so this is almost
always the orphan-process case.

## The commit was aborted but every hook job showed green

The lefthook jobs run in parallel and the summary prints **last**. A non-zero
exit with all-green checkmarks means one job failed in the middle of the
output. The most common cause is `mod-tidy`: `go get <mod>@<version>` leaves
stale lines in `go.sum`.

```bash
go mod tidy && git add go.mod go.sum && git commit
```

## CSS looks stale / a style change did nothing

Editing a `.templ` without regenerating leaves the committed CSS bundles out of
date.

```bash
make templ && make css-all
```

The pre-commit hook does this for you, but `css-check` can pass by inertia when
nobody rebuilt — so if you bypassed the hooks, rebuild explicitly.

> Do not "touch" a class in test data or prose to make a check pass.
> Tailwind's content scanner reads `.templ`, `.go`, and anything matched by the
> `@import "tailwindcss" source(...)` directive — a `class="…p-1…"` accidentally
> emitted as a utility creates spurious CSS and fails `css-check`.

### `css-check` fails with NO source change (and `make css-all` will not fix it)

You edited nothing, `git status` is clean apart from a file under
`web/resources/static/`, and rebuilding to an identical-looking bundle does not
clear it. Cause: **a stale `node_modules`.**

`package-lock.json` pins the exact Tailwind and DaisyUI versions, so the
committed bundle is reproducible only against those versions. An older
`node_modules` (left by a previous session, or restored from a backup) builds a
bundle that differs in whitespace, ordering and component CSS — `git diff` looks
like the whole file changed, and re-running `make css-all` cannot fix it because
the version is wrong, not the content.

```bash
node -e 'console.log(require("node_modules/tailwindcss/package.json").version)'
# compare against package-lock.json -> node_modules/tailwindcss.version
make css-install   # reinstalls only when the versions disagree
make css-check     # goes green
```

`make css-install` detects this itself (it compares the installed versions
against the lockfile) and `bin/check-css.sh` builds through `make css-all`, so
in practice this is self-healing. The reason to know it is that the FIRST symptom
looks like a broken repo rather than a stale dependency, which is how a real
tailwindcss 4.3.2-vs-4.3.3 mismatch got misread as "the committed CSS is
stale".

If the versions already match and it still fails, the bundle really is stale —
rebuild, do not investigate the lockfile further.

## Realtime events arrive for other users, or not at all

Two separate mechanisms, and mixing them up is the usual cause:

- **Record mutations** fan out twice per mutation: Hub record event to the
  owner's streams (no-echo on `clientID`, except offline replays which reach
  everyone) + JetStream CRUD op cross-instance, then each stream re-lists
  scoped by owner. If delivery is wrong, check the owner scoping in
  `broadcastRecord` and the exclusion rule — a replay stuck on its
  optimistic row means fan-out sits behind the replay short-circuit again.
- **Ephemeral signals** (toasts, client count, workflow progress) flow through
  the SSE Hub.

If realtime is silently dropped, check the fan-out path: the mutation must
reach `publishSync` (Hub record event + JetStream op) *before* any early
return, and the stream's `clientID` must not be excluded unless it is the
live originating tab — replays reach everyone (see
[Admin & Dashboard](admin-dashboard.md#app-session-cookie-why-one) for the
single-cookie model).

## "I changed the UI and nothing happened"

Most likely one of:

1. **Stale binary.** `pkill -x web` and `make dev` again.
2. **Stale templ.** `make templ` — editing a `.templ` does nothing until the
   `_templ.go` is regenerated.
3. **Stale CSS.** `make css-all`.
4. **Datastar patch with no target.** A `PatchElements` whose top-level element
   lacks an `id`, or has no `WithSelector`, throws
   `PatchElementsNoTargetsFound` in the browser console. Pair
   `internal/datastar.RenderAndPatch` with an explicit selector.

## `go build -tags …` succeeded but the feature is missing at runtime

This is the silent-drift failure mode of the unified-build era. There are no
build tags any more. If you are passing `-tags jetstream dagnats`, you are
building a stale configuration that no longer reflects what ships. `make build`
is just `go build ./cmd/web` and includes everything.

## Scaffolding refuses: "this tree needs go X, but GOTOOLCHAIN pins go Y"

The `go` directive in `go.mod` is enforced at **patch** level: a go1.27.2
toolchain will not build a tree that declares `go 1.27.3`. The Go default
(`GOTOOLCHAIN=auto`) downloads whatever the tree asks for, so this only bites
when the setting is pinned:

```bash
go env GOTOOLCHAIN           # go1.27.2 or local => no self-upgrade
go env -w GOTOOLCHAIN=auto   # the fix
```

The installer checks this before it mutates anything
(`checkToolchainForTree` in `internal/installer/requirements.go`), so a
refused run leaves the checkout untouched: no trimmed wiring, no renamed
module, no half-written feature.

## The DagNats workflow is stuck in `running`

Kill the server mid-run and restart — the engine resumes at the last incomplete
step, which is the whole point of durable state on JetStream. If it does not
resume, DagNats ≥ v0.0.20 fixed a bug where grouped-step retries never fired
and left a run stuck forever; make sure you are on a current version (see
`go.mod`).

If you are on a current version and it still sticks, inspect the run at the
console — see [Admin & Dashboard](admin-dashboard.md#dagnats-console).

## Browser writes return 403 but curl works

`sameOriginGuard` compares the browser's `Origin` header with `r.Host` for
unsafe methods. A reverse proxy that rewrites `Host` makes every browser
POST/PUT/PATCH/DELETE a 403, while curl and webhooks pass because they send no
`Origin`. Keep the original `Host` at the proxy; cloudflared and Caddy do. See
[Architecture](architecture.md#security-boundary).

## The deploy says green but the live app is old

**Start here — check whether the container was actually recreated:**

```bash
ssh "$DEPLOY_HOST" 'docker inspect gogogo \
  --format "started={{.State.StartedAt}} health={{.State.Health.Status}} image={{.Config.Image}}"'
```

A `started` older than the workflow run means the deploy did **not** apply, no
matter what the run concluded. This is the check that matters: `deploy-prod.sh`
used to swallow every `docker compose up` failure behind a `cmd || { ... }`
list (exempt from `set -e`) ending in `|| true`, so the script exited 0 and the
workflow reported **success** while the previous container kept serving. It
happened three times in one afternoon. Step 7 now exits non-zero and step 8
verifies health, so a green run should mean it landed — but the uptime check is
still the cheapest proof, and it is what to reach for when a fix "did not
work".

The two known causes of a silent no-apply, both from a repository rename:

- **A stale container holds the port.** The container name changed, so compose
  created a *new* container instead of replacing the old one, and the new one
  died with `Bind for 127.0.0.1:8080 failed: port is already allocated`. Both sat
  side by side. Fix: `docker rm -f <old-name> <new-name>` and re-deploy.
- **The secrets file went somewhere the compose does not read.** The render
  target and the compose `env_file` disagreed, giving
  `env file /home/deploy/.secrets/<app>.env not found`. Both sides now use
  `/home/deploy/.secrets/<app>.env`.

To compare what is *served* against the repo:

```bash
diff <(curl -s https://<host>/static/app.min.css) \
     <(git show HEAD:web/resources/static/app.min.css)
```

For the version badge, compare what the navbar renders against the tag you
built. Note that the badge alone cannot prove which commit is live: the deploy
derives `VERSION` from `git describe --tags`, so a commit without a tag reports
the *previous* tag. Prefer the container's start time.

There is no `/api/version` endpoint — a request for one returns 404, so do not
use it as a health check:

```bash
# what the repo says the current tag is
VERSION=$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')
# what the running box reports: read BuildLabel/BuildCommit off the navbar,
# or grep the served HTML
curl -s https://<host>/ | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1
```

A mismatch with `started` unchanged means the container is stale. Static assets
are served with `Cache-Control: public, max-age=0, must-revalidate` and a
content-hash ETag, so a stale CSS in the browser means a stale deploy, not a
cache.

## The deploy workflow fails on the server

Known gotchas:

- **Permission denied writing the container dir.** The `deploy` user is not
  root — grant access with `setfacl`/`chmod`, never `chown`.
- **`git pull --ff-only` aborts.** Never `scp` into the server's repo clone; the
  deploy workflow pushes to a fresh checkout instead.
- **`403 Unauthorized` from Tailscale during "Bring up Tailscale".** The runner
  authenticates with a GitHub OIDC token whose `sub` claim embeds the
  repository *name*. Renaming the repo (or the owner) invalidates the federated
  identity, so every deploy fails at login — while `TS_OAUTH_CLIENT_ID` and
  `TS_AUDIENCE` look untouched in GitHub. Fix on the Tailscale side: update the
  credential's subject, or create a new credential and re-set both secrets.
  Full write-up at the top of [Deploy](deploy.md).
- **Renaming the repo also leaves server-side artefacts behind** that no deploy
  can fix by itself: the old container holding the port, and the secrets path.
  See the rename callout in [Deploy](deploy.md).

**Reading the logs of a run that succeeded.** `gh run view <id> --log-failed`
prints nothing useful when the run concluded successfully but the deploy did not
apply — there is no failed step to show. Use the full log and read the deploy
step:

```bash
gh run view <id> --log | grep -iE "Starting new container|port is already|env file|timed out"
```

## Related

- [Local CI](local-ci.md) — the gate and its tiers.
- [Configuration](configuration.md) — every env var and its default.