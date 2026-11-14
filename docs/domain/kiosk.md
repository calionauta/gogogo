# kiosk

**Owns.** The order flow: a closed catalog of choices, the deterministic parse of free text (typed or spoken) against it, the priced preview, the confirmed order row, and the chat panel that reports them (`/kiosk`, `/api/kiosk/*`).
**Does not own.** Speech recognition (the browser's Web Speech API; the page degrades to typing), the model client (`llm`), or the genui panel machinery (`genui`).

## Language

| term | means | not |
|---|---|---|
| catalog | the closed set of items, sizes, milks and extras this counter sells | the whole product catalog |
| order | one confirmed row, owned by one user | a cart, a draft |
| preview | the parse plus its price, before anything is written | an order |
| corrected | the confirmed order differs from the preview the user was shown | an edited order |

## Invariants

- Parsing is deterministic over the closed catalog; the same utterance yields the same order, and an unresolved one says what is missing instead of guessing.
- `owner` is stamped from the session; no request field can set it.
- The price is recomputed and stored at confirm time, from the catalog, not from the client's numbers.
- `corrected` is set only when the posted preview differs from the serialization of the confirmed order.
