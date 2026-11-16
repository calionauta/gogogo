// Playwright tracing for the browser suites — one file per context, driven by
// SMOKE_TRACE.
//
// WHY THIS EXISTS, and why it is not `if (tracePath) tracing.start()` copied
// into each suite. scripts/run-browser-suite.sh exports SMOKE_TRACE for EVERY
// attempt (the failed one included — that is the whole point) and
// scripts/ci-fanout.sh does the same for its serial retry. But only smoke.mjs
// and chat-scroll.test.mjs ever read it: notes-caret, whiteboard-client and
// sse-reopen silently ignored it, so the wrapper produced a path, announced
// "trace: …", and wrote nothing. A diagnostic that reports itself as present
// while collecting no evidence is worse than no diagnostic, because the next
// reader stops looking.
//
// The fan-out's own comment ("the retry run records a Playwright trace so the
// flake keeps its timeline") was therefore true for 2 of 5 suites. This module
// makes it true for all of them, and scripts/check-run-browser-suite.test.sh
// pins it: a suite CI runs that does not honour SMOKE_TRACE fails the contract
// test rather than being discovered later, mid-flake, with no timeline.
//
// ONE FILE PER CONTEXT. Playwright writes one file per `tracing.stop`, so
// pointing every context at the same path means the last stop overwrites the
// others. Those suites are multi-context by design (two authenticated users, a
// fresh-context read, a mobile viewport), and the interesting interaction IS
// the one between contexts — so each gets its own suffixed file instead of
// fighting for one. A single-context suite keeps the plain `$name.trace.zip`
// path the wrapper advertises.
//
// Usage:
//   import { traces } from "./lib/trace.mjs";
//   const tracer = traces();                    // reads SMOKE_TRACE once
//   await tracer.start(ctxA, "a");              // no-op when unset
//   ...
//   await tracer.stopAll();                     // in the suite's finally
//
// Never throws: a failed trace must not turn a green suite red, and must not
// mask the assertion failure that is the reason anyone wanted the trace.

// Injected by scripts/lib/suite-supervisor.mjs? No — read directly. A suite
// may also be run bare (node scripts/x.test.mjs), where SMOKE_TRACE is absent
// and every call here is inert.
const basePath = process.env.SMOKE_TRACE || "";

export function traces() {
  const started = [];
  return {
    enabled: Boolean(basePath),

    // start attaches tracing to ONE context. `label` distinguishes the
    // contexts in the output filenames. Inert when SMOKE_TRACE is unset.
    async start(context, label) {
      if (!basePath) return;
      try {
        // screenshots + snapshots: a caret/geometry bug is a VISUAL fact, and
        // the DOM snapshot alone does not show where a dot painted.
        await context.tracing.start({ screenshots: true, snapshots: true });
        started.push({ context, label });
      } catch (e) {
        // NOT silent. This module exists because three suites grew a trace path
        // they never wrote to; swallowing a failed start() here would rebuild
        // that exact trap one layer down (the wrapper prints "trace: …" and the
        // file is never created). The suite keeps running — a lost trace must
        // not decide the verdict — but the miss is said out loud, and
        // check-run-browser-suite.test.sh asserts it.
        console.error(`  ⚠ trace NOT started for context "${label || "?"}": ${e.message}`);
      }
    },

    // stopAll writes every started trace and reports the paths. Called from a
    // finally (and per-case by suites whose contexts are short-lived) so a
    // thrown suite still leaves its timeline behind — which is exactly the run
    // anyone wants the trace of.
    async stopAll() {
      for (const { context, label } of started) {
        // A label ALWAYS suffixes, even for a single context: sse-reopen stops
        // once per case, and a plain path there would have each case's stop()
        // overwrite the previous case's evidence.
        const path = label ? withLabel(basePath, label) : basePath;
        try {
          await context.tracing.stop({ path });
          console.log(`  ⏱ trace saved: ${path}`);
        } catch (e) {
          // Bounded, but NOT silent. This catch was silent in the first draft and
          // it swallowed a real failure: whiteboard's stop() threw and the suite
          // printed nothing, wrote no file, and looked traceable. That is the
          // same "reports itself present, collects nothing" trap this module
          // exists to remove, rebuilt one layer down — so the miss is announced.
          console.error(`  ⚠ trace NOT saved to ${path}: ${e.message}`);
        }
      }
      started.length = 0;
    },
  };
}

// withLabel turns `notes.trace.zip` + `b` into `notes.trace-b.zip`.
function withLabel(path, label) {
  return path.replace(/\.zip$/, `-${label}.zip`);
}
