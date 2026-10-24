#!/bin/bash
# check-no-jsx.sh — fail on JSX idioms that templ passes through silently.
#
# Why this exists: most JSX inside a .templ file fails LOUDLY — arrow
# functions and `{...}` object literals are not valid Go expressions, so
# `templ generate` rejects them and the generated-templ hook goes red.
# `className` is the exception. Templ passes unknown attributes through
# verbatim, so `className="btn"` compiles clean (verified: exit 0) and
# renders `<div className="btn">` — an attribute browsers ignore, i.e. a
# silently unstyled control with no error anywhere. It is also the single
# most distinctive JSX-ism an LLM imitates from the vendored daisyui skill
# (whose framing is "HTML or JSX").
#
# The same silence covers CDN links: the daisyui skill advertises
# cdn.jsdelivr.net/unpkg alternatives, and `css-check` cannot see them (it
# proves the committed bundle matches its inputs, not that no second bundle
# ships). Both patterns are banned by skills/gogogo-ui/SKILL.md; this is
# the deterministic backstop for the sessions where the skill does not fire.
#
# Scope mirrors src/css/input.css: features/, web/, internal/ only. site/
# has its own stylesheet and docs/ renders no app markup.
#
# Exit codes: 0 clean, 1 forbidden pattern found, 2 usage/filesystem error.

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TREES=("features" "web" "internal")

fail=0

# 1. className in .templ: the silently-passing JSX-ism. `class` is the only
# legitimate spelling in this repo; grep is exact enough that no allowlist
# is needed (zero legitimate hits in-tree, verified).
if grep -rn --include='*.templ' 'className' "$ROOT/features" "$ROOT/web" "$ROOT/internal" 2>/dev/null; then
  echo "  ❌ className in .templ — use class= (className renders verbatim and unstyled)" >&2
  fail=1
fi

# 2. CDN stylesheets/scripts in app markup: a second bundle beside
# /static/app.min.css. Matches the daisyui skill's advertised alternatives.
if grep -rEn --include='*.templ' --include='*.go' 'cdn\.jsdelivr\.net|unpkg\.com' "$ROOT/features" "$ROOT/web" "$ROOT/internal" 2>/dev/null \
  | grep -v '_test\.go' | grep -v '_templ\.go'; then
  echo "  ❌ CDN link in app markup — bundle is /static/app.min.css only (see docs/ui-themes.md)" >&2
  fail=1
fi

if [ "$fail" -eq 0 ]; then
  echo "  ✓ no JSX idioms in app markup"
fi
exit "$fail"
