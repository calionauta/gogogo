# Search

Search is a **plugin**, not a dependency: one `Searcher` seam, three layers you
can switch per request, and no external engine. The todo list is the reference
consumer — `internal/search` is the plugin's half, `features/todo/handlers/`
holds the consumer's half, and the two meet at a field name (`"title"`).

Remove it with `trim search` and the app keeps working: the list falls back to
the plain unfiltered view. See [Trim and degrade](#trim-and-degrade).

## Why no external engine

The driver ships no FTS5 — verified across tursogo v0.8.2, v0.8.3 and
v0.9.0-pre.1, where `USING fts5` answers `unknown module name 'fts'`.

A separate index store (Bleve, bluge) would put the truth of a row in two
places. So the layers are ours, in the same database file, and the heaviest one
writes postings **inside the row's own transaction**.

The trade is honest: the cheap work stays cheap, and the expensive layer is one
flag away rather than one service away.

## The three layers

| Layer | `Match` label | Where the work happens | Extra write | Ranking | Typos |
|---|---|---|---|---|---|
| `substring` | exact | SQL `LIKE` in the consumer's own query | none | list order | no |
| `fuzzy` | typo-tolerant | Go, over the viewer's visible candidate rows (own + public) | none | tier score | yes |
| `bm25` | relevance | Go scoring of `search_postings` in the same DB | one postings row per distinct term per mutation | BM25 | no (stemmed tokens) |

**`substring` is the floor.** It costs nothing — no extra write, no scan in Go
— and it is the layer that still answers when the plugin is trimmed or the
smarter modes are off.

**`fuzzy` is tiered, not scored.** The query is only as good as its weakest
token (AND semantics: one token that matches nothing rejects the row), and the
score is the weakest tier reached:

| Score | Tier | Example |
|---|---|---|
| 1.00 | the normalized query appears verbatim, contiguous | `invoice` |
| 0.95 | every query token IS a field token, not contiguous | `guard smoke` |
| 0.90 | every query token is a prefix of a field token | `parse dif` |
| 0.80 | every query token is one edit away | `smoek` |
| 0.75 | subsequence, guarded by a length floor and ratio | `handooff` |
| — | some token matched nothing → no hit | |

Comparison happens between stems; the reported span is the raw token's, which
is what keeps a highlight on the right word.

**`bm25` is ranked, not filtered.** Constants are Lucene's defaults (`k1=1.2`,
`b=0.75`), and two properties come free that no substring match has: a rare
term outranks a common one (IDF), and a short document stating the term once
outranks a long one repeating it (saturation + length normalization). A
document matching only some terms still scores — that is the deliberate
difference from `fuzzy`: ranking, not filtering.

## One seam, chosen at runtime

`SEARCH_MODE` decides which layer a tab **starts** on; it does not pin the
build, because **all three layers are always wired**. The search box's `Match`
select sends `?mode=` with the next request, so switching layers is a query
parameter, not a restart.

An unknown or unwired mode falls back to the default inside
`SearchLayers.For` — a hand-edited URL or a tab holding a mode the operator
removed keeps searching instead of erroring.

Consumers hold a `search.Searcher` (or the todo feature's stdlib-only
`SearchLayers`) and never branch on a mode name. That containment is what makes
a fourth layer a one-line addition — see
[Adding a fourth layer](#adding-a-fourth-layer).

## The contract: search only narrows

Both halves are visibility-scoped **in SQL**: the viewer's own rows plus public
rows, never another owner's private row. The handler intersects the hits
with the list it already scoped for visibility and filter. A hit whose row is
not in that list is dropped, never resurrected — even if a stale posting
outlived its row.

An empty query returns the list untouched, so "no search" is exactly what the
app rendered before the plugin existed.

## The index, and the boot heal

Postings are a cache of a Go-side pure function (`search.Stem` over
`search.Tokenize`), so SQL cannot rebuild them. A row created while `bm25` was
unwired has no postings, and relevance search would silently answer "no
results" for data the user can see.

`HealSearchIndex` runs once at boot: it reads the index inventory and the rows,
and re-indexes exactly the rows missing from the inventory. It is idempotent and
does nothing when the two agree, so it is safe on every boot rather than a
migration step nobody remembers to run — and it also repairs postings lost to a
rolled-back crash or a backup taken before the index existed.

It deliberately does not delete postings for rows that no longer exist: an
orphan cannot match anything (every hit is intersected with the visible set),
and the postings carry the row's visibility so BM25 can include public rows
without dropping the owner scope. A boot-time delete is a worse failure mode
than a stale key.

## Trim and degrade

`trim search` removes `internal/search`, the consumer half
(`features/todo/handlers/search_source.go`) and its tests, the boot helper
(`internal/server/boot_search.go`), and the one composition-root call.

What stays, on purpose: the handler's search slice (`search.go`), the
`/api/todos/search` endpoint, the `SEARCH_MODE` knob, and the template's box.
They hold stdlib-only interfaces and a bool, so the tree still compiles and the
endpoint answers with the unfiltered list.

A trimmed build shows no `Match` picker — one option would be noise.

## Adding a fourth layer

The database's own FTS is the expected arrival: the switch in `New` is the only
place that knows the layers exist, so the day the driver ships it, the addition
is one case there plus one name in `Modes()`.

Boot builds every layer it has the inputs for (`NewAll`), and the picker renders
from the list the server sends — so a new layer appears everywhere at once,
with a raw name until `searchModeLabel` gives it a friendly one.

## Files

- `internal/search/` — the plugin: `Searcher`, `New`, `NewAll`, `Modes`,
  `Tokenize`, the three layers, the stemmer, the SQL index.
- `features/todo/handlers/search*.go` — the consumer half: the source, the read
  path, the mode vocabulary, the heal.
- `internal/server/boot_search.go` — the composition root.
- `internal/dbschema/migrations/0011_search.sql` — `search_postings`.

## Related

- [Features](features.md) — the capability table and its runtime opt-outs.
- [Configuration](configuration.md) — `SEARCH_MODE` and the runtime constants.
- [Scope taxonomy](scope-taxonomy.md) — why the plugin half can be deleted and
  the feature half cannot.
- [Stack tradeoffs](stack-tradeoffs.md) — what an external search engine would
  have bought and cost.
