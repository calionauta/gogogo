# Generative UI rules (genui + chat)

The agent emits **data**; the renderer decides what exists. Everything
below follows from that one line, and it is enforcement, not style: the
check that holds each rule is named.

## Which band does this answer need?

- **Static (Ask):** the model selects a registered component and fills
  typed props. `html_block` is deliberately absent from Ask's prompt, so
  the model cannot reach invented markup (`PromptForCatalog` vs
  `PromptForChat`).
- **Escape hatch (Chat):** one open-ended component — `html_block` — for
  layouts the catalog genuinely cannot express. Constrained twice:
  `SanitizeHTML` (closed allowlist; no script/style/handlers/forms/media)
  and `ModelHTMLVocabulary` (classes safelisted via `@source inline`, so
  an advertised class IS a styled class — `TestModelHTMLClassVocabulary`
  fails when list, safelist and bundle disagree).
- Dropping `html_block` from the registry turns invented markup off
  app-wide: the same mechanism enables and disables it.

## The catalog

- **Entry bar: a new component must answer a user request no existing
  component answers.** The catalog is a contract the model holds in
  working memory, so width costs accuracy before it costs tokens.
  Researched convergence is 20–40 primitives; the stricter production
  practice is 5–10. We sit at 6 leaves + `section` — treat that as a
  feature, not as a backlog.
- **Molecule/organism over atom.** Atoms (raw div/span) hand the model
  freedom and take the design system away; page-level components decide
  everything and leave the model nothing.
- **Props are the contract.** Prefer closed sets to open values
  (`action_buttons.method` is `POST|GET`; `url` must be a same-origin
  path, enforced by `checkAction`). An open string where an enum belongs
  re-introduces the unsafe-eval problem in a nicer costume.
- **Descriptions are written for the model.** In
  `PromptForCatalog`/`PromptForChat` say *when* to use a component, not
  what it looks like, and keep it short — it rides every request.
- **Accessibility lives in the catalog, not in the prompt.** The model
  cannot be relied on for roles, focus order or labels; audit the Templ
  component once and sample real compositions (good parts can still
  compose a bad page: heading order, focus traps).

- **`todo_list` renders read-only, with a link to /todo.** A write from the panel would have to land on the todo feature's patch target to stay live, and the panel does not own one: a button that writes while the list on screen stays stale reads as broken, which is worse than a link. Making it interactive means giving the todo feature a patch target this component can host (or a return channel for the click) — its own slice, not a half-working button now. The same reasoning governs `plan_cards`' **Apply** button: it POSTs to `/api/todos` from a page (`/chat`, `/genui`) that has no `#todo-list`, so the list patch matches nothing there and the row was created invisibly. Until a panel owns a patch target, such a write must at least say where the result went — the button names the destination ("Saves to your todos" + a link), and the response marks itself (`origin=genui` → `emitToastAction`) with a toast that does not auto-dismiss and links to `/todo`.

## Validation, repair, fallback

- Validation is a **runtime gate**, on every answer, not a CI check:
  `ParseDirectives` for Ask (fail closed — strictness is the demo),
  `ParseChatAnswer` for chat (degrade to prose, never fail a message).
- One bounded repair before the user sees a failure:
  `components.AnswerWithRepair` sends the validation error plus an
  excerpt of the rejected answer back to the model
  (`RepairFeedback`), capped at `repairAttempts`. Every extra attempt is
  a full round-trip on a tab that is already showing a spinner.
- Keep the two failure modes distinct: a `*components.ValidationError`
  means "answered, never in shape" (model/prompt problem); anything else
  from the responder is "never answered" (provider/key problem). Ask
  shows the provider hint for the second and must never blame the model's
  JSON for it.
- The fallback is a **product surface**: Ask's error result plus toast,
  chat's prose bubble (`unrenderable` / `stripPayload`). Never the raw
  payload.
- **The app's own components answer best when the question is about the app.** Both legs' prompts advertise the installed panels (today `todo_list {filter: open|done|all}`) — a panel of the viewer's own rows, filled by its provider from the store at answer time. The model chooses WHICH slice and never the content: a panel declares the props it accepts (`PanelProvider.Writable`) and an answer naming a server-owned field is a rejected shape, not something hydration quietly overwrites. `native` is a SURFACE (see `surface.go`), always on for both legs; the vocabulary is one registry (`installPanelRegistry` at boot), so the prompt and the router cannot disagree about it.
- **The prompt forbids inventing the user's data, and the prompt carries it.** A soft "reach for the app's own data when the request is about it" lost to the model's habit of writing a plausible list: asked to "plan my days from to-dos" it emitted a `data_table` of made-up titles. Two halves fix that and both are needed — `panelsGuidance` states the prohibition and the required answer shape (panel as the top-level component), `dataRule` repeats it last so it lands immediately before the request, and `DataContext` (`datacontent.go`) puts a bounded digest of the REAL rows in the prompt. A provider opts into the digest by implementing the optional `PanelSummarizer`; without real rows the model can only invent, so the rule alone is not enough.
- **Aliases are a spelling fix, not a vocabulary.** Models write `todos`/`todo`/`task_list` for `todo_list` and `ALL` for `all`. Both used to fail validation, which dropped a correctly-understood panel and left an empty answer. `panelAliases` + `todo.NormalizeFilter` canonicalize the spelling; an unrecognizable value still fails closed, and only compiled-in entries resolve.
- **Hydration replaces, it never drops.** A panel whose fill fails keeps its slot as an explicit "couldn't load this" placeholder with a link to the page that owns the data (`Hydrate` → `unavailableDirective`, `RenderAll` → `PanelUnavailable`). Dropping it was the old rule, on the theory that "you have no todos" would be a lie — true, but a silently MISSING panel is the lie users actually hit: the request was understood, the model emitted the right component, and the user saw nothing with no way to tell that from an empty list.
- **`panels/` and `renderers` are one seam, two owners.** A panel is contributed by the FEATURE that owns the page (`features/todo/handlers/genui_panel.go` implements `PanelProvider`; its row types and Templ live in `features/todo/`). `features/genui` knows names and props in the abstract and never what a todo is — one-way dependency, enforced by `check-flatdeps`. Adding a panel is one provider plus one line in the boot slice; if that ever needs a second edit in this package, the seam has regressed.
- Measure it, do not guess: every answer logs one line
  (`genui: catalog answer validated` with attempts/repaired/components;
  terminal cases with `outcome=fallback|degraded`). The repair rate and
  the unrecoverable rate are `read_json` queries — see docs/duckdb.md.

## Interop: adopt the shape, not the dependency

Our transport is Datastar SSE + HTML morphs inside one app we own, so
AG-UI (agent↔frontend events) and MCP Apps (third-party host iframes)
buy us nothing today. What the 2026 specs are worth is their *shape*,
and we already have most of it:

| A2UI concept | Ours |
|---|---|
| `updateComponents` (flat list, id refs) | `Directive{Component, Props, Children}` |
| catalog advertised to the model | `PromptForCatalog`/`PromptForChat` (A2UI v0.9 "prompt-first") |
| envelope with message types | `queue.Job{Type: genui|chat...}` payloads |
| `updateDataModel` | Datastar `MergeSignals` |
| renderer owns execution | Templ components server-side, patched by selector |
| catalog negotiation | `CatalogComponents()` + `Registered()` |

Keeping that mapping honest is what makes a future A2UI adapter a thin
function rather than a rewrite — and it is the reason not to take the
dependency now (see docs/inspirations.md).
