# UI components comparison: DaisyUI, Starbase, datastar-ui, dsx (2026-10-21)

Reviewed: **2026-10-21**. Scope: choose the component base for a Go/templ app
that renders server-side, streams with Datastar over SSE, and wants to stay
light and maintainable if an upstream stops.

The short version: **keep DaisyUI as the base**, add Cally for dates, and take
combobox/command behaviour from a small, Datastar-native source instead of
adopting a whole design system. The four candidates were judged on Datastar/SSE
fit, lightness, performance, functionality, maturity, and forkability.

## Snapshots at review time

| project | stack | maturity | JS footprint | licensing |
|---|---|---|---|---|
| **DaisyUI** | CSS-only Tailwind plugin, semantic classes | 42.6k stars, years of use | none | MIT |
| **Starbase** | Rocket web components + Datastar; Go/templ gallery, CQRS/SSE | created 2026-09-21, 33 stars, one month old, company-backed; Rocket is beta | Rocket runtime + per-component JS (the date picker is ~55 KB) | MIT, but seven PD-rockets components are Beer-Ware |
| **datastar-ui** | templ + Datastar + Tailwind/shadcn; no custom JS | created 2025-05-21, 157 stars, active | Datastar only | MIT |
| **dsx** | Go/templ + DaisyUI + Datastar; no Alpine or HTMX | created 2026-03-10, no public adoption yet | Datastar only | MIT |

## Scores (1 = weak, 5 = strong)

| criterion | DaisyUI | Starbase | datastar-ui | dsx |
|---|---|---|---|---|
| Datastar fit | 5 | 4 | 5 | 5 |
| SSE/morph friendliness | 5 | 3 | 5 | 5 |
| lightness | 5 | 2 | 5 | 5 |
| render/runtime performance | 5 | 3 | 4 | 4 |
| functionality out of the box | 2 | 4 | 3 | 5 |
| maintainable if upstream stops | 5 | 2 | 4 | 4 |
| maturity / adoption | 5 | 2 | 3 | 1 |

DaisyUI wins the weighted result because the top priorities are a hypermedia
app: stateless CSS, no client runtime, and a codebase that keeps working even
if the upstream disappears. Its functionality gap is real, but it is local to
three widgets and can be closed without replacing the base.

## Why each alternative does not become the base

**Starbase** is the most capable for complex widgets (date picker, kanban,
virtual scroll, drag surfaces), but it is one month old, relies on beta Rocket,
styles with shadow DOM and its own `--sb-*` tokens, and has no combobox or
command palette. DaisyUI classes do not cross the shadow boundary. It is a good
source for an isolated widget, not a foundation.

**datastar-ui** is the strongest fallback if the product ever drops pluggable
skins: pure Datastar, Go/templ source, no extra runtime, MIT, and a year of
activity. It is shadcn-flavoured rather than DaisyUI, and it has no combobox or
command palette.

**dsx** is the closest functional fit: DaisyUI + Datastar + Go/templ with
combobox, command bar and calendar, and zero Alpine or HTMX. It has no public
adoption yet, so it is a source to copy and own, not a dependency to trust.

## Gap-closing plan while keeping DaisyUI

1. **Date picker:** vendor **Cally** (MIT, under 9 KB, accessible, and officially
   styled by DaisyUI 5) and wrap it in a small templ component. It is the one
   widget that is genuinely "do not rebuild".
2. **Combobox / command palette:** implement the pattern once with Datastar
   (input + listbox + option roles + keyboard and focus management) or copy the
   relevant DaisyUI-compatible source from dsx. A command palette is the same
   pattern with a fixed command list, so one implementation covers both.
3. **Menu behaviour:** DaisyUI already provides positioning and open/close;
   add the keyboard/ARIA layer (roving focus, Home/End, Escape, focus return)
   as a small helper, not as a framework.

This keeps the shell CSS-only. JavaScript is added only where a widget needs
live state, and no second design system enters the tree.

## Forkability if an upstream stops

- **DaisyUI:** best case. The compiled CSS is already in the repo; it keeps
  rendering untouched. Forking means editing CSS.
- **datastar-ui / dsx:** good. The components are Go/templ plus Datastar
  attributes; copy them and keep going. dsx also carries its own helpers.
- **Starbase:** worst case. You inherit dozens of JS/TS web components, a beta
  Rocket runtime, and a patched Datastar build.

## Revisit triggers

- The product stops promising pluggable skins and needs many complex widgets:
  re-evaluate **datastar-ui** as the single system.
- A rich canvas, kanban or virtual-scroll surface is needed and a different
  visual language is acceptable: use **Starbase** as an island.
- **dsx** gains real adoption (releases, contributors, users): reconsider it as
  the DaisyUI-native source for combobox, command and calendar.

## Implemented on 2026-10-21

- **Cally v0.9.2** is vendored at `/static/cally.js`; `createdFilter` in the
  Todo list wraps `<calendar-date class="cally">` and filters rows by creation
  date (`data-created` + `data-show`, no request).
- **Command palette** (`features/authviews/command_palette.templ`) is a native
  `<dialog>` with a Datastar-filtered listbox, Ctrl/Cmd+K, and ArrowUp/ArrowDown
  focus movement. It is the combobox pattern with a fixed command list.
- **Menu behaviour:** the palette supplies the keyboard/ARIA path; the navbar
  keeps native `<details>` for grouped links and the user menu.

No Basecoat, Starbase or dsx runtime was added. Cally is the only new client
asset.

## Decisions after the implementation review

- **No generic form combobox yet.** The command palette implements the
  input + listbox + keyboard pattern; a form combobox would have no consumer in
  the current product. Build it when a form actually needs searchable options,
  reusing the palette with a `@get` search endpoint.
- **The date filter is client-side on purpose.** The Todo list renders every
  visible row, so `data-created` + `data-show` filters instantly with no
  request. If the list is paginated or server-filtered, the date belongs in the
  query string and the handler must filter server-side. Both patterns exist in
  the ecosystem (dsx drives calendar month navigation through a handler;
  datastar-ui keeps the picker value in client signals).
- **The navbar keeps native `<details>`.** It is accessible, needs no runtime,
  and the command palette covers the keyboard-first path. A custom menu
  component would add JavaScript without removing a real failure mode.

### Popover dismissal and palette ranking

- The date popover uses the native Popover API (`popovertarget` + `popover`),
  so light dismiss, Escape and top-layer stacking come from the browser.
  `data-on:click__outside` (the pattern datastar-ui uses) was evaluated, but
  the native popover is less code and works across all three skins.
- Command-palette matching is substring on the visible label and hint,
  matching Basecoat's command implementation. Fuzzy ranking needs a small
  scorer in `command-palette.js` and is worth adding only when the command
  list grows; with a handful of static destinations, substring is predictable
  and cheaper.
