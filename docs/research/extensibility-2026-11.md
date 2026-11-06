# Extensibility: core plus installable extensions, and a community registry

Reviewed: 2026-11-01. Status: proposal — nothing here is implemented; the spike
plan at the end is the gate before any of it lands.

This page answers one question: **can gogogo become a core plus a set of
installable, uninstallable extensions — with a registry and community-authored
extensions — without losing the one thing that makes it gogogo (a single Go
binary, one flat stack, no framework)?**

It records what exists today, what the 2026 state of the art in Go extension
mechanisms actually is, which mechanism fits this project and why, what the
migration would cost, and the ordered set of spikes that can falsify the idea.
Read this before proposing any extension architecture of your own.

## Verdict in one paragraph — and the correction this revision makes

The runtime loader question is settled and not in our favour: Go has no stable
ABI, no unload, and `plugin` (buildmode=plugin) is a well-documented trap that
Caddy rejected twice. The winning shape is **compile-time composition with
explicit registration** — Caddy's model. gogogo already has that infrastructure
(`internal/capabilities`, `MountFeature`, and the `trim`/`add`/`export`
machinery), but its install mechanism reaches the wrong conclusion for a
community: it patches the core by **string anchors inside our own source files**,
safe for one pinned template and combinatorially unsafe for N third-party authors.

**But everything written above still assumed the wrong product.** The earlier
revisions of this page treated a gogogo extension as a *pinned package* — the
pi/bb/Dark-Harness shape, a dependency you install and do not touch. That is not
what a gogogo extension is. A gogogo extension is a **functional recipe**: a
working slice — routes, a page, tables, a queue job, a policy row — copied into
the user's repository, carrying a receipt that says exactly what was written and
where it came from, and **intended to be modified immediately afterwards**. The
user is not installing a dependency. They are installing a competent starting
point they will own.

That one difference rewrites the architecture, because the two models fail in
different directions:

| | pinned package | function, evolving recipe |
|---|---|---|
| Unit of install | a module ID at a version | a set of files, a baseline, a receipt |
| User's edits | not expected, often overwritten | **the entire point** |
| Compatibility risk | breaks at compile time | **breaks at merge time** |
| What must be frozen | the API surface | **the file set and its provenance** |
| Update story | bump a version | replay "what I changed" over "what upstream changed" |
| Failure mode | surfaces stall | **forks rot silently** |

So the corrected thesis, in one sentence: **gogogo extensions are versioned
templates copied into a repo, not dependencies resolved into a build.** The
architecture question is therefore not "how do we load a plugin" (already
answered: we don't) but "**how do we install code that is meant to be edited, and
keep it honest over time**". Everything in this page after this section is
written against that question.

One consequence has to be stated before the rest, because it is where the recipe
model genuinely breaks down: **a copied file can be diffed and re-merged; a copied
migration cannot.** Migrations are append-only and immutable once applied, and two
owners of one migration history is a production-data bug, not a merge conflict.
So the recipe model must be **split**: the code is copied and owned by the user;
the schema, routes and job types are **declared** with stable IDs and applied by an
engine. That split is this page's central architectural recommendation, and the
reason it is safe is the evidence in the section on provenance and drift.

The distribution problem the Node comparison raises is already solved here in a
way JS cannot match: nothing is resolved at run time, the artifact is the
distribution unit, and a deploy that pulls an image by digest never needs a
toolchain on the target.

## The starting position is better than it looks

Before designing anything, know what already exists. This is not a greenfield.

| Piece | Where | What it does |
|---|---|---|
| Unit registry | `internal/capabilities/registry.go` and its siblings | Every removable-or-toggleable unit: id, removal kind, scaffold class, owned dirs/files, dependencies, runtime opt-out, warnings, notes |
| Removal machinery | `internal/installer/manifest.go` | `planTrim` turns a keep set into a drop set; `applyTrim` deletes paths, rewires imports, tidies `go.mod` |
| Install machinery | `internal/installer/add_spans.go` | Reinverts a trim: copies directories, re-inserts wiring spans at string anchors, rebases the module path |
| Scaffold command | `internal/installer/newcmd.go` | Writes a handler, a page, the composition wiring, the registry entry and the policy rows, then proves the build |
| Port tool | `internal/installer/export.go` | Bundles one unit into a scratch module with a recipe for transplanting it |
| Routing seam | `internal/server/route.go` | `MountFeature` walks a capability's own router and flattens it onto the root tree; a duplicate method+pattern panics at boot naming both owners |
| Composition root | `internal/server/chi.go` | One handler per capability, one mount line each; optional surfaces mount behind nil checks |
| Navigation | `internal/ui/modules.go` | Every destination is a `Module` value; the shell renders what the slice returns |
| Migrations | `internal/dbschema/migrate.go` | Embedded SQL, `schema_migrations(version TEXT PRIMARY KEY)` |
| Authorization | `internal/rbac/casbin/policy.csv` | Route plus verb policy, with a coverage test that walks the real mux |
| Cross-feature guard | `cmd/check-flatdeps/main.go` | No feature imports another feature's domain; a closed allowlist of shared UI leaves |

Two properties of this list matter more than they look:

- **`MountFeature` is already a plugin seam.** It takes a router and its
  absolute paths, and it enforces collision detection. Nothing about the router
  is hardcoded to a fixed set of features — the *list* is hardcoded, not the
  mechanism.
- **The safety nets are already generic.** The SCOPE annotation check, the RBAC
  coverage test, the flat-dependency guard and the registry conformance test all
  iterate over whatever the registry says. A new unit needs no new gate.

But one property is missing, and it is the blocker.

## The blocker: install patches the core by string anchors

`internal/installer/add_spans.go` inserts code by finding literal markers inside
files of the core:

| Anchor | File edited |
|---|---|
| `\tdefer stopLifecycle()` | `cmd/web/main.go` |
| `\t_ = components.Layout(` | `features/todo/components/todo_list.templ` |
| `\treturn components.TodoListRegion(signals)` | `features/todo/components/todo_list.templ` |
| `>6. Finalize</li>` | `features/todo/components/todo_list.templ` |

With a single official template, a pinned release, a drift gate and a test that
asserts every substr still matches the live file, this is reliable. It is
reliable *because the distance between the anchor and its file is zero by
construction*.

With community authorship, that distance is no longer zero. Every extension
authors their span against some version of the core. When the core moves a line,
the insertion either fails loudly (good) or lands in the wrong place (bad, and
the review risk is the payload). The maintenance cost of the anchor table does
not grow linearly with authors — it grows with authors × core revisions.

This is the finding that reorders the whole plan: **the anchor table is not an
implementation detail of `add`, it is the thing that makes third-party extension
distribution impossible.** Any proposal that keeps anchor-patching and adds a
registry on top is decoration.

Second, smaller blocker: Go's `internal/` is not importable by another module,
so today there is genuinely nothing for a third-party extension to compile
against. There is no public extension surface to write.

## The taxonomy problem: two axes, not one

The question "are the current plugins core, and are the features extensions?" has
a clean answer, and it is that the question conflates two axes with different
owners.

| Axis | Values | Who decides | Answers |
|---|---|---|---|
| Removal class | `core` / `plugin` / `feature` | **The project** | If I delete this, what disappears? |
| Extension class | shipped / installable | **The project, per release** | Is this in the official catalog, or something an operator may add? |
| Opinionatedness | opinionated / replaceable | **The operator** | May I edit this in my checkout and keep my edits? |

The third axis is the one this page originally missed, and it is the answer to
"can someone using gogogo alter the core?" — **yes, and no mechanism is needed
for it**, because editing a checkout already works. What was missing is not a
capability but a *guarantee*: today nothing tells an operator which files are
safe to edit. The extension model should state it instead of enforcing it.

The predicate the project answers per release is deliberately not "updateable
or not". A browser cannot be edited; the point of a personal fork is that it
can. So every unit carries an opinionatedness note, and the honest split is:

| | meaning | example |
|---|---|---|
| **Opinionated** | edit freely; upgrading is your problem, and the tooling cannot help | `todo`'s list markup, the landing page copy, the theme catalog |
| **Replaceable** | consumed through an interface, so a private implementation survives an upgrade | an LLM provider, a delivery channel |

A shared unit cannot be simultaneously opinionated and upgrade-safe, because
"the official version" and "my version" would both claim the same paths. This is
the structural reason the official boundary is worth drawing, and the reason the
previous revision's answer to this question was incomplete.

### Consequence: the plugin class collapses for installed units

A unit whose dependency is installed in the binary needs no removal class at all
— deleting it would not compile. So one packaging fact has two expressions
depending on installation, and the registry should describe both rather than
keep two truths:

| Installation | Packaging fact | The `core`/`plugin`/`feature` distinction |
|---|---|---|
| **In the binary** (official default set) | Core | **Collapses.** Editing is free; removing is a fork. There is no supported removal, so the class carries no mechanism |
| **In the catalog, not installed** | Extension | **Applies.** The catalog needs a dependency field, because a shared unit declares what it serves |

This is the direct answer to "would the current plugins become core?" — **yes,
for the official default set**, and that is the right call. The registry does not
lose information: the dependency field (`DependsOn`) already exists and keeps
telling an installed extension what it serves and needs. What goes away is the
*expectation of a supported removal* for installed units.

The honest cost, and it must be written into the adopting PR: `trim` as an
official workflow ends. Dropping a capability from a checkout becomes an
unsupported fork (deleting a directory whose importers remain does not
compile). In exchange the surface gets real: a core that never moves, a catalog
that grows, and a seam that carries community code without patching core files.

### The classification as this repository actually stands

Not guesswork — read from the registry table. What follows is a proposal to
debate, not a decision.

**Must be core.** These are the boot contract; if any becomes optional, an
install becomes a fork and "official gogogo" stops being a product. Note the
class column is the *removal* class of today and is expected to collapse for
this group once the unit is in the binary.

- `appconfig`, `secrets` — configuration and its decryption. Nothing loads first.
- `server`, `instance`, `hostgate` — the boot path, the single-instance lock,
  host capacity gating.
- `database` — the schema, migrations, generated queries.
- `auth`, `rbac` — sessions and policy. The security invariants are written
  against these; making them removable makes every invariant conditional.
- `shell` — the one app shell every page renders inside.
- `observability` — the request-scoped log path the security rules require.
- `app`, `demo-seed` — the application value and its seed.
- `capabilities`, `components` — the registry itself, and the shared UI leaves
  that `check-flatdeps` allows across features.

**Natural extension candidates.** User-facing surface, own namespace, nothing in
the core depends on them: `todo`, `chat`, `review`, `room`, `whiteboard`,
`notes`, `credits`, `landing`, `config-view`, `account`, `notifications`,
`search`, `genui`, `license`, `license-activation`, `demos`.

The catalog splits these two ways, and the split is a product decision per
release rather than a property of the code:

- **Ships in the default set** (installed in every official binary, still
  described by a catalog entry so the wiring stays mechanical): the reference
  slice and whatever the first-run experience depends on.
- **Shipping-off extension** (in the catalog, one command away from installed):
  `review`, `room`, `whiteboard`, `credits` and the operator surfaces are the
  obvious candidates — a fresh project does not need a GoAkt grain or a billing
  ledger.

**Wired libraries, core for installed units.** `nats`, `dagnats`, `goakt`,
`genkit`, `llm`, `collab`, `datastar`, `crudwire`, `offline-sync`, `delivery`,
`perf`. These are third-party packages the core *wires*; they stay in the core
namespace in the extension sense too, because `BootChi` names them. They remain
*installable-as-dependency* for an extension that needs them (`go get` does the
work), which is a different question from whether they are a catalog entry.

A unit that owns loose files inside a core package cannot be a self-contained
extension. `whiteboard` is the proof: it carries `collab` and `notes` and the
shared `internal/server/presence.go`, so it stays installed in the binary until
that file becomes an owned seam with a nil-safe registration. The rule this
generalises to: **an extension owns its directories, and optionally files it
owns outright — never a span inside a core file.**

**The uncomfortable one.** `todo` is the reference implementation that every
other feature is copy-pasted from, and it appears in every proof, smoke and
fixture. Making it an extension is architecturally desirable and practically
expensive. This is a real tradeoff, not a formality — see the open questions.

## Mechanism survey (2026)

Verdicts below follow from one constraint: Go has no stable C-compatible ABI for
its own types, no runtime unload, and no cross-toolchain dynamic linking. Every
mechanism is a workaround for that fact, and the workaround chosen decides the
cost.

### stdlib `plugin` (buildmode=plugin) — do not use

Linux/BSD/macOS only, unsupported by the race detector, and crashes unless every
part of the program is built with the identical toolchain, build tags and flags.
Plugins cannot be unloaded. This has not improved: Caddy rejected it in 2022 and
again in 2026 as a contributed PR, with the maintainer calling it too brittle to
improve the user experience; and Go 1.27 had a regression where loading multiple
plugins corrupted stdlib itabs. The caveat set is the feature.

- <https://pkg.go.dev/plugin>
- <https://github.com/golang/go/issues/20461>
- <https://github.com/caddyserver/caddy/issues/5183>
- <https://github.com/caddyserver/caddy/pull/7488>
- <https://github.com/golang/go/issues/81303>

### Compile-time modules plus a generated wiring step — **works today, recommended**

A module is an ordinary Go package that registers itself; a generated main imports
the modules the operator selected. Zero runtime overhead, full type safety, no
unload problem because there is nothing to unload. This is what Caddy does, and
Caddy is the only Go project that has actually run a community extension
registry at scale.

Two lessons from Caddy's own history are load-bearing and both are about what
*not* to build:

1. Their hosted build server (`add-package` / `upgrade` proxied through a
   caddyserver.com machine) was removed from core in 2025 for centralizing
   distribution, encouraging mutable infrastructure and lacking attestation or
   reproducibility. The local builder is the surviving path.
2. The cost of the model is the restart: every install is a rebuild. That is
   acceptable for infrastructure and awkward for an app that wants an install
   button.

- <https://caddyserver.com/docs/extending-caddy>
- <https://github.com/caddyserver/xcaddy>
- <https://github.com/caddyserver/caddy/issues/7010>

### hashicorp/go-plugin (subprocess plus gRPC) — works, with caveats

The host spawns the extension as a child process and talks gRPC over stdio, with
protocol version negotiation built in. Extensions are ordinary binaries compiled
independently, so toolshed mismatch is not a failure mode and "uninstall" is
killing a process. This is what Terraform, Vault, Nomad and Grafana's entire
backend plugin system use.

The costs are real: a process per extension with supervision and its own memory;
a serialization boundary at every call; and a coarse-grained seam, since routing
fine-grained work through it turns local calls into IPC. Measured local-IPC
per-call cost lands in the microseconds (Unix socket cheaper than HTTP loopback),
which is irrelevant for a request handler and ruinous for a per-row render loop.

This is the only mechanism that delivers **install without rebuild**. That is
exactly the requirement a self-hosted template does not have, because its
operator already has the Go toolchain.

- <https://github.com/hashicorp/go-plugin>
- <https://grafana.com/developers/plugin-tools/key-concepts/backend-plugins>
- <https://github.com/go-gitea/gitea/issues/20126>

### WASM embedding (wazero / WASI) — works, with caveats; only for an untrusted registry

The only genuine sandbox in the list. WASI is capability-based — a module starts
with no ambient authority and the host grants paths and hosts explicitly — and
wazero is pure Go, so it does not break cross-compilation. Production users
include Cilium, Trivy, k6 and OPA.

The tax is heavy for this project: per-call overhead in the same microseconds
range as IPC (you pay microservice latency without the process), extension
authors must compile to the `wasip1` target where Go's standard library is
degraded and no `wasip2` target exists upstream, and the friendly Go SDK on top
(extism-go) is stale and pinned to an older wazero. Choose this only if the
registry is **untrusted by design**. It is not: in this model the operator
installs what they choose.

- <https://github.com/wazero/wazero>
- <https://wasi.dev/security>
- <https://github.com/golang/go/issues/65333>

### Embedded scripting (goja / Starlark / Lua) — works, with caveats: trusted code only

Viable for small customization hooks — a rule, a transform, a template snippet —
and hosted in production by PocketBase, which had to solve the obvious problem by
serializing each handler into its own isolated runtime because one JS runtime
cannot serve concurrent requests. The honest limit: the script runs in our
process with our memory and our goroutines. There is no boundary. Any "sandbox"
bolted on afterwards is an AST blocklist, not an enforcement point.

Yaegi (Go as script) is a separate and worse case: original maintainers departed,
a large open issue backlog, removal proposed by a major distribution, and a
benchmark position an order of magnitude behind goja. Do not use it.

- <https://pocketbase.io/docs/js-overview/>
- <https://github.com/traefik/yaegi/discussions/1699>

### Out-of-process HTTP or webhook plus a manifest — works, wrong shape here

Document a manifest and POST to a URL. This is what Gitea actually shipped
instead of a plugin system, and it is correct for *integration* extensions. It is
the wrong shape for an in-app feature, because every extension becomes a deployed
service with its own uptime, secrets and auth.

### Summary table

| Mechanism | Verdict for this project | Deciding reason |
|---|---|---|
| Compile-time modules plus generated wiring | **Works today — recommended** | No overhead, type-safe, no unload problem; the only model with a real Go precedent |
| hashicorp/go-plugin subprocess | Works, with caveats | Only path to install-without-rebuild; costs a process, IPC and a supervisor |
| wazero plus WASI | Works, with caveats | Only real sandbox; costs a second toolchain and per-call latency we do not need |
| goja / Starlark hooks | Works, with caveats | Fine for small hooks; no isolation whatsoever |
| Out-of-process HTTP hook | Works | Right for integrations, wrong for in-app features |
| stdlib `plugin` | **Do not use** | Toolchain-identical requirement, no unload, active regressions |
| Yaegi | **Do not use** | Unmaintained, slow, removal proposed downstream |

## Precedents: what an agent-ecosystem package model actually looks like

All three of the projects named in the original question are JS/npm and all three
accept full process trust. They are calibration for *shape*, not templates for
*mechanism* — and the JS distribution model is exactly what must not be copied,
for the reason in the next subsection.

What is worth taking from each, stated as a mechanism rather than an impression:

| Precedent | Take | Explicitly do not take |
|---|---|---|
| pi.dev | a package is a manifest declaring every surface it contributes (extensions, skills, prompts, themes) under one ID; sources are pinned specs; project packages load only after the operator trusts the project | npm as the transport, and "runs inside the process" as an implied safety story |
| bb | the marketplace is a **git repository of manifests**; one repo holds many plugins; the exact source is shown before install; the default catalog is human-reviewed by pull request; bridges ship as **content-addressed artifacts verified by digest** before caching | a second package registry |
| DeepSeek Harness | **profiles as ordered stacks of layers** — a config is a composition, not a fork — and "everything is a plugin" as the composition primitive | a foundation-model vendor's plugin ABI as the model |
| Caddy | compile-time modules, one registration call per module, a builder that generates the main module | a hosted build server (they removed theirs) |
| VS Code | extensions in a **separate process**, which is how it gets a real sandbox | the process model itself — priced in the mechanism survey, not needed here |

### The Go answer to the JS distribution problem

The thing to avoid is not JavaScript the language; it is the **Node model**: an
interpreter must be present, packages are resolved at runtime from a mutable tree,
and the install is a copy into a directory. The matching Go model is the opposite
in every dimension, and it needs nothing that is not already in the toolchain:

| Node property | Go counterpart | Consequence here |
|---|---|---|
| interpreter required at run time | one static binary, no runtime | an operator runs a container; nothing to install |
| packages resolved at run time | module graph resolved at **build** time, pinned in `go.mod`/`go.sum` | "installed" is a property of the artifact, not of a directory |
| mutable global tree | per-project module cache + checksum database | two projects cannot silently diverge |
| install = copy files | install = compile one artifact | needs a builder somewhere — see below |
| `node_modules` drift | build is reproducible from `go.sum` | rollback is re-running a previous artifact |

**Who compiles is the only real question the JS comparison raises, and it already
has three good answers here:**

| Model | Who compiles | Already exists | Extension policy applies at |
|---|---|---|---|
| **Official image** | CI compiles with the official default set | published by digest; the server pulls it | vendoring time — the image is the same everywhere |
| **Custom image** | CI of the operator's fork compiles their set | the deploy workflow already pulls by digest, and building locally is the documented fallback | **build** time — the policy is ordinary code at the composition root |
| **Dev loop** | the operator, locally, from the module cache | Air reload uses the Go toolchain | build time |

That table is the reason this project can have an official catalog and
community extensions without a Node-style runtime and without a build service:
**the artifact is the distribution unit, and the catalog decides what goes into
it.** Nothing is resolved at run time, so nothing can drift on a running host.

- **pi.dev** — a package is an ordinary directory or npm package whose manifest
  declares extensions, skills, prompts and themes, discovered by a keyword.
  Install sources are npm specs, git refs and paths, and versions are pinned.
  Project packages load only after project trust. The security model is stated
  bluntly rather than implied: an extension runs inside the host process with the
  same operating-system permissions.
- **bb** — an extensions page plus marketplaces that are git repositories of
  manifests, where one repository can hold many plugins, the exact source is
  shown before install, the default marketplace is human-reviewed by pull
  request, and bridges ship as content-addressed artifacts whose digest is
  verified before caching.
- **DeepSeek Harness** — "everything is a plugin" over a component framework,
  with profiles as ordered stacks of patch layers. This is a configuration
  composition model, not a sandbox.

The calibration that matters: **VS Code, the one system with a real sandbox, gets
it by making extensions a separate process.** WordPress never built one and has
paid for it since. Sandboxing is a process, VM or WASM decision made at
architecture time — it cannot be added later as a policy layer.

- <https://pi.dev/docs/latest/packages>
- <https://pi.dev/docs/latest/extensions>
- <https://getbb.app/changelog>
- <https://code.visualstudio.com/api/advanced-topics/extension-host>

## Registry: do not build a second Go ecosystem

Go already has the best supply chain in the industry: module proxy, `go.sum`,
and the checksum database as a transparency log. For provenance above that, SLSA
level 3 with sigstore for Go modules via GitHub Actions is documented and
supported. Grafana gates its catalog on signing by default, which is the
strongest precedent for a curated tier.

What a manifest repository actually needs to work:

| Field | Why |
|---|---|
| `id` | Namespaced, stable, and the prefix of every route, table and job type the extension owns. Flat names collide and get squatted; that is why npm scopes exist |
| `module` path plus version | What `go get` resolves |
| `digest` | What the install verifies before caching (the bb model) |
| `minCore` | The oldest core this extension compiles against |
| declared surface | Routes, tables, job types, nav entries, policy rows — the install can then prove collisions *before* mutating anything |
| signature | Curated tier only; unpinned community tier relies on `go.sum` |

A git repository of such manifests plus `go.sum` plus a digest is sufficient and
correct for a first version. Anything more — a self-hosted checksum log, custom
signing infrastructure — is a second Go ecosystem with a fraction of the
auditors.

The failure mode to avoid is Caddy's: a hosted build service that silently
becomes CI infrastructure with no SLA. **Local build, no build server.**

- <https://go.dev/ref/mod>
- <https://github.blog/security/supply-chain-security/slsa-3-compliance-with-github-actions/>
- <https://grafana.com/docs/grafana/latest/administration/plugin-management/plugin-sign/>
- <https://docs.brew.sh/Tap-Trust>

## The part nobody solves: uninstall with schema and jobs

No 2026 system does reversible migrations, and the ones that tried converged on
the same answer: declare schema, tag provenance, **never auto-drop**.

- Shopware states it flatly: no migration rollback; cleaning the database on
  removal is the plugin's own uninstall responsibility.
- Nextly has the cleanest published design: plugins declare schema, every entity
  carries a source tag, uninstall leaves tables orphaned rather than dropped, and
  a separate prune command reviews them. Uninstall refuses when another enabled
  plugin depends on the object, or when a module has no reversible down step.
- The wider consensus on down-migrations is that they are a comfort rather than a
  safety net; the discipline is expand/contract with destructive drops deferred
  to an explicit later step.

The rules that follow, and that any spike must respect:

1. **Every database object is namespaced by extension id.** Tables, indexes and
   any supporting object carry the extension's prefix. Migrations live in the
   extension's own embedded filesystem and land in `schema_migrations` under the
   extension's version prefix — the ledger's primary key already supports this
   without a schema change.
2. **An extension never owns a column on a core table.** If it needs extra data
   about a core entity, it owns a side table keyed by the core entity's id.
3. **Uninstall is non-destructive by default.** Remove wiring, remove the
   extension from the manifest, drain its job queue, and leave the data. A
   separate, explicit purge is the only thing that drops.
4. **Uninstall refuses when a dependent extension is installed.**
5. **Job types are namespaced and drained, never deleted mid-flight.** A job type
   without a registered handler is already handled: the queue's not-found handler
   surfaces it instead of silently dropping it.

- <https://developer.shopware.com/docs/guides/plugins/plugins/database/database-migrations.html>
- <https://nextlyhq.com/docs/plugins/schema>

## The recipe model: what the evidence says, and where it breaks

This is the section that decides the architecture, so it carries the research rather
than summarizing it away. gogogo's extension model has more in common with
shadcn/ui's registry, `copier update` and Debian's patch queue than with npm — and
all three have documented limits that must be designed around rather than
discovered later.

### Precedent: shadcn/ui is the closest thing to a recipe registry, and it has no update story

A shadcn registry is plain JSON over HTTP: a catalog file plus one item per entry
carrying a type, a file list with source and target paths, npm dependencies, and
`registryDependencies` (other items). The item's source carries **no version**. The
CLI fetches, resolves dependencies recursively, rewrites imports to the project's
aliases, installs dependencies and writes files. Building your own registry is a
documented, supported path — a `registry.json` at a GitHub repo root is enough, with
private repos supported via token and namespacing (`@acme/x`) added later. A
Go/Templ port already exists, which is the closest prior art to this project.

The failure that matters: **there is no update command.** The CLI has `add`, which
prompts on an existing file, silently skips byte-identical content and replaces
wholesale on `--overwrite` — no diff, no merge. `--diff` is a **two-way** diff, so it
cannot distinguish the user's edits from upstream's. The state file records style,
aliases and registries — **not which components were installed, nor at which
version**, which is precisely why a real three-way merge is impossible. The feature
request for an update command has been open for years, and the maintainer position
is stated plainly: once installed, the code is yours and updating it is your
responsibility.

The honest counter-argument is equally important, and it is why the model still
makes sense here: most copied files never need updating, because what actually
moves is the layer below (the primitives and the CSS framework), and that arrives
through the package manager. Criticism of copy-paste registries concentrates on
styling wrappers, where a stale file is cosmetic.

> **But for gogogo the criticism lands fully.** A stale shadcn button looks slightly
> off; a stale recipe that owns routes, jobs and tables is a correctness and
> security defect. So the recipe model can only be adopted *together with* the
> provenance and merge machinery shadcn never shipped. Adopting the format without
> the machinery is exactly the gap users complain about.

Two more transferable details, both traps if ignored:

- `cssVars` merges into shared project state and is unreconciled. That is the same
  failure shape as a recipe writing a migration: an unarbitrated write into a shared
  file.
- `registryDependencies` are **not inherited** — pinning `x@v1.2.0` does not pin what
  `x` pulls in.

### Precedent: three-way merge is only solved by `copier`, and its maintainers are replacing the engine

`copier update` regenerates the *old* template output, diffs it against the project,
applies the new template and re-applies the project's diff. Its documented failure
modes are real, not hypothetical: conflict-context lines default to one where git uses
three (fewer conflicts, but similar blocks relocate); the abort path is broken enough
to need its own documentation section; files the user *deleted* are excluded from
future updates; and `recopy` is an escape hatch that loses all local edits. The
maintainers are rewriting the merge engine around `git merge`, which is the strongest
available signal that file-level three-way merge is not a solved foundation.

`cruft` (cookiecutter's update path) is worse: it patches, falls back to `git apply
--reject` leaving `.rej` files, has silently dropped changes and skipped newly added
template files. `cookiecutter`, `yeoman` and Go's `gonew` have no update path at all.

One mechanism from `copier` is worth taking outright: **`_migrations`, a versioned
hook gate.** A shell command runs only when crossing a version boundary, receiving
from/to version and stage. That is a first-class "when you cross 0.4, rename this
column" hook, and nothing in the shadcn model can express it.

**The ceiling of the model, stated honestly:** a recipe survives user modification at
*file* granularity, one commit at a time, with a human resolving markers. Per-recipe
update is strictly harder than whole-project update, because there are many
independent baselines instead of one.

### What actually works for "I own this, I changed 30%, upstream fixed a bug"

Not a file-level three-way merge. The mature answer is that **upstream's fix arrives as
commits, the user's 30% is commits, and the merge is `git merge` or `rebase` over
both.** Evidence from the systems that do this for a living:

- `git subtree` sets a marker so later pulls can find the prefix; `--squash` erases
  that marker and breaks subsequent pulls. Any locally edited file conflicts.
- The vendor-branch pattern (pristine upstream on its own branch, tagged per import,
  merged into the working line) is manual and honest.
- The patch queue is the mature form: user changes live as a commit series, replayed
  over each upstream import. Debian does this with its patch-queue branches; FreeBSD
  ports keeps patches as separate files against a distfile. Note what the most
  experienced vendors do: **patches stay separate from code.**

So the design guidance is: give each recipe a real repository branch upstream, make the
user's copy a fork with a remote, and let git arbitrate. The tool's job is to make
`merge` feasible — produce a clean baseline — not to replace it.

### Provenance: the one thing shadcn got wrong, and it is cheap to fix

For a later tool to answer "where did this file come from, what version, and which
lines did the user change", three facts must be recorded, in **one committed sidecar
per installed recipe** (`.recipes/<id>.yml`):

1. `upstream:` repo, ref (tag plus sha), item, license.
2. `files:` installed path → **content hash of the pristine upstream content**. This
   is the missing third leg of any three-way merge, and its absence is exactly why
   `--diff` is two-way.
3. `applied_at:` the commit in *the user's* repo.

Hashes, not only a version string: a version cannot tell you which lines the user
changed. Emit `SPDX-License-Identifier` headers so license obligations survive the
copy, following the REUSE file-tag convention plus its sidecar form for files that
cannot carry a comment. Note that Go's `// Code generated ... DO NOT EDIT.` is the
**wrong** signal for a recipe — it means "do not touch", which is the inverse of
the intent; a recipe needs a marker that means "you are expected to own this".

### Where the model breaks: schema, and it breaks hard

This is the finding that reshapes the architecture. **No system lets you copy a
feature-with-schema and keep upgrading it**, because there are two unresolvable owners
of one migration history. Every real system picks one, and the ones that tried to have
both pay in production data:

- Django's `startapp` is one-shot; third-party migrations break with
  `InconsistentMigrationHistory` when upstream squashes or renames. Django Oscar's
  escape is explicit one-way forking, and its upgrade docs admit migrations must be
  copied by hand — and a forked app must fork its parents too.
- Rails gems copy migrations with `skip: true`, giving same-name collisions with no
  reconciliation.
- Laravel's publish hook **rewrites the migration filename timestamp**, so
  republishing renames the file and can re-run schema creation.
- Prisma and the Go migration family treat applied migrations as immutable — editing
  one is an error whose resolution is a reset.

The transferable conclusion, and this page's central recommendation:

> **Split every recipe in two.** The **code** (pages, handlers, components, wiring) is
> copied and owned by the user. The **schema, routes and job types** are *declared*
> with stable IDs and applied by the engine. Never hand the user an editable migration
> file and then claim you can upgrade it — that is the trap that fails by re-running
> or by inconsistent history, and both hit production data.

This split resolves the tension inside the recipe model instead of picking a side: the
part a user most wants to modify (how the feature looks and behaves) is theirs; the part
where modification is structurally unsafe (the append-only history) is declared and
engine-applied. It also makes "uninstall is non-destructive" possible, because the
engine knows which objects belong to which recipe.

### What transfers, in one table

| Element | Verdict for gogogo |
|---|---|
| Registry JSON format (items, file lists, typed entries, resolvable dependencies) | **Adopt.** The Go/Templ port proves the shape works with Go modules and Templ. |
| Copy-on-install with user edits as the normal case | **Adopt**, with the cost disclosed rather than implied. |
| Per-item two-way `--diff` as the update story | **Trap.** It cannot separate the user's edits from upstream's. |
| Provenance sidecar with per-file pristine hashes | **Build it.** Cheap, and it is the difference between an honest model and shadcn's gap. |
| File-level three-way merge engine | **Do not build.** `copier`'s maintainers are quitting theirs; use `git merge`. |
| Versioned hooks (`_migrations`-style) | **Adopt.** The only found mechanism for "crossing version X needs a data fix". |
| Recipes that add tables | **Split** code (copied) from schema (declared, engine-applied). |
| `DO NOT EDIT` generated markers | **Do not use** for recipe files — opposite semantics. |

## Trust: say it plainly

Three permission models exist, and only three.

1. **Trusted in-process** — no boundary. The only mitigations are a trust prompt,
   pinned digests, and review of *versions* (Obsidian's 2026 move was to automate
   review of every version, explicitly not to sandbox).
2. **Separate process** — OS-level limits actually apply; crash isolation and a
   real kill.
3. **WASM** — capability-based with no ambient authority.

Anything else — AST scanning, allowlists of "dangerous APIs", lint rules — is
advisory, not enforcement. The documentation must state which model an extension
gets, in the same blunt register pi uses, rather than implying a safety that does
not exist. For this project, the truthful statement for v1 is: **an extension is
compiled into your binary and runs with your binary's permissions.**

- <https://obsidian.md/blog/future-of-plugins/>
- <https://obsidian.md/help/plugin-security>

## The direction this analysis recommends

Six layers, with the boundary drawn so that installing an extension never edits
core wiring, and so that the *configuration* of a project is a data file rather
than a series of code edits.

Two distinctions carry the whole design and are worth stating before the layers,
because every later decision depends on them:

- **Slots, not hooks.** An extension fills a declared slot. It never inserts code
  into a core handler. "Extension inside an existing route" therefore means *in a
  page's slot*, never *in the route's logic*.
- **Declared at build time, policy at boot, reality in the handler.** Which
  extensions exist is a build fact. Whether one is active is a boot-time policy
  over the installed set. Whether a given user may read a given record is still
decided in the handler, by the owning unit — and none of those three levels may
borrow another's job.

### Layer 0 — core, never an extension

Boot, configuration, database, auth, RBAC, the shell, observability, and the
capability registry. These stay in the binary unconditionally. The reasoning is
not purity: if boot is extensible, every install is a fork, and the official
distribution stops being a product you can reason about or support.

### Layer 1 — a recipe is a file set, a declaration, and a receipt

Layer 1 as this page first wrote it was a *pinned package*: one interface, one module
path, one import line. That is the wrong shape for a gogogo extension, and the
correction is the most important structural change this revision makes. Keep the
registration seam — it is still how declared surfaces reach the router — but an
extension is no longer a module you import. It is **three parts**.

**1. The copied code** — the working slice, in the user's own tree, immediately
editable:

```
features/pomodoro/            ← copied. Yours. Edit it, rename it, gut it.
├── handlers/                 ← pages and endpoints, wired through the seam
├── components/               ← templ components, yours to restyle
├── domain/                   ← the logic, yours to change
└── README.md                 ← the recipe: what this does, what to change first
```

**2. The declaration** — the part that must NOT be copied, because it carries the
append-only facts (schema, route patterns, job types, policy rows). It stays
identified by a stable recipe ID so the engine can apply and later recognise it:

```yaml
# .recipes/pomodoro.yml — the receipt. Committed. Written by install, read by
# update, uninstall and the drift check.
id: pomodoro
upstream:
  repo: github.com/calionauta/gogogo
  ref: v0.56.0
  path: features/pomodoro
  license: MIT
files:                        # installed path → hash of the PRISTINE content.
  features/pomodoro/handlers/handler.go: sha256:...
  features/pomodoro/domain/timer.go:    sha256:...
schema:                       # declared, engine-applied, never user-edited
  - ext_pomodoro_session
  - ext_pomodoro_settings
routes:
  - {method: GET, pattern: /pomodoro}
jobs: [ext:pomodoro:tick]
slots: [dashboard.card]
policy: ["pomodoro, /pomodoro, GET, user"]
applied_at: 9f2c1ab           # a commit in YOUR repo
```

**3. The seam registration** — generated, and now a *pointer* rather than an import
of a module the user does not own:

```go
// internal/server/recipes.gen.go — GENERATED from .recipes/*.yml.
// Registers the declarations. The code is already in the tree; this only tells
// the engine what the code claims.
package server

var recipeDeclarations = []extension.Declaration{
    pomodoro.Declaration,
}
```

The distinction that makes this coherent: **the declaration is the engine's
business, the code is the user's.** That is not a stylistic split — it is the only
split the evidence supports (see the section on where the model breaks). The
declared half can be upgraded, diffed and uninstalled safely because its history is
append-only and identified. The copied half is upgraded by `git merge` over two
branches, with the pristine hashes in the receipt supplying the third leg.

Note what the receipt *enables*, and it is the whole reason for the extra file: with
a hash per file, `update` can say precisely which files the user touched and which
are untouched — untouched files fast-forward silently, touched files go to a real
merge, and a schema declaration that changed is surfaced as an engine upgrade rather
than a merge conflict in a `.sql` file.

### Layer 1b — what the user is told to do first, because recipe means editable

A recipe that is installed and never modified is the failure mode of this model (the
criticism of copy-paste registries is largely about exactly that). So installation
ends with guidance, not a completion message: the recipe's `README.md` names the
three places a user is most likely to want their own version — the copy, the
fields, the flow — and the install output says plainly that the code is now theirs
and that upstream fixes will arrive as a merge, not an update.

This is also where the authoring skill lands (Spike 6): the skill's job is not "how
to install" but "how to be a good owner of a copied recipe" — rename it to what it
means in your product, delete what you do not need, keep the declaration in step
with the code, and record why you diverged in the recipe's own README.

### The registration seam still exists, and it is still worth building

The engine needs *one* place where declarations become routes, nav entries, jobs and
policy rows, and that place must be collision-checking and generated. It is the same
`MountFeature` seam this project already has, driven by declarations instead of
hardcoded calls. So the interface survives — it just stops being the *unit of
distribution*, and becomes the **unit of declaration**:

```go
// pkg/extension — the public surface an extension compiles against.
// Nothing in here may import cmd/ or internal/server; it names contracts only.

type Extension interface {
    ID() string
    Routes() []Route                 // mounted by MountFeature, absolute patterns
    Nav() []NavEntry                 // shell rail and dock entries
    Migrations() fs.FS               // its own embedded SQL, namespaced
    Jobs() map[string]queue.Handler  // types prefixed with the extension id
    Policy() []PolicyRow             // its own policy rows, never the core CSV
    Config() []ConfigField           // rendered by the operator config page
    Panel(ctx) (templ.Component, error) // a slot in the shell: dashboard rail, chat native panel
}
```

`Panel` is the seam that makes a recipe able to *contribute a surface fragment*
rather than only whole routes. It is what a recipe needs to put a card on the
dashboard or a native panel into the chat, and it is the reason the chat's native
provider registry is worth genericising: the pattern that a panel renders the
feature's own components already exists, it is simply pointed at one hardcoded
provider.

The registration code the surface needs, stated once so the spike has a target. Note
that only the *declaration* side goes through it — the copied handlers are ordinary
project code and reach it like any other feature does:

```go
// internal/server/extensions.gen.go — GENERATED from gogogo.yaml.
// One import per installed extension; this file is the entire wiring diff of
// an install.
package server

import (
    _ "github.com/someone/gogogo-ext-pomodoro"
)
```

The project's composition file lists which receipts are installed. It is data, and it
is the only file an install/edit/remove sequence touches besides the copied tree and
the receipt itself:

```yaml
# gogogo.yaml — the project's composition: what is installed, and which
# official recipes the project does not ship. Editing it is the supported way
# to add or remove a recipe; the generated .go and the receipts follow from it.
recipes:
  - pomodoro            # receipt at .recipes/pomodoro.yml
  - my-crm @ ./extensions/my-crm   # local, never submitted, first class
disabled: []
```

The generated file mirrors the receipts, so the two cannot disagree with each other;
what can disagree is the code and its declaration, and **that is what the drift check
is for**:

```
recipe pomodoro:
  code    modified in 4 of 9 files   (yours — nothing to do)
  schema  declaration changed at v0.56.1 → upgrade available, engine-applied
  routes  declared /pomodoro does not exist in the code → FAIL
  jobs    code registers ext:pomodoro:tick, declaration omits it → FAIL
```

That report is the honest version of "keep it honest over time", and it is only
possible because the receipt exists. It is also the answer to a question the pinned
model never had to ask: **how do you know a copied recipe still does what it claims?**
The declaration is checked against the code, and the code is checked against the
build.

A recipe *may* still be published as an ordinary Go module for the people who want
that — the earlier revision's shape — but that is a second distribution mode, not the
default and not the thing the catalog describes. Do not lead with it.

### Layer 2 — the catalog: curated holds, everything else is local

The catalog is a git repository of entries. It contains **only reviewed
entries**, and the review is the whole point: a catalog entry is a claim that this
recipe is a good citizen — that it was read, that its declaration matches its code,
and that it behaves under the installed sets listed. A curated catalog is cheap to
review and does not scale, which is exactly the trade being bought.

The claim an author wants is not "the catalog accepted me". It is "**this was
reviewed, and it will be described accurately however the project morphs**" — and
that is a *per-version* claim, because a unit's surface can depend on installed
units and on slots the project defines. So the catalog record is conversational in
form:

| Field | Meaning |
|---|---|
| `id` at a pinned version | what was reviewed; a new version needs a new review |
| `worksWith` | the installed sets the review actually exercised |
| `slots` | the slots it fills, and therefore what it looks like when a surrounding page presents differently |
| `routes`, `tables`, `jobs` | the exact surface claimed, so a collision is provable before install |
| `digest`, `minCore` | what to verify, and the oldest core it compiles against |
| `reviewedAt`, `reviewedBy`, `notes` | what made it acceptable, and the caveats a user should read |

That is a **behavior contract with the reviewer's signature on it**, not a
license-check badge. The mechanism is mundane — YAML in a git repository — because
the value is the review, not the format.

```yaml
# catalog/<id>/<version>.yaml — one reviewed artifact per version.
# A recipe, so there is no module path to resolve: there is a file set and a
# pristine hash per file, and the reviewer signs off on the behavior.
id: pomodoro
version: v1.2.0
source:
  repo: github.com/calionauta/gogogo
  path: features/pomodoro          # the pristine baseline lives here
  ref: v0.56.0
files:                             # installed path → pristine hash
  features/pomodoro/handlers/handler.go: sha256:...
  features/pomodoro/domain/timer.go:     sha256:...
license: MIT
minCore: v0.56.0
worksWith:                          # the sets the review exercised
  - core
  - core+todo
slots:
  - dashboard.card
  - settings.section
routes:
  - {method: GET, pattern: /pomodoro}
tables: [ext_pomodoro_session]
jobs: [ext:pomodoro:tick]
upstreamDeps: [todo@v0.56.0]        # pinned explicitly: refs are never inherited
reviewedAt: 2026-11-14
reviewedBy: calionauta
reviewedAgainst: 3f8a1c2          # the commit the reviewer actually exercised
notes: |
  Slots are single, not floating; in a dense dashboard the card is 4 columns.
  No write access to core tables. Two files are expected to be edited by
  every adopter (the page header and the interval default) — the review says
  which, so the drift report is not noisy.

**Status, 2026-11-03: the format is published, validated, and consumed.**
`internal/recipe/catalog.go` implements the entry above, and `Validate` enforces the review
rules the evidence supports: id and v-semver, source repo+ref, one sha256 per file, license,
`minCore`, the **dependency-closure declaration** (absent and empty are different claims — the
pointer exists so "I need nothing" cannot be mistaken for "I did not say"), the three review
fields, `ext_<id>` tables with core tables refused, `ext:<id>:` jobs, and known slot ids. Each
rule has a mutation its subtest catches. The consumer landed the same day: `gogogo recipe install --entry <entry.yml> <contents-dir>`
verifies that every reviewed file is present and hashes to the reviewed bytes, refuses files
the entry did not review, and only then installs through the same receipt path — the review is
about bytes, not about a name. What is deliberately not built: hosting and download. The entry
is a local artifact; fetching it is a distribution decision, not a format one.

**The personal tier is first class, not a degraded mode.** The same receipt shape
describes a local recipe that was never submitted anywhere:

```yaml
# gogogo.yaml — no catalog entry required. This is the normal case.
recipes:
  - my-crm @ ./extensions/my-crm   # local; or a private repo, or a git ref
  - pomodoro                       # catalog entry, reviewed (receipt has the rest)
```

Three consequences follow, and they are the reason the plan splits this way:

- **The user decides whether to submit.** An extension is usable the moment it
  builds. Submitting is a *social* act — asking for a review — not a technical
  prerequisite. Nothing in the install path requires a catalog row.
- **A new gogogo release cannot break a personal extension silently.** Without a
  catalog record there is no `worksWith` claim, so the upgrade path must say so:
  personal extensions upgrade *advisory*, curated ones upgrade *verified*.
- **The catalog stays small enough to be trustworthy.** It is a reviewed set of
  behaviors, not a directory of everything that exists.

### Layer 3 — an in-module tier, which is what makes the catalog real and the
product-shaped question answerable

The catalog cannot be populated by third parties on day one, because there is no
public surface yet and no reason for anyone to trust it. So the catalog starts
with units the project already owns: each candidate unit becomes a legitimate
**in-module extension** — its own registration package, contributing through the
surface, carrying its own catalog entry — and the core is refactored to consume it
through the seam. That is not a refactor for its own sake; it is the only way to
prove the surface against real code before asking a stranger to write against it.

This layer also does the work that keeps the project a *template for products*
rather than a fixed app: an in-module extension is where operators build their
own thing. Because it lives in the operator's fork, it may use `internal/` freely,
edit core files, and shape the composition file — and the project promises none of
it. The cost, stated plainly: **in-module extensions are not upgrade-safe**, so
their upgrade path is a git merge the operator owns.

The order that follows: an in-module extension is proven first, the surface is
frozen second, promotion to an out-of-tree module happens third (or never, if the
in-module tier turns out to be enough).

### Layer 4 — the upgrade story, which is where "alter the core" gets answered

Editing a checkout works today and needs no mechanism. What it lacks is a policy,
so the model states one and the tooling enforces only the boundary:

| Path | Who upgrades it | Mechanism |
|---|---|---|
| **Official default set** | the project (image by digest, or `go get` on the module) | the operator upgrades and keeps not being able to tell it was ever an extension |
| **Shipping-off extension** | the project, as a pinned dependency | `go get <module>@latest`, then rebuild — one command, standard Go |
| **Third-party extension** | the author, or the operator as a fork | pinned in `go.mod`; the operator's choice whether to track upstream |
| **Edited core** | **the operator, alone** | a git fork (or a patch queue). No merge machinery, and the docs say so plainly |

That last row is the honest answer and should be documented as a warning, not
deferred: an edited core is a fork, the project will not merge it back, and a
three-way merge of a fork is not a service gogogo offers. The contrast with
browsers is worth keeping in the docs because it explains the choice rather than
hiding it — a browser cannot be self-hosted-edited, a personal app can, and the
cost is exactly that you own the diff.

### Layer 5 — slots: what "usable inside existing pages and routes" means, and
what it must never mean

This is the layer a template-for-products lives or dies on, so the reasoning is
written out rather than assumed. The requirement is real: a new product built on
gogogo will want its thing *on the dashboard*, *in the settings page*, *in the
header*, *beside the todo list* — not only on its own route. An extension system
that can only add whole routes produces a product whose surfaces never talk to
each other, which is a plugin directory, not a framework.

So the answer is yes — through **declared slots, with fixed shapes**:

| Slot | What it is | Extension supplies |
|---|---|---|
| `dashboard.card` | a card in the dashboard grid | a component, a title, a size (small or wide), a refresh stream |
| `settings.section` | a section inside the settings page | a form section plus its own save endpoint |
| `nav.sidebar`, `nav.dock` | a destination | an entry, not a fragment |
| `record.row-actions` | buttons on a row of an owning unit's list | declarative actions (verb, label, href or endpoint), **not** a component |
| `page.embed` | an embeddable shell for an owning page | an iframe-able route name |
| `chat.native-panel` | a panel the assistant can render | a panel provider |

And three rules that decide whether this stays a framework or becomes a mess:

1. **A slot declares what fragments may be put in it: a grid cell or a full-width
   row — never "a fragment anywhere".** Slots without containers are how layouts
   rot: today the todo list sits in a container that also holds an interpolated
   seven-step install card, so a newly inserted fragment is always in a tight
   column and reads badly. A slot contract that says "you get a grid cell of width
   *n*" is designable; "you get a place in the page" is not.
2. **The slot's owner renders; the extension supplies content.** No extension
   renders a page's frame. So a core page that gains slots can change its layout
   without breaking extensions, which is the whole reason the catalog record lists
   `slots`.
3. **No slot inside a mutation path.** A slot may present data and may post to its
   *own* endpoint. It may never add a step to a core handler or a column to a core
   table. The moment an extension needs "run my check when the user saves a todo",
   the honest answers are two — promote that hook into the surface as a real seam
   with its own contract, or don't. A per-extension variant of a core handler is
the beginning of the fork this page rejects.

**The critical question, answered honestly: why would a user browse a catalog?**
Not for a second todo list. They browse because the catalog offers things that
*change the product they are shipping* — and that only works if the catalog is
organized by what a product needs, not by what the template shipped:

| Family | Examples of what it means for a product |
|---|---|
| Money | billing, plans, usage metering, invoices |
| Identity | SSO, team invites, org roles |
| Customers | CRM-lite, contacts, conversation history |
| Operations | admin audit view, feature flags, support inbox |
| Growth | waitlist, referrals, onboarding flows |
| Product surfaces | a dashboard widget pack, a report, a data export |

That framing has a hard consequence the project must accept: a template for
products is a **platform on a fixed core**, not a curated plugin directory. A
narrow official catalog is correct and should stay narrow — the honest promise is
"the core stays small and stable, personal extensions are first class and cost you
only your own fork", not "we reviewed two hundred plugins". A project that
promises breadth in the official catalog will end up either reviewing nothing
properly or paying a maintainer to review a long tail it cannot attest to.

So the recommendation for the catalog is **curated, small, and open to
submission**, while the *ecosystem* stays open by construction through local and
out-of-tree extension support. Breadth belongs to the ecosystem, trust to the
catalog. Confusing the two is the classic failure: a large unvetted catalog that
users trust because it looked official.

### What happens to trim

Trim stops being the install/uninstall mechanism and becomes **obsolete** for
installed units, because there is nothing to remove from a binary whose code
depends on the unit. The distinction to preserve:

- **Extension axis** (new): what is in the distribution at all. Uninstall =
  unwire. Non-destructive. Reversible. This is the only supported shape change.
- **Trim axis** (existing `Kind`): an unsupported fork. Removing an installed unit
  no longer compiles. The registry keeps the class for the catalog, not as a
  supported operation.

The anchor family in `internal/installer/add_spans.go` and the module-path
rebasing in `internal/installer/add_rebase.go` lose their reason to exist once
extensions no longer patch core files, and `trim`/`add`/`export` become installer
history rather than the extension mechanism. That code should not be deleted
blindly — it still serves the intra-release "put a unit back" case and the port
tool — but it stops being the growth path, and its removal is a separate,
measured change after the seam has proven itself.

### What happens to the scaffold prompt and the install-time lists

The conclusion this section reaches is stronger than "trim loses its job":
**scaffold, trim and `Kind` all lose their reason to exist as first-class
mechanisms, and the reason is that their purpose was ramp compression for a
single official template.**

- Today the prompt exists so a fresh project does not drag in 17 optional units
  on day one. Under the extension model the same goal is met by "the binary is
  the default set, everything else is installed from the catalog on demand" — so
  the prompt has nothing to do that the catalog does not do better.
- Today the three lists (capability registry, prompt table, README table) exist
  because there are three consumers of one fact. Under the extension model the
  fact moves into the catalog, and the catalog is the manifest — one source, no
  table to keep honest.
- Today `trim` exists because "remove an optional feature" was a desired
  operation. The extension axis makes it a supported *uninstall* instead, and the
  removal of an installed unit becomes a fork.

The instruction that follows from that: **do not repair `scaffold`, `trim` or the
`Scaffold` axis as part of the extension work.** They are superseded, not broken.
Reconciling them first — the analysis in the scaffold-choice page — is the wrong
work in the right order. Let the extension model subsume them, delete the prompt
when the catalog is the entry point, and retire the anchor family in the same
measured sequence.

What must survive the collapse: `DependsOn`, the registry's owned-paths data (the
conformance test and the docs generator both read it), and the install-time
checks (file sizes, secrets, policy coverage) which already iterate whatever is
registered and need no change.

## Impact: what actually changes

| Change | Cost | Notes |
|---|---|---|
| A public extension surface | **High** | `internal/` is unimportable by another module. Either the consumed packages move to a public tree, or extensions live in-module. This is the single largest decision below |
| Stop anchor-patching for extensions | Medium | `add_spans.go` and `add_rebase.go` shrink to the intra-release restore case |
| Navigation becomes a slot | Low | `internal/ui/modules.go` and the shell template already render a slice; the entries move behind the extension surface. Smallest seam, largest perceived effect for an extension author |
| Per-extension migrations | Low | `internal/dbschema/migrate.go` embeds one filesystem and sorts filenames; it needs a per-extension filesystem and a version prefix |
| Policy rows per extension | Low | A second CSV, plus the coverage test iterating registered extensions |
| RBAC coverage test | Low | Already walks the real mux, so extension routes are covered for free once mounted |
| Registry | Medium | Only worth building after the first two deliver |
| Docs and skill | Low | A skill that teaches the extension surface is what makes the model community-driven at all |

The public-surface decision deserves to be named as the fork in the road:

- **Option A — public packages.** Move what extensions consume to a public tree.
  This is a large import-path refactor across the module, and it is the only path
  to a genuinely third-party, out-of-tree extension.
- **Option B — in-module extensions.** Extensions live under a top-level directory
  in the same module. An author forks, adds a directory, and the official
  repository accepts pull requests. This preserves every current invariant, costs
  almost nothing, and gives up "a third party publishes without forking".
- **Option C — both, staged.** In-module first (it is a spike, not a refactor),
  then promote packages to public once the surface has stopped moving.

Option C is what the spike plan below assumes, because it defers the expensive
refactor until the seam has proven itself cheap. The refinement worth noting: for
the in-module tier, **the surface package can be new code that delegates to
`internal/`**, so nothing has to move for the first tier to work. The public-tree
refactor is only needed at promotion time — which is what makes Layer 3 possible
as a same-repository change rather than a module-wide migration. A versus B
remains a product decision and needs an explicit answer before the second spike; C
is the only sequence where the first tier is cheap.

## Alternatives considered and rejected

| Alternative | Why rejected | Revisit if |
|---|---|---|
| stdlib `plugin` for runtime loading | Toolchain-identical requirement, no unload, active regressions | Go ships a supported ABI with unload (no sign of this) |
| Hosted build server | Caddy removed theirs for centralization, mutability and lack of attestation | Never for the official project; a third party may run one |
| WASM for every extension | Second toolchain, degraded stdlib, per-call latency, isolation nobody asked for in a self-hosted app | The registry becomes open-submission and unreviewed |
| goja hooks as the extension mechanism | No isolation, no strong typing, serialization of runtimes, and the surface expressible in JS is far smaller than what a feature needs | A "customization hook" need appears that does not require routes or schema |
| Keeping anchor-patching and adding a catalog | A catalog on top of a mechanism that does not scale to N authors is decoration | Spike 0 shows anchors survive a modified core, which is not expected |
| Deleting trim entirely | It still serves intra-release restore and the port tool, and its removal is a separate change after the seam proves itself | Recipes cover every unit, including the reference slice |
| Making trim a supported uninstall | An installed unit is imported by the core; removing it does not compile, and no tooling changes that | Never — this is the consequence of compile-time composition |
| Copying the migration with the code (shadcn's `cssVars` shape) | Two owners of one append-only history; fails by re-running or by inconsistent history, and both hit production data | Never — split the declaration from the code instead |
| A file-level three-way merge engine | `copier`'s maintainers are replacing theirs with `git merge`; the ceiling is a human resolving markers | Git cannot arbitrate the merge, which no evidence suggests |
| Two-way `--diff` as the update story | It cannot separate the user's edits from upstream's, which is the whole problem | Never for a recipe that must be updatable |

## Spike plan — **all nine executed; the architecture survived**

Ordered by decision power, not by enthusiasm. Spikes 0 and 1 could kill the idea;
spending effort on the later ones before 0 and 1 was the classic failure, and it was
avoided.

**Outcome: no spike killed the model, and three changed the plan in ways that reading
could not have produced.** The summary of what each one settled:

| # | Spike | What it settled | Changed the plan? |
|---|---|---|---|
| 0 | falsify the anchor | Two of three failure modes are **silent**, and two anchors are **dead in this repository today** while `--check` stays green | Confirms the recipe model; the anchor table is unreviewed state no gate can see |
| 1 | the seam as a recipe | Declarations drive composition with **no core edit**; collisions report both owners before the build | Yes — validation runs on declarations, before compilation |
| 2 | receipt + drift | Three file states plus declaration-vs-code checks; one guard was **initially vacuous** | Yes — drift check is a hard prerequisite, not a nicety |
| 3 | the command | Install touches the copied tree and receipt **only**; blocked plan is all-or-nothing | Yes — uninstall keeps the receipt and never drops data |
| 4 | update after edits | Decidable from the receipt alone; **no merge engine needed** | Yes — git arbitrates, the receipt supplies the baseline |
| 5 | slots | The owning page **cannot learn the recipe id** even if it wanted to | Yes — row actions must be data, never a component |
| 6 | subprocess host | **21.66 µs** per local round trip vs 0.0005 µs in-process | Rejected as a default, with a number instead of an opinion |
| 7 | packaging | **57.5% of the binary** is dependency closure, not recipe code | Yes — the catalog must record dependency delta, and it argues for a *small* curated set more strongly than review cost does |
| 8 | authoring skill | Writing the rules once exposed three of them as preconditions | Yes — "commit your edits" is a requirement, not advice |

The sequence and what depended on what:

| # | Spike | Cost | Blocks | Kills the idea? |
|---|---|---|---|---|
| 0 | falsify the anchor | ½ day | — | **DONE — idea confirmed: silent failures** |
| 1 | the seam, as a recipe | 2–3 days | 2, 3, 4 | **DONE for declarations — no core edits needed** |
| 2 | the receipt and the drift check | ~1 day | 4, 5 | **DONE — validated; one guard was initially vacuous** |
| 3 | the command | ½ day | 5, 7 | **DONE at engine level — CLI name is a product decision** |
| 4 | update after the user edited | ~1 day | — | **DONE — decidable; git resolves, not this code** |
| 5 | slots | ~1 day | 7 | **DONE — the page cannot learn the recipe id** |
| 6 | subprocess host | ~1 day | — | **DONE — 21.66 µs/call; priced out as a default** |
| 7 | packaging without a toolchain | ½ day | — | **DONE — 57.5% of the binary is dependency closure** |
| 8 | the authoring skill | ½ day | adoption | **DONE — `skills/gogogo-recipe-authoring`** |

The ordering this revision changes, and why: the receipt and the drift check (2) and
the update-after-edits spike (4) move **ahead of everything about catalogs**, because
the recipe model stands or falls on them. A catalog is worthless if installed
recipes cannot be kept honest; the drift check is what makes "functional recipe"
different from "copy-pasted code with no provenance".

Spike 3 (slots) is placed before the catalog on purpose: it decides whether gogogo
is a catalog-equipped app or a platform for products, and no catalog work is worth
doing until that is answered.

The first extension built in Spike 1 must be **an existing in-module unit, not a
greenfield toy**. A toy proves the interface compiles; an existing unit proves it
carries real routes, real tables, real policy rows, a real job type and a real nav
entry, which is the only evidence that matters. Pick the smallest real candidate
that never owns a shared core file — `review` or `credits` rather than
`whiteboard`, for the reason the classification section gives.

Slots are also proven before the catalog is built, because a catalog of
whole-route extensions is a plugin directory and a catalog of slot-filling
extensions is a framework. Building the catalog first would entrench the wrong
shape.

### Spike 0 — falsify the anchor — **DONE, and the verdict is unambiguous**

Take the existing add path for one live unit and run it against a **deliberately
modified core** — move a line in the todo list template and in the composition
root, the way a version bump would.

- **Question:** does the current install mechanism fail loudly, fail silently, or
  land in the wrong place?
- **Method:** `internal/installer/spike_anchor_test.go` — five probes over the real
  `findAnchor`, `spanPresent` and the live anchor table. The baseline
  `gogogo --check --dir .` is green before and after, which is itself part of the
  result.
- **Kill criterion:** if it fails loudly with a named marker in every case, the
  anchor model is safer than this document claims and the urgency drops. If any
  case lands silently in the wrong place, the anchor model is disqualified for
  third-party use and Layer 1 is justified.

**Result: two of the three failure modes are silent, so the anchor model is
confirmed inadequate for third-party authors.** The measurements, per probe:

| Probe | Behavior | Verdict |
|---|---|---|
| Anchor line absent | `findAnchor` refuses: "cannot locate insertion anchor (evolved file?)" | **loud** — the good case |
| Anchor line re-indented | same refusal, because the match is exact line equality | loud, but the anchor is a whole-line formatting contract with the core |
| Span lines present but reordered | `spanPresent` is a **line-set** test, so it reports "already installed" and `add` is a **no-op with no error** | **silent** |
| Two identical anchor lines | `findAnchor` returns the **first** match, so insertion position is document order, not intent | **silent** |
| Anchor table vs the live tree | two of three `.templ` anchors are **dead in this repository today** | **silent, and present-tense** |

The last row is the finding that mattered most, because it is not hypothetical. The
anchor table names `features/todo/components/todo_list.templ` with the marker
`>6. Finalize</li>` and `features/todo/handlers/chi.go` with
`return components.TodoListRegion(signals)`; both strings are **gone** from those
files. A file was split into `layout.templ` plus `todo_list.templ` and the strip
that used to live there moved to `state_panel.templ`, and the anchors were left
behind. `gogogo --check --dir .` still reports `CHECK-OK` for every unit, because
`checkRuleSets` only validates anchors for units that *still carry strip rules for
that file* — the anchor became orphaned when the strip moved, and an orphaned
anchor is unreachable by the drift gate by construction.

Two conclusions, both changing the plan:

1. **The anchor table is unreviewed state that no gate can see.** This is not a
   discipline problem to fix with more care; it is a structural blind spot in the
   mechanism, and it is proof for the recipe model's central claim — the file set
   and its provenance must be recorded data, not inferred from string markers in
   the core.
2. **The failure mode that matters for a community is the silent one.** A loud
   failure costs an author five minutes; a silent one ships a page that looks fine
   and is structurally wrong. Any mechanism whose "already installed" test is a
   set-membership check will produce that class of bug under third-party
   divergence.

Cost: half a day, as estimated. The probes are left in the tree because their
assertions are the recorded evidence; they assert current behavior including the
bad parts, and each carries a `finding:` log line naming what it proves. Convert
them into regression tests for the recipe model, or delete them with the anchor
machinery — but do not delete them before the replacement exists.

**Follow-up, 2026-11-01 — the dead anchors were unreachable, and the table is now
gone.** The last probe in that list (the anchor table versus the live tree) was
replaced by `TestEveryDeclaredAddAnchorIsLive`, which walks *every* unit's strip
rules instead of a hand-copied three-row sample: the rule must still match a span,
a declared anchor must be a whole line that exists, must not sit inside the span
it deletes, and must survive every other unit's strips of the same file. It found
two dead anchors, and one more fact the prose above did not have: `refuseIrreversible`
already refuses **every** installer unit as manual, so `add` never reaches anchor
resolution at all. The dead anchors were therefore not a live regression — they
were transitively dead state, the same class as the orphaned strip rules. The
`.templ`/handler half of the table is deleted instead of repaired (repairing a
fallback nothing can call is work with no reader), the one comment-prefix anchor
in the desktop strip is fixed to its whole line, and the gate now holds the
remaining two entries to the live tree. Detail in the phase-4 note below.

### Spike 1 — the seam, as a recipe — **DONE for the declaration half**

The question changed with this revision: it is no longer "can a module register" but
**"can a recipe's declaration alone drive the composition root"**.

- **Method:** `internal/recipe/seam.go` — a `Provider` contract (ID, declaration,
routes), a `Registry` fed by a generated file, a `Validate` that runs on declarations
alone, and a `Mount` that refuses an invalid set.
- **Success criterion, binary:** a recipe reaches the live router through the seam with
the root never restating the URL, and a collision is reportable before the build
rather than as a boot panic.
- **Kill criterion:** a declaration that cannot be separated from the copied code.

**Result: the declaration half of the seam works, and it is small.** No new dependency.
`Validate` reports conflicts with both owners named and `Mount` refuses to mount
anything when the set is invalid:

```
recipe set is invalid: route "GET /dashboard" claimed by pomodoro and timer;
                      job "tick" claimed by pomodoro and (no recipe prefix);
                      table "todos" claimed by pomodoro and core
```

That message is the deliverable. It is the difference between a community that can
install two recipes without discovering the conflict in production and one that
cannot. The existing router panic stays as the last line of defence; this runs first,
on data, before compilation.

Properties established, each with a test hand-mutated to watch it fail: a route reaching
the live router through the seam (positive path); two recipes on one method+pattern
conflicting with both named, and Mount refusing instead of panicking later (duplicate
detection disabled reddens both); a job type without its recipe prefix refused (prefix
rule disabled reddens it); a recipe declaring a core table refused (boundary disabled
reddens it); registration in ID order not file order; and a valid set reporting zero
conflicts (the negative control).

**What Spike 1 did NOT prove, and must not be read as proven:** the copied-code half.
The provider in these tests is a real chi router, so the mounting path is real, but it
lives in the module rather than in a copied recipe directory, and no nav entry, policy
row or slot was rendered. The remaining work is exactly the nav/`Panel` surface and the
generated registration file — Spike 3 territory plus the slot work of Spike 5. So the
architecture is validated where it mattered most (declarations can drive composition
with no core edits: yes) and unvalidated where the ordinary work remains (does the
copied tree render inside the real shell: not yet).

### Spike 2 — the receipt and the drift check — **DONE, mechanism validated**

This is the spike that makes the model honest, and it was cheap enough that skipping
it would have been negligence.

- **Question:** given a recipe the user has already edited, can the tool report which
  files are pristine, which are touched, and whether the declaration still matches
  the code?
- **Method:** `internal/recipe/` — a receipt type with a per-file pristine sha256, a
  `Check` that compares receipt against tree, and a declaration check that compares
  claims against the code actually present.
- **Kill criterion:** if a per-file hash plus the declaration cannot answer "is this
  still what it claims", the update story has no foundation.

**Result: the answer is yes, and the mechanism is small.** The receipt carries
`upstream{repo,ref,path}`, `files{path → pristine sha256}`, and
a `declaration{tables,routes,jobs,slots}`. `Check` returns three file states —
pristine, modified, deleted — and a list of declaration problems. The report a user
sees:

```
recipe pomodoro:
  code    7 pristine, 2 modified (yours — nothing to do), 0 deleted
            M features/pomodoro/domain.go
            M features/pomodoro/components/page.templ
  update  untouched files fast-forward; modified files need a real merge
  decl    matches the code
```

What the spike established, stated as properties rather than features:

| Property | Why it matters |
|---|---|
| A modified file is reported modified, never pristine | The only thing preventing a destructive overwrite. A hash is what makes "did the user touch this" answerable at all |
| A deleted file is a state, not an error | Removal is user intent; an update must not resurrect it |
| A fully pristine tree reports "fast-forward is safe" | The common case costs nothing — no merge, no prompts |
| A declared route absent from the code is a **problem** | This is the drift a version string cannot express, and it is what ships a broken nav entry |
| A declared job the code does not register is a **problem** | Same class, and this one burns CPU forever instead of breaking a page |
| A recipe declaring a **core** table is refused | The ownership boundary that makes uninstall safe and migrations unmergeable-free |
| A matching declaration produces no problems | The negative control; without it every check above could be passing by rejecting everything |

**Two findings from PROVE (hand-mutating the implementation to watch each check fail):**

1. Disabling the modified/pristine comparison, the route check, or the job check each
turned a test red immediately — those guards are real.
2. The core-table boundary test **passed for the wrong reason** on the first attempt:
the declared core table also had no SQL in the fixture, so the missing-SQL check fired
first and masked it. The test only became a real guard after the fixture was given SQL
that *contains* the core table name and the boundary check was moved ahead of the SQL
check. This is worth recording because it is the exact failure mode the project's testing
rule warns about: a green test that cannot go red in the way it claims to.

**The limit the spike also measured:** the declaration check is textual and
conservative. It proves "the code mentions this pattern somewhere", not "this route is
mounted with this method". That is the right trade for a drift *warning* — a strict
claim belongs in a test — but it must be written down, because a reader will otherwise
assume the check is stronger than it is.

Deliberately **not** built: install, update, merge, YAML persistence, CLI wiring. The
spike ends at "the mechanism answers the question". Spike 4 is where it becomes an
update.

### Spike 3 — the command — **DONE: engine and CLI**

Install and remove, both mechanical, both on the recipe model.

- **Question:** is installation a mechanical operation with no hidden patch step?
- **Method:** `internal/recipe/apply.go` — `Apply` executes a plan, `SaveReceipt`/
  `LoadReceipt`/`ListReceipts` persist the receipt, `NewReceipt` builds it from the
  bytes actually written, and `Remove` is uninstall.
- **Kill criterion:** any mutation outside the copied tree, the receipt, the
  generated registration and the user's module file.

**Result: at the engine level the answer is yes — install touches the copied tree and
the receipt and nothing else.** No anchor, no span, no module rebase, no core patch.
The mutation surface is exactly what the kill criterion allows.

Properties established, each with a hand-mutation that reddens it:

| Property | Mutation that reddens it |
|---|---|
| A fresh install writes the file set and leaves the receipt describing it | — (positive path) |
| A freshly installed tree is pristine by its own receipt | — (this is what makes update 1 a fast-forward) |
| A **blocked plan is applied as nothing at all**, never partially | the `Blocked` guard disabled (a partial write occurs) |
| Uninstall deletes only **pristine** files and reports the kept ones | `StateModified` routed into the delete branch (user file destroyed) |
| The receipt round-trips through YAML with hashes and declaration intact | — (the whole update story depends on this) |
| One receipt cannot silently hijack another's file | — (rename safety) |
| Uninstall **leaves the receipt** so the data still has an owner | — (recovery from a mistaken uninstall) |
| An empty tree lists as zero recipes, not an error | — (a fresh clone is the normal state) |

Two decisions this spike settled that the research section argued for and that are now
enforceable rather than aspirational:

1. **Uninstall does not remove the receipt**, and does not touch data. Deleting the
   receipt is a separate explicit act. Without that, an accidental uninstall orphans
   tables with no recorded owner, which is the failure the Shopware/Nextly evidence
   warned about.
2. **A blocked plan is all-or-nothing.** Refusing to apply rather than applying the
   non-conflicting subset is the difference between an updater and a machine that
   leaves a tree half-updated with no record of where it stopped.

**The CLI dispatch landed 2026-11-03, as `gogogo recipe install|status`.** The command was
withheld from the spike because a name is a product decision; once named, the work was the
`case` the note predicted:

- `recipe install <dir> [--dir target] [--yes]` reads the recipe's `.recipe.yml` (id,
  upstream, declaration) plus its file set, plans against the receipt that is already
  there, and applies **nothing** without `--yes`. With it: copies the tree, writes
  `.recipes/<id>.yml`, and then runs the drift check — a declared route the copied code
  does not register fails the command, which is the one defect a receipt exists to expose.
- `recipe status [id]` is read-only: pristine, modified (the user's), deleted, plus the
  declaration problems.

**Registration is generated, as of the same day.** The seam's `Provider` is a handler
method, so mounting installed code needs its constructor and dependencies; the contract is
one exported function per recipe package:

```go
func Register(reg *recipe.Registry, d register.Deps) { reg.Add(NewHandler(d.Cfg, d.Auth, d.Queries).Recipe()) }
```

`internal/recipe/register` carries `Deps` (config, auth, store, SSE hub) and `Func`. The
manifest also names the `module` its imports were written against, and install rewrites it
to the target tree's own module in `.go`/`.templ` files (never in prose) — a recipe written
once must still resolve after the project's rename. The installer renders
`internal/server/recipes.gen.go` from the receipts — sorted, gofmt'd, module path read from
the tree's own `go.mod` — and a receipt marked removed is skipped, so
an uninstall never leaves an import of a package that is gone. The repository commits the
empty generated file, and a freshness test fails if it drifts from the receipts, exactly as
the CSS and templ gates do.

One deliberate omission: install does not run `go build`. It validates that the `register`
package exists in the copied set and that the generated file parses; proving an arbitrary
tree needs that tree's toolchain and module cache, and the command ends by naming the build
to run.

### Spike 4 — update after the user edited — **DONE, and it needs no merge engine**

- **Question:** with upstream ahead by a version and the user ahead by their own
  edits, does an honest update exist?
- **Method:** `internal/recipe/plan.go` — one decision table over three inputs the
  receipt already provides: the pristine hash (what install wrote), the current tree
  (what the user did), and the new upstream content.
- **Kill criterion:** having to build a file-level three-way merge engine.

**Result: the update is decidable, and install is a special case of it.** No merge
engine was written and none is needed for the common cases. The complete table:

| | upstream has it | upstream dropped it |
|---|---|---|
| **not installed** | write | — |
| **pristine** | fast-forward | obsolete |
| **modified** | **conflict** | conflict |
| **deleted by user** | gone (not resurrected) | gone |

The properties this pins down, each with a test hand-mutated to watch it fail:

| Property | Why it is load-bearing |
|---|---|
| A fresh install is all writes | makes install and update ONE mechanism instead of two |
| A pristine file with a moved upstream fast-forwards | the common case costs no human attention |
| modified + upstream moved is a **conflict that blocks the plan** | this is the line between an updater and a data-loss bug |
| modified + upstream unchanged **keeps the user's bytes and carries no content** | if this regresses the model becomes destructive |
| upstream dropping an untouched file is obsolete (cleanable) | cleanup is possible without risking user work |
| upstream dropping a **modified** file conflicts rather than deletes | the user's work is never collateral |
| a user-deleted file is `gone`, never rewritten | removal is intent, not an accident |
| operations are in path order | a plan can be reviewed and diffed stably |
| the report labels a kept file "(yours)" | language trains behaviour: a report calling an edit a problem teaches people to overwrite it |

Four mutations were applied to the implementation and all four reddened a test:
ignoring user edits (destructive), resurrecting a deleted file, never fast-forwarding
(updates never land), and disabling the conflict branch. The one mutation that did not
compile first — removing the assignment entirely — was rewritten into a valid form
because a mutation that cannot build proves nothing.

**The honest remainder, and it is the part that still needs git.** The plan decides
*what* conflicts; it does not resolve one. When both sides changed the same file, the
answer is a three-way merge with three real inputs — pristine, current, upstream — and
that is `git merge`'s job, exactly as the research section concluded. What the plan adds
is the thing git cannot supply on its own: **the pristine baseline**, which is why the
receipt's hashes are the enabling fact rather than a nicety. So the integration is
"produce a branch with the pristine content, let git merge the rest", not "reimplement
merge".

Note also what the plan deliberately does NOT decide: what to do when the user's edit
and the upstream change are in *different* files (trivially both apply, and the table
handles it) versus the same file but non-overlapping hunks (git resolves it, the plan
still reports a conflict). Being coarse here is correct: a planner that tries to classify
hunks is building the engine this spike was supposed to avoid.

### Spike 5 — slots — **DONE, and the abstraction holds**

This is the spike that decides whether gogogo is a catalog-equipped app or a platform
for products, so it must not wait for the catalog.

- **Question:** can a recipe render content inside a core page — a dashboard card and
  a settings section — without the core page knowing which recipe it is, and without
  the recipe touching the core page's logic?
**Status, 2026-11-03: the first real page is wired.** `settings.section` renders in
`/config`: the composition root owns one `recipe.SlotRegistry`, the page receives
`Fragments(SlotSettingsSection)` and iterates them inside its own card frame, and the
handler never sees a recipe id. A fixture fragment proves the page renders it; removing the
handler's read of the registry turns the test red. `dashboard.card` stays unrendered because
this app has no dashboard page — inventing one to host a slot would be the page-first
mistake the slot shapes exist to prevent. **2026-11-06: that stays true by refusal instead of
by inattention.** Offering a slot and having no page draw it was the one silent drop left in
the slot model, so the offer is now a table of hosts: registration, `recipe install` (before a
file is copied) and the catalog review all refuse a slot this build does not render.

**Status, 2026-11-04: the second real page is wired.** `nav.sidebar` renders in the shell's
rail as its own "Extensions" group, seeded through the request context so `internal/ui` keeps no
knowledge of the recipe package. Registration refuses any href that is not root-relative
(`//host`, `javascript:`, absolute URLs), so a contribution cannot turn the sidebar into a
redirect; the rail renders labels and paths and never a recipe id. `record.row-actions` stays
registry-only: it is about another unit's entity, and entity ownership is still a code decision
rather than data.

- **Method:** `internal/recipe/slot.go` — a closed slot set, a declared shape per slot,
  a registry that takes contributions, and **one** call an owning page makes.
- **Kill criterion:** if filling a slot requires the owning page to know the recipe's
  ID, the abstraction failed.

**Result: the page cannot learn the recipe's ID even if it wants to.** The owning page's
entire involvement is one signature:

```go
out := slots.RenderSlot(SlotDashboardCard, func(f Fragment) string {
    return `<section class="ui-card">` + f.Render() + `</section>`
})
```

There is no recipe id in that signature, and a test asserts that no identifier leaks
into the rendered output. That absence is the abstraction — not a convention, not a
lint rule.

Properties established, each with a hand-mutation that reddens it:

| Property | Mutation that reddens it |
|---|---|
| A page renders contributions without ever naming the recipe | — (the signature makes it impossible) |
| The page owns the container; the recipe supplies only content | — (asserted: the frame appears once per contribution) |
| An unknown slot is **refused** at registration | slot validation disabled (silently accepted) |
| Every offered slot declares a shape | — (closed table) |
| Row actions are **data**, not components | — (type is data; no templ, no handler reference) |
| Contributions render in stable order | ordering removed |
| An empty slot renders nothing | — (no empty chrome for an uninstalled project) |

**Three findings worth carrying out of the spike:**

1. **The container contract is the load-bearing part, and the repository confirms why.**
   The live todo page lays out a `grid-cols-1` with a `lg:col-span-3` column, and the
   same container also carries an interpolated multi-step install card. A fragment
   inserted there today lands in a tight column regardless of intent — precisely the
   "slot without a container" failure the research section predicted. Declaring
   `ShapeCell` vs `ShapeWide` is what turns that from a guess into a contract.
2. **Row actions had to be data, not a component.** A component would need the owning
   recipe's entity names and its authorization decisions, which is the hook-inside-a-
   core-handler failure arriving through the UI. As declarative data (label, href, verb,
   confirmation) the owning page keeps the authority and the recipe keeps no reference.
   This is the most valuable slot for a product-shaped system and the easiest to get
   wrong.
3. **A closed slot set is a feature, not a limitation.** Accepting an arbitrary slot id
   would silently drop content the shell does not lay out. Refusing at registration
   converts a rendering mystery into a build error.

**What the spike did not do:** it did not touch a real `.templ` page. The registry and
its contract are real and tested; the wiring of `RenderSlot` into the shell's dashboard
and settings page is ordinary templ work that belongs to the adoption PR, not to a
research spike — and doing it here would have meant editing the thing the spike exists
to prove doesn't need editing per-recipe.

### Spike 6 — subprocess host — **DONE as a measurement; the mechanism is priced out**

The purpose was never to adopt this; it was to price the alternative honestly instead of
dismissing it from the research alone.

- **Method:** a minimal Go probe, not a full plugin host — one local-socket round trip
  against an in-process call, which is the dominant cost of any out-of-process recipe.
- **Result, measured on this machine:**

| Path | Cost per call |
|---|---|
| in-process call | ~0.0005 µs |
| local socket round trip | **21.66 µs** |

That is a ~40 000× gap, and it is not the interesting number by itself. The interesting
one is what it means per surface:

| Surface | Calls per request | Extra cost | Verdict |
|---|---|---|---|
| A recipe's own page or endpoint | tens | ~1 ms | tolerable |
| A slot filling a dashboard with rows | hundreds to thousands | 10–100 ms | **visible** |
| A per-row render loop or a hot read path | thousands | seconds | **unusable** |

The conclusion matches what the research predicted and now has a local number behind it:
**an out-of-process recipe is a coarse-grained seam.** It would be defensible for a
webhook-style integration and is the wrong shape for a recipe a user installs to render
UI. Combined with the packaging finding (a build-time recipe costs nothing at runtime),
the case for subprocess loading in this project is: **there is none, unless install
without rebuild becomes a requirement** — and the packaging spike showed the rebuild is
not the bottleneck for a self-hosted operator, because the artifact is the distribution
unit and the server pulls it by digest.

Two things worth keeping from this spike even though the mechanism is rejected:

1. **The number is the deliverable.** "We don't need subprocess plugins" is an opinion;
   "21.66 µs per call, so a slot rendering 200 rows costs 4 ms of pure IPC" is a
decision someone else can check.
2. If a future need is genuinely external — a language other than Go, a process the
   operator must restart independently, an untrusted registry — the seam already supports
   it, because the recipe contract is declarations plus a router, and an implementation
   that happens to live behind a socket is not excluded by anything in the design. The
   rejection is of the *default*, not of the possibility.

### Spike 7 — packaging without a toolchain — **DONE, with a finding that changes the catalog's tooling**

The question the JS comparison actually raises: does an operator who has no Go
installed get recipes at all?

- **Question:** can a prebuilt artifact carry a non-default recipe set end to end,
  through the same digest-pinned deploy the project already uses?
- **Method:** build the default set, then trim four recipes (`whiteboard`, `review`,
  `credits`, `genui`) with the existing installer and build again. Measure the delta.
- **Kill criterion:** if neither path yields a working image without a toolchain on
  the target, state that as an explicit requirement rather than an implicit one.

**Measured (stripped, `-trimpath -ldflags="-s -w"`):**

| Build | Size |
|---|---|
| default set | 88.3 MB |
| four recipes removed | 37.5 MB |
| **delta** | **50.7 MB — 57.5% of the binary** |

**The delta is not the recipes. It is what they pull in.** The removed recipes' own
symbols are negligible; the weight is transitive: `genkit` and the model client account
for 229 symbols on their own, alongside casbin, otel, `x/crypto` and a compression
library. So the recipe model's *packaging* cost is dominated by dependency closure, not
by file count — which has two consequences the plan did not previously carry:

1. **"Recipe size" is the wrong metric for the catalog, and the review should record a
   dependency delta.** A 2 000-line recipe that drags a model framework costs 50 MB; a
   4 000-line recipe using only the standard library costs almost nothing. An entry that
   does not declare its dependency closure will surprise the operator at build time.
2. **This is an argument for the curated catalog being small** that is stronger than the
   review-cost argument. Each heavy entry is paid by every operator who installs it, and
   the cost is invisible in the recipe's own diff.

**A second finding, unrelated to packaging but discovered here and worth recording:** the
installer's *prove* step fails on a renamed project. Trimming and renaming together
breaks `TestGoToolchainPinIsConsistent` because the proof runs inside the renamed module
and the test asserts `AGENTS.md` mentions the Go version — the rename rewrote paths
without the doc keeping the string the test looks for. The trim itself was correct (the
directories and imports were right); only the proof gate failed, and it failed in a way
that reads as "the trim broke something". That is a false positive in the safety net and
it is worth a issue of its own: a proof step that cannot distinguish "the change is wrong"
from "the doc did not follow the rename" trains an operator to ignore it.

**What this spike settles about the JS comparison:**

| Model | Who compiles | Verdict |
|---|---|---|
| Official image by digest | CI, with the default set | **solved** — the server pulls it; no toolchain on target |
| Custom image | the operator's CI or local `docker build` | **solved**, and it is how a fork with extra recipes ships |
| A **third-party** recipe in an official image | nobody — the author cannot push to the official registry | **the real limit** |

So the honest statement for the docs, and it is the kill-criterion answer: **adding a
third-party recipe requires a Go toolchain (or a Dockerfile and CI) somewhere, just not
necessarily on the running server.** That is Caddy's split, and it should be written as a
requirement rather than discovered: the official set is a solved problem, third-party
recipes need their author or their operator to build.

### Spike 8 — the authoring skill — **DONE, and it doubles as the model's contract**

Not "how to install" — that is a command. **"How to be a good owner of a copied
recipe"**: what you may own, what you may never touch, how to keep the declaration in
step with the code, and what happens to your edits at update time.

- **Deliverable:** `skills/gogogo-recipe-authoring/SKILL.md`, registered at
  `.agents/skills/gogogo-recipe-authoring`, and passing
  `cmd/check-skill-frontmatter` (frontmatter valid, registered, references resolve).
- **Structure:** the three parts and who owns each → check-before-you-write (catalog,
  reference slice, scaffold) → the four things a recipe may never do → the slot table
  with shapes → what the receipt records and why hashes not versions → the
  after-you-edit table → submitting to the catalog → a closing checklist → three cases
  where the model genuinely does not fit.

**The spike produced a finding about the model itself, which is why it was worth doing
rather than deferring:** writing the skill forced every rule to be stated once, and three
of them turned out to be load-bearing in a way the architecture sections had only implied.

1. **"Keep your changes in commits, not in one uncommitted blob"** is not advice, it is a
   precondition. The update path uses git as the arbiter and the receipt as the third
   input; a user who edits and never commits has destroyed the baseline and there is no
   honest merge left. That belongs in the docs as a requirement, and it is now stated as
   one.
2. **The dependency-closure warning belongs in the author's checklist**, not only in the
   packaging analysis. `npm`-shaped ecosystems surface cost at install; a recipe's cost is
   invisible in its own diff and only appears at build time. An author who does not declare
   it is handing the operator a surprise measured in tens of megabytes.
3. **"Consult `features/todo` before inventing a structure" is the mechanism that keeps
   copied recipes recognisable.** Without it every recipe invents its own handler shape and
   the model degenerates into unrelated forks — the scale-rot failure the research section
   recorded from the shadcn-at-scale literature.

The skill also closes the loop on the model's worst failure mode: a recipe that is copied,
never understood, and abandoned. Stating "this is yours the moment it is written, here is
what to change first, here is what to keep in step" is the only defence that exists — no
sandbox and no gate can prevent neglect.

## Explicitly do not do

- **Do not hand the user an editable migration file.** It is the Django Oscar / Laravel
  publish trap: it fails by re-running or by inconsistent history, and both hit
  production data. Schema is declared and engine-applied; code is copied.
- **Do not build a file-level three-way merge engine.** `copier`'s maintainers are
  replacing theirs with `git merge`. Produce a clean baseline and let git arbitrate.
- **Do not mark a recipe file with `DO NOT EDIT`.** The marker's meaning is the inverse
  of a recipe's intent: a recipe is installed to be owned.
- **Do not ship the recipe format without the receipt and the drift check.** That is
  precisely shadcn's gap, and the criticism of copy-paste registries is correct about
  it. For a recipe owning routes, jobs and tables, a stale copy is a correctness and
  security defect rather than a cosmetic one.
- **Do not let a `registryDependencies`-style reference stay unpinned in the receipt.**
  shadcn's refs are not inherited, so a dependency's own dependencies can move under
  the user.
- **Do not use buildmode=plugin.** Caddy paid to discover this twice; the Go 1.27
  itab regression is the price of ignoring it.
- **Do not host a build server for the official catalog.** Caddy removed theirs
  and named the reasons.
- **Do not call in-process Go recipes sandboxed.** They are not. State the permission
  model in the same blunt register the precedents use.
- **Do not let a recipe own a column on a core table, or an unprefixed job type, table
  name or route.**
- **Do not let uninstall drop data by default.**
- **Do not make boot, config, database, auth, RBAC or the shell extensible.** That
  is a fork with extra steps.
- **Do not build the catalog before Spikes 1 and 2 pass.** A catalog in front of an
  unproven seam is a folder of YAML nobody can install from.
- **Do not delete the anchor machinery in the same change that introduces the
  seam.** Restore-within-a-release still uses it; retire it in a later, measured
  step.

## What to do next, now that the spikes have run

This section exists because the spike results changed the order of work. It replaces
"what should we spike" with "what should we build, in what order, and what must be
written down first".

**Status, 2026-11-04 — the maintained block.** Detail stays in the phase sections below;
this is the summary to update on every landing.

Done: the engine (seam, receipt, drift, planner — Spikes 1–4); the seam wired into
`BootChi`, with `review`, `credits`, `config-view`, `room`, `todo`, `notes`, `whiteboard`,
`landing`, `account` and `notifications` converted; the
nil-provider rule for optional units; the anchor drift gate; the CLI (`recipe install`,
`update`, `uninstall`, `status`); and the **generated registration** that mounts an
installed recipe — `.recipe.yml` → `.recipes/<id>.yml` → `internal/server/recipes.gen.go`,
with one `Register(reg, deps)` function per recipe; the **catalog entry format** published,
validated and **consumed** (`recipe install --entry`, hash-verified against the reviewed bytes);
the **first two real slots** — `settings.section` renders in `/config` and `nav.sidebar` puts a
recipe's destination in the shell's rail, with neither page learning a recipe id (the rail
refuses a non-root-relative href at registration, so a contribution cannot leave the origin); and
the **schema half of the seam** — a recipe that owns tables ships its own DDL (`recipe.Migrator`),
the root applies it through the core ledger under the recipe's id at boot, and the declaration is
checked against the SQL in both directions; and **install-time codegen (2026-11-05)** — the DDL is
mirrored flat into `internal/db/recipe_schema/` as `<id>__<migration>.sql`, which is a path `sqlc`
actually reads, so a recipe-owned table gets generated queries instead of hand-written SQL.
`recipe status` compares the mirror against the tree, and `recipe install` is the repair.

**Install now finishes, and uninstall unwinds (2026-11-06).** Three gaps closed in one pass:

- **Install runs the generators instead of naming them.** templ is a `go tool` dependency of the
  module, so the copy is generated with the pinned version; `sqlc` is a standalone binary, so it
  runs when it is on `PATH` and the install says `make sqlc` when it is not. A recipe's table gets
  generated queries without the operator being told to re-run a command the installer could run.
- **The policy rows are the tool's.** `policy.csv` was written before the recipe existed, and a
  route the policy has never seen is a 403 for every signed-in user in production — an install
  that "worked" and shipped a page nobody can open. Install writes the rows into a block marked
  with the recipe id, `recipe status` compares that block with the declaration, and `uninstall`
  removes exactly it. The file stays the one authorization source; the block is data, not a second
  policy stream.
- **Uninstall removes what install derived.** Generated output whose source the uninstall just
  deleted (`page_templ.go`, `sqlcgen/<query>.sql.go`) goes with it, then the generators run again
  so the rest of the tree stays consistent. A file the user edited stays, and so does its
  generated companion — uninstall never breaks a build it did not finish removing.

**A reference recipe ships in the repo** (`_extensions/pomodoro`, 2026-11-06): one table, one
migration, one query file, one page, one write and one sidebar entry, installed end to end by the
CLI. It is the artifact the "worth using" and "right shape" rows below have no test for, and the
shape an author copies instead of inventing.

**Two gates closed with it (2026-11-06).** The generated `AGENTS.md` now states the Go pin the
target tree's `go.mod` declares — the scaffolder replaced a file the prove step then read, so a
real scaffold failed its own proof. And the SQL boundary is enforced on the statements, not only
on the declaration: a migration that alters, drops, indexes, seeds or triggers a core table is
refused at boot, while a foreign key that references one is not. Gap analysis 2026-11-06 records
what is still open.

Every route-owning unit in this repo now mounts through the seam: `review`, `credits`,
`config-view`, `room`, `todo`, `notes`, `whiteboard`, `landing`, `account`, `notifications`. The
composition root is the only place left that registers a router directly, and that is by design.

Next, in order:

1. **Phase 4 is decided, not pending: `add` stays, so the anchors stay.** Retiring them needs the
   precondition the phase itself names — removal stops being anchored once ownership is data
   (receipts for in-tree units). Until then, `trim` is the only working removal path for a unit
   that ships in-tree, and `add` is the only way back from it; deleting `add` first would leave
   removal one-way, which is strictly worse than the drift the anchors cost. The trigger to
   revisit is written in the phase section: the first in-tree unit that ships as a receipt.
2. **One slot left, and it needs a page rather than plumbing.** `dashboard.card` has no dashboard
   to land in, and inventing one to host a card is the page-first mistake the shapes exist to
   prevent. `nav.sidebar` and `record.row-actions` shipped (2026-11-04) because both had a real
   host — the rail and the todo row. The gap closed as a gate, not as a page (2026-11-06): the
   shape stays declared, the contribution is refused until a host exists.
3. **The first table-owning conversion is the probe that closes the schema split, and it is now
   unblocked.** `todo` remains the intended unit. Moving one of its tables to a prefixed recipe
   migration is what proves an *in-tree* unit can own DDL through the ledger; the reference recipe
   (`_extensions/pomodoro`) proves a *copied* one can, end to end, which is the fixture the
   install-and-generate path was missing but not the conversion itself.

### Build, in this order

1. **~~Wire the recipe engine into the composition root.~~ DONE (2026-11-03).** The converted
   set mounts only through the seam, and the route gate compares declaration against what
   mounted. The installed path is closed too: `recipe install` writes the receipt and the
   generated registration that mounts it.
2. **~~Give the shell two slots.~~ `settings.section` DONE (2026-11-03), `nav.sidebar` DONE
   (2026-11-04).** Both live in a real page: the settings section in `/config`, the extension
   destinations in the shell rail. `dashboard.card` still has no dashboard to land in and
   `record.row-actions` waits on entity ownership.
3. **~~Choose the command name and wire the dispatch.~~ DONE (2026-11-03).** The name is
   `gogogo recipe <verb>`, and all four verbs ship: `install` (from a directory or a reviewed
   entry), `update`, `uninstall`, `status`.
4. **~~Publish the catalog format with dependency delta required.~~ DONE (2026-11-03).** Spike 7
   made the closure mandatory: `upstreamDeps` is a pointer, so "absent" cannot be mistaken for
   "empty". The format is validated entry by entry and consumed by `recipe install --entry`,
   which verifies the bytes against the reviewed hashes before anything is planned.
5. **Only then** consider a second distribution tier (out-of-tree modules). The in-module
   tier satisfies the template's purpose, and promoting packages is the expensive refactor
   the plan defers until the surface has stopped moving.

### Write down before the first adopting PR

These are decisions the evidence now supports, and they should land as docs rather than as
institutional memory. Status, 2026-11-05: **all five are stated.** The first four are in this
page (the one an operator reads before installing anything), the trim sequence gained a
decision in the phase-4 section (`add` stays until ownership is data), and the schema split
is written as the authoring contract in
`skills/gogogo-recipe-authoring/SKILL.md#the-three-parts-of-a-recipe-and-who-owns-each` —
where an author will read it before copying a migration — with the reason recorded here in
open question #4.

- **The trust statement, in pi's register:** a recipe is compiled into the binary and runs
  with the binary's permissions. No sandbox exists. Silently implying otherwise is the one
  dishonest thing this model could do.
- **"You need a toolchain to add a third-party recipe"** — stated as a requirement, per
  Spike 7's kill criterion. The official set needs none; a third-party recipe needs its
  author's or operator's build.
- **"Commit your edits"** — a precondition of the update path, per Spike 8. A user who
  edits without committing has destroyed the baseline.
- **The `trim` retirement sequence:** subsumed → deleted, never repaired. Spike 0 showed
  the anchor table is already drifting unreviewed, so repairing it is investment in a
  mechanism with a known structural blind spot.
- **The schema split, stated as a hard rule** (code and schema both copied, ownership
  declared) — with the reason, because a reader will otherwise try to own a core table or
  hand-write a query, and both will work right up until they do not. Stated in the authoring
  skill; enforced by `recipe.ValidateSchema` in both directions and by the ledger's id
  prefix.

### Two issues filed independently of this work — both closed

Discovered during the spikes, real, and not part of the recipe model:

1. **Dead anchors in `internal/installer/add.go` — closed by the generalized gate.** The
   anchors were dead and `gogogo --check` reported OK, because a strip moved to a different
   file and orphaned them. `--check` was never the place the class could be caught: `add`
   refuses every unit before it mutates the tree, so the anchors had no caller at all.
   `TestEveryDeclaredAddAnchorIsLive` now walks every rule of every unit and fails on a
   marker that matches nothing, on an anchor that is not a whole line, and on an anchor its
   own span or a sibling strip removes.
2. **The proof failed on a scaffolded project — closed 2026-11-06.** The cause was sharper
   than "the rename rewrote paths": `writeAgents` REPLACES the template's `AGENTS.md`, and
   the prove step then runs `TestGoToolchainPinIsConsistent`, which requires that same file
   to name the Go version. Every real scaffold therefore failed a test its own output had
   just broken, and the failure read as a broken trim. The generated `AGENTS.md` now states
   the pin the target tree's `go.mod` declares (`installer.agents.go`), and
   `TestGeneratedAgentsKeepsTheToolchainPin` asserts the pin test's own predicate.

### What would make this analysis wrong

Stated plainly so a future reader can falsify it rather than inherit it:

- If third-party authors turn out to need **install without rebuild** (a hosted operator
  who cannot run CI), the subprocess work becomes justified and Spike 6's number is the
  budget for it: ~22 µs per call, tolerable for endpoints, unusable for slot rendering.
- If most installed recipes are **never edited**, the recipe model is over-engineered for
  its users and the pinned-package model would have been right. The evidence from the
  copy-paste registries says most copies are never edited — so this project's bet is that
  a *template's* users differ from a *component library's* users, which is testable by
  looking at what people actually change.
- If the catalog grows past the point where review is honest, breadth was the wrong promise
  and the small-curated-set position should be re-examined, not quietly relaxed.

## Can this be refactored safely today? — the honest answer

This is the question a reader arrives with after the spikes, so it gets an explicit
answer rather than an implication. **The engine is ready; the project is not, and the
blockers are specific.**

### What is genuinely done

`internal/recipe` implements the four mechanisms the spikes validated: declaration
validation and mounting, receipt persistence with pristine hashes, the drift check, the
install/update/remove planner, and the slot registry. Thirty-five tests, each of the
load-bearing ones with a hand-mutation proving it can fail. That is a real foundation.

One phase-4 item is already done, and it is the gate that protects the phases before it:
`TestEveryDeclaredAddAnchorIsLive` walks every unit's strip rules and fails on a dead
rule, a dead anchor, an anchor inside its own span, or an anchor a sibling strip removes.
The stale `.templ`/handler half of the anchor table is deleted with it, and the package is
registered in the capability registry so a new package cannot ship ownerless.

Phase 1 has landed its two gates: `rbac.RoutePolicyProblems` walks a COMPOSED router (so a
recipe's runtime route is checked, not invisible), and `dbschema.MigrateWith` gives a recipe's
SQL a namespaced path into the same ledger the core schema uses.

### What is not done, measured

| Gap | Evidence |
|---|---|
| **Nothing is wired.** The engine has zero production callers | `grep internal/recipe` outside the package returns nothing |
| 21 hardcoded mount sites must become declarations | `MountFeature(` appears 21 times across `chi.go` (11), plus delivery, license gate, perf, route |
| The migration engine cannot carry a recipe's schema | `dbschema.Migrate(sqlDB *sql.DB)` takes **no filesystem**, so a recipe's SQL has no path into the ledger |
| One unit still owns a loose core file | `presence.go` is listed as owned by `whiteboard` in the capability registry |
| 223 installer tests encode the current model | `internal/installer/*_test.go` |

### The blocker the spikes did NOT surface, and it is the important one

**The repository's strongest safety net reads the source tree, not the router.**
`internal/rbac/coverage_test.go` builds its route list by walking `features/**/chi.go` and
regex-matching `r.Get("...")` patterns — it never boots the server. Every probe in
`internal/capabilities` and `cmd/check-flatdeps` reads files the same way.

That has a consequence the plan did not carry: a recipe registered through the seam
contributes routes at **runtime**, so those routes are invisible to a source-walking test.
A refactor that moves mounting to declarations would make the RBAC coverage gate
progressively blind, and it would stay green while it lost its coverage. That is the same
class of silent failure Spike 0 found in the anchor table — a gate that cannot see the
thing it exists to check.

So the gate must be re-pointed **in the same change** as the wiring: it has to read the
declarations (which are data, and therefore easier to read than Go source), or boot the
router in-process and walk it. Either is cheap. Discovering it afterwards is not.

### Verdict on "refactor now"

**Not as one change, and not before two preconditions.** But yes in a sequence that keeps
`main` green at every step, because the recipe model was designed so each step is
additive:

| Phase | Work | Why it is safe |
|---|---|---|
| **1** | Re-point the gates to read declarations; add the recipe schema path to the migration engine; make `presence.go` an owned seam | No behavior changes. This is the phase people skip and then cannot recover from |
| **2** | Convert **one** unit to declare itself while keeping its hardcoded mount | The engine is proven against real routes, a real table and real policy rows; deleting the hardcoded line is the proof |
| **3** | Convert the rest, one per PR | Each PR is mechanically verifiable and independently revertable |
| **4** | Retire the anchor machinery and the scaffold prompt | Only after nothing calls them |

**Do not start with phase 1 skipped.** The concrete failure mode is: wiring lands, tests are
green, RBAC coverage quietly stops seeing most routes, and the next route that ships without
a `user` row passes. That is a security gate losing its teeth during a refactor that looks
successful.

### The two preconditions, stated as gates rather than intentions

1. **A test that fails when a recipe route has no policy row.** Until that exists, phase 2
   makes the project less safe than it is today.
2. **A test that fails when a recipe's declared table is missing from the ledger.** The
   migration engine change is not optional and not deferrable; it is the reason the schema
   split exists.

Neither is hard. Both are the difference between a refactor and a rewrite that looks like
one.

### Every phase is RED → GREEN → PROVE

No step below is "write the code and trust it". Each phase names the test that must fail
**first**, the implementation that turns it green, and the hand-mutation that proves the test
can go red again. A phase whose test cannot be made to fail is not finished, it is unverified.

The rule from `skills/gogogo-coding-standards/references/go-testing.md` applies and is not
negotiable: acceptance comes from the human or a fresh context, not from the implementer; the
mutation is applied by hand, watched to fail, and **reverted** — never committed.

#### Phase 1 — make the safety nets see recipes (no behavior change)

**1.1 Re-point the RBAC gate.** Today `TestRBACCoverageMatchesRoutes` builds its route list by
walking `features/**/chi.go` and regex-matching patterns. That is the gate that goes blind.

**Status: DONE (2026-11-01), with the mechanism changed — see the note after the table.**

| Step | Action | Expected |
|---|---|---|
| **RED** | `TestRuntimeMountedRouteNeedsAPolicyRow`: mount a fake recipe route (`GET /recipe-probe`) through the real `Router.MountFeature` seam and assert the gate names it | **fails** to compile — no runtime inventory exists, and the source scan cannot see a runtime mount |
| **GREEN** | `rbac.RoutesWalked` + `rbac.RoutePolicyProblems` (production, `internal/rbac/coverage.go`); the composed tree is checked at the end of `TestBuildChiMux`; both intent tables move to `internal/rbac/policy_intent.go` so one copy exists | passes — the probe route is reported, the real tree is clean |
| **PROVE** | Delete `p, user, /api/todos, .*` from `policy.csv` | BOTH gates go **red**, naming `GET`/`POST /api/todos` |
| **Regression** | `TestRBACCoverageMatchesRoutes`, `TestCoreRoutesAreScanned`, `TestRBACPublicRoutesArePublic`, `TestRBACPrivateRoutesDenyAnonymous` | all stay green |

What the implementation taught, and where it departs from the plan above:

- **The inventory is the composed router, not a declaration union.** `internal/rbac` cannot
  import `internal/recipe`: the source gate exists precisely so a trimmed tree stays checkable,
  and a plugin import would drag a package the trim may have deleted into a core test. So the
  runtime gate walks a tree the boot path built. The source scan stays as the pre-boot half;
  both now read the same intent tables, and only the runtime half can see a recipe.
- **Pattern-level, not method-level, for walked routes.** The policy is written as "object =
  path, action = method; method checks inside feature routers stay theirs", and `MountSubtree`
  / `Handle` register a path for every verb. Demanding one row per verb would demand decisions
  the edge does not make. The source gate keeps its method precision.
- **The runtime gate lives in an existing fixture on purpose.** A new `internal/server` test
  file that wires feature handlers becomes a new file every trim unit must edit, or the trim
  proof build fails on a dangling import. Reusing `TestBuildChiMux`'s tree costs three lines
  and no trim surface.
- **It found two real gaps on the first run**, both invisible to a source scan:
  `/api/security/csp-report` is mounted from a CONSTANT (`r.Post(cspReportPath, …)`), so the
  regex never matched it; the policy grants it to `anonymous` while the intent table never
  declared it public, and the two lists had disagreed since the route shipped. `/static` is the
  `MountSubtree` PREFIX, which registers the bare path with no policy row, so `GET /static` was
  admin-only while `GET /static/*` was public. Both are fixed here.
- **The intent tables were duplicated state, not test-only metadata.** They decide what the
  gate expects, so they moved to production (`policy_intent.go`) where the runtime gate can
  read the same copy. A second copy would drift the way the anchor table did. That move also
  moved a TRIM target: the perf unit drops the `/debug/*` entries from that file, and the
  manifest drift check (`TestCheckTreeAgainstRepoRoot`) caught the stale path immediately.
  Every later conversion that moves code between files must move its strip markers with it —
  the gate for that already exists, which is the point of phase 1.

**1.2 Give a recipe's schema a path to the ledger.** `Migrate(sqlDB *sql.DB)` reads one embedded
FS and sorts filenames.

**Status: DONE (2026-11-01).** `dbschema.MigrationSet{ID, FS, Dir}` + `MigrateWith`; `Migrate`
delegates. Versions land as `<id>/0001_init`; core keeps its unprefixed names. `TestRecipeMigrationReachesLedger`
covers two sets that both number from 0001, the core namespace, and per-set idempotence. PROVE:
dropping the prefix makes the ledger rows disappear (the core row already owns `0001_init`, so the
recipe is skipped entirely — a silent no-op, not a loud clash).

| Step | Action | Expected |
|---|---|---|
| **RED** | Add `TestRecipeMigrationReachesLedger`: a fake recipe with its own `fs.FS` containing `0001_init.sql`; after migrating, expect a `schema_migrations` row | **fails** — no way to pass a filesystem |
| **GREEN** | Migrate over a set of filesystems, version-prefixing by recipe id; core's own files keep their current versions | passes |
| **PROVE** | Drop the prefix when recording the version | the test goes **red** (two recipes collide on `0001`) |
| **Regression** | `TestMigrateIsIdempotent` and the schema tests | green, and idempotence must hold **per filesystem** |

**1.3 Make `presence.go` an owned seam.** It is the only core file a unit claims.

**Status, 2026-11-03: resolved by narrowing the invariant, not by moving the file.**
The broad rule ("no unit owns a file in another unit's package") does not describe the copy
path, and the registry's `Dirs` are trim metadata, not an ownership map: `goakt` and `room`
share `features/room`, `components` nests under `features/todo`, and `genui` claims
integration files inside `todo`/`config-view` — all deliberate. A gate over that map is either
vacuous or permanently red, and a capability-level gate was tried and produced exactly those
false positives.

The copy hazard has a precise shape and now a precise gate: **an installed recipe may not ship
a path another installed recipe's receipt claims.** `assertNoReceiptCollision` refuses it before
any write, so an uninstall cannot delete a neighbour's file and a second install cannot
overwrite the first; the test installs a second recipe shipping the same path and finds the
first untouched. `internal/server/presence.go` stays claimed by `whiteboard` while trim exists:
it is the file the trim deletes, and moving a `ChiStack` method into a unit package is an import
cycle. When ownership becomes data (phase 4), the file is core and the unit's `Files` entry
drops.

| Step | Action | Expected |
|---|---|---|
| **RED** | Add `TestNoUnitOwnsAFileInAnotherUnitsPackage`: the registry must not list a path inside a package another unit owns | **fails** today — `whiteboard` lists `internal/server/presence.go` |
| **GREEN** | Move the registration behind a nil-safe seam in a file `whiteboard` owns; the server calls it if non-nil | passes |
| **PROVE** | Remove the nil check | the presence tests panic or the boot smoke fails → **red** |
| **Regression** | `TestBothFeaturesBypassPeerSetWhenGoAktRuns` (the presence seam test) | green |

Gate for leaving phase 1: **the full suite green with zero behavior change**, and each of the
three PROVE mutations has been observed to fail and been reverted. Phase 1 is the only phase
where "nothing broke" is the whole deliverable, so it is also the one where an unobserved red is
most likely to hide.

**Where phase 1 stands (2026-11-01): 1.1 and 1.2 are DONE, proved by mutation; 1.3 is parked with
its rule narrowed.** The two gates that landed are the ones the later phases depend on — a recipe
route cannot mount without a policy decision, and a recipe's SQL reaches the ledger under its own
namespace. 1.3 is the only item left, and it is a presence refactor rather than a wiring edit, so
it belongs to a change that can run the presence suite in full.

#### Phase 2 — convert one clean unit (`review` or `credits`), hardcoded mount kept

This phase is the risk probe the plan otherwise lacks, and it is why no further spike is
needed. `review` and `credits` own no core file, so they are convertible without touching 1.3.

| Step | Action | Expected |
|---|---|---|
| **RED** | Add `TestRecipeDeclarationMatchesMountedRoutes`, table-driven over registered recipes: every declared route is mounted, every mounted route is declared | **fails** — the unit has no declaration |
| **RED** | Add `TestRecipeTablesAreInTheLedger`: every declared table exists after boot | **fails** — the table still arrives via the core migration |
| **GREEN** | Add the unit's `Declaration` + receipt; register through the seam; keep the existing `MountFeature` call | passes; the unit now mounts **twice**, so the duplicate guard panics |
| **GREEN** | Delete the hardcoded `MountFeature` line | passes — the seam alone carries it. **This deletion is the proof of the phase** |
| **PROVE** | Remove one route from the declaration while the code still registers it | `TestRecipeDeclarationMatchesMountedRoutes` goes **red** |
| **PROVE** | Remove the declaration's table entry | `TestRecipeTablesAreInTheLedger` goes **red** |
| **Regression** | the 223 installer tests, RBAC coverage, flatdeps, boot smoke | green |

The double-mount panic is not a hazard to engineer around — it is a free assertion. If the seam
and the hardcoded line ever disagree, boot says so and names both owners.

**Status, 2026-11-01: the conversion half is DONE for `review`; the table half is proved by
fixture, because `review` owns no table.**

- The provider lives beside the routes (`features/review/handlers/recipe.go`), so a route and its
  declaration cannot drift without one edit.
- The registry hangs off the composed `Router` (`mux.recipes`). A separate builder function would
  have forced the gate to reconstruct handlers, which is new trim surface for every unit; reading
  the tree's own record costs nothing and cannot disagree with what mounted.
- Keeping both the hardcoded mount and the seam panicked exactly as predicted:
  `POST /api/review/run registered twice (by review and review)`. Deleting the hardcoded line was
  the last step, and `TestBuildChiMux` now asserts declaration ≡ mounted routes in both directions.
- PROVE: dropping one declared route makes the gate red (`mounts POST /api/review/run without
  declaring it`); the table gate's negative is a fixture declaring `todos` plus a table that does
  not exist. A unit WITH a table is the missing probe — phase 3 supplies it (`todo`), and the
  place to strengthen is there, not by inventing a table for review.
- Trim coupling, again: the unit's line drops moved from `chi.go`'s mount to `chi.go`'s
  `r.recipes.Add(...)`, and the manifest drift check named the stale markers immediately. The
  registry import stays because `internal/recipe` is not a trimmed unit.

The rest of the phase — nav entry, panel/slot surface, receipt for a real install — is NOT done:
the seam mounts routes and nothing else, and a receipt has not been written for a real unit yet.

#### Phase 3 — convert the rest, one unit per PR

**Status, 2026-11-03: `credits` converted, and the recipe id is not the package name in one
respect worth recording — the SCOPE class must follow the registry's `Kind`, not the directory.**
`features/credits` is a *plugin* (it serves todo metering, not users), so its provider file carries
`// SCOPE:plugin`; `TestScopeAnnotationsMatchRegistryKind` fails otherwise. The gates needed no new
test: the two phase-2 checks are table-driven and named the credits route the moment it was
dropped from the declaration.

**Status, 2026-11-03, second conversion: `config-view`.** Same shape, no new test: the gate named
`POST /config/users/revoke` the moment it was dropped from the declaration. Two facts worth
recording because they recur for every remaining unit:

- **The unit's drop marker moves with the mount, not with the unit.** The strip entry replaced
  `r.MountFeature("config", configH.Routes())` with `r.recipes.Add(configH.Recipe())`; leaving the
  old substr would have made the drift check pass (the line is gone from `chi.go`) while trimming
  silently stopped removing the mount. The check that catches this is the trim build, not the
  manifest parse.
- **The `config-view` id is the recipe id, not the package name.** The declaration lives in
  `features/config/handlers` but registers as `config-view`, so the SCOPE class comes from
  `internal/capabilities/registry_identity.go` (`KindFeature`) and the owner in the claim table is
  the unit id. Reading the directory name instead would have produced a second ownerless surface.

No new tests. The two checks from phase 2 go table-driven over every registered recipe, so each
conversion is automatically covered and **each PR is independently revertable**. A PR that
converts a unit must not touch a second unit's declaration; that rule is what keeps the PRs
independent rather than merely small.

**Decisions taken, and what each unblocked (2026-11-03).** After `review`, `credits` and
`config-view`, every remaining route-owning unit needed a decision rather than a line swap:

- **An optional unit registers no provider.** `room` is nil when its engine did not start, so a
  declaration would claim nine routes the composed tree does not serve — the route gate reported
  exactly that, nine times. Decision: `Recipe()` answers nil when the handler is nil, and
  `Registry.Add` treats nil as a no-op (`seam_test.go` pins both halves). Absence of code becomes
  absence of declaration, which is the only shape that keeps the "mounted without declaring"
  direction exact. Rejected alternative: teaching the gate to skip nil-mounted providers — that
  weakens the direction for every unit to serve one. `room` is converted on top of the rule.
- **`todo` converts routes-only; its table waits for the schema split.** `todos` is in
  `isCoreTable` and created by the core migration, so `Validate` refuses the declaration — and it
  would be a lie even if allowed, because core creates the table. The phase-2 sentence that named
  `todo` as the phase-3 table probe was wrong: the *unit* probe moved to the schema split (a
  prefixed, recipe-owned migration), while the *gate's* positive direction is now proved by a
  fixture that declares a real table (`notes`), so "no problems" can no longer mean "nothing
  checked". The unit still ships installed and is removed manually; mounting through the seam does
  not decide whether the reference slice ships as a recipe (Open questions, item 2).
- **Notes, whiteboard, account, landing and notifications are not phase-3 work.** They are not
  dropped; each has a phase and a named decision:

  | Unit | Lands in | Decision it needs |
  |---|---|---|
  | `notes` | **done (2026-11-03)** | routes-only, no table: the `notes` rows are still core-created, so the declaration waits for the schema split |
  | `whiteboard` | **done (2026-11-03)** | routes-only, no table, for the same reason; the loose file (`presence.go`) stays whiteboard-claimed while trim exists |
  | `account` | **done (2026-11-04)** | the "mounts after `buildChiMux`" objection was an artifact: built from the same `register.Deps` the root already holds, it declares its 12 routes beside them and mounts through the seam. Still manual-removal identity UX; it is now a recipe like the rest |
  | `landing` | **done (2026-11-04)** | the split is exactly "serves `/`" (a declared route, mounted through the seam) versus "answers every unmatched path" (`r.NotFound`, core policy, one trim-able line). The landing component still renders the not-found page |
  | `notifications` | **done (2026-11-04)** | the "shares its bus with the session stream" objection only meant the root must own the construction: it builds handler and bus inside the composition (kept on the tree for the middleware step) and the six routes mount through the seam |
  | `todo`'s table | schema split | `todos` is core-created and in `isCoreTable`; the declaration moves in when a prefixed recipe migration owns the table |

What stays open: the table gate proves only `declared ⊆ existing` — "this unit owns a table and
does not declare it" has no gate. That direction needs the per-recipe SQL and the schema split,
and it is the one item the `todo` probe was reaching for.

**`whiteboard` and `notes` were the last conversions, and 1.3 is what unblocked them.** The rule
that kept them out earlier ("do not convert anything touching `collab` before 1.3 is exercised")
was a sequencing guard, not a property of the unit: with the copy hazard gated at the receipt
layer, the conversion is the same routes-only change as the others.

#### Phase 4 — retire the anchors and the scaffold prompt

This phase's RED landed early, at the end of phase 1's first day: the generalized
gate was cheap to write and it immediately found real drift.

| Step | Action | Expected |
|---|---|---|
| **RED** | `TestEveryDeclaredAddAnchorIsLive`: for every unit rule, the strip must match a live span; a declared anchor must be a whole line that exists in the template tree, outside the rule's own span, and surviving every sibling strip of the same file | **failed** — the whiteboard desktop rule pinned a comment *prefix*, and the genui rule's path default named a line the todo handler no longer carries |
| **GREEN** | Delete the unreachable `.templ`/handler table entries and their constants; pin the whiteboard rule to the full comment line; keep the two live path defaults (`cmd/web/main.go`, `cmd/desktop/main.go`) | passes |
| **PROVE** | Restore the prefix anchor on the whiteboard desktop rule | the gate goes **red**, naming the rule and the line |
| **Regression** | the installer's trim/add tests, `TestAddManualWiringPinsReversibility`, `TestAddDagnatsRestoresBootOrder`, the trim-preset proof builds | green |

Two facts found while landing it, both sharper than Spike 0's prose:

- **`add` never reaches the anchor table.** `addManualWiring` reports every unit as
  manual and `refuseIrreversible` refuses before any mutation, so the dead anchors
  were *transitively* dead, not merely stale. That is why deleting them changed no
  behavior — and why repairing them would have produced a plausible table nobody
  can execute.
- **A prefix is not an anchor.** `findAnchor` compares whole lines, so
  `addBefore: "// desktopConfig loads config"` could never match the comment it
  named. The gate's whole-line property is what makes that visible; the old
  three-row probe looked for a *substring* and would have called it live.

What remains for phase 4 proper: retire the two path defaults and
`spike_anchor_test.go`'s mechanism probes once the catalog is the entry point and
nothing calls the installer's add path.

**Decision, 2026-11-04: `add` stays, so this phase does not start yet.** The phase's own
precondition is "nothing calls the installer's `add` path", and the honest reading of it is not
"delete the command": `trim` is still the only working removal path for a unit that ships in-tree,
and `add` is the only way back from it. Removing `add` today would make removal one-way —
strictly worse than the anchor drift this phase exists to end — while the catalog only installs
recipes that were never in the tree. The trigger is explicit and belongs to the next phase of
work: **the first in-tree unit that ships as a receipt** (its files, its declaration and its
ownership in data). At that point the anchor table has no remaining caller, the two path defaults
have no remaining target, and this phase becomes a deletion rather than a decision.

Two rules for this phase, both learned from Spike 0:

- **`spike_anchor_test.go` is deleted with the machinery, not before.** Its asserts
  are the recorded evidence that the mechanism drifted. Its table probe is already
  gone because the generalized gate replaced it in the same change; the other four
  probes test behavior this phase removes.
- **Write the live-tree gate before the first conversion, not at this phase.** It
  started failing the day it was written, and every phase in between now has a gate
  that sees the drift it is most likely to cause.

#### What is NOT red-proofable, and how it is verified instead

Three things in this refactor cannot be guarded by a test that fails first, and pretending
otherwise would be worse than saying so:

| Item | Why | What is done instead |
|---|---|---|
| "The catalog is worth using" | No test expresses product value | A real recipe is built end to end in phase 2 and a human reviews the diff |
| "The surface is the right shape" | It becomes obvious only against a second and third unit | Phase 3's per-unit PRs are the review points, each revertible |
| The recipe-migration call site | No in-tree unit owns a table, so removing `r.recipeErr = bootRecipes(...)` from the root would leave every test green: the gate has no positive case to lose | The function the root calls (`applyRecipeMigrations`) is tested against a real sqlite ledger, twice (apply lands the table, second call is a no-op) and once for the no-store error; the call site itself becomes provable the day a unit moves a table into a prefixed recipe migration |

### Do the remaining gaps need more spikes? — **no, and here is the test**

A spike is justified when the *mechanism* is unknown. Everything left is *integration* whose
mechanism is already proven, and three checks settle it without spending a day:

| Check | Result |
|---|---|
| Does the real composition root satisfy the recipe seam's mount contract? | **Yes, exactly.** `Router.MountFeature(owner string, h chi.Router)` is byte-for-byte the `Mounter` interface, so wiring is a zero-adapter call — no signature churn, no translation layer |
| Can a recipe's schema reach the ledger? | **Yes, with one design decision.** `Migrate` reads one embedded FS and sorts filenames to derive versions. Per-recipe migrations mean either merging filesystems or prefixing version names; the ledger's primary key already accepts a prefix, so the work is naming, not structure |
| Is the loose-file blocker big? | **One file.** `presence.go` is the only core file a unit claims; making it an owned seam is mechanical |

So the answer is: **stop spiking, start phase 1.** The signal that a project has over-spiked is a
proposed spike whose output would be "now implement it" when implementing it is the cheaper path.
That is exactly where this stands — the remaining unknown is not "does the seam work" but "does
it hold against real routes", and that is answered by building it, not by probing it.

**One caveat, and it belongs to phase 2, not to a spike:** converting the first real unit is
the remaining risk, because the engine has never touched a real `.templ`, a real policy row or a
real job. That risk is carried by **doing phase 2 with a clean unit** — `review` or `credits`,
which own no core file — and by not planning phase 3 until phase 2 has landed. Treating phase 2
as the risk probe is cheaper and more honest than a separate spike that only simulates it.

What would justify one more spike: if phase 1 reveals that the route list cannot be produced
from declarations without duplicating the router (that is the gate-re-point risk), stop and
probe that specific question before phase 2. Until then it is speculation.

## Transitional machinery: SCOPE annotations and trim (what retires, what survives)

The endpoint does **not** depend on per-file `// SCOPE:` annotations, and it does not depend on
trim. Both answer "what can be removed"; the recipe model replaces that question with ownership —
**core is whatever no receipt claims**, and a declaration says which routes, tables, jobs and
slots a recipe owns.

- **`SCOPE:` retires.** The three-way class (core / plugin / feature) is a removal taxonomy. With
  recipes it collapses: a file is either core (nobody's receipt) or owned by exactly one recipe.
  A per-file annotation beside a receipt would be two sources of truth for the same fact — the
  drift class this page is about. What survives is a table/route ownership check that refuses a
  recipe claiming a core object, plus the operator-facing "what breaks if I delete this", which
  the receipt and the catalog entry already answer.

  **Why it still earns its place today, and why that ends:** for a file with no receipt — which is
  every file — the annotation is the only cheap answer to "may I delete this?", and it is a
  design forcing function: an author must state a removal story when the file is created, not
  when someone tries to remove it. It also survives where a gate needs a *class*, not an owner:
  `docs/scope-taxonomy.md`, the installer's unit map, and the review guidance for "never delete
  core without asking". What makes it obsolete is ownership becoming data: once a receipt names
  the files it installed and a core manifest names the rest, `make check-scope` can assert the
  same property from those two sources. The annotation is a stand-in for a missing receipt, not a
  category the recipe model needs.
- **Trim retires with it.** Trim removes a capability by editing core files through string
  anchors; Spike 0 measured why that cannot carry N authors. The catalog plus a stable core
  answers the same need at install time, so anchors, span insertion and the scaffold lists go in
  phase 4. Until phase 3 finishes, trim is still the only working removal path, and every
  conversion must keep its strip markers in step.
- **Not optional:** the composition root stays the one place that flattens capability routers
  (today `Router.MountFeature`), and the RBAC gate keeps walking that composition. Neither is a
  removal taxonomy; both are what a recipe leans on.

## Gap analysis, 2026-11-01

| Area | State |
|---|---|
| Engine (`internal/recipe`) | Seam, receipt, drift, planner, slots, all tested. Production callers: the seam (the converted units), and `Plan`/`Apply`/`ReceiptAfter`/`Check` through `gogogo recipe`. Slots have two production callers: `settings.section` in `/config` and `nav.sidebar` in the shell rail |
| Phase 1.1 RBAC runtime gate | Done, proved; found two live policy gaps |
| Phase 1.2 schema → ledger | Done, proved; namespaced versions |
| Phase 1.3 `presence.go` | Resolved by narrowing the invariant; the copy hazard is gated at the receipt layer (`assertNoReceiptCollision`) and the file stays whiteboard-claimed while trim exists |
| Anchor gate (phase 4 partial) | Done; stale `.templ`/handler table deleted |
| **Phase 2 wiring** | **Done for `review`** — declaration beside its routes, mounted only through the seam, gates red-proofed. Nav/panel surface, receipt and slots still open |
| **Phase 3 conversions** | **Converted:** `review`, `credits`, `config-view`, `room`, `todo`, `notes`, `whiteboard`, `landing`, `account`, `notifications` — routes only, since no in-tree unit owns a table yet; the first one that does declares it and ships DDL through `recipe.Migrator`. Nothing outside the seam is left except the composition root itself |
| Catalog / install command | **Install/update/uninstall/status wired** (`gogogo recipe …`, 2026-11-03): tree + receipt + generated registration + drift, no anchors. **Install finishes the tree (2026-11-06):** it writes the declared routes' policy block, regenerates pages (`go tool templ generate`) and queries (`sqlc generate` when on `PATH`), and uninstall removes the block plus the generated output of what it deleted. The **catalog entry format is published, validated and consumed** (`recipe install --entry`, hash-verified); hosting and download are not built. The reference recipe is `_extensions/pomodoro` |
| Slots in a real page | **`settings.section` renders in `/config` and `nav.sidebar` renders in the shell rail** (2026-11-04), both with a proof and neither page learning a recipe id; the rail also refuses a contribution whose href is not root-relative. `record.row-actions` renders on the shared todo row (2026-11-04) with a registration-time contract (label, closed verb, root-relative template naming the row once). `dashboard.card` keeps its shape but has no host page, so a declaration for it is refused at registration, install and review rather than accepted and dropped |
| Schema split enforcement | **Enforced at the declaration level (2026-11-04):** `recipe.Migrator` + `Registry.MigrationSets()` feed `dbschema.MigrateWith` from the composition root, and `recipe.ValidateSchema` refuses both a declared table with no `CREATE` and a `CREATE` the declaration omits — comments stripped, `ext_<id>` prefix and the core-table refusal reused. **A recipe-owned table now has a query path (2026-11-05):** install mirrors its DDL flat into `internal/db/recipe_schema/` (`<id>__<migration>.sql`), `sqlc.yaml` reads that directory, and `recipe status` reports a mirror that stopped matching the tree. No in-tree unit declares a table yet, so the shipped mirror is empty and the wiring's positive case is still a root-level fixture (see "What is NOT red-proofable") |
| SCOPE + trim retirement | Phase 3 has landed; both stay until ownership is data. Decision (2026-11-04): `add` is not retired while `trim` is the only removal path for an in-tree unit — deleting the way back first would make removal one-way. Trigger: the first in-tree unit that ships as a receipt |

## Gap analysis, 2026-11-06

Re-read against the tree rather than against the plan's earlier claims. Several rows closed;
the rest name the decision each one waits on, and none of them is waiting on a spike.

| Area | State |
|---|---|
| SQL text boundary | **Closed (2026-11-06).** `recipe.ValidateSchema` scans each statement's target, so an `ALTER`, `DROP`, index, seed write or trigger naming a core table is refused at boot. A statement on the recipe's own `ext_` table and a foreign key that only *references* a core one stay legal |
| Toolchain pin in a scaffolded tree | **Closed (2026-11-06).** The generated `AGENTS.md` replaces the template's, and the prove step's pin test requires that file to name the Go version — so a real scaffold failed its own proof and the failure read as a broken trim. The section now states the pin `go.mod` declares |
| Dead anchors in `add.go` | **Closed by the generalized gate.** `TestEveryDeclaredAddAnchorIsLive` walks every rule of every unit; `--check` was never the place the class could be caught, because `add` refuses before it mutates |
| Phase 4 — `add`, `trim`, the scaffold prompt, `SCOPE:` | **Open, gated on evidence that does not exist yet.** `.recipes/` holds no receipt, so no in-tree unit owns its files in data and `trim` is still the only removal path for one; deleting `add` first would make removal one-way (`SCOPE:` retires in the same step, see the transitional-machinery section) |
| `todo`'s table | **Open, and it is a human decision, not a mechanism.** `todos` is created by a core, append-only migration, so moving it to a prefixed recipe migration is a data migration on the one slice every fixture, smoke and proof copies |
| `dashboard.card` | **Open by design, and now refused rather than dropped (2026-11-06).** There is no dashboard page and inventing one to host a card is the page-first mistake the slot shapes exist to prevent. `HostFor` names the page that renders each slot, so registration, install and the catalog review all reject a slot this build does not draw, and `recipe status` reports a declaration whose code contributes nothing. `dashboard.card` keeps its shape and gains no host until a page exists |
| Catalog hosting / download | **Deferred past v1.** The entry format is published, validated and consumed by `recipe install --entry`; fetching one over the network is distribution, not the model |
| `new feature` | **Not on the retirement list.** It writes one feature into the tree the user already owns — no catalog, no receipt. Open question 7 retires `scaffold`, `trim` and the `Scaffold` axis (the guided prompt and its unit lists), not the command that builds a slice inside a project |

## Open questions for the human

What remains is written as decisions where the answer is now clear, because an agent
reading this needs an answer, not a menu.

**Settled by this revision:**

0. **Is a gogogo extension a pinned package or an evolving recipe?** A recipe. That is
   the correction this revision makes, and it reorders everything: the file set, the
   receipt and the drift check come before any catalog work.
1. **Would the current plugins become core?** Yes — for the official default set,
   and the removal class collapses with it. `DependsOn` keeps describing what each
   shared unit serves, so the catalog does not lose the information.
2. **Do we need the ability to remove a specific core unit?** No. Removal stays
   possible as an unsupported fork; it stops being a supported operation. This was
   the single largest simplification this revision made — it deletes a whole class
   of tooling (anchors, span insertion, module rebasing) from the recipe path.
3. **Can someone alter the core?** Yes — a fork, documented as a fork, with no
   merge machinery promised. The catalog tells them what is replaceable and safe to
   override; everything else is owned by their diff.
4. **Install/uninstall with an official catalog plus community entries?** The
   catalog holds **curated entries only**. Everything else — a personal recipe, an
   in-module one, a private module — is installable without any catalog row.
   Submitting for review is the author's decision, never a technical prerequisite.
5. **Public packages, in-module, or staged?** Staged (Option C), in-module first.
   The first tier needs no import refactor because the recipe surface can be new code.
6. **Slots or hooks?** Slots only, with fixed shapes and a declared surface. A recipe
   may present data inside a core page and post to its own endpoint; it may never add
   a step to a core handler or a column to a core table.
7. **Do `scaffold` and `trim` survive?** No. The catalog plus a stable core
   supersedes all three lists, and the prompt has nothing left to compress. They are
   superseded rather than broken, so they are not repaired — they are subsumed and
   then deleted, in that order.
8. **How is a recipe updated after the user edits it?** Untouched files fast-forward
   by pristine hash; touched files go to `git merge` with the receipt as the third
   leg; the declaration upgrades through the engine with no merge at all.

**Still open, and now unblocked by evidence rather than by argument:**

1. **Which slots ship in v1?** Settled for two (2026-11-04): `settings.section` (a real page
   that already owns the frame) and `nav.sidebar` (the rail is a real surface, and the collision
   the doc feared — extensions mixed into the built-in module list — is resolved by rendering
   them as their own group from a separate registry, so `ui.Modules` stays the shell's own list).
   `record.row-actions` shipped in the same pass (2026-11-04) with a stricter contract than the
   nav entry: a label, a closed verb, a root-relative template naming the row exactly once with
   `{id}`, and rendering on the todo row — the core page substitutes the id, the recipe never
   hands it a component. Known limit, recorded rather than hidden: the row renders every
   contributed action on every row the page shows, because the page has no per-action permission
   data; authorization stays where it already is for deletes — the recipe's own endpoint scoped
   by the session (`canDeleteRow` is the precedent). `dashboard.card` stays registry-only: this app has no dashboard, and
   inventing one to host a card is the page-first mistake.
2. **Does the reference slice ship installed or as a recipe?** `todo` is both the
   catalog's best example of a modifiable recipe and the thing every proof, smoke and
   fixture copies. It can be both, but that combination deserves one deliberate decision.
3. **Is a pinned-module tier shipped at all?** The recommendation is no for v1: a dual
   mode doubles what has to be proven, and the in-module tier already satisfies the
   template's purpose.
4. **How much of the schema split lands in v1?** Settled for the declaration level
   (2026-11-04): the ledger prefix is the recipe id, the root applies each recipe's own
   filesystem, and DDL must match the declaration in both directions. The SQL text itself was
   the remaining hole and **closed on 2026-11-06** (below). The first unit to move a table from
   `isCoreTable` to a prefixed recipe migration is still ahead — `todo` remains the intended
   probe.

   **The blocker that surfaced on inspection (2026-11-04) is closed (2026-11-05).**
   `internal/db/sqlc.yaml` takes its schema from `../dbschema/migrations`, the CORE set, so a
   table arriving through `recipe.Migrator` was invisible to `sqlc`: the first table-owning unit
   could have no generated queries, and writing its SQL by hand is the one rule that keeps
   queries and schema from drifting. The decision was **install-time codegen**, and the shipped
   shape is the installer writing the mirror beside `sqlc.yaml` —
   `internal/db/recipe_schema/<id>__<migration>.sql`, read by a second `schema:` entry.

   **Two measurements changed the layout the plan first assumed.** `sqlc` reads only the top
   level of a `schema:` entry — it does not descend into subdirectories — and it skips a
   directory whose name starts with a dot. A mirror at `.recipes/schema/<id>/…` would therefore
   be silently inert: no error, no table, just queries that do not compile against a schema
   nobody generated. Hence flat, visible, id-prefixed files, one directory shared by every
   recipe (the prefix is what keeps two recipes' `0001`s apart, and what scopes prune and
   uninstall).

   **Three properties hold it together.** The mirrored bytes come from the tree after the plan
   applied, not from upstream, because the runtime embeds the tree's files — mirroring upstream
   would leave codegen reading one schema while the database applies another, which is the
   failure mode this whole item exists to remove. Pruning is scoped to `<id>__`, so an update
   that drops a migration removes its DDL and an uninstall takes only its own. And `recipe
   status` compares mirror to tree, so a migration edited without a reinstall is reported
   instead of silently disagreeing with codegen.

   **What it costs the author:** a table-owning recipe ships its DDL as
   `<package>/migrations/**.sql` — the path segment is the convention the installer can anchor
   (it cannot know which directory `Migrations()` embedded) — and the runtime reads that same
   subdirectory, not the filesystem root (2026-11-06: reading the root applied nothing and booted
   with no table, silently). The operator has no follow-up step: install runs `sqlc generate`
   itself, and names `make sqlc` only when the binary is absent.

   **The SQL text is gated too (2026-11-06), and the boundary is the target, not the mention.**
   `ValidateSchema` scans every statement, so an `ALTER`, a `DROP`, an index, a seed write or a
   trigger that names a core table is refused at boot — the ledger would have applied it just
   as silently as a `CREATE`. Two things stay legal on purpose: a statement acting on the
   recipe's own `ext_` table, and a foreign key that merely *references* a core one (`user_id
   TEXT REFERENCES users(id)`), because a reference is a dependency and ownership is what the
   boundary is about. Banning the mention would have banned the correct way to scope a
   recipe's rows to an owner.

   **Still ahead:** the first in-tree unit to move a table from `isCoreTable` to a prefixed
   recipe migration (`todo` remains the intended probe).

## Related

- [Scope taxonomy](../scope-taxonomy.md) — the removal classes this page keeps as a
  packaging axis.
- [Architecture decision](../architecture-decision.md) — the flat-stack record,
  including why plugins live in-repo today.
- [Features](../features.md) — the capability table and the "adding a new feature"
  procedure the extension surface has to replace.
- [Scaffold choice](scaffold-choice-2026-10.md) — the earlier analysis of the three
  keep-or-drop lists that the extension axis is meant to collapse.
- [Skills policy](skill-reading-policy.md) — why the authoring skill of Spike 4 is
  scoped to the component sections rather than a full read.
