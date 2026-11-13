# Generative-UI schemas: what OpenUI and A2UI specify, and what is worth copying

Reviewed: 2026-11-12

The question this note answers: **is there a schema pattern in OpenUI or A2UI
worth following for components this app generates on demand, outside the
catalog, or for the catalog's own components?** It evaluates the two formats
against what `features/genui` already enforces and records what was adopted,
what was rejected, and why. The short version: copy the *validation discipline*,
not the format.

## What each standard actually specifies

**OpenUI Lang** (Thesys). A compact, line-oriented language (`root = Stack([chart])`)
that a model emits instead of JSON. The system prompt is *generated from your
component library*, so the vocabulary the model is told about and the set the
renderer maps are the same list. The parser validates each line, drops what is
invalid, and renders the rest; the vendor's Gateway repairs invalid output while
it streams. `$ref` allows one definition to be reused.

**A2UI** (Google). JSON envelopes (`createSurface`/`updateComponents`) over a
**catalog**, where a catalog is a JSON Schema (draft 2020-12) with a `catalogId`
and one schema per component: `required`, `enum`, and
`unevaluatedProperties: false`. Props can be `Dynamic*` — a literal or a data
reference — so structure is model-authored and values are bound to a server data
model. Validation is two-phase (agent-side before sending, client-side on
receipt), and catalogs are swappable, which is how one envelope serves different
component sets.

## The measured evidence

The OpenUI benchmark (2026-08-18, `lang-core 0.2.16`, 46 interface briefs × 6
models × 4 generations = 1,104 runs per format) separates structural validity
from render success:

| Format | Structural validity | Render success | Blank screens |
|---|---|---|---|
| OpenUI | 96.9% | 99.9% | 1 |
| A2UI | 95.6% | 96.6% | 37 |
| json-render | 82.8% | 99.6% | 4 |

Two readings matter more than the ranking:

1. **The two leading formats are within 1.3 points on validity.** Reliability is
   roughly format-agnostic; what separates them is token cost (OpenUI's generated
   system prompt is about 5.0k tokens against A2UI's 12.6k) and the *shape* of the
   failure (blank screens vs invalid runs).
2. **Repair, not the format, is what protects users.** The same vendor's
   production funnel reports 88% of invalid generations repaired by incremental
   editing and 0.9% reaching a user broken.

That second point is the one that describes this repository's situation: the
catalog fails closed at parse time, so a bad generation is an error toast, not a
broken interface.

## What this codebase already has

| Standard's mechanism | Here |
|---|---|
| `unevaluatedProperties: false` | `decodeProps` runs `DisallowUnknownFields`; each renderer rejects unknown props |
| `required` / `enum` | per-renderer checks and closed prop sets (`PanelSpec.Values`, action field names) |
| Declared capabilities | `ActionProvider` + `checkDeclaredAction`: the model picks *which offered action with what content*, never which endpoint |
| `Dynamic*` bound to a data model | the `native` surface: panels fill rows server-side, so there is no data path for a model to forge |
| Catalog-as-data | the `renderers` map + `catalogLeafNames` + prompt fragments, with the parser and prompt reading the same names |
| Generated prompt from the library | `PromptForSurfaces` built from the same band |

So the gap is not strictness and not a schema language. It is that **the
catalog's contract is not measured**, and that a prompt and a validator can drift
apart without anything failing.

## Decision

Adopted, in `features/genui/components/catalog_fixtures_test.go`:

1. **A fixture corpus per component** — a valid generation shape and the
   counter-examples a model is known to produce, expressed in the JSON a model
   emits, asserting parse *and* render. Validity and "did it answer the request"
   stay separate: the corpus measures the contract, not the model.
2. **A prompt↔validator parity gate** — every advertised name resolves to a
   renderer, and every advertised name is spelled in the prompt. A name in one
   and not the other is the drift that reaches users.
3. **Fail closed, unchanged.** One invalid component refuses the whole answer. The
   reliability data argues for repair *around* a strict core, not for rendering
   half an interface.

Rejected:

- **OpenUI Lang / A2UI envelopes as a second runtime.** Both would duplicate the
  renderer and the contract to buy token savings this app does not spend (a
  ten-name catalog with closed props) and a repair loop it does not need (parse
  already refuses).
- **Partial salvage** (render the good components, drop the bad). A generative
  region missing the card that carried the action looks complete and is not.
- **A hand-written JSON Schema file for the catalog.** Today the Go structs *are*
  the schema, and one consumer reads them. Writing a second machine-readable
  copy would be a fourth place to keep in step. The trigger to generate one is
  concrete: a second consumer (an MCP-exposed catalog, a native shell) that
  cannot import the Go package — then generate it from the structs, never by hand.
- **A2UI's `Dynamic*` data binding.** This repo gets the same guarantee more
  cheaply by keeping model-authored structure and server-owned data on separate
  surfaces.

## What is left open

A stored answer is re-validated through the catalog on reload
(`features/chat/handlers`). If a component is renamed or removed, an old answer
can stop rendering — the catalog has no version stamp, which is the one idea in
A2UI worth revisiting. **Trigger:** the first stored answer that fails to
re-render after a catalog change. The fix then is a catalog id/version recorded
with the answer, not a new format.

## Status

Shipped 2026-11-12: the fixture corpus and the parity gate. No dependency, no
runtime change, no prompt edit. Model reliability against this catalog is still
measured outside the suite, with the operator's own key — a test never calls a
real model.

## References

- OpenUI, language introduction: https://www.openui.com/docs/openui-lang
- OpenUI, reliability: https://www.openui.com/docs/openui-lang/reliability
- OpenUI, format benchmark: https://www.openui.com/benchmarks/framework
- OpenUI, benchmark methodology: https://www.openui.com/benchmarks/methodology
- A2UI, catalogs: https://a2ui.org/concepts/catalogs
- A2UI, protocol v0.9.1: https://a2ui.org/specification/v0.9.1-a2ui
- A2UI, basic catalog schema: https://github.com/a2ui-project/a2ui/blob/main/specification/v0_9/catalogs/basic/catalog.json
