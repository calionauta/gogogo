#!/bin/bash
# CSS staleness check — ported from .githooks/pre-commit.old.
# If a staged templ/go change affects generated classes, rebuilds the
# Tailwind bundle and blocks if the result is unstaged.
#
# Build through the Makefile, not `npm run build`, so this inherits
# css-install's dependency check: a node_modules tree whose Tailwind/DaisyUI
# version differs from package-lock.json produces a bundle that differs from
# the committed one, which would otherwise surface here as a FALSE "CSS is out
# of date" on a commit that changed no CSS source. The Makefile reinstalls on
# mismatch before building, so this hook only ever reports a real staleness.
#
# Build the artifact you diff. This hook once diffed a file it had not
# rebuilt, so a stale committed bundle passed pre-commit while `make css-check`
# failed on the same clean tree. One command (make css-check) now keeps this
# hook, the Makefile and CI in agreement.
set -e

grep -q "tailwindcss" package.json 2>/dev/null || exit 0

# Without node/npm there is nothing to build with. Say so loudly rather than
# passing silently: a silent skip is indistinguishable from "CSS is current".
if ! command -v npm >/dev/null 2>&1; then
  echo "⚠ npm not found; cannot verify CSS is current — install Node and re-run"
  exit 0
fi

echo "→ Rebuilding CSS (Tailwind v4 + DaisyUI v5)..."
make css-check >/dev/null

stale=()
git diff --quiet --exit-code web/resources/static/app.min.css 2>/dev/null || stale+=("app.min.css")

if [ ${#stale[@]} -gt 0 ]; then
  echo "❌ CSS is out of date: ${stale[*]}"
  echo "   Run \`make css\` and stage web/resources/static/"
  exit 2
fi
echo "✅ CSS up to date"
