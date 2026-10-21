#!/bin/bash
# CSS staleness check — ported from .githooks/pre-commit.old.
# If a staged templ/go change affects generated classes, rebuilds every
# Tailwind bundle and blocks if the result is unstaged.
#
# Build through the Makefile, not `npm run build`, so this inherits
# css-install's dependency check: a node_modules tree whose Tailwind/DaisyUI
# version differs from package-lock.json produces a bundle that differs from
# the committed one, which would otherwise surface here as a FALSE "CSS is out
# of date" on a commit that changed no CSS source. The Makefile reinstalls on
# mismatch before building, so this hook only ever reports a real staleness.
#
# All skins, not just the DaisyUI bundle. This hook used to run `make css`
# and diff `app.min.css` alone, so a stale `basecoat.min.css` passed
# pre-commit, passed CI, and shipped — while `make css-check` (which builds
# css-all) failed on the same clean tree. The shadcn bundle repeated the same
# hole. One command, `make css-all`, keeps this hook, the Makefile's
# css-check, and CI in agreement.
set -e

grep -q "tailwindcss" package.json 2>/dev/null || exit 0

# Without node/npm there is nothing to build with. Say so loudly rather than
# passing silently: a silent skip is indistinguishable from "CSS is current".
if ! command -v npm >/dev/null 2>&1; then
  echo "⚠ npm not found; cannot verify CSS is current — install Node and re-run"
  exit 0
fi

echo "→ Rebuilding CSS (Tailwind v4 + DaisyUI v5 + Basecoat + shadcn)..."
make css-all >/dev/null

stale=()
git diff --quiet --exit-code web/resources/static/app.min.css 2>/dev/null || stale+=("app.min.css")
git diff --quiet --exit-code web/resources/static/basecoat.min.css 2>/dev/null || stale+=("basecoat.min.css")
git diff --quiet --exit-code web/resources/static/shadcn.min.css 2>/dev/null || stale+=("shadcn.min.css")

if [ ${#stale[@]} -gt 0 ]; then
  echo "❌ CSS is out of date: ${stale[*]}"
  echo "   Run \`make css-all\` and stage web/resources/static/"
  exit 2
fi
echo "✅ CSS up to date (all skins)"
