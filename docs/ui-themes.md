# UI themes

Every page renders inside one shell (`internal/ui`), dressed by **one
component library** — DaisyUI v5 over Tailwind v4 — plus a **theme** chosen
at runtime.

| Layer | What it is | Where |
|-------|-----------|-------|
| **Components** | The DaisyUI class vocabulary + the compiled asset bundle | `src/css/input.css` → `web/resources/static/app.min.css`, rendered once by `internal/components.Assets()` |
| **Theme** | A DaisyUI palette: colours, radius, shadows | `src/css/input.css` (`@plugin "daisyui"`), catalogued in `internal/themes` |

One library ships: **DaisyUI** (`app.min.css`). The palettes are `light`
(default), `dark`, `lofi` ("Calm") and `dim` ("Zinc"), picked from the **Tema**
list in the account panel (the identity row at the foot of the sidebar). Theme
and sound are preferences, so they live in the account surface, not as icon
toggles competing with navigation — the placement PostHog moved to (#19001) and
the one Linear, Notion and Vercel keep. Visual variety comes from palettes, never
from a second component library or a second CSS bundle: one vocabulary,
one bundle, zero forked markup.

## Themes

```
src/css/input.css  →  tailwindcss v4 CLI  →  web/resources/static/app.min.css
```

```bash
make css         # build the bundle
make css-check   # fail if the committed bundle is stale (pre-commit + CI)
make css-all     # alias for css
```

Adding a theme is one edit plus one list:

1. Add the name to `@plugin "daisyui" { themes: light --default, dark, lofi, dim, <name>; }`
   in `src/css/input.css` and rebuild the bundle.
2. Add it to `internal/themes` with its label and whether it is dark.

`themes_test.go` fails if the catalog and the compiled list disagree in either
direction: a catalog entry the bundle does not compile renders as the default
palette (a picker option that changes nothing), and a compiled theme missing
from the catalog is unreachable from the UI.

The picker (`internal/ui/preferences.templ`) is markup only: it sets the
`$theme` signal, and Datastar owns `data-theme` and the `dark` class through the
bindings in `authviews.ThemeRootAttrs`. Nothing writes those attributes
imperatively — Datastar reverts external writes to a bound attribute. The same
helper emits `data-theme-dark`, the one list the pre-paint bootstrap
(`theme_head.templ`) and `web/resources/static/theme.js` read, so neither keeps a
second copy of "which palettes are dark".

The theme is not a parameter: it lives in the `$theme` signal, persisted to
`localStorage` under `themeMode` and applied before first paint.

## The asset block

`internal/components.Assets()` is the one stylesheet + runtime script block
every page renders (shell, login, landing). One spelling for all pages, so a
URL change touches one file. Every URL is versioned through
`resources.StaticURL` so the browser and any CDN in front of the app can
cache the file immutably instead of revalidating it on every navigation
(see `resources.AssetHandler`).

## Related

- Agents: read `skills/gogogo-ui/SKILL.md` (template UI rules over the
  vendored `skills/daisyui/SKILL.md` vocabulary) before editing markup.
- [UI sounds](ui-sounds.md)
- [Local CI](local-ci.md) — `bin/red-proof-ui.sh` proves the guards that keep a
  removed surface out.
