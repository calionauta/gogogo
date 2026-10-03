# Code quality

The template ships a strict `golangci-lint` configuration (27 linters)
designed to catch the kinds of mistakes LLMs make most often: unchecked errors,
insecure patterns, broken context propagation, resource leaks, and inconsistent
error wrapping. The goal is not to block development but to redirect agents
toward correct Go idioms automatically.

## What the linters enforce

| Category | Linters | What they catch |
|----------|---------|-----------------|
| Correctness | `govet`, `staticcheck`, `errcheck`, `ineffassign`, `unused` | Shadowed variables, dead code, unchecked returns |
| Error handling | `errorlint`, `nilerr`, `gosec` | Wrong `%w` formatting, returning nil inside an error path, hardcoded credentials |
| Resource safety | `bodyclose`, `noctx` | HTTP bodies and contexts not closed or propagated |
| Test quality | `thelper`, `testifylint`, `sloglint`, `containedctx` | Missing `t.Helper()`, `assert` vs `require` misuse, context embedded in structs |
| Complexity | `gocyclo`, `gocognit`, `funlen` | Functions too long or too nested to hold in working memory |
| Style | `revive`, `gocritic`, `tagliatelle`, `goconst`, `dupl`, `lll`, `modernize` | Non-idiomatic patterns, magic numbers, duplicated code, long lines |
| Formatting | `gofumpt` + `goimports` (formatters, not linters) | Compulsory consistent layout and import ordering |

The configuration lives in `.golangci.yml` at the project root — read it if you
need to understand what each linter expects.

**If a lint forces you to restructure code, that is usually a sign the original
approach had a deeper issue.**

## Running each layer

| Command | What it checks |
|---|---|
| `make lint` | `go vet` + `golangci-lint` (27 linters) over the web packages — `scripts/web-packages.sh` excludes `cmd/desktop` and `cmd/gui` |
| `make datastar-lint` | Datastar-specific anti-patterns in `.templ` files |
| `make fmt` | `gofumpt` + `goimports` formatting only |
| `make ci-local` | Full local gate, identical to CI: templ → datastar-lint → css-check → check-scope → golangci-lint → race tests → build |

We deliberately keep `gofumpt` and `goimports` as **formatters** (not linters)
so `golangci-lint run` never auto-formats your files — formatting is a separate
explicit step. `ci-local` uses `golangci-lint` as the authoritative formatter
gate rather than the standalone `gofumpt` binary, which can be a newer release
than the one golangci-lint bundles and would otherwise produce false-positive
listings.

## Scope your lint runs

`golangci-lint run ./...` on the whole repo is ~10× slower than scoping to the
packages you touched. Always scope:

```bash
golangci-lint run ./features/todo/... ./router/...
```

Full-repo lint is reserved for `make lint` and `make ci-local`.

## Git hooks

The lefthook hooks (`make setup`) run `gofumpt`, `goimports`, `datastar-lint`, a
CSS staleness check, `go mod tidy`, the SCOPE annotation linter, and
`golangci-lint` on every commit — so formatting and lint violations never reach
the remote. Jobs are glob-filtered (only run when matching files are staged) and
executed in parallel.

Run **`make signoff`** for the full gate before pushing. See
[Local CI](local-ci.md) for the tier ladder and why it beats waiting on remote
runners.

## Datastar-specific rules

`make datastar-lint` catches mistakes `golangci-lint` cannot see, because they
live in `.templ` markup rather than Go:

- A `PatchElements` whose top-level element lacks an `id` and a
  `WithSelector` selector throws `PatchElementsNoTargetsFound` in the client.
  Pair `internal/datastar.RenderAndPatch` with an explicit selector.
- Prefer Datastar attributes (`data-on:*`, signals, expressions,
  `__window`/`__document` modifiers) over vanilla JS for client-side logic.
  Inline JS only when unavoidable, kept adjacent to the markup so the behavior
  stays local.

The lint runs with `-only-errors` in CI so intentional custom attributes (the
Morpheus skin's `data-neo-*` attributes, for example) do not fail the gate.
Add genuinely intentional attributes to `.datastar-lint.yaml` under
`attributes.allowed`.

## File size limits

The pre-commit hook runs a `file-sizes` check. Large files are a smell in this
template specifically because they are usually a sign that a feature grew
monolithically — split by concern rather than raising the limit.

## Related

- [Local CI](local-ci.md) — the tier ladder and `make signoff`.
- [Scope taxonomy](scope-taxonomy.md) — the other structural linter
  (`check-scope`).