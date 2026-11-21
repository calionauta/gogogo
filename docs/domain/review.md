# review

**Owns.** The writer → critic → judge loop: propose a fix, critique it a bounded number of times, classify the verdict, and deliver it two ways — streamed for the page (`/api/review/run`), as one JSON verdict for a program (`/api/review/verdict`) — keeping nothing server-side.
**Does not own.** The model connection and metering (`llm`, `credits`), the prompt files (`internal/prompts`), or a diff/patch engine — the proposal is text.

## Language

| term | means | not |
|---|---|---|
| proposal | the writer's candidate change, as text | a committed edit |
| critic | the bounded review pass over a proposal | an unbounded retry loop |
| verdict | the classifier's APPROVE/REVISE answer | a human approval |

## Invariants

- The critique loop is bounded; a run cannot spin on the same proposal.
- The verdict comes from the classifier step, not from the critic's prose.
- A run leaves no artifact behind — only the billing ledger records it.
- Both representations execute the same `Runner.run` with the same bounds; the JSON route answers the `Verdict` exactly as the page renders it, so a program never has to parse patch frames.
- With no provider configured the page renders the setup hint and no run starts.
