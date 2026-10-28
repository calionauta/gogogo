# UI shell and kit

Every module page renders a **body** inside one shell. This is the page to read
before adding a screen: it says what the shell owns, what a page owns, and which
class to use so a new screen looks like the other eight.

## Anatomy

```
internal/ui.Shell(d Data, head, sound, actions, body)
├── <html>   data-signals (page seed), data-theme bindings, data-theme-dark
├── <aside id="app-rail">   brand · build badge · theme toggle · sound mute · pinned Inbox (indicator count) · []Module groups · account row
│      account row → panel: identity · Account · Config (admin) · Tema list · sign out
├── <header class="app-module-header">
│      drawer trigger (≤1024px) · title · context slot · [ State ] [ Stack ] · search input · actions
├── <main class="app-body">  ← the page
└── <aside id="state-panel"> ← when the page passes one; visibility is $panelVisible, section is $panelMode (signals, no reload)
```

`Data` carries: `Title`, `Module` (id used for the active nav item), `Heading`,
`Context` (one line of state), `Email`, `Role`, `BuildLabel`, `BuildCommit`,
`Signals` (the page's own seed JSON), `Stack` (`[]TechItem`), `Demo`,
`Panel`, `OfflineSync`, `StreamPage`, `FullBleed`.

**The rail is ordered, not alphabetised.** The inbox is pinned above the first
heading: it is the always-on surface, and it carries the unread count as a
DaisyUI `indicator` bubble on its icon. The count is seeded once per page
navigation by `inboxBadgeMiddleware` (`internal/server/inbox_badge.go`), kept
live on pages without a feature stream by the leader session stream
(`session.js`), and refreshed on visibility/focus/online on pages that already hold a stream —
a second SSE connection would exhaust the browser's per-host budget at five
same-user tabs. Every tab that hears a new count broadcasts it over the
BroadcastChannel; the badge binds the value to `$inboxUnread` and always
exists, hidden at zero. Config is `MenuOnly`: an operator surface, reached
from the account menu right under Account, never a daily rail destination.

**`Panel` is nil for a reason.** A page passes a panel only when it has a
pipeline that runs *beside* the page — a queue, a model call, phases. `/config`,
`/room`, `/notes` and `/whiteboard` do not: their demo **is** the page (a table,
a lock, a canvas), so a machinery column would add nothing.

The header carries up to two panel buttons, and each opens only its own
section: `[ State ]` (`Data.Panel` — the running machinery) and `[ Stack ]`
(`Data.Stack` — the technology key). A page with both gets both buttons; a
page whose demo *is* the page still opens its key. The close control is pinned
to the panel's top-right in every variation.

**Demo mode never reloads.** The buttons and the panel close control flip the
`$panelVisible`/`$panelMode` signals and persist the on/off choice with a
background `fetch` to `/_ui/demo` (session cookie). The cookie seeds the next
server render, so the panel survives a reload — but opening, switching or
closing a section does not navigate, morph, or flash.

## What a page owns

A page contributes: its `<head>` extras (as a one-line `*Head()` wrapper that the
trim rules can empty), its mute control (a one-line `*Sound()` wrapper, same
reason — the shell renders it in the rail brand), optional header `actions`, its
body, and — when it has one — its panel.

Every shell page that loads the sound runtime must render the mute switch;
`internal/server/pages_contract_test.go` pins `app-sound-toggle` on each one, so
a page added without it fails the contract instead of silently losing the rail's
only global control. The room demo is the deliberate exception: its capability
ships no sound runtime, so it passes `nil` and renders no speaker.

The page never renders `<html>`, `<main>`, a banner stack or a toast root.

## The kit

All classes read the theme's semantic tokens (`--color-base-*`,
`--color-primary`, `--color-error`…), so all four palettes and both modes come
for free. Reach for these before writing new markup:

The kit is **layout only**. Controls come from DaisyUI (`btn`, `input`,
`select`, `checkbox`, `badge`, `table`, `alert`, `join`, `toggle`, `avatar`);
do not invent a `.ui-` twin for one. A custom class wearing a component's name
(a `.ui-btn` re-implementing `btn`) is how two vocabularies drift apart, and the
first one to drift is the one nobody checks.

| Class | Use |
|---|---|
| `.ui-page` / `.ui-page-narrow` | the page column, centred. Every module body starts with one. |
| `.ui-stack` | vertical rhythm (1rem). Rows of unrelated controls use it instead of ad-hoc margins. |
| `.ui-card` + `.ui-card-head` / `.ui-card-body` | a bordered surface with a title row. |
| `.ui-empty` / `.ui-empty-title` / `.ui-empty-body` | centred empty state (icon, title, one line of copy). |
| `.ui-row` | one horizontal row of controls, when a wrapper is genuinely needed. |
| `.ui-meta` | monospace micro-text for machine facts (store, generation, attempts). |
| `.ui-title` | a card or section title. |
| `.ui-kbd` | keyboard hints. |

`cmd/check-classes` (`make check-classes`) makes this list executable: every
`app-*` / `ui-*` / `is-*` class in a `class=` attribute must be defined in
`chrome.css`, every selector there must be used somewhere, every class named in
this page must exist, and every class retired by the DaisyUI 5 migration
(`label-text`, `form-control`, `tab-sm`, …) is reported with its replacement.

DaisyUI's own vocabulary is **derived from the vendored skill**
(`.agents/skills/daisyui/SKILL.md`), not transcribed here: the gate reads the
skill's class lists and syntax samples, so a version bump widens the checks
automatically — and a bump that reshapes a component section fails loudly
instead of silently narrowing them. That is what makes the auto-bump PR a real
review gate.

The approved *compositions* — the shapes a page should copy rather than invent —
live in [UI compositions](ui-compositions.md), with `internal/ui/registry.json`
as the machine half (`go run ./cmd/ui-registry list|show <id>`).

### The same rules travel: `daisyui-lint`

The DaisyUI half of that gate is a published linter
([`calionauta/daisyui-lint`](https://github.com/calionauta/daisyui-lint)), so a
project that is not gogogo can run the same checks. This repo consumes it as a
pinned tool (`go tool daisyui-lint`, version in `go.mod`):

```bash
go tool daisyui-lint --version                # tool version + the daisyUI release it validates
go tool daisyui-lint --strict web/            # fail CI on warnings too
go tool daisyui-lint --css "src/*.css" web/    # classes your own CSS defines are not DaisyUI's
go tool daisyui-lint --format json web/ | jq '.[]'  # machine-readable, stable codes
```

Outside this repo, pin the install (`go install github.com/calionauta/daisyui-lint/cmd/daisyui-lint@v0.2.0`)
— `@latest` is a moving target for a gate, exactly the failure `go.mod`'s `tool`
directive removes here.

It reports stable codes — `UNKNOWN_CLASS`, `PART_WITHOUT_PARENT`, `HOST_MISMATCH`,
`RETIRED_CLASS`, `DUPLICATE_MODIFIER`, `ARBITRARY_VALUE` — and every message is
the fix (the nearest declared class, the v5 replacement, the scale value).
Severity is the policy: `ARBITRARY_VALUE` warns by default, fails under
`--strict`, and a project accepts a specific token in `.daisyui-allow` (one per
line, `#` comments) instead of silencing a rule. Exit codes: `0` clean, `1`
findings at error severity, `2` usage or setup. The vocabulary is parsed from the
skill, so it tracks daisyUI instead of drifting.

### What a consumer of this template inherits — and may change

- **`make check-classes` degrades instead of breaking.** If the vendored skill,
  the registry or `chrome.css` is absent (you trimmed a unit, or swapped the UI
  library), the direction that needs it prints one line and the rest keeps
  running. The maintainer's own run passes `--strict`, which turns every absence
  into a failure — that is how *this* repo cannot silently lose its gate. Keep
  `--strict` only if you keep all three inputs.
- **The smoke assertions are the template's contract.** `scripts/smoke.mjs`
  asserts the account panel's geometry, the demo panel, no horizontal overflow at
  three widths, and accessible names on `/login` and `/todo`. When you redesign
  the shell, update these together with it — they are the regression net for the
  defects this template has actually shipped.
- **Accessibility: structural + axe.** The dependency-free checks (names, labels,
  alt, overflow, panel geometry) always run; `@axe-core/playwright` is a
  devDependency and CI runs the browser suite with `SMOKE_A11Y=1`, so the
  WCAG-grade pass (serious/critical, wcag2a) is a gate here. Its first run found
  13 contrast nodes across `/login`, `/todo` and `/account`; the fixes raised the
  muted-text floor to 70% and gave the presence pill and inactive tabs an
  AA-compliant colour. Keep the pass on when you restyle: contrast is the one
  accessibility failure a class gate cannot see.
- **Off-scale values are a policy, not a bug.** `ARBITRARY_VALUE` warns so a
  deliberate `text-[10px]` is a conversation; accept it in `.daisyui-allow` or
  move it onto the scale.

**Rhythm rules.** One `.ui-page` per body; one card per concern; one filled
control per screen (the primary action); machine data in `.ui-meta`, prose in
body text — never the reverse. The module header's context slot is for *state*
(`turso · outbox on · 0 online · SSE`), never for a sentence.

## Mobile

Below 1024px the rail is an offcanvas drawer: opened from the header trigger,
closed by the backdrop, Escape, or tapping a destination. Datastar owns
`$railOpen`; there is no script for it. The demo panel becomes a sheet.

The stacked shell is a **column flex**, not a block: `.app-main` has to fill the
viewport, or the work area has no definite height. That height is what a
`FullBleed` page spends — the whiteboard's canvas chain (`app-body` → `flex-1` →
`#canvas-wrap`) resolves to zero without it, so the board renders but cannot be
drawn on. `scripts/whiteboard-client.test.mjs` pins the canvas height at 900px
for exactly that reason.

## Adding a module

1. One entry in `internal/ui/modules.go` (`ID`, `Title`, `Group`, `Icon`, `URL`,
   `Admin`). The sidebar, the header title and the palette all read it. A
   `Pinned` module renders above the group headings (no heading of its own);
   a `MenuOnly` module is reached from the account menu and never renders in
   the rail.
2. A body inside `ui.Shell` (`// SCOPE:` per the taxonomy).
3. If it has a pipeline, a `*StatePanel` and `Panel:` in the shell data.
4. If it needs its own head extras or mute control, the one-line `*Head()` /
   `*Sound()` wrappers (see "What a page owns") — never inline the call, or a
   feature trim cannot drop just that line.

Trim units drop the `[]Module` line, so a trimmed feature cannot leave a dead
link.

## Related

- [UI themes](ui-themes.md)
- [UI sounds](ui-sounds.md)
- [Local CI](local-ci.md) — `bin/red-proof-ui.sh` proves the guards around this.
