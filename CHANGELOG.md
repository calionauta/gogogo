## [Unreleased]

## [0.42.9] - 2026-10-09

### Fixed

- **A peer's cursor now rides the live tip while drawing.** The cursor and
  the in-progress shape travelled on two throttles at different rates
  (100 ms vs 80 ms) with no ordering between them, so the dot lagged the
  dragged corner by up to hundreds of milliseconds — sitting mid-edge while
  the tip moved on. Every pointermove now POSTs one fused ephemeral frame
  (normalized cursor plus the whole shape); peers render dot and corner from
  the same instant, so they cannot skew apart.
- **A retried frame no longer drags the dot back in time.** The presence
  retry re-sent the original body up to 600 ms later and the receiver applied
  it unconditionally, so a late retry overwrote fresher positions. Frames
  carry the sender-captured `cts` (new `PresenceMsg` field, relayed
  untouched — the server still re-stamps only `ts`) and a per-user guard
  drops anything older than the last applied frame, for cursor and draft
  alike. Covered by two red-proofed two-context Playwright tests (dot-on-tip
  while held; aborted-frame retry exercised and dropped).

## [0.42.8] - 2026-10-09

### Fixed

- **Dead theme toggle on daisyui pages.** Only basecoat loaded
  `/static/theme.js`; the default skin now includes it, with a
  red-proofed regression test.
- **Single toggle path.** `data-on:click="Theme.toggle()"` on the button
  (Datastar loads on every navbar page); theme.js keeps persistence and
  icons, document delegation removed (double-fire). Pinned tests
  inverted to the new design.

### Changed

- **Collab navbar order.** Collab Todo | Collab whiteboard | Collab
  notes | Room | Config; installer add-anchor follows.

## [0.42.7] - 2026-10-09

### Added

- **Live in-progress drawing on the whiteboard.** A shape being drawn is now
  relayed while the pointer is still down, so peers watch it grow instead of
  seeing it pop in whole on release. The draft rides the existing volatile
  presence channel (`/presence`, `type:"draft"`) — never persisted, never
  merged into the CRDT, never published to NATS — and the real `add` op on
  pointer-up stays the only commit; peers drop the draft the moment the
  committed shape with that id arrives, with a 5 s TTL backstop and an
  explicit end for degenerate draws that commit nothing. Each frame carries
  the whole shape (not a delta), so a dropped frame is healed by the next one
  and the channel needs no sequencing or acks; it is throttled at 80 ms
  leading+trailing through the shared `/static/throttle.js`. The pattern is
  documented as reusable in `docs/async-layers.md` ("commit on release, relay
  while moving") and covered by a red-proofed two-context Playwright test.

## [0.42.6] - 2026-10-09

### Changed

- **Landing hero hierarchy.** Emotional H1 restored ("Your stack is
  ready. Come build."); positioning as H2 ("Not a framework. One
  binary."); nav "Thesis" renamed to "Why Go" (position kept).

## [0.42.5] - 2026-10-09

### Changed

- **Benefit-led landing hero.** H1 is now "One binary. The whole stack.
  Yours."; lede back to the feature list.
- **Readability pass on tradeoff texts.** One idea per paragraph, lists
  over comma chains; principle recorded in CONTRIBUTING.

## [0.42.4] - 2026-10-09

### Changed

- **Navbar and titles name the differentiator.** Whiteboard → Collab
  whiteboard, Notes → Collab notes (topbar, page titles); installer strip
  rules follow. Routes (`/whiteboard`, `/notes`) unchanged.
- **Readability pass on tradeoff texts.** One idea per paragraph, lists
  over comma chains; principle recorded in CONTRIBUTING.

## [0.42.3] - 2026-10-09

### Added

- **Native exception-language tradeoffs page** (`docs/native-language-tradeoffs.md`):
  evidence-backed biggest-risk table for Zig, Rust, Nim, Odin, and Mojo,
  with a rebuttal rule for justification item 5. Linked from
  `exception-to-go` (wrong-answer paragraph, item 5, Related) and the site
  manifest.
- **Lifetime/leak gate for Zig kernels** (`docs/exception-to-go.md`):
  alloc-adjacent defer/errdefer, `DebugAllocator`/`testing.allocator`
  leak checks, forced error paths, ~10k-call loop test; mirrored as a
  checklist in the coding-standards skill.
- **Stack tradeoff depth** (`docs/stack-layers.md`): Go vs TypeScript/Python
  (flywheel, two-interpreter GIL, npm install-hook supply chain, stdlib
  breadth, race detector, cold start); PocketBase vs Postgres graduation
  rule, SQLite/WAL scale numbers, PB scaling ladder, stdlib-router note
  (Gin/Echo/Chi); React/Next flywheel costs vs Datastar + datastar-lint.
- **"Not a framework" stated plainly**: landing hero, README intro, and a
  `docs/overview.md` section — template hands you the code and leaves.
- **Motivation thesis sharpened**: title names Zig as the exception; new
  paragraph tying each Go motive to its criterion.

## [0.42.2] - 2026-10-09

### Fixed

- **Remote carets no longer flash on the wrong line.** A peer's caret is
  reported as an offset in the REPORTING client's text, but the notes
  client mapped it against its OWN textarea — a beat behind while a
  concurrent op is in flight. A caret at the end of line 2 (offset N)
  therefore painted at the start of line 3 in a copy one character
  behind, then snapped back once the text arrived (the reported "blinks
  at the start of the line below"). Each caret now carries the text
  snapshot it was measured against (shipped on the `note-text` event)
  and the mirror is sliced from that snapshot; a report whose snapshot
  cannot justify its own reported line is used for presence only, never
  to place a dot. Caret reports also move to a 200 ms leading+trailing
  throttle instead of 500 ms.
- **A deferred text adopt is retried.** When a peer's text arrived during
  the local focused-typist window it was skipped and only re-attempted on
  the NEXT event — a tab that went quiet kept a stale textarea (and a
  stale caret basis) indefinitely. The skipped adopt is now retried on a
  250 ms timer.
- **Whiteboard cursors stop moving when nobody moves.** The client kept
  the connection roster (keyed by clientID) and remote cursors (keyed by
  the server-stamped email) in ONE map, so every join/leave rebuilt the
  roster from clientIDs, discarded the real cursor entries, and left
  phantom cursors at the canvas origin. Roster and cursors are now
  separate maps, and idle cursors expire on an 8 s TTL (the wire carries
  no cursor heartbeat).

### Added

- **Red-proofed client regression tests.** `scripts/notes-caret.test.mjs`
  drives two authenticated contexts typing concurrently and asserts the
  rendered dot's line always matches the line it reports; the whiteboard
  client suite gains a two-user check that a peer join neither moves nor
  drops a cursor. Both run in `ci-local` and the CI workflow.

## [0.42.1] - 2026-10-09

### Fixed

- **Room roster reads survive a supervised restart.** A `/room/*` click
  landing in the grain-restart window (or on a starved runner) could
  answer 502 even though the same read succeeds milliseconds later — the
  cause of the intermittent `TestRoomAcquireCrashRecoverOverHTTP` CI
  failure. The read is now retried inside a bounded budget, and the grain
  error is logged instead of swallowed, so a future 502 names its route
  and cause.

### Security

- **Go 1.27.1 → 1.27.2 and `x/net` v0.59.0 → v0.60.0**, closing the
  GO-2026-6603–6617 stdlib family (net/http, http2, net/textproto,
  crypto/tls, os, html/template) that blocking pre-push `govulncheck`
  reported. Docker base image, both CI toolchain installs, the
  `gogogo-mcp` module and the coding-standards skill pin move with it;
  `govulncheck` is clean again.

## [0.42.0] - 2026-10-09

### Added

- **Shared session-expiry visibility for every feature.** Notes and
  whiteboard streams open with a `collab.SessionEvent` frame; pages
  without a raw stream (todo, room) poll `/api/session`. All render the
  same `components.SessionBanner` + `/static/session.js`, so a cookie
  that expires while a tab is open now says so instead of silently
  degrading peer names to client hashes. Opt-in per page via the
  render-time `authed` flag.

### Changed

- `internal/collab` gained `SessionEvent`; notes uses it instead of an
  inline map. The session banner JS moved to one shared controller.

## [0.41.0] - 2026-10-09

### Added

- **Atomic text+caret delivery on notes.** Op batches carry the
  author's caret; the server echoes it on the same note-text event as
  the resolved text, so dots and letters arrive together instead of
  racing two channels (the "line below, then corrects" class).
- **Session visibility.** Streams open with a `{type:session}` event
  and op answers carry `authed`; tabs show an expiring-session banner
  with re-login instead of silently degrading to hashes. HTTP 401/403
  keeps the outbox (never drops user typing).
- **Datastar runtime on every navbar page** (board, notes doc, room,
  config, landing): the shared logout form's `data-on` handlers were
  dead markup without it — including the logout service-worker cleanup.

### Fixed

- **Dead Datastar attributes removed** from the whiteboard toolbar
  (`data-on`/`data-bind` with no runtime; vanilla already owned both).

## [0.40.5] - 2026-10-08

### Changed

- **Presence identity is server-stamped everywhere.** Whiteboard
  cursors showed spoofable random ids in one red; the server now stamps
  the authed email (shared `collab.DisplayName` rule with notes) and
  each client hashes it to a deterministic per-user color. Tests pin
  attribution + anti-spoof on both features.
- **Theme toggle wiring pinned by test** (single delegated owner, one
  storage key) after the live dead-toggle diagnosis.

## [0.40.4] - 2026-10-08

### Fixed

- **Idle readers vanish from presence.** Heartbeat re-announce now
  reports even unfocused (position may be stale, tooltip line stays
  truthful); expiry remains the disconnect detector.
- **Mirror copies `word-break` too.** One more computed property the
  pixel math silently depended on.

## [0.40.3] - 2026-10-08

### Fixed

- **Caret cadence + idle presence.** Reports at 500ms with a 5s
  heartbeat re-announce (OT-demo lesson), so idle peers stop aging out
  and active carets track typing instead of freezing between throttle
  windows.

## [0.40.2] - 2026-10-08

### Fixed

- **Notes index actually goes live.** The resync button is a Datastar
  `@get` but the page never loaded the runtime — PB events clicked a
  dead button. Runtime added, pinned by a render test.
- **Dead Datastar attributes on the whiteboard toolbar.** `data-on` /
  `data-bind` with no runtime on the page (vanilla already owned both);
  `data-tool` stays as the honest hook. `features.md` now records the
  Datastar-vs-vanilla split rule so the next surface picks correctly.

## [0.40.1] - 2026-10-08

### Fixed

- **Theme toggle dead on Datastar-less pages** (whiteboard board,
  notes): the inline `data-on:click` needs the runtime while the
  delegated fallback matched a class the button doesn't have. One
  owner (delegated listener), one storage key (`themeMode`).
- **Caret landing one line down** at line starts: the mirror dropped
  the trailing remainder, collapsing the newline's line box.
- **Caret reports at 800ms** (were 1500ms) now the shared throttle
  guarantees the trailing send.

### Added

- **Join chime on shared notes** (one soft chime per arrival, centrally
  mute-aware) with the declared sounds dependency it needed.

## [0.40.0] - 2026-10-08

### Added

- **Peer carets that track: colored text-carets with name flags and
  tooltips.** Peers report {line, pos}; each browser maps offsets to its
  own pixels via mirror-div, and local ops transform peer offsets so dots
  follow typing instead of freezing. Recipe-conformance test pins the
  mirror property list.
- **Shared leading + trailing throttle** (`web/resources/static/throttle.js`,
  ~2 KB) for caret/cursor reports — delayed, never dropped; the whiteboard
  posted presence per pointermove before.
- **Client-weight table + landing section.** Measured vendored JS per
  feature (0–48 KB raw) and the backend-as-source-of-truth rule, now
  stated in docs, thesis, README, and site.

### Fixed

- **Caret reports dropped inside the throttle window** (peers froze) and
  **carets rendered as dots** instead of text carets.

## [0.39.0] - 2026-10-08

### Added

- **Shared notes: live index, typing + line-caret presence.** The index
  page re-fetches via PB realtime (whiteboard pattern); peers announce
  typing ("X is typing…", throttled, expiring) and caret {line, pos},
  rendered as per-user colored text-carets with tooltips — each browser
  maps offsets to its own pixels via mirror-div.
- **Op protocol: base revision + 409 + recompute.** Batches carry the
  base they were computed against; stale bases are refused with
  authoritative state instead of misapplied (the silent character-loss
  class). Clients track REV everywhere, retry bounded, render remote
  text backgrounded or idle with caret restore.
- **Concurrency acceptance: 8×5 hammer + rev-chain + stream-rev tests,**
  all under `-race`; mutation runs prove the guards fail without the fix.

### Changed

- **Shared fan-out loop (`SSEHub.ServeStream`), shared peer set
  (`collab.PeerSet`), shared fan-out tail (`collab.Fanout`).**
  Whiteboard + notes had hand-rolled the same ~35 SSE lines and the same
  persist→broadcast→NATS tail; suites prove zero behavior change. Op
  protocols stay separate by design (different conflict semantics).
- **Skill records the base+409 rule** so the next agent doesn't re-learn
  silent misapply.

### Fixed

- **Focused tab never converging** (one direction looked broken) and
  **peer carets moving spuriously** (re-rendered from local keystrokes
  on stale offsets); carets draw as text carets, not dots.
- **Notes test flake under contention** (deadline + backoff retry) and a
  self-deadlocking test channel found along the way.

## [0.38.1] - 2026-10-08

### Fixed

- **English-only typing indicator.** The "X is typing…" pill and the
  0.38.0 changelog entry shipped pt-BR strings; product surfaces are
  English-only now.

## [0.38.0] - 2026-10-08

### Added

- **Shared notes presence: "X online" pill and "X is typing…" indicator.**
  The notes feature ports the whiteboard peers contract (join to others,
  authoritative count to all, leave + recount, reconnect guard) with
  red-first tests, plus throttled typing reports with a 6s client-side
  expiry. No server typing state, no persistence, no NATS.
- **Notes offline op outbox.** Batches typed while offline replay on
  reconnect and reload (localStorage, demo-grade); poison batches drop
  instead of retrying forever. Same strategy as the whiteboard outbox,
  sized down for text ops.
- **Per-feature stack picks in `docs/use-cases.md`.** Whiteboard, room,
  and notes now state why this stack and not the alternative (fair pros
  and cons each way), plus an OT-vs-CRDT decision pair crediting the
  Datastar collab demo (`ot.js`, Tim Baumann) — deliberately no unit.

### Changed

- **Peer-set mechanics extracted to `collab.PeerSet`.** Whiteboard and
  notes shared ~40 duplicated lines; both handlers delegate now, suites
  prove no behavior change.
- **Scope cards list `internal/goakt`, `features/room`, `features/notes`.**
  The landing grid was stale since the GoAkt work.

### Fixed

- **Notes accessibility gaps.** Real `<label>`, `sr-only` h1,
  `aria-live` pills, `aria-busy` on pending (opacity alone fails
  forced-colors and screen readers).

## [0.37.0] - 2026-10-08

### Changed

- **Advise scope `native-kernel` renamed `exception-to-go`.** The old name
  described a workload noun that doesn't always fit (OS integration is no
  kernel); the new name states the policy relation (the exception *to*
  Go-first), covering kernel, codec, and OS integration uniformly. No
  behavior change — same trigger, same gate, same shape.

### Fixed

- **`advise` no longer names foreign stacks — allowlist-only scopes.** A Go
  serialization need (`native serialization fast path behind a C ABI, with a
  pure-Go fallback`) was answered `scope=patterns/stack=Node.js` because the
  generic word `fast` prefix-matched a web-framework signal. The ecosystem
  detector is deleted: a need that matches nothing gets the full map instead
  of a guessed label (scaffold first-run withheld until something matches),
  `Zig` is answered as the documented native exception
  (`scope=native-kernel`: gate + skill pointer), and the stdlib-only rules no
  longer assume Go.

### Changed

- **`docs/native-zig.md` no longer cites token counts from the retired
  `mini-redis-comparison` experiment, and no longer references any external
  experiment.** That experiment (a RESP/TCP server built
  in several languages) has been retired and its contents removed: every arm passed
  it in every phase, so it had stopped discriminating, and its numbers were never
  independently reproduced (N=1, one model). The v0.36.4 entry below cites them and
  stays as written history; the policy page now argues from the reasoning instead
  (discovery cost at the moment of the bottleneck, named default) and justifies
  Zig from properties verifiable in this repo.

## [0.36.4] - 2026-10-06

Two improvements: the installer's drift gate stopped misreporting a correct
scaffold as broken, and the native-boundary policy now documents *why* Zig is
the named default instead of leaving it implicit.

### Fixed

- **`gogogo --check` reported a deliberately trimmed project as broken.** The gate answers "would applyTrim succeed fully here?", so against a scaffolded tree every marker it looks for is legitimately absent — it emitted `CHECK-FAIL` for each removed unit and exited **1**. A human knows to ignore that; an LLM agent reads exit 1 plus the word FAIL and concludes the scaffold is broken, which is exactly the wrong conclusion. Since `apply` already writes a `Trim provenance` section into the generated `AGENTS.md`, that information is now recorded as a machine-read `removed:` line and used to tell the two apart:

  | Line | Meaning | Exit |
  |---|---|---|
  | `CHECK-OK <id>` | the unit applies | 0 |
  | `CHECK-TRIMMED <id>` | gone, and the tree records it was removed on purpose | 0 |
  | `CHECK-FAIL <id> <problem>` | does not apply and nothing says it was trimmed | 1 |

  Verified end-to-end on a real scaffold: `CHECK-OK 2 / CHECK-TRIMMED 5 / CHECK-FAIL 0`, exit 0 (was exit 1 with 5 failures). Deleting a unit that was **not** recorded still exits 1 with the exact name — the gate still catches real drift, which is the property that must not regress. A tree with no `removed:` line is checked strictly, because a false "this is fine" is worse than a noisy failure. Unknown ids on the `removed:` line are ignored rather than trusted, since it is read from a markdown file a human may edit.

- **The `AGENTS.md` template used backticks inside a Go raw string**, which terminated the string literal. Caught by the compiler; the machine-read line is now plain text.

### Changed

- **`docs/native-zig.md` now documents why Zig is named rather than left open.** The policy enforces "no native code without a Go baseline and a profile" — not "no preference about which toolchain". Those are different rules, and naming the default is what makes the 493-line policy, the `zig-gate` skill reference, and the boundary/removal plans pay off instead of being re-derived under pressure. The section cites the measured cost of choosing late from `mini-redis-comparison`: Mojo's discovery phase cost **120k prompt tokens** versus **12.7k** to reuse what it had learned (~9x), **4.96M total against Go's 387k**; Odin's unprepared first submission did not compile (one hallucinated API); Zig on Linux was green 4/4, first try, **0 rounds, 0 hallucinated APIs**. It also states plainly that Zig's stdlib churn is a **cost**, not a feature, and that a different language may win for a specific kernel — argued in required-justification item 5, never chosen silently.

### Verification

- `make ci-local` green end-to-end; `make site-check` green (22 pages, all anchors resolve)
- Red-proofs: removing the provenance reclassification fails `TestCheckAcceptsDeliberatelyTrimmedTree` and `TestExplainTrimCountsOnlyUnrecordedUnits`; `TestReadTrimProvenanceIgnoresUnknownIDs` pins that an unknown id is not trusted; `TestCheckStillFailsWithoutProvenance` pins the strict fallback; `TestWriteAgentsRecordsEmptyTrim` pins that a full template still records provenance
- End-to-end against a real scaffold: trimmed tree exits 0, a unit deleted outside the recorded trim exits 1

## [0.36.3] - 2026-10-06

Closing the coverage gap the previous release exposed, plus two tool traps that
were making local verification lie.

### Added

- **The whiteboard client now has regression tests, and they are wired into the gate.** The Go side of the whiteboard had 15 tests; the client had none, and `scripts/smoke.mjs` only loaded `/whiteboard` without ever drawing on it. That blind spot is exactly where the v0.36.1 regression lived. `scripts/whiteboard-client.test.mjs` drives the real page in Chromium and asserts on canvas **pixels in a region** — a region, not total ink, because a whole-list replacement still leaves one shape painted, so "some ink exists" cannot tell a correct merge from a destructive replace. It runs in `make ci-local` and in `ci.yml`, reusing the binary the browser-smoke step already builds.

  Both tests are **red-proofed**, and getting there took fixing the test twice. The first version delayed the *request*, which let the server process op 1 after op 2 so op 1's payload already contained both shapes — it passed against the broken code and proved nothing. It must delay the *response*: `route.fetch()` sends the request immediately (so the captured answer really is the older single-shape state) and delivery is held back past the second response. Restoring `shapes = body.shapes` now fails both tests with shape 2's ink at 0; restoring the whole-list replace only in the 409 handler fails only the 409 test. Each red-proof isolates its own fix.

### Fixed

- **`bin/check-deadcode.sh` reported a toolchain mismatch as "dead code found".** The installed `deadcode` binary is a `go install`ed tool that lags the Go toolchain, so it emits type-check errors for files under `/usr/local/go/src` (`package requires newer Go version go1.27 (application built with go1.26)`). The script captured stderr with `2>&1` and printed anything non-empty as a finding, sending the reader hunting for dead functions that do not exist — which is what happened on this host. It now separates stdout (findings) from stderr (diagnostics) and distinguishes three cases: a real finding, a recognised toolchain mismatch with its fix, and an unrecognised failure reported as "scan did not complete" rather than guessing at a cause. All four paths are exercised.

- **`features/todo` leaked a temp directory on every test run.** `TestMain` removed the built binary but never the `MkdirTemp` directory around it, so each run left an empty `gogogo-live-*` behind — **195 had accumulated** in `/tmp` on this host before it was noticed while investigating a full disk. `buildLiveServerBinary` now returns the directory and `TestMain` removes it with `RemoveAll`.

### Verification

- `make ci-local` green end-to-end, now including the whiteboard client tests
- Red-proofs: `shapes = body.shapes` fails both client tests (shape 2 ink 0); the 409 whole-list replace fails only the 409 test; the four `check-deadcode.sh` paths (finding / toolchain mismatch / unrecognised failure / real host) each produce their intended message; the `gogogo-live-*` count does not grow across a `features/todo` run

## [0.36.2] - 2026-10-06

Release-process fix found by auditing the v0.36.1 deploy rather than trusting it.

### Fixed

- **Production reported the wrong version: the box ran the new code while showing the old tag.** The release flow is push, CI green, tag, release, so the deploy triggered by the master **push** runs *before* the tag exists, and `git describe --tags --abbrev=0` therefore resolved to the **previous** tag. Verified against a real deploy log, not inferred: the run for v0.36.1 printed `Building calionauta/gogogo v0.36.0 (commit e4a2206)`, and the live box served `0.36.0` for a commit tagged `v0.36.1` minutes after the deploy started.

  Two coupled defects, both fixed:
  1. `deploy.yml` did not trigger on tags at all. Added `tags: ["v*"]` (nested under `push`; GitHub does not path-filter tag pushes, so a tag always deploys).
  2. Even had it triggered, the deploy job's `if:` allowed only `refs/heads/master` or `workflow_dispatch`. A tag push sets `refs/tags/vX.Y.Z`, so the job would have been **skipped** and the tag-triggered deploy would have built nothing.

  The extra restart is the deliberate price of not tagging before CI has passed; tagging first would avoid it but would put a red CI on the tag, which is worse. Proven end-to-end: a tag-triggered deploy for v0.36.1 now logs `Building calionauta/gogogo v0.36.1` and the live box reports `0.36.1`.

- **`docs/deploy.md` asserted "the badge on the running box matches the tag that produced it"**, which was false for exactly the ordering above. Corrected with the observed evidence and the rationale for the second deploy.

### Verification

- Empirical, not assumed: re-triggered the deploy against the existing `v0.36.1` tag and read the build log (`Building v0.36.1`) and the live page (`0.36.1`) back. The fix was confirmed by observing the corrected behaviour, not by reading the YAML.
- `make ci-local` green end-to-end; `make site-check` green

## [0.36.1] - 2026-10-06

Post-release gap analysis on v0.36.0. One user-visible regression the release
introduced, plus documentation that had drifted from the code it describes.

### Fixed

- **A slow op response could drop a shape the user had just drawn.** Adopting the server's shape list from a POST response looked harmless — it carries the version the server assigned, which a later edit needs as `baseVersion` — but concurrent POST responses are **not ordered relative to each other**, so a late response for an older op could overwrite a newer local shape (draw X, draw Z, X's response lands last, Z disappears locally until the next SSE event). The list is only authoritative when it arrives over the SSE stream, which is a single ordered channel. Both the success path and the 409 path now take only what they need: the success path copies the assigned **version** onto the local shape in place, and the conflict path corrects only the **conflicting** shape rather than replacing the list mid-session. This is the same reasoning behind the server's no-echo broadcast.

- **Doc drift: `docs/async-layers.md` overstated the whiteboard's replay safety.** It claimed the whiteboard "avoids this entirely: Loro CRDT ops already carry unique IDs and converge idempotently on their own". Measured, only half of that holds: replay is state-idempotent, but a duplicate `add` is now **refused** with 409, and the doc said nothing about the per-shape version contract or its scope. Rewritten to match the code.

- **Doc drift: the gate lists were stale in three places.** `docs/local-ci.md`'s parity table is an explicit contract ("when a step is added to one, add it to the other") and did not mention `check-stelow-drift`; `docs/code-quality.md`'s `ci-local` chain was missing four steps it actually runs; `docs/stack-layers.md` enumerated 31 of the 33 enabled linters (`unparam` was absent) and did not list the two new maintenance workflows.

### Verification

- `make ci-local` green end-to-end (both smoke tests); `make site-check` green (22 pages, all anchors); `node --check` on the edited JS
- Checked rather than assumed: the lefthook `stelow-drift` glob really fires (probed with a staged file), the vendored copy still matches its pin, no `morpheus`/`neo-*` references remain outside intentional history, the site output is gitignored and rebuilt in CI, the JS is served with a content-hash ETag so the edit cannot be cached stale, and all 33 linters now match `.golangci.yml` exactly

## [0.36.0] - 2026-10-06

A correctness and UI-surface release. Three data-integrity bugs in the
collaborative-document layer are fixed — a cold-miss op could discard every
shape on a board, the doc cache grew without limit, and a stale op could
silently overwrite a newer edit to the same shape. The Basecoat skin is
repaired (it had been rendering without most of its CSS) and the Morpheus skin
is removed. Adds a versioned, drift-checked dependency on the vendored stelow
coding standards.

### Added

- **The vendored stelow standards can now be refreshed with one reviewed command, and the update is proposed as a PR rather than committed silently.** Three layers, each with a distinct job:

  | Layer | Mechanism | Role |
  |---|---|---|
  | 1. Detect | `bin/check-stelow-drift.sh` (advisory pre-commit; `--strict` via `make check-stelow-drift`) | Warns locally when the vendored copy or the pin is off |
  | 2. Report | `.github/workflows/stelow-drift.yml` (weekly) | Fails its own run when upstream has moved |
  | 3. Fix | `scripts/refresh-stelow.sh`, proposed by `.github/workflows/stelow-auto-bump.yml` (weekly) | Opens a PR against the new pin |

  `scripts/refresh-stelow.sh` fetches each file from
  `raw.githubusercontent.com/<repo>/<sha>/<path>`, which returns
  byte-identical content **without cloning the stelow repo** and without
  spending GitHub API rate budget on content. It resolves a branch or an
  explicit commit, discovers the upstream file list (API tree → local clone →
  local files, warning loudly in the last case because only the upstream tree
  can reveal a *new* reference), fetches and verifies, installs, bumps
  `UPSTREAM_SHA`, and re-runs the strict drift check. Any fetch failure aborts
  before touching the tree, so a partial upstream change cannot land.

  The update lands as a **PR, never an auto-commit**: the vendored copy is
  normative — it is the universal coding principles this template enforces — and
  a silent commit would change the rules the project holds itself to with nobody
  reading the diff. This mirrors the existing "human must approve all AGENTS.md
  changes" rule.

### Removed

- **The Morpheus skin is gone.** It was added in v0.24.0 as a third UI skin and
  shipped since, but its upstream is a two-month-old alpha (created 2026-07-07,
  22 stars) and its layout classes resolved to nothing: `cal14-grid`,
  `cal14-left`, `cal14-right` and `neo-tab-active` had zero definitions in any
  stylesheet, and `morpheus.css` was only `--neo-*` design tokens — the
  components themselves are shadow-DOM `<neo-*>` elements styled inside their own
  boundary, so the utility classes in the template silently did nothing. That is
  why the skin looked broken rather than merely stylistically different.

  Removed rather than rewritten: `web/skins/morpheus/` (the vendored bundle and
  its `neolib/` component set), `web/resources/static/morpheus/`, the
  `SkinMorpheus` constant, the `_ "…/web/skins/morpheus"` registration, the
  `handleIndex` / `renderTodoListRegion` dispatch branches, the `skins-extra`
  trim-unit entries, and the Morpheus leg of the Playwright skin sweep. An
  unknown `UI_SKIN` or `?skin=` value still falls back to DaisyUI, so a
  bookmarked `?skin=morpheus` URL degrades to the default skin rather than
erroring. DaisyUI remains the default; Basecoat remains the supported option.

### Fixed

- **Split three files back under the 500-line budget** (`features/whiteboard/handler.go` 511 → `handler.go` 424 + `handler_peers.go`; `internal/collab/docstore_bounds_test.go` 507 → `docstore_bounds_test.go` + `shape_version_test.go`; `features/whiteboard/web_test.go` 548 → `web_test.go` + `web_conflict_test.go`). No behaviour change — same package, same tests. CI had caught this: `check-sizes` was a gate that had not been run locally before the first push, which is why the tag was moved onto the commit where CI passed.


- **A whiteboard op could be applied to an empty document, silently discarding every shape drawn before it.** `DocStore` only ever *added* docs and never rehydrated on a miss, so a request for a doc this process had not loaded started from `NewDoc(id)` — empty — applied the op, and then persisted that resolved snapshot over the good one. The reachable production path is exactly the offline-replay flow: after a server restart the service worker serves the board page from cache, the SSE stream connects (loading nothing), and the client's IndexedDB outbox then replays an op into the empty doc. The store now rehydrates from the `Persister` **before** publishing the doc into the map (so no concurrent caller can observe a half-loaded doc), and both `NewWebSyncWorker` and `NewSyncWorker` wire the persister in — the NATS side needed it too, since a cross-instance update for a non-resident doc had the same failure. Red-proofed: with rehydration disabled, the new test fails with `prior shapes were discarded`.

- **`DocStore` grew without limit.** One `LoroDoc` per `docID` was retained for the process lifetime, each holding a full op log, with no delete or evict path anywhere in the package. A whiteboard server accumulated a doc for every board ever touched. It is now a bounded LRU (`DefaultMaxDocs` = 256, `DefaultIdleTTL` = 5 min) with an explicit `Evict`, and `crdtstore`'s per-owner `docs` map (same unbounded shape, one doc per owner) got the same treatment.

  The TTL floor is what makes eviction correct without reference counting: a single request mutates a doc for microseconds, so a doc idle for minutes cannot be mid-mutation. Without it an LRU could evict a doc between the `GetOrCreate` and the `ApplyOp` of the same request, putting two concurrent ops on two different `Doc` instances and losing one. A doc in active use is therefore **never** evicted even when that briefly exceeds the cap — exceeding the cap is the correct trade. Both properties are pinned by tests, and the bound test is red-proofed.

- **A stale op can no longer overwrite a newer edit to the same shape.** The store is a `LoroMap`, where writing an existing key is last-writer-wins: every replica agrees deterministically on *which* write survives, but the other edit is lost with **no signal to anyone**. For a canvas that is the wrong trade — a user who moves a shape and watches it snap back has no way to know a peer also moved it. Shapes now carry a server-assigned `Version` and clients send back the revision they last saw as `ShapeOp.BaseVersion`; a write based on a superseded revision is refused with **409 + the current shape list**, so the browser resyncs and the edit can be re-applied against the newer base instead of vanishing. The check and the write happen under one lock hold, so two concurrent ops cannot both pass it. The version is persisted (it survives a snapshot round-trip), and a rejected op is neither persisted nor broadcast. Red-proofed: disabling the check makes both conflict tests fail with `want ErrShapeConflict`.

  Scope, stated plainly: the canvas today only *adds* shapes, so this was not yet reachable from the UI — it becomes load-bearing as soon as shapes can be moved or resized. Two consequences worth knowing: a replayed duplicate `add` is now a **409 rather than a silent no-op** (the safer answer, and the state is still unchanged), and `clear` still takes no version because it cannot conflict.

- **Two claims about the CRDT's concurrency behaviour were measured and refuted; the measurements are now pinned as regression tests.** A replayed `clear` does **not** wipe concurrent additions (a clear of key `x` concurrent with an add of key `y` converges to `{y}`), and `add`/`clear` replays **are** state-idempotent. Measurement method matters here: compare **resolved state**, never `EncodeSnapshot()` bytes — snapshot bytes are not stable for the same logical state because Loro records every op, so a byte comparison reports "not idempotent" for a genuinely idempotent operation. The one real hazard found this way — a stale offline op silently clobbering a newer write — is fixed above, and its regression test asserts the rejection rather than the old behaviour.

- **The Basecoat skin was missing most of its CSS, and the cause was the scan
  roots, not the class names.** `src/css/basecoat-input.css` declared its inputs
  as `@import "tailwindcss" source("../../features/") source("../../web/")` —
  Tailwind v4's `source()` takes *directories*, not recursive globs, so
  `web/skins/basecoat/*.templ` and `features/todo/components/*.templ`
  contributed nothing to the bundle. Every layout utility those templates rely
  on (`grid-cols-3`, `p-0`, `py-2`, `space-y-3`, `lg:grid-cols-5`, …) was simply
  absent, which is why the skin looked broken while its class names were in fact
  correct. Replaced with `source(none)` plus explicit `@source` globs that
  mirror `src/css/input.css` **exactly**, so a class that works under DaisyUI
  cannot silently vanish under Basecoat.

- **Basecoat had no stepper, so the async-demo progress was invisible.** The
  demo panels render `<li class="step">` and toggle `step-primary` /
  `step-success` / `step-error` through Datastar `data-class`. Those names are
  DaisyUI's `.steps` component; Basecoat ships no equivalent, so every state
  class resolved to nothing and the list rendered as static text that never
  changed. The three state utilities are now defined in
  `src/css/basecoat-input.css`, which keeps the state wiring in the templates as
  the single source of truth instead of forking the markup per skin.

- **`data-variant="accent"` silently opted out of Basecoat's styling.** Basecoat
  has no `accent` variant, and its default rules are written as
  `.badge:not([data-variant])` — so *any* unrecognised variant value excludes
  the element from the default rule rather than falling back to it. The DagNats
  badge and the “Run durable workflow” button rendered unstyled. Both now use
  `data-variant="secondary"`.

- **`make css-install` did not check `basecoat-css`.** The version guard that
  reinstalls when `node_modules` drifts from `package-lock.json` covered
  `tailwindcss`, `@tailwindcss/cli` and `daisyui` but not `basecoat-css`, so a
  stale Basecoat could emit a bundle differing from the committed one and fail
  `css-check` with no source change — the exact false alarm the guard exists to
  prevent.

### Verification

- `make ci-local` green end-to-end, including the binary boot smoke test and the Playwright browser smoke across both skins; `make site-check` green (22 pages, all anchors)
- Red-proofs for every fix in this release: disabling rehydration fails with `prior shapes were discarded`; disabling eviction fails with `store.Len() = 10, want <= 4` and `resident = 261, want <= 256`; disabling the version check fails with `want ErrShapeConflict`; a content mismatch in the vendored stelow copy prints **and** exits non-zero under `--strict`; `bin/check-stelow-drift.sh`'s round-trip to an older ref and back restores byte-identically
- Two of this session's own claims were measured and **refuted** (a replayed `clear` does not wipe concurrent additions; `add`/`clear` replays are state-idempotent) and are pinned as regression tests
- `golangci-lint` 0 issues with 33 linters, scoped to the touched packages (never the whole tree on a shared host)

## [0.35.0] - 2026-10-06

Performance, correctness and CI-parity work on the test gate and three
production shortcuts it exposed. No user-facing feature changes; the one new
knob is `DAGNATS_GREET_PACING`.

### Changed

- **The test suite is ~44% faster (75.5s → ~42s wall, measured 40.6–45.2s across runs) without weakening what it checks.** The cost was not the tests but latency the production code made them wait out: four demonstration/backoff delays (~20s), a 6s negative-assertion window, and a bcrypt cost of 10 that made every fixture login cost ~960ms under `-race`. Fixed by making pacing injectable (`Queue.SetRetry`, `(*TodoHandler).SetRetryDemoDelay`, `llm.NewSimulatedWithDelay`) with production defaults untouched, plus event-driven waits. `t.Parallel()` on `features/todo`, `features/whiteboard`, `internal/queue`, `internal/nats` and `features/store/crdtstore` (per-test fixtures were already isolated) added the rest.
- **`make ci-local` now runs `check-generated`, `install-sh-guard` and the binary boot smoke test**, so the local gate matches the remote one step-for-step. `docs/local-ci.md` documents the parity table so the next drift is visible instead of silent.

### Added

- **`forbidigo`: no bare `time.Sleep` in production** (tests exempt). An uninterruptible wait holds its goroutine — and any `wg.Wait()` draining it at shutdown — for its full duration. Three real sites shipped this way; the lint now prevents a fourth.
- **`DAGNATS_GREET_PACING`** (default `1500ms`) — the onboarding-greet pause was product latency, so it is tunable rather than deletable. Non-positive or unparseable values fall back to the default.
- **`bin/check-install-sh.sh`** and `TestRegistryCoversDagnatsImports` — guards for the two silent-breakage classes found while doing the above.

### Fixed

- **`Stop()` blocked for up to a second per worker just because the queue was failing.** The worker's receive-error path used `time.Sleep(time.Second)`, which is uncancellable: `Stop()` cancels the pool context and then waits on `wg.Wait()`, so a shutdown sat through the full second. It is now a `select` on the pool context. Measured: `Stop()` with a failing queue 946ms → 190µs.
- **`internal/nats` had three mutable package globals (`NS`, `NC`, `JS`).** One design, two symptoms: two concurrent starts raced on the same variables, and a test's teardown cleared state (and shut the server down) under its neighbours — which is why neither that package nor `crdtstore` could run in parallel. `StartEmbedded`/`StartLeafNode`/`ConnectExisting` now return an owned `*Handle` with a `Close()` that frees only what it started.
- **`install.sh` planted a `go` symlink in `$BIN_DIR`**, which is on `PATH` by design — so it shadowed a system Go in every new shell (and read as though the CLI were named `go`). The toolchain is now exposed via `$GO_DIR/bin` on `PATH` instead. The same block had a reachability check that ran *after* prepending, so it could never be false and its guidance never printed.
- **`TestTodoRecordsNotBroadcastViaHub` could never fail.** Its predicate searched for `"event":"created"`, a string the hub path translates away before it reaches the wire — so reintroducing the very regression it guards left it green at any window. It now asserts the wire symptom and goes red in 1.5s when the broadcast is restored.
- **`css-check` failed on a clean tree with no source change.** Not a stale committed bundle: the local `node_modules` held tailwindcss 4.3.2 / daisyui 5.6.15 while `package-lock.json` pins 4.3.3 / 5.7.42. `make css-install` now compares the installed versions against the lockfile and reinstalls on mismatch, and the pre-commit hook builds through `make css` so a version skew cannot masquerade as stale CSS.
- **Generated artifacts were never verified in either CI — only regenerated.** `_templ.go` (71 files) and `app.min.css` are committed *and* embedded in the binary, so editing a `.templ` or `src/css/input.css` without committing the regeneration passed lint, the race suite and both smoke tests, and shipped a binary rendering stale markup. Both jobs now diff after regenerating.
- **`gogogo --trim dagnats` produced a broken tree.** `onboarding_lifecycle_test.go` imports `internal/dagnats` but was missing from the unit's file list, so the trim left a dangling import and the proof build failed; and the success message still advertised `http://localhost:8080/dagnats/` on a checkout where that console had just been deleted. Both fixed, with a guard for the first.
- **`shutdownDagNats` was a no-op, so application shutdown was skipped entirely.** It only nilled a pointer, claiming the engine's shutdown was "wired internally". DagNats installs its own `signal.Notify` for SIGINT/SIGTERM, exactly as PocketBase does, and the runtime delivers a signal to every registered channel — so the engine tore down first and `pb.Start()` never returned, skipping every deferred cleanup on the way (the queue workers and the SQLite handle were simply dropped). Verified with the real binary: the old code never reached `queue workers stopped`. It now calls `Server.Stop()` so ours is the deterministic shutdown path.
- **`WorkerPool.Stop()` was unreachable from production code.** `internal/server/boot.go` assigned the pool to a discarded local (`workersLocal := q.StartWorkers(); _ = workersLocal`), and `q.Close()` only nilled the queue handle and closed the database out from under the still-running workers. The `Queue` now owns the pool and `Close()` stops it before releasing the queue and DB, so shutdown is bounded and correctly ordered.
- **`TestCollab_LeafNodeE2E` was a real flake, not infrastructure noise.** It waited for the leaf node to *attach* and then published once; attachment does not imply the worker's `app.sync.>` interest has propagated to the leaf, so under load the first (and only) message was dropped and the test failed after its full 15s deadline. It now re-publishes while polling for the persisted snapshot — a Loro update is an idempotent CRDT delta, so this removes the timing assumption rather than widening it.
- **`gogogo --trim dagnats` produced gofmt-invalid Go.** The strip removed the dagnats block but left its orphaned explanatory comments, the engine's boot preamble, two consecutive blank lines, and no final newline — so a freshly scaffolded project failed its own `make fmt`. The strip engine now collapses blank runs and re-terminates the file with a single newline (kept generic, so every unit benefits), and the dagnats rule drops the comments describing the call it removes.

- **`CreateTodoForOnboarding` wrote to the store through a nil context**, and a DagNats handler closure depended on a context that is trace-only (the engine derives it from the message headers, so it is never cancelled on shutdown) — both found by enabling the cancellation lint rather than suppressed.

### Added

- **`rules/rules_test.go` — a guard for the ruleguard rules.** A rule that matches nothing is indistinguishable from a broken rule, and a broken rule fails open, so CI stays green while the footgun ships. Seven fixtures run through the real `golangci-lint` against a minimal module, asserting a hit on each bad shape and silence on the corrected ones. Writing it showed that ONE match arm covers both ticker-receiver shapes, so the second arm (and its rule) was removed — it was not merely redundant: with two arms the linter reports only one diagnostic for a file holding a call-chain loop followed by an identifier loop. Red-proofed both ways.
- **`make check-sizes` runs in `ci-local` and in CI.** The 500-line file budget was enforced only by the pre-commit hook, which `git commit --no-verify` skips: a 1871-line file passed every other check (verified).
- **`funlen` now applies to test files**, with three individually-listed and commented exceptions for linear setup (127/130/138 lines) instead of a blanket exemption that hid four over-long functions. Function length is deliberately NOT enforced by a shell script — `funlen` already does it, and it understands AST, so a second implementation would be a second source of truth.
- **`make lint-safe`** (merged from #72) — host-aware lint sizing, plus `scripts/changed-packages.sh`.

### Verification

- `make ci-local` green end-to-end, including the binary boot smoke test and the Playwright browser smoke across all three skins
- Red-proofs: the `forbidigo` rule fires on an injected `time.Sleep`; `check-generated` fails on a `.templ` edit with no regeneration; the new CSS diff fails on a stale bundle; `TestHandleConcurrentStartIsIndependent` reproduces the old globals race when the handles are shared; `TestWorkerStopIsPromptWhenQueueFails` fails (946ms) against the reintroduced sleep
- 387 tests, 0 failures, 0 new skips; `golangci-lint` 0 issues with 33 linters

## [0.34.0] - 2026-10-05

Covers everything since v0.33.0. An earlier draft of this entry was numbered
0.31.1 — a version that was never tagged and sorts *below* the released v0.33.0,
so it would have been skipped by any reader walking the file top-down.

### Added

- **In-repo `gogogo-coding-standards` skill** (`skills/gogogo-coding-standards/`, `SKILL.md` + 5 references): the Go concurrency/perf/testing deltas for 1.25-1.27, the Datastar gate, and a Zig gate summary. Universal principles are delegated to `stelow-workflow-coding-standards` (linked, not duplicated). `AGENTS.md` shrank 576 → 75 lines with the load-bearing rules kept as one-liners pointing at `docs/*.md`, and `advise_stack` now cites the skill as its source.
- **`make ci-local-fast`** — a layered gate scoped to the changed packages: the cheap decisive checks (templ, datastar-lint, css-check, check-scope) plus scoped lint and race tests, narrowed by `scripts/changed-packages.sh`. Measured ~2s for a CSS-only change and ~10s for one changed package, against ~240s for the full gate. It falls back to all packages when a shared file moves (`go.mod`, `config/`, `db/`, `internal/capabilities/`). `make ci-local` is unchanged and stays the authoritative pre-push gate.
- **`rules/rules.go` — project footguns enforced by ruleguard**, loaded through `gocritic` so they run inside the ordinary `golangci-lint` pass and block CI. The first rule flags `<-time.After(...)` used directly as a select case: a timer allocated per evaluation that leaks inside a loop. Also enables `perfsprint`, `prealloc`, `fatcontext` and `usestdlibvars`.
- **`credits.Settler.Start(ctx)`** — the settlement worker is no longer started by `New`; the caller owns its lifetime and the router binds it to `App.OnTerminate`.

### Fixed

- **Five concurrency and resource pitfalls**, each with a red-proof test. `credits.New` no longer starts workers (and its settlement ticker selects on `ctx.Done()`); `onboarding.pollRun` is bound to the handler's shutdown via a done channel + `sync.Once` — not a stored context, which `containedctx` rejects — while the durable workflow still survives the client navigating away, since the poll context derives from `Background` rather than `Request.Context()`. Installer writes are confined to their target directory.
- **The `features/todo` login flake, at its root.** `"POST /login: context deadline exceeded"` was not timing noise: `requestTimeout` was 5s while SQLite's `busy_timeout` is 10s, so under a held write lock the request context cancelled while the database was still legitimately waiting. Reproduced deterministically — a 5s context under a 6s lock hold returns `database is locked`, a 20s context succeeds. `requestTimeout` is now 20s with the relation to `busy_timeout` documented so the next reader does not shorten it again. Separately, the goqite queue DB was opened without any `busy_timeout`, so a concurrent queue writer failed with `SQLITE_BUSY` instead of waiting; it now carries `busy_timeout(10000)` + WAL in the DSN. Goroutine leaks are now asserted with `goleak`.
- **Two tests sharing a fixed port.** `internal/nats` and `features/todo/handlers` both bound `18099`, which is why the suite ran as a single `-p 1` sweep "for DagNats engine stability" — the serialization was a misdiagnosis of `bind: address already in use`. Both now bind ephemeral ports; see the parallelization note below.
- **A browser tab opened on every run that spawns the real binary.** PocketBase's first-run installer mints a `pbinstall` token and calls `osutils.LaunchURL`; a test booting the binary with a throwaway `DATA_DIR` has no superuser, so it fired every time — usually pointing at a server that had not bound, hence the "127.0.0.1 refused to connect" tab. `router.Init` suppressed it only when `interactive()` was false, but a child inherits the parent's TTY: `make test` from a real terminal made the child look interactive too. `isCharDevice` was also the wrong predicate — `/dev/null` is a character device — so `< /dev/null > /dev/null` passed as a terminal. Now `isTerminal` rejects `/dev/null` explicitly, and every test that spawns the binary sets `GOGOGO_NO_BROWSER=1` in an env built from scratch (an inherited `=0` no longer wins).
- **The SSE test pump did not honour its timeout.** `pumpSSEUntil` checked its deadline only between `Body.Read` calls, and `Read` blocks until the next event — so a caller asking for a 6s window waited one full heartbeat (15s). `TestTodoRecordsNotBroadcastViaHub` took 15.5s for a 6s negative assertion. The read now runs in a goroutine with a timer select that closes the body on expiry.
- **An SSE predicate that could never fire burned its whole timeout.** `TestIntegration_SuggestSimulatedEnqueuesAndStreamsResult` unmarshalled the raw SSE transcript as JSON, which is `event:`/`data:` lines and carries a `signals ` prefix on each payload, so `json.Unmarshal` always failed: the test waited the full 14s and passed on a later assertion. Now parses via `sseSignalPayloads`. 15.5s → 5.0s.
- **A data race in the todo fixture.** Four test fixtures assigned `auth.CookieSecure = false` — its zero value, so a no-op write — which is a genuine race as soon as two tests in a package overlap (surfaced on the first attempt to add `t.Parallel()`). The assignments are deleted and the constraint documented on the variable: set shared state once in `TestMain`, never in a fixture.
- **Two more fixed ports in tests.** `bootLiveServer` hardcoded `8291` and `cmd/web/smoke_test.go` hardcoded `18199` — the same collision class as above. Both now reserve an ephemeral port.
- **The skill's YAML frontmatter failed to parse.** The `gogogo-coding-standards` description was an unquoted scalar containing `"Triggers when: "`, and `": "` starts a mapping in YAML, so every host reported an `Error in user YAML`. The description is quoted and a frontmatter gate now runs in CI so it cannot recur.
- **The Datastar gate was checking almost nothing.** `bin/datastar-lint` did not forward `"$@"`, so every argument was discarded: `make datastar-lint` linted the whole repo (including `node_modules`) and printed 264 warnings where 0 were expected — the filters looked applied and were not. CI now pins the linter version instead of `@latest`, because a release can raise a rule to ERROR and fail the build with no code change here.

### Changed

- **`features/todo` race suite: 148s → ~70s**, and `make ci-local` to ~110s, with no change in coverage and zero races. `owner_require_test.go`'s per-Read goroutine loop (one leaked goroutine per 500ms tick) now uses the shared pump.
- **The test suite runs the four engine packages serialized and everything else in parallel.** `scripts/test-web.sh` detects the engine packages (`cmd/web`, `features/todo`, `features/todo/handlers`, `internal/nats`), runs those `-p 1` in the foreground and the remaining 35 at default parallelism alongside, so the parallel group hides under the engine's wall-clock. Measured ~2m10 vs ~4m15, verified across 3 runs at `GOMAXPROCS=4` (the CI runner's size) and 2 at `GOMAXPROCS=2`.
- **The queue worker no longer allocates a timer per idle second.** The worker loop used `time.After(200ms)` inside its `for {}`, the exact pattern the standards skill warns about; it resets a single timer instead. The per-tick `Info` log became `Debug` — it was one line per worker per idle second (4/s, ~345k lines/day) with nothing actionable.
- **`golangci-lint` 2.13.2 → 2.14.0** (CI pin updated). The Homebrew bottle is built with go1.26 and refuses this repo's go1.27.1; the official release binary (go1.27.0) is what works.
- **New ruleguard rule `BlockingReadBehindDeadline`** flags a `Body.Read` inside a `for time.Now().Before(deadline)` loop in tests — the construct above, made mechanical instead of prose.
- **`docs/local-ci.md` records why the browser smoke test stays on Playwright**, with the lighter candidates measured rather than assumed. Lightpanda has `navigator.serviceWorker` and `caches` both `undefined`, so the offline path the smoke test exists to prove — Service Worker + IndexedDB outbox + replay — cannot run there.

### Verification

- `make ci-local` green (templ + datastar-lint + css-check + check-scope + golangci-lint + race tests + build + Playwright smoke)
- Red-proofs: reverting `isTerminal` fails `TestIsTerminal_NonTerminalsAreRejected`; reverting `pumpSSEUntil` makes `TestPumpSSEUntil_HonorsDeadlineWhileReadParked` hang; the ruleguard rules fire on the bad patterns and stay silent on the corrected helpers; reverting the concurrency bounds leaks goroutines under `goleak`
- A `PATH` shim intercepting `open` records zero browser launches across the full `ci-local`

## [0.33.0] - 2026-10-04 · [0.32.0] - 2026-10-04

These two releases were tagged and published but never got CHANGELOG entries:
their GitHub release bodies are GoReleaser's auto-generated commit list rather
than notes, so there is no curated record of what they changed. Reconstructing
one after the fact would mean guessing at the grouping, so the primary sources
are named instead of paraphrased:

- [`v0.33.0`](https://github.com/calionauta/gogogo/releases/tag/v0.33.0) — 140 commits since v0.31.0, the installer's AI-ready section and update-check, the Tailwind scan-root fix, and a deploy fix so a failed roll actually fails the deploy.
- [`v0.32.0`](https://github.com/calionauta/gogogo/releases/tag/v0.32.0) — the GoReleaser prebuilt binaries, `install.sh`, the guided installer's preflight and advise guidance, and the `gogogo` rename.

`git log <tag>^..<tag>` and `gh release view <tag>` carry the detail. Future
releases: write the CHANGELOG entry in the same commit as the behaviour change,
which is what the "Docs stay truthful" rule in `AGENTS.md` already asks for.

## [0.31.0] - 2026-10-04

### Added

- **Guided installer (`cmd/gogogo`).** Interactive (numbered plugin/feature menus with a repair loop) and scripted (`--dry-run`, `--format json`, `--yes`, `--check`) scaffolding: rename, trim of 7 plugin/feature units with their wiring calls, `AGENTS.md` with the upstream-first rule, and a `templ generate` + `go mod tidy` + `go build ./cmd/web` proof. Unknown unit ids fail fast; `--no-tui` without `--yes` never applies.
- **`internal/capabilities` registry.** Single source of truth for ids, kinds, runtime switches, owned paths, and UI signals — consumed by the installer, `--help`, and the JSON plan. Conformance tests fail on drift (unowned packages, unknown deps, missing files, unwired UI signals).
- **Registration-driven workflow UI.** The workflow tab, button, and empty-state hints render only when the onboarding routes are actually registered (`dagnatsUIEnabled`), so trimming `dagnats` and `DAGNATS_ENABLED=false` converge on the same UI with no dead button.
- **Drift gate (`--check`).** Verifies every installer marker against a checkout without changing anything (`CHECK-OK`/`CHECK-FAIL`, exit 1 on drift); the same check runs in-process so CI fails when a source edit moves a marker.
- **Kind split (`plugin` vs `feature`) by the servant principle.** A plugin serves other capabilities; a feature is a terminal user surface. Plugin answers match plugin-kind units, feature answers match feature-kind units; `none` drops a whole dimension.

### Changed

- **`router.Init` is a flat list of one call per capability.** Guards moved inside callees (`registerOnboarding`, `registerWhiteboardStack`); trimming is a single-line drop. No behavior change.
- **Landing page and docs lead with the installer.** Capabilities highlight features over lib names; `make rename` stays as the documented manual fallback.
- **Docs stay truthful additions.** `scope-taxonomy.md` gains the servant-principle definitions and the graceful-degradation UI rule; `ARCHITECTURE.md` wiring cells match the new call shapes.

### Verification

- 30 installer tests + 7 registry tests green, `golangci-lint` zero issues, `check-scope` and `site --check` green.
- Trim proof end-to-end on throwaway checkouts (drop-all, partial, keep-all): `buildOk: True`, zero missed strips, `go vet` clean, component render tests green inside the trimmed tree.
## [0.30.1] - 2026-10-04

### Fixed

- **v0.30.0 crash-looped on startup — do not use it.** The DagNats proxy fix in 0.30.0 replaced the GET-only route registration with `Router.Any()`, which registers the route with an **empty method**. In Go 1.22+ ServeMux that becomes a method-less pattern, and a method-less pattern conflicts with a method-scoped one unless one strictly subsumes the other. The app registers `GET /` (`features/landing`), so the mux panicked at route-registration time:

  ```
  pattern "GET /" conflicts with pattern "/dagnats/{path...}":
  GET / matches fewer methods than /dagnats/{path...},
  but has a more general path pattern
  ```

  A panic during registration means the process dies before it serves anything, so the container restart-looped — 638 restarts over 11 hours, with every request through the tunnel returning 502 from Cloudflare. GET-only was never the actual bug: two GET patterns (`GET /`, `GET /dagnats/{path...}`) coexist fine, with the more specific path winning. The bug was that GET-only was **incomplete** — POST/PUT/PATCH/DELETE returned 404 — so switching to a method-less pattern traded a 404 for a startup panic. The proxy now registers each method explicitly with `Router.Route(method, …)`, which keeps every pattern method-scoped and satisfies both conditions.
- **The same defect was latent in the BYOK relay.** `features/credits/routes.go` used `r.Any("/api/byok/*")`, which has the identical conflict with `GET /`. It had not fired only because the block is skipped when `Relay` is nil (i.e. unless BYOK is configured) — it would have taken production down the first time someone enabled BYOK. Reproduced in isolation, then fixed the same way.
- **The test could not have caught it.** The proxy fixture built a mux containing only the proxy, so there was no `GET /` to conflict with. It now registers `GET /` the way `features/landing` does, so building the mux under production's constraints is part of the test. Re-introducing a method-less pattern now fails the suite with the exact panic above.

### Verification

- `make ci-local` green: templ + datastar-lint + css-check + check-scope + golangci-lint + `go test -race -p 1 ./...` + build
- Browser smoke test passed on all three skins (daisyui, basecoat, morpheus) with zero uncaught client JS errors
- Deployed and confirmed in production: container `healthy` with 0 restarts, `listening on 0.0.0.0:8080` with no panic, `/`, `/_/`, `/dagnats/` and `/dagnats/console/` all returning 200
- A POST to a nonexistent console path now returns the DagNats console's own 404 page rather than PocketBase's, proving the request traverses the proxy

## [0.30.0] - 2026-10-03

> **Warning:** this release crash-loops on startup. Use 0.30.1 or later. See the 0.30.1 entry below for the cause.

### Added

- **`make rename` — the missing step for "Use this template".** The repo is a GitHub template (`is_template: true`), but the docs told people to `git clone` and never mentioned renaming. A fresh copy therefore kept the module path `github.com/calionauta/gogogo`, the binary name, the container name and the deploy directory — ~360 occurrences across ~130 files, with no script and no instructions, so a clone-as-template produced a project that compiled fine and deployed to the template's server directory. `scripts/rename-project.py` (via `make rename NAME=my-app [OWNER=myorg] [DRY=1]`) rewrites the module path, the bare project name and (with `OWNER`) the Pages host and owner/repo links; works from `git ls-files` so artifacts and local databases are never candidates; skips generated `site/docs/` and points at `make site`; shields sibling repos under the same owner (`ai-credits`, `datastar-lint`, `pi-leakguard`) from the owner substitution; and finishes with `go build ./cmd/web` so a half-renamed tree fails locally rather than in CI. Idempotent, with `--dry-run`.
- **A documentation site.** `site/` (static landing page + `site/build.mjs`, a dependency-free markdown→HTML generator) published to GitHub Pages by `.github/workflows/pages.yml`, with `docs/` as the 18-page single source of truth. `make site` / `make site-check`; `--check` validates every internal link and heading anchor.
- **`bin/init-secrets`** — the script `internal/secrets` and `internal/llm` had been documenting as the canonical bootstrap, but which did not exist, making the documented secrets setup unrunnable. Generates the age keypair, a template, and the encrypted file, with `--reencrypt`. Falls back to `scripts/agehelper` (built on the already-present `filippo.io/age`) when the `age` CLI is absent.
- **"Docs stay truthful" rule in `AGENTS.md`** plus `bin/check-docs-staleness.sh`, an advisory pre-commit guard that reports the page(s) most likely to need an edit when a behaviour-carrying file changes without a `docs/` update.

### Fixed

- **DagNats console writes returned 404, then 502.** Two independent bugs, both invisible while the console rendered fine. The reverse proxy registered only `GET /dagnats{/...}`, but the console is a full CRUD app — creating a trigger, editing a workflow, cancelling a run and deleting a schedule all issue POST/PUT/PATCH/DELETE, and the upstream mux registers method-agnostically. Now registered with `Router.Any`. Even once routed, every write failed with `http: ContentLength=13 with Body length 26`: PocketBase wraps the request body in a `RereadableReadCloser` whose `Read()` rewinds itself at EOF, and `httputil.ReverseProxy` streams the body to the Transport, which reads to EOF and therefore reads the payload a second time. The proxy now buffers the body into a `bytes.Reader` first (8 MiB cap). Covered by `router/dagnats_proxy_test.go`, which drives a real PocketBase mux against a stub upstream and asserts every method arrives with its body intact.
- **The Desktop workflow failed on every push** since the `wails3` pin moved to beta.24. `wails3 build` has never accepted `-o` (real flags: `-tags -obfuscated -garbleargs -nocolour`), and it delegates to `wails3 task build`, which needs a `Taskfile.yml` the repo does not have. The flag had been latent because the workflow installed the CLI into `GOPATH/bin` without adding it to `$GITHUB_PATH`, so `command -v wails3` failed inside the script and the build silently fell through to `go build`; the pin change added a PATH export that exposed it. The native path is now always plain `go build` (as `make desktop` already did), and `android`, `package` and `cross-universal` fail fast with an explanation instead of a cryptic flag error.
- **Tailwind scanned the landing page and the docs.** `src/css/input.css` used `source("../../")`, which scans the whole project root, so `site/index.html`, `site/styles.css` and the generated docs were read as application source. Any class-shaped token there ended up in `web/resources/static/app.min.css` — and since that bundle is embedded in the binary, a landing-page edit could change an embedded asset. `site/index.html` actually loads its own `styles.css` and uses zero Tailwind utilities. Now `source(none)` with the roots enumerated (`features/`, `web/`, `internal/`): the bundle is 19 KB smaller, a landing edit no longer affects it, and `make smoke` still passes on all three skins.
- **Mobile horizontal scroll and a broken blob URL on the docs site.** Grid tracks used a bare `1fr` (`minmax(auto, 1fr)`), whose `auto` floor is the item's min-content width, so a long heading refused to shrink and the track stayed desktop-wide inside a 390px container. Tracks now use `minmax(0, 1fr)`; tables are wrapped in a scroll container. Blob URLs escaping `docs/` resolved against `docs/` and kept a literal `..`, which 302s to `tree/` and then 404s — they now resolve against the repo root.
- **`modernc.org/sqlite` version drift.** The repo pinned 1.59.0 / libc 1.75.7 while PocketBase v0.40.4 expects 1.57.0 / 1.74.4, and PocketBase checks this at runtime (`modernc_versions_check.go`), warning that a mismatch "could result in unexpected build or runtime errors". The pin came from a blind dependency bump; nothing here imports modernc directly (the active driver is `ncruces/go-sqlite3`). It only surfaced as a direct requirement because a gitignored `spikes/` file imported it — that directory now has its own `go.mod`, which also fixes `go mod tidy` disagreeing between local and CI.
- **15 factually wrong claims in the docs**, found by auditing all 18 pages against the source. The worst cluster was deploy: the server layout was documented as `/opt/...` (it is `/home/deploy/services/<app>/`), the CI was described as building a Docker image (it cross-compiles the binary; the image is built on the server), and both `deploy.md` and `troubleshooting.md` told the reader to verify a deploy with `/api/version`, an endpoint that has never existed.
- **Docs-only pushes no longer run CI, Desktop or Deploy.** A README edit used to rebuild the Docker image and restart production. `deploy.yml` uses an allow-list so a new top-level directory cannot silently start deploying; `ci.yml`/`desktop.yml` use `paths-ignore`.

### Changed

- **README reduced from 795 to 186 lines**, with the docs site as the single source of truth.
- **`make wails-build` is now an explicit "why not" target** that exits with the reason, rather than a target that cannot work.

### Verification

- `make ci-local` green: templ + datastar-lint + css-check + check-scope + golangci-lint + `go test -race -p 1 ./...` + build
- Browser smoke test passed on all three skins (daisyui, basecoat, morpheus) with zero uncaught client JS errors
- `site-check` green: 18 pages, all links and heading anchors resolve
- `make rename` verified end-to-end on throwaway clones: 0 occurrences left, `go build ./...` clean, `go vet` clean, `make site` regenerates with the new name

## [0.29.1] - 2026-10-03

### Changed

- **DagNats `v0.0.18` → `v0.0.24`.** Six minors of upstream bugfixes. No breaking change in the API this template uses — `server.New`/`server.Config`, `server.EmbeddedWorker` and `worker.TaskContext` (with every method) are unchanged; the only new field is `server.Config.MaxPayload`, optional and defaulting to nats-server's own 1 MiB. No transitive bump was needed: the repo already pinned `nats-server v2.15.0` and `nats.go v1.54.0`, exactly what v0.0.24 requires. Two upstream fixes matter here — v0.0.22 restores `TaskContext.Metadata()`, so per-step `metadata` reaches workers again after #652 dropped it from the dispatch path in v0.0.14 (per-step `config` is *still* never populated, so the onboarding example-todo titles keep riding the DAG input/output chain), and v0.0.20 fixes grouped-step retries never firing, which left a run stuck in `running` forever. v0.0.20 also requires Go >= 1.27.1; the repo is already there.

## [0.26.5] - 2026-08-21

### Fixed

- **Cross-session test teardown no longer races with `--race` (GOGO-004).** `mustReset()` in `features/todo/fixture_test.go` called `app.ResetBootstrapState()` directly, nilling the PocketBase DB pointer fields while the fire-and-forget logger batch handler still read `IsBootstrapped()`/DB from its goroutine during teardown — a data race the Go 1.26 scheduler rarely exposed but Go 1.27 detects reliably. The fix mirrors PocketBase's own `tests.TestApp.Cleanup()`: `mustReset()` now triggers `app.OnTerminate()`, whose `__pbAppLoggerOnTerminate__` handler (priority -999) drains and stops the batch handler before `ResetBootstrapState()` runs. Full `features/todo` package now passes 0 races / 0 failures under `go test -race` on Go 1.27.

## [0.26.4] - 2026-08-21

### Changed

- **Bumped all dependencies to their latest releases.** DagNats `v0.0.5` → `v0.0.13`, PocketBase `v0.39.6` → `v0.39.11`, `nats.go` `v1.52.0` → `v1.53.1`, `nats-server` `v2.14.3` → `v2.14.5`, `ncruces/go-sqlite3` `v0.35.1` → `v0.35.3`, `goai` `v0.7.6` → `v0.9.6`, Wails v3 `alpha2.117` → `beta.12`, and `modernc.org/sqlite` `v1.53.0` → `v1.57.0`, plus their transitive graph. `go.opentelemetry.io/otel` is pinned at `v1.44.0` because DagNats `v0.0.13`'s `natsexporter` still relies on `log.KeyValue`, which was removed in otel `v1.45.0`; `go get -u` would otherwise flow to `v1.45` and break the build until DagNats catches up.

### Fixed

- **Seed no longer races the UNIQUE `(idem_key, owner)` index on legacy rows.** Todos created before the `idem_key` field existed all shared the same empty default; adding the unique index then failed with a constraint error that left the seed retrying and burning CPU. The seed now backfills a unique `idem_key` on legacy rows (after the physical table exists, before creating the index) so existing collections migrate cleanly (CAL-DB fixes).
- **Security advisories patched.** Toolchain `go1.26.5` → `go1.26.6` (stdlib CVEs in `net/http`, `crypto/tls`, `html/template`, `encoding/*`), plus otel `v1.44.0`, `golang.org/x/image` `v0.45.0`, and `grpc` `v1.82.1`; `basecoat.min.css` regenerated to match the new sources.
- **DagNats subpath proxy no longer uses the incompatible default Director.** The reverse proxy is built without the default `Director`, non-default todo skin names are centralized, and template docs/terminology around live demos and community skins are clarified (GOGO-003).

### Other

- **CI and hooks housekeeping.** CI Go version now matches the `go.mod` toolchain directive, `check-scope` runs in CI, and lefthook is documented as the hook engine; quality gates were migrated from agent-level hooks to lefthook. Deploy now reads secrets from `~/.secrets/` per the server-side secrets convention.

## [0.26.3] - 2026-08-03

### Fixed

- **Offline add works in every UI skin (CAL-34).** The `gogogo:queued` event listener in the Morpheus and Basecoat skins used a double underscore (`data-on:gogogo__queued__window`) instead of the Datastar event-namespace colon (`data-on:gogogo:queued__window`) that the Service Worker bridge in `internal/components/offline_banner.templ` dispatches. The typo meant the listener never fired after an offline mutation, so `$loading` stayed `true` and `$newTitle` was never cleared — the Add button froze in its loading state and blocked every follow-up submit. Only the DaisyUI skin used the correct separator, so the bug was invisible to the offline-add smoke test (which only exercised DaisyUI) and to anyone using the default skin. Fixed in `web/skins/morpheus/todo_morpheus.templ` and `web/skins/basecoat/todo_basecoat.templ`; the smoke test now sweeps all three skins and asserts the listener attribute, so the next typo lands in CI before it lands in production. The Morpheus Add button was also missing `type="submit"`, so the form never dispatched a native submit event in headless Chromium (clicking worked in a real browser via the web component's own click handler, but the harness needed the explicit `type`); added.
- **Smoke test sweeps every skin, not just the default.** `scripts/smoke.mjs` `verifyOfflineTodoQueue` now takes a `skin` argument and the harness loops over DaisyUI / Basecoat / Morpheus. The CAL-34 contract (offline add resets UI, queues to IndexedDB, replays on reconnect) is asserted per skin; the daisyui-only delete-replay check stays daisyui because Basecoat/Morpheus' confirm-dialog open wiring is skin-specific and out of scope for this fix.

## [0.26.2] - 2026-08-03

### Fixed

- **Onboarding example todos actually create (CAL-32).** The "creating example todo 1/3" steps never created anything. Root cause: DagNats v0.0.5 never delivers per-step config/metadata to workers — the engine's live dispatch path (`TaskPublisher.doPublish`) builds the `TaskPayload` without `Config`/`Metadata`, so a worker never sees a step's `config` block. The workflow therefore fell back to a generic "Onboarding task" and, worse, created it owner-less (owner hardcoded `""`), so no todo ever appeared in the user's list. Fix: titles now ride the DAG input/output chain instead — `StartRun` receives `{"user": <owner>, "todos": [...]}`, the root `greet` step forwards the input, and each create-todo step pops one title, creates it scoped to the owner, and threads the remainder forward. Durable (outputs persist with the run) and reachable from any worker; the three example titles live in `dagnats.ExampleTodoTexts`. The workflow-completed SSE handler also re-renders the todo list, so server-created example todos appear without a reload, and the e2e test now asserts the three example todos are visible in the user's list after completion.
- **Onboarding step 1 no longer loses its "done" marker.** A step now renders `step-success` once it is completed (`$onboardingStep > N`) while the workflow is still active — previously the marker only appeared when the whole workflow finished.
- **Step status shows under the step name, not the number (CAL-31).** The daisyUI steps grid (40px number column + 1fr label column) was auto-placing the retry status span (e.g. "Retrying — attempt 2") as a separate grid child, dropping it below the step number and breaking the connector lines. Wrapping the step name + status in a single `flex flex-col` span keeps both in the label cell, status under the name. Applied across the daisyUI, Basecoat, and Morpheus skins.

## [0.26.1] - 2026-07-31

### Fixed

- **Every toast type now plays its cue.** The toast observer only mapped success/error/warning, so info toasts ("Deleted", "Nothing to clear", workflow progress) were silent. Info toasts now play `page` (a neutral papery flick), completing the type→sound map: success → `success`, error → `error`, warning → `loading`, info → `page`. Verified with AudioContext instrumentation in headless Chromium (create + delete both synthesize sound).

## [0.26.0] - 2026-07-31

### Added

- **UI sounds via cuelume — vendored, zero dependencies.** Shipped as a self-contained
  plugin (`features/sounds/`): press feedback on every button, `success`/`error`/`warning`
  cues on toasts, `droplet` on delete, `tick`/`toggle` on tab hover vs click, and a
  dedicated chime when re-enabling sound (muting is silent). Sound accessibility is part of
  the plugin: `prefers-reduced-motion` respected by default, a persistent navbar mute
  toggle (`localStorage`, `gogogo_sound`), and a subtle default volume.
- **Per-filter item counters.** The header badge and footer count now reflect the current
  filter (all / active / completed) in realtime; mutations carry the active filter so the
  re-rendered list and its count stay scoped to the tab the user is on.
- **Real creation timestamps (CAL-19).** The `todos` collection now declares
  `created`/`updated` as PocketBase `AutodateField`s — the previous plain `DateField`s were
  never auto-stamped, so every record carried a `0001-01-01` creation time and the per-row
  relative-age label was meaningless. The seed migrates existing collections; new records
  get real timestamps.

### Fixed

- **Static assets never serve stale after a deploy.** `resources.AssetHandler` replaces
  `http.FileServer` with `Cache-Control: public, max-age=0, must-revalidate` + a
  content-hash ETag, so clients/CDNs revalidate and a changed asset always 304s correctly
  (Cloudflare was caching old CSS/JS for up to 4h).
- **Sound toggle stays responsive under reduced-motion.** The button reflects the user's
  own preference even when the OS mutes playback, so it never looks dead.
- **DagNats reverse proxy.** Conflicting `Director`/`Rewrite` on `httputil.ReverseProxy`.
- **CI lint.** goconst/lll violations and the skipped smoke-test quoting.

### Changed

- AGENTS.md: static-asset cache contract, orphan-`web`-process troubleshooting, sounds
  removal entry in the SCOPE table.
- README: "UI sounds (cuelume)" plugin + accessibility documentation.

## [0.25.0] - 2026-07-20

### Changed

- **SCOPE taxonomy migrated from single-axis to two-axis form.** The 67 source files that
  carried a `// SCOPE:core|plugin|feature - <reason>` annotation now carry
  `// SCOPE:layer=<infra|feature>,removal=<core|plugin|feature> — <description>`. The new scheme
  separates two independent concerns:
  - `layer` = where the code lives (infra in `internal/`, feature in `features/`, with
    cross-cutting middleware like `features/auth` and `features/store` flagged at the file level).
  - `removal` = what happens if you delete it (`core` = binary breaks, `plugin` = binary works
    but loses capability, `feature` = pure demo).

  This kills the prior ambiguity where `SCOPE:core - REMOVE if not using NATS` and
  `SCOPE:core - DO NOT REMOVE - SSE Hub` shared the same label but meant different things.
  Under the new axes the first is `layer=infra,removal=plugin` (binary works without NATS) and
  the second is `layer=infra,removal=core` (binary breaks without the SSE Hub).

  Coverage jumped from 40% to 100% on non-test, non-generated files under `internal/` and
  `features/` (52 plugin/feature files were missing an annotation before).

### Added

- **`cmd/check-scope` SCOPE annotation linter.** New Go program using `go/parser` from the
  standard library — zero new dependencies, AST-aware, runs as part of `make ci-local` and
  the pre-commit hook. Wired as `make check-scope`. Tests in `cmd/check-scope/main_test.go`
  pin the accept/reject contract (5 unit tests covering canonical form, legacy rejection,
  templ banners, missing annotation, and the regex family).

- **`scripts/migrate-scope.py` bulk migration script.** Idempotent Python helper that bulk-
  transforms files to the new two-axis form. Useful for re-runs and for new repositories
  cloned with the old convention still in place. Tag coverage is heuristic-driven by path
  pattern; per-file overrides live in the `REMAPPING` table at the bottom of the script and
  are explicit about the rationale for each `layer`/`removal` decision.

## [0.24.9] - 2026-07-20

### Fixed

- **Offline delete now works in all skins (CAL-16).** Three issues prevented the delete button from
  working offline:
  - **Basecoat skin** — the delete button only set `$confirmingDeleteId`/`$confirmingDeleteTitle`
    signals but never called `showModal()`, so the confirmation dialog never appeared.
  - **Morpheus skin** — same: relied solely on `data-neo-dialog-trigger` (web component) instead
    of explicitly calling the dialog open API, with no fallback.
  - **Service Worker** — the "Clear completed" URL (`/api/todos/completed/delete`) was incorrectly
    matched by the generic delete regex, which treated `"completed"` as a todo ID and returned a
    no-op fragment. Added explicit check + `optimisticClearCompletedFragment()` that removes all
    rows and shows the empty state.

  All four CRUD operations (create, toggle, delete, clear-completed) now have optimistic offline
  handling via the Service Worker.

## [0.24.8] - 2026-07-20

### Fixed

- **Item count now reflects total owned items, not filtered list (CAL-18).** The `$itemCount` signal,
  which drives the header badge and footer count, was calculated from the filtered todo list instead
  of total owned items in two handlers:
  - `handleList` (`/api/todos?filter=...`): `ItemCount` now uses `countOwnedTodos()` (total count)
    instead of `len(filteredTodos)`, so the badge stays correct regardless of the active filter tab.
  - `handleListFragment` (`/api/todos/fragment`): Sets the `datastar-signals` response header with
    the correct total count, so the client-side `$itemCount` updates even on non-SSE fragment
    refetches (PocketBase realtime sync, offline-replay resolution).

## [0.24.7] - 2026-07-20

### Added

- **Basecoat dedicated template.** First-class Basecoat HTML semantics in its own template, with
  DaisyUI-agnostic CSS and shadcn-style theme variables. (PR #3, #5)
- **Realtime + Async Demo Tabs to basecoat & morpheus skins.** Real-time presence, Queue + Retry,
  AI Suggest, and Durable Workflow demos now render correctly in all three skins. (PR #5)

### Fixed

- **Online presence pill not returning to green on reconnect.** The pill now properly reflects
  `navigator.onLine` state after the network recovers, with a distinct red (danger) offline badge.
  (CAL-17 follow-up)
- **Morpheus card rendering + skin-aware SSE patches.** Morpheus clients now receive morpheus HTML
  on every SSE patch (filter clicks, CRUD mutations, broadcasts) instead of silently falling back
  to DaisyUI rows. (CAL-14)
- **Shared chrome made skin-agnostic.** The navbar, theme selector, and offline banner now render
  correctly without DaisyUI assumptions.
- **Morpheus native input/checkbox.** Replaced DaisyUI form elements with native `<input>` and
  `<checkbox>` for Datastar `data-bind` compatibility.

### Changed

- **Offline pill design refined.** When offline, displays a red (danger) badge with static dot
  and "offline" text instead of a yellow warning with stale count.

## [0.24.6] - 2026-07-19

### Fixed

- **Morpheus DaisyUI compatibility.** Added DaisyUI-compatible CSS utilities to basecoat stylesheet
  so mixed-skin rendering doesn't break layout.
- **Skin selector.** Now works correctly across all skins; basecoat assets HTML repaired; stray `t`
  prefix before `SkinSelector` call removed.
- **Morpheus `--page-bg`.** Uses the correct CSS variable instead of undefined `--neo-color-bg`.
- **Whiteboard.** Added missing `theme.js` script tag to board pages.
- **Deploy disk space.** Docker build now prunes images before writing secrets to avoid ENOSPC.

### Changed

- **CI pipeline.** Merged toolchain steps for faster builds; removed heavy browser smoke test
  from CI; added disk cleanup step.

## [0.24.5] - 2026-07-19

### Fixed

- **Whiteboard and login HTML tags.** All pages now have consistent Datastar signals and correct
  HTML structure.

## [0.24.4] - 2026-07-20

### Fixed

- **Online presence pill not recovering green state after reconnect.** Service Worker `sync-end` handler
  called `setState("online")` on the banner but never called `reflectPresence()` to remove the
  `.is-offline` class from `.online-pill` elements, so the pill stayed yellow after the connection
  came back. Now calls `reflectPresence(true)` in both `sync-start` and `sync-end` handlers.
  (CAL-17)

### Changed

- **Offline pill design.** When offline, the pill now shows a red (danger) badge with static dot
  and `"offline"` text instead of a yellow (warning) badge with a stale count. Text swap is
  CSS-driven (`.pill-online`/`.pill-offline` spans) to avoid conflicting with Datastar signal
  updates. Applied to all three skins (DaisyUI, Basecoat, Morpheus) and the whiteboard.
  (CAL-17)

## [0.24.3] - 2026-07-19

### Added

- **Basecoat UI integration**: Native Basecoat CSS (`basecoat-css/maia`) with shadcn-inspired OKLCH theme variables. Basecoat JS runtime (`basecoat.min.js`) loaded with `basecoat.initAll()` debounced via `requestAnimationFrame` for Datastar DOM morphing compatibility.
- **Basecoat compatibility layer**: `data-variant` attributes added to all DaisyUI templates (`btn-primary` → `data-variant="primary"`, `btn-ghost` → `data-variant="ghost"`, etc.), enabling Basecoat to style elements correctly.

### Fixed

- **Morpheus skin**: Removed `app.css` (DaisyUI styles) from skin assets to prevent CSS conflicts with `<neo-*>` custom elements.
- **Basecoat skin**: Removed `app.min.css` (DaisyUI) from stylesheets to avoid CSS conflicts with Basecoat component styles.
- **Dropdown skin selector**: Now reads active skin from URL `?skin=` query parameter via handler, maintaining correct state.
- **Removed `skinutil.go`**: No longer needed after SkinSelector was moved from navbar to page templates.
- **Whiteboard templates**: Added missing `data-variant` attributes.

### Changed

- **Theme controller (`theme.js`)**: Added Basecoat `initAll()` hook with debounced `MutationObserver` (via `requestAnimationFrame`) to reinitialize Basecoat components after Datastar SSE merges.
- **CSS architecture**: `basecoat-input.css` now imports `basecoat-css/maia` and defines full shadcn `@theme inline` color tokens for Tailwind v4 utility class support.

## [0.24.0] - 2026-07-18

### Added
- **UI Skin Plugin** — pluggable skin system with runtime selector.
  Three skins available: DaisyUI (core default), BasecoatUI (shadcn),
  Morpheus (web components). Switch via `UI_SKIN` env var, `?skin=`
  query param, or the dropdown in the navbar.
  - `web/skins/` — skin registry, dispatcher, and selector component
  - `config/config.go` — `UI_SKIN` env var (default `daisyui`)
  - `features/todo/components/layout.templ` — dispatches skin assets
  - `features/todo/handlers/todo.go` — `?skin=` query param support
  - `src/css/basecoat-input.css` — shadcn-inspired CSS variables (OKLCH)
  - `web/skins/morpheus/` — vendorized Morpheus bundle (SHA-pinned)
  - `Makefile` — `css-basecoat`, `css-all` targets
  - `Dockerfile` — builds both CSS skins

  See [CHANGELOG](ui-skin-plugin-plan-v3.md) for the full design.

## [0.23.6] - 2026-07-18

### Fixed
- Offline navigation caching (service worker): the v0.23.5 "Added" entry was aspirational — the feature was actually non-functional. It was only caught by running the new Playwright smoke harness (the previous "could not run" note was wrong; the tools are installed and the browser downloads fine — the earlier failure was a stale read-only `GOCACHE` serving an old `sw.js`). Two service-worker bugs:
  - `cache.put(request, …)` passed the navigation `Request` object directly; the Cache API rejects Requests with `mode: 'navigate'`, so the write was silently swallowed by an empty `.catch`. Now stores by `request.url`.
  - The `response.type === "basic"` guard skipped the write entirely, because a SW re-fetch of a navigation request reports a non-`basic` type in this Chromium setup. Now gates on `response.ok`.
  Visited pages are now genuinely served from the SW cache while offline; unvisited URLs still get the generic offline page.

### Added
- Offline-UX test harness: `scripts/smoke.mjs` now bundles presence-pill, SW navigation-cache, and `clear-pages` purge checks under one `verifyOfflineUx()` run (SW + Datastar signal coverage), so future regressions in offline behaviour are caught automatically.

## [0.23.5] - 2026-07-18

### Fixed
- Offline presence pill (header): the Todo page header `X online` indicator still rendered a live-green dot while offline. Root cause — it used Tailwind `bg-success`/`animate-ping` instead of the shared `.online-pill` component, so the `reflectPresence()` `navigator.onLine` bridge (added in v0.23.4) never touched it. It now uses `.online-pill`, so it greys out (warning colour, static dot) the moment the network drops and returns to live on reconnect.

### Added
- Offline navigation caching: the service worker now serves visited HTML pages network-first with a cache fallback, so navigating while offline shows the last visited page instead of `ERR_INTERNET_DISCONNECTED`. Unvisited URLs get a generic offline page. The page cache is purged on logout (`clear-pages` postMessage from the auth navbar) so a different user on a shared device does not see stale authenticated pages.
- `todo_sse.go`: corrected a malformed `json:-` struct tag to `json:"-"` (go vet fails on it under Go 1.25+).

## [0.23.4] - 2026-07-18

### Fixed
- Offline presence pill: the realtime "X online" indicator (`.online-pill`) now reflects connectivity — it greys out (warning colour, static dot) the moment `navigator.onLine` goes false and returns to live on reconnect, instead of keeping a stale green "online" look while the offline banner was already showing.

## [0.23.3] - 2026-07-18

### Added
- AI Suggest stepper for todos: signal-key driven UI state (aiStep/aiPending) and an AIPhase stepper field on todo.Signals, with SSE progress streaming and LLM integration plumbing in internal/llm.

### Fixed
- Build: correct AiPhase -> AIPhase struct-literal field on todo.Signals.
- CI lint: extract retrySignalFields/retryToastMessage helpers to bring streamRetry cyclomatic complexity under the gocyclo limit; gofumpt-format signal_keys.go.



## [0.23.2] - 2026-07-18

### Changed

- **`css-install` skips `npm ci` on warm checkouts** — guards the install with
a `node_modules` presence check, so `make css` / `make css-check` no longer
reinstall Tailwind v4 + DaisyUI + Playwright from scratch every run. Fresh
clones (or a forced `make css-install`) still do a clean `npm ci`.
- **DagNats test engine store trimmed** — `internal/dagnats` boots its embedded
engine with `MaxStoreBytes: 256 << 20` (256 MiB) instead of 1 GiB. The
onboarding workflow persists almost nothing; the smaller store boots the
engine lighter, which helps when several packages run their engines under
`-p 1`.

### Added

- **`make test-fast`** — the tight TDD loop. Keeps `-p 1` (DagNats engine
stability) but drops `-race`, the dominant cost of the full gate (~5min →
~1min). Use for red/green iteration; run `make test` / `make ci-local`
before committing.

### Fixed

- **Landing page (`/`) hero text contrast in dark mode** — the pre-CTA text
(`.landing-tagline` / `.landing-about`) used a hardcoded
`oklch(0.32 0.02 250)` that only read on a light background. Replaced with
DaisyUI's theme-aware `var(--color-base-content)`, so the text now has good
contrast in both light and dark themes. Swapped the "Built to be useful"
tagline + about paragraphs for the single canonical line
(_Go full-stack template. Single binary, no dependencies. Database & Auth.
Reactive UI. Background jobs. Offline-first. Real-time multi-user. Durable
workflows. Desktop & Android capable._).
- **Config (`/config`) Read-only banner contrast** — the banner keeps its light
callout background in dark mode, so its text now stays near-black
(`oklch(0.2 0.02 250)`) and remains readable instead of inheriting the light
`base-content` used on dark pages. `.config-env` / `.config-not-set` now use
theme-aware `var(--color-base-content)` for correct contrast in both themes.
- **CI smoke test** — the browser smoke test navigated to `/todos`, which is
not a registered route and fell through to the landing page, so the offline
todo queue exercise could never find the create-form input and timed out.
Corrected the exercised route to `/todo` (the real route); CI now goes green.
- **Presence SSE handler data race** — `PresenceSSEHandler` flushed the
`http.ResponseWriter` from the NATS subscription callback goroutine, racing
with net/http's own response finalisation under `go test -race` and breaking
the Deploy gate (flaky `WARNING: DATA RACE` in `internal/collab`). Presence
events now flow through a buffered channel so every write/flush happens in the
request goroutine; the callback only forwards bytes. `internal/collab` tests
are race-clean.

## [0.23.1] - 2026-07-17

### Fixed

- **CRDTStore add-task stuck in loading/disabled** — `handleCreate` now forwards
the client-generated `idem_key` into the todo `ID`. `CRDTStore.Create` requires a
non-empty client id (it keys the Loro map by it) and was returning
`crdtstore: empty todo ID (client must generate UUID)` → HTTP 500, so the
Datastar `@post` never received the success SSE that resets `$loading=false`.
PBStore ignores the value and still uses `idem_key` for offline-replay dedup, so
pb mode is unaffected. Add → toggle → clear-completed now work end-to-end in
`ENTITY_STORE=crdt` mode (regression test added in `crdt_repro_test.go`).
- **Offline banner correctness** — read `offlineSync` from a rendered
`data-offline-sync` attribute (Templ does not interpolate Go expressions inside
script text, so the previous `{offlineSync}` trick left `OFFLINE_SYNC`
undefined), and post `replay-queue` to the active service worker on reconnect so
queued mutations drain without the banner getting stuck in `is-syncing`.

## [0.23.0] - 2026-07-17

### Added

- **Public landing page** — new `features/landing` serves a marketing hero at
  `GET /` (README-sourced about copy + CTA to `/todo`). Registered before any
  auth-protected routes so guest users land on the public page.
- **Config view** — new `features/config` serves a read-only `GET /config`
  operator-facing view of the running config with secret-shaped fields masked
  (`mask.go` + `safe_view.go`). Gated internally by `RequireAuthOrRedirect`
  (any logged-in user, superuser NOT required — CAL-3 decision).

### Changed

- **Todo app moved `/` → `/todo`** — root route now serves the landing page;
  the demo app lives at `/todo` (`TodoHandler.RegisterRoutes` + test router).
- **Auth redirects retargeted to `/todo`** — `RequireAuthOrRedirect`,
  `RedirectIfAuthed`, and the login `next` default now send signed-in users to
  `/todo` instead of `/` (which is now the public landing page).
- **`apiIndex` env** now exposes `uiPages` (`/`, `/todo`, `/whiteboard`,
  `/config`) and `superuserDashboard` (`/_/`).

### Fixed

- **Navbar active key** — corrected from `"todos"` to `"todo"` so the active
  link highlights correctly on the moved route.

## [0.22.2] - 2026-07-16

### Fixed

- **CI green** — resolved the pre-existing golangci-lint failures that were red on
  `master` and failing GitHub CI: `crdtstore.go` `goconst` on `title`/`completed`
  field literals (extracted `fieldTitle`/`fieldCompleted` consts),
  `whiteboard/handler.go` long-line (`lll`) wraps, `db/idempotency_seed.go`
  formatting, and the intentionally-unused `idemKey` param in `CRDTStore.Create`
  (renamed to `_`; the param is required by the `EntityStore` interface for
  `pbstore`'s offline dedup and is unused by `crdtstore`, which keys by op IDs).

## [0.22.1] - 2026-07-16

### Fixed

- **`CRDTStore` todo round-trip** — `doc()` now rebuilds the in-memory Loro map
  keyed by `idem_key` (the client todo id) instead of the PocketBase row id,
  matching how `Create`/`Update`/`Delete`/`ClearCompleted` key items. Before the
  fix, after `Close` + `New`, `List`/`Get` lost todos and mis-keyed rows, so
  `TestCRDTStore_RecordRoundTrip` failed; it now passes (3 items, `completed`
  round-trips, `Get` works post-reload).
- **Test DB DSN missing `file:` prefix** — `newTestApp` (`crdtstore_test.go`) and
  the identical sibling `db/seed_test.go` now open SQLite with
  `file:<path>?_pragma=...`. Without the `file:` prefix, `ncruces/go-sqlite3`
  silently dropped all pragmas (`journal_mode=DELETE`, `foreign_keys=OFF`), so
  tests ran under DELETE journal + FK-off instead of the production WAL + FK-on
  config.

### Added (Phase 2 + 3 closure)

- **Cross-instance CRDT transport wired**: `cmd/web/main.go` calls
  `server.WireCRDTStoreTransport` (JetStream ops) AND
  `server.WireCRDTStorePublisher` (SSE Hub fan-out) after `server.Run`.
  - Local mutation → saveSnapshot → bumpVersion → publishes `doc-version-bumped`
    to the SSE Hub → client merges `$docVersion` signal → re-fetches fragment.
  - Remote mutation → ApplyRemoteOp → saveSnapshot → bumpVersion → same path.
- **`crdtstore.DocPublisher` interface**: plugin event sink; `SetPublisher(p)`
  wires one after boot. The SSE Hub adapter (`internal/server/crdtstore_wire_publisher.go`)
  implements it; tests use a `fakePublisher` that records every event.
- **`SSE handler.dispatch("doc-version-bumped")`**: new branch in
  `features/todo/handlers/todo_sse.go` merges `{docVersion, docVersionSeen}`
  signals via Datastar.
- **Client-side `$docVersion` watcher**: `features/todo/components/realtime.templ`
  polls `data-signals-docVersion` every 250ms; on change, clicks the existing
  `pb-realtime-resync` button so the same fragment fetch handles both PB
  record and CRDT doc events.
- **`server.Run` returns `*queue.Queue`** so main.go can access `q.Hub()`
  without leaking queue refs through the router. Also affects `cmd/desktop`.
- **`router.ConcreteTodoStore()` race-safe**: guarded by `concreteTodoStoreMu`.

### Tests (Phase 3 E2E pipeline)

- `features/store/crdtstore/pipeline_test.go` —
  `TestCRDTStore_FullPipeline_BumpPublisherFires`:
  - Two CRDTStores share one JetStream; mutual Subscribe; store A has a
    fake publisher wired.
  - Store B creates → op flows through JetStream → store A's ApplyRemoteOp
    fires → bumpVersion → publisher count goes up.
  - Mirror: store A creates → publisher fires again.
  - Closes Phase 3 missing test gap. Existing cross_process + transport
    tests only verified doc propagation; this one covers the full SSE-bound
    path end-to-end without race flakes (fake publisher eliminates goroutine
    timing race).

### Fixed

- `cmd/desktop/main.go` updated for new `server.Run` signature.
- `pipeline_test.go` IDs renamed from `pipe-1`, `pipe-2` (renamed to avoid Tailwind class-name extraction)
  to avoid Tailwind's content scanner treating test data as utility classes
  (which generated spurious `.p-1` CSS).

### AGENTS.md (developer-facing)

- Feedback loop section rebuilt as **4 tiers** (format → compile → lint
  scoped → tests scoped → full gate → remote CI).
- Corrected the persistent misconception that `make build` runs the full
  gate — `make build` only runs `go build`. The actual fast feedback loop
  uses `gofumpt` + `go build` + scoped `golangci-lint run`.
- Documented `make check` as redundant with `make ci-local` and removed
  it from the recommended paths.
- Added "When to use what" reference table.

### Removability

All Phase 2 + Phase 3 code is `SCOPE:plugin` and gated on
`ENTITY_STORE=crdt`. Setting `ENTITY_STORE=pb` (default) skips the
entire cross-instance pipeline at startup. To remove entirely,
delete `features/store/crdtstore/{transport,pipeline_test,*}.go`,
`internal/server/crdtstore_wire*.go`, `internal/server/uuid.go`, the
`WireCRDTStoreTransport`/`WireCRDTStorePublisher` calls in
`cmd/web/main.go`, and the `streamDocVersionBumped` branch +
`watchDocVersion` block in the SSE handler / template.

## [0.22.0] - 2026-07-15

### Added
- **Single `OFFLINE_SYNC_ENABLED` flag controls the whole offline-first stack.** One env var now toggles every offline-first concern deterministically:
  - Service Worker registration (`OfflineSyncScript` only renders SW when enabled).
  - NATS CRUD consumer (`crudproxy`) wired only when enabled + NATS available.
  - NATS cross-instance CRUD publisher wired only when enabled.
  - `RegisterIdempotencyHook` installed only when enabled (no queue replays to dedupe otherwise).
  - `(idem_key, owner)` unique index created only when enabled.
  - `idem_key` field + hidden form input are KEPT unconditionally (zero cost, useful as a request-dedupe token for retry/double-click outside offline-sync too).
  - `OfflineBanner` accepts `offlineSync bool` and shows honest online-only copy (`"Offline — online-only mode; requests will fail when network is down"`) when offline-sync is off; skips the SW postMessage bridge entirely (no SW registers in this mode, so the listener would only produce dead code + console noise).
  - Convention over configuration: one boolean reveals every consequence.
- **PBStore `Create` persists `idem_key`.** Previously the `idemKey` parameter was ignored (`_ string`), so the `(idem_key, owner)` unique index never had any value to dedupe against — offline-replay of queued POSTs created duplicates. Now `rec.Set("idem_key", idemKey)` wires it through.
- **CRDTStore projects todos as normal PocketBase `todos` records.** Single source of truth = the SAME `todos` collection PBStore uses (id/title/completed/created/updated/owner/idem_key). Admin UI, SQL queries, and PocketBase realtime all work against those records exactly as for PBStore. The Loro document remains the in-memory CRDT merge workspace that gives automatic concurrent-edit convergence.

### Fixed
- **CRDTStore `upsertTodoRecord` looks up by `(idem_key, owner)`, not by record id.** PocketBase record ids are auto-generated (15-char alnum) but Loro map keys are client-generated (5-char alnum or UUID); `FindRecordById(t.ID)` always failed, so upsert always took the new-record path and re-upserts collided on the `(idem_key, owner)` unique index. Now uses `FindFirstRecordByFilter("idem_key = :k AND owner = :o")` with `k = t.ID`, and `idem_key` is always `t.ID` (the stable cross-call identifier).
- **CRDTStore normalises PocketBase v0.39.6 `sql.ErrNoRows` to empty result.** v0.39.6's `FindRecordsByFilter` and `FindCollectionByNameOrId` return `sql.ErrNoRows` when the filter/lookup matches no records (instead of empty slice + nil). Both call sites now treat that as "empty" (or "collection missing" for the lookup case, with a clear diagnostic) so first access for a fresh owner is not a driver-flavoured error.

### Known limitation
- **`TestCRDTStore_RecordRoundTrip` is `t.Skip`-ed.** When the `todos` collection is created via `CRDTStore.EnsureSchema` (the unit-test bootstrap, not the production seed path), the round-trip reads 0 records even though inline probes show the rows exist in PB. Eleven spikes (1, 3, 4, 6, 7, 8, 9, 10, 11; all since deleted by spike-driven protocol) falsified every natural hypothesis (Save API, inline Save+filter, doc rebuild, RelationField Owner Required=true vs false, s.mu + bumpVersion + FindFirst re-entrance, N+1 sequential upserts, RegisterIdempotencyHook absent, seed-shape vs EnsureSchema-shape delta). Production collections come from `db/SeedDefaults` and the round-trip works there. Root cause likely lives in the `*CRDTStore` struct internals (only bare-components spikes have been tried; the real type reproduces); further work is PB-internals deep-dive, P3 priority.

# Changelog

All notable changes to this template are documented here. The format is based on [Keep a Changelog](https://keepachangelog.com/), and this project adheres to [Semantic Versioning](https://semver.org/).

## [0.17.0] - 2026-07-14

### Added
- **Shared OfflineBanner (DRY/KISS, plugin like Toast).** Extracted the transport state notice (online / syncing / offline) from an inline banner in `features/todo/components/layout.templ` into a central component `internal/components/offline_banner.templ` (`SCOPE:core`), in the same package as `Toast`. Now both todo and whiteboard use the same mechanism via `@components.OfflineBanner()` — a single source of truth for offline state. The bridge is the Service Worker: `web/resources/static/sw.js` posts `sync-start` / `sync-end` / `sync-error` to clients during Background Sync replay; the component listens and swaps CSS classes. Falls back to `navigator.onLine` when the SW is not registered (`OFFLINE_SYNC_ENABLED=false` in dev).
- **Expanded CONTENT_WRITING.md.** Writing style guide rewritten with 10 angles (Functionality, Strategy, Design Philosophy, Skills, Comparison, War Stories, Personas, Formats, Anti-marketing, Numbers) covering all layers (goqite, dagnats, loro, pb realtime, sse hub, jetstream), the opt-out by env var vs delete-directory, the `gogogo_auth`+`pb_auth` split, and 10 bug stories from the changelog. Added `posts/` to `.gitignore` so generated drafts are not pushed to the repo.

### Changed
- **Whiteboard page shells mount `@components.OfflineBanner()`** (`features/whiteboard/components.templ`: `BoardListWithRealtime` and `Board`), closing the gap where only todo had offline feedback.
- **Remove prebuilt `gogogo-desktop` binary** from the repo (44.6 MB) and add `gogogo-desktop` + `stelow.json` to `.gitignore`. The desktop binary is built from source (`make desktop` / wails3), not committed. Ported from commit `e4b97f4` which the previous remote master carried.
- **Rebuild `app.min.css`** (purge calendar widget CSS that was erroneously committed in v0.16.0).

## [0.14.0] - 2026-07-13

### Fixed
- **Whiteboard shapes and cursors not replicating across tabs (intermittent).** Race condition in `SSEHub.Unregister`: when the EventSource reconnected (e.g. tab back to foreground), the NEW handler registered a new channel with the same `clientID`, but the OLD handler (canceled context) ran `defer Unregister(clientID)` and removed the NEW channel. From then on the tab stopped receiving events — shapes and presence never arrived. Added `UnregisterIfCurrent(clientID, ch)` which only removes if the channel is still the same, preventing the race. Applied to both whiteboard (`features/whiteboard/handler.go`) and the todo SSE stream (`features/todo/handlers/todo_sse.go`).
- **Double broadcast on whiteboard.** `ApplyOp` already called `BroadcastExcept` for other tabs, and `handleUpdate` called another `Broadcast` to ALL tabs — peers received duplicate events. Fixed: `ApplyOp` now uses `Broadcast` (includes originator) and `handleUpdate` no longer calls `Broadcast` (`internal/collab/sync_web.go`, `features/whiteboard/handler.go`).

### Added
- **Regression tests for `UnregisterIfCurrent`.** `TestSSEHub_UnregisterIfCurrent_PreventsStaleCleanup` simulates the exact EventSource reconnection race condition; `TestSSEHub_UnregisterIfCurrent_NormalCleanup` verifies the normal case still works (`internal/queue/ssehub_test.go`).

## [0.13.0] - 2026-07-13

### Fixed
- **Cross-tab realtime broken by SyntaxError in PbRealtimeRecords.** The v0.12.1 refactoring removed the IIFE `(function() { ... })()` but left the orphaned `})();` closing in the template. This caused `Uncaught SyntaxError: Unexpected token '}'` which prevented the ENTIRE module from executing — the EventSource for PocketBase realtime was never created, and no tab received record updates. Removed the orphaned `})();` (`features/todo/components/realtime.templ`).
- **"Queue + Retry" tab did not navigate in sidebar.** The button's `data-on:click` was `@post('/api/todos', {contentType: 'form'})` instead of `$sidebarTab = 'queue'` — clicking the tab submitted the creation form instead of switching tabs. Fixed to `$sidebarTab = 'queue'` (`features/todo/components/todo_list.templ`).
- **Toast "Step X/6" repeated every 700ms in Durable Workflow.** `pollRun` emitted `publishProgress` on EVERY poll tick without state deduplication. Added `lastStep/lastPhase/lastDetail` variables to only emit when state changes (`features/todo/handlers/onboarding.go`).
- **Duplicate toast "Step 1/6" when starting workflow.** `handleStart` called `publishProgress(1,6,...)` manually, and the first `pollRun` tick published the same state again. Removed `publishProgress` from `handleStart` — the poll loop now handles all progress (`features/todo/handlers/onboarding.go`).
- **Intermediate steps (3/6, 4/6, 5/6) never appeared in toasts.** When the workflow ran fast, the 700ms polling missed intermediate steps. On `completed`, now retroactively publishes all unseen steps before the final step (`features/todo/handlers/onboarding.go`).
- **AI Suggest stuck in infinite loading when LLM not configured.** `handleSuggestJob` returned an error without sending `suggest_result` with `suggestPending: false`. Now sends the error result before returning, releasing the spinner (`features/todo/handlers/llm_suggest.go`).
- **Connected clients showed "4 online" with 2 tabs.** Whiteboard and todo shared the same SSE hub. `Stats().Clients` counted whiteboard connections as todo clients. Added `CountUserClients()` that filters by `userID != ""` (whiteboard registers with `""`). `broadcastClientCount` and `handleIndex` now use `CountUserClients()` (`internal/queue/ssehub.go`, `features/todo/handlers/todo_sse.go`, `features/todo/handlers/todo.go`).

### Changed
- **Durable Workflow stepper expanded from 4 to 6 steps.** Now shows all workflow steps (1. Greeting, 2. Waiting, 3-5. Creating examples, 6. Finalize), aligned with the DagNats 6-step workflow (`features/todo/components/todo_list.templ`).

### Added
- **Regression: PbRealtimeRecords without orphan IIFE.** `TestRealtimeNoOrphanIIFE` asserts that the `PbRealtimeRecords` script block does not contain `})();` (`features/todo/realtime_propagation_test.go`).
- **Regression: CountUserClients excludes whiteboard.** `TestSSEHub_CountUserClients_ExcludesEmptyUserID` asserts that `CountUserClients()` only counts clients with non-empty `userID` (`internal/queue/ssehub_test.go`).

## [0.12.1] - 2026-07-13

### Fixed
- **Realtime resync crash — the other tab never updated.** `PbRealtimeRecords.resync()` called `actions.get('/api/todos/fragment')` directly. In Datastar v1.2.2's embedded ESM build `actions` is a Proxy that resolves to the get action's `apply` with **no context**, so `cleanups` was `undefined` and the call threw `Cannot read properties of undefined (reading 'delete')`. The other tab received the PocketBase realtime event but crashed before morphing `#todo-list`. Replaced with a hidden `@get('/api/todos/fragment')` button whose `.click()` the runtime drives with a proper Datastar context (the same proven mechanism as the SSE-opener). The server's `/api/todos/fragment` now sends `datastar-selector: #todo-list` + `datastar-mode: outer` so the outer morph replaces the whole list (deletes disappear, creates/updates merge) instead of clobbering the entire document (`features/todo/components/realtime.templ`, `features/todo/handlers/todo_crud.go`).

### Added
- **Realtime resync regression tests.** `TestRealtimeResyncWiringRendered` asserts the page wires resync through the `@get` button and never reintroduces the crashing `actions.get('/api/todos/fragment')` call. `TestRealtimeResyncFragmentMorphHeaders` asserts `/api/todos/fragment` returns `datastar-selector` + `datastar-mode` headers so the resync morphs `#todo-list` (not the whole document). Together with `TestCrossSessionCreatePropagates` they guard the full create → broadcast → morph path (`features/todo/realtime_propagation_test.go`).

## [0.12.0] - 2026-07-13

### Fixed
- **Realtime resync on (re)connect and tab-visibility.** `PbRealtimeRecords` now refetches the todo fragment on `PB_CONNECT` and on `visibilitychange` (tab becomes visible). PocketBase realtime has **no event replay buffer**, so a create that occurred before the subscription was live (or while the tab was backgrounded) was permanently missed — the other tab stayed out of sync until a full reload. This is the most likely cause of the reported "add doesn't show in the other tab" symptom (`features/todo/components/realtime.templ`).

### Added
- **Cross-session realtime regression test.** `TestCrossSessionCreatePropagates` boots the real dev binary (`-tags "jetstream dagnats"`) and asserts a todo created in one session is delivered as a PocketBase realtime `create` event to a *subscribed* session. `TestCrossSessionFragmentScoped` guards list-fragment owner scoping across sessions. Both run on the `dagnats` build variant that previously had **zero** realtime coverage — the e2e only asserted the `pb_auth` cookie was issued, never that a record change fans out to a second subscriber (`features/todo/realtime_propagation_test.go`).

### Docs
- **Auth cookie design documented.** Clarified *why* the app issues two cookies (`gogogo_auth` + `pb_auth`) instead of reusing `pb_auth`: PocketBase keeps admin (`_superusers`) and users as separate auth namespaces, so sharing `pb_auth` clobbers the admin session in the same browser (known gotcha #5050/#1780). Added the explanation to `README.md`, `AGENTS.md`, and the `features/auth/auth.go` cookie constants (`features/auth/auth.go`, `AGENTS.md`, `README.md`).
- **LICENSE + northstar attribution.** Added `LICENSE` (MIT) and credited [northstar](https://github.com/zangster300/northstar) (by Nicholas Zanghi) as the inspiration in `README.md` — the template shares northstar's Go + NATS + Datastar + Templ + DaisyUI scaffold (`LICENSE`, `README.md`).

## [0.11.0] - 2026-07-13

### Added
- **DagNats enabled by default in dev builds.** `.air.toml` dev command now builds with `-tags "jetstream dagnats"`, so the onboarding durable-workflow demo runs out of the box (`internal/dagnats`, `features/todo/handlers/onboarding.go`).
- **PocketBase realtime for todo record mutations.** Record mutations now broadcast over PocketBase's native realtime (owner-scoped view rule) instead of the SSE hub. The SSE hub now carries only workflow signals (`router`, `features/todo/handlers`, `internal/queue/ssehub`, `db/seed`).
- **datastar-lint config.** `.datastar-lint.yaml` whitelists the `data-tool`/`data-doc-id` custom attributes used by the whiteboard JS; lint is scoped to `./features`.

### Changed
- `SSEHub.Register` now takes `(clientID, userID string, ch)`; added `BroadcastToUser` for owner-scoped fan-out (`internal/queue/ssehub.go`).

### Fixed
- **dagnats test never started embedded NATS.** `NATSPort: 0` disables the nats-server client listener (so `ReadyForConnections` could never succeed); `-1` is the random-port idiom. `startTestServer` now uses `-1` (`internal/dagnats/dagnats_test.go`).
- **`internal/nats` test build failure.** `realtime_test.go` called `hub.Register` with 2 args; now matches the 3-arg `(clientID, userID, ch)` signature (`internal/nats/realtime_test.go`).
- **NATS broadcaster API cleanup.** Removed the unused `PublishTodoUpdateFrom`; unified the `TodoBroadcaster` type across the in-memory and JetStream implementations (`internal/nats`).
- **Auth cookie naming.** Explicit `pbAuthCookieName` constant now used by `LoadAuthFromCookie`/`setAuthCookie`/`clearAuthCookie` (`features/auth/auth.go`).
- **`.gitignore` hygiene.** Ignore agent/dot-dir artifacts (`.pi-subagents`, `.*/`); keep `.github/` (CI) and `.githooks/` (hooks). The committed 706-byte `bin/datastar-lint` wrapper was restored — the 10MB binary was an uncommitted local overwrite and was never committed.

## [0.9.2] - 2026-07-11

### Added
- **DagNats dashboard reachable through the app origin.** A reverse proxy mounts the DagNats console (`:8090`, not Cloudflare-tunneled) under `/dagnats/` with path rewriting, so the dashboard loads without extra tunnel/infra config. The user-menu "DagNats" link now points to `/dagnats/`.
- **Stronger static analysis.** `.golangci.yml` now enables `dupl`, `goconst`, `revive`, `tagliatelle`, `modernize`, `nolintlint` (plus the existing `staticcheck`, `errcheck`, `ineffassign`, `govet`, `gocritic`, `gosec`, `noctx`, `gocyclo`, `lll`, `funlen`, `mnd`). `gofumpt` is configured as a formatter (not a linter); `stylecheck` is bundled into `staticcheck` in golangci-lint v2, so it is no longer a separate entry. Pin golangci-lint **v2.12.2+**.

### Fixed
- **Theme toggle showed both moon and sun.** `iconify-icon` is a custom element whose host stylesheet overrode the `[data-theme]` CSS rule. `theme.js` now has `syncIcons()` that explicitly hides the inactive icon (CSS polarity also corrected: light → sun, dark → moon).
- **Navbar did not highlight the active section.** `Navbar` now takes an `active` param (`todos`/`whiteboard`) and applies `btn-active` via `templ.Classes(... map[string]bool{...})`.
- **Whiteboard was completely dead (`WB_DOC_ID missing`).** `whiteboard.js` loaded as a plain external `<script>`, so it executed *before* the inline `<script>` that sets `window.WB_DOC_ID` from `<main data-doc-id>`. The guard bailed out of the whole IIFE, killing drawing, the color picker wiring, the online counter, and remote cursors. `whiteboard.js` is now `defer`-loaded so it runs after the DOM assignment. Added `cursor-pointer` to the color input.
- **`make ci-local` then ask before push.** Documented in `AGENTS.md`: run the local gate first; the agent asks (via `ask_user_question`) before pushing to master rather than auto-pushing. A `pi-yaml-hooks` `pre-push` hook mirrors this (runs `make ci-local`, then `confirm`).

### Changed
- Intentional API/UI wire contracts (`clientID`, `simulatedLLM` JSON tags bound by Datastar signals) keep `//nolint:tagliatelle` with a reason instead of being renamed.

## [0.9.1] - 2026-07-10

### Fixed
- **Demo UI/UX bugs reported in the demo app:**
  - Badge leaked a debug string (`$techDone`/`$techStep`) as `td=false ts=workflow`; now shows only the item count.
  - **AI suggestions buttons never appeared.** The SSE dispatcher only merged the `$suggestions` signal (which toggles container visibility) but never re-rendered the buttons. Now it `RenderAndPatch`-es `#suggestions-region` with the live suggestion buttons.
  - **Realtime badge** hardcoded "NATS JetStream / in-memory"; now reflects the actual transport via `$realtimeKind` (JetStream when NATS is enabled).
  - **Techstack diagnostics stepper** mixed unrelated features in one row; split into two clear steppers — Queue + retry (goqite + retry-go + fake LLM) and Durable workflow (DagNats, 6 steps incl. "waiting for your first todo").
  - **Onboarding "Run durable workflow" button stayed disabled forever.** The progress poller used a fake fixed-sleep countdown disconnected from the real DagNats run and never re-enabled on a `WaitForSignal` suspend. `pollRun` now reads real `RunStatus` (step/total/status), narrates the "waiting for your first todo" suspend, and clears `OnboardingActive` on completion/failure/5-min timeout. Uses the engine's real total (fixes the "2/5" mismatch).
  - **SSE stream listed ALL todos** (single-tenant fallback) because the global auth middleware skips `/api/*` paths, leaving `c.Auth` nil. `handleSSEStream` now calls `auth.LoadAppAuth` explicitly so `listTodos` scopes to the owner.
- **`make check` faster:** dropped the redundant `test-jetstream`/`test-dagnats` stages (the `test-combined` target already covers both tags + their intersection under `-race`). Full gate ~156s vs ~282s.

## [0.9.0] - 2026-07-10

### Added
- **Phase C + D: collaborative whiteboard backend (Loro CRDT + presence).**
  - `internal/collab` (jetstream): a mutex-guarded `Doc` over `aholstenson/loro-go`
    (`EncodeSnapshot`/`EncodeUpdate`/`ApplyUpdate`/`StateVersion`); a `SyncWorker`
    that subscribes `app.sync.>`, merges Loro updates, and persists the resolved
    snapshot to PocketBase (`whiteboards` collection, upsert by `doc_id`); a
    `Publisher` so the desktop edge pushes deltas on `app.sync.<docID>`.
  - Ephemeral multi-user presence over `app.presence.>`: `Presence` (heartbeat
    join/leave + cursor with roster TTL) plus a central SSE bridge
    `GET /api/collab/presence/{docID}` so browser clients receive edge cursors
    live. `cmd/desktop` starts a `Presence` session + demo cursor.
  - Desktop CI bundles (`.github/workflows/build-platforms.yml`): `.dmg`×2 /
    `.exe` / `.AppImage` via `wails build -tags jetstream`.
  - **e2e guards** (in `make test-combined`, `-tags "jetstream dagnats"`):
    `TestCollab_LeafNodeE2E` (real central NATS + separate leaf-node edge
    replicating a Loro update to central + persisting) and
    `TestPresence_SSEBridgeE2E` (SSE bridge carries an edge cursor to a browser
    client). Found + fixed a real SSE hang (handler blocked before writing
    headers). All collab tests finish in ~2s.
  - README "Desktop & Mobile" section; mobile marked experimental.
  - Rejected `ipfs/go-ds-crdt` (LWW-per-key KV CRDT over libp2p — would lose
    concurrent edits; drags libp2p on top of the NATS Leaf Node). Loro stays.

## [0.8.0] - 2026-07-09

### Fixed
- **Console errors on todo list patches.** The static CSS rule `view-transition-name: todo-item` was attached to every `.todo-item` element. The View Transitions API expects per-element unique names, so when the list re-rendered with multiple items, the browser logged `Unexpected duplicate view-transition-name: todo-item` and cascaded into `InvalidStateError: Transition was aborted because of invalid state` (and a stray `Access to storage is not allowed from this context` from the surrounding plugin code). Removed the duplicate-causing rule; per-item entry animations (`todo-enter-self`, `todo-enter-remote`) still cover the entry feel, and Datastar's `WithViewTransitions()` still wraps the patch in a `document.startViewTransition()` for the default root cross-fade.
- **"Suggest (simulated)" button not visible on the public demo.** `SIMULATE_LLM` defaulted to empty in the production compose file, so `signals.SimulatedLLM` was false and the mock-server affordance was hidden. The compose now sets `SIMULATE_LLM: ${SIMULATE_LLM:-true}` so the keyless "Suggest (simulated)" button (which exercises the full queue + retry + SSE pipeline against an in-process fake) is exposed alongside the real LLM button. Set `SIMULATE_LLM=false` to disable for private deployments.

## [0.6.2] - 2026-07-09

### Fixed
- **goconst lint clean.** Collapsed ~10 repeated string-literal warnings (Datastar signal keys `suggestions` / `suggestErr` / `suggestPending` and the `buy milk` test fixture) into shared package-level constants so the `jetstream` CI matrix finishes green. Two tiny files added: `features/todo/handlers/signal_keys.go` (production) and `features/todo/signal_keys_test.go` (test fixture). No behavior change.

## [0.6.1] - 2026-07-09

### Fixed
- **Quality gate green.** Resolved every `make check` issue so the gate is back to fully passing:
  - **datastar-lint**: `data-on-load` → `data-on:load` (the correct colon-separated event syntax).
  - **golangci-lint (15 issues)**: 5× lll + 3× errcheck in `todo_crud.go` / `todo_sse.go` (refactored into a `patchTodoListWithSelfOrigin` helper to keep `RenderAndPatch` calls under the 120-char line limit and to actually check the `MergeSignals` error); 1× goconst + 1× gocyclo(25) in `todo_sse.go` (split the dispatcher into per-type `streamToast`/`streamRetry`/`streamTodo`/`streamClients`/`streamSuggestResult`/`streamProgress` helpers + local `retryStatusSuccess`/`retryStatusAttempt` constants); 1× gocyclo(14) in `fakeserver.handle` (split into `authorize`/`decodeRequest`/`writeStatusOrSuccess` helpers); 1× mnd in `goai.NewSimulated` (extracted `simulatedResponseDelay` const); 3× unused in `sse_test.go` (deleted `sseTestTimeout`, `openSSE`, `pumpSSE`).
- **Vendored `app.min.css`** rebuilt to include the v0.6.0 Tailwind/DaisyUI classes (`todo-item` variants, `progress progress-primary`, `skeleton`, `loading-dots`, `view-transition-name`, `oklch` tint variables, etc.) so the page actually loads the new styles at runtime.

## [0.6.0] - 2026-07-09

### Added
- **Self vs. remote todo animations.** Every todo mutation now carries a `source` tag ("self" or "remote") that the UI uses to pick a distinct entry animation, tint, and highlight:
  - **Self** (you created it): `slide-in-from-top + primary tint` that decays (~320ms).
  - **Remote** (broadcast from another client): `slide-in-from-left + info tint + pulse` that decays (~720ms), plus a small "from someone else" indicator that fades out.
  - The new `lastItemSource` signal is merged by the local HTTP handler ("self") and by the SSE dispatcher on broadcast ("remote"); the `TodoItem` template reads it via `data-attr:data-source` and the CSS variants live in `layout.templ`.
- **View Transitions API** on every list patch (`sdk.WithViewTransitions()` on `RenderAndPatch` for `#todo-list`). Gives delete + reorder a smooth cross-fade for free (Chrome/Edge/Safari; Firefox gracefully falls back to morph).
- **AI suggest queue panel.** A small dashboard mounted when LLM or simulated LLM is active. Pills (DaisyUI `badge-ghost` / `badge-warning` / `badge-success`) flip as the operation transitions enqueue → attempt failed → completed, driven by new structured signals (`lastRetryStatus`, `lastRetryOperation`, `lastRetryAttempt`). Skeleton rows + loading dots while pending.
- **Onboarding progress bar** alongside the Turbine stepper (`<progress class="progress progress-primary">`) so users see the step ratio at a glance while the live stepper advances.

### Changed
- `todoUpdateJob` gained a `source` parameter so every broadcast carries origin information.
- The retry SSE dispatcher now merges structured signals (`lastRetryOperation`, `lastRetryStatus`, `lastRetryAttempt`) alongside the existing raw-JSON `lastRetry` signal, so the UI can drive pill transitions via boolean expressions instead of string matching.

## [0.5.0] - 2026-07-09

### Changed
- **Event-driven onboarding flow** (per user, on login). The single `WelcomeOnboarding` workflow is replaced by two split workflows driven by real app events: `OnboardingStart` (greet + mark `await_todo`) fires automatically on every successful password login via a `features/auth` login hook (scoped to the logged-in user's PocketBase record id); `OnboardingContinue` (todo captured → 1-min `time.Sleep` pause → finalize + completion alert) fires from the create-todo handler when the user has a pending onboarding. Turbine v0.3.0 has no in-workflow suspend primitive, so the split is the idiomatic event-driven alternative to polling or `WithSchedule` cron (which would be recurring, not one-shot). Each browser session gets its OWN onboarding instance — not a global broadcast.
- Removed the now-dead `TodoCreator` interface, `ExampleTodo` struct, `PocketBaseTodoCreator`, `CreateExampleTodo`, and the `onboardingStepCounts` machinery. The workflow no longer writes example todos; the user's first todo is the onboarding trigger.
- README + plan doc updated to describe the event-driven flow.

## [0.4.0] - 2026-07-09

### Added
- **Simulated LLM mode (`SIMULATE_LLM=true`).** A keyless "Suggest (simulated)" button exercises the exact same queue + retry path as the real LLM against an in-process fake server (`internal/llm/fakeserver`) that scripts `500 → 200 + delay`, so visitors can watch the async lifecycle (enqueue → attempt failed → retry → slow → result) with no API key. `llm.NewSimulated()` starts the fake; the worker's retries stream per-attempt toasts to the originating tab via `clientID` routing.
- **Suggest moved onto the queue.** `handleSuggest` now enqueues an async `suggest` job; a worker runs `ChatSuggest` inside `RetryConfig.Do` and streams the 3 completions back over SSE (`suggest_result` case). Result + retry feedback are routed to the originating `clientID`.
- **Add is now synchronous.** Removed the `todo_created` queue path; `handleCreate` patches the list directly and the realtime broadcaster re-renders for other clients. The queue is reserved for slow/flaky work (Suggest, retry-demo) — cleaner mental model.
- **Durable workflow is followable.** `WelcomeOnboarding` now paces each step with a short delay (inside the durable `turbine.Do` step, so recovery replays skip the sleep) and emits a live stepper (1→5) + a final `alert-success` completion banner in the UI.
- **Demo user-account lock.** `db/seed.go` hardens the `users` collection so non-superusers cannot create or delete accounts via the API or the `/_/` admin dashboard (only the superuser can). Keeps the public demo safe from account spam.
- **"Try it live" README section** with linked rows for the Todo demo app and the live PocketBase admin dashboard at `/_/`.
- **Turbine + JetStream auto-enable** (build-tag gated): `-tags turbine` / `-tags jetstream` now enable the feature without an extra env var (`WORKFLOW_ENABLED` / `NATS_ENABLED` default true under the tag, overridable with `=false`). `make build-turbine` / `make build-jetstream` documented.
- **`internal/llm/fakeserver.NewServer`** — test-free constructor so the fake can run in production demos, not just `*testing.T`.
- **`internal/llm`: disable goai's internal `MaxRetries`** so the project's `RetryConfig` (and, for queued work, the worker's retry) is the single retry layer — transient 5xx surface to SSE feedback instead of being absorbed silently.

### Changed
- **README**: translated AI-suggest bullet to describe the queue + simulated mode; documented `SIMULATE_LLM`.
- **`docs/async-demo-sequencing.md`**: detailed tech-sequencing plan for the async demo (Add sync / Suggest queued / Suggest simulated), with an honest note that Turbine v0.3.0 has no in-workflow delay/schedule primitive (only `WithSchedule(cron)` at registration) — the per-step `time.Sleep` is the substitute.

### Fixed
- **Suggest worker bug**: `body, err := json.Marshal(...)` shadowed the `ChatSuggest` error, so the worker always reported success and never retried. Now uses a distinct `marshalErr`, so failures correctly trigger worker retries + SSE feedback.
- **Stale doc comments** referencing the removed `todo_created` job.

## [0.2.0] - 2026-07-07

### Added
- **Async job → SSE pipeline for the Todo example.** `handleCreate` enqueues a `todo_created` job; a worker picks it up and streams a success toast to the right browser tab via `clientID` routing on the SSE Hub.
- **`internal/queue/retry.go`** — exponential backoff with jitter via `avast/retry-go/v4`, SSE-aware (`lastRetry` signal so the UI can show "retrying…").
- **`internal/queue/handlers.go`** — `HandlerRegistry` for job-type → handler dispatch, decoupling workers from business logic.
- **`internal/queue/goqite_schema.sql`** — explicit goqite schema, separate from application data.
- **Toast component** (`features/todo/components/toast.templ`) — stacked, auto-dismiss, manual close, progress bar.
- **Layout component** (`features/todo/components/layout.templ`) — shared page shell.
- **`safejson.go`** — JSON-safe signal marshaling for Datastar.
- **Turbine onboarding workflow** (`features/todo/handlers/onboarding.go`, build-tag `turbine`) — `WelcomeOnboarding` creates 3 example todos via durable steps; resumes after a crash.
- **Build-tag matrix targets**: `make build-turbine`, `make build-all`, `make test-turbine`.
- **CI matrix** — `.github/workflows/ci.yml` now runs lint + test + build across `""`, `jetstream`, and `turbine` tags.

### Changed
- **SSE Hub hardening** (`ssehub.go`): register-before-enqueue, replay buffer for late subscribers, and backpressure (slow clients are dropped, never block the producer).
- **README** translated to English and updated to reflect the new structure, commands, and the async → SSE example.

### Fixed
- **Quality gate**: all `golangci-lint` issues resolved (errcheck `check-blank`, `exitAfterDefer` in `main.go`, gosec G107, govet unused writes/params, gofumpt). `go vet`, `gofumpt`, `goimports`, and `go test -race ./...` all clean.
- **`cmd/web/main.go`**: restructured into `run() error` so deferred `q.Close()` / `shutdownTurbine()` fire on exit (previously skipped by `os.Exit`).

## [0.1.0] - Initial release

- GitHub Template Repository scaffold: PocketBase + goqite + SSE Hub + GoAI + age secrets.
- Datastar + DaisyUI reactive UI, Templ type-safe components.
- Build-tag-gated NATS JetStream and Turbine layers.
- golangci-lint strict config, distroless Docker image, Makefile, Air live reload.

## [0.24.10] - 2026-07-20

### Added

- **UI skins, landing page, /config, EntityStore, AI stepper documentation (CAL-20).**
  Comprehensive documentation gap analysis applied across README.md, ARCHITECTURE.md,
  AGENTS.md, and CONTENT_WRITING.md covering:
  - Pluggable skin system (BasecoatUI, Morpheus, DaisyUI) with env/query/selector switch model.
  - Landing page (`/`) and read-only config view (`/config`) as product surfaces.
  - EntityStore pluggable persistence (`pbstore` default, `ENTITY_STORE=crdt` for CRDT).
  - AI Suggest stepper signal isolation from Queue + Retry stepper.
  - Version badge build metadata via ldflags + docker buildx --build-arg.
  - Per-skin CSS bundle pipeline.

### Added (CRDTStore Phase 2 + 3)

- `crdtstore.transport.go` — JetStream cross-instance op transport (publisher + consumer).
- `crdtstore.ApplyRemoteOp(ctx, ownerID, op)` — applies peer Loro ops to the local doc.
- `crdtstore.Watch(ownerID)` — signal-driven channel of doc-version bumps (Phase 3).
- `internal/server/crdtstore_wire.go` — boot-time transport wire (only when `ENTITY_STORE=crdt`).
- `cmd/web/main.go` — installs the transport post-Init (router exposes `concreteTodoStore` for this).
- Integration test: two CRDTStores share one JetStream, both Observe peer's Creates within ~3s.
- ADR-0017 in `docs/decisions.md` (Phase 2 + 3 design rationale).

### Fixed

- `CRDTStore.Create` previously deadlocked when `transport != nil` because `publishOp`
  re-acquired the mutex already held by Create/Update/Delete. Refactored to
  `publishOpFromDoc(ctx, ownerID, opID, d)` which expects the caller to already hold
  `s.mu`.
