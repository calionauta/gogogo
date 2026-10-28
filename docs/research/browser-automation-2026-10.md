# Browser automation on a shared 8 GB host (2026-10-28)

Reviewed: 2026-10-28

The question: these suites drive real Chromium because the notes caret,
whiteboard and offline behaviour only exist in a real browser. On a 4-vCPU /
8 GB arm64 box that also runs the coding-agent daemon, a production app and
several co-tenant services, which engine should carry that job — and what has
to be true for a browser suite to be *safe to run unattended*?

The answer is **keep `chrome-headless-shell`, but stop treating engine choice
as the safety mechanism**. The engine was never what broke; the lifecycle was.
Both halves are recorded here because the second is the one that costs an
outage.

## What actually happened

2026-10-28: swap reached **0 B free**. The bb daemon sat in `D`-state while
still accepting TCP, so the UI reported `OpenCode server is unavailable:
{"errorName":"ServerUnavailable"}` and the TCP proxy crash-looped (445
restarts). 37 browser processes were live; the oldest had been running **20
hours**; three renderers from one suite held **1.9 GB of swap**.

The suite had `browser.close()` on its happy path only. Its `.finally()`
killed the dev server and removed the temp dir, then returned — so any throw
leaked the whole tree. A second suite (`whiteboard-client`) had the identical
bug, because each carried its own copy of the lifecycle.

Two facts that redirect the whole analysis:

- **A dead suite is the easy case.** Playwright launches Chrome with
  `--remote-debugging-pipe`; when the suite process dies the pipe closes and
  Chrome exits by itself (verified with SIGKILL). The leak that matters is a
  suite that **hangs while still alive** — its browser stays a healthy child
  and keeps its pages in swap. `timeout 300` does not help, because it kills
  the parent, not the tree.
- **The existing memory cap did not cover the browsers.** `bb-daemon.service`
  carries `MemoryMax=4500M`, yet browsers were observed in
  `app-org.chromium.Chromium-<pid>.scope`, a **sibling** scope those limits
  never applied to. At the incident the daemon's `MemoryCurrent` was 2.58 G —
  comfortably under its ceiling — while swap was empty, because the cgroup
  satisfied its RAM limit by spilling unbounded pages (`MemorySwapMax=infinity`).
  Containment was inconsistent, so a per-unit cap was a belt with no braces.

## The options

| Option | Shape | Footprint | Fits these suites? |
|---|---|---|---|
| **`chrome-headless-shell`** (current) | Playwright's headless-only Chromium build | ~600 MB per renderer tree; ~2–4 processes per context | **Yes** — the only one of these that implements service workers, IndexedDB, layout and multi-context cookie isolation |
| **Lightpanda** | separate engine in Zig, CDP-compatible, official arm64 image | project benchmarks: 123 MB vs 2 GB peak, ~9x faster on crawl workloads | **No** — no service workers, no IndexedDB, no rendering. Excellent for scraping, DOM extraction and agent browsing; structurally unable to test an offline/PWA path |
| **happy-dom / jsdom** | JS DOM, no browser process | milliseconds, ~MB | **No** — no layout, so no caret, no scroll, no painted geometry. Useful only for pure DOM logic |
| **Remote CDP** (Cloudflare Browser Rendering, browserless) | browser runs elsewhere, suites connect over CDP | zero local RAM | **Yes**, at the cost of a network hop, a vendor dependency and per-run billing; the honest fallback if the box ever needs to run suites in parallel |
| **Full Chromium** (`chromium-1248`) | non-headless build | heavier still | No reason here — and it was the 20-hour corpse found during the incident |

Lightpanda is the interesting one and the answer is still no **for this class
of test**: the suite under investigation existed to reason about `sw.js`, and
a service worker is precisely what it does not implement. That is a property
of the engine, not a maturity gap, so it will not close with time. Where it
does fit — the scraping skills, route smoke checks, DOM assertions — it is a
~16x memory win and worth adding as a second backend rather than a replacement.

## What actually makes a suite safe

1. **Own the lifecycle in one place.** `scripts/lib/browser.mjs` exports
   `withBrowser`, which closes in a `finally`. Eleven scripts launch Chromium;
   the two that leaked each owned a private copy of the lifecycle. Duplication
   was the root cause, not the missing line. `make check-browser-cleanup`
   enforces it, and the gate self-tests that it can still fail on the original
   defect — a predicate that cannot go red is green forever.
2. **Take the decision out of the writer's hands at the OS level.** A browser
   suite should be runnable by an agent that has never read this page. So the
   host carries a reaper on a 5-minute `systemd --user` timer
   (`reap-playwright-orphans.sh`: orphan detection plus a 10-minute age
   backstop, whole-tree kill), and `browser-test-scope.sh` runs a suite inside
   a cgroup whose teardown kills every descendant — closing the leak even when
   the script is wrong.
3. **Cap swap, not only RAM.** `MemorySwapMax` is the line that would have
   prevented this. A RAM-only cap is satisfied by paging to swap, and empty
   swap is what actually kills the box.

## Revisit triggers

- A suite needs to run in parallel with another on this host → the local
  budget is ~1 suite at a time; go to remote CDP rather than raising limits.
- Any test starts asserting on painted output, screenshots or PDFs →
  Lightpanda is out for it by definition.
- Lightpanda ships service workers and IndexedDB → re-evaluate as the default
  for the whole browser-test tier, since the memory argument stops being moot.
- The host stops running co-tenant daemons → the OS guardrails become
  optional; until then they are the load-bearing part.
