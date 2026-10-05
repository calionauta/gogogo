# Datastar (.templ) gate

`golangci-lint` cannot see `.templ` markup. This gate covers it. Full context: `docs/code-quality.md`, `docs/local-ci.md`.

## Commands (authoritative)

```bash
make templ && make datastar-lint   # after any .templ change
```

- `make datastar-lint` = `bin/datastar-lint -only-errors -r ./features` (`Makefile`). `-only-errors` keeps intentional custom attrs green.
- CI installs with `go install github.com/calionauta/datastar-lint@latest` (`.github/workflows/ci.yml`). Locally use the wrapper (`bin/datastar-lint` falls back to `~/Development/datastar-lint`); never bypass with raw flags.
- Pre-commit runs it on `*.{templ,go}` (`bin/check-datastar.sh` scans `./features/ ./web/ ./internal/web/`). Air `pre_cmd` runs templgen + lint on every save (`.air.toml`).

## Rules

1. `PatchElements` whose top-level element lacks `id` + `WithSelector` throws `PatchElementsNoTargetsFound` client-side. Always pair `internal/datastar.RenderAndPatch` with an explicit selector.
2. Prefer Datastar attributes (`data-on:*`, signals, expressions, `__window`/`__document` modifiers) over vanilla JS. Inline JS only when unavoidable, adjacent to the markup (locality of behavior).
3. Intentional custom attributes (whiteboard `data-tool`/`data-doc-id`, Morpheus `data-neo-*`) go in `.datastar-lint.yaml` under `attributes.allowed` — never silence with broad ignores.

## Scan roots (why landing edits are safe)

Tailwind input scans only `features/`, `web/`, `internal/` (`source(none)` + explicit `@source`). Consequences:

- `site/**` and `docs/**` edits cannot stale `app.min.css` (landing uses its own stylesheet, zero Tailwind utils).
- `css-check` fails only when a class-shaped token in a scanned tree changes the bundle without `make css`. Rebuild, don't exclude.
- Test data / JSON / prose containing `p-1`-like tokens inside scanned trees creates spurious CSS — keep fixtures out or rename the key.
