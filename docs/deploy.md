# Deploy to your own box

> **Renaming the repository or the GitHub owner breaks the deploy until you
> re-authorize it.** The CI runner authenticates to Tailscale with a GitHub
> OIDC token, and that token's `sub` claim embeds the repository's *name*
> (`repo:<owner>/<name>:...`). Renaming the repo, transferring it, or moving it
> to another owner changes the claim, the existing federated identity no longer
> matches, and every deploy fails with:
>
> ```
> failed to exchange JWT for access token: 403 Unauthorized
> Visit https://login.tailscale.com/admin/settings/trust-credentials/view/***
> ```
>
> The failure looks like a broken secret but nothing on the GitHub side changed.
> Fix it by editing the federated identity's subject in the Tailscale admin
> console to match the new name, or by creating a new one and updating
> `TS_OAUTH_CLIENT_ID` / `TS_AUDIENCE`. Run `make rename` *before* wiring the
> deploy, not after.

> **Renaming also leaves the server in a state the deploy cannot fix by
> itself.** Two artefacts on the box carry the old name, and both must be
> cleared by hand once, after the rename:
>
> 1. **The container.** The old container (`<old-name>`) keeps holding the
>    published port, so the new one (`<new-name>`) fails to start with
>    `Bind for 127.0.0.1:8080 failed: port is already allocated`. The deploy
>    script reports that only as a healthcheck timeout, and the run still exits
>    green — so it looks like a success while the previous container keeps
>    serving. Remove it: `docker rm -f <old-name>`.
> 2. **The secrets file.** The compose `env_file` and the CI render target must
>    agree on `~/.secrets/<app>.env`. If they disagree you get
>    `env file /home/deploy/.secrets/<app>.env not found`, again masked as a
>    timeout. `make rename` renames the *repository*; it cannot rename a file on
>    a machine it has never seen.
>
> Both failures share a nasty property: **the deploy exits 0 and the site stays
> up on the old container.** Verify a deploy actually landed by checking the
> running container's uptime, not the workflow's conclusion:
>
> ```bash
> ssh $DEPLOY_HOST 'docker inspect <app> --format "{{.State.StartedAt}} {{.State.Health.Status}}"'
> ```
>
> A `StartedAt` older than the workflow run means the deploy did not apply.

The default workflow is **copy the template + `make rename` + `make dev`** for
local work. For a permanent
deployment, the project ships a production deploy workflow that publishes to a
server of your choosing — recommended: a small Linux box + Tailscale +
a Cloudflare-tunneled domain. No registry, no cold starts, full control.

## Server layout (multi-project standard)

Every project that adopts the pattern lives under `/home/deploy/services/` and
shares the same shape — siblings differ only by name:

```
/home/deploy/services/
└── gogogo/                ← this project
    ├── compose/
    │   └── docker-compose.prod.yml
    ├── env/
    │   └── .env                     ← non-secret env (DATABASE_URL, APP_URL, ...)
    ├── repo/                        ← git clone at master: the image build context,
    │                                   and the source of deploy-prod.sh + the
    │                                   assertion script the deploy runs
    ├── scripts/
    │   └── deploy-prod.sh           ← mode 700, copied from repo/scripts on each deploy
    ├── data/                        ← persistent volume, survives restarts
    │   (bind-mounted into the container at /var/lib/<app>/data —
    │   app.db, queue.db, nats/, dagnats/; see deploy/docker-compose.prod.yml)
    │   The container is non-root (65532), read-only rootfs, all caps
    │   dropped, no-new-privileges; /tmp is a 64 MB tmpfs and this data
    │   volume is the only other writable path.

/home/deploy/.secrets/gogogo.env       ← mode 600, dir 700, rendered every deploy
                                         from GH Secrets; outside the services tree

/home/deploy/services/<other-project>/   ← siblings follow the same shape
```

> **Not `/opt/`.** The deploy user is non-root, and `/opt` would need root to
> create. The workflow creates the tree with `mkdir -p` as `deploy`
> (`.github/workflows/deploy.yml`, "Ensure layout + run deploy"), and
> `scripts/deploy-prod.sh` reads `APP_DIR="/home/deploy/services/${PROJECT}"`.

## First production boot (avoid the lockout)

A production environment starts with `RBAC_ENABLED=true`, `SIGNUP_ENABLED=false`
and `DEMO_ACCOUNTS_ENABLED=false`, and `ADMIN_EMAILS` promotes **existing** rows
only. A brand-new database therefore has no account and no way in. Bootstrap in
this order:

1. Set `SIGNUP_ENABLED=true` for the first boot (or `DEMO_ACCOUNTS_ENABLED=true`
   for a demo), create the owner account, then restart with `SIGNUP_ENABLED=false`.
2. Put that address in `ADMIN_EMAILS` and restart once more; promotion needs the
   row to exist.
3. Keep `ADMIN_EMAILS` set: it is also the `/console/` and `/dagnats/` operator
   allowlist. `/config` only needs the admin role.

If you really want the open single-tenant posture, set `RBAC_ENABLED=false`
explicitly; the default is enforcing.

## Email delivery (verification, reset, magic links)

Set `SMTP_ADDR` (587/STARTTLS), `SMTP_FROM`, and the optional
`SMTP_USERNAME`/`SMTP_PASSWORD` in the age-encrypted secrets file. Set
`APP_URL` to the public origin; both magic links and verification/reset
links are built from it. Publish SPF, DKIM and DMARC for the sending domain
before turning on `SIGNUP_ENABLED` or `REQUIRE_EMAIL_VERIFICATION`; without
them, mail lands in spam and the account lifecycle looks broken. The
template uses `net/smtp` STARTTLS (port 587); implicit TLS (465) needs a
provider that speaks STARTTLS or an HTTP mail API behind the same `Mailer`
seam.

### Local/demo providers (no real delivery)

| Option | What it is | Free? | Use here |
|---|---|---|---|
| **Mailpit** | MIT single-binary SMTP sink + web UI, maintained successor to MailHog | yes, unlimited, self-hosted | `make mailpit`; point `SMTP_ADDR=127.0.0.1:1025`, `SMTP_FROM=dev@example.test`; UI `:8025` |
| **Ethereal** | disposable SMTP by the Nodemailer maintainer, no signup, preview links | yes, ephemeral | one-off smoke tests; copy host/user/pass from ethereal.email |
| **Mailtrap Sandbox** | hosted capture, free 50 emails/mo | yes | shared staging capture; never delivers |
| **Sendtrap / SMTPBox / SendPit** | hosted sandbox free tiers | yes | newer alternatives; verify retention/API before adopting |

Mailpit binds its UI to `127.0.0.1:8025` on purpose. The inbox contains live
magic-link, verification and reset tokens, so exposing it publicly would hand
every visitor a way into every pending account. A public demo user can trigger
a link but cannot read it; the local operator opens the UI through SSH or a
tailnet tunnel. `make mailpit` is a development/debug aid, not a mail service.

### Real delivery (demo or production)

| Provider | SMTP | Free tier (verify current) |
|---|---|---|
| **Resend** | `smtp.resend.com:587`, user `resend`, password = API key | ~3k/month, verified domain |
| **Brevo** | `smtp-relay.brevo.com:587` | ~300/day |
| **SendGrid** | `smtp.sendgrid.net:587`, user `apikey` | ~100/day |
| **Postmark / SES / Mailgun** | provider SMTP | varies; SES is pay-as-you-go |

The app only knows `SMTP_ADDR`/`SMTP_FROM`/`SMTP_USERNAME`/`SMTP_PASSWORD`;
swapping provider is an env change, no code. The secrets file is scoped by
`APP_NAME` (`~/.secrets/<APP_NAME>.env.age`): a generated project must be
renamed before boot, and must never reuse another project's secrets file. Name
scoping prevents accidental reuse, not a hostile process: anything running as
the same OS user can read every file in `~/.secrets`. Run an untrusted project
under its own user or container instead of treating the filename as a sandbox.
For real delivery publish SPF, DKIM and DMARC first. `hdls.tools` is an MCP agent toolbox with a mailbox
(create inbox, send/receive, 100 outbound/day); it is **not** an SMTP relay
and is not wired here.

## First-time setup on the server

1. Install Docker and create a `deploy` user with SSH key access.
2. Add the box to your Tailscale tailnet.

> **New to Tailscale?** It is a private WireGuard mesh between your machines
> ([install](https://tailscale.com/download), then `tailscale up` and approve
> the machine in the admin console). The deploy needs it so the CI runner
> reaches the server over the tailnet — no public SSH port, no static deploy
> credential (auth is a short-lived OIDC JWT per run). Nothing in local
> development needs it; it becomes required only at this step.
3. Configure a Cloudflare Tunnel routing your domain (e.g.
   `fullstack.example.com`) to the Tailscale hostname on port 8080.
4. Add the GitHub Actions secrets — see the header comment of
   `.github/workflows/deploy.yml` for the full list (SSH host, user, key,
   Tailscale OIDC client id + audience, project name, and the app's own
   secrets). You do **not** create the directory tree by hand; the workflow
   does it on first deploy.

> Secrets stay server-side by design: the workflow renders
> `~/.secrets/<app>.env` (600) from GitHub Secrets on every deploy and
> nothing secret is ever committed. The template also ships age tooling
> (`bin/init-secrets` + `internal/secrets`) for projects that need
> encrypted config — deliberately **not** used on a single box, where the
> identity would live next to the ciphertext and the gain over 600
> files + no-repo + monitoring is ~zero. Revisit if secrets ever need to
> travel through git (multi-operator GitOps).

> Two gotchas that bite on a non-root `deploy` user:
> - Grant the container write access with `setfacl`/`chmod`, **never `chown`** —
>   the deploy user is not root.
> - Never `scp` into the server's repo clone — `git pull --ff-only` aborts on a
>   dirty tree. Use the deploy workflow, which ships the binary and compose file
>   to `bin/` and `compose/` instead.

> Password brute-force throttling lives here, not in the app — the app
> deliberately tracks no per-email login attempts (that would let anyone
> lock anyone else out; see `internal/auth/ratelimit.go`). Rate-limit
> `/login` and `/api/auth/*` at this ingress (Caddy `rate_limit` or the
> Cloudflare WAF) before exposing the box.

## After setup, every push to `master` deploys

`.github/workflows/deploy.yml` runs when something the app is **built from**
lands on `master` (the path allow-list lives in the workflow, so docs and
`site/` are excluded by construction) and on `v*` tags. A `reconcile` job runs
first and skips the deploy when production already reports this `master` HEAD:
it reads `X-Gogogo-Build` from `/health` and compares the trailing short SHA.

1. **Builds the image once** (`image` job) on a native arm64 runner, from
   `deploy/Dockerfile.prod` — the same file `scripts/smoke-prod-image.sh` boots
   in CI. It pushes two tags and reports the **digest**; the digest is the
   artifact's identity, the tags are labels for humans. A repository without arm
   runners sets the variable `BUILD_RUNNER=ubuntu-latest` and the same build runs
   through QEMU instead — slower, same result, no file edit.
2. Brings up Tailscale in the runner (OIDC workload identity, scoped to the run).
3. Logs the server into the registry for the single pull (token over ssh stdin,
   never on a command line or in a file), SCPs
   `deploy/docker-compose.prod.yml` to `compose/docker-compose.prod.yml`, and
   renders the secrets file to `/home/deploy/.secrets/<app>.env` (dir 700, file
   600) from GitHub Actions secrets.
4. SSHes in, moves the source clone at `/home/deploy/services/<app>/repo` to
   `master`, copies `scripts/deploy-prod.sh` out of that clone, and runs it.
5. The script **pulls the digest** (nothing is compiled on the server), runs
   `docker compose up -d --wait` so the healthcheck gates the restart, checks
   the container is `healthy` **and** was recreated by this run, and then
   **asserts that `/health`'s `X-Gogogo-Build` names the commit that was
   built**. A failed pull leaves the previous container healthy and the deploy
   looking green; that assertion is the only thing that tells the two apart, so
   it is a hard failure, not a warning.
6. The workflow then asserts the **public** path: `scripts/assert-public-ready.sh`
   polls `https://<public host>/health` until it answers with the expected
   build. The origin and the public path are different claims — a container can
   be healthy on `127.0.0.1` while the tunnel, the vhost, or a container that
   has not finished binding its port still fails the user. The 502s seen during
   a deploy came from exactly that window (the gateway logged "connection reset
   by peer" while the app itself was healthy, and the image was created five
   seconds before the app bound its port). A deploy that only proves the origin
   can report green while the site is down, so it checks the path users walk.
   The gate has its own contract test (`scripts/assert-public-ready.test.sh`,
   wired into CI): it must pass on the right build, fail on a different one,
   and fail without hanging on a broken path.

The healthcheck is `CMD ["/app","health"]` (interval 30s, timeout 5s, retries 3,
start_period 10s): the binary's own `health` subcommand does an internal GET to
`/health` and exits 0 on 200.

**One artifact, named by digest.** The bytes CI built are the bytes production
runs, which is also what makes rollback cheap (below). Without an `APP_IMAGE`
digest the script builds the image locally from the clone instead — that is the
offline path and the path a first-time setup uses before CI has ever pushed.

**Registry access.** The image is pushed to `ghcr.io/<owner>/<repo>`, and the
server pulls that digest. GHCR creates the package **private even when the
repository is public**, so the deploy logs in by default with the run's
`GITHUB_TOKEN` (`packages: read`) over ssh stdin, and `deploy-prod.sh` logs out
on exit — no registry credential survives the deploy.

That default needs no setup and works for a private repository and a public one
alike. The login is best-effort on purpose: it is an enabler, not the contract —
if a credential was needed and missing, the pull fails loudly right after, and
the build assertion still refuses to report success. If you publish the package
on purpose (Package settings → Change visibility → Public) and would rather hold
no credential at all during the deploy, set the repository variable
`GHCR_PUBLIC=true` and the login step is skipped. Visibility itself has no API a
workflow token can call, so publishing is a one-click manual step.

## Rollback

Rollback is "run the previous digest", not "rebuild an older commit". The
digest of every deploy is printed by the `image` job (`Pin the digest` step) and
by the deploy log's `pulling <ref>` line.

1. Find an earlier digest: `docker image ls --digests` on the server (pull-mode
   pruning keeps a week of images), or the previous run's `image` job log.
2. Deploy it by hand, from the stable compose path:
   ```bash
   cd /home/deploy/services/<app>/compose
   APP_IMAGE=<registry>/<owner>/<repo>@sha256:<digest> \
     docker compose -f docker-compose.prod.yml up -d --wait
   ```
3. Prove what landed:
   ```bash
   bash /home/deploy/services/<app>/repo/scripts/assert-deploy-build.sh <that-commit>
   ```
4. The next push to `master` (or a `workflow_dispatch` run) moves production
   forward again — the reconcile job sees the badge is behind and deploys.

## Build pipeline and version badge metadata

The compile steps **outside** `go build` are the CSS bundle and
the Go ldflags that bake the version badge into the binary. All assets are
embedded into the binary via `//go:embed` — there is no runtime CSS build step,
no JS runtime, and no CDN.

```
src/css/input.css          →  tailwindcss v4 CLI  →  web/resources/static/app.min.css        (DaisyUI)
                                                              │
                                                        //go:embed in the Go binary
```

The sidebar's build badge shows `BuildLabel` (the git tag) and `BuildCommit` (the
short SHA), both baked in via Go `ldflags`:

```bash
# Local build — the Makefile sets these automatically:
make build
#   VERSION   = git describe --tags --abbrev=0 | sed 's/^v//'   (e.g. 0.29.1; "dev" if no tags)
#   COMMIT    = git rev-parse --short HEAD                    (e.g. b54916e)
#   BUILDTIME = date -u +"%Y-%m-%dT%H:%M:%SZ"
# LDFLAGS = -ldflags="-w -X main.Version=$VERSION -X main.CommitHash=$COMMIT -X main.BuildTime=$BUILDTIME"
```

The CI image build passes the same three values as build args (`deploy.yml`,
the `image` job's `build-args`), so the badge on the running box matches the tag
that produced it — and the deploy refuses to finish unless it does.

> **Why the tag push is supposed to deploy again (but doesn't).** The release flow is push → CI green → tag,
> so the deploy triggered by the *push* runs before the tag exists and
> `git describe --tags --abbrev=0` resolves to the **previous** tag — the box
> would run the new code while reporting the old version. (Observed for real: the
> v0.36.1 push deployed a binary labelled `0.36.0`, and again at v0.48.0:
> header `0.47.0-5095026` serving the tagged tree.) `deploy.yml` keeps a
> `tags: ["v*"]` arm for that re-deploy, but tags created by release-please
> use `GITHUB_TOKEN`, which starts no workflow runs — so the arm never
> fires. The `schedule` reconciler (every 30 min) closes the gap in both
> dimensions: running build SHA vs master HEAD (code drift), and badge
> version vs latest tag (label drift — redeploys the same code so the
> badge reports what is running). Tagging
> before CI passes would avoid the second restart, but a red CI would then
> already be on the tag — not a trade worth making.
>
> **The one place that gap is bridged instead of reconciled.**
> `release.yml` (the GoReleaser installer binaries, not the deploy) does not
> wait for the schedule: `release-please.yml` dispatches it via
> `workflow_dispatch` when it cuts a release, because that event is a
> documented exception to the `GITHUB_TOKEN` anti-recursion rule. Before the
> bridge existed, `on: push: tags` never fired for a release-please tag, so
> v0.43.0–v0.50.0 published **zero** release assets and `install.sh latest`
> 404'd on the tarball. `deploy.yml` keeps the schedule reconciler because a
> deploy is idempotent and can wait; an installer download cannot.

> The Dockerfile declares `ARG VERSION COMMIT BUILDTIME` at the stage top, **not
> inline inside a `RUN` chain** — an inline `ARG` breaks the Buildkit parse.

**Confirming a deploy.** The cheapest proof a fix is live is a byte-diff of an
embedded asset:

```bash
diff <(curl -s https://<host>/static/app.min.css) \
     <(git show HEAD:web/resources/static/app.min.css)
```

There is no `/api/version` endpoint, so do not use one as a check — it 404s.
For the badge, compare the sidebar's rendered `BuildLabel`/`BuildCommit` against
`git describe --tags --abbrev=0` and `git rev-parse --short HEAD`.

## Docker image

```bash
make docker-image   # Build and push the multi-arch image to ghcr.io (LOCAL target)
```

This is an **optional, local** convenience for publishing a multi-arch image
(e.g. to run the same build on another host). The deploy path does not use it:
the `image` job builds `linux/arm64` from `deploy/Dockerfile.prod` on a native
arm64 runner and the server pulls that image **by digest**.

The runtime image is alpine, not scratch: the Turso driver dlopens its Rust
library, which needs the musl loader and `libgcc_s`. Its healthcheck is
`CMD ["/app","health"]` — an in-binary subcommand, so it never depends on
`wget`/`curl`/`CMD-SHELL` existing in the base image.

## Related

- [Configuration](configuration.md) — the env vars the deploy writes.
- [Local CI](local-ci.md) — run the same gate before pushing.

## Backup and restore

```bash
./scripts/backup.sh [DATA_DIR] [DEST_DIR]   # .backup + integrity_check per .db, JetStream when reachable
./scripts/restore.sh --dry-run <backup>     # monthly drill: verifies without touching anything
./scripts/restore.sh --force <backup> [DATA_DIR]  # stop the app first; moves -wal/-shm aside
```

What gets backed up: `data.db`, `queue.db` (both integrity-checked),
JetStream streams/KV (only when the `nats` CLI reaches the server —
ephemeral fan-out streams are losable by design, see
[async-layers](async-layers.md#durability-and-retention-per-store)).
Browser state is never backed up. Restore swaps files with the app
stopped; every engine recovers on boot, so there is no cross-store
coordination step.