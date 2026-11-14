# Domain cards

Every feature slice has one written page, `docs/domain/<capability-id>.md`,
that says what the slice **is** — not what files it owns. The registry already
answers "what does this unit own and how is it removed"; the card answers "what
does this noun mean, what is it not, and which rules must stay true".

The reason is narrow and practical: the expensive failure when changing a slice
is not missing code, it is a *plausible parallel model* of the domain — an
invented field, a second owner column, a delete that skips the policy, a prompt
that calls a note a "document". Four short parts give a reader (human or model)
the vocabulary, the boundary and the rules in one read.

## The contract

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
```

The card has two writers and two readers. **`gogogo new feature <name>` writes
it** (the scaffold knows the routes it just registered and the rules the
generated handler already keeps), and **`capabilities_list` reads it**: pass
`dir` and every feature in the registry answer carries the one-line `owns`
boundary, so an agent choosing a unit sees what the slice is for and not just
its id. The card is written outside the recipe receipt on purpose — it is
documentation, not pristine unit code, so rewriting a boundary must not read as
drift in the code it describes.

`make check-domains` proves the mechanical half:

- every `feature` capability has a card, and every card maps to a `feature`
  capability (a card for a slice that is gone is the same drift, reversed);
- `**Owns.**` and `**Does not own.**` are present **and non-empty** — a heading
  is not a boundary;
- `## Language` carries a table with at least one term row;
- `## Invariants` carries at least one bullet.

It does not judge the prose. A gate that guessed at meaning would become the
second source of truth the card exists to prevent; review reads the words.

## Writing one

- **Owns** is the noun plus what the slice decides about it.
- **Does not own** names the nearest wrong neighbour and where that lives. This
  is the line that stops a second model of the same thing.
- **Language** is the glossary: one row per word the code, the UI and the
  prompts must agree on. If a synonym exists in the tree, say which one wins.
- **Invariants** are rules a test can point at: who stamps the owner, which
  policy gates a delete, what is stored versus recomputed. Not a feature list.

Keep the card current in the **same commit** as a behaviour change — the same
rule the rest of `docs/` follows. If the code and the card disagree, the card is
the bug you fix now.

The proposal that led here, including what of DDD was deliberately left out, is
the "Domain-first gogogo" research note under `docs/research/`.

## What the gate deliberately does not check

- **The routes.** `docs/unit-contracts.md` already lists them, generated from
  the code. A second copy in the card would be a second source of truth — the
  exact drift this gate exists to prevent. The card names the *slice*, the
  generated doc names the *surface*.
- **The prose.** A gate that guessed at meaning would become the second source
  of truth the card exists to prevent. A heading is not a boundary; a wrong
  boundary is review's job.
