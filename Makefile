APP_NAME    := gogogo
APP_DIR     := cmd/web
PORT        ?= 8080
VERSION     := $(shell v=$$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//'); echo $${v:-dev})
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILDTIME   := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS     := -ldflags="-w -X main.Version=$(VERSION) -X main.CommitHash=$(COMMIT) -X main.BuildTime=$(BUILDTIME)"
# Smoke artifacts live in the OS temp dir, never in the repo and never
# hardcoded to /tmp (wrong on Windows, ignores $TMPDIR elsewhere). node
# resolves the per-OS dir; the assignment is recursive (=) so non-node
# users never pay for it, falling back to $TMPDIR then /tmp.
OS_TMPDIR = $(shell node -e "process.stdout.write(require('os').tmpdir())" 2>/dev/null || echo $${TMPDIR:-/tmp})
# Prebuilt binary the smoke tests boot. Overridable via environment or
# command line (SMOKE_BIN=... make ci-local); otherwise the default above.
SMOKE_BIN ?= $(OS_TMPDIR)/gogogo-ci-local-web
# Wails v3 pin. Kept in sync with go.mod (wails/v3) and the install line in
# .github/workflows/desktop.yml. The desktop BUILD does not use the CLI (it is
# plain `go build`); the pin exists for `wails3 doctor` / `wails3 init` tooling.
WAILS_VERSION := v3.0.0-beta.24

.PHONY: all build desktop sqlc sqlc-check desktop-setup-cross desktop-cross-windows desktop-cross-darwin desktop-cross-linux desktop-cross-universal desktop-cross wails-build run clean restart templ fmt css css-install datastar-lint test lint vet check-sizes deadcode ci-local signoff check-skill-frontmatter deps dev docker-image setup rename help smoke smoke-prod-image smoke-clean gui run-gui lint-gui check-generated install-sh-guard check-classes daisyui-lint check-docs docs-index

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

# css-all is kept as an alias for css: one bundle ships, but CI, the Docker build
# and the pre-commit hook still call css-all, and a rename would have to touch
# all three. (Visual variants are DaisyUI themes, not bundles — docs/ui-themes.md.)
css-all: css

# css-check fails if the generated CSS is out-of-date. Used by the
# pre-commit hook and CI to catch forgotten rebuilds.
css-check: css-all
	@git diff --quiet --exit-code web/resources/static/app.min.css || (echo "  ❌ app.min.css out of date. Run \`make css\` and re-commit."; exit 1)
	@echo "  ✓ CSS is up to date"

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
# like data-tool/data-doc-id/data-test) are reported but do not block. Those go
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

# check-host-capacity is the T4 pre-flight. It measures this machine (free
# RAM, cores, 5-min load, free disk) and refuses `make ci-local` when the box
# cannot carry the full gate — full-repo lint + full race suite + four
# parallel Playwright suites pins a small shared host at 99% CPU and stops it
# answering (production and co-tenants starve). Same evidence-over-labels
# approach as lint-safe, one level up. Override with CI_LOCAL_FORCE=1.
check-host-capacity:
	@bash scripts/check-host-capacity.sh

# check-fast-capacity is the T3.5 pre-flight with the lighter budget
# (see the --fast thresholds in scripts/check-host-capacity.sh). A big
# changeset on a wedged box degrades further below via -p 1 instead of
# wedging it.
check-fast-capacity:
	@bash scripts/check-host-capacity.sh --fast

# check-scope enforces that every non-test, non-generated .go file
# under internal/ and features/ carries a leading doc-comment line
# matching `// SCOPE:layer=<infra|feature>,removal=<core|plugin|feature>`.
# Lives outside cmd/web/ on purpose: developer tool, not part of the
# runtime binary. Run after adding/removing files in those trees.
check-scope:
	@echo "→ check-scope (SCOPE annotation linter)..."
	@go run ./cmd/check-scope
	@echo "✅ SCOPE annotations present"

# check-classes makes the UI rules executable: every app-*/ui-*/is-* class in
# a class attribute must be defined in chrome.css, every selector there must be
# used somewhere, every class in docs/ui-shell.md's kit table must exist, and
# the DaisyUI part rules (toggle on an <input>, join-item inside a join,
# dropdown-content inside a dropdown) are enforced. The three failures it was
# written for — .ui-field-group (used, never defined), the phantom .ui-btn doc
# table, and a daisyUI component on the wrong element — all passed every other
# gate in this repo.
check-classes:
	@echo "→ check-classes (markup ↔ stylesheet ↔ doc)..."
	@go run ./cmd/check-classes --strict
	@echo "✅ classes and DaisyUI parts agree"

# daisyui-lint dogfoods the published linter on this repo. The version is pinned
# by the `tool` directive in go.mod (same rule as templ/deadcode/govulncheck);
# upgrading it is a reviewed dependency bump.
daisyui-lint:
	@echo "→ daisyui-lint (pinned tool, dogfood)..."
	@go tool daisyui-lint --strict \
		--css "web/resources/static/chrome.css,web/resources/static/app.css" \
		features internal

# vocabulary regenerates the snapshot EMBEDDED in cmd/daisyui-lint from the
# vendored skill. The snapshot is committed (the binary must work in a project
# with no vendored skill) and TestBundledVocabularyMatchesSkill fails when it is
# stale — the same rule as _templ.go and app.min.css: commit the artifact, let CI
# prove regeneration produces no diff.
vocabulary:
	@echo "→ regenerating internal/daisyui/vocabulary.json from the vendored skill..."
	@tmp=$$(mktemp) && \
		if go run ./cmd/daisyui-lint --emit-vocabulary > "$$tmp" && [ -s "$$tmp" ]; then \
			mv "$$tmp" internal/daisyui/vocabulary.json && \
			echo "✅ $$(grep -c 'base' internal/daisyui/vocabulary.json) components"; \
		else \
			rm -f "$$tmp"; echo "❌ generation failed; the committed snapshot is untouched"; exit 1; \
		fi
