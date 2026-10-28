# Native panels: Chat answers with the app's own data

A **native panel** is a Chat answer component whose content the app owns. The
model picks the component name and a slice; the server fills the rows from the
store and renders the feature's own UI. `todo_list` is the reference.

## The rule: render the feature's components, never a copy

A panel's `Render` returns the SAME row, card and dialog the feature's page
renders. Only three things are surface-specific:

- **Where the writes post** — the panel carries its instance id as `_target`
  so the feature's own routes answer in place.
- **Which slice is shown** — the model's prop (e.g. `filter`) is data, not
  markup.
- **Instance ids** — server-minted and scoped per answer, so a thread can hold
  several panels.

A chat-specific copy is a drift bug. It still looks plausible, so tests pass
and review misses it: the first todo panel lost the shared badge, the author,
the age, the viewer delete gate and the native confirmation dialog while
`/todo` kept them. The fix was to make the panel call the page's `TodoItem` and
`deleteConfirmModal` and pass only the two action expressions — see
`features/todo/components/panel.templ`.

The same rule covers actions: reuse the page's wrapper (`todoMutateOn`) and the
feature's routes. If the panel needs a parameter the page does not have, extract
the shared component; do not fork it.

## The provider, in one screen

A feature contributes one `PanelProvider` (`features/genui/components/panel.go`):

- **`Component`** — model-facing name, stable, snake_case (`todo_list`).
- **`Prompt`** — when to pick it and the closed enum it may choose.
- **`Writable`** — the props the MODEL may set; rows are server-owned.
- **`Fill`** — owner-scoped rows from the store; map the page's model.
- **`Render`** — the feature's own component. No new markup.

Boot installs the set in `bootAI` (`internal/server/boot_helpers.go`).
`Hydrate` fills a live answer; `Rehydrate` re-fills stored history, so a reload
shows current rows. A provider that cannot fill is replaced by the honest
"couldn't load this" placeholder, never an empty panel.

## Interactive panels (the part that drifted)

The panel's rows carry the page's real actions:

- **Toggle/delete posts** carry `_target=<panel instance id>` plus the slice;
  the feature's routes answer by re-rendering the panel at that slot
  (`features/todo/handlers/panel_action.go`).
- **Delete confirmation** is the page's own dialog component, parameterized by
  a `deleteModalSpec`; pending-row signals are per instance, so two answers
  cannot confirm each other's row.
- **Slice tabs** ask the existing list route with `_target`; the route answers
  with the panel at that slot, never the page's `#todo-list`.
- **DOM ids** are prefixed by the panel instance, so the same row in two
  answers is not a duplicate id.

## Checklist for a new panel

- [ ] `Render` calls the feature's own page component; no second row/card/dialog.
- [ ] Actions reuse the page's wrapper and routes; no panel-only endpoint.
- [ ] `Writable` lists only model props; `Fill` is owner-scoped and maps the page model.
- [ ] Ids and pending-row signals are server-minted and scoped per instance.
- [ ] Tests: live wire round-trip (`rows` arriving as `[]any`), reload re-fill, native row markup, dialog scoping.
- [ ] Trim: the panel files are in the genui unit; every panel branch in a shared handler sits inside `// trim-genui:panel:start/end` and has a strip rule in `internal/installer/trim_genui.go`.

Reference: `features/todo/handlers/genui_panel.go`,
`features/todo/components/panel.templ`, `features/todo/handlers/panel_action.go`.
