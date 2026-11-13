# Domain-first gogogo: what DDD buys an AI contributor

Reviewed: 2026-11-12

Source: [Matt Pocock, 2026-11-11](https://x.com/mattpocockuk/status/2120606575819956439)
— "if you can describe your business processes in clear, consistent language…
you've already half-built all the software you need to automate the process.
DDD has never been more powerful." The companion claim from the same author
([readability post](https://x.com/mattpocockuk/status/2117953152376799498)) is
what makes it actionable for this template: "does the code use the same domain
language as the human describing the code?" is the property that lets an agent
translate words into changes.

**Question.** What of domain-driven design would make gogogo easier for an AI
to evolve safely — not as ceremony, but as the smallest set of artifacts that
turn "add a feature" from a guess into a bounded edit?

**Short answer.** Four things the template already half-has, made explicit:
a **bounded context per capability**, a **domain card** with the ubiquitous
language and invariants, the **service layer as the only writer**, and the
**recipe declaration as the published contract**. The rest of DDD (aggregates,
repositories, domain events) either exists under another name or is ceremony
for this stack.

## The mapping

| DDD concept | In gogogo today | What is missing |
|---|---|---|
| Bounded context | one `features/<name>/` slice, one registry row, one owner (`docs/scope-taxonomy.md`) | the slice has no written boundary: what it *means*, not just what it owns |
| Ubiquitous language | handler/service/query names, message copy | a glossary, so the model reuses the product's words instead of inventing synonyms — in code **and in prompts** |
| Invariants | `features/<name>/service/` owns writes, `canDelete*` gates deletes (`docs/service-layer.md`) | a stated list, so a test can point at the invariant it proves |
| Published contract | the recipe `Declaration` (routes, actions, panels, metrics) | nothing — this is already the anti-corruption layer, and it is what MCP clients read |
| Anti-corruption layer | `cmd/check-flatdeps` (no cross-feature domain import) | nothing — the gate is the boundary |

## The one artifact worth adding: the domain card (a per-domain wiki)

The tweet's claim is not "write more design docs". It is that **consistent
language is already half the implementation**: once "note", "share" and
"board" mean one thing everywhere, the agent's prompt, the handler names, the
DB columns and the review conversation all describe the same object, and the
remaining work is mechanical. A glossary that lives nowhere is re-derived on
every session; a glossary in the tree is read every session. That is why the
card is a wiki the agents read *and update*, not a one-time specification.

`docs/domain/<feature>.md`, written by `gogogo new feature` and kept next to
the slice:

```markdown
# notes

**Owns.** The user's shared notes: title, body, visibility, delete rights.
**Does not own.** Presence and caret (that is `collab`), the inbox row that
announces a share (that is `notifications`).

## Language
| term | means | not |
|---|---|---|
| note | a saved document owned by one author | a chat message |
| share | read access granted to every signed-in user | a copy |

## Invariants
- `Create` stamps the owner from the session; a client id is never accepted.
- `Delete` requires `canDeleteDoc` — author or admin.
- A note's title is trimmed and non-empty; body may be empty.
```

Why this helps an AI specifically: the failure mode is not missing code, it is
a *plausible* parallel model of the domain (invented fields, a second owner
column, a delete that skips the policy, a prompt that calls a note a
"document"). Three short sections give the model the vocabulary, the boundary
and the rules in one read — the same information a senior reviewer would
volunteer before a change. The language section is also the source the prompt
files (`docs/prompts.md`) are written against: a panel prompt says "note"
because the card says "note", not because the model was guessed at twice.

## Where it plugs in

1. **Scaffold.** `gogogo new feature <name>` writes the card from a template
   (owns / does-not-own / language / invariants), the same way it writes the
   handler, page, registry row and RBAC rows. A card with only the template
   placeholders is a `TODO` the review sees.
2. **Gate.** `go run ./cmd/check-domains`: every `KindFeature` capability has a
   card; every route in its `Declaration` appears in the card's "Owns" section
   (a route nobody documents is a surface nobody owns); every exported
   `service` mutation has a matching invariant bullet or an explicit
   `// no invariant:` marker. Cheap, text-only, no new dependency.
3. **Agent instructions.** `AGENTS.md` (and the scaffolded project's copy) say:
   read `docs/domain/<feature>.md` before editing the slice, and update it in
   the same commit as a behavior change — the same rule `docs/` already has.
   The rule is one sentence because it must be followed on every session:
   "If the code and the card disagree, the card is the bug you fix in the same
   commit" (the wording `docs/` already uses for behavior docs).
4. **MCP.** `capabilities_list` gains the card's one-line "Owns" summary, so a
   client choosing a unit sees the boundary, not just the id.

## What NOT to add

- **Aggregates and repositories.** The service layer plus sqlc queries is the
  same boundary with less indirection; a repository interface per table would
  be the pass-through `docs/dependency-principles.md` rejects.
- **Domain events as a framework.** `internal/queue` and the recipe actions
  already carry cross-slice effects; a second event bus is a second truth.
- **A diagram per feature.** The capability graph (`features/graph`) renders
  the real wiring; a hand-drawn context map goes stale in a week.

## Status

Shipped (PR #425): the card gate (`check-domains`), the scaffold writing the
card, the MCP catalog reporting the one-line boundary, and the twelve cards
for the feature slices. The measured claim below is still open — it needs an
agent run, not a gate.

## Decision

Adopt the domain card plus the `check-domains` gate, generated by the
scaffolder and consumed by the agent instructions. It is the cheapest DDD
artifact that changes behavior, and it is the one that most directly reduces an
AI's ability to invent a second, inconsistent domain model.

The measured claim to verify after adoption is narrow and testable: on a task
that names a domain noun ("add a share field to notes"), does the agent touch
the same files and the same words a human reviewer would? Compare one task
before and after the cards exist; if there is no difference, the cards are
ceremony and should be deleted rather than maintained.
