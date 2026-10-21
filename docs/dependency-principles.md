# Dependency principles: write it or take the library

> Six principles, one procedure, one closed approval gate. This page decides
> whether a missing piece is ours to write or someone else's to maintain.

Every change answers one question before it writes code: **is this ours?** The
answer is not "it depends" — it is a short ordered check, and this page is that
check. [Exception to Go](exception-to-go.md) is the same rule applied to
native code; [Costs, tradeoffs](stack-tradeoffs.md) records the choices already
made and why.

## Read this first, if you read nothing else

- **Reuse** what the binary already ships before you create anything.
- **Own the seam, rent the depth** — your domain and interfaces, other people's
  crypto, parsers, protocols.
- **One artifact, one native floor** — nothing new may raise it (a native
  library, a build tool, a runtime library). This one is a ban, not a
  trade-off; the floor is already paid, see [principle 3](#3-the-native-floor-is-paid-once-nothing-new-may-raise-it).
- **Cheap to remove** beats cheap to add.
- **Agent-legible** beats feature-rich.
- **Write the tradeoff** where a reviewer reads it.
- **Four things need a human**: artifact shape, a core substitution, an exit
  that is a rewrite, a maintenance signal under the floor. Nothing else does.

Direction only gets you so far — apply them in the
[order below](#decision-procedure), which is the part that produces a decision.

## Why state principles at all

An agent without a stated direction does not choose at random. It chooses by
familiarity: whatever it saw most in training, which is why a Go template
receives React, `axios`, and a Python OCR service as "the obvious approach".

Principles exist to pre-empt that reflex with a *direction*, a *measurable
tiebreaker*, and a *place to record the reasoning*. Without the tiebreaker the
agent resolves every trade-off in its own favour; without the record the next
agent re-litigates it.

## Three formats, three different jobs

Using the wrong format is how principles rot. An "avoid X unless justified" rule
is an even-over statement wearing a ban's clothes: the agent writes the
justification and proceeds, every time.

| Format | Use it for | Why |
|---|---|---|
| **Hard ban / closed list** | Non-negotiable shape (the artifact) and irreversible decisions | A ban does not trade off, and a *closed* list cannot be expanded by argument |
| **Even-over statement** | Priority between two real goods | Encodes direction, stays arguable, and stops oscillation — but only with one measurable tiebreaker attached |
| **Written trade-off** | Anything past the gate, or any new direct dependency | Puts chosen / rejected / cost / revisit-if where review actually reads it |

Write a ban as a ban. Write an even-over as an even-over. Never let a
paragraph do both.

## The principles

| # | Principle | Even over | Tiebreaker you answer | Authority |
|---|---|---|---|---|
| 1 | **Reuse before you create** | originality | Can you name the symbol or capability that already does it? | agent |
| 2 | **Own the seam, rent the depth** | "own everything" *and* "adopt everything" | Would your own implementation be accepted as correct without reading its internals? | agent |
| 3 | **The native floor is paid once; nothing new may raise it** | *(ban — no trade-off)* | Same C compiler, same musl runtime, same cross-compile, no new runtime library? | human on fail |
| 4 | **Cheapest to remove wins** | cheapest to add | Time-to-remove × blast radius: do its types appear outside its own package? | human when a revert is a rewrite |
| 5 | **Agent-legible over feature-rich** | features | Read the behaviour in the source you have, in one sitting, no codegen or reflection? | agent |
| 6 | **Say the tradeoff where review reads it** | a silent default | One line with chosen / rejected / cost / revisit-if in the PR body? | agent |

### 1. Reuse before you create

Direction: **reuse even over originality.** Search in this order and stop at the
first hit: Go standard library → what this binary already ships → modules
already in `go.mod` → a small owned implementation → a new dependency.

Tiebreaker: can you name the existing symbol? A grep plus the docs map is a
five-minute check, and it is the check that fails most often — the dominant
error in this repo is not adopting a library, it is writing a second version of
something the template already owns.

- Every feature's owned surface is listed in [Unit contracts](unit-contracts.md).
- The todo slice is the reference implementation to copy.
- A duplicate is worse than a dependency: it doubles the code that must stay
  correct and still leaves you maintaining it.

### 2. Own the seam, rent the depth

Direction: **own the seam even over own everything, and rent the depth even over
adopt everything.** This is the principle that decides *where* the boundary
sits, which is the only genuinely hard part of the question.

- **You own**: domain rules, authorization decisions, the wire format at your
  HTTP boundary, session and auth semantics, the interface that isolates
  anything rented, and the removal path.
- **You rent**: cryptography, parsers and encoders, wire protocols, calendar and
  timezone rules, Unicode normalisation, image and audio codecs, and provider
  SDK surfaces — behind one small interface.

Tiebreaker: **would a careful engineer accept your implementation of this as
correct without reviewing its internals?** For crypto, parsers, and protocol
state machines the answer is never, so rent. For "should this row be deletable
by its owner" the answer is always yes, so own it.

Renting the depth buys correctness, because the hard part moves to code with
more readers than you. Owning the seam buys legibility, because the rule the
product actually enforces stays visible in one file.

Never rent the seam: the wrapper that isolates a rented piece is yours, one to
three methods, small enough to replace in an afternoon. `internal/llm` wrapping
the GoAI SDK is the pattern to copy.

### 3. The native floor is paid once; nothing new may raise it

This is a ban, but it is **not** "no CGO": that floor is already spent and a
new feature cannot un-spend it. The ladder that decides *whether* to go native
lives in [Native dependencies: pure Go first](stack-layers.md#native-dependencies-pure-go-first);
this principle is what protects the result.

- **Paid today.** `internal/collab` binds the Loro C library (CGO, rung 3) and
  the Turso driver dlopens a Rust shared object through `purego` (rung 2).
- **Build side.** A C compiler is required everywhere — `gcc` locally,
  `musl-dev` in the image builder, `aarch64-linux-gnu-gcc` for the
  cross-build — and `-tags musl` selects the embedded platform library.
- **Runtime side.** The shipped image is alpine and dynamically linked: it
  needs the musl loader, `libgcc_s`, and a writable cache dir. A static or
  `scratch` image panics at the first DB open — proven in production, not a
  preference.
- **Nothing new may raise either price.** No new native library, no new build
  tool, no new runtime `.so` or `NEEDED` entry, no new cross-compile blocker,
  no JS or Python at build or boot. A dependency that fits inside the floor
  already paid is free on this axis.
- **A runtime opt-out is not a build opt-out.** `NATS_ENABLED`, `DAGNATS_ENABLED`,
  and `GOAKT_ENABLED` no-op a subsystem at runtime; it is still compiled in,
  because this repo has no build tags. Never offer a dependency as "optional"
  on the strength of a flag.

Failing this is not a judgement call and not something to argue in prose. It is
[gate item 1](#when-a-human-must-approve-closed-list), or it does not ship.

### 4. Cheapest to remove wins

Direction: **cheapest to remove even over cheapest to add.** Adoption cost is
the number everyone quotes; removal cost is the one that compounds.

Tiebreaker: time-to-remove × blast radius — how many packages change, and how
many earlier decisions unwind, if this goes away. Two concrete probes:

- Do the library's types appear in signatures outside its own package? If they
  never leave, it is cheap.
- Does it own a store, a wire format, a session, or a migration? If yes, it is
  a **substitution**, not an addition, and substitutions need a human.

Count the costs nobody puts in the pull request: version-churn tickets, escape
hatches for the behaviour the library lacks, the translation layer between its
types and yours, transitive supply chain, and the context an agent burns
reading a large API it did not write.

A dependency whose types are contained in one package is the default shape.
A dependency that becomes the driver, the store, the session, or the template
engine is a core path rewrite wearing a dependency's clothes.

### 5. Agent-legible over feature-rich

Direction: **legible even over feature-rich.** This template's thesis is code
that an agent can read, verify, and extend; a library that buys features by
destroying that property is a bad trade here even when it is a good library.

Tiebreaker: can the behaviour be read in the source you have, in one sitting,
and re-derived — with no code generation, no reflection, no configuration
indirection standing between the call and the effect?

- **Typed surface.** Go types in, Go types out. A stringly or `any`-shaped API
  means you own a translation layer forever.
- **Compile-time over runtime.** Misuse should fail `go build`, not production.
- **Escape hatch.** When it lacks what you need, is there a public extension
  point, or is the answer a fork?
- **No magic.** Registration by `init()`, struct tags driving behaviour, and
  codegen you cannot read all cost the agent more than the lines they save.

The consequence is counter-intuitive and correct: 150 typed lines you own can
beat a smaller call into a library you cannot inspect.

### 6. Say the tradeoff where review reads it

Direction: **a recorded trade-off even over a silent default.** The reasoning
does not survive in a chat log, and the next agent inherits the code without it.

- Anything past the gate, and any new direct dependency, leaves one line in the
  PR body: **chosen / rejected / accepted cost / revisit-if**.
- The reviewer reads the pull request, not the conversation, so that is where it
  goes. `.github/PULL_REQUEST_TEMPLATE.md` already asks the question.
- The changelog belongs to release-please. Record *why* here or in the PR body,
  never as hand-written release narrative.

## Decision procedure

Answer in order. Steps 1–3 decide *what* the piece is; steps 4–6 decide whether
you may proceed without a human.

```
1. Does this binary already do it?
     yes -> reuse it, stop

2. Is the standard library enough?
     yes -> use it, stop

3. Is the missing part deep or shallow?
     shallow (a few rules you can state) -> own it, go to 4
     deep    (spec-driven, correctness-hard, data-heavy) -> rent it, go to 4

4. Does the candidate keep the artifact shape (principle 3)?
     no  -> human gate, stop

5. Does it pass the cheap-to-remove probe (principle 4)?
     no  -> human gate, stop

6. Write the trade-off line, then proceed
```

Two habits make step 3 reliable. **Own shallow logic the first time you need
it, generalize on the third repetition** — the same rule this repo already uses
for vertical slices. **Rent deep logic the first time**, because the second
implementation of a parser is the expensive one.

Step 3 is a question about the *problem*, not about your confidence in the
library. "There is a popular library for it" is not an answer.

## When a human must approve (closed list)

The gate covers four classes and nothing else. Everything outside it: decide,
record, proceed — do not ask.

1. **Artifact shape.** Anything that raises the native floor beyond what is
   already paid: a new CGO or native library, a second toolchain at build or
   boot, a new runtime library, or a service that must run for the app to start.
2. **Substitution.** Replacing the database driver, the store, the session or
   auth implementation, the template engine, the router, or the async layers —
   i.e. rewriting a core path instead of adding beside it.
3. **An exit that is a rewrite.** No bounded removal: types leak into the
   domain, a migration is attached, or the library owns a wire format. The test
   is one question — *would a revert be a rewrite?*
4. **Maintenance under the floor.** One maintainer, no tagged release, or no
   release in the last twelve months, with no substitute available.

Bring the gate a drafted trade-off line, not a question. A gate item with no
written cost is a request to decide on the agent's behalf, and the answer is no.

## The trade-off line

One line, in the PR body, starting with `Deps:` — the prefix is what the PR
check greps for, and a line the check cannot see is a line nobody wrote. Long
enough to be checkable, short enough that it gets written.

```
Deps: <lib>@<version> — rejected <own code | alternative>; cost <transitive
count, churn, translation layer>; revisit if <concrete trigger>.
```

Good: *added `golang.org/x/image` for thumbnail decode — rejected a 40-line
JPEG-only path (PNG/WebP/EXIF orientation missing); cost 1 transitive, pure Go;
revisit if the vendor count grows.*

Bad: *added a library for image support because it is the standard choice.*
That is a preference, not a trade-off, and it answers none of the four fields.

## The rubric: what "agent-legible" scores

Use the same five axes on a candidate library and on your own code. They are not
five separate opinions — one decision usually turns on two of them.

**At decision time** (you can answer these before writing anything):

| Criterion | Question |
|---|---|
| **Locality** | Is the behaviour readable in the source you have, without generated or reflected indirection? |
| **Compile-time failure** | Does misuse break `go build`, or fail silently at runtime? |
| **Typed surface** | Go types in and out, or a translation layer you now own? |
| **Escape hatch** | Missing behaviour: public extension point, or a fork? |
| **Exit cost** | Time-to-remove × blast radius (principle 4). |
| **Dependency delta** | Transitive count, licence, CGO, release cadence, bus factor. |

**After the fact** (these measure the principles over time, not one decision):

| Criterion | How it is measured |
|---|---|
| **Correctness** | The feature does what was asked, and flags invalid input instead of crashing. Remember which side of it you own: correctness is a reason to rent the deep and to own the shallow-and-specific. |
| **Agent cost** | Tokens, rounds, and clock spent getting it working — a portfolio metric, unmeasurable on a single choice. |
| **Error class** | Grammar, stale version, logic, or hallucination. **Stale version is the dependency-specific one**: an agent recalls the API from its training snapshot instead of the pinned module. Read the module source in the go module cache; never trust recall. |
| **Performance** | Median operations per second across runs, plus the gain actually delivered when optimisation was requested. No profile, no claim. |
| **Coexistence** | Translation between types, deploy environments, fallback plan, and a clean exit if it is ever stripped out. |

## Worked cases

Examples are the spec. These are the shapes that actually arrive.

| Case | Verdict | Which principle |
|---|---|---|
| A UUID for a new row | Reuse `dbschema.NewID()` | 1 — adding a UUID module duplicates owned code |
| A route for a new page | `MountFeature` on the capability's own router | 1, 2 — route ownership is the seam you keep |
| Timezone-aware scheduling | `time` first; a tzdata-backed library only if `time` cannot express it | 2 — calendar rules are deep |
| Image decode and resize | Rent a pure-Go module | 2, 3 — deep, and it still cross-compiles |
| A second workflow engine | DagNats already ships | 1 — check [Async layers](async-layers.md) before proposing an engine |
| Another LLM provider | Add behind the owned client in `internal/llm` | 2 — rent the provider, own the wrapper |
| Replacing the Turso driver | Human gate | 4 — substitution, revert is a rewrite |
| A JS UI framework for one widget | Reject | 3, 5 — Templ and Datastar are the shipped direction |
| An abstraction layer over the generated queries | Reject | 5 — direct `dbgen` calls are the seam; a wrapper adds indirection, not capability |
| A job queue on Redis | Reject | 3 — second artifact; goqite already ships |

## Anti-patterns

- **Justification theatre.** "Strong justification, human-approved" applied to
  everything means the gate decides nothing. The gate is the closed list above.
- **Popularity as evidence.** Stars, download counts, and "everyone uses it" are
  familiarity wearing a metric's clothes. Cite a benchmark, a profile, a source
  read, or a transitive count.
- **LOC-delta reasoning.** "It saved 40 lines" ignores churn tickets, escape
  hatches, and the translation layer. Compare ownership cost, not line count.
- **Building the framework.** A local abstraction that only wraps one library is
  a pass-through layer; delete it and call the dependency directly.
- **Reimplementing to avoid a dependency.** The cost of a well-chosen dependency
  is usually lower than the cost of owning its edge cases. That is why deep
  things get rented.
- **Vendoring a whole ecosystem.** One library is a line item; a plugin system,
  a second language, or a framework is an architecture.

## Related

- [Exception to Go](exception-to-go.md) — the native ban and its justification
  procedure; gate item 1 in detail.
- [Costs, tradeoffs](stack-tradeoffs.md) — the rejections already made, with
  cost and revisit trigger for each.
- [Scope taxonomy](scope-taxonomy.md) — how to mark what you add so it can be
  deleted later.
- [Code quality](code-quality.md) — the gates that enforce what prose cannot.
- [Stack in layers](stack-layers.md) — what already ships, before you search
  anywhere else.
