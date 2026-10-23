# UI skins and themes

Every page renders inside one shell (`internal/ui`), and the shell is dressed by
**one skin** plus a **theme** chosen at runtime.

| Layer | What it is | Where |
|-------|-----------|-------|
| **Skin** | The component class vocabulary and its asset bundle | `web/skins/<name>/` (assets only, no page templates) |
| **Theme** | A DaisyUI palette: colours, radius, shadows | `src/css/input.css` (`@plugin "daisyui"`), catalogued in `internal/themes` |

One skin ships: **DaisyUI** (`app.min.css`). The palettes are `light`
(default), `dark`, `lofi` ("Calm") and `dim` ("Zinc"), picked from the **Theme**
disclosure in the sidebar footer.

> **Why one skin.** A second component library is not a second palette: its
> component model differs by *markup* (different tab, step and colour
> vocabulary), so every page either forks or degrades. Compiling one in cost a
> second CSS bundle in every binary, a second CSS build on every commit, and a
> forked `/todo`. Themes buy the palette/radius/typography variety for a few KB
> of CSS and zero forked markup; a second vocabulary is a docs page plus a
> `web/skins/<name>/` directory, added only when a real consumer needs it.

## Themes

```
src/css/input.css  →  tailwindcss v4 CLI  →  web/resources/static/app.min.css
```

```bash
make css         # build the bundle
make css-check   # fail if the committed bundle is stale (pre-commit + CI)
make css-all     # alias for css (kept for callers that still use the name)
```

Adding a theme is one edit plus one list:

1. Add the name to `@plugin "daisyui" { themes: light --default, dark, lofi, dim, <name>; }`
   in `src/css/input.css` and rebuild the bundle.
2. Add it to `internal/themes` with its label and whether it is dark.

`themes_test.go` fails if the catalog and the compiled list disagree in either
direction: a catalog entry the bundle does not compile renders as the default
palette (a picker option that changes nothing), and a compiled theme missing
from the catalog is unreachable from the UI.

The picker (`internal/ui/theme_selector.templ`) is markup only: it sets the
`$theme` signal, and Datastar owns `data-theme` and the `dark` class through the
bindings in `authviews.ThemeRootAttrs`. Nothing writes those attributes
imperatively — Datastar reverts external writes to a bound attribute. The same
helper emits `data-theme-dark`, the one list the pre-paint bootstrap
(`theme_head.templ`) and `web/resources/static/theme.js` read, so neither keeps a
second copy of "which palettes are dark".

## Two ways to switch

1. **Env var (process-wide).** `UI_SKIN=daisyui ./gogogo` sets the default skin
   for the lifetime of the binary.
2. **Query string (per request).** `?skin=daisyui` on any route. Unknown values
   fall back to DaisyUI with a warning log, so an old link never 404s the page.

The theme is not a parameter: it lives in the `$theme` signal, persisted to
`localStorage` under `themeMode` and applied before first paint.

## The plugin contract

`web/skins/skin.go` defines a skin as `Skin{Name, Assets}`, registered at init
time by a blank import in `features/todo/components/skin_imports.go`. The
registry is what makes "add a skin" a docs page instead of a refactor: create
`web/skins/<name>/skin.go`, register it from that import file, add a CSS input
and a `make css-<name>` target. `skins.DefaultAssets` is the DaisyUI block used
when no skin is registered (the normal state in a single-package component
test).

**A skin may not fork a page.** It supplies assets, and at most class vocabulary
consumed through component props. That rule is what the deleted second skin
broke, and why it cost 700+ lines of duplicated markup.

### An assets-only skin owes the shared markup a class for every name it uses

If a skin reuses the shared templates and swaps only the stylesheet, DaisyUI's
component classes are the only thing styling the page — but the skin's bundle is
built from its own input file and inherits none of them. A missing definition is
silent: the markup renders, no console error fires, no Go test fails, and the
control simply collapses.

`make css-check` cannot catch it (it proves the committed bundle matches its
source, not that the source covers the markup), so a skin that follows this shape
needs a class-parity sweep: render the shared templates, collect every class, and
fail on the ones the skin's bundle does not define. The second skin this repo
shipped carried exactly that test, which is how its missing `.steps` and `.toast`
definitions were found.

## Removal

Delete `web/skins/`, drop the blank imports in
`features/todo/components/skin_imports.go`, and drop the `@skins.*` calls from
`internal/ui/shell.templ`. `internal/ui` is the only consumer, so the seam is one
import away from gone.

## Related

- Agents: read `skills/gogogo-ui/SKILL.md` (template UI rules over the
  vendored `skills/daisyui/SKILL.md` vocabulary) before editing markup —
  the skill points back here as the normative seam.
- [UI sounds](ui-sounds.md)
- [Local CI](local-ci.md) — `bin/red-proof-ui.sh` proves the guards that keep a
  removed surface out.
