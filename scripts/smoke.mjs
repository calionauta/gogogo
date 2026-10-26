// SCOPE:core - Browser smoke test (headless). Builds the binary, boots it
// on an ephemeral port + data dir, creates a test user, loads the real app
// pages, and exercises offline todo create/delete through Service Worker +
// IndexedDB + reconnect replay. It FAILS on uncaught client-side JS errors,
// a stuck offline form, a mutation that was not queued, or replay that does
// not converge the UI. `make ci-local` / CI run it before deployment.
//
// No project cache is involved: a fresh browser context is used every run,
// so it loads the genuine served HTML (not a stale browser/Cloudflare copy).
//
// Requires: `npx playwright install chromium` (run by `make smoke`).

import { createHash } from "node:crypto";
import { chromium } from "playwright";
import { accessibleNameProblems, axeProblems, horizontalOverflow, panelGeometry } from "./lib/a11y.mjs";
import { spawn, spawnSync, execSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const PORT = Number(process.env.SMOKE_PORT || 8099);
const BASE = `http://127.0.0.1:${PORT}`;
const SU_EMAIL = "smoke-superuser@local.dev";
const SU_PASS = "SmokeSuperuserPass!123";
const USER_EMAIL = "smoke-user@local.dev";
const USER_PASS = "SmokeUserPass!123";

// /chat is here for the same reason as the rest: it is a page the smoke can
// load, and its live column is a Datastar patch target (#chat-messages) whose
// failure mode was invisible without a browser (nested container → doubled
// answers). Loading it catches a broken patch target and any JS error on load.
const ROUTES = ["/todo", "/chat", "/whiteboard", "/room", "/login", "/review"];

// Trim-aware gating: a trimmed checkout must neither fail on routes that
// no longer exist nor silently pass over them. SMOKE_FEATURES lists the
// features present (default: the full template surface); trim output
// (installer Warns) tells you the value to use. Unknown names fail fast:
// a typo must never read as "trimmed".
const ALL_FEATURES = ["todo", "chat", "whiteboard", "room", "login", "review"];
const FEATURES = (process.env.SMOKE_FEATURES || ALL_FEATURES.join(",")).split(",");
for (const f of FEATURES) {
  if (!ALL_FEATURES.includes(f)) {
    console.error(`❌ unknown SMOKE_FEATURES entry ${JSON.stringify(f)} (want ${ALL_FEATURES.join(",")})`);
    process.exitCode = 1;
  }
}
const hasFeature = (f) => FEATURES.includes(f);
const GATED_ROUTES = ROUTES.filter((r) => hasFeature(r.slice(1)));

const fail = (msg) => {
  console.error("❌ " + msg);
  process.exitCode = 1;
};

// Per-page signals the SERVER seeds into <html data-signals>. The no-flash
// theme bootstrap in <head> runs before Datastar reads that attribute, so it
// must merge its `theme` key in — replacing the object dropped these seeds
// and left /review's Run button disabled at load with no way to submit
// (the only handler that clears the signal lives on the unwirable form).
// Keyed by the routes this run visits (GATED_ROUTES).
const SEEDED_SIGNALS = { "/review": "review_pending" };

// Route → the submit control that must be USABLE on load. A disabled control
// here means the page is born in a stuck state: nothing the user does can
// clear a signal that only its own submit handler clears (and the form that
// would is the one holding the dead button).
const ENABLED_ON_LOAD = { "/review": "#review-composer button" };

const tmp = mkdtempSync(join(tmpdir(), "gogogo-smoke-"));
const runtimeDir = join(tmp, "runtime");
mkdirSync(runtimeDir, { recursive: true });
const runtimeEnv = {
  ...process.env,
  DATA_DIR: runtimeDir,
  DATABASE_PATH: join(runtimeDir, "app.db"),
  NATS_ENABLED: "false",
  DAGNATS_ENABLED: "false",
  // The suite creates its own user. An operator secrets file on the host
  // can set ENVIRONMENT=production, which closes public signup by default;
  // this fixture opts in explicitly so the smoke is not host-dependent.
  SIGNUP_ENABLED: "true",
  OFFLINE_SYNC_ENABLED: "true",
  // Belt-and-suspenders: router.Init already suppresses the first-run
  // installer for a non-TTY process, but an explicit flag means a future
  // change to that heuristic cannot start popping browser tabs again.
  GOGOGO_NO_BROWSER: "1",
};
const providedBin = process.env.SMOKE_BIN;
const bin = providedBin ? resolve(providedBin) : join(tmp, "web");

let server = null;
let browser = null;

async function api(method, path, { body, token } = {}) {
  const res = await fetch(BASE + path, {
    method,
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: "Bearer " + token } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  let json = null;
  try {
    json = text ? JSON.parse(text) : null;
  } catch {
    /* non-JSON */
  }
  return { status: res.status, json };
}

// signupCookie registers (idempotently) the smoke user and returns the
// gogogo_auth cookie value the owned auth mints — the same cookie a
// browser login would carry, so the page context authenticates without
// any token plumbing.
async function signupCookie(email, password) {
  const res = await fetch(BASE + "/api/auth/signup/credential", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password, name: "Smoke User" }),
  });
  if (!res.ok) return "";
  const setCookies = res.headers.getSetCookie
    ? res.headers.getSetCookie()
    : [res.headers.get("set-cookie")];
  const line = (setCookies || []).find((c) => c && c.startsWith("gogogo_auth="));
  if (!line) return "";
  return line.split(";")[0].slice("gogogo_auth=".length);
}

async function waitForHealth(timeoutMs = 30000) {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    try {
      const r = await fetch(BASE + "/health");
      if ((await r.text()).trim() === "ok") return;
    } catch {
      /* server not up yet */
    }
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error("server did not become healthy in " + timeoutMs + "ms");
}

async function pendingMutationCount(page) {
  return page.evaluate(() =>
    Promise.race([
      new Promise((resolve, reject) => {
        const open = indexedDB.open("gogogo-offline-queue");
        open.onerror = () => reject(open.error);
        open.onsuccess = () => {
          const db = open.result;
          const tx = db.transaction("pending", "readonly");
          const count = tx.objectStore("pending").count();
          count.onerror = () => reject(count.error);
          count.onsuccess = () => {
            db.close();
            resolve(count.result);
          };
        };
      }),
      new Promise((_, reject) =>
        setTimeout(() => reject(new Error("timed out reading offline queue")), 5000),
      ),
    ]),
  );
}

async function waitForServiceWorkerControl(page) {
  await page.evaluate(async () => {
    if (!("serviceWorker" in navigator)) throw new Error("service worker unsupported");
    await navigator.serviceWorker.ready;
  });
  if (!(await page.evaluate(() => Boolean(navigator.serviceWorker.controller)))) {
    await page.reload({ waitUntil: "load" });
    await page.evaluate(() => navigator.serviceWorker.ready);
  }
}

// timedWait wraps page.waitForFunction with elapsed logging: when an
// offline/sync wait flakes under parallel CI load, the log shows HOW
// LONG each convergence step took instead of a bare timeout — the next
// flake arrives as data (tight margin vs. starvation), not a mystery.
async function timedWait(page, name, fn, arg, opts) {
  const start = Date.now();
  try {
    return await page.waitForFunction(fn, arg, opts);
  } finally {
    console.log(`  ⏱ ${name}: ${Date.now() - start}ms`);
  }
}

// Verify the search box end to end: type a term that matches ONE of two
// rows, and require the list to narrow to it. This is the smoke-level proof
// that the endpoint, the layer and the `#todo-list` patch agree a unit test
// can pass with the template wired to nothing.
//
// Verify the account row's panel, the one surface whose regression is invisible
// in source: an anchored menu that opens the wrong way renders perfectly and
// lands outside the viewport, so "clicking the account does nothing" has
// shipped twice. Three assertions, each of which failed at least once: the
// panel must be INSIDE the viewport, it must sit ABOVE its trigger (it opens
// upward), and its surface must differ from the rail behind it (border or
// background — a panel on the same colour is invisible against it).
//
// `fail()` records and continues by design (the suite reports every failure in
// one run), so this verifier counts its own and prints the success line only
// when nothing broke — a check that prints "✓" after a "❌" trains people to
// read the wrong line.
async function verifyAccountMenu(page) {
  console.log("-> Exercising the account menu (rail footer)...");
  let bad = 0;
  const check = (ok, msg) => {
    if (!ok) {
      fail(msg);
      bad++;
    }
  };

  await page.goto(BASE + "/todo", { waitUntil: "load", timeout: 20000 });

  const trigger = page.locator("#app-rail .app-rail-foot .app-dropdown > summary").first();
  await trigger.waitFor({ state: "visible", timeout: 5000 });
  await trigger.click();

  const panel = page.locator("#app-rail .app-dropdown-menu").first();
  await panel.waitFor({ state: "visible", timeout: 5000 });

  const box = await panel.boundingBox();
  const row = await trigger.boundingBox();
  const viewport = page.viewportSize();
  if (!box || !row || !viewport) {
    fail("account menu: no geometry to verify (panel or trigger has no box)");
    return;
  }
  check(
    box.y >= -1 && box.y + box.height <= viewport.height + 1,
    `account menu panel is outside the viewport: y=${Math.round(box.y)} h=${Math.round(box.height)} of ${viewport.height}`,
  );
  check(
    box.y + box.height <= row.y + 2,
    "account menu panel did not open upward (it must sit above its row)",
  );

  const surface = await panel.evaluate((el) => {
    const rail = el.closest("#app-rail");
    const panelBg = getComputedStyle(el).backgroundColor;
    const railBg = rail ? getComputedStyle(rail).backgroundColor : panelBg;
    return { border: getComputedStyle(el).borderTopWidth, panelBg, railBg };
  });
  check(
    surface.border !== "0px" || surface.panelBg !== surface.railBg,
    "account menu panel has no border and no surface difference: it is invisible against the rail",
  );

  // The preference block belongs to this panel (that is what it is for), and
  // its sound setting must be an input-backed toggle, not a button wearing the
  // component's class name.
  check(
    (await page.locator("#app-pref-theme").count()) > 0,
    "account menu has no palette list (#app-pref-theme)",
  );
  check(
    (await page.locator("#app-rail .app-pref-setting input[type=checkbox]").count()) > 0,
    "account menu's sound setting is not an input-backed toggle",
  );

  await page.keyboard.press("Escape");
  await trigger.click();
  await panel.waitFor({ state: "hidden", timeout: 5000 });

  if (bad === 0) {
    console.log("  ✓ account panel opens upward, stays on screen, with a distinct surface");
  }
}

// Structural UI checks: the classes of defect a 2026 study names as dominant
// (missing names, layout disorder) and the one that started this suite (an
// anchored panel opening off-screen). They are invariants, not screenshots:
// pixel baselines cost 3.8x longer review and flag changes nobody can see.
async function verifyStructuralUi(page) {
  console.log("-> Checking structural UI (names, overflow, panel geometry)...");
  let bad = 0;
  const check = (ok, msg) => {
    if (!ok) {
      fail(msg);
      bad++;
    }
  };

  for (const route of ["/login", "/todo"]) {
    await page.goto(BASE + route, { waitUntil: "load", timeout: 20000 });
    const names = await accessibleNameProblems(page);
    check(names.length === 0, `${route}: ${names.slice(0, 3).join("; ")}`);
    const axe = await axeProblems(page);
    if (axe.ran) {
      check(axe.problems.length === 0, `${route} (axe): ${axe.problems.slice(0, 3).join("; ")}`);
      if (axe.problems.length === 0) console.log(`  \u2713 axe ${route}: no serious/critical violations`);
    } else {
      // Never silent: an off axe run is reported, because "skipped" and "clean"
      // must not look the same in a log.
      console.log(`  - axe skipped (${axe.reason})`);
    }
  }

  // Every shipped palette, not just the default one: contrast is theme-specific
  // (raw primary as text fails AA in the dark palettes and passes in light), and
  // the catalog is read from the account panel's swatches — the same list
  // internal/themes ships, so a new palette is swept without touching this file.
  await page.goto(BASE + "/todo", { waitUntil: "load", timeout: 20000 });
  const themes = await page.$$eval(".app-pref-swatch[data-theme]", (els) =>
    [...new Set(els.map((el) => el.getAttribute("data-theme")).filter(Boolean))],
  );
  for (const theme of themes) {
    await page.evaluate((value) => localStorage.setItem("themeMode", value), theme);
    await page.goto(BASE + "/todo", { waitUntil: "load", timeout: 20000 });
    const applied = await page.evaluate(() => document.documentElement.getAttribute("data-theme"));
    const axe = await axeProblems(page);
    if (!axe.ran) {
      console.log(`  - axe skipped for theme ${theme} (${axe.reason})`);
      continue;
    }
    check(
      applied === theme,
      `theme ${theme} did not apply (data-theme=${applied})`,
    );
    check(axe.problems.length === 0, `theme ${theme} (axe): ${axe.problems.slice(0, 3).join("; ")}`);
    if (axe.problems.length === 0) console.log(`  \u2713 axe theme ${theme}: clean`);
  }
  await page.evaluate(() => localStorage.setItem("themeMode", "light"));

  for (const width of [1280, 820, 500]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto(BASE + "/todo", { waitUntil: "load", timeout: 20000 });
    const overflow = await horizontalOverflow(page);
    check(overflow === null, `at ${width}px: ${overflow}`);
  }
  await page.setViewportSize({ width: 1280, height: 900 });

  // The demo panel is anchored to the right edge: it must land inside the view.
  await page.goto(BASE + "/todo", { waitUntil: "load", timeout: 20000 });
  const toggle = page.locator(".app-demo-toggle").first();
  if (await toggle.count()) {
    const problems = await panelGeometry(page, ".app-demo-toggle", "#state-panel", { expectAbove: false });
    check(problems.length === 0, `demo panel: ${problems.join("; ")}`);
    await toggle.click(); // close again
  }

  if (bad === 0) console.log("  \u2713 structural UI clean: names, overflow, panel geometry");
}

// Deliberately asserts on a MIDDLE substring: the default layer is the SQL
// filter, and an exact-match regression would show up here as "no match".
async function verifyTodoSearch(page) {
  const marker = `searchable-${Date.now()}`;
  const other = `unrelated-${Date.now()}`;

  console.log("-> Exercising todo search...");
  await page.goto(BASE + "/todo", { waitUntil: "load", timeout: 20000 });

  for (const title of [marker, other]) {
    // The composer's copy is UI, not contract: match either wording.
    const input = page.getByPlaceholder(/what needs doing|add a new todo/i);
    // The previous create's response no longer writes $newTitle (the box owns
    // it and clears itself on success), so two creates no longer race. The
    // clear is still worth doing: it makes each iteration start from a known
    // empty box rather than depending on the last clear having landed.
    await input.fill("");
    await input.fill(title);
    await input.press("Enter");
    await page.locator('[id^="todo-"]:not(#todo-list)').filter({ hasText: title })
      .waitFor({ state: "visible", timeout: 20000 });
    // Readiness = the box cleared itself after the request succeeded. This is
    // the client's own clear (the response no longer writes $newTitle, see
    // todo_list.templ), so it is also what proves the create round-tripped
    // before the next one starts.
    await page.waitForFunction(() => {
      const el = document.querySelector('input[data-bind="newTitle"]');
      return !el || el.value === "";
    }, null, { timeout: 20000 });
  }

  const box = page.getByPlaceholder("Search todos...");
  if ((await box.count()) === 0) {
    console.log("  -> search box absent (plugin trimmed), skipping");
    return;
  }
  const patch = page.waitForResponse(
    (r) => r.url().includes("/api/todos/search") && r.status() === 200,
    { timeout: 20000 },
  );
  await box.fill("searchable");
  await patch;

  // The list is patched as one region, so narrowing REMOVES the non-matching
  // rows. Waiting on the marker row would be vacuous — it is already visible
  // from the create above, so that wait resolves before the patch arrives and
  // the assertion samples a stale DOM (observed as a CI flake). The detach of
  // the non-matching row IS the transition, so it is the readiness signal.
  const rows = page.locator('[id^="todo-"]:not(#todo-list)');
  await rows.filter({ hasText: other }).waitFor({ state: "detached", timeout: 20000 });
  await rows.filter({ hasText: marker }).waitFor({ state: "visible", timeout: 20000 });
  const leaked = await rows.filter({ hasText: other }).count();
  if (leaked !== 0) {
    throw new Error(`search did not narrow the list: ${leaked} non-matching row(s) still rendered`);
  }
  console.log("  OK search narrowed the list to the matching row");
}

// verifyTodoCalendar proves the date filter's web component actually RENDERS.
// The bug that shipped was structural: the <calendar-date> element stayed in
// the markup while the module that defines it was dropped, so the popover
// opened on an unknown tag and every server-side test still passed.
//
// Cally splits the work: <calendar-date> is the form-associated container and
// <calendar-month> draws the grid inside its own shadow root, so the day cells
// are one level deeper than the element — and a raw querySelector on
// <calendar-date> finds nothing even when everything works. Playwright's CSS
// engine pierces open shadow roots, so the plain locator is the assertion.
//
// The popover is closed again without picking a day, so the filter state the
// offline checks rely on stays empty.
async function verifyTodoCalendar(page) {
  console.log("-> Exercising the Cally date filter...");
  await page.goto(BASE + "/todo", { waitUntil: "load", timeout: 20000 });
  await page.locator("#created-filter-btn").click();

  const days = page.locator('[part~="day"]');
  try {
    // Attached, not visible: the upgrade is what broke, and the popover
    // opening is a separate concern this check should not fail on.
    await days.first().waitFor({ state: "attached", timeout: 8000 });
  } catch {
    // Report what the page actually has: the next reader should not need
    // another CI run to learn which half is missing.
    const diag = await page.evaluate(() => {
      const date = document.querySelector("calendar-date");
      const month = document.querySelector("calendar-month");
      const script = document.querySelector('script[src*="cally.js"]');
      return {
        online: navigator.onLine,
        script: script ? script.getAttribute("src") : null,
        defined: !!customElements.get("calendar-date"),
        dateShadow: !!(date && date.shadowRoot),
        monthShadow: !!(month && month.shadowRoot),
      };
    });
    fail(`the date filter's calendar never rendered: ${JSON.stringify(diag)}`);
  }

  const count = await days.count();
  if (count < 28) fail(`calendar rendered ${count} day cells — Cally did not upgrade the element`);
  console.log(`  ✓ calendar upgraded (${count} day cells)`);

  // Close without selecting: the filter signal must stay empty for the
  // offline queue checks that follow.
  await page.keyboard.press("Escape");
}

async function verifyOfflineTodoQueue(page, context) {
  const title = `offline-smoke-${Date.now()}`;
  console.log("→ Exercising offline todo add + delete queue…");
  await page.goto(BASE + "/todo", { waitUntil: "load", timeout: 20000 });
  await waitForServiceWorkerControl(page);

  // A service worker restart leaves a NEW global scope behind, and Chrome can
  // keep several instances alive across a long suite (observed: three). An
  // injection that only touches serviceWorkers()[0] can therefore miss the
  // instance that actually handles the replay, which reads as a product flake
  // ("the 401 never happened") when it is really an unpatched worker. Apply
  // every injection to EVERY live instance and read aggregate counters.
  const inEveryWorker = async (fn) => {
    for (const worker of context.serviceWorkers()) {
      try {
        await worker.evaluate(fn);
      } catch {
        /* instance retired between the list and the call */
      }
    }
  };
  const workerSum = async (fn) => {
    let total = 0;
    for (const worker of context.serviceWorkers()) {
      try {
        total += await worker.evaluate(fn);
      } catch {
        /* instance retired */
      }
    }
    return total;
  };

  const banner = page.locator("#offline-banner");
  if ((await banner.getAttribute("data-offline-sync")) !== "true") {
    throw new Error("offline banner rendered with offline sync disabled");
  }

  // CAL-34 regression guard: the SW postMessage bridge in
  // internal/components/offline_banner.templ dispatches the event as
  // `gogogo:queued` (Datastar event-namespace separator is the colon).
  // A listener spelled `gogogo__queued` (double underscore) once left
  // $loading stuck true after an offline add, blocking every follow-up
  // submit. Catch that here by asserting the rendered attribute uses the
  // colon separator before the window modifier.
  const queuedListener = await page.evaluate(() => {
    for (const el of document.querySelectorAll("*")) {
      for (const attr of el.attributes) {
        if (attr.name.startsWith("data-on:gogogo") && attr.name.includes("queued")) {
          return attr.name;
        }
      }
    }
    return null;
  });
  if (queuedListener !== "data-on:gogogo:queued__window") {
    throw new Error(
      `the page listens for "${queuedListener ?? "<none>"}"; ` +
        'expected "data-on:gogogo:queued__window" (Datastar event namespace is ":", not "__")',
    );
  }

  try {
    await context.setOffline(true);
    await timedWait(page, "offline-banner-queued", () =>
      document.querySelector("#offline-banner-text")?.textContent?.includes("queued"),
    );

    const titleInput = page.getByPlaceholder(/what needs doing|add a new todo/i);
    const addButton = page.getByRole("button", { name: "Add" });
    await titleInput.fill(title);
    // Submit via Enter on the input: clicking the Add button does not always
    // dispatch a native form-submit event in headless Chromium, which is fine
    // for users (they click too) but breaks the harness.
    await titleInput.press("Enter");
    await timedWait(page, "offline-input-cleared", () =>
      document.querySelector('input[name="title"]')?.value === "",
    );

    // A second title must re-enable Add. This catches the CAL-34 bug
    // where the first offline request left $loading=true forever.
    await titleInput.fill(title + "-probe");
    if (await addButton.isDisabled()) {
      throw new Error("Add stayed disabled after an offline mutation queued");
    }
    await titleInput.fill("");

    if ((await pendingMutationCount(page)) !== 1) {
      throw new Error("offline create was not persisted in IndexedDB");
    }

    await context.setOffline(false);
    // Rows carry id="todo-<id>" and the container is #todo-list (which
    // itself starts with "todo-"). Scope by id prefix, exclude the list
    // container, and narrow by text for the exact row match.
    const row = page
      .locator('[id^="todo-"]:not(#todo-list)')
      .filter({ hasText: title });
    await row.waitFor({ state: "visible", timeout: 20000 });
    // Wait for replay to drain the IndexedDB queue. The SW's optimistic row
    // is replaced by the server row on replay, but the pending-count drain is
    // the single source of truth for "the server accepted the replay" — the
    // optimistic swap is a DOM detail.
    await timedWait(page, "offline-queue-drain", async () => {
      const open = indexedDB.open("gogogo-offline-queue");
      const db = await new Promise((resolve, reject) => {
        open.onerror = () => reject(open.error);
        open.onsuccess = () => resolve(open.result);
      });
      const tx = db.transaction("pending", "readonly");
      const request = tx.objectStore("pending").count();
      const count = await new Promise((resolve, reject) => {
        request.onerror = () => reject(request.error);
        request.onsuccess = () => resolve(request.result);
      });
      db.close();
      return count === 0;
    }, null, { timeout: 15000 });
    // Wait for the SW's optimistic row to be replaced by the server row: the
    // delete test below clicks the row's delete button, which the disabled
    // optimistic button would block.
    {
      await timedWait(
        page,
        "daisyui-optimistic-swap",
        (t) => {
          const els = document.querySelectorAll('[id^="todo-"]:not(#todo-list)');
          for (const el of els) {
            if (el.textContent?.includes(t) && el.getAttribute("data-pending") !== "true") {
              return true;
            }
          }
          return false;
        },
        title,
        { timeout: 20000 },
      );
    }
    await timedWait(page, "offline-queue-drain-2", async () => {
      const open = indexedDB.open("gogogo-offline-queue");
      const db = await new Promise((resolve, reject) => {
        open.onerror = () => reject(open.error);
        open.onsuccess = () => resolve(open.result);
      });
      const tx = db.transaction("pending", "readonly");
      const request = tx.objectStore("pending").count();
      const count = await new Promise((resolve, reject) => {
        request.onerror = () => reject(request.error);
        request.onsuccess = () => resolve(request.result);
      });
      db.close();
      return count === 0;
    }, null, { timeout: 10000 });

    // Queue a delete offline too, then prove it replays and the UI converges.
    await context.setOffline(true);
    // Scope the click to the row (the confirm-modal "Delete" button lives
    // inside the dialog, so an unscoped locator would be ambiguous) and accept
    // either delete-button marker.
    await row.locator(
      'button[aria-label*="Delete"], button[title*="Delete"]',
    ).first().click();
    const dialog = page.getByRole("dialog");
    await dialog.getByRole("button", { name: "Delete", exact: true }).click();
    await dialog.waitFor({ state: "hidden" });
    if ((await pendingMutationCount(page)) !== 1) {
      throw new Error("offline delete was not persisted in IndexedDB");
    }

    await context.setOffline(false);
    await row.waitFor({ state: "detached", timeout: 20000 });
    // The detach is OPTIMISTIC (todo-offline.js removed the row); the queue
    // still has to drain before the next phase starts, or its pending count
    // includes this delete and the phases interfere.
    {
      const deadline = Date.now() + 20000;
      let pending = await pendingMutationCount(page);
      while (pending !== 0 && Date.now() < deadline) {
        await new Promise((r) => setTimeout(r, 250));
        pending = await pendingMutationCount(page);
      }
      if (pending !== 0) throw new Error("the offline delete never drained after reconnect");
    }
    console.log("  ✓ offline add resets UI, queues, replays; offline delete replays");

    // --- a transient replay failure must be retried, not silently dropped ---
    // The `online` event fires when the OS reports a link, not when packets
    // move, so the first replay fetch can fail a moment before the network is
    // usable. The queue used to give up on that first failure and leave the
    // mutation in IndexedDB forever (optimistic row stuck, other tabs never
    // told). Inject two failed commits in the Service Worker and require the
    // queue to drain WITHOUT a reload. Red-proof: revert replayOne's backoff
    // and the pending count stays 1.
    const sw = context.serviceWorkers()[0];
    if (!sw) throw new Error("no service worker to inject the transient replay failure into");
    await inEveryWorker(() => {
      const orig = self.fetch;
      self.__failReplays = 2;
      self.fetch = function (input, init) {
        const url = typeof input === "string" ? input : input.url;
        const method = (init && init.method) || (input && input.method) || "GET";
        if (self.__failReplays > 0 && method === "POST" && url.indexOf("/api/todos") !== -1) {
          self.__failReplays--;
          return Promise.reject(new TypeError("Failed to fetch (injected transient)"));
        }
        return orig.apply(self, arguments);
      };
    });

    const retryTitle = `offline-retry-${Date.now()}`;
    await context.setOffline(true);
    await timedWait(page, "offline-retry-banner", () =>
      document.querySelector("#offline-banner-text")?.textContent?.includes("queued"),
    );
    await titleInput.fill(retryTitle);
    await titleInput.press("Enter");
    {
      const deadline = Date.now() + 10000;
      let pending = await pendingMutationCount(page);
      while (pending !== 1 && Date.now() < deadline) {
        await new Promise((r) => setTimeout(r, 200));
        pending = await pendingMutationCount(page);
      }
      if (pending !== 1) {
        const swCount = await sw.evaluate(() => queueCount());
        const items = await sw.evaluate(() => loadAllPending().then((xs) =>
          xs.map((x) => ({ url: x.url.slice(0, 70), owner: (x.owner || "").slice(0, 6) }))));
        throw new Error(`the offline retry probe was not queued in IndexedDB (pending=${pending} swCount=${swCount} items=${JSON.stringify(items)})`);
      }
    }
    await context.setOffline(false);
    {
      const deadline = Date.now() + 20000;
      let pending = await pendingMutationCount(page);
      while (pending !== 0 && Date.now() < deadline) {
        await new Promise((r) => setTimeout(r, 250));
        pending = await pendingMutationCount(page);
      }
      if (pending !== 0) {
        throw new Error("a transient replay failure left the mutation queued forever (no retry)");
      }
    }
    await page.waitForFunction(
      (t) => {
        const els = document.querySelectorAll('[id^="todo-"]:not(#todo-list)');
        for (const el of els) {
          if (el.textContent?.includes(t) && el.getAttribute("data-pending") !== "true") return true;
        }
        return false;
      },
      retryTitle,
      { timeout: 20000 },
    );
    console.log("  ✓ a failed replay is retried until the queue drains (no reload)");

    // --- an expired session pauses the queue; it must not retry forever ---
    // A 401 cannot be fixed by replaying: the SW must keep the mutation,
    // stop BOTH retry layers and ask for the one action that helps (sign in).
    // Before this policy the queue re-registered Background Sync on every
    // auth failure, so a signed-out device replayed in a loop forever.
    await inEveryWorker(() => {
      const prev = self.fetch;
      // Sticky: a real expired session refuses EVERY replay, not just the
      // first one. A one-shot mock let the second trigger of the same
      // reconnect (page kick + an already-registered Background Sync event)
      // succeed, which drained the queue and read as a retry-loop failure.
      self.__force401 = true;
      self.__force401Hits = 0;
      self.fetch = function (input, init) {
        const hdrs = new Headers((init && init.headers) || undefined);
        if (self.__force401 && hdrs.get("X-Offline-Replay") === "1") {
          self.__force401Hits++;
          return Promise.resolve(new Response("sign-in required", { status: 401 }));
        }
        return prev.apply(self, arguments);
      };
    });

    const authTitle = `offline-auth-${Date.now()}`;
    await page.evaluate(() => {
      window.__swmsgs = [];
      navigator.serviceWorker.addEventListener("message", (e) =>
        window.__swmsgs.push(e.data && e.data.type));
    });
    await context.setOffline(true);
    await timedWait(page, "offline-auth-banner", () =>
      document.querySelector("#offline-banner-text")?.textContent?.includes("queued"),
    );
    await titleInput.fill(authTitle);
    await titleInput.press("Enter");
    {
      const deadline = Date.now() + 10000;
      let pending = await pendingMutationCount(page);
      while (pending !== 1 && Date.now() < deadline) {
        await new Promise((r) => setTimeout(r, 200));
        pending = await pendingMutationCount(page);
      }
      if (pending !== 1) throw new Error("the auth-pause probe was not queued");
    }
    await context.setOffline(false);
    try {
      await timedWait(page, "offline-auth-pause", () =>
        document.querySelector("#offline-banner-text")?.textContent?.includes("sign in to sync"),
      null, { timeout: 20000 });
    } catch (err) {
      // The banner is the only surface of this state; when it never arrives,
      // the worker messages say which half went missing (no replay vs no
      // sync-auth vs a later sync-end that overwrote it).
      const diag = await page.evaluate(() => ({
        banner: document.querySelector("#offline-banner-text")?.textContent,
        class: document.querySelector("#offline-banner")?.className,
        online: navigator.onLine,
        controller: Boolean(navigator.serviceWorker.controller),
        messages: window.__swmsgs,
      }));
      console.error("auth-pause diagnostic:", JSON.stringify({
        diag,
        workers: context.serviceWorkers().length,
        hits: await workerSum(() => self.__force401Hits || 0),
        pending: await pendingMutationCount(page),
      }));
      throw err;
    }
    // Hold: a paused queue neither drains nor drops its mutation.
    await new Promise((r) => setTimeout(r, 2000));
    if ((await pendingMutationCount(page)) !== 1) {
      throw new Error("a 401 must keep the mutation queued while it asks for sign-in");
    }
    // ...and it must not replay again on its own: the triggers that raced the
    // first pass are spent, so the 401 counter has to stay flat. A queue that
    // re-arms Background Sync on an auth failure grows it every few seconds.
    {
      const before = await workerSum(() => self.__force401Hits || 0);
      await new Promise((r) => setTimeout(r, 2000));
      const after = await workerSum(() => self.__force401Hits || 0);
      if (after !== before) {
        throw new Error(`the paused queue kept replaying (${before} -> ${after} 401s)`);
      }
    }
    // A valid session again = the next page load resumes the drain.
    await inEveryWorker(() => { self.__force401 = false; });
    await page.reload({ waitUntil: "load" });
    await page.evaluate(() => navigator.serviceWorker.ready);
    {
      const deadline = Date.now() + 20000;
      let pending = await pendingMutationCount(page);
      while (pending !== 0 && Date.now() < deadline) {
        await new Promise((r) => setTimeout(r, 250));
        pending = await pendingMutationCount(page);
      }
      if (pending !== 0) throw new Error("the paused queue never resumed after a fresh page load");
    }
    await page.waitForFunction(
      (t) => {
        const els = document.querySelectorAll('[id^="todo-"]:not(#todo-list)');
        for (const el of els) {
          if (el.textContent?.includes(t) && el.getAttribute("data-pending") !== "true") return true;
        }
        return false;
      },
      authTitle,
      { timeout: 20000 },
    );
    console.log("  ✓ a 401 pauses the queue (kept, no retry loop) and resumes on a fresh session");

    // --- entries are owner-scoped and expire ---
    // One entry claims another account, one is older than the TTL. Neither
    // may ever reach the server: a queued intent from a different person must
    // not be written into this account, and a week-old intent must not be
    // replayed against changed rows. Both are dropped with a visible notice.
    const owner = await sw.evaluate(() => queueOwner());
    const wantOwner = createHash("sha256").update(USER_EMAIL.toLowerCase()).digest("hex");
    if (owner !== wantOwner) {
      throw new Error(`queue owner = ${JSON.stringify(owner)}, want the SHA-256 of the signed-in email`);
    }

    const foreignTitle = `offline-foreign-${Date.now()}`;
    const staleTitle = `offline-stale-${Date.now()}`;
    await context.setOffline(true);
    await timedWait(page, "offline-scope-banner", () =>
      document.querySelector("#offline-banner-text")?.textContent?.includes("queued"),
    );
    await titleInput.fill(foreignTitle);
    await titleInput.press("Enter");
    await titleInput.fill(staleTitle);
    await titleInput.press("Enter");
    {
      const deadline = Date.now() + 10000;
      let pending = await pendingMutationCount(page);
      while (pending !== 2 && Date.now() < deadline) {
        await new Promise((r) => setTimeout(r, 200));
        pending = await pendingMutationCount(page);
      }
      if (pending !== 2) throw new Error(`expected 2 queued probes, got ${pending}`);
    }
    await sw.evaluate(async () => {
      const items = await loadAllPending();
      const db = await idbOpen();
      await new Promise((resolve, reject) => {
        const tx = db.transaction("pending", "readwrite");
        const store = tx.objectStore("pending");
        store.put(Object.assign({}, items[0], { owner: "another-account" }));
        store.put(Object.assign({}, items[1], { queuedAt: Date.now() - 8 * 24 * 60 * 60 * 1000 }));
        tx.oncomplete = resolve;
        tx.onerror = () => reject(tx.error);
      });
    });
    await context.setOffline(false);
    {
      const deadline = Date.now() + 20000;
      let pending = await pendingMutationCount(page);
      while (pending !== 0 && Date.now() < deadline) {
        await new Promise((r) => setTimeout(r, 250));
        pending = await pendingMutationCount(page);
      }
      if (pending !== 0) throw new Error("foreign/stale queue entries were not dropped");
    }
    {
      const toasts = await page.evaluate(() => document.querySelector("#toast-container")?.textContent || "");
      for (const want of ["another account", "older than a week"]) {
        if (!toasts.includes(want)) {
          throw new Error(`no user-visible notice about a dropped change (${want}); saw: ${JSON.stringify(toasts)}`);
        }
      }
    }
    await page.waitForFunction(
      (titles) => {
        const text = document.querySelector("#todo-list")?.textContent || "";
        return !titles.some((t) => text.includes(t));
      },
      [foreignTitle, staleTitle],
      { timeout: 20000 },
    );
    console.log("  ✓ foreign-account and stale queue entries are dropped with a visible notice");

    // --- a failed queue write must FAIL the action, not fake success ---
    // The worker used to answer 204 even when the IndexedDB write rejected.
    // The page then heard neither queued-item nor sync-error, so the form
    // stayed in its loading state forever and the todo vanished: no row, no
    // error, no way to submit again. A 503 makes the action fail, which keeps
    // the title (with a visible alert) and releases the button.
    const failTitle = `offline-storefail-${Date.now()}`;
    await inEveryWorker(() => {
      self.__origAddPending = addPending;
      self.addPending = () => Promise.reject(new Error("smoke: simulated IndexedDB failure"));
    });
    try {
      await context.setOffline(true);
      await timedWait(page, "offline-storefail-banner", () =>
        document.querySelector("#offline-banner-text")?.textContent?.includes("queued"),
      );
      await titleInput.fill(failTitle);
      await titleInput.press("Enter");
      await timedWait(
        page,
        "offline-storefail-reset",
        (t) => {
          const input = document.querySelector('input[name="title"]');
          const add = document.querySelector('form#todo-create button[type="submit"]');
          return !!input && input.value === t && !!add && !add.disabled;
        },
        failTitle,
        { timeout: 10000 },
      );
      if (!(await page.locator("#todo-save-error").isVisible())) {
        throw new Error("a failed queue write left no visible error");
      }
      const optimistic = await page.evaluate((t) =>
        [...document.querySelectorAll('[id^="todo-"]:not(#todo-list)')]
          .some((el) => el.textContent?.includes(t)), failTitle);
      if (optimistic) throw new Error("a failed queue write still rendered an optimistic row");
      if ((await pendingMutationCount(page)) !== 0) {
        throw new Error("a failed queue write still left an entry in IndexedDB");
      }
      const toasts = await page.evaluate(() => document.querySelector("#toast-container")?.textContent || "");
      if (!toasts.includes("couldn't be saved")) {
        throw new Error(`a failed queue write was not announced; saw: ${JSON.stringify(toasts)}`);
      }
      console.log("  ✓ a failed queue write fails the action, keeps the title, and says so");
    } finally {
      await inEveryWorker(() => { self.addPending = self.__origAddPending; });
      await context.setOffline(false);
    }

    // --- a lost SW message must not hold the form ---
    // The optimistic row is feature-owned (todo-offline.js), so the page
    // cannot draw it without queued-item — but the PENDING state belongs to
    // the request, not to the worker: once the action settles (204 queued),
    // the button has to release even if no message ever arrives. Before the
    // form cleared its own state, a dropped message (or a page that never
    // loaded the bridge) left the first offline add spinning forever with
    // the title already cleared.
    const lostTitle = `offline-nomsg-${Date.now()}`;
    await inEveryWorker(() => {
      self.__origNotify = notifyClients;
      self.notifyClients = () => Promise.resolve();
    });
    try {
      await context.setOffline(true);
      await timedWait(page, "offline-nomsg-banner", () =>
        document.querySelector("#offline-banner-text")?.textContent?.includes("queued"),
      );
      await titleInput.fill(lostTitle);
      await titleInput.press("Enter");
      await timedWait(
        page,
        "offline-nomsg-release",
        () => {
          // The title is empty after a queued add, so the Add button is
          // disabled for the RIGHT reason (nothing to submit). The pending
          // state is the spinner's data-show binding.
          const input = document.querySelector('input[name="title"]');
          const spinner = document.querySelector('form#todo-create .loading-spinner');
          return !!input && input.value === "" && !!spinner &&
            getComputedStyle(spinner).display === "none";
        },
        null,
        { timeout: 10000 },
      );
      if ((await pendingMutationCount(page)) !== 1) {
        throw new Error("a silent queue write did not persist the mutation");
      }
      if (await page.locator("#todo-save-error").isVisible()) {
        throw new Error("a successfully queued mutation reported a save failure");
      }
      console.log("  ✓ a queued mutation releases the form even when no SW message arrives");
    } finally {
      await inEveryWorker(() => { self.notifyClients = self.__origNotify; });
      await context.setOffline(false);
    }
    // Durability never depended on the message: the replay still lands.
    await page.waitForFunction(
      (t) => {
        const els = document.querySelectorAll('[id^="todo-"]:not(#todo-list)');
        for (const el of els) {
          if (el.textContent?.includes(t) && el.getAttribute("data-pending") !== "true") return true;
        }
        return false;
      },
      lostTitle,
      { timeout: 20000 },
    );
    {
      const deadline = Date.now() + 15000;
      let pending = await pendingMutationCount(page);
      while (pending !== 0 && Date.now() < deadline) {
        await new Promise((r) => setTimeout(r, 250));
        pending = await pendingMutationCount(page);
      }
      if (pending !== 0) throw new Error("the silent queue write never replayed");
    }
  } finally {
    await context.setOffline(false);
  }
}

async function verifyOfflineUx(page, context) {
  // Bundles the CAL-9 offline-UX contract under one harness run:
  //   1. the header presence pill (.online-pill) greys out (is-offline) when the
  //      network drops — this is the bug the v0.23.5 fix closed (the header used
  //      to render a live-green dot via Tailwind bg-success/animate-ping instead
  //      of the shared .online-pill that reflectPresence() drives off navigator.onLine);
  //   2. navigating to a VISITED page while offline is served from the SW PAGE_CACHE
  //      (network-first with cache fallback), not ERR_INTERNET_DISCONNECTED;
  //   3. the auth navbar logout posts {type:'clear-pages'} so the page cache is
  //      purged on sign-out (shared-device safety).
  const pill = page.locator(".online-pill");
  console.log("→ Exercising offline-UX (presence pill + SW nav cache + clear-pages)…");

  // Cache /todo under the SW BEFORE going offline. The very first navigation
  // may predate SW control (or clients.claim() may take over the page without
  // a fresh navigation), so we explicitly (re)load once control is established
  // — that navigation is what networkFirstPage intercepts and caches. Without
  // this, caches.match('/todo') is empty and the offline navigation test below
  // has nothing to serve from the SW cache.
  await page.goto(BASE + "/todo", { waitUntil: "load", timeout: 20000 });
  await waitForServiceWorkerControl(page);
  await page.reload({ waitUntil: "load" });
  await waitForServiceWorkerControl(page);

  // 1) online => pill must NOT be greyed out. Allow a brief settle in case
  // navigator.onLine reported false transiently during SW install/claim.
  await new Promise((r) => setTimeout(r, 1500));
  const pills = await page.evaluate(() => {
    const els = [...document.querySelectorAll(".online-pill")];
    return els.map((e) => ({ cls: e.className, text: e.textContent.trim().slice(0, 30), online: navigator.onLine }));
  });
  console.log("ALL_PILLS:", JSON.stringify(pills));
  const pillState = await pill.first().evaluate((el) => ({
    online: navigator.onLine,
    off: el.classList.contains("is-offline"),
    cls: el.className,
  }));
  console.log("PILL_STATE:", JSON.stringify(pillState));
  if (pillState.off) {
    throw new Error("presence pill shows is-offline while online (should be live)");
  }

  // The page must be cached by the SW for offline navigation to have something to serve.
  // networkFirstPage caches asynchronously after the response, so poll until it lands.
  let cached = false;
  for (let i = 0; i < 16; i++) {
    cached = await page.evaluate(
      (u) => caches.match(u).then((r) => !!r),
      BASE + "/todo",
    );
    if (cached) break;
    await new Promise((r) => setTimeout(r, 500));
  }
  if (!cached) {
    const dump = await page.evaluate(async () => {
      const out = {};
      for (const n of await caches.keys()) {
        const c = await caches.open(n);
        out[n] = (await c.keys()).map((r) => r.url + " [" + r.method + "/" + r.mode + "]");
      }
      return out;
    });
    console.log("CACHE DUMP:", JSON.stringify(dump));
    throw new Error("todo page was not cached by the service worker (PAGE_CACHE)");
  }

  try {
    // 2) offline => pill greys out (reproduces the reported bug when broken).
    await context.setOffline(true);
    await page.waitForFunction(
      () => document.querySelector(".online-pill")?.classList.contains("is-offline"),
      null,
      { timeout: 8000 },
    );

    // 3) offline navigation to a visited page is served from the SW cache.
    await page.goto(BASE + "/todo", { waitUntil: "load", timeout: 20000 });
    await page.getByPlaceholder(/what needs doing|add a new todo/i).waitFor({ state: "visible", timeout: 15000 });
    console.log("  ✓ offline navigation served the cached todo page");

    // 4) The logout form delegates to the transport helper, which asks the
    // queue size, confirms a loss, then posts clear-user-data (page cache +
    // queued mutations) before leaving for /login. Native submit remains the
    // fallback when the helper is absent (script blocked).
    const logoutForm = page.locator('form[action="/logout"]');
    const onSubmit = await logoutForm.getAttribute("data-on:submit");
    if (!onSubmit || !onSubmit.includes("__gogogoLogout")) {
      throw new Error("logout form does not delegate to __gogogoLogout");
    }
    if (!(await page.evaluate(() => typeof window.__gogogoLogout === "function"))) {
      throw new Error("offline-banner.js did not define __gogogoLogout");
    }
  } finally {
    await context.setOffline(false);
  }

  // 5) logout (online) => SW purges the cached page (and the queue).
  // Drives the mechanism directly — POST /logout for the session plus the
  // clear-user-data postMessage the helper sends — instead of clicking
  // through the login UI. The login page is trimmable while the endpoint and
  // the SW protocol are core; this keeps the purge covered either way.
  console.log("→ Logging out to verify clear-user-data purge…");
  await page.evaluate(async (base) => {
    await fetch(base + "/logout", { method: "POST" });
    const r = await navigator.serviceWorker.ready;
    if (r.active) r.active.postMessage({ type: "clear-user-data" });
  }, BASE);
  await page.waitForFunction(
    (u) => caches.match(u).then((r) => !r),
    BASE + "/todo",
    { timeout: 8000 },
  );
  console.log("  ✓ logout purged the cached page and the queued mutations");
}

let tracePath = "";
try {
  if (providedBin) {
    console.log(`→ Using prebuilt binary ${bin}…`);
  } else {
    console.log("→ Building binary (./cmd/web)…");
    // 240s: the unified binary embeds Turso+NATS+DagNats+Loro etc., and
    // `go build ./cmd/web` can exceed 180s on a cold cache, tripping a false
    // failure right after a clean ci-local pass. Prefer using SMOKE_BIN to
    // reuse the binary ci-local already built.
    execSync(`go build -o ${JSON.stringify(bin)} ./cmd/web`, {
      stdio: "inherit",
      timeout: 240000,
    });
  }

  console.log(`→ Starting server on ${BASE} (data dir ${runtimeDir})…`);
  // Fail fast on a busy port: an orphaned server from an interrupted run
  // holds 8099 and the fresh one exits(1), which surfaces later as a
  // confusing "signup failed". Never kill implicitly (it could be a dev
  // server) — say exactly what to do instead. Any TCP answer means busy,
  // regardless of status; only connection-refused means free.
  try {
    await fetch(`${BASE}/health`);
    throw new Error(
      `port ${PORT} already serves (orphan server?) — free it, then re-run ` +
        `(lsof -ti :${PORT} to find it)`,
    );
  } catch (e) {
    if (String(e.message || e).includes("already serves")) throw e;
    // Unreachable: the port is free, boot normally.
  }
  // The flat binary has no CLI subcommands: listen address + data dir
  // ride env vars (same as production), and the owned auth issues the
  // session cookie at signup — no superuser/collection API anymore.
  server = spawn(bin, [], {
    stdio: "ignore",
    env: {
      ...runtimeEnv,
      HOST: "127.0.0.1",
      PORT: String(PORT),
      TURSO_DB_PATH: join(runtimeDir, "app.db"),
      GOAKT_ENABLED: process.env.SMOKE_ROOM === "0" ? "false" : "true",
    },
  });
  server.on("exit", (code) => {
    if (code && code !== 0) console.error(`server exited with code ${code}`);
  });

  await waitForHealth();

  console.log(`→ Signing up the smoke user (${USER_EMAIL})…`);
  let userToken = "";
  for (let i = 0; i < 10 && !userToken; i++) {
    userToken = await signupCookie(USER_EMAIL, USER_PASS);
    if (!userToken) await new Promise((res) => setTimeout(res, 500));
  }
  if (!userToken) throw new Error("smoke signup failed: no session cookie");

  console.log(`→ Launching headless Chromium; testing routes: ${GATED_ROUTES.join(", ")}`);
  const skipped = ROUTES.filter((r) => !hasFeature(r.slice(1)));
  for (const s of skipped) console.log(`  → ${s} trimmed, skipping`);
  browser = await chromium.launch({ headless: true, args: ["--no-sandbox"] });
  const context = await browser.newContext();
  await context.addCookies([
    { name: "gogogo_auth", value: userToken, url: BASE + "/" },
  ]);

  const pageErrors = [];
  const consoleErrors = [];
  const page = await context.newPage();
  // Trace-on-retry (see scripts/ci-fanout.sh): the fan-out sets SMOKE_TRACE
  // only for the serial retry run, so the passing run stays light and a
  // flake arrives with a full timeline (actions, DOM, network) attached.
  // Playwright's own guidance is the same shape: traces on retry, never on.
  tracePath = process.env.SMOKE_TRACE || "";
  if (tracePath) {
    await context.tracing.start({ screenshots: true, snapshots: true });
  }
  page.on("pageerror", (err) =>
    pageErrors.push({ msg: String(err), stack: err.stack || "", url: page.url() }),
  );
  page.on("console", (msg) => {
    if (msg.type() === "error") consoleErrors.push(msg.text());
  });

  for (const route of GATED_ROUTES) {
    pageErrors.length = 0;
    consoleErrors.length = 0;
    try {
      await page.goto(BASE + route, { waitUntil: "load", timeout: 20000 });
    } catch (e) {
      console.error(`  ! navigation to ${route} failed: ${e.message}`);
    }
    if (route === "/todo") {
      await page.content().then((c) => writeFileSync("/tmp/served_todos.html", c));
    }
    // Give inline scripts + SSE a moment to execute before reading the error
    // buffers. This is an ABSENCE assertion ("no pageerror fired"), so there is
    // no event to await — the window IS the check, and it is labelled as such.
    // A shorter window would let a late error escape into the next route's
    // buffer, which is the failure this loop exists to prevent.
    await page.waitForTimeout(1000); // settle window: absence check, no event to await
    if (pageErrors.length > 0) {
      fail(`uncaught JS error on ${route}: ${pageErrors.map((e) => e.msg).join(" | ")}`);
      for (const e of pageErrors) {
        console.error(`      at ${e.url}`);
        if (e.stack) console.error(e.stack.split("\n").slice(0, 4).join("\n"));
      }
    }
    const tag = pageErrors.length ? "❌" : "✓";
    console.log(`  ${tag} ${route} (console errors: ${consoleErrors.length})`);
    for (const ce of consoleErrors) console.log(`      console.error: ${ce}`);

    const seeded = SEEDED_SIGNALS[route];
    if (seeded) {
      const live = await page.evaluate((name) => {
        let signals = {};
        try {
          signals = JSON.parse(document.documentElement.dataset.signals || "{}");
        } catch (e) {
          return { parsed: false, has: false };
        }
        return { parsed: true, has: Object.prototype.hasOwnProperty.call(signals, name), value: signals[name] };
      }, seeded);
      if (!live.parsed) {
        fail(`${route}: <html data-signals> is not parseable JSON after load`);
      } else if (!live.has) {
        fail(
          `${route}: server-seeded $${seeded} vanished from <html data-signals> (the theme bootstrap must merge, not replace) — the page's controls start in the wrong state`,
        );
      } else {
        console.log(`      seeded $${seeded} survived load (${JSON.stringify(live.value)})`);
      }
    }
    const control = ENABLED_ON_LOAD[route];
    if (control) {
      const state = await page.evaluate((sel) => {
        const el = document.querySelector(sel);
        return el ? { present: true, disabled: el.disabled, text: (el.textContent || "").replace(/\s+/g, " ").trim() } : { present: false };
      }, control);
      if (state.present && state.disabled) {
        fail(`${route}: ${state.text || control} is disabled on load with no run in flight — a dead control`);
      }
    }
  }

  pageErrors.length = 0;
  consoleErrors.length = 0;
  if (!hasFeature("room")) {
    console.log("  → /room trimmed, skipping roster/presenter/crash");
  } else {
  // Room demo (GoAkt grains): the page's own polling loop heartbeats on
  // load, so the roster must contain this user; acquire must elect them
  // presenter; crash must bump the room generation (supervisor restart).
  console.log("→ Exercising room roster + presenter lock + crash hook…");
  await page.goto(BASE + "/room", { waitUntil: "load", timeout: 20000 });
  // The room's own polling loop heartbeats on load; poll the roster endpoint
  // for the member rather than napping 1500ms and hoping. The loop below
  // already polls — this just removes the upfront nap, so the bounded retry
  // carries the deadline instead of a fixed guess.
  // Heartbeat is fire-and-forget: poll briefly so transport timing, not
  // grain behavior, decides the verdict.
  let rosterHasUser = false;
  for (let i = 0; i < 10 && !rosterHasUser; i++) {
    rosterHasUser = await page.evaluate(async (email) => {
      const r = await fetch("/room/roster");
      const j = await r.json();
      return (j.members || []).some((m) => m.name === email);
    }, USER_EMAIL);
    if (!rosterHasUser) await page.waitForTimeout(500); // poll interval
  }
  if (!rosterHasUser) fail("room roster does not contain the logged-in user after heartbeat");
  const genBefore = await page.evaluate(async () => (await (await fetch("/room/roster")).json()).generation);
  const presenter = await page.evaluate(async (email) => {
    await fetch("/room/acquire", { method: "POST" });
    const j = await (await fetch("/room/roster")).json();
    return j.presenter;
  }, USER_EMAIL);
  if (presenter !== USER_EMAIL) fail(`room presenter = ${presenter}, want ${USER_EMAIL}`);
  await page.evaluate(() => fetch("/room/crash", { method: "POST" }));
  let genAfter = genBefore;
  for (let i = 0; i < 40 && genAfter <= genBefore; i++) {
    await page.waitForTimeout(500); // poll interval
    genAfter = await page.evaluate(async () => (await (await fetch("/room/roster")).json()).generation);
  }
  if (genAfter <= genBefore) fail(`room generation stuck at ${genAfter} after crash (want > ${genBefore})`);
  if (pageErrors.length > 0) {
    fail(`uncaught JS error during room test: ${pageErrors.map((e) => e.msg).join(" | ")}`);
  }
  } // hasFeature("room")

  // CAL-34: sweep the offline-queue contract. A typo in the offline-reset
  // listener (data-on:gogogo:queued__window vs. gogogo__queued) is invisible to
  // unit tests because the listener is inline markup.
  // One skin ships: the offline-queue contract is swept once, on /todo.
  const offlineSkins = ["daisyui"];
  // Mobile: the sidebar is a drawer, not a scrolling strip of eight links.
  // These assertions ARE the UX decision — the rail starts off-canvas, the
  // module header carries the trigger, Escape closes it, and the page you
  // navigated to is not covered by it afterwards.
  {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(BASE + "/todo", { waitUntil: "load", timeout: 20000 });
    const trigger = page.locator(".app-rail-toggle");
    if (!(await trigger.isVisible())) {
      fail("mobile: no drawer trigger in the module header");
    }
    const before = await page.locator("#app-rail").boundingBox();
    if (!before || before.x >= 0) {
      fail(`mobile: the rail is on screen before opening (x=${before && before.x})`);
    }
    await trigger.click();
    await page.waitForFunction(
      () => document.querySelector("#app-rail").classList.contains("is-open"),
      null,
      { timeout: 5000 },
    );
    console.log("  ✓ mobile: rail starts off-canvas and opens from the header");
    await page.keyboard.press("Escape");
    await page.waitForFunction(
      () => !document.querySelector("#app-rail").classList.contains("is-open"),
      null,
      { timeout: 5000 },
    );
    console.log("  ✓ mobile: Escape closes the drawer");
    await page.setViewportSize({ width: 1280, height: 900 });
  }

  // Demo mode: the state panel is signal-driven and never reloads. The panel
  // HTML ships with the page; $panelVisible controls visibility, and a
  // background fetch persists the session cookie so it survives a reload.
  {
    await page.goto(BASE + "/todo", { waitUntil: "load", timeout: 20000 });
    // Fresh session starts hidden: the node exists (no conditional render),
    // but Datastar hides it via data-show.
    if (await page.locator("#state-panel").isVisible()) {
      fail("demo mode: the state panel is visible by default (product mode must start hidden)");
    }
    const toggle = page.locator(".app-demo-toggle");
    if (!(await toggle.isVisible())) fail("demo mode: no How it works toggle in the module header");
    await toggle.click();
    await page.waitForSelector("#state-panel", { state: "visible", timeout: 5000 });
    console.log("  ✓ demo mode: the toggle shows the state panel without a reload");
    await page.reload({ waitUntil: "load" });
    if (!(await page.locator("#state-panel").isVisible())) {
      fail("demo mode: the panel did not survive a reload (the cookie is not being set)");
    }
    console.log("  ✓ demo mode: the cookie keeps it visible across a reload");
    const closeBtn = page.locator("#state-panel button[aria-label='Close panel']");
    if (!(await closeBtn.isVisible())) fail("demo mode: the panel has no close button");
    await closeBtn.click();
    await page.waitForSelector("#state-panel", { state: "hidden", timeout: 5000 });
    console.log("  ✓ demo mode: the close button hides the panel without a reload");
    await page.locator(".app-demo-toggle").click();
    await page.waitForSelector("#state-panel", { state: "visible", timeout: 5000 });
    await page.locator(".app-demo-toggle").click();
    await page.waitForSelector("#state-panel", { state: "hidden", timeout: 5000 });
    console.log("  ✓ demo mode: the toggle hides it again");
  }

  // Every module page offers the panel: the machinery is a property of the
  // module, not of /todo. A page that stops passing a Panel loses its toggle
  // silently otherwise.
  for (const route of ["/todo", "/chat", "/review"]) {
    await page.goto(BASE + route, { waitUntil: "load", timeout: 20000 });
    if (!(await page.locator(".app-demo-toggle").isVisible())) {
      fail(`demo mode: ${route} has no How it works toggle (did it stop passing a Panel?)`);
    }
  }
  console.log("  ✓ demo mode: every module page offers the panel");

  await verifyAccountMenu(page);

  await verifyStructuralUi(page);

  for (const skin of offlineSkins) {
    pageErrors.length = 0;
    await verifyTodoSearch(page);
    await verifyOfflineTodoQueue(page, context);
    if (pageErrors.length > 0) {
      fail(`uncaught JS error during offline queue test: ${pageErrors.map((e) => e.msg).join(" | ")}`);
    }
  }
  await verifyTodoCalendar(page);
  await verifyOfflineUx(page, context);
  if (pageErrors.length > 0) {
    fail(`uncaught JS error during offline queue test: ${pageErrors.map((e) => e.msg).join(" | ")}`);
  }

  // Stop tracing BEFORE closing: the finally below can only save a trace
  // while the context is open, and the happy path closes it here — which
  // is exactly why passing retries used to arrive without timelines.
  if (tracePath) {
    await context.tracing.stop({ path: tracePath });
    console.log(`  ⏱ trace saved: ${tracePath}`);
  }
  await context.close();
} catch (e) {
  fail(e.stack || String(e));
} finally {
  if (tracePath) {
    try {
      await context.tracing.stop({ path: tracePath });
    } catch {
      // The context may already be closed on the happy path; a failed
      // stop must not mask the real result.
    }
  }
  if (browser) await browser.close().catch(() => {});
  if (server) server.kill("SIGKILL");
  rmSync(tmp, { recursive: true, force: true });
}

if (process.exitCode === 1) {
  console.error("\n❌ Browser smoke test FAILED");
} else {
  console.log("\n✅ Browser smoke test passed (no uncaught client JS errors)");
}
