// SCOPE:core - Behavioral PoC: doc-version watcher is reactive, not polling.
//
// Boots the real binary, logs in, loads /todo, and proves the resync
// wiring executes without timers: an init script records every
// setInterval registration, so the old 250ms watchDocVersion poll would
// FAIL this test and the data-effect subscription passes. Console and
// page errors fail the run (a broken data-effect expression surfaces as
// a Datastar runtime error, not a silent no-op).
//
// Run: node scripts/todo-docwatch.test.mjs (SMOKE_BIN reuses a build,
// SMOKE_PORT overrides the port).
import { chromium } from "playwright";
import { spawn, spawnSync, execSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const PORT = Number(process.env.SMOKE_PORT || 18091);
const BASE = `http://127.0.0.1:${PORT}`;
const SU_EMAIL = "docwatch-superuser@local.dev";
const SU_PASS = "DocwatchSuperuserPass!123";
const USER_EMAIL = "docwatch-user@local.dev";
const USER_PASS = "DocwatchUserPass!123";

const fail = (msg) => {
  console.error("❌ " + msg);
  process.exitCode = 1;
};

const tmp = mkdtempSync(join(tmpdir(), "gogogo-docwatch-"));
const pbDir = join(tmp, "pb");
const runtimeDir = join(tmp, "runtime");
mkdirSync(runtimeDir, { recursive: true });
const runtimeEnv = {
  ...process.env,
  DATA_DIR: runtimeDir,
  DATABASE_PATH: join(runtimeDir, "app.db"),
  NATS_ENABLED: "false",
  DAGNATS_ENABLED: "false",
  OFFLINE_SYNC_ENABLED: "true",
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

  console.log(`→ Starting server on ${BASE}…`);
  server = spawn(bin, ["serve", "--http", `127.0.0.1:${PORT}`, "--dir", pbDir], {
    stdio: "ignore",
    env: runtimeEnv,
  });
  await waitForHealth();

  const upsert = spawnSync(bin, ["superuser", "upsert", "--dir", pbDir, SU_EMAIL, SU_PASS], {
    encoding: "utf8",
    env: runtimeEnv,
  });
  if (upsert.status !== 0) throw new Error("superuser upsert failed");
  const su = await api("POST", "/api/collections/_superusers/auth-with-password", {
    body: { identity: SU_EMAIL, password: SU_PASS },
  });
  const suToken = su.json?.token;
  if (!suToken) throw new Error("superuser auth failed");
  await api("POST", "/api/collections/users/records", {
    token: suToken,
    body: { email: USER_EMAIL, password: USER_PASS, passwordConfirm: USER_PASS },
  });
  const auth = await api("POST", "/api/collections/users/auth-with-password", {
    body: { identity: USER_EMAIL, password: USER_PASS },
  });
  if (!auth.json?.token) throw new Error("user auth failed");

  console.log("→ Loading /todo with interval recorder…");
  browser = await chromium.launch({ headless: true, args: ["--no-sandbox"] });
  const context = await browser.newContext();
  await context.addCookies([{ name: "gogogo_auth", value: auth.json.token, url: BASE + "/" }]);
  // Record every setInterval registration BEFORE page scripts run. The old
  // watcher registered setInterval(watchDocVersion, 250); the reactive one
  // registers none — so this assertion fails on the old markup by construction.
  await context.addInitScript(() => {
    window.__intervals = [];
    const orig = window.setInterval.bind(window);
    window.setInterval = (fn, ms, ...args) => {
      window.__intervals.push({ ms, src: String(fn).slice(0, 120) });
      return orig(fn, ms, ...args);
    };
  });

  const pageErrors = [];
  const consoleErrors = [];
  const page = await context.newPage();
  page.on("pageerror", (err) => pageErrors.push(String(err)));
  page.on("console", (msg) => {
    if (msg.type() === "error") consoleErrors.push(msg.text());
  });
  let streamOpened = false;
  page.on("response", (res) => {
    if (res.url().includes("/api/todos/stream") && res.status() === 200) streamOpened = true;
  });

  await page.goto(BASE + "/todo", { waitUntil: "load", timeout: 20000 });
  await page.waitForTimeout(1500);

  const hasEffect = await page.evaluate(() => Boolean(document.querySelector("[data-effect]")));
  if (!hasEffect) fail("no [data-effect] watcher element on /todo");
  else console.log("  ✓ reactive watcher element present");

  const polls = await page.evaluate(() =>
    (window.__intervals || []).filter((t) => /watchDocVersion|docVersion/i.test(t.src)),
  );
  if (polls.length > 0) fail(`polling watcher still registered: ${JSON.stringify(polls)}`);
  else console.log("  ✓ no docVersion polling interval registered");

  if (!streamOpened) fail("todo SSE stream never opened");
  else console.log("  ✓ todo SSE stream opened");

  if (pageErrors.length > 0) fail(`uncaught JS errors: ${pageErrors.join(" | ")}`);
  else console.log("  ✓ zero uncaught JS errors");
  for (const ce of consoleErrors) console.log(`      console.error: ${ce}`);

  await context.close();
} catch (e) {
  fail(e.stack || String(e));
} finally {
  if (browser) await browser.close().catch(() => {});
  if (server) server.kill("SIGKILL");
  rmSync(tmp, { recursive: true, force: true });
}

if (process.exitCode === 1) {
  console.error("\n❌ docwatch PoC FAILED");
} else {
  console.log("\n✅ docwatch PoC passed (reactive watcher, no polling, stream live)");
}
