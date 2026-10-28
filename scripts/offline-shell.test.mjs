// SCOPE:plugin - Shell-level proof for the offline contract (no request parks).
//
// `scripts/outbox.test.mjs` proves the transport properties that need a request
// held open (write-ahead, lost response, two tabs). This suite proves the parts
// that live in the SHELL and the ACCOUNT:
//
//   1. SHARED DEVICE   sign-out purges the queue, the next account inherits
//                      nothing and replays nothing
//   2. LOGOUT CONFIRM  the sign-out warns only when an intent is queued
//   3. DROP NOTICE     an intent the server permanently refuses says so
//   4. SHELL CACHE     offline navigation works, and no /api response is ever
//                      in Cache Storage
//   5. NO-CACHE MODE   OFFLINE_SYNC_ENABLED=false retires the worker, keeps
//                      the write path, and the copy says exactly that
//
// Run: node scripts/offline-shell.test.mjs
//   SMOKE_BIN=/path/to/web   reuse a build (else it builds ./cmd/web)
//   SMOKE_PORT=18371         first server port (the no-cache server uses +1)
//   BROWSER=webkit           run the same suite in WebKit (closer to Safari)
import { chromium, webkit } from "playwright";
import { spawn, execSync } from "node:child_process";
import { createServer } from "node:net";
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const PORT = Number(process.env.SMOKE_PORT || 18371);
const PLAIN_PORT = PORT + 1;
const BASE = `http://127.0.0.1:${PORT}`;
const PLAIN_BASE = `http://127.0.0.1:${PLAIN_PORT}`;
const ENGINE = (process.env.BROWSER || "chromium").toLowerCase();
const USER_EMAIL = "offline-shell@local.dev";
const USER_PASS = "OfflineShellPass!123";

const results = [];
let failures = 0;
const check = async (name, fn) => {
  try {
    await fn();
    results.push("  ✓ " + name);
  } catch (e) {
    failures++;
    results.push("  ❌ " + name + "\n     " + String(e.message || e).split("\n")[0]);
  }
};

const tmp = mkdtempSync(join(tmpdir(), "gogogo-offline-shell-"));
const providedBin = process.env.SMOKE_BIN;
const bin = providedBin ? resolve(providedBin) : join(tmp, "web");
const servers = [];
let browser = null;

function runtimeEnv(port, extra = {}) {
  const dir = join(tmp, "runtime-" + port);
  mkdirSync(dir, { recursive: true });
  return {
    ...process.env,
    // A distinct APP_NAME keeps secrets.Load from decrypting the HOST's
    // production env file (it would close signup).
    APP_NAME: "gogogo-offlineshell",
    ENVIRONMENT: "development",
    SIGNUP_ENABLED: "true",
    DATA_DIR: dir,
    DATABASE_PATH: join(dir, "app.db"),
    TURSO_DB_PATH: join(dir, "app.db"),
    NATS_ENABLED: "false",
    DAGNATS_ENABLED: "false",
    OFFLINE_SYNC_ENABLED: "true",
    GOGOGO_NO_BROWSER: "1",
    HOST: "127.0.0.1",
    PORT: String(port),
    ...extra,
  };
}

function assertPortFree(port) {
  return new Promise((resolveP, reject) => {
    const srv = createServer();
    srv.once("error", (err) => reject(new Error(`port ${port} in use (${err.code})`)));
    srv.once("listening", () => srv.close(() => resolveP()));
    srv.listen(port, "127.0.0.1");
  });
}

async function waitHealthy(base, timeoutMs = 30000) {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    try {
      if ((await (await fetch(base + "/health")).text()).trim() === "ok") return;
    } catch {
      /* not up yet */
    }
    await new Promise((r) => setTimeout(r, 400));
  }
  throw new Error("server did not become healthy: " + base);
}

let accountSeq = 0;
// freshCookie signs up a NEW account. A cookie is single-use here: the moment a
// check signs out, the session row is deleted and the same cookie would bounce
// the next page to /login (which is how the first version of this suite failed
// three checks in a row).
async function freshCookie(base, label) {
  accountSeq++;
  const cookie = await signupCookie(base, `${label}-${accountSeq}@local.dev`, USER_PASS);
  if (!cookie) throw new Error(`signup ${label} failed`);
  return cookie;
}

async function signupCookie(base, email, password) {
  const res = await fetch(base + "/api/auth/signup/credential", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password, name: "Offline" }),
  });
  if (!res.ok) return "";
  const cookies = res.headers.getSetCookie ? res.headers.getSetCookie() : [res.headers.get("set-cookie")];
  const line = (cookies || []).find((c) => c && c.startsWith("gogogo_auth="));
  return line ? line.split(";")[0].slice("gogogo_auth=".length) : "";
}

async function waitFor(pred, timeoutMs = 20000, label = "condition") {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    if (await pred()) return true;
    await new Promise((r) => setTimeout(r, 150));
  }
  throw new Error(`timed out waiting for ${label}`);
}

const outboxStates = (page) =>
  page.evaluate(() =>
    new Promise((resolve) => {
      const open = indexedDB.open("gogogo-outbox");
      open.onerror = () => resolve([]);
      open.onsuccess = () => {
        const db = open.result;
        if (!db.objectStoreNames.contains("ops")) { db.close(); return resolve([]); }
        const all = db.transaction("ops", "readonly").objectStore("ops").getAll();
        all.onsuccess = () => resolve(all.result.map((e) => e.state));
      };
    }),
  );

const outboxCount = (page) =>
  page.evaluate(() =>
    new Promise((resolve) => {
      const open = indexedDB.open("gogogo-outbox");
      open.onerror = () => resolve(0);
      open.onsuccess = () => {
        const db = open.result;
        if (!db.objectStoreNames.contains("ops")) { db.close(); return resolve(0); }
        const tx = db.transaction("ops", "readonly");
        const c = tx.objectStore("ops").count();
        c.onsuccess = () => resolve(c.result);
        c.onerror = () => resolve(0);
      };
    }),
  );

const input = (page) => page.getByPlaceholder(/what needs doing|add a new todo/i);

async function newPage(context, base, guardSw = false) {
  if (guardSw) await context.route("**/sw.js", (r) => r.abort());
  const page = await context.newPage();
  page.on("pageerror", (e) => console.log("PAGEERROR", String(e).slice(0, 200)));
  await page.goto(base + "/todo", { waitUntil: "load", timeout: 25000 });
  return page;
}

// queueCreate writes an intent the server will refuse, so it stays queued
// while the page is ONLINE (no request parking, no timing games).
async function queueCreate(page, context, base, status = 503) {
  // PAGE route, not context: the page's unrouteAll must be able to lift it
  // (a context route survived the reload and kept answering 503 forever).
  await page.route("**/api/todos?*", (r) => r.fulfill({ status, body: "blocked" }));
  const title = `shell-${Date.now()}`;
  await input(page).fill(title);
  await input(page).press("Enter");
  // PENDING, not merely stored: an intent still "inflight" is a request the
  // page is waiting on, and reloading there leaves a 30s lease nobody drains.
  await waitFor(async () => {
    const states = await outboxStates(page);
    return states.includes("pending");
  }, 12000, "the intent to settle as pending");
  return title;
}

try {
  if (!providedBin) {
    console.log("→ Building binary (./cmd/web)…");
    execSync(`go build -o ${JSON.stringify(bin)} ./cmd/web`, { stdio: "inherit", timeout: 240000 });
  }
  await assertPortFree(PORT);
  await assertPortFree(PLAIN_PORT);

  console.log(`→ Starting servers (${BASE}, ${PLAIN_BASE})…`);
  servers.push(spawn(bin, [], { stdio: "ignore", env: runtimeEnv(PORT) }));
  servers.push(spawn(bin, [], { stdio: "ignore", env: runtimeEnv(PLAIN_PORT, { OFFLINE_SYNC_ENABLED: "false" }) }));
  await waitHealthy(BASE);
  await waitHealthy(PLAIN_BASE);

  const plainCookie = await freshCookie(PLAIN_BASE, "nocache");

  const engine = ENGINE === "webkit" ? webkit : chromium;
  browser = await engine.launch({
    headless: true,
    args: ENGINE === "webkit" ? [] : ["--no-sandbox", "--disable-dev-shm-usage", "--disable-gpu"],
  });
  console.log(`→ Browser: ${ENGINE}`);

  // ---- 1: shared device --------------------------------------------------
  await check("shared device: sign-out purges the queue, the next account inherits nothing", async () => {
    const cookie = await freshCookie(BASE, "shared");
    const otherCookie = await freshCookie(BASE, "shared-other");
    const ctx = await browser.newContext();
    await ctx.addCookies([{ name: "gogogo_auth", value: cookie, url: BASE + "/" }]);
    const page = await newPage(ctx, BASE, true);
    const title = await queueCreate(page, ctx, BASE, 503);
    await page.unrouteAll({ behavior: "ignoreErrors" });

    page.on("dialog", (d) => d.accept());
    await page.evaluate(() => document.querySelector('form[action="/logout"]').requestSubmit());
    await page.waitForURL("**/login**", { timeout: 15000 });

    await waitFor(async () =>
      page.evaluate(async () => {
        const dbs = await indexedDB.databases().catch(() => []);
        const cachesLeft = (await caches.keys().catch(() => [])).length;
        const regs = (await navigator.serviceWorker.getRegistrations().catch(() => [])).length;
        return !dbs.some((d) => d.name === "gogogo-outbox") && cachesLeft === 0 && regs === 0;
      }), 15000, "Clear-Site-Data to purge origin storage");

    await ctx.addCookies([{ name: "gogogo_auth", value: otherCookie, url: BASE + "/" }]);
    const other = await newPage(ctx, BASE, true);
    if ((await outboxCount(other)) !== 0) throw new Error("the next account inherited a queue");
    const rows = await other.evaluate(() => document.querySelector("#todo-list")?.textContent || "");
    if (rows.includes(title)) throw new Error("the next account sees the previous account's title");
    await ctx.close();
  });

  // ---- 2: logout confirm -------------------------------------------------
  await check("logout confirm: warns when an intent is queued, dismiss keeps the page", async () => {
    const cookie = await freshCookie(BASE, "confirm");
    const ctx = await browser.newContext();
    await ctx.addCookies([{ name: "gogogo_auth", value: cookie, url: BASE + "/" }]);
    const page = await newPage(ctx, BASE, true);
    await queueCreate(page, ctx, BASE, 503);

    const seen = [];
    page.on("dialog", (d) => { seen.push(d.message()); d.dismiss(); });
    await page.evaluate(() => document.querySelector('form[action="/logout"]').requestSubmit());
    await waitFor(() => seen.length > 0, 6000, "the confirm dialog");
    if (!seen[0].includes("queued change")) throw new Error(`dialog said ${JSON.stringify(seen[0])}`);
    if (!page.url().includes("/todo")) throw new Error("a dismissed sign-out navigated anyway");

    // With an empty queue the dialog must not appear at all. Recover the way a
    // user does — the route is gone, so reload and let the page drain on load.
    await page.unrouteAll({ behavior: "ignoreErrors" });
    await page.reload({ waitUntil: "load" });
    // A forced pass each tick: an explicit kick is what the product does on
    // load/online/visible, and the fixture must not wait out the backoff.
    await waitFor(async () => {
      await page.evaluate(() => window.GogogoOutbox.drain(true)).catch(() => {});
      return (await outboxCount(page)) === 0;
    }, 30000, "the queue to drain");

    page.removeAllListeners("dialog");
    let asked = false;
    page.on("dialog", (d) => { asked = true; d.accept(); });
    await page.evaluate(() => document.querySelector('form[action="/logout"]').requestSubmit());
    await page.waitForURL("**/login**", { timeout: 15000 });
    if (asked) throw new Error("an empty queue still asked about losing changes");
    await ctx.close();
  });

  // ---- 3: drop notice ----------------------------------------------------
  await check("drop notice: a permanently refused intent says so in the banner", async () => {
    const cookie = await freshCookie(BASE, "drop");
    const ctx = await browser.newContext();
    await ctx.addCookies([{ name: "gogogo_auth", value: cookie, url: BASE + "/" }]);
    const page = await newPage(ctx, BASE, true);
    await queueCreate(page, ctx, BASE, 400); // permanent, not auth (401/403 pauses)
    // The notice is a hidden span until a drop happens: assert VISIBILITY, not
    // just the text (the expression is in the DOM from the first paint).
    try {
      await waitFor(() => page.locator("#offline-banner-drop").isVisible(), 12000, "the drop notice");
    } catch (e) {
      const diag = {
        count: await outboxCount(page),
        banner: await page.locator("#offline-banner").getAttribute("class"),
        dropClass: await page.locator("#offline-banner-drop").getAttribute("class"),
        dropText: await page.locator("#offline-banner-drop").textContent(),
        bannerText: await page.locator("#offline-banner-text").textContent(),
      };
      await page.evaluate(() => window.GogogoOutbox.drain(true)).catch(() => {});
      await new Promise((r) => setTimeout(r, 3000));
      diag.afterCount = await outboxCount(page);
      diag.afterDropClass = await page.locator("#offline-banner-drop").getAttribute("class");
      console.error("DROP DIAG", JSON.stringify(diag));
      throw e;
    }
    const text = await page.locator("#offline-banner-drop").textContent();
    if (!text.includes("couldn't be saved")) throw new Error(`unexpected notice: ${JSON.stringify(text)}`);
    if (!text.includes("refused")) throw new Error(`the notice lost the reason: ${JSON.stringify(text)}`);
    await waitFor(async () => (await outboxCount(page)) === 0, 15000, "the queue to settle");
    await ctx.close();
  });

  // ---- 4: shell cache ----------------------------------------------------
  await check("shell cache: offline navigation works and no /api response is cached", async () => {
    const cookie = await freshCookie(BASE, "cache");
    const ctx = await browser.newContext();
    await ctx.addCookies([{ name: "gogogo_auth", value: cookie, url: BASE + "/" }]);
    const page = await newPage(ctx, BASE);
    await page.evaluate(() => navigator.serviceWorker.ready);
    await page.reload({ waitUntil: "load" });
    await waitFor(() => page.evaluate(() => Boolean(navigator.serviceWorker.controller)), 15000, "SW control");
    await waitFor(async () => (await page.evaluate(() => caches.keys())).includes("gogogo-pages-v2"), 15000, "the page cache");

    const apiCached = await page.evaluate(async () => {
      for (const key of await caches.keys()) {
        const reqs = await (await caches.open(key)).keys();
        if (reqs.some((r) => new URL(r.url).pathname.startsWith("/api/"))) return true;
      }
      return false;
    });
    if (apiCached) throw new Error("an /api response is in Cache Storage: the worker touched the API");

    await ctx.setOffline(true);
    await page.goto(BASE + "/todo", { waitUntil: "load", timeout: 20000 });
    await input(page).waitFor({ state: "visible", timeout: 15000 });
    await ctx.setOffline(false);
    await ctx.close();
  });

  // ---- 5: no-cache mode --------------------------------------------------
  await check("no-cache mode: no worker, cache-disabled copy, writes still queue", async () => {
    const ctx = await browser.newContext();
    await ctx.addCookies([{ name: "gogogo_auth", value: plainCookie, url: PLAIN_BASE + "/" }]);
    const page = await newPage(ctx, PLAIN_BASE);

    const online = await page.locator("#offline-banner-text").textContent();
    if (online.includes("requests will fail")) throw new Error("stale copy: the write path is not disabled");

    await ctx.setOffline(true);
    await waitFor(async () => (await page.locator("#offline-banner-text").textContent())?.includes("page cache disabled"),
      10000, "the cache-disabled copy");
    const title = `shell-nocache-${Date.now()}`;
    await input(page).fill(title);
    await input(page).press("Enter");
    await waitFor(async () => (await outboxCount(page)) === 1, 10000, "the intent");
    await ctx.setOffline(false);
    await waitFor(async () => {
      const res = await page.request.get(PLAIN_BASE + "/api/todos/fragment?filter=all&scope=all&q=&clientID=").catch(() => null);
      return Boolean(res && res.ok() && (await res.text()).includes(title));
    }, 25000, "the replay to land");

    const regs = await page.evaluate(() => navigator.serviceWorker.getRegistrations().then((r) => r.length));
    if (regs !== 0) throw new Error(`no-cache mode registered ${regs} worker(s)`);
    await ctx.close();
  });
} catch (e) {
  failures++;
  results.push("  ❌ suite error\n     " + String(e.stack || e).split("\n").slice(0, 3).join("\n     "));
} finally {
  console.log(results.join("\n"));
  if (browser) await browser.close().catch(() => {});
  for (const s of servers) s.kill("SIGKILL");
  rmSync(tmp, { recursive: true, force: true });
}

if (failures > 0) {
  console.error(`\n❌ offline shell suite FAILED (${failures} failing check(s), ${ENGINE})`);
  process.exitCode = 1;
} else {
  console.log(`\n✅ offline shell suite passed (${results.length} checks, ${ENGINE})`);
}
