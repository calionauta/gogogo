# Glossary — one meaning per term

gogogo has more nouns than any one person can hold. This page is the **one
place a term is defined**: the ubiquitous language the code, the docs and the
agents share. A term that is not here has no agreed meaning yet.

The rule is short: **if you introduce a term, define it here in the same
commit** — and if you retire one, move it to the table at the bottom, with the
word that replaced it. One concept, one name, one place.

## The concepts

| Term | Meaning | Lives in |
|---|---|---|
| **recipe** | A working slice — pages, handlers, components, tables, a job, policy rows — **copied** into your tree, with a receipt, and intended to be edited. You own the code the moment it is written. | `_recipes/`, `.recipes/<id>.yml` |
| **receipt** | The recipe's record: where it came from, what it declares, and the hash of every file it installed. The only source for drift and update. | `.recipes/<id>.yml` |
| **declaration** | The append-only half of the receipt: what the code *claims* — routes, tables, jobs, slots, actions, metrics, outcomes, panel. The engine applies it; you never hand-edit it. | `internal/recipe` |
| **capability** | One row of the registry: the thing that owns a set of files and routes. Its **kind** says how it is removed. | `internal/capabilities` |
| **kind** | How a capability is removed: `core` (the template), `plugin` (a **servant** — it serves others and outlives them), `feature` (a terminal user surface — a page or journey disappears). | `internal/capabilities` |
| **servant** | A `plugin`-kind capability: whiteboard, graph, license-activation. It serves other capabilities, so it is not theirs to remove. | `internal/capabilities` |
| **unit** | What a capability owns: its directories, its files, and its `Register`. The unit is the removal granularity. | `docs/unit-contracts.md` |
| **slot** | A named contribution point a recipe fills without touching the host page: `dashboard.card`, a settings section, a row action. The page owns the frame; the recipe supplies the content. | `internal/recipe` |
| **metric** | A declared COUNT over a table the recipe owns. It answers **how much happened**. | `internal/recipe` |
| **outcome** | A declared job as a success count over the eligible count, plus the counter signals that limit the reading. It answers **did the job get done**. | `internal/recipe` |
| **panel** | A declared chat surface: component, prompt and the values it may write. The feature's own components, never a copy. | `features/genui` |
| **drift** | The difference between the receipt's hashes and the tree: **pristine**, **modified** or **deleted**. `recipe status` reads it; `recipe update` keeps your edits. | `internal/recipe` |
| **catalog** | The reviewed set of recipe versions an install can come from, each with its own licence, version and reviewed-against commit. | `catalog/` |
| **evolution surface** | `cmd/gogogo` + `cmd/gogogo-mcp`: the developer's tools. They may rewrite the checkout. Never shipped to a product's users. | `cmd/gogogo`, `cmd/gogogo-mcp` |
| **product surface** | `cmd/web` routes + `POST /api/ext/invoke`, under the product's own session and RBAC. What a product's users and their agents call. | `cmd/web` |
| **gate** | A check that can fail the build. A gate that only prints is a warning, and says so. | `Makefile`, `bin/` |
| **agent index** | The map an agent reads first: `llms.txt`, then the page it points at. | `llms.txt` |

## Words to stop using

A term retired here is retired for good: the checker below rejects the
compound forms, so the old name cannot come back through a doc or a skill.
(The bare English word is not banned — a *file extension* and a *public
extension point* are different words.)

| Retired | Say instead | Since |
|---|---|---|
| `gogogo extension …` | `gogogo recipe …` | #385 |
| `_extensions/` | `_recipes/` | #385 |
| `extensions.gen.go` | `recipes.gen.go` | #385 |
| `extension_list` / `extension_describe` / `extension_invoke` (MCP tools) | `recipe_list` / `recipe_describe` / `recipe_invoke` | #385 |

The reason is not taste: a recipe is **copied and owned**, an extension is
**depended upon**. Calling it the second name made people expect a package
in `go.mod` and miss the receipt — which is the whole point of the model.

## Related

- [Scope taxonomy](scope-taxonomy.md) — what a capability is, and how it is removed.
- [Unit contracts](unit-contracts.md) — what each unit owns, generated.
- [Recipes: the authoring contract](../skills/gogogo-recipe-authoring/SKILL.md) — what a recipe may own and what it must never touch.
