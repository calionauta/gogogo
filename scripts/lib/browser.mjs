// Shared Playwright browser lifecycle for every script under scripts/.
//
// WHY THIS EXISTS: a browser suite that throws before its happy-path
// browser.close() abandons a Chromium tree at ~600 MB per renderer. On this
// shared host those pages land in swap; once swap is empty the kernel parks
// co-tenant daemons in D-state while they still accept TCP, which is exactly
// how the 2026-10-28 outage presented ("OpenCode server is unavailable:
// {errorName:ServerUnavailable}"). The worst leaked tree found was 20 hours
// old. Eleven scripts launch Chromium; two of them (notes-caret,
// whiteboard-client) had the identical defect because each carried its own
// copy of the lifecycle.
//
// So the lifecycle lives here once. `withBrowser` closes the browser in a
// finally, which means the leak is impossible by construction rather than by
// each author remembering. scripts/check-browser-cleanup enforces that new
// scripts use it (or an equivalent finally), and ~/bin/reap-playwright-orphans.sh
// reaps the trees of suites that hang anyway.
//
// Usage:
//   import { withBrowser } from "./lib/browser.mjs";
//   await withBrowser(async (browser) => {
//     const ctx = await browser.newContext();
//     ...
//   });
import { chromium } from "playwright";

// Drop the GPU/swiftshader paths: Playwright's headless defaults enable
// --use-angle=swiftshader-webgl, which burns CPU on a 4-vCPU box, and these
// suites assert on layout geometry and DOM state, not on painted pixels.
// `--disable-dev-shm-usage` is already a Playwright default; it is repeated
// here so the intent survives if that default ever changes.
export const SAFE_ARGS = [
  "--no-sandbox",
  "--disable-dev-shm-usage",
  "--disable-gpu",
  "--disable-software-rasterizer",
  "--disable-extensions",
  "--js-flags=--max-old-space-size=512",
];

export function launchBrowser(opts = {}) {
  return chromium.launch({
    headless: true,
    ...opts,
    args: [...SAFE_ARGS, ...(opts.args ?? [])],
  });
}

// Runs `fn` with a browser and guarantees closure, including on throw.
// The optional `finallyFn` runs after the browser is closed (server cleanup,
// temp-dir removal) and is also exception-safe.
export async function withBrowser(fn, opts = {}, finallyFn) {
  const browser = await launchBrowser(opts);
  try {
    return await fn(browser);
  } finally {
    await browser.close().catch(() => {});
    if (finallyFn) await finallyFn();
  }
}
