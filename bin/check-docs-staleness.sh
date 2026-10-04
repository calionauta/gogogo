#!/bin/bash
# Docs staleness — warns when a behaviour-carrying change lands without a
# matching docs/ update.
#
# Why this exists: docs/*.md (18 pages, published to GitHub Pages by
# site/build.mjs) describes the current product. A veracity audit of this repo
# found 15 claims that were true of an earlier README and false of the code —
# the deploy directory, the CI's build strategy, a non-existent /api/version
# endpoint. None of them were caught by any check, because nothing linked the
# prose to the code. This is that link, kept deliberately cheap and
# non-blocking.
#
# Policy (mirrors stelow's "Docs stay truthful" and bb-plugin-stelow's "a
# feature without an entry does not exist"):
#   - docs/ is the single source of truth for behaviour; README links to it.
#   - A commit that changes behaviour updates the doc IN THE SAME COMMIT.
#   - site/docs/ is generated — never edit it; run `make site`.
#
# This check WARNS, it does not block. A rename, an internal refactor, or a
# test-only change legitimately touches Go without touching prose, and a
# blocking gate here would train people to bypass it. Exit is always 0; the
# value is the printed signal plus the suggested page.
set -e

[ -f go.mod ] || exit 0
[ -f AGENTS.md ] || exit 0

# Nothing staged (e.g. run standalone in CI) — nothing to judge.
git diff --cached --quiet && exit 0

STAGED=$(git diff --cached --name-only)

# Already touching docs? Then the author has made the call; stay quiet.
if echo "$STAGED" | grep -qE '^(docs/.*\.md|README\.md)$'; then
  echo "✅ docs touched alongside the change"
  exit 0
fi

# Which behaviour surfaces changed? These are the files whose content the docs
# make claims about. Editing them without editing docs/ is the exact drift this
# check looks for. Everything else (tests, generated files, assets, tooling) is
# deliberately excluded — those do not change documented behaviour.
SURFACE=$(echo "$STAGED" | grep -E \
  '^(config/config\.go|router/.*\.go|db/.*\.go|Makefile|\.github/workflows/.*\.yml|scripts/.*\.(sh|mjs)|features/.*\.go|internal/(queue|nats|dagnats|llm|collab|secrets|datastar)/.*\.go)$' \
  | grep -vE '(_test\.go|_templ\.go)$' || true)

[ -n "$SURFACE" ] || exit 0

COUNT=$(echo "$SURFACE" | wc -l | tr -d ' ')
echo "⚠️  docs staleness: $COUNT behaviour-carrying file(s) changed, no docs/*.md staged"
echo "$SURFACE" | head -12 | sed 's/^/    /'
[ "$COUNT" -gt 12 ] && echo "    … and $((COUNT - 12)) more"

# Point at the page most likely to need the edit. Deliberately coarse — a hint,
# not an oracle; the author knows better than a glob does.
echo
echo "    Likely page(s) to review:"
case "$SURFACE" in
  *config/config.go*)      echo "      docs/configuration.md  (every env var + default)" ;;
esac
case "$SURFACE" in
  *router/*)               echo "      docs/architecture.md · docs/todo-example.md  (routes, transports)" ;;
esac
case "$SURFACE" in
  *internal/queue/*|*internal/dagnats/*|*internal/nats/*)
                           echo "      docs/async-layers.md · docs/features.md  (the six async layers)" ;;
esac
case "$SURFACE" in
  *internal/llm/*)         echo "      docs/llm-and-credits.md" ;;
esac
case "$SURFACE" in
  *internal/collab/*)      echo "      docs/todo-example.md · docs/architecture.md  (whiteboard/CRDT)" ;;
esac
case "$SURFACE" in
  *internal/secrets/*)     echo "      docs/getting-started.md  (secrets setup) · docs/configuration.md" ;;
esac
case "$SURFACE" in
  *Makefile*)              echo "      docs/getting-started.md · docs/local-ci.md  (commands)" ;;
esac
case "$SURFACE" in
  *.github/workflows/*|*scripts/*) echo "      docs/deploy.md · docs/local-ci.md  (CI + deploy reality)" ;;
esac
case "$SURFACE" in
  *features/*)             echo "      docs/features.md · docs/scope-taxonomy.md  (capability + opt-out)" ;;
esac

echo
echo "    If the change is internal-only (rename, refactor, test), ignore this."
echo "    If it alters behaviour, update the page above in this commit —"
echo "    a stale doc is a second source of truth read by the next person."
echo "    Then: make site   (regenerates the published HTML; never edit site/docs/)"

# Non-blocking by design. See the header for why.
exit 0
