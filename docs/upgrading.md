# Upgrading a scaffolded project

A project scaffolded from gogogo is a **copy you own**, not a fork you track.
That is the point of not being a framework — and it is also the reason nothing
tells you when upstream moved on.

Two read-only commands close that gap. Neither one changes anything, and
neither runs on its own: there is no background check, no nag, no telemetry.

## What is recorded

Every scaffolded `AGENTS.md` carries the base revision:

```
scaffolded-from: calionauta/gogogo@v0.53.0
template-commit: 0123456789abcdef0123456789abcdef01234567
```

That is the only datum the comparison needs, and it is the same one every
scaffolder that supports updates keeps (copier's `.copier-answers.yml`,
cruft's `.cruft.json`). Trees scaffolded before it existed simply report
`unknown` — they are never assumed to be current.

## `--check-update`: is anything behind?

```bash
gogogo --check-update --dir .
```

It compares **two halves**, because they drift independently:

| Half | Question | Source |
|---|---|---|
| binary | is my installed tool current? | the binary's release tag |
| project | is this tree's base current? | `scaffolded-from:` above |

Comparing only the binary is the trap: update the tool and a months-old tree
reports as current. Passing `--dir` compares the project too.

**Exit codes:** `0` nothing to do (including "could not check" — a read-only
advisory must not fail a pipeline when the network is down), `3` an update
exists, `1` usage error. `3` rather than `2` because this repo already spends
`2` on "the proof build failed".

`--format json` is the machine shape: `installed`, `latest`, `binaryStatus`,
`tree`, `treeRepo`, `treeCommit`, `treeLatest`, `treeStatus`, `source`,
`checkedAt`, `note`, `error`. Statuses are `behind`, `current`, `ahead`,
`unknown` — `ahead` is real (a branch build) and must not read as "available".

**A fork is compared against the fork.** The tree half resolves the latest
release of the repo the marker names, not upstream's, so a project scaffolded
from your own fork is judged against your fork. If that fork has no releases,
the verdict is `unknown` with a `note` naming it — never `current`.

The answer is cached for 24 hours in the OS cache directory, and a stale entry
is still used when the network is gone (reported as `source: cache-stale`). That
cache is also why the check needs no credential: one anonymous request per
machine per day sits far under GitHub's 60/hour ceiling.

## `migrate`: what actually changed?

```bash
gogogo migrate --dir .
```

It reads the base from the marker, resolves the latest release, and prints
every release in between — newest first, one bullet per change — then the gates
that prove the result:

```
gogogo: 0.50.0 → 0.53.0 — 3 release(s), 14 change(s), from calionauta/gogogo@v0.53.0
...
prove the result with:
  gogogo --check --dir .
  make check-scope && make check-docs
  make ci-local-fast
```

The list comes from `CHANGELOG.md` at the **pinned tag**, which release-please
already generates. A curated migration guide would be a second document to
maintain and the first one to rot; the changelog is correct by construction.

When the range contains a release that changed structure rather than adding a
feature, the report names the curated note for it. `migrate` reads
`docs/migrations/index.md` at the tag being migrated to and prints the notes that
fall inside the range, which is the difference between a bullet that says a layer
was replaced and the mapping that says what replaced it. `--notes PATH` reads the
index from your own copy when you are offline.

Flags: `--from TAG` overrides the recorded base, `--to TAG` the latest release,
`--changelog PATH` a local file instead of the network (offline, or a clone you
already have), `--format json` for the machine shape. Pass `--to` as well when
you are offline: the default target is resolved from the releases API.

## There is no `migrate --apply`, on purpose

Overwriting generated files clobbers your work. Merging them needs a common
ancestor that a renamed, trimmed copy does not have — which is why
[cruft](https://cruft.github.io/cruft/) users report abandoning updates, and
why copier has to exclude deleted paths by hand.

What works instead is the shape this repo already has: **a list to apply
deliberately, plus gates that prove the result.** The tree's own `make` targets
are the conformance check — `check-scope`, `check-docs`, `lint`, tests — and the
project's agent can apply the list and run them.

So the loop is:

1. Read `migrate` output.
2. Apply what applies to your tree — by hand, or with your agent.
3. Run the gates.
4. Update `scaffolded-from:` to the revision you migrated to.

**Step 4 is yours, not the tool's.** Nothing in `migrate` rewrites the marker:
scaffolding is the only moment the installer knows which revision it is writing
down, and a command that edits provenance on request would let a project claim a
base it never ran. The failure is cheap and self-reporting — `--check-update`
keeps saying `behind` until you bump the line, and `--check` prints the base it
read on every run.

## What upstream will never do

| Not done | Why |
|---|---|
| A background version check | It would make an offline run, a CI runner and an agent session behave differently — and every "phones home" default has to be defended (npm's `update-notifier` needs four opt-outs; `gh` shipped telemetry by default in 2026 and spent the backlash) |
| Auto-update | The tool may have been installed by `install.sh`, by `go install`, or by a package manager; nobody can safely replace all three. Notify and delegate, never self-replace |
| Fetching docs to inject as instructions | Runtime-fetched docs become part of an agent's instructions and cannot be pinned (OWASP AST05). Docs ship pinned with the tree; only the changelog is fetched, and only as data |
| Merging template changes into your tree | You own every line. A migration you did not read is a regression you did not review |
| Rewriting `scaffolded-from:` for you | Provenance must record what actually happened. A tool that bumps the marker on request lets a tree claim a base it never ran — see step 4 above |
| Comparing versions inside `--check` | `--check` answers "does this tree match the manifest?", which must work with no network and no cache: it is the gate CI and a trimmed tree rely on. Version comparison is `--check-update` and `migrate`, both explicit |

## Related

- [CLI](cli.md) — the same engine as scriptable commands.
- [MCP](mcp.md) — `version_status` exposes the check without a shell.
- [Scope taxonomy](scope-taxonomy.md) — which units are safe to delete in the first place.
- [Local CI](local-ci.md) — the gates the migration loop ends with.
