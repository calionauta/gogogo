# UI skins

Every page ships with a runtime-switchable UI skin. Three skins are compiled
into the same binary; the active one is chosen per process via env, per request
via query string, or interactively via the `SkinSelector` widget in the navbar.

| Skin | What it is | CSS / JS |
|------|-----------|----------|
| **DaisyUI** (default) | The reference UI. Server-rendered DaisyUI v5 components over TailwindCSS, morph-friendly with Datastar | `app.min.css` |
| **Basecoat** | [BasecoatUI](https://basecoatui.com) (a shadcn-style component lib) with shadcn-inspired OKLCH `@theme inline` color tokens, native Basecoat JS runtime (`basecoat.initAll`) debounced via `requestAnimationFrame` for Datastar DOM morphing | `basecoat.min.css` + `basecoat.min.js` |
| **[Morpheus](https://github.com/romshark/morpheus)** | Vendorized web-components bundle (SHA-pinned, `web/skins/morpheus/VENDOR_SHA`) that gives the todo demo a different visual treatment without DaisyUI | `morpheus/bundle.js` + theme CSS |

> **⚠️ Basecoat and Morpheus are community-supported.** DaisyUI (the default) is
> the polished, battle-tested skin that gets the most development attention.
> Basecoat and Morpheus integrate correctly but may have rough edges in their
> current state — CSS alignment nuances, missing component states (disabled,
> focus, error), and less extensive Datastar morph testing.
>
> **Contributions are welcome.** Every skin is a self-contained directory under
> `web/skins/<name>/` — changes are scoped and safe.

## Three ways to switch

1. **Env var (process-wide).** `UI_SKIN=basecoat ./gogogo`
   switches the active skin for the lifetime of the binary.
2. **Query string (per request).** Append `?skin=morpheus` to any route; the
   skin dispatcher reads it and renders that skin's assets without restart.
3. **Interactive selector.** The navbar exposes a `SkinSelector` that updates a
   query param and reloads.

## The plugin contract

`web/skins/skin.go` defines every skin as a `Skin{Name, Assets}` value,
registered at init time via blank imports in
`features/todo/components/skin_imports.go`. The dispatcher falls back to
DaisyUI when the env value is unknown, logging a warning.

**Adding a fourth skin:** create `web/skins/<name>/`, register it from the
import file, add a `make css-<name>` target. See
`web/skins/daisyui/skin.go` for the minimal reference implementation — assets
only, no Templ templates (those stay in the feature).

## Building the CSS bundles

```
src/css/input.css          →  tailwindcss v4 CLI  →  web/resources/static/app.min.css        (DaisyUI bundle)
src/css/basecoat-input.css →  tailwindcss v4 CLI  →  web/resources/static/basecoat.min.css  (Basecoat bundle)
                                                              │
                              web/skins/morpheus/static/bundle.js                          (Morpheus, vendorized, SHA-pinned)
                                                              │
                                                        //go:embed in the Go binary
```

```bash
make css         # DaisyUI (default)
make css-basecoat # Basecoat
make css-all     # every skin
```

Morpheus ships vendorized and needs no rebuild.

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