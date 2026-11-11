// Runs ONE browser suite under a wall-clock deadline.
//
// WHY THIS EXISTS. A browser suite can hang instead of failing: an in-page
// promise that never settles (a fetch the server never answers), a
// `browser.close()` that waits on a wedged Chromium, a `page.evaluate` whose
// await never resolves. Playwright's default timeouts do not cover those —
// `page.evaluate` has NO timeout at all — so the suite simply stops making
// progress. On 2026-11-11 the whiteboard suite hung exactly like that in CI
// and the step ran for 33+ minutes (and would have run for hours: the job has
// no timeout of its own), while every other run of the same suite took 20
// seconds. A hang must be REPORTED as a hang, not as a slow test.
//
// WHY A SUPERVISOR AND NOT GNU `timeout`. `timeout` is absent on macOS (where
// the local fan-out runs the same suites), and it does not tell the caller
// WHY the suite ended. This supervisor exits 124 — the code GNU timeout uses
// for "the deadline expired" — so the caller can say TIMEOUT instead of
// FAILED, and it kills the whole PROCESS GROUP on expiry: the suite's node
// process, its Chromium tree and the Go server it spawned. A suite that
// hangs must not leave a 600 MB Chromium behind (the leak scripts/lib/
// browser.mjs exists to prevent; ~/bin/reap-playwright-orphans.sh reaps the
// rest).
//
// Usage:
//   node scripts/lib/suite-supervisor.mjs <seconds> <suite.mjs> [args...]
//
// Exit: the suite's own exit code, or 124 when the deadline expired.
import { spawn } from "node:child_process";

const seconds = Number(process.argv[2]);
const suite = process.argv[3];
const args = process.argv.slice(4);

if (!Number.isFinite(seconds) || seconds <= 0 || !suite) {
  console.error("usage: suite-supervisor.mjs <seconds> <suite.mjs> [args...]");
  process.exit(2);
}

const child = spawn(process.execPath, [suite, ...args], {
  // Its own process group: the kill below reaches Chromium and the server too.
  detached: true,
  stdio: "inherit",
});

let expired = false;
const timer = setTimeout(() => {
  expired = true;
  console.error(`suite-supervisor: ${seconds}s deadline expired — killing ${suite} (process group)`);
  try {
    // Negative pid = the whole group. SIGKILL because the suite's own cleanup
    // (a finally that closes the browser) cannot run on a wedged suite.
    process.kill(-child.pid, "SIGKILL");
  } catch {
    try {
      child.kill("SIGKILL");
    } catch {}
  }
}, seconds * 1000);

child.on("exit", (code, signal) => {
  clearTimeout(timer);
  if (expired) {
    process.exit(124);
  }
  if (signal) {
    console.error(`suite-supervisor: ${suite} died on ${signal}`);
    process.exit(1);
  }
  process.exit(code ?? 1);
});

child.on("error", (err) => {
  clearTimeout(timer);
  console.error(`suite-supervisor: cannot run ${suite}: ${err.message}`);
  process.exit(1);
});
