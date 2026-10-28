# UI compositions

The DaisyUI skill (vendored at `.agents/skills/daisyui/`, upstream
[daisyui.com/SKILL.md](https://daisyui.com/SKILL.md)) is a **dictionary**:
it lists classes, modifiers and the syntax of each component. It cannot tell you
where the account menu goes, that a preference belongs in the account panel, or
that a boolean setting wants a switch. Those are **compositions** — the grammar
on top of the vocabulary — and they are what makes a page look designed instead
of assembled.

Copy these. Inventing a shape is the defect: every one below replaced a
hand-rolled version that looked right in a diff and wrong on screen.

**The machine half is `internal/ui/registry.json`.** An agent asks for the shape
instead of reading this page top to bottom:

```bash
go run ./cmd/ui-registry list        # id, title, canonical source
go run ./cmd/ui-registry show field-action
```

Each entry carries `when` (use it), `avoid` (the anti-pattern it replaced), the
source file that still holds the markup, the classes it promises, and the anchor
of the section below. `make check-classes` validates all of it — the source
exists, every promised class is still there, the anchor resolves, and every
numbered section below has an entry — so the registry cannot drift into a
plausible-looking lie. The class vocabulary itself ships as a standalone binary
too (this repo runs it as a pinned `go tool daisyui-lint`, version in `go.mod`;
see [the shell page](ui-shell.md#the-same-rules-travel-daisyui-lint)), so a
project outside gogogo can enforce the same rules.

## 1. Account row (the rail's footer)

The whole row is the trigger, and the panel opens **upward**. A menu anchored to
the bottom edge that opens downward lands off-screen — that was the original bug
(`docs/ui-compositions.md` only exists because someone shipped it).

Markup: `features/authviews/controls.templ` (`UserMenu`). Shape:
`<details class="app-dropdown app-dropdown-up">` → `<summary class="app-dropdown-trigger">`
with `avatar avatar-placeholder` + identity block (`badge` for the role) +
CSS caret → `<ul class="app-dropdown-menu">`.

The panel holds the account's links, in reading order: **Account**, then
**Config** for admins (the operator surface lives here, not in the rail), the
palette list, and sign out. The caret flips direction while the panel is open
(`.app-dropdown[open] .app-account-caret`), with the same transition DaisyUI's
own dropdown uses.

**Do not** put icon toggles beside it (theme, sound, a disclosure). Preferences
go **inside** the panel — see §2 — because that is the account surface, and
because a rail footer with five small controls has no visual anchor.

## 2. Preference row (inside the account panel)

A preference is one row: the option list for a choice, and — for a boolean —
`toggle` on a real `<input>`, always inside a `<label>` so the whole row is the
hit area. Palettes get a real swatch (`[data-theme]` scopes DaisyUI variables,
so a swatch declares its own theme) and a check on the active one.

The sound mute is the one boolean that does not live here: it is the speaker
icon in the rail brand, because a guest page has no panel to open and the
landing ships sounds too. One control, one owner — do not add a second switch
to this panel.

Markup: `internal/ui/preferences.templ`. The option carries
`data-class="{'is-active': …}"` and `aria-pressed`; the click sets the
`$theme` signal — never the DOM, Datastar reverts external writes.

**Do not** offer two ways to pick a theme (a two-state sun/moon toggle *and* a
palette list): four palettes, two of them dark, means the toggle silently
discards the choice.

## 3. Empty state

`.ui-empty` with a title and one line of copy. Padding, centring and the icon
centring come from the class, not from utilities sprinkled per page.

```html
<div class="ui-empty" id="todo-empty-state">
  @icon.Icon("material-symbols:checklist", "h-12 w-12 opacity-30")  <!-- catalogue: internal/icon -->
  <p class="ui-empty-title">No todos here yet.</p>
  <p class="ui-empty-body">Add one above to get started.</p>
</div>
```

**Do not** re-invent an empty state with ad-hoc margins: five pages used
`.ui-empty` while the class had no rule at all, so the icon was centred by a
utility and the copy stayed flush left.

## 4. Field plus its action

An input and the button that submits it are **one control**: DaisyUI `join`,
with paired sizes (`input-sm` next to `btn-sm`) and `flex-1` on the field.

```html
<div class="join w-full">
  <input class="input input-sm join-item flex-1" name="q" />
  <button class="btn btn-primary btn-sm join-item" type="submit">Add</button>
</div>
```

**Do not** use a bare wrapper `<div class="ui-field-group">`: it was used in
four pages with no rule behind it, and the two controls rendered at different
heights.

## 5. Stack key (the demo panel's evidence)

"What this page demonstrates" is one component, rendered by the shell from
`Data.Stack`, behind the `[ Stack ]` button — never in the chrome, and never
per page. The `[ State ]` button opens the page's machinery instead; the two
sections never show at once, and the panel's close control sits pinned at the
top-right in both.

Markup: `internal/components/techstack.templ` — a `STACK` caption plus one row
per layer: `dd`/`dt` pair, a dot, the name, and what the layer does **here**
(not what the layer is in the abstract).

**Do not** add a counter to the label (`stack 7` said nothing about which
seven), and do not put the list back in the sidebar: seven static rows cost
~300px of footer and pushed the account menu off-screen.

## 6. State, inline

Counts, verdicts and statuses are DaisyUI `badge` with a semantic colour
(`badge-primary`, `badge-success`, `badge-error`). A role or a mode reads as a
badge too — `badge badge-sm`.

A **grouped status card** (the `/config` runtime view) lists several states
under captions, one aligned row per item: label on the left, DaisyUI `status`
dot plus the state WORD on the right. The dot is decoration — the word carries
the meaning, so colour is never the only signal. Put a summary badge in the
card head (`1 needs attention` / `nothing to fix`) and keep the per-item hint in
`title`.

**Do not** put `badge` on an element and then strip `padding`, `border` and
`background` in a scoped rule: that is a custom component wearing the library's
class name, and the next reader believes it is a badge. **Do not** print eight
identical `Label: state` badges in a `flex-wrap` row: posture and capability
become indistinguishable, and the strip reads as a tag cloud (the reported
`/config` status).

## 7. Transient feedback (toast)

An action's result — "saved", "scheduled", "deleted", "cleared" — is a toast
appended to the shell's `#toast-container`, never an inline notice that stays on
the page. The shell renders the container once (`components.ToastContainer`),
the handler appends one toast, and the component removes itself (3s; 8s for
`error`).

```go
dshelpers.RenderAndPatch(sse,
	ic.Toast("Reminder scheduled.", "success", ic.NewToastID()),
	sdk.WithSelectorID("toast-container"), sdk.WithModeAppend())
```

Markup: `internal/components/toast.templ`; the live example is
`features/notifications/handlers/remind.go`.

**Do not** render a success message as an `alert alert-success` inside the
page. It stays until the next navigation, pushes the list down, and competes
with the content that the action actually changed. Inline `alert` is for
persistent state the user must act on: a warning, an error to fix, a feature
that is switched off.

---

## Why these and not others

Checked in a browser at 1280, 820 and 500px, light and dark, and guarded by
`make check-classes` (markup ↔ stylesheet ↔ doc) plus the account-menu
assertion in `scripts/smoke.mjs` (the panel must open upward, inside the
viewport, with a surface that differs from the rail). A composition that cannot
be checked that way is a suggestion, not a rule.
