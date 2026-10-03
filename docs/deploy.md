# Deploy to your own box

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
└── gogogo-fullstack-template/                ← this project
    ├── bin/
    │   ├── gogogo-fullstack-template          ← current binary (chmod 755)
    │   └── gogogo-fullstack-template.previous ← prior binary, kept for fast rollback
    ├── compose/
    │   └── docker-compose.prod.yml
    ├── env/
    │   └── .env                     ← non-secret env (DATABASE_URL, APP_URL, ...)
    ├── secrets/
    │   └── gogogo-fullstack-template.env   ← mode 600, regenerated every deploy from GH Secrets
    ├── data/
    │   └── pb_data/                 ← persistent volume, survives restarts
    ├── repo/                        ← git clone of this repo (source for env/.env + deploy-prod.sh)
    └── scripts/
        └── deploy-prod.sh           ← the on-server deploy runner (mode 700)

/home/deploy/services/<other-project>/   ← siblings follow the same shape
```

> **Not `/opt/`.** The deploy user is non-root, and `/opt` would need root to
> create. The workflow creates the tree with `mkdir -p` as `deploy`
> (`.github/workflows/deploy.yml`, "Ensure layout + run deploy"), and
> `scripts/deploy-prod.sh` reads `APP_DIR="/home/deploy/services/${PROJECT}"`.

## First-time setup on the server

1. Install Docker and create a `deploy` user with SSH key access.
2. Add the box to your Tailscale tailnet.
3. Configure a Cloudflare Tunnel routing your domain (e.g.
   `fullstack.example.com`) to the Tailscale hostname on port 8080.
4. Add the GitHub Actions secrets — see the header comment of
   `.github/workflows/deploy.yml` for the full list (SSH host, user, key,
   Tailscale OIDC client id + audience, project name, and the app's own
   secrets). You do **not** create the directory tree by hand; the workflow
   does it on first deploy.

> Two gotchas that bite on a non-root `deploy` user:
> - Grant the container write access with `setfacl`/`chmod`, **never `chown`** —
>   the deploy user is not root.
> - Never `scp` into the server's repo clone — `git pull --ff-only` aborts on a
>   dirty tree. Use the deploy workflow, which ships the binary and compose file
>   to `bin/` and `compose/` instead.

## After setup, every push to `master` deploys

`.github/workflows/deploy.yml` runs on every push to `master` and:

1. Builds the project (Go install, npm deps, CSS, then the binary).
2. **Cross-compiles the production binary in the runner** — no Docker image is
   built there. The step is
   `CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC=aarch64-linux-gnu-gcc go build
   -trimpath -ldflags="-s -w -extldflags=-static -X main.Version=…"`,
   producing `bin/<app>.new` for the target architecture (arm64 for the
   reference box; adjust `GOARCH`/`CC` for your host).
3. Uploads that binary as an artifact, then brings up Tailscale in the runner
   (OIDC workload identity, scoped to the run).
4. SCPs the binary to the server as `gogogo-fullstack-template.new`, plus the
   compose file to `compose/docker-compose.prod.yml`.
5. Writes the secrets file
   (`/home/deploy/services/gogogo-fullstack-template/secrets/gogogo-fullstack-template.env`)
   with mode 600, rendered from GitHub Actions secrets.
6. SSHes in, ensures the directory tree exists, and runs
   `scripts/deploy-prod.sh`, which:
   - atomically renames `gogogo-fullstack-template.new` →
     `gogogo-fullstack-template` and keeps the old binary as `.previous`,
   - restarts the container with
     `docker compose -f deploy/docker-compose.prod.yml up -d --wait` — `--wait`
     blocks until the compose healthcheck passes, rather than polling a fixed
     number of seconds,
   - the healthcheck itself is `CMD ["/app","health"]` (interval 30s, timeout
     5s, retries 3, start_period 10s): the binary's own `health` subcommand
     does an internal GET to `/health` and exits 0 on 200.
7. Prints the new container status and the last log lines for confirmation.

The Docker image is built **on the server** from the `build:` stanza in
`deploy/docker-compose.prod.yml` — the runner only ships a static binary.

> Restarting a stack requires `cd <project-dir> && docker compose up -d`, never
> a blanket `docker restart <container>` — compose recreates networks, volume
> mounts, and dependencies in order.

**Secrets are never stored long-term on the server.** Every deploy re-renders
the secrets file from GitHub Actions secrets. The file is `chmod 600`, owned by
 the `deploy` user, and overwritten on every run — there is no history of
secrets on disk.

## Build pipeline and version badge metadata

The compile steps **outside** `go build` are the CSS bundles (one per skin) and
the Go ldflags that bake the version badge into the binary. All assets are
embedded into the binary via `//go:embed` — there is no runtime CSS build step,
no JS runtime, and no CDN.

```
src/css/input.css          →  tailwindcss v4 CLI  →  web/resources/static/app.min.css        (DaisyUI)
src/css/basecoat-input.css →  tailwindcss v4 CLI  →  web/resources/static/basecoat.min.css  (Basecoat)
                                                              │
                              web/skins/morpheus/static/bundle.js                          (Morpheus, vendorized)
                                                              │
                                                        //go:embed in the Go binary
```

The navbar version badge shows `BuildLabel` (the git tag) and `BuildCommit` (the
short SHA), both baked in via Go `ldflags`:

```bash
# Local build — the Makefile sets these automatically:
make build
#   VERSION   = git describe --tags --abbrev=0 | sed 's/^v//'   (e.g. 0.29.1; "dev" if no tags)
#   COMMIT    = git rev-parse --short HEAD                    (e.g. b54916e)
#   BUILDTIME = date -u +"%Y-%m-%dT%H:%M:%SZ"
# LDFLAGS = -ldflags="-w -X main.Version=$VERSION -X main.CommitHash=$COMMIT -X main.BuildTime=$BUILDTIME"
```

The CI deploy cross-compiles with the same ldflags (`deploy.yml`, "Build prod
binary"), so the badge on the running box matches the tag that produced it.

> The Dockerfile declares `ARG VERSION COMMIT BUILDTIME` at the stage top, **not
> inline inside a `RUN` chain** — an inline `ARG` breaks the Buildkit parse.

**Confirming a deploy.** The cheapest proof a fix is live is a byte-diff of an
embedded asset:

```bash
diff <(curl -s https://<host>/static/app.min.css) \
     <(git show HEAD:web/resources/static/app.min.css)
```

There is no `/api/version` endpoint, so do not use one as a check — it 404s.
For the badge, compare the navbar's rendered `BuildLabel`/`BuildCommit` against
`git describe --tags --abbrev=0` and `git rev-parse --short HEAD`.

## Docker image

```bash
make docker-image   # Build and push the multi-arch image to ghcr.io (LOCAL target)
```

This is an **optional, local** convenience for publishing an image. The deploy
workflow does not use it: the runner cross-compiles a static binary and the
image is built on the server from `deploy/docker-compose.prod.yml`. Run
`make docker-image` only if you want a registry-hosted image (e.g. to run the
same build on another host).

The scratch image has no shell, so its healthcheck is
`CMD ["/app","health"]` — never `wget`/`curl`/`CMD-SHELL`, none of which exist
in a scratch base.

## Related

- [Configuration](configuration.md) — the env vars the deploy writes.
- [Local CI](local-ci.md) — run the same gate before pushing.