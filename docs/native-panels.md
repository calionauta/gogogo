# Native panels: Chat answers with the app's own data

> UI label: **recipe panels** in Chat and /config. `native` is the surface name (`GENUI_CHAT_SURFACES`, model prompt); recipe is the human word for the same thing.

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

Boot installs the set in the root (`internal/server/chi.go`): every recipe
appends its provider to `register.Deps.Panels` during the recipe step and
the root installs the catalog once. `Hydrate` fills a live answer;
`Rehydrate` re-fills stored history, so a reload shows current rows. A
provider that cannot fill is replaced by the honest
"couldn't load this" placeholder, never an empty panel.

**History is versioned against the catalog.** `components.CatalogVersion()` is
the fingerprint of the vocabulary the chat surfaces advertise — leaves plus
installed panel names, hashed, never bumped by hand. The worker stamps it on
every stored answer (`store.Message.CatalogVersion`); on reload, an answer whose
stamp differs is filtered through `FilterDirectives` before rendering, so a
renamed or retired component drops out and the rest of the message survives.
Without the stamp, one retired name makes `RenderAll` fail and blanks the whole
answer. Unstamped (older) rows and matching rows render exactly as stored.

## Declarative spec (schema-first)

The component name, prompt and writable props also live in the recipe's
`Declaration` (`internal/recipe/receipt.go`): `PanelSpec{Component,
Prompt, Writable}`. `checkDeclaration` and the seam `Validate()` reject a
non-snake_case component, an empty prompt or an empty writable list at
boot (fail fast). The code provider should mirror the spec (the boot
never guesses the scaffold's file). Scaffolded recipes get both: the
declaration entry and a `genui_panel.go` skeleton using the shared
`SlicePanel` base in `features/genui/components/panel.go` — only the two
closures (List, Render) are recipe-specific.

## The catalog (where recipes live)

The catalog **is this repository**: `.recipes/*.yml` receipts +
`internal/capabilities/registry*.go` rows + `GenerateGo`'s generated
`internal/server/recipes.gen.go`. There is no separate registry repo by
design — the drift check (`checkDeclaration`) compares code against the
pinned receipt in the same commit, so a recipe added elsewhere would need
a second source of truth to stay in sync. New recipes arrive as PRs
adding: `features/<name>/` (handlers + components), a capability row, a
`.recipes/<name>.yml`, and generated `recipes.gen.go`.

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
- [ ] Port path: the panel files are in the genui built-in unit; every panel branch in a shared handler sits inside `// trim-genui:panel:start/end` and has a wiring rule in `internal/installer/builtin_genui.go` (the marker vocabulary the export bundle reads).

Reference: `features/todo/handlers/genui_panel.go`,
`features/todo/components/panel.templ`, `features/todo/handlers/panel_action.go`.
