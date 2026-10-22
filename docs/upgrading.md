# Upgrading a scaffolded project

A project scaffolded from gogogo is a **copy you own**, not a fork you track.
That is the point of not being a framework — and it is also the reason nothing
tells you when upstream moved on.

Two read-only commands close that gap. Neither one changes anything, and
neither runs on its own: there is no background check, no nag, no telemetry.

## Which command when

| The question | Command | Changes anything? |
|---|---|---|
| Is the tool — or this tree — behind? | `gogogo --check-update --dir .` | No. **Exit 3 means "yes"**: a verdict, not a failure |
| What changed since my base? | `gogogo migrate --dir .` | No |
| Does the tree still match the unit manifest? | `gogogo --check --dir .` | No |
| Which command, for my goal? | `gogogo advise --intent update\|add-unit\|new-feature --dir .` | No — it prints the ordered route for this tree |
| Restore a unit I trimmed | `gogogo add <unit> --from <template> --dir .` | Yes, with `--yes` |
| My app already exists; I want one piece | `gogogo export <unit> --dir <template> --to <scratch>` | Only the scratch directory |

The last row is the case this page does not cover, because it is not an
upgrade: a tree that was never scaffolded has no `scaffolded-from:` marker, so
there is no base to be behind. See
[Porting into an existing app](porting-into-an-existing-app.md).

Exit codes are part of the contract: `--check-update` returns **3** when an
update exists and **0** for everything else, including "could not check" (a
read-only advisory must not fail a pipeline when the network is down). An agent
driving a shell must read the verdict, not the status.

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

## When a release changes structure

A changelog bullet is enough for a rename or a new flag. It is not enough when a
release replaces a layer, because the bullet names the outcome while the work is
the mapping. The release that removed the framework layer is the example: one
line in `CHANGELOG.md`, a rewritten data layer in practice.

Nothing here merges that for you, but the diff is the source of truth and you
already have access to it:

```bash
git clone https://github.com/calionauta/gogogo /tmp/upstream   # or reuse a clone
git -C /tmp/upstream log --oneline v0.43.0..v0.44.0            # the commits, oldest first
git -C /tmp/upstream diff v0.43.0..v0.44.0 -- internal/ features/ cmd/
```

Apply in commit order rather than as one patch, since a layer swap usually moves a
file before changing it, and run the gates after each step. Then bump
`scaffolded-from:` to the tag you landed on. Where the diff and your tree
disagree about a file you edited, your tree wins: the goal is a tree that passes
the gates, not an upstream copy.

## There is no `migrate --apply`, on purpose

Overwriting generated files clobbers your work, and a three-way merge needs two
things a renamed copy does not keep: paths that still line up, and a base that
still applies. The scaffold rewrites the module path across the tree and renames
every file that carries the project name, so a merge against upstream conflicts
from the first hunk. A `--depth 1` clone also starts one commit deep, so there is
no shared base until you unshallow it.

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

## The installer is the one framework-shaped piece, and only at build time

`trim`, `add` and `new` act on markers in the tree — the call lines in
`cmd/web/main.go`, the route lines in `internal/server/chi.go`, the navbar links,
the `// SCOPE:` annotations. A release that moves wiring is therefore a
compatibility event for your installer even when the running app is untouched,
and the failure is mechanical rather than silent: a strip whose marker is gone
records a miss in the receipt instead of corrupting the file.

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
