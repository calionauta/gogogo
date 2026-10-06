APP_NAME    := gogogo
APP_DIR     := cmd/web
PORT        ?= 8080
VERSION     := $(shell v=$$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//'); echo $${v:-dev})
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILDTIME   := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS     := -ldflags="-w -X main.Version=$(VERSION) -X main.CommitHash=$(COMMIT) -X main.BuildTime=$(BUILDTIME)"
# Wails v3 pin. Kept in sync with go.mod (wails/v3) and the install line in
# .github/workflows/desktop.yml. The desktop BUILD does not use the CLI (it is
# plain `go build`); the pin exists for `wails3 doctor` / `wails3 init` tooling.
WAILS_VERSION := v3.0.0-beta.24

.PHONY: all build desktop desktop-setup-cross desktop-cross-windows desktop-cross-darwin desktop-cross-linux desktop-cross-universal desktop-cross wails-build run clean restart templ fmt css css-install datastar-lint test lint vet check-sizes deadcode ci-local signoff check-skill-frontmatter deps dev docker-image setup rename help smoke gui run-gui lint-gui check-generated install-sh-guard

all: build

templ:
	@echo "→ Generating templ files..."
	@go tool templ generate

build: templ
	@echo "→ Building $(APP_NAME) v$(VERSION)..."
	@go build $(LDFLAGS) -o $(APP_NAME) ./$(APP_DIR)

desktop: templ
	@echo "→ Building desktop shell (Wails v3 + Leaf Node) v$(VERSION)..."
	@go build $(LDFLAGS) -o gogogo-desktop ./cmd/desktop

# NOTE: `wails3 build` cannot be used in this repo — it takes no `-o` flag and
# delegates to `wails3 task build`, which needs a Taskfile.yml at the root
# (make is the task runner here). `make desktop` is the real build; this target
# is kept only as an explicit "why not" so nobody re-adds it by mistake.
wails-build:
	@echo "✗ Not available: 'wails3 build' needs a Taskfile.yml (this repo uses make)."
	@echo "  Use: make desktop   (plain 'go build -o gogogo-desktop ./cmd/desktop')"
	@exit 1

# Cross-preview via wails-cross (opt-in, NOT a gate). One Linux runner generates
# previews for all three platforms; darwin binaries come out UNSIGNED (test only).
# Worth it for: PR preview / multi-OS smoke without three native runners.
# Final release still happens on native runners (macOS signing). Needs Docker.
desktop-setup-cross:
	@echo "→ One-time wails-cross setup (~800MB, macOS SDK via wailsapp/macosx-sdks)..."
	@echo "  Building from the wails source (no wails3 CLI / Taskfile needed)..."
	@rm -rf /tmp/wails-src
	@git clone --depth 1 https://github.com/wailsapp/wails /tmp/wails-src
	@docker build -t wails-cross \
		-f /tmp/wails-src/build/docker/Dockerfile.cross \
		/tmp/wails-src/build/docker/

desktop-cross-windows: templ
	@echo "→ Cross preview Windows/amd64 (wails-cross, unsigned preview)..."
	@./scripts/desktop-build.sh cross-windows

desktop-cross-darwin: templ
	@echo "→ Cross preview macOS/arm64 (wails-cross, UNSIGNED, test only)..."
	@./scripts/desktop-build.sh cross-darwin

desktop-cross-linux: templ
	@echo "→ Cross preview Linux/amd64 (wails-cross)..."
	@./scripts/desktop-build.sh cross-linux

desktop-cross-universal: templ
	@echo "→ Cross preview macOS universal (amd64+arm64, UNSIGNED, test only)..."
	@./scripts/desktop-build.sh cross-universal

desktop-cross: desktop-cross-windows desktop-cross-darwin desktop-cross-linux
	@echo "✅ cross previews in build/cross/ (darwin binaries UNSIGNED)"

run:
	@echo "→ Starting $(APP_NAME) on port $(PORT)..."
	@PORT=$(PORT) ./$(APP_NAME)

clean:
	@echo "→ Cleaning..."
	@rm -f $(APP_NAME)
	@find . -name '*.log' -delete

test:
	# Plain parallel run across packages (scripts/test-web.sh). It used to be
	# `-p 1` because the DagNats tests bound FIXED ports and two packages
	# grabbed the same 18099; that looked like engine starvation but was a
	# port collision. All DagNats tests now bind ephemeral HTTP ports and read
	# the address back, so nothing needs serializing: ~110s vs ~255s.
	# cmd/desktop is a separate Wails target (needs GTK/WebKit libs only on
	# desktop build hosts); web-packages.sh excludes it as CI does.
	@bash scripts/test-web.sh -race -count=1

# test-fast is the tight TDD loop. Same run as `test` but drops -race,
# which is the dominant cost of the full gate. Use it for red/green
# iteration; run `test` (or `make ci-local`) before commit.
test-fast:
	@bash scripts/test-web.sh -count=1

# css-install installs the npm dev dependencies (Tailwind CLI + DaisyUI
# v5). Idempotent. Run once after cloning; CI calls this in the
# Docker build stage so contributors don't need to.
#
# It does NOT trust a bare `node_modules` directory. A stale tree (cloned
# from an older lockfile, or left by an earlier session) has a DIFFERENT
# Tailwind/DaisyUI version than package-lock.json, so `make css` emits a
# bundle that differs from the committed one and `css-check` fails with no
# source change — a false alarm that reads as "the repo is broken". Compare
# the installed versions against the lockfile and reinstall on mismatch;
# without this, correctness of the whole CSS gate depends on a one-time
# manual `npm ci` nobody remembers to run.
css-install:
	@if ! node -e 'const fs=require("fs");const lock=JSON.parse(fs.readFileSync("package-lock.json","utf8"));const want=n=>lock.packages["node_modules/"+n]?.version;let bad=[];for(const n of ["tailwindcss","@tailwindcss/cli","daisyui"]){const w=want(n);let g;try{g=JSON.parse(fs.readFileSync("node_modules/"+n+"/package.json","utf8")).version}catch{}if(w&&g!==w)bad.push(n+" "+(g||"absent")+" -> "+w)}process.exit(bad.length?1:0)' 2>/dev/null; then \
		echo "→ CSS deps missing or stale vs package-lock.json — installing (npm ci)..."; \
		npm ci --silent; \
		echo "  ✓ installed"; \
	else \
		echo "  ✓ CSS deps match package-lock.json"; \
	fi

# css runs the Tailwind v4 CLI to build web/resources/static/app.min.css
# from src/css/input.css. Run this whenever you add new utility
# classes in .templ files; the pre-commit hook also runs it on
# .templ changes to catch stale CSS before commit.
css: css-install
	@echo "→ Building CSS (Tailwind v4 + DaisyUI v5)..."
	@npm run build --silent
	@echo "  ✓ built web/resources/static/app.min.css"

# css-basecoat builds the companion BasecoatUI (shadcn) stylesheet from
# src/css/basecoat-input.css. Produces web/resources/static/basecoat.min.css
# which is loaded at runtime when UI_SKIN=basecoat.
css-basecoat: css-install
	@echo "→ Building BasecoatUI CSS (shadcn-inspired)..."
	@npx tailwindcss -i ./src/css/basecoat-input.css -o ./web/resources/static/basecoat.min.css --silent
	@echo "  ✓ built web/resources/static/basecoat.min.css"

# css-all builds both CSS skins at once. Called by CI and Docker build.
css-all: css css-basecoat

# css-check fails if the generated CSS is out-of-date. Used by the
# pre-commit hook and CI to catch forgotten rebuilds.
css-check: css-all
	@git diff --quiet --exit-code web/resources/static/app.min.css || (echo "  ❌ app.min.css out of date. Run \`make css\` and re-commit."; exit 1)
	@git diff --quiet --exit-code web/resources/static/basecoat.min.css || (echo "  ❌ basecoat.min.css out of date. Run \`make css-basecoat\` and re-commit."; exit 1)
	@echo "  ✓ CSS is up to date"
	@echo "  ✓ basecoat CSS is up to date"

# fmt checks formatting with gofumpt + goimports (no --fast shortcuts).
fmt:
	@echo "→ Checking formatting (gofumpt + goimports)..."
	@test -z "$$(gofumpt -l .)" || (echo "  ❌ gofumpt issues:"; gofumpt -l .; exit 1)
	@test -z "$$(goimports -l -local github.com/calionauta/gogogo $$(find . -name '*.go' ! -name '*_templ.go'))" || (echo "  ❌ goimports issues"; goimports -l -local github.com/calionauta/gogogo $$(find . -name '*.go' ! -name '*_templ.go'); exit 1)
	@echo "  ✅ formatting clean"

# datastar-lint checks the Datastar surface: .templ/.html attributes and Go
# backend SDK calls (sse.PatchElements and friends).
#
# -only-errors: real issues fail the gate; warnings (intentional custom attrs
# like data-tool/data-doc-id/data-neo-*) are reported but do not block. Those go
# in .datastar-lint.yaml under attributes.allowed instead of being silenced.
#
# Scoped to ./features and ./internal so the every-save Air pre_cmd does not
# walk node_modules. NOTE: a finding only fails this target when its severity is
# ERROR — warnings alone exit 0.
datastar-lint:
	@echo "→ Running datastar-lint..."
	@bin/datastar-lint -only-errors -r ./features ./internal

lint:
	@echo "→ go vet..."
	@PKGS=$$(bash scripts/web-packages.sh); go vet $$PKGS
	@echo "→ golangci-lint (full, no --fast)..."
	@if which golangci-lint >/dev/null 2>&1; then PKGS=$$(bash scripts/web-packages.sh); golangci-lint run $$PKGS; else echo "  ❌ golangci-lint not installed (brew install golangci-lint)"; exit 1; fi

# lint-safe is the host-aware lint. It measures the machine at run time (free
# RAM + cores) and sizes the run to what that evidence supports: full repo only
# when there is headroom, otherwise changed packages; always memory-capped in a
# cgroup when the platform offers one; never clears the cache. Prefer it over
# `make lint` on any host that also runs other work (agents, production, a
# co-tenant daemon). Rationale + overrides live in scripts/lint-safe.sh.
lint-safe:
	@bash scripts/lint-safe.sh

check-sizes:
	@mkdir -p .githooks
	@[ -f .githooks/check-sizes.sh ] || bin/setup-hooks.sh >/dev/null 2>&1
	@.githooks/check-sizes.sh

# check-scope enforces that every non-test, non-generated .go file
# under internal/ and features/ carries a leading doc-comment line
# matching `// SCOPE:layer=<infra|feature>,removal=<core|plugin|feature>`.
# Lives outside cmd/web/ on purpose: developer tool, not part of the
# runtime binary. Run after adding/removing files in those trees.
check-scope:
	@echo "→ check-scope (SCOPE annotation linter)..."
	@go run ./cmd/check-scope
	@echo "✅ SCOPE annotations present"

# check-skill-frontmatter validates every SKILL.md frontmatter with a real
# YAML parser. The repo's build never reads that YAML — the skill *host* does —
# so an unquoted ": " in a description ships silently and only shows up as
# "Error in user YAML" in the host UI. This gate is what stops it.
check-skill-frontmatter:
	@echo "→ check-skill-frontmatter (SKILL.md frontmatter linter)..."
	@go run ./cmd/check-skill-frontmatter
	@echo "✅ SKILL.md frontmatter valid"

deadcode:
	@which deadcode >/dev/null 2>&1 && PKGS=$$(bash scripts/web-packages.sh) && deadcode -test $$PKGS || echo "  (deadcode not installed, run: go install golang.org/x/tools/cmd/deadcode@latest)"

# check is the single quality gate. Run it after EVERY significant edit,
# not just before commit. It formats, lints .templ, vets, lints, sizes,
# scans dead code, runs the full race test suite, and verifies the
# generated CSS is up to date. make setup installs the blocking
# pre-commit hook that enforces the same gate on every commit.
# ci-local runs the same quality gate as CI but locally, so you can
# catch issues before pushing. Runs lint, tests (parallel across packages —
# see scripts/test-web.sh), and a single unified build — no more tag matrix.
# check-generated is the generated-artifact drift guard: regenerate the
# checked-in artifacts and fail if any of them DIFFERS from what is committed.
#
# The `_templ.go` files are committed (71 of them) so a checkout builds without
# the templ toolchain. That means editing a `.templ` and forgetting to commit
# its regeneration ships a binary that renders the OLD markup — and both CI and
# the browser smoke test pass anyway, because they build from the committed
# `_templ.go`. This is the `css-check` mistake in a second place.
#
# Deterministic: `templ generate` is idempotent (verified — a second run
# produces a byte-identical tree), unlike the Tailwind/Astro generators that
# motivated disabling actions/cache.
check-generated: templ
	@if ! git diff --quiet --exit-code -- '*_templ.go'; then \
		echo "  ❌ committed _templ.go does not match the .templ sources."; \
		echo "     Run \`make templ\` and COMMIT the regenerated files."; \
		git diff --stat -- '*_templ.go'; \
		exit 1; \
	fi
	@echo "  ✓ generated templ files match their sources"

ci-local: check-generated check-sizes datastar-lint css-check check-scope check-skill-frontmatter install-sh-guard
	@echo "→ lint (golangci-lint, same as CI)"
	@if which golangci-lint >/dev/null 2>&1; then PKGS=$$(bash scripts/web-packages.sh); golangci-lint run $$PKGS; else echo "  ❌ golangci-lint not installed (brew install golangci-lint)"; exit 1; fi
	@echo "→ tests (parallel across packages)"
	@bash scripts/test-web.sh -race -count=1
	@echo "→ build (single build, reused by the smoke tests)"
	@go build $(LDFLAGS) -o /tmp/gogogo-ci-local-web ./cmd/web/
	@echo "→ binary boot smoke test (asserts the SSE transport contract)"
	@RUN_SMOKE=1 SMOKE_BIN=/tmp/gogogo-ci-local-web go test -count=1 -timeout 180s -run TestSmoke_BootedBinaryServesAndSyncs ./cmd/web/
	@echo "→ browser smoke test (Playwright)"
	@npx playwright install chromium
	@SMOKE_BIN=/tmp/gogogo-ci-local-web node scripts/smoke.mjs
	@rm -f /tmp/gogogo-ci-local-web
	@echo "✅ ci-local passed"

# install-sh-guard is the install.sh bootstrap contract (never plant a `go`
# entry in $BIN_DIR, keep the toolchain reachable). Cheap grep + `sh -n`.
install-sh-guard:
	@bash bin/check-install-sh.sh

# Fast local gate: the same cheap-but-decisive checks as ci-local (templ,
# datastar-lint, css-check, check-scope, scoped lint) PLUS race tests for ONLY
# the packages your change touches. Skips the full test sweep and the browser
# smoke. Use it every few edits; run the full `ci-local` (and `make signoff`)
# before pushing.
#
# Why: the full suite is dominated by features/todo (~90s+, >60% of the run),
# so a CSS or installer tweak would otherwise pay for the whole suite. This
# narrows on the changed packages, falling back to all packages when a shared
# file (go.mod, config/, db/) changed.
ci-local-fast: check-generated check-sizes datastar-lint css-check check-scope check-skill-frontmatter install-sh-guard
	@echo "→ lint (golangci-lint, scoped to changed packages)"
	@if which golangci-lint >/dev/null 2>&1; then PKGS=$$(bash scripts/changed-packages.sh); if [ -z "$$PKGS" ]; then echo "  (no Go packages changed)"; else golangci-lint run $$PKGS; fi; else echo "  ❌ golangci-lint not installed (brew install golangci-lint)"; exit 1; fi
	@echo "→ tests (race, changed packages only)"
	@PKGS=$$(bash scripts/changed-packages.sh); if [ -z "$$PKGS" ]; then echo "  (no Go packages changed — ran cheap checks only)"; else go test -race -count=1 $$PKGS; fi
	@echo "✅ ci-local-fast passed (full gate before push: make ci-local)"

# smoke boots the built binary in a headless browser, fails on uncaught client
# errors, and exercises offline todo add/delete through IndexedDB + reconnect
# replay. This catches both script-rendering and offline-queue regressions.
# Requires `npx playwright install chromium` (run automatically here).
smoke:
	@echo "→ Browser smoke test (Playwright)…"
	@npx playwright install chromium
	@node scripts/smoke.mjs

# signoff runs the full local CI then stamps the current commit green via
# the gh-signoff extension (basecamp/gh-signoff). This lets you skip
# waiting on remote runners for the common case. NOTE: our deploy triggers
# on push to master (not PR merge), so the signoff status is ADVISORY
# here — we deliberately do NOT run `gh signoff install` (which would
# require the status for PR merge and is meaningless for push-to-deploy).
# If/when we move to a PR-based flow, enable `gh signoff install` too.
signoff: ci-local
	@echo "→ stamping commit green via gh signoff..."
	@gh signoff -f
	@echo "✅ signed off — safe to push"

.PHONY: ci-local ci-local-fast signoff

.PHONY: site site-check

deps:
	@go mod tidy

setup:
	@bin/setup-hooks.sh

# Rename the project identity (module path, binary, container, titles, deploy
# paths). This repo is a GitHub template, so a fresh copy still says
# gogogo everywhere — ~360 occurrences across ~130 files.
# Doing that by hand invites a half-renamed tree that compiles but deploys to
# the wrong directory, so the script does the whole pass and then builds to
# prove it. See scripts/rename-project.py for exactly what is and is not touched.
rename:
	@test -n "$(NAME)" || { echo "usage: make rename NAME=my-app [OWNER=myorg] [DRY=1]"; exit 1; }
	@python3 scripts/rename-project.py $(NAME) \
		--owner "$(or $(OWNER),calionauta)" $(if $(DRY),--dry-run,)

dev:
	@echo "→ Starting Air live reload (ENVIRONMENT=development unless already set)..."
	@ENVIRONMENT=$${ENVIRONMENT:-development} air

docker-image: templ
	@echo "→ Building Docker image v$(VERSION) (commit $(COMMIT))..."
	# Pass build metadata into the image so the navbar version badge
	# reflects exactly what was built. The Dockerfile consumes these
	# as ARG VERSION / ARG COMMIT / ARG BUILDTIME.
	docker buildx build --platform=linux/amd64,linux/arm64 \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILDTIME=$(BUILDTIME) \
		-t ghcr.io/calionauta/$(APP_NAME):latest \
		-t ghcr.io/calionauta/$(APP_NAME):$(VERSION) \
		--push .

coverage:
	@echo "→ Running tests with coverage..."
	# -p 1 here on purpose: a single coverage.out needs every package's profile
	# merged, and `go test` with -coverprofile writes one file per invocation.
	@PKGS=$$(bash scripts/web-packages.sh); go test -race -p 1 $$PKGS -count=1 -coverprofile=coverage.out -covermode=atomic
	@go tool cover -func=coverage.out | sort -k3 -r | head -30
	@echo "---"
	@go tool cover -html=coverage.out -o coverage.html
	@echo "→ Full report: coverage.html"
	@rm -f coverage.out

help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "Targets:"
	@echo "  build          Build binary (unified: everything included)"
	@echo "  desktop        Build desktop shell (Wails v3, native, fast gate)"
	@echo "  desktop-cross  Cross previews win+mac+linux via wails-cross (opt-in, unsigned)"
	@echo "  desktop-setup-cross  One-time wails-cross Docker setup (~800MB)"
	@echo "  fmt            Check formatting (gofumpt + goimports)"
	@echo "  datastar-lint  Lint .templ files for Datastar anti-patterns"
	@echo "  test           Run tests with race detector"
	@echo "  coverage       Run tests with coverage report (HTML)"
	@echo "  lint           Run go vet + golangci-lint (full)"
	@echo "  lint-safe      Host-aware lint: scoped + memory-capped (safe on shared hosts)"
	@echo "  check-sizes    Check file/function size limits"
	@echo "  check-scope    Enforce SCOPE:layer=…,removal=… annotations"
	@echo "  check-skill-frontmatter  Validate SKILL.md YAML frontmatter (host-parsed, build-invisible)"
	@echo "  deadcode       Scan for dead code"

	@echo "  css            Build app.min.css from src/css/input.css (Tailwind v4 + DaisyUI v5)"
	@echo "  ci-local       Full pre-push gate (= CI): lint, race tests, build, browser smoke"
	@echo "  ci-local-fast  Fast gate: cheap checks + race tests for changed packages only (~2-30s)"
	@echo "  signoff        ci-local + gh signoff stamp (safe to push)"
	@echo "  css-install    Install CSS build dependencies (npm)"
	@echo "  dev            Live reload with Air"
	@echo "  templ          Generate Templ components"
	@echo "  deps           go mod tidy"
	@echo "  setup          Install git hooks"
	@echo "  rename         Rename the project (NAME=my-app [OWNER=myorg] [DRY=1])"
	@echo "  docker-image   Build and push Docker image"

# gui builds the gogpu/ui native POC (proof that the backend is reachable
# without HTTP). Tests run headless (no window/GPU); the shipped binary
# builds with CGO_ENABLED=0 per the gogpu requirement. Validated in CI
# by the gui-poc job in .github/workflows/desktop.yml.
gui:
	@echo "→ Testing native gogpu/ui POC v$(VERSION) (headless)..."
	@go test -race -count=1 ./cmd/gui
	@echo "→ Building native gogpu/ui POC v$(VERSION)..."
	@CGO_ENABLED=0 go build $(LDFLAGS) -o gogogo-gui ./cmd/gui

# lint-gui runs the full 27-linter gate on cmd/gui. NOT in CI (linting
# the wgpu/naga tree is minutes-cold per push and only catches style —
# breakage is caught by vet+tests+build in the gui-poc job). Run this
# locally before committing any cmd/gui change; keep it at zero issues.
lint-gui:
	@echo "→ golangci-lint on cmd/gui (manual gate, see gui-poc NOTE)..."
	@golangci-lint run ./cmd/gui/...

run-gui:
	@echo "→ Starting the native gogpu/ui POC (window + GPU required)..."
	@./gogogo-gui

# --- Docs site (GitHub Pages) ----------------------------------------------
# site/index.html + styles.css + assets/ are hand-written sources; everything
# under site/docs/ plus llms*.txt and sitemap.xml is generated from docs/*.md.
# CI (.github/workflows/pages.yml) runs `site-check` then `site` before
# publishing, so a broken internal link fails the deploy instead of shipping.
site:
	@echo "→ Building docs site from docs/*.md..."
	@node site/build.mjs

site-check:
	@echo "→ Checking docs links and heading anchors..."
	@node site/build.mjs --check
