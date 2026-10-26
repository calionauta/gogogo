# Datastar (.templ) gate

`golangci-lint` cannot see `.templ` markup. This gate covers it. Full context: `docs/code-quality.md`, `docs/local-ci.md`.

## Commands (authoritative)

```bash
make templ && make datastar-lint   # after any .templ change
```

- `make datastar-lint` = `bin/datastar-lint -only-errors -r ./features ./internal` (`Makefile`). Two trees because the **Go analyzer** validates backend SDK calls (`sse.PatchElements`) which live under `internal/`; the HTML analyzer covers the `.templ` under `features/`. `-only-errors` keeps intentional custom attrs (`data-tool`, `data-doc-id`) green.
- CI pins the version in `go.mod`'s `tool` directive (`go tool datastar-lint`), **not** `@latest`: a new release can raise a rule to ERROR and fail the build with no code change here. One place, resolved by the toolchain — the `bin/datastar-lint` wrapper prefers it over a PATH binary.
- Locally use the wrapper (`bin/datastar-lint`); it forwards `"$@"` and adds `--analyzers html,go`. Never bypass it with raw flags — the wrapper is what supplies the analyzers.
- Pre-commit runs it on `*.{templ,go}` (`bin/check-datastar.sh`, scanning `./features/ ./web/ ./internal/`). Air `pre_cmd` runs templgen + lint on every save (`.air.toml`).

## API shape (read before writing a Go SDK call or rule)

Pinned core: `web/resources/static/datastar.js` is Datastar **v1.0.4**
(self-hosted, no CDN). Current docs live at `https://data-star.dev`
(guide + reference/attributes + reference/actions + reference/sse_events);
`datastar-lint` is tested against v1.0.4. The "Datastar v1.2.2" in
AGENTS.md is the **Go SDK** (`datastar-go`, the server side); the two
version independently — quote the bundle banner, not the SDK, when
reasoning about browser behavior. Version gaps already bitten:
`data-on-signal-patch-filter` does NOT exist in this core (use `data-effect`
on the signal instead); `data-effect`, `data-init`, `data-on-interval`,
`data-indicator`, `data-on:datastar-fetch` all verified present in the
bundle. When the docs show an attribute, grep the bundle before using it
— a silently-ignored attribute is worse than a missing one.

**No signal scoping in this core — the one gap that shapes component design.**
There is exactly ONE global signal store (`root` in `engine/signals.ts`),
addressed by absolute dot paths, and `data-signals` never receives the
element it sits on: a nested `data-signals` MERGES into that one store
(later write wins) and does not shadow for its subtree. The docs agree
("Signals are globally accessible from anywhere in the DOM").
Consequences, all verified against this bundle:

- Two instances of the same component collide on every generic name
  (`$filter`, `$loading`, `$newTitle`): one input drives both. Nesting via
  dot-notation is data depth, not DOM hierarchy.
- A LEADING underscore hides a signal from requests, not from the store:
  the request filter defaults to `exclude: /(^|\.)_/`. So `_foo` is
  client-only state, not local state.
- `data-scope-children` is dispatched by the core's morph code and has NO
  listener in the core bundle (1 constant, 2 dispatches, 0
  `addEventListener`s) — the core only ANNOUNCES that a scoped subtree was
  patched. Its consumer is Rocket: `datastar-rocket.js` registers
  `datastar-scope-children` itself (`this.addEventListener(we, this.#k)`) and
  sets the marker on every component host. So it is a real extension seam,
  not dead code — but it is useless without a plugin that listens, and it is
  undocumented. Never build on it directly.
- Rocket (`$$name`, per-instance paths under `_rocket.*`) is the intended
  fix, and it is the consumer of that scope event. It is NOT in the core
  bundle: core ships `datastar.js`/`-core`/`-aliased` only, and Rocket was
  Pro-only until v1.0.4 made it a free, BETA bundle ("API is subject to
  change") at `bundles/datastar-rocket.js` — verified, and it must be loaded
  IN ADDITION to core, not instead of it. Two measured caveats before
  adopting it:
  1. **Its instance state lives in `_rocket.<instance>`, and the REQUEST path
     keeps the core `_`-exclude.** Rocket's own expression default is
     `exclude: /(?!)/` (it wants `$$x` visible to expressions), but the
     `@get/@post` option default is still `exclude: /(^\.?|\\.)_/`. So
     `_rocket.*` does NOT reach the server unless the call opts in via
     `filterSignals`. A server-driven panel must read/write the shared
     `__root` surface, not a local — the same shape as `$foo` today.
  2. **Components render into a SHADOW ROOT by default** (`attachShadow`,
     mode `open`, with `light` as the opt-out). Shadow DOM blocks
     document-level CSS: our Tailwind/DaisyUI classes would resolve only
     because CSS custom properties inherit through shadow boundaries and
     `app.min.css` is loaded at document level — utility classes defined for
     the document do NOT style shadow-internal elements. Any Rocket component
     must use the `light` mode or restate styles inside.

  **Verdict for this repo: do not adopt Rocket.** It buys automatic
  per-instance naming, which the prefix helper already gives us for free
  (`internal/components/instance.go`), and it costs a second 65 KB beta
  bundle, a shadow-DOM/light-mode decision in every component, and a
  beta API. Revisit if a component must exist 3+ times on one page with
  independent state AND the prefix helper has become the bottleneck.

  **What to do instead (house pattern):** prefix the signal names per
  instance and give each instance its own patch-target id — one helper,
  `internal/components/instance.go` (`InstanceID`, `SignalNames`, `Signals`),
  which is the generalized form of the toast precedent. Two hard rules:
  (1) never share an `id` between instances — no-selector patches resolve
  via `getElementById` (FIRST match only) and selector patches hit EVERY
  match, so one patch would morph both; (2) an un-prefixed reference fails
  SILENTLY by falling through to the global store, which reads as
  cross-talk, not as an error — the helper's `Signals` map is what makes
  the names come from one place, and `make datastar-lint` is where the
  mistake is caught.

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
   Hub record-event subscribe (not Datastar events). A `setInterval`
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
7. **A `data-bind` signal is client-owned: never write it from a server
   response.** `datastar-lint` enforces this
   (`CLIENT_BOUND_SIGNAL_NO_OMITEMPTY` = error, `CLIENT_BOUND_SIGNAL_WRITTEN` =
   error) across all **six** write forms — literal and constant map keys,
   struct-literal fields, field assignments (the Go side), plus
   `data-signals:key` and `data-signals={obj}` (the mark-up side; both honour
   the `ifmissing` modifier) — so this is a pointer, not the rule. **The
   preferred fix is to stop writing the field at all**: keep it off the wire
   (`json:"-"`, or `omitempty` where the server may set it later) and render
   the value into the input's `value=`; `data-bind` seeds the signal from it
   with `{ifMissing: true}` (upstream `bind.ts`), so no server merge is needed
   and no suppression is required. The two traps a reader still needs are that
   a response **echoing the request's own parameter** is the same bug (a late
   `/search` reply for `?q=kes` re-imposes "kes" over the "kestrel" typed
   since, which is why `displaySignals` seeds nothing and the URL is
   authoritative only once, at page load), and that
   `MarshalAndPatchSignalsIfMissing` / `WithOnlyIfMissing(true)` is the
   documented escape hatch (a write routed
   through it cannot overwrite an existing signal, so the check allows it).
   Full reasoning and the lived bug: `docs/code-quality.md`,
   `docs/realtime-recipe.md`.
8. **A template that owns `<html>` must wire `$theme`, and must render
   `ThemeHead`.** The toggle lives in `Navbar`, so any page including the
   shell shows a working-looking button — but `$theme` only acts when `<html>`
   carries `data-signals` + `data-attr:data-theme` + `data-class:dark`. Without
   them the click hides BOTH icons (the CSS reveals the moon only under
   `html.dark`) and the theme never changes: the button appears to vanish. Use
   the helper, never a copy: `<html lang="en" { authviews.ThemeRootAttrs("")... }>`
   (or `ThemeRootAttrs(MarshalSignals(signals))` to keep the page's own seeds).
   `ThemeHead` carries no icon script: icons are inline SVG
   (`internal/icon.Icon`), so there is no web-component tag to forget and no
   0×0 flash — that whole failure shape is gone. Guards:
   `features/authviews/theme_test.go` (template scan) plus the browser smoke.

## Scan roots (why landing edits are safe)

Tailwind input scans only `features/`, `web/`, `internal/` (`source(none)` + explicit `@source`). Consequences:

- `site/**` and `docs/**` edits cannot stale `app.min.css` (landing uses its own stylesheet, zero Tailwind utils).
- `css-check` fails only when a class-shaped token in a scanned tree changes the bundle without `make css`. Rebuild, don't exclude — **unless the tree renders no HTML at all.** `internal/installer/**` is excluded (`@source not`) for exactly that reason: it is the CLI, it only NAMES classes as data (and its prose contains words like "diff"), so scanning it injected spurious DaisyUI CSS. Every class it names is defined in a `.templ` that IS scanned, so nothing is lost. Exclude only a package that renders no markup; anything that renders belongs in the scan.
- Test data / JSON / prose containing `p-1`-like tokens inside scanned trees creates spurious CSS — keep fixtures out or rename the key.
- **Model-authored markup has no source to scan**, so its classes are safelisted explicitly: `@source inline("…")` in `src/css/input.css` (the themed half) and `features/genui/components/htmlclasses.go`'s structural half. The list is that Go file, the prompt shows exactly it (`ModelHTMLVocabulary`), and `TestModelHTMLClassVocabulary` fails when the Go list, the safelist and the built bundle disagree — with the fix in the message. A class the model invents that is NOT in that list renders unstyled: measured before the safelist existed, `opacity-60` and `sm:grid-cols-3` (both used by the keyless chat demo answer) were absent from `app.min.css`.

## Realtime patterns (don't invent transports)

`docs/realtime-recipe.md` is the recipe: hidden-`@get` streams (`retry: 'always'`, see below),
`ListResync` for record lists, fragment morphs with selector
headers, `data-effect` for signal reactions (never polling), `fail()` on
500s, `ic.AutoScroll()` as the last child of an append-only feed (it
rides the patch that carries the new item, so no listener is needed), and
the conformance test that pins every `/api/*` URL to a Go route.
MCP/CLI surfaces the same knowledge via `advise` (presets
`realtime-transport`, `ai-features`); the skill is the execution half.
## Upstream watch (before and after a bundle bump)

Pinned browser core: the banner in `web/resources/static/datastar.js` (**v1.0.4**).
The Go SDK (`datastar-go`) versions independently. Unknown option keys are
silently ignored, so grep the bundle before trusting a documented attribute
or option.

**Shipped / closed**

- **#1073 — `retry: 'auto' | 'error' | 'always' | 'never'`** (released in
  v1.0.0-RC.7). `always` reconnects after a CLEAN server close too, with
  backoff (1s→30s, `retryMaxCount` resets on every 200); a `204` stops it.
  The proposed `permanent` option never shipped, and `Last-Event-ID` IS
  resent on reconnect.
- **#1068 — server-closed streams**: under the default `auto`, a graceful
  close is terminal. That is why every opener passes `retry: 'always'`.

**Open**

- **#1203 — native restart handle for silent socket death** (mobile deep
  sleep; fetch never rejects). Open, no milestone, no assignee. The
  maintainer's workaround is re-issuing the action from `resume` /
  `pageshow`; `__gogogoSSERefresh` does exactly that today.
- **No issue tracks a native open-ack.** The maintainer points to
  `datastar-fetch` events; the ack signal (`AckStreamOpened`) is this
  repo's proof of delivery, not a placeholder for an upstream feature.

**Re-check on every bump** (each is load-bearing here):

1. `retry` semantics, backoff and the 204 stop (`retry: 'always'` on every opener).
2. `openWhenHidden` default for GET (the room depends on opting in).
3. `datastar-fetch` event `type` values and the `datastar-signal-patch`
   `detail` shape (sse-open.js matches `detail.streamOpen`, which carries
   `<clientID>:<nonce>` — a bare clientID repeats on reconnect and
   Datastar never dispatches an unchanged value, so the ack must differ
   per open).
4. `requestCancellation` by method+URL (the forced refresh depends on it).
5. Markov/morph behavior for `mode: outer` (the room patches `#room-card-content`).
6. Attribute existence in the bundle (the docs can be ahead of the pin).

When #1203 ships a restart primitive, delete the re-click half of
`__gogogoSSERefresh` and call the primitive; if an open-ack ever ships,
delete the ack loop in `sse-open.js` (nothing tracks one yet).

