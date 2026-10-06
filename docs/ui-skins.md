# UI skins

Every page ships with a runtime-switchable UI skin. Two skins are compiled into
the same binary; the active one is chosen per process via env, per request via
query string, or interactively via the `SkinSelector` widget in the navbar.

| Skin | What it is | CSS / JS |
|------|-----------|----------|
| **DaisyUI** (default) | The reference UI. Server-rendered DaisyUI v5 components over TailwindCSS, morph-friendly with Datastar | `app.min.css` |
| **Basecoat** | [BasecoatUI](https://basecoatui.com) (a shadcn-style component lib) with shadcn-inspired OKLCH `@theme inline` color tokens, native Basecoat JS runtime (`basecoat.initAll`) debounced via `requestAnimationFrame` for Datastar DOM morphing | `basecoat.min.css` + `basecoat.min.js` |

> **⚠️ Basecoat is community-supported.** DaisyUI (the default) is the polished,
> battle-tested skin that gets the most development attention. Basecoat
> integrates correctly but is the secondary skin: it is developed against a
> third-party component library, so its class vocabulary is mapped onto the
> shared templates rather than being the origin of them.
>
> **Contributions are welcome.** Every skin is a self-contained directory under
> `web/skins/<name>/` — changes are scoped and safe.
>
> Both bundles are built from the same scan roots (`features/`, `web/`,
> `internal/`), so a utility class that resolves under one skin resolves under
> the other. The per-skin work is confined to class *vocabulary*: Basecoat has no
> `.steps`, no `data-variant="accent"`, and no DaisyUI semantic colours, so the
> skins differ in markup where the component model differs.
>
> A third skin (Morpheus, a web-components kit) shipped from v0.24.0 and was
> removed: its upstream was a two-month-old alpha and its layout classes
> (`cal14-grid`, `neo-tab-active`) had no CSS anywhere, so it rendered broken
> rather than merely differently. The changelog entry records the full list of
> what was deleted.

## Two ways to switch

1. **Env var (process-wide).** `UI_SKIN=basecoat ./gogogo`
   switches the active skin for the lifetime of the binary.
2. **Query string (per request).** Append `?skin=basecoat` to any route; the
   skin dispatcher reads it and renders that skin's assets without restart.
3. **Interactive selector.** The navbar exposes a `SkinSelector` that updates a
   query param and reloads.

## The plugin contract

`web/skins/skin.go` defines every skin as a `Skin{Name, Assets}` value,
registered at init time via blank imports in
`features/todo/components/skin_imports.go`. The dispatcher falls back to
DaisyUI when the env value is unknown, logging a warning.

**Adding a third skin:** create `web/skins/<name>/`, register it from the
import file, add a `make css-<name>` target. See
`web/skins/daisyui/skin.go` for the minimal reference implementation — assets
only, no Templ templates (those stay in the feature).

## Building the CSS bundles

```
src/css/input.css          →  tailwindcss v4 CLI  →  web/resources/static/app.min.css        (DaisyUI bundle)
src/css/basecoat-input.css →  tailwindcss v4 CLI  →  web/resources/static/basecoat.min.css  (Basecoat bundle)
                                                              │
                                                        //go:embed in the Go binary
```

```bash
make css         # DaisyUI (default)
make css-basecoat # Basecoat
make css-all     # every skin
```

> Editing a `.templ` without `make templ && make css` leaves
> `web/resources/static/app.min.css` stale. The pre-commit hook regenerates
> `app.min.css` automatically whenever `.templ` or `.go` files change, and
> `make ci-local` includes a `css-check` step that fails the gate if the
> working CSS file is out of date.

## Basecoat runtime gotcha

Do **not** call `basecoat.initAll` from inline page scripts. Datastar re-morphs
the DOM and loses JS-attached event handlers. The template debounces
`basecoat.initAll` via `requestAnimationFrame` instead.

## Removal

Delete `web/skins/`, drop the blank imports in
`features/todo/components/skin_imports.go`, and drop the `SkinSelector` call
from the navbar. The handler's lazy fallback returns DaisyUI assets when no
skin is registered.

## Related

- [UI sounds](ui-sounds.md)
- [Removing skins](scope-taxonomy.md#what-ships-as-what)