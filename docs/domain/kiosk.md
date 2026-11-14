# kiosk

**Owns.** The order flow: a closed catalog of choices, the deterministic parse of free text (typed or spoken) against it, the priced preview, the confirmed order row, and the chat panel that reports them (`/kiosk`, `/api/kiosk/*`).
**Does not own.** Speech recognition (the browser's Web Speech API; the page degrades to typing), the model client (`llm`), or the genui panel machinery (`genui`).

## Language

| term | means | not |
|---|---|---|
| catalog | the closed set of items, sizes, milks and extras this counter sells | the whole product catalog |
| order | one confirmed row, owned by one user | a cart, a draft |
| preview | the parse plus its price, before anything is written | an order |
| draft | the order currently on screen, posted back as the `previewed` token so the next utterance applies to it | a second order |
| decision | one typed answer from the jev-like engine (an item ID, include/exclude, or a yes/no probability) | free-form text |
| corrected | the confirmed order differs from the preview the user was shown | an edited order |

## Invariants

- Parsing is deterministic over the closed catalog and is the FAST PATH: the same utterance yields the same order, and an unresolved one says what is missing instead of guessing.
- The decision engine resolves the customer's FINAL intent over the draft in one typed call: additions, removals, retractions ("add cinnamon… no, remove it") and "only the drink I named". It chooses only among the catalog's own IDs and an answer outside them is discarded, so it can invent no item, size, milk, extra or price.
- The parser is additive and cannot remove; a removal or a retraction is the engine's job. When the engine is off, the parser-only path still adds and never subtracts.
- The engine runs only when it can change the answer: a correction cue, an extra in play, or a draft already on screen (two words are enough then), otherwise a non-complete parse of three words or more. A complete plain order costs no round trip.
- `owner` is stamped from the session; no request field can set it.
- The price is recomputed and stored at confirm time, from the catalog, not from the client's numbers.
- `corrected` is set only when the posted preview differs from the serialization of the confirmed order. Preview and confirm run the same draft merge and the same decision, so the engine cannot make them disagree.
