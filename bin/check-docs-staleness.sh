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

# What diff to judge. The default is the INDEX: the pre-commit hook asks
# "does this commit carry its docs?". CI asks the same question about the
# PR: `--diff <base>..HEAD` judges the branch diff instead. One
# implementation, two callers — the answer must not depend on who asks.
DIFF=(--cached)
if [ "${1:-}" = "--diff" ]; then
  DIFF=("${2:-}")
fi

# Nothing changed (e.g. run standalone) — nothing to judge.
git diff "${DIFF[@]}" --quiet && exit 0

STAGED=$(git diff "${DIFF[@]}" --name-only)

# Already touching docs? Then the author has made the call; stay quiet.
# "Docs" is every surface a reader reads: the pages, the agent index, the
# skills and the landing. A change that updates only llms.txt has made the
# call too.
if echo "$STAGED" | grep -qE '^(docs/.*\.md|README\.md|llms\.txt|AGENTS\.md|skills/.*\.md|site/index\.html)$'; then
  echo "✅ docs touched alongside the change"
  exit 0
fi

# Which behaviour surfaces changed? These are the files whose content the docs
# make claims about. Editing them without editing docs/ is the exact drift this
# check looks for. Everything else (tests, generated files, assets, tooling) is
# deliberately excluded — those do not change documented behaviour.
#
# The globs were written for a tree that no longer exists (`router/`, `db/`,
# `internal/nats`, `internal/llm`, `internal/secrets`, `internal/datastar`,
# `config/config.go`): the watcher had been silent for every surface that
# matters most — the CLI, the MCP server, the capability registry and the
# recipe contract. The list below is the tree as it is now.
SURFACE=$(echo "$STAGED" | grep -E \
  '^(cmd/gogogo/.*\.go|cmd/gogogo-mcp/.*\.go|internal/(capabilities|recipe|installer|ui|uiregistry|rbac|auth|search|observability|llm|collab|dagnats|nats|queue|secrets)/.*\.go|features/.*\.go|config/.*\.go|web/.*\.go|Makefile|\.github/workflows/.*\.yml|scripts/.*\.(sh|mjs)|bin/.*\.sh)$' \
  | grep -vE '(_test\.go|_templ\.go)$' || true)

[ -n "$SURFACE" ] || exit 0

COUNT=$(echo "$SURFACE" | wc -l | tr -d ' ')
# A GitHub annotation makes the signal visible ON the PR, not only in a
# terminal: the reader of the diff sees it without running the hook.
warn() { if [ "${CI:-}" = "true" ]; then echo "::warning::$1"; else echo "$1"; fi; }
warn "⚠️  docs staleness: $COUNT behaviour-carrying file(s) changed, no docs/*.md in the diff"
echo "$SURFACE" | head -12 | sed 's/^/    /'
[ "$COUNT" -gt 12 ] && echo "    … and $((COUNT - 12)) more"

# Point at the page most likely to need the edit. Deliberately coarse — a hint,
# not an oracle; the author knows better than a glob does.
echo
echo "    Likely page(s) to review:"
case "$SURFACE" in
  *cmd/gogogo-mcp/*)   echo "      docs/mcp.md · cmd/gogogo-mcp/README.md  (the tool table is generated: run make docs-index)" ;;
esac
case "$SURFACE" in
  *cmd/gogogo/*)       echo "      docs/cli.md · cmd/gogogo/README.md  (every verb needs a row)" ;;
esac
case "$SURFACE" in
  *internal/capabilities/*|*internal/recipe/*)
                        echo "      docs/scope-taxonomy.md · skills/gogogo-recipe-authoring/SKILL.md  (the removal class, the recipe contract)" ;;
esac
case "$SURFACE" in
  *internal/installer/*) echo "      docs/cli.md · docs/local-ci.md  (the verbs and the gates)" ;;
esac
case "$SURFACE" in
  *internal/ui/*|*internal/uiregistry/*) echo "      docs/ui-shell.md · skills/gogogo-ui/SKILL.md  (the shell vocabulary)" ;;
esac
case "$SURFACE" in
  *internal/rbac/*|*internal/auth/*)    echo "      docs/rbac.md · docs/security.md  (the policy rows)" ;;
esac
case "$SURFACE" in
  *internal/llm/*|*internal/search/*|*internal/observability/*)
                        echo "      docs/llm-and-credits.md · docs/search.md" ;;
esac
case "$SURFACE" in
  *internal/collab/*|*internal/dagnats/*|*internal/nats/*|*internal/queue/*)
                        echo "      docs/async-layers.md · docs/features.md  (the async layers)" ;;
esac
case "$SURFACE" in
  *internal/secrets/*)     echo "      docs/getting-started.md  (secrets setup) · docs/configuration.md" ;;
esac
case "$SURFACE" in
  *config/*)               echo "      docs/configuration.md  (every env var + default)" ;;
esac
case "$SURFACE" in
  *features/*)             echo "      docs/features.md · docs/scope-taxonomy.md  (capability + opt-out)" ;;
esac
case "$SURFACE" in
  *Makefile*)              echo "      docs/local-ci.md · docs/getting-started.md  (the commands)" ;;
esac
case "$SURFACE" in
  *.github/workflows/*|*scripts/*|*bin/*) echo "      docs/local-ci.md · docs/deploy.md  (CI + deploy reality)" ;;
esac

echo
echo "    If the change is internal-only (rename, refactor, test), ignore this."
echo "    If it alters behaviour, update the page above in this commit —"
echo "    a stale doc is a second source of truth read by the next person."
echo "    Then: make site   (regenerates the published HTML; never edit site/docs/)"

# Non-blocking by design. See the header for why.
exit 0
