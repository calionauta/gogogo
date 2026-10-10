# Datastar (.templ) gate

`golangci-lint` cannot see `.templ` markup. This gate covers it. Full context: `docs/code-quality.md`, `docs/local-ci.md`.

## Commands (authoritative)

```bash
make templ && make datastar-lint   # after any .templ change
```

- `make datastar-lint` = `bin/datastar-lint -only-errors -r ./features ./internal` (`Makefile`). Two trees because the **Go analyzer** validates backend SDK calls (`sse.PatchElements`) which live under `internal/`; the HTML analyzer covers the `.templ` under `features/`. `-only-errors` keeps intentional custom attrs (`data-tool`, `data-doc-id`) green.
- CI pins the version (`go install github.com/calionauta/datastar-lint@v0.13.3`), **not** `@latest`: a new release can raise a rule to ERROR and fail the build with no code change here.
- Locally use the wrapper (`bin/datastar-lint`); it forwards `"$@"` and adds `--analyzers html,go`. Never bypass it with raw flags — the wrapper is what supplies the analyzers.
- Pre-commit runs it on `*.{templ,go}` (`bin/check-datastar.sh`, scanning `./features/ ./web/ ./internal/`). Air `pre_cmd` runs templgen + lint on every save (`.air.toml`).

## API shape (read before writing a Go SDK call or rule)

Pinned core: `web/resources/static/datastar.js` is Datastar **v1.0.3**
(self-hosted, no CDN). Current docs live at `https://data-star.dev`
(guide + reference/attributes + reference/actions + reference/sse_events);
`datastar-lint` is tested against v1.0.4. Version gaps already bitten:
`data-on-signal-patch-filter` does NOT exist in v1.0.3 (use `data-effect`
on the signal instead); `data-effect`, `data-init`, `data-on-interval`,
`data-indicator`, `data-on:datastar-fetch` all verified present in the
bundle. When the docs show an attribute, grep the bundle before using it
— a silently-ignored attribute is worse than a missing one.

The Datastar Go SDK is **method-only**. Every patch call is a method on
`*datastar.ServerSentEventGenerator` from `datastar.NewSSE(w, r)`:

```go
sse := datastar.NewSSE(w, r)
sse.PatchElements(`<div id="x">x</div>`, datastar.WithSelector("#x"))
sse.RemoveElement("#temporary")
_ = sse.MarshalAndPatchSignals(map[string]any{"k": "v"})
```

There is **no** package-level `datastar.PatchElements(sse, ...)` — never has been
in any 1.x release, and writing it does not compile (`undefined:
datastar.PatchElements`). Only the *option constructors* are package-level
(`datastar.WithSelector`, `datastar.WithSelectorID`, `datastar.WithModeAppend`…).
`PatchElementf`, `RemoveElementf` and `RemoveElementByID` take **no** options, so
they can never be "missing a selector".

## Rules

1. `PatchElements` needs a selector or the client throws `PatchElementsNoTargetsFound` and the update silently never lands. Pair `internal/datastar.RenderAndPatch` with an explicit selector. This is an **error** in the linter, so it fails the gate.
2. A helper taking `opts ...PatchElementOption` and forwarding them (`sse.PatchElements(html, opts...)`) is correct and must not be flagged — the selector comes from the caller.
3. Vanilla JS is the last resort, never the default (locality of
   behavior). Before writing any inline `<script>` or static `.js`,
   enumerate the Datastar primitive that could own it (`data-on:*`
   incl. `__window`/`__document`, `data-effect`, `data-on-interval`,
   `data-on-signal-patch`, `data-indicator`, `data-init`) and — when
   none fits — write the reason in a comment next to the script. The
   standing allowlist (each proven against the primitive list):
   canvas/op batching + Loro + outbox (`whiteboard.js`, `notes.js`),
   Service Worker + global controllers (`sw.js`, `session.js`,
   `theme.js`), stream bootstrap openers (no `data-on:load` on body),
   PB-realtime subscribe (not Datastar events). A `setInterval`
   polling loop or `innerHTML` render is never acceptable for signal
   changes or fragment updates — use `data-effect` /
   `data-on-interval` + fragment morph (`docs/realtime-recipe.md`).
4. Intentional custom attributes (whiteboard `data-tool`/`data-doc-id`, cuelume `data-cuelume-*`) go in `.datastar-lint.yaml` under `attributes.allowed` — never silence with broad ignores.
5. The action list tracks the Datastar core release: `@query()` is v1.0.4+. An action missing from the linter's regex falls through to the "no action matched" branch and silently skips the URL-format and method checks, so re-check this list on a core upgrade.
6. Templ does not interpolate `{...}` inside `<script>` bodies or inside
quoted attribute strings (`data-room="{ id }"` renders literally). Pass
values to inline scripts as unquoted data attributes
(`data-room={ roomID }`) read back with `getAttribute`. Neither
datastar-lint nor the Go linters see this class — the browser smoke
(`scripts/smoke.mjs`, which loads every page and fails on JS errors)
is the gate that catches it.

## Scan roots (why landing edits are safe)

Tailwind input scans only `features/`, `web/`, `internal/` (`source(none)` + explicit `@source`). Consequences:

- `site/**` and `docs/**` edits cannot stale `app.min.css` (landing uses its own stylesheet, zero Tailwind utils).
- `css-check` fails only when a class-shaped token in a scanned tree changes the bundle without `make css`. Rebuild, don't exclude — **unless the tree renders no HTML at all.** `internal/installer/**` is excluded (`@source not`) for exactly that reason: it is the CLI, it only NAMES classes as data (and its prose contains words like "diff"), so scanning it injected spurious DaisyUI CSS. Every class it names is defined in a `.templ` that IS scanned, so nothing is lost. Exclude only a package that renders no markup; anything that renders belongs in the scan.
- Test data / JSON / prose containing `p-1`-like tokens inside scanned trees creates spurious CSS — keep fixtures out or rename the key.

## Realtime patterns (don't invent transports)

`docs/realtime-recipe.md` is the recipe: hidden-`@get` permanent streams,
`PbRealtimeResync` for record lists, fragment morphs with selector
headers, `data-effect` for signal reactions (never polling), `fail()` on
500s, and the conformance test that pins every `/api/*` URL to a Go
route. MCP/CLI surfaces the same knowledge via `advise` (presets
`realtime-transport`, `ai-features`); the skill is the execution half.
