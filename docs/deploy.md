# Deploy to your own box

The default workflow is **clone + `make dev`** for local work. For a permanent
deployment, the project ships a production deploy workflow that publishes to a
server of your choosing — recommended: a small Linux box + Tailscale +
a Cloudflare-tunneled domain. No registry, no cold starts, full control.

## Server layout (multi-project standard)

Pick a directory on your server and follow this layout for **every** project
that adopts the pattern — siblings share the same shape:

```
/opt/
└── gogogo-fullstack-template/                  ← this project
    ├── bin/
    │   ├── gogogo-fullstack-template             ← current binary (chmod 755)
    │   └── gogogo-fullstack-template.previous    ← prior binary, kept for fast rollback
    ├── compose/
    │   └── docker-compose.prod.yml
    ├── env/
    │   └── .env                       ← non-secret env (DATABASE_URL, APP_URL, ...)
    ├── secrets/
    │   └── gogogo-fullstack-template.env         ← mode 600, regenerated every deploy from GH Secrets
    ├── data/
    │   └── pb_data/                    ← persistent volume, survives restarts
    ├── repo/                           ← git clone of this repo (for re-syncing on each deploy)
    ├── scripts/
    │   └── deploy-prod.sh             ← the on-server deploy runner
    └── README.md                       ← operator's guide (link to this page)

/opt/<other-project>/                  ← siblings follow the same shape
    ├── bin/
    ├── compose/
    ├── env/
    ├── secrets/
    └── data/
```

## First-time setup on the server

1. Install Docker and create a `deploy` user with SSH key access.
2. Add the box to your Tailscale tailnet.
3. Configure a Cloudflare Tunnel routing your domain (e.g.
   `fullstack.example.com`) to the Tailscale hostname on port 8080.
4. Clone the repo at `/opt/gogogo-fullstack-template/repo/` and create the
   directories:
   ```bash
   mkdir -p bin compose env secrets data/pb_data scripts
   ```
5. Add the GitHub Actions secrets — see `.github/workflows/deploy.yml` for the
   full list (SSH host, user, key, project name, and the app's own secrets).

> Two gotchas that bite on a non-root `deploy` user:
> - Grant the container write access with `setfacl`/`chmod`, **never `chown`** —
>   the deploy user is not root.
> - Never `scp` into the server's repo clone — `git pull --ff-only` aborts on a
>   dirty tree. Use the deploy workflow, which pushes to a fresh checkout.

## After setup, every push to `master` deploys

`.github/workflows/deploy.yml` runs on every push to `master` and:

1. Builds the project (lint + race tests + CSS build).
2. Builds the production Docker image (linux/amd64 scratch) in the runner.
3. SCPs the new binary to the server as `gogogo-fullstack-template.new`.
4. Writes the secrets file
   (`/opt/gogogo-fullstack-template/secrets/gogogo-fullstack-template.env`) with
   mode 600.
5. SSHes in and runs `scripts/deploy-prod.sh`, which:
   - atomically renames `gogogo-fullstack-template.new` →
     `gogogo-fullstack-template` and keeps the old binary as `.previous`,
   - restarts the container via
     `docker compose -f docker-compose.prod.yml up -d`,
   - waits up to 30s for `/health` to return 200.
6. Prints the new container status and the last 20 log lines for confirmation.

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
short SHA), both baked in via Go `ldflags` and flowing through the Docker image
via `ARG`:

```bash
# Local build — the Makefile sets these automatically:
make build
#   VERSION   = git describe --tags --abbrev=0 | sed 's/^v//'   (e.g. 0.29.1; "dev" if no tags)
#   COMMIT    = git rev-parse --short HEAD                    (e.g. b54916e)
#   BUILDTIME = date -u +"%Y-%m-%dT%H:%M:%SZ"
# LDFLAGS = -ldflags="-w -X main.Version=$VERSION -X main.CommitHash=$COMMIT -X main.BuildTime=$BUILDTIME"

# Docker build (CI / deploy):
docker buildx build --platform=linux/amd64,linux/arm64 \
  --build-arg VERSION=$VERSION \
  --build-arg COMMIT=$COMMIT \
  --build-arg BUILDTIME=$BUILDTIME \
  -t ghcr.io/calionauta/gogogo-fullstack-template:latest \
  -t ghcr.io/calionauta/gogogo-fullstack-template:$VERSION \
  --push .
```

> The Dockerfile declares `ARG VERSION COMMIT BUILDTIME` at the stage top, **not
> inline inside a `RUN` chain** — an inline `ARG` breaks Buildkit parse.

**Confirming a deploy.** The cheapest proof a fix is live is a byte-diff of an
embedded asset:
`diff <(curl https://<host>/static/app.min.css) <(git show HEAD:web/resources/static/app.min.css)`.
For the version badge specifically:
`diff <(curl https://<host>/api/version) <(echo $VERSION)`.

## Docker image

```bash
make docker-image   # Build and push the multi-arch image to ghcr.io
```

The scratch image has no shell, so its healthcheck is
`CMD ["/app","health"]` — never `wget`/`curl`/`CMD-SHELL`, none of which exist
in a scratch base.

## Related

- [Configuration](configuration.md) — the env vars the deploy writes.
- [Local CI](local-ci.md) — run the same gate before pushing.