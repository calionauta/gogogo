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

import { chromium } from "playwright";
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
const ROUTES = ["/todo", "/chat", "/whiteboard", "/room", "/login", "/genui", "/review"];

// Trim-aware gating: a trimmed checkout must neither fail on routes that
// no longer exist nor silently pass over them. SMOKE_FEATURES lists the
// features present (default: the full template surface); trim output
// (installer Warns) tells you the value to use. Unknown names fail fast:
// a typo must never read as "trimmed".
const ALL_FEATURES = ["todo", "chat", "whiteboard", "room", "login", "genui", "review"];
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
const SEEDED_SIGNALS = { "/genui": "genui_pending", "/review": "review_pending" };

// Route → the submit control that must be USABLE on load. A disabled control
// here means the page is born in a stuck state: nothing the user does can
// clear a signal that only its own submit handler clears (and the form that
// would is the one holding the dead button).
const ENABLED_ON_LOAD = { "/genui": "button[data-attr\\:disabled]", "/review": "#review-composer button" };

const tmp = mkdtempSync(join(tmpdir(), "gogogo-smoke-"));
const runtimeDir = join(tmp, "runtime");
mkdirSync(runtimeDir, { recursive: true });
const runtimeEnv = {
  ...process.env,
  DATA_DIR: runtimeDir,
  DATABASE_PATH: join(runtimeDir, "app.db"),
  NATS_ENABLED: "false",
  DAGNATS_ENABLED: "false",
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
        const open = indexedDB.open("gogogo-offline-queue", 1);
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

async function verifySingleOnlineTodoSubmit(page, skin) {
  if (skin !== "basecoat") return;

  const title = `online-single-submit-${Date.now()}`;
  const skinPath = `/todo?skin=${encodeURIComponent(skin)}`;

  console.log("→ Exercising Basecoat rapid online submit guard…");
  await page.goto(BASE + skinPath, { waitUntil: "load", timeout: 20000 });
  await page.evaluate(() => {
    const originalFetch = window.fetch.bind(window);
    let release;
    window.__smokeHeldTodoPost = {
      count: 0,
      release: () => release?.(),
      restore: () => {
        window.fetch = originalFetch;
      },
    };
    window.fetch = async (input, init) => {
      const url = typeof input === "string" ? input : input.url;
      if ((init?.method || "GET").toUpperCase() !== "POST" || !url.includes("/api/todos")) {
        return originalFetch(input, init);
      }
      window.__smokeHeldTodoPost.count++;
      await new Promise((resolve) => {
        release = resolve;
      });
      return originalFetch(input, init);
    };
  });

  try {
    const titleInput = page.getByPlaceholder("Add a new todo...");
    await titleInput.fill(title);
    await titleInput.press("Enter");
    await titleInput.press("Enter");
    await page.waitForFunction(() => window.__smokeHeldTodoPost.count >= 1);
    const postCount = await page.evaluate(() => window.__smokeHeldTodoPost.count);
    if (postCount !== 1) {
      throw new Error(`rapid Basecoat submit made ${postCount} requests; expected exactly one`);
    }
  } finally {
    await page.evaluate(() => {
      window.__smokeHeldTodoPost.release();
      window.__smokeHeldTodoPost.restore();
    });
  }

  const row = page
    .locator('[id^="todo-"]:not(#todo-list)')
    .filter({ hasText: title });
  await row.waitFor({ state: "visible", timeout: 20000 });
  console.log("  ✓ Basecoat rapid online submit sends one request");
}

// Verify the search box end to end: type a term that matches ONE of two
// rows, and require the list to narrow to it. This is the smoke-level proof
// that the endpoint, the layer and the `#todo-list` patch agree a unit test
// can pass with the template wired to nothing.
//
// Deliberately asserts on a MIDDLE substring: the default layer is the SQL
// filter, and an exact-match regression would show up here as "no match".
async function verifyTodoSearch(page, skin) {
  const marker = `searchable-${Date.now()}`;
  const other = `unrelated-${Date.now()}`;
  const skinPath = skin === "daisyui" ? "/todo" : `/todo?skin=${encodeURIComponent(skin)}`;

  console.log(`-> Exercising todo search (skin=${skin})...`);
  await page.goto(BASE + skinPath, { waitUntil: "load", timeout: 20000 });

  for (const title of [marker, other]) {
    const input = page.getByPlaceholder("Add a new todo...");
    await input.fill(title);
    await input.press("Enter");
    await page.locator('[id^="todo-"]:not(#todo-list)').filter({ hasText: title })
      .waitFor({ state: "visible", timeout: 20000 });
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

  const rows = page.locator('[id^="todo-"]:not(#todo-list)');
  await rows.filter({ hasText: marker }).waitFor({ state: "visible", timeout: 20000 });
  const leaked = await rows.filter({ hasText: other }).count();
  if (leaked !== 0) {
    throw new Error(`search did not narrow the list: ${leaked} non-matching row(s) still rendered`);
  }
  console.log(`  OK skin=${skin}: search narrowed the list to the matching row`);
}

async function verifyOfflineTodoQueue(page, context, skin = "daisyui") {
  const title = `offline-smoke-${Date.now()}`;
  const skinPath = skin === "daisyui" ? "/todo" : `/todo?skin=${encodeURIComponent(skin)}`;
  console.log(`→ Exercising offline todo add + delete queue (skin=${skin})…`);
  await page.goto(BASE + skinPath, { waitUntil: "load", timeout: 20000 });
  await waitForServiceWorkerControl(page);

  const banner = page.locator("#offline-banner");
  if ((await banner.getAttribute("data-offline-sync")) !== "true") {
    throw new Error("offline banner rendered with offline sync disabled");
  }

  // CAL-34 regression guard: the SW postMessage bridge in
  // internal/components/offline_banner.templ dispatches the event as
  // `gogogo:queued` (Datastar event-namespace separator is the colon).
  // The basecoat skin previously listened for
  // `gogogo__queued` (double underscore) — a typo that left $loading
  // stuck true after an offline add, blocking every follow-up submit.
  // Catch that here by asserting the rendered attribute uses the
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
      `skin=${skin} listens for "${queuedListener ?? "<none>"}"; ` +
        'expected "data-on:gogogo:queued__window" (Datastar event namespace is ":", not "__")',
    );
  }

  try {
    await context.setOffline(true);
    await timedWait(page, "offline-banner-queued", () =>
      document.querySelector("#offline-banner-text")?.textContent?.includes("queued"),
    );

    const titleInput = page.getByPlaceholder("Add a new todo...");
    const addButton = page.getByRole("button", { name: "Add" });
    await titleInput.fill(title);
    // Submit via Enter on the input — works for every skin regardless
    // of whether the Add trigger is a native <button> (DaisyUI/Basecoat)
    // or a data-variant button. Clicking the row button
    // doesn't always dispatch a native form-submit event in headless
    // Chromium, which is fine for users (they click too) but breaks the
    // headless harness; Enter on the input bypasses the question.
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
    // Row class differs per skin (DaisyUI uses .todo-item, Basecoat
    // uses .item); every skin uses id="todo-<id>" for the row, and the
    // container is #todo-list (which itself starts with "todo-"). Scope by
    // id prefix, exclude the list container, and narrow by text for the
    // exact row match.
    const row = page
      .locator('[id^="todo-"]:not(#todo-list)')
      .filter({ hasText: title });
    await row.waitFor({ state: "visible", timeout: 20000 });
    // Wait for replay to drain the IndexedDB queue. The basecoat
    // skin's SSE patch is structurally different from the SW's
    // optimistic row (DaisyUI-classed div vs. basecoat row), so the
    // data-pending swap that DaisyUI gets is unreliable across skins.
    // The pending-count drain is the single source of truth for
    // "the server accepted the replay".
    await timedWait(page, "offline-queue-drain", async () => {
      const open = indexedDB.open("gogogo-offline-queue", 1);
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
    // DaisyUI: also wait for the SW's optimistic row to be replaced by
    // the server row (needed because the delete test below clicks the
    // row's enabled delete button, which the disabled optimistic
    // button would block). The other skins handle the optimistic→server
    // swap differently, so this guard is daisyui-only.
    if (skin === "daisyui") {
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
      const open = indexedDB.open("gogogo-offline-queue", 1);
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
    // The delete-confirm dialog auto-open behaviour varies per skin:
    //   - DaisyUI: the row button calls .showModal() inline.
    //   - Basecoat: the row button only sets $confirmingDeleteId;
    //     the dialog's open/close wiring is skin-specific. Keep this part
    //     of the sweep daisyui-only so the harness stays decoupled from
    //     pre-existing skin-specific dialog wiring; the CAL-34 contract
    //     (offline add resets UI + queue + replay) is already covered
    //     above for every skin.
    if (skin !== "daisyui") {
      console.log(`  ✓ skin=${skin}: offline add resets UI, queues, replays`);
      return;
    }
    await context.setOffline(true);
    // The delete-button shape varies per skin:
    //   - DaisyUI: button[title="Delete todo"] (no aria-label)
    //   - Basecoat: button[aria-label="Delete"]
    // The row locator above already targets the right row (filtered by
    // text), so scope the click to that row and match any of the three
    // shapes. The confirm-modal "Delete" button lives inside the
    // dialog, so scoping to the row keeps the locator unambiguous.
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
    console.log(`  ✓ skin=${skin}: offline add resets UI, queues, replays; offline delete replays`);
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
    await page.getByPlaceholder("Add a new todo...").waitFor({ state: "visible", timeout: 15000 });
    console.log("  ✓ offline navigation served the cached todo page");

    // 4) logout form must wire the clear-pages purge message. The form
    // drives its own POST (native submit is swallowed by Datastar's
    // data-on handler), so the contract lives on data-on:submit__prevent.
    const logoutForm = page.locator('form[action="/logout"]');
    const onSubmit = await logoutForm.getAttribute("data-on:submit__prevent");
    if (!onSubmit || !onSubmit.includes("clear-pages")) {
      throw new Error("logout form does not post clear-pages to the service worker");
    }
  } finally {
    await context.setOffline(false);
  }

  // 5) logout (online) => SW purges the cached page.
  // Drives the mechanism directly — POST /logout for the session plus
  // the clear-pages postMessage the navbar form sends — instead of
  // clicking through the login UI. The login page is trimmable while
  // the endpoint and the SW protocol are core; this keeps the purge
  // covered either way. Same coverage, zero UI coupling.
  console.log("→ Logging out to verify clear-pages purge…");
  await page.evaluate(async (base) => {
    await fetch(base + "/logout", { method: "POST" });
    const r = await navigator.serviceWorker.ready;
    if (r.active) r.active.postMessage({ type: "clear-pages" });
  }, BASE);
  await page.waitForFunction(
    (u) => caches.match(u).then((r) => !r),
    BASE + "/todo",
    { timeout: 8000 },
  );
  console.log("  ✓ logout purged the cached todo page (clear-pages)");
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
    // Give inline scripts + SSE a moment to execute.
    await page.waitForTimeout(1000);
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
  await page.waitForTimeout(1500);
  // Heartbeat is fire-and-forget: poll briefly so transport timing, not
  // grain behavior, decides the verdict.
  let rosterHasUser = false;
  for (let i = 0; i < 10 && !rosterHasUser; i++) {
    rosterHasUser = await page.evaluate(async (email) => {
      const r = await fetch("/room/roster");
      const j = await r.json();
      return (j.members || []).some((m) => m.name === email);
    }, USER_EMAIL);
    if (!rosterHasUser) await page.waitForTimeout(500);
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
    await page.waitForTimeout(500);
    genAfter = await page.evaluate(async () => (await (await fetch("/room/roster")).json()).generation);
  }
  if (genAfter <= genBefore) fail(`room generation stuck at ${genAfter} after crash (want > ${genBefore})`);
  if (pageErrors.length > 0) {
    fail(`uncaught JS error during room test: ${pageErrors.map((e) => e.msg).join(" | ")}`);
  }
  } // hasFeature("room")

  // CAL-34: sweep every UI skin through the offline-queue contract.
  // Each skin has its own `.templ`, and a typo in the offline-reset
  // listener (data-on:gogogo:queued__window vs. gogogo__queued) used
  // to slip through because the harness only covered daisyui.
  // Basecoat is auto-detected, not assumed: with skins-extra trimmed the
  // server falls back to DaisyUI with a warning log, so a blind second
  // pass would run DaisyUI twice labeled "basecoat" (silent coverage
  // loss). A genuinely identical render can only mean the fallback —
  // the two skins share no markup.
  const offlineSkins = ["daisyui"];
  {
    // Authenticated compare (page carries the session cookies): the login
    // redirect would render identically for both and fake a fallback.
    const distinct = await page.evaluate(async (base) => {
      const a = await (await fetch(base + "/todo")).text();
      const b = await (await fetch(base + "/todo?skin=basecoat")).text();
      return a !== b;
    }, BASE);
    if (distinct) offlineSkins.push("basecoat");
    else console.log("  → basecoat skin absent (fallback to DaisyUI), single pass");
  }
  for (const skin of offlineSkins) {
    pageErrors.length = 0;
    await verifySingleOnlineTodoSubmit(page, skin);
    await verifyTodoSearch(page, skin);
    await verifyOfflineTodoQueue(page, context, skin);
    if (pageErrors.length > 0) {
      fail(`uncaught JS error during offline queue test (skin=${skin}): ${pageErrors.map((e) => e.msg).join(" | ")}`);
    }
  }
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
