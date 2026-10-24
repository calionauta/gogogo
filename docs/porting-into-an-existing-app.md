# Porting into an existing app

`gogogo add` and `gogogo new` only touch a checkout they scaffolded. They
rewrite the scaffold's own router, composition root and capability registry,
and they refuse rather than guess at yours. That is the right default — a tool
that edits an arbitrary codebase is a rewrite wearing a flag — but it leaves
one case uncovered: **my app already exists, and I want this piece.**

`gogogo export` serves that case. It is read-only in the direction that
matters:

```bash
gogogo export whiteboard --dir ~/gogogo --to /tmp/port-whiteboard --module example.com/myapp
```

| What it writes | Where |
|---|---|
| the unit's owned files | `<to>/<unit>/…`, keeping repo-relative paths |
| `PORTING.md` | `<to>/PORTING.md` — the routes it registers, the imports to rewrite, the wiring to write, the proof |

It refuses a `--to` inside the template checkout and a `--to` that is not
empty. Nothing in `--dir` changes, and nothing outside `--to` is written. The
dependency closure comes along: exporting `whiteboard` also exports `sounds`,
because the board page calls `@sounds.SoundAssets()`.

## What you are actually taking

| Piece | Portable as-is? |
|---|---|
| handler, page, store, tests | Yes — it is a vertical slice, not a layer |
| the routes it registers | Yes, but **you** choose the path; the recipe lists what each handler expects |
| the embedded assets and their `embed.go` | Yes — each feature serves its own `/static/<feature>/` subtree |
| the capability registry entry | No. It is gogogo's inventory of its own tree; your tree has no equivalent and does not need one |
| the `casbin/policy.csv` row | Only if you adopted route+verb policy. Otherwise the handler's own session check is your answer |
| the SSE/CRDT wiring | Yes, if your client speaks Datastar + the same hub shape; otherwise take the handler and leave the transport |

## The five steps

The mirror of [Removing a component](scope-taxonomy.md#removing-a-component):

1. Export the unit into a scratch directory (`--to`), with `--module` set to
   the module path of the tree you are porting into.
2. Move the files to the repo-relative paths the recipe's table names. Keep
   them: the Go packages, the generated `_templ.go` and the tests all assume
   them.
3. Rewrite the import prefix. Every copied file that still imports
   `github.com/calionauta/gogogo` must import your module instead — the recipe
   lists them, because it measured them rather than guessing.
4. Wire it by hand: mount the handler's router where you want it, add the
   sidebar entry (`internal/ui/modules.go`), and — if the handler reads or writes user data — check the
   session **in the handler**. That last one is not optional: gogogo
   deploys with `RBAC_ENABLED=true` (development starts open), but the policy
   file only governs routes — the handler's own session/ownership check is the
   real gate on user data either way.
5. Prove it with **your** gates: `go build ./...`, then your test suite. The
   template's own tests for the unit assume template fixtures; port the ones
   that assert behaviour you keep, drop the ones that assert the template's
   mux.

## What no recipe can know

The export is a checklist, not a patch, for four reasons that are all yours:
your router's shape, your auth and session model, your layouts, and whether
your codes live in the same table. `add` is mechanical because it edits anchors
it wrote itself; nothing in your tree was written by this tool, so the wiring
stays a decision.

If the unit you want is not a portable slice — a plugin that rewires the
composition root (`dagnats`, `goakt`), or the auth stack — the honest answer is
to scaffold with the units you want and move your own code into that tree
instead. That inverts the work: you keep gogogo's wiring and port your
handlers, which is the direction the tooling actually supports.

## Related

- [Upgrading](upgrading.md) — the same "list to apply deliberately" shape, for a
  tree that *was* scaffolded.
- [Scope taxonomy](scope-taxonomy.md#removing-a-component) — the reverse
  checklist, and which units are safe to delete in the first place.
- [Unit contracts](unit-contracts.md) — what each unit owns, its routes and its
  store calls, generated from the registry rather than hand-written.
- [CLI](cli.md) — `export`'s flags and the agent contract.
