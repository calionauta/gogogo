#!/bin/bash
# api-routes — pre-commit hook for the .templ/.js → Go route contract.
#
# Every /api/* URL a template or client script fires must match a route
# registered in Go; a renamed handler without its template is otherwise a
# silent dead button (Datastar gets a 404 and nothing renders). The check
# is one Go test (TestTemplAPIURLsHaveRoutes, ~1s incremental) — the same
# implementation CI runs, not a parallel bash reimplementation that could
# disagree with it.
set -uo pipefail

go test ./internal/capabilities/ -run 'TestTemplAPIURLsHaveRoutes' -count=1
