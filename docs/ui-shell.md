# UI shell and kit

Every module page renders a **body** inside one shell. This is the page to read
before adding a screen: it says what the shell owns, what a page owns, and which
class to use so a new screen looks like the other eight.

## Anatomy

```
internal/ui.Shell(d Data, head, sound, actions, body)
├── <html>   data-signals (page seed), data-theme bindings, data-theme-dark
├── <aside id="app-rail">   brand · build badge · []Module groups · footer
│      footer: theme picker · mute · stack key · account menu
├── <header class="app-module-header">
│      drawer trigger (≤1024px) · title · context slot · [ Demo ] · ⌘K · actions
├── <main class="app-body">  ← the page
└── <aside id="state-panel"> ← only in demo mode, only when the page passes one
```

`Data` carries: `Title`, `Module` (id used for the active nav item), `Heading`,
`Context` (one line of state), `Email`, `Role`, `BuildLabel`, `BuildCommit`,
`Skin`, `Signals` (the page's own seed JSON), `Stack` (`[]TechItem`), `Demo`,
`Panel`, `OfflineSync`, `StreamPage`, `FullBleed`.

**`Panel` is nil for a reason.** A page passes a panel only when it has a
pipeline that runs *beside* the page — a queue, a model call, phases. `/config`,
`/room`, `/notes` and `/whiteboard` do not: their demo **is** the page (a table,
a lock, a canvas), so hiding it behind a toggle would hide the point. A page with
no panel gets no `[ Demo ]` button.

## What a page owns

A page contributes: its `<head>` extras (as a one-line `*Head()` wrapper that the
trim rules can empty), its mute toggle (a one-line `*Sound()` wrapper, same
reason), optional header `actions`, its body, and — when it has one — its panel.

The page never renders `<html>`, `<main>`, a banner stack or a toast root.

## The kit

All classes read the theme's semantic tokens (`--color-base-*`,
`--color-primary`, `--color-error`…), so all four palettes and both modes come
for free. Reach for these before writing new markup:

| Class | Use |
|---|---|
| `.ui-page` / `.ui-page-narrow` | the page column, centred. Every module body starts with one. |
| `.ui-stack` | vertical rhythm (1rem). Rows of unrelated controls use it instead of ad-hoc margins. |
| `.ui-card` + `.ui-card-head` / `.ui-card-body` | a bordered surface with a title row. |
| `.ui-btn` + `.ui-btn-primary` / `.ui-btn-ghost` / `.ui-btn-error` / `.ui-btn-sm` | the only button shapes. Height 2.25rem, one focus ring. |
| `.ui-field` + `.ui-field-group` | inputs, selects, textareas; a field glued to its action is one control. |
| `.ui-badge` + `-primary` / `-success` / `-error` | inline state (counts, "set", verdicts). |
| `.ui-empty` / `.ui-empty-title` / `.ui-empty-body` | centred empty state. |
| `.ui-table` | data tables (config). Header cells are uppercase micro-labels. |
| `.ui-callout` (+ `-warning` / `-error`) | a note in the flow: read-only banners, disabled states. |
| `.ui-meta` | monospace micro-text for machine facts (store, generation, attempts). |
| `.ui-title` | a card or section title. |
| `.ui-kbd` | keyboard hints. |

**Rhythm rules.** One `.ui-page` per body; one card per concern; one filled
control per screen (the primary action); machine data in `.ui-meta`, prose in
body text — never the reverse. The module header's context slot is for *state*
(`turso · outbox on · 0 online · SSE`), never for a sentence.

## Mobile

Below 1024px the rail is an offcanvas drawer: opened from the header trigger,
closed by the backdrop, Escape, or tapping a destination. Datastar owns
`$railOpen`; there is no script for it. The state panel becomes a sheet.

## Adding a module

1. One entry in `internal/ui/modules.go` (`ID`, `Title`, `Group`, `Icon`, `URL`,
   `Admin`). The sidebar, the header title and the palette all read it.
2. A body inside `ui.Shell` (`// SCOPE:` per the taxonomy).
3. If it has a pipeline, a `*StatePanel` and `Panel:` in the shell data.

Trim units drop the `[]Module` line, so a trimmed feature cannot leave a dead
link.

## Related

- [UI skins and themes](ui-skins.md)
- [UI sounds](ui-sounds.md)
- [Local CI](local-ci.md) — `bin/red-proof-ui.sh` proves the guards around this.
