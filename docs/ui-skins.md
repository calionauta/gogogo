# UI skins

Every page ships with a runtime-switchable UI skin. Three skins are compiled
into the same binary; the active one is chosen per process via env, per request
via query string, or interactively via the `SkinSelector` widget in the navbar.

| Skin | What it is | CSS / JS |
|------|-----------|----------|
| **DaisyUI** (default) | The reference UI. Server-rendered DaisyUI v5 components over TailwindCSS, morph-friendly with Datastar | `app.min.css` |
| **Basecoat** | [BasecoatUI](https://basecoatui.com) (a shadcn-style component lib) with shadcn-inspired OKLCH `@theme inline` color tokens, native Basecoat JS runtime (`basecoat.initAll`) debounced via `requestAnimationFrame` for Datastar DOM morphing | `basecoat.min.css` + `basecoat.min.js` |
| **shadcn** | shadcn's visual language expressed as tokens plus a compatibility layer over the shared semantic classes (`btn`, `card`, `badge`, `input`, `tabs`, `table`, `modal`, `steps`, …). No vendored component source, no client framework | `shadcn.min.css` |

Every page renders the active skin through `skins.ActiveAssets`, and
`skins.Resolve` applies the `?skin=` override to every route, not only the Todo
reference feature. The navbar selector and the `?skin=` query therefore change
the whole app.

> **⚠️ Basecoat is community-supported.** DaisyUI (the default) is the polished,
> battle-tested skin that gets the most development attention. Basecoat
> integrates correctly but is the secondary skin: it is developed against a
> third-party component library, so its class vocabulary is mapped onto the
> shared templates rather than being the origin of them.
>
> **Contributions are welcome.** Every skin is a self-contained directory under
> `web/skins/<name>/` — changes are scoped and safe.
>
> All bundles are built from the same scan roots (`features/`, `web/`,
> `internal/`), so a utility class that resolves under one skin resolves under
> the others. The per-skin work is confined to class *vocabulary*: Basecoat has
> no `.steps`, no `data-variant="accent"`, and no DaisyUI semantic colours, so
> the skins differ in markup where the component model differs.
>
> An earlier skin (Morpheus, a web-components kit) shipped from v0.24.0 and was
> removed: its upstream was a two-month-old alpha and its layout classes
> (`cal14-grid`, `neo-tab-active`) had no CSS anywhere, so it rendered broken
> rather than merely differently. The changelog entry records the full list of
> what was deleted.

## Why shadcn is a skin, not vendored shadcn-templ

[shadcn-templ](https://shadcn-templ.com) is a copy-in **component library**: using
it means vendoring its templ source and its behaviour shims. A skin is an asset
bundle selected at runtime. The shadcn skin keeps the first meaning and takes
the second shape: it maps the semantic class vocabulary the shared templates
already use onto shadcn tokens and recipes. One template tree keeps rendering
under all three skins, and a feature added once is styled everywhere.

If a future component genuinely needs shadcn-templ's source (a combobox with
its own state machine, for example), vendor that one component beside the
feature and keep the skin for the shell.

## Three ways to switch

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

**Adding a skin:** create `web/skins/<name>/`, register it from the
import file, add a `make css-<name>` target. See
`web/skins/daisyui/skin.go` for the minimal assets-only reference. A skin that
needs its own markup can keep a templ page beside it; Basecoat does.

## Building the CSS bundles

```
src/css/input.css          →  tailwindcss v4 CLI  →  web/resources/static/app.min.css        (DaisyUI bundle)
src/css/basecoat-input.css →  tailwindcss v4 CLI  →  web/resources/static/basecoat.min.css  (Basecoat bundle)
src/css/shadcn-input.css   →  tailwindcss v4 CLI  →  web/resources/static/shadcn.min.css    (shadcn bundle)
                                                              │
                                                        //go:embed in the Go binary
```

```bash
make css         # DaisyUI (default)
make css-basecoat # Basecoat
make css-shadcn  # shadcn
make css-all     # every skin
```

> Editing a `.templ` without `make templ && make css-all` leaves a bundle
> stale — `app.min.css`, `basecoat.min.css` and `shadcn.min.css` are committed
> and embedded. The pre-commit hook rebuilds all of them whenever `.templ`,
> `.go` or `.css` files change, and CI diffs them after `make css-all`, so a
> stale skin fails the gate instead of shipping. `make css` alone is the
> DaisyUI bundle only: it is never sufficient to commit.

## Basecoat runtime gotcha

Do **not** call `basecoat.initAll` from inline page scripts. Datastar re-morphs
the DOM and loses JS-attached event handlers. The template debounces
`basecoat.initAll` via `requestAnimationFrame` instead.

## Removal

Delete `web/skins/`, drop the blank imports in
`features/todo/components/skin_imports.go`, and drop the `SkinSelector` call
from the navbar. The handler's lazy fallback returns DaisyUI assets when no
skin is registered.

## Behaviour gaps: date picker and command palette

DaisyUI is CSS-only, so the three behaviour gaps are filled without importing a
second design system:

- **Date picker.** Cally v0.9.2 (MIT, one small self-contained module) is
  vendored at `/static/cally.js` and loaded only by the Todo layout, which is
  the only page that uses it. The
  Todo page wraps `<calendar-date class="cally">` in `createdFilter`; each row
  carries `data-created` and a `data-show` comparison, so the filter is a
  client-side display decision with no request. DaisyUI styles the web
  component; `app.css` supplies a compact part-based fallback for the other
  skins.
- **Command palette.** `features/authviews/command_palette.templ` renders a
  native `<dialog>` opened by Ctrl/Cmd+K or the navbar button. The input owns a
  filter signal, the options are real links, and ArrowUp/ArrowDown move focus
  through the visible commands. No framework runtime.
- **Combobox pattern.** The palette's input + listbox + keyboard handling is the
  reusable combobox pattern. A form combobox that needs server search can reuse
  it with a `@get` endpoint, the same way the Todo search box does.

Cally is the only new client asset for these gaps. No Basecoat JS, Starbase or
dsx runtime was added; the skin contract and the CSS-only shell stay intact.

## Related

- [UI sounds](ui-sounds.md)
- [Removing skins](scope-taxonomy.md#what-ships-as-what)
