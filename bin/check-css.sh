#!/bin/bash
# CSS staleness check — ported from .githooks/pre-commit.old.
# If a staged templ/go change affects generated classes, rebuilds the
# Tailwind v4 + DaisyUI v5 bundle and blocks if the result is unstaged.
#
# It builds through `make css`, not `npm run build`, so it inherits
# css-install's dependency check: a node_modules tree whose Tailwind/DaisyUI
# version differs from package-lock.json produces a bundle that differs from
# the committed one, which would otherwise surface here as a FALSE "CSS is out
# of date" on a commit that changed no CSS source. `make css` reinstalls on
# mismatch before building, so this hook only ever reports a real staleness.
set -e

grep -q "tailwindcss" package.json 2>/dev/null || exit 0

# Without node/npm there is nothing to build with. Say so loudly rather than
# passing silently: a silent skip is indistinguishable from "CSS is current".
if ! command -v npm >/dev/null 2>&1; then
  echo "⚠ npm not found; cannot verify CSS is current — install Node and re-run"
  exit 0
fi

echo "→ Rebuilding CSS (Tailwind v4 + DaisyUI v5)..."
make css >/dev/null

if ! git diff --quiet --exit-code web/resources/static/app.min.css 2>/dev/null; then
  echo "❌ CSS is out of date. Run \`make css\` and stage web/resources/static/app.min.css"
  exit 2
fi
echo "✅ CSS up to date"
