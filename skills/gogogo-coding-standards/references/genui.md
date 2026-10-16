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

- **`todo_list` renders read-only, with a link to /todo.** A write from the panel would have to land on the todo feature's patch target to stay live, and the panel does not own one: a button that writes while the list on screen stays stale reads as broken, which is worse than a link. Making it interactive means giving the todo feature a patch target this component can host (or a return channel for the click) — its own slice, not a half-working button now.

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
- **The app's own components answer best when the question is about the app.** Chat's prompt also advertises `todo_list {filter: open|done|all}` — a panel of the viewer's own rows, hydrated by the worker from the store at answer time. The model chooses WHICH slice and never the content (props are the render shape; unknown fields fail closed), hydration happens in the worker because the renderer has no store, and a panel that cannot be hydrated is dropped rather than rendered empty. Chat-only: Ask never advertises it and the router's labels come from Ask's own vocabulary.
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
