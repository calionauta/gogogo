# Troubleshooting

Symptoms and where they actually come from. The most common gogogo issues are
**processes orphans segurando portas** and **assets stale** — not logic bugs.

## A browser tab opens at `127.0.0.1/_/#/pbinstall/…` during tests

PocketBase's first-run installer (`apis.DefaultInstallerFunc`) mints a
`pbinstall` token and calls `osutils.LaunchURL` — it **opens a browser** on the
machine running the process. A test that boots the real binary with a throwaway
`DATA_DIR` has no superuser, so it hits this on every run. The tab usually lands
before the server has bound, which is the "This site can't be reached /
127.0.0.1 refused to connect" variant.

`router.Init` suppresses it whenever `interactive()` is false, and
`interactive()` requires **stdin AND stdout to be real terminals**. The
subtlety that made this recur: a child process inherits *the parent's* TTY
status, so `go test` (or `make test`) launched from a real terminal produces a
child that also looks interactive. Detection alone cannot cover that case —
every test that spawns the binary must also set the explicit override:

```go
// Build the child env explicitly; do not append to os.Environ(), which may
// carry a GOGOGO_NO_BROWSER=0 that would re-enable the launch.
cmd.Env = []string{ ..., "GOGOGO_NO_BROWSER=1" }
```

`GOGOGO_NO_BROWSER=0` forces interactive (escape hatch for PTY-less
automation). To confirm no browser is launched, put a shim early on `PATH` and
check whether it fires:

```bash
mkdir -p /tmp/shim && printf '#!/bin/bash\necho "LAUNCHED: $*" >> /tmp/shim/open.log\n' > /tmp/shim/open && chmod +x /tmp/shim/open
PATH=/tmp/shim:$PATH make ci-local
cat /tmp/shim/open.log   # empty = correct
```

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

### `css-check` fails with NO source change (and `make css` will not fix it)

You edited nothing, `git status` is clean apart from
`web/resources/static/app.min.css`, and rebuilding to an identical-looking
bundle does not clear it. Cause: **a stale `node_modules`.**

`package-lock.json` pins the exact Tailwind and DaisyUI versions, so the
committed bundle is reproducible only against those versions. An older
`node_modules` (left by a previous session, or restored from a backup) builds a
bundle that differs in whitespace, ordering and component CSS — `git diff` looks
like the whole file changed, and re-running `make css` cannot fix it because the
version is wrong, not the content.

```bash
node -e 'console.log(require("node_modules/tailwindcss/package.json").version)'
# compare against package-lock.json -> node_modules/tailwindcss.version
make css-install   # reinstalls only when the versions disagree
make css-check     # goes green
```

`make css-install` detects this itself (it compares the installed versions
against the lockfile) and `bin/check-css.sh` builds through `make css`, so in
practice this is self-healing. The reason to know it is that the FIRST symptom
looks like a broken repo rather than a stale dependency, which is how a real
tailwindcss 4.3.2-vs-4.3.3 mismatch got misread as "the committed CSS is
stale".

If the versions already match and it still fails, the bundle really is stale —
rebuild, do not investigate the lockfile further.

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