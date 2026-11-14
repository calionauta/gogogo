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
| **recipe** | A working slice — pages, handlers, components, tables, a job, policy rows — **copied** into your tree, with a receipt, and intended to be edited. You own the code the moment it is written. | `catalog/`, `.recipes/<id>.yml` |
| **receipt** | The recipe's record: where it came from, what it declares, and the hash of every file it installed. The only source for drift and update. | `.recipes/<id>.yml` |
| **declaration** | The append-only half of the receipt: what the code *claims* — routes, tables, jobs, slots, actions, metrics, outcomes, panel. The engine applies it; you never hand-edit it. | `internal/recipe` |
| **capability** | One row of the registry: the thing that owns a set of files and routes. Its **kind** says how it is removed. | `internal/capabilities` |
| **kind** | The removal class — the one answer to *what disappears if I delete this*: `core` (the binary or its gates break), `servant` (the binary works, a capability stops), `feature` (a user surface disappears). One name per level, in code, JSON, docs and UI. | `internal/capabilities` |
| **servant** | A `servant`-kind capability: it serves other capabilities and outlives them (`nats`, `llm`, `graph`, `license-activation`). Not installable as a recipe; deleted by hand with the consumers that name it. | `internal/capabilities` |
| **unit** | What a capability owns: its directories, its files, and its `Register`. The unit is the removal granularity. | `docs/unit-contracts.md` |
| **slot** | A named contribution point a recipe fills without touching the host page: `dashboard.card`, a settings section, a row action. The page owns the frame; the recipe supplies the content. | `internal/recipe` |
| **metric** | A declared COUNT over a table the recipe owns. It answers **how much happened**. | `internal/recipe` |
| **outcome** | A declared job as a success count over the eligible count, plus the counter signals that limit the reading. It answers **did the job get done**. | `internal/recipe` |
| **panel** | A declared chat surface: component, prompt and the values it may write. The feature's own components, never a copy. | `features/genui` |
| **drift** | The difference between the receipt's hashes and the tree: **pristine**, **modified** or **deleted**. `recipe status` reads it; `recipe update` keeps your edits. | `internal/recipe` |
| **catalog** | The reviewed set of recipe versions an install can come from, each with its own licence, version, `author`, `released` date and reviewed-against commit. It is also the only place a recipe is listed as installable. | `catalog/` |
| **author** | Whose work a recipe is: `{name, email?}`. The name is required on a catalog entry and travels into the receipt, so credit survives the copy. `released` (a date) rides beside it and is catalog-scoped: a bundled recipe's release is the template's. | `catalog/`, `.recipes/<id>.yml` |
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
| `_extensions/` | `catalog/` (reviewed entry) or `.recipes/` (receipt) | #385 |
| `_recipes/` | `catalog/` (what can be installed) or `.recipes/` (what is installed) | this commit |
| `extensions.gen.go` | `recipes.gen.go` | #385 |
| `extension_list` / `extension_describe` / `extension_invoke` (MCP tools) | `recipe_list` / `recipe_describe` / `recipe_invoke` | #385 |
| `Kind: plugin` (removal class) | `Kind: servant` | this commit |
| `trim` as an installer operation (`--plugins`, `--features`, keep/drop presets) | nothing — scaffold ships the whole tree; remove a feature with `recipe uninstall <id>`, a servant by hand | this commit |
| `trim_plan` / `trim_apply` (MCP tools) | `scaffold_plan` / `scaffold_apply` — they scaffold, never trim | this commit |

Two reasons, one per row group. **One name per level:** `plugin` in code and
`servant` in prose made a reader translate before they could read, so the class
has a single name now — the one the registry stores. **One mechanism per
outcome:** `trim` described a scaffold-time keep/drop list that no longer
exists, so three surfaces kept advertising a command and a flag set that would
fail. The bare words stay legal where they mean something else (a DaisyUI
plugin, a provider plugin, a file extension).

## Related

- [Scope taxonomy](scope-taxonomy.md) — what a capability is, and how it is removed.
- [Unit contracts](unit-contracts.md) — what each unit owns, generated.
- [Recipes: the authoring contract](../skills/gogogo-recipe-authoring/SKILL.md) — what a recipe may own and what it must never touch.
