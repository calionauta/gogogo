// SCOPE:core - Behavioral test: doc-version watcher is reactive, not polling.
//
// Boots the real binary, signs a user up (the cookie is the session),
// loads /todo, and proves the resync
// wiring executes without timers: an init script records every
// setInterval registration, so the old 250ms watchDocVersion poll would
// FAIL this test and the data-effect subscription passes. Console and
// page errors fail the run (a broken data-effect expression surfaces as
// a Datastar runtime error, not a silent no-op).
//
// Run: node scripts/todo-docwatch.test.mjs (SMOKE_BIN reuses a build,
// SMOKE_PORT overrides the port).
import { chromium } from "playwright";
import { spawn, execSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const PORT = Number(process.env.SMOKE_PORT || 18091);
const BASE = `http://127.0.0.1:${PORT}`;
const USER_EMAIL = "docwatch-user@local.dev";
const USER_PASS = "DocwatchUserPass!123";

const fail = (msg) => {
  console.error("❌ " + msg);
  process.exitCode = 1;
};

const tmp = mkdtempSync(join(tmpdir(), "gogogo-docwatch-"));
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

// The owned auth mints the session cookie at signup — the same path
// smoke.mjs drives, and the only one there is (no superuser CLI, no
// records API). Returns the gogogo_auth cookie value, "" on failure.
async function signupCookie(email, password) {
  const res = await fetch(BASE + "/api/auth/signup/credential", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password, name: "Docwatch Test" }),
  });
  if (!res.ok) return "";
  const setCookies = res.headers.getSetCookie
    ? res.headers.getSetCookie()
    : [res.headers.get("set-cookie")];
  const line = (setCookies || []).find((c) => c && c.startsWith("gogogo_auth="));
  if (!line) return "";
  return line.split(";")[0].slice("gogogo_auth=".length);
}

// The first signup can race the migration, so retry like smoke.mjs does.
async function signupSession(email) {
  for (let i = 0; i < 10; i++) {
    const cookie = await signupCookie(email, USER_PASS);
    if (cookie) return cookie;
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error(`signup failed for ${email}: no session cookie`);
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
  // The flat binary has no CLI subcommands (no `serve`, no `--dir`): the
  // listen address and the data dir ride env vars, exactly as smoke.mjs
  // boots it.
  server = spawn(bin, [], {
    stdio: "ignore",
    env: {
      ...runtimeEnv,
      HOST: "127.0.0.1",
      PORT: String(PORT),
      TURSO_DB_PATH: join(runtimeDir, "app.db"),
    },
  });
  await waitForHealth();

  const cookie = await signupSession(USER_EMAIL);

  console.log("→ Loading /todo with interval recorder…");
  browser = await chromium.launch({ headless: true, args: ["--no-sandbox"] });
  const context = await browser.newContext();
  await context.addCookies([{ name: "gogogo_auth", value: cookie, url: BASE + "/" }]);
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
  console.error("\n❌ docwatch FAILED");
} else {
  console.log("\n✅ docwatch passed (reactive watcher, no polling, stream live)");
}
