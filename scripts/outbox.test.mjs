// SCOPE:plugin - Behavioral proof for the durable outbox (write-ahead + receipt).
//
// Three properties the design depends on, none of which a Go test can see:
//
//   1. WRITE-AHEAD. The intent is in IndexedDB BEFORE the request settles.
//      The request is held open here; the assertion runs while it hangs.
//      The old Service Worker queue could only react to a failed fetch, so a
//      tab closed during a hanging request lost the write.
//
//   2. LOST RESPONSE. The server commits, the response never arrives, the
//      outbox replays the SAME intent. A toggle is read-then-flip, so a second
//      delivery must be a no-op (op_receipts). Before the receipt, the replay
//      flipped the row back.
//
//   3. NO WORKER REQUIREMENT. Both run with /sw.js blocked: the queue lives in
//      the page, so "the registration was blocked/evicted" is not a lost write.
//
// Run: node scripts/outbox.test.mjs  (SMOKE_BIN reuses a build, SMOKE_PORT
// overrides the port). Not wired into a gate: it needs a browser and the CI
// browser job already runs five suites; it is the on-demand proof for the
// offline-write contract.
import { chromium } from "playwright";
import { spawn, execSync } from "node:child_process";
import { createServer } from "node:net";
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const PORT = Number(process.env.SMOKE_PORT || 18371);
const BASE = `http://127.0.0.1:${PORT}`;
const USER_EMAIL = "outbox-user@local.dev";
const USER_PASS = "OutboxUserPass!123";

const fail = (msg) => {
  console.error("❌ " + msg);
  process.exitCode = 1;
};

const tmp = mkdtempSync(join(tmpdir(), "gogogo-outbox-"));
const runtimeDir = join(tmp, "runtime");
mkdirSync(runtimeDir, { recursive: true });
const runtimeEnv = {
  ...process.env,
  // A distinct APP_NAME keeps secrets.Load from decrypting the HOST's
  // production ~/.secrets/gogogo.env.age into this process (it would flip
  // ENVIRONMENT=production and close signup).
  APP_NAME: "gogogo-outboxtest",
  ENVIRONMENT: "development",
  SIGNUP_ENABLED: "true",
  DATA_DIR: runtimeDir,
  DATABASE_PATH: join(runtimeDir, "app.db"),
  NATS_ENABLED: "false",
  DAGNATS_ENABLED: "false",
  OFFLINE_SYNC_ENABLED: "true",
  GOGOGO_NO_BROWSER: "1",
  HOST: "127.0.0.1",
  PORT: String(PORT),
  TURSO_DB_PATH: join(runtimeDir, "app.db"),
};
const providedBin = process.env.SMOKE_BIN;
const bin = providedBin ? resolve(providedBin) : join(tmp, "web");

let server = null;
let browser = null;

// assertPortFree refuses to run against a co-tenant process. This host runs
// several agents; a fixed port is routinely taken, and talking to someone
// else's server turns a product failure into a mystery 401.
function assertPortFree(port) {
  return new Promise((resolve, reject) => {
    const srv = createServer();
    srv.once("error", (err) =>
      reject(new Error(`port ${port} is already in use (${err.code}); set SMOKE_PORT to a free one`)),
    );
    srv.once("listening", () => srv.close(() => resolve()));
    srv.listen(port, "127.0.0.1");
  });
}

async function signupCookie(email, password) {
  const res = await fetch(BASE + "/api/auth/signup/credential", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password, name: "Outbox" }),
  });
  if (!res.ok) {
    console.error("signup status", res.status, (await res.text()).slice(0, 300));
    return "";
  }
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

// outboxEntries reads the ONE durable outbox. `state` distinguishes an intent
// still on the wire ("inflight") from one waiting for a replay ("pending").
async function outboxEntries(page) {
  return page.evaluate(() =>
    new Promise((resolve) => {
      const open = indexedDB.open("gogogo-outbox");
      open.onerror = () => resolve([]);
      open.onsuccess = () => {
        const db = open.result;
        if (!db.objectStoreNames.contains("ops")) {
          db.close();
          return resolve([]);
        }
        const tx = db.transaction("ops", "readonly");
        const all = tx.objectStore("ops").getAll();
        all.onsuccess = () =>
          resolve(
            all.result.map((e) => ({ url: e.url, state: e.state, key: e.key, tries: e.tries })),
          );
        all.onerror = () => resolve([]);
      };
    }),
  );
}

// serverHas asks the SERVER (the only truth for "the write landed").
async function serverHas(page, title) {
  const res = await page.request
    .get(BASE + "/api/todos/fragment?filter=all&scope=all&q=&clientID=", { timeout: 10000 })
    .catch(() => null);
  if (!res || !res.ok()) return false;
  return (await res.text()).includes(title);
}

// serverChecked reports the row's persisted state: true/false, or null when
// the row is absent. The two-tab and lost-response cases need the STATE, not
// "the row exists" — asserting on existence cannot tell one flip from two.
async function serverChecked(page, title) {
  const res = await page.request
    .get(BASE + "/api/todos/fragment?filter=all&scope=all&q=&clientID=", { timeout: 10000 })
    .catch(() => null);
  if (!res || !res.ok()) return null;
  const html = await res.text();
  const idx = html.indexOf(title);
  if (idx < 0) return null;
  return html.slice(Math.max(0, idx - 900), idx).includes("checked");
}

async function waitServer(page, title, timeoutMs = 25000) {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    if (await serverHas(page, title)) return true;
    await new Promise((r) => setTimeout(r, 200));
  }
  return false;
}

try {
  if (providedBin) {
    console.log(`→ Using prebuilt binary ${bin}…`);
  } else {
    console.log("→ Building binary (./cmd/web)…");
    execSync(`go build -o ${JSON.stringify(bin)} ./cmd/web`, {
      stdio: "inherit",
      timeout: 240000,
    });
  }

  await assertPortFree(PORT);
  console.log(`→ Starting server on ${BASE}…`);
  server = spawn(bin, [], { stdio: "ignore", env: runtimeEnv });
  await waitForHealth();

  const authCookie = await signupCookie(USER_EMAIL, USER_PASS);
  if (!authCookie) throw new Error("signup did not return a gogogo_auth cookie");

  browser = await chromium.launch({ headless: true, args: ["--no-sandbox"] });
  const context = await browser.newContext();
  await context.addCookies([{ name: "gogogo_auth", value: authCookie, url: BASE + "/" }]);
  // No worker: the queue must not depend on a registration existing.
  await context.route("**/sw.js", (route) => route.abort());

  const page = await context.newPage();
  const pageErrors = [];
  page.on("pageerror", (err) => { pageErrors.push(String(err)); console.log("PAGEERROR", String(err).slice(0, 300)); });

  await page.goto(BASE + "/todo", { waitUntil: "load", timeout: 20000 });

  // ---- 1) write-ahead: the intent exists while the request hangs ----------
  console.log("→ Proving the intent is stored before the request settles…");
  let releaseFirst;
  const firstHang = new Promise((r) => { releaseFirst = r; });
  let releaseReplay;
  const replayHang = new Promise((r) => { releaseReplay = r; });
  // phase: hold the first send, then hold the drain's REPLAY so the pending
  // state is observable, then let everything through.
  let phase = "hold-first";
  await page.route("**/api/todos?*", async (route) => {
    if (phase === "hold-first") {
      phase = "hold-replay";
      await firstHang;       // the request is OPEN here
      return route.abort();  // then it fails, like a dying network
    }
    if (phase === "hold-replay") {
      phase = "pass";
      await replayHang;      // the replay is OPEN here
      return route.continue();
    }
    return route.continue();
  });

  const titleA = `outbox-writeahead-${Date.now()}`;
  await page.getByPlaceholder(/what needs doing|add a new todo/i).fill(titleA);
  await page.getByPlaceholder(/what needs doing|add a new todo/i).press("Enter");

  let entries = [];
  const deadline = Date.now() + 8000;
  while (Date.now() < deadline) {
    entries = await outboxEntries(page);
    if (entries.length === 1) break;
    await new Promise((r) => setTimeout(r, 100));
  }
  if (entries.length !== 1) {
    fail(`while the request hung, the outbox held ${entries.length} intents (want 1) — the write is not ahead of the network`);
  } else if (!entries[0].url.includes("idem_key=")) {
    fail("the stored intent lost its idem_key: a replay could duplicate the row");
  } else {
    console.log("  ✓ the intent is durable while the request is still open");
  }
  if (entries[0] && entries[0].state !== "inflight") {
    fail(`the stored intent state = ${entries[0].state}, want "inflight" (it has not settled yet)`);
  } else {
    console.log("  ✓ the intent is marked inflight, not queued-after-failure");
  }

  releaseFirst();
  // Wait for the drain to pick it up and block on the replay route.
  const pendingDeadline = Date.now() + 10000;
  let pendingState = "";
  while (Date.now() < pendingDeadline) {
    entries = await outboxEntries(page);
    pendingState = entries[0] ? entries[0].state : "(gone)";
    if (pendingState === "pending") break;
    await new Promise((r) => setTimeout(r, 100));
  }
  if (pendingState !== "pending") {
    fail(`the failed intent is ${pendingState}, want "pending" — the replay must own it, not a lost request`);
  } else {
    console.log("  ✓ a failed request turns the intent pending and the drain picks it up");
  }

  releaseReplay();
  if (!(await waitServer(page, titleA))) {
    fail("the pending create never reached the server after the request failed");
  } else {
    console.log("  ✓ the pending create replayed and landed");
  }

  // ---- 2) lost response: the replay must not flip twice -------------------
  console.log("→ Proving a lost response cannot apply a toggle twice…");
  const titleB = `outbox-lost-${Date.now()}`;
  const input = page.getByPlaceholder(/what needs doing|add a new todo/i);
  await input.fill(titleB);
  await input.press("Enter");
  if (!(await waitServer(page, titleB))) throw new Error("setup create never landed");

  // The first delivery reaches the server and its RESPONSE is lost. The
  // replay is then HELD so the test can prove (a) the original really landed
  // and (b) the replay is what the receipt has to neutralise. Without the
  // hold, a replay that never happens also "passes".
  let togglePhase = "first";
  let releaseToggle;
  const toggleHang = new Promise((r) => { releaseToggle = r; });
  await page.route("**/api/todos/*/toggle*", async (route) => {
    if (togglePhase === "first") {
      togglePhase = "replay";
      await route.fetch();      // the server commits
      await route.abort("failed"); // the page never sees the answer
      return;
    }
    if (togglePhase === "replay") {
      togglePhase = "pass";
      await toggleHang;         // hold the replay for the assertion
      return route.continue();
    }
    return route.continue();
  });

  const row = page.locator('[id^="todo-"]:not(#todo-list)').filter({ hasText: titleB }).first();
  await row.locator('input[type="checkbox"]').click();

  // (a) the original landed: the server is already checked while the replay
  // is still held.
  let landed = null;
  const holdDeadline = Date.now() + 15000;
  while (Date.now() < holdDeadline) {
    landed = await serverChecked(page, titleB);
    if (landed === true) break;
    await new Promise((r) => setTimeout(r, 100));
  }
  if (landed !== true) {
    fail(`the first delivery never landed (state ${landed}); the lost-response case was not exercised`);
  } else {
    console.log("  ✓ the original delivery landed (response lost)");
  }
  if (togglePhase !== "replay") {
    fail("the outbox never replayed the lost intent — the receipt was not exercised");
  } else {
    console.log("  ✓ the outbox attempted the replay with the same intent");
  }

  // (b) release the replay: the receipt must make it a no-op.
  releaseToggle();
  await new Promise((r) => setTimeout(r, 3000));
  const after = await serverChecked(page, titleB);
  if (after !== true) {
    fail(`after the replay the row is ${after}: the replay flipped it back (two deliveries, two effects)`);
  } else {
    console.log("  ✓ the replay produced exactly ONE flip");
  }

  // ---- 3) two tabs: one intent, one effect --------------------------------
  console.log("→ Proving two tabs cannot double-apply one queued intent…");
  const page2 = await context.newPage();
  await page2.goto(BASE + "/todo", { waitUntil: "load", timeout: 20000 });

  // Queue a toggle while offline from tab 1, then let BOTH tabs drain.
  const titleC = `outbox-tabs-${Date.now()}`;
  await input.fill(titleC);
  await input.press("Enter");
  if (!(await waitServer(page, titleC))) throw new Error("setup create for the two-tab case never landed");

  await context.setOffline(true);
  const rowC = page.locator('[id^="todo-"]:not(#todo-list)').filter({ hasText: titleC }).first();
  await rowC.locator('input[type="checkbox"]').click();
  await new Promise((r) => setTimeout(r, 800)); // let the intent settle as pending
  await context.setOffline(false);
  // Wake both tabs so both drains race for the same entry.
  await page.evaluate(() => window.GogogoOutbox && window.GogogoOutbox.drain());
  await page2.evaluate(() => window.GogogoOutbox && window.GogogoOutbox.drain());

  const start2 = Date.now();
  let done = false;
  while (Date.now() - start2 < 25000) {
    if ((await serverChecked(page, titleC)) === true) { done = true; break; }
    await new Promise((r) => setTimeout(r, 250));
  }
  if (!done) {
    fail("two draining tabs did not converge on exactly one toggle");
  } else {
    console.log("  ✓ two tabs, one intent, one effect");
  }
  await page2.close();

  if (pageErrors.length > 0) fail(`uncaught JS errors: ${pageErrors.join(" | ")}`);
  else console.log("  ✓ zero uncaught JS errors");
} catch (e) {
  fail(e.stack || String(e));
} finally {
  if (browser) await browser.close().catch(() => {});
  if (server) server.kill("SIGKILL");
  rmSync(tmp, { recursive: true, force: true });
}

if (process.exitCode === 1) {
  console.error("\n❌ outbox PoC FAILED");
} else {
  console.log("\n✅ outbox PoC passed (write-ahead, lost-response, two tabs)");
}
