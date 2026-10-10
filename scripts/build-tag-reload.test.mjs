// SCOPE:core - Behavioral proof: stale-tab reload fires EXACTLY once.
//
// A build-tag mismatch must reload the tab once (fresh DOM from the new
// server), then stay stable. The first implementation stored the tag only
// on first sight and reloaded without storing — so the fresh page saw the
// same mismatch and reloaded forever (lived bug: endless refresh loop
// after every deploy). This test poisons sessionStorage with a stale tag,
// triggers the check, and counts navigations: exactly 1, then silence.
// Against the old code it observes 3+ reloads and fails.
//
// Run: node scripts/build-tag-reload.test.mjs (SMOKE_BIN reuses a build,
// SMOKE_PORT overrides the port).
import { chromium } from "playwright";
import { spawn, spawnSync, execSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const PORT = Number(process.env.SMOKE_PORT || 18094);
const BASE = `http://127.0.0.1:${PORT}`;
const SU_EMAIL = "buildtag-superuser@local.dev";
const SU_PASS = "BuildtagSuperuserPass!123";
const USER_EMAIL = "buildtag-user@local.dev";
const USER_PASS = "BuildtagUserPass!123";

const fail = (msg) => {
  console.error("❌ " + msg);
  process.exitCode = 1;
};

const tmp = mkdtempSync(join(tmpdir(), "gogogo-buildtag-"));
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
  if (!su.json?.token) throw new Error("superuser auth failed");
  await api("POST", "/api/collections/users/records", {
    token: su.json.token,
    body: { email: USER_EMAIL, password: USER_PASS, passwordConfirm: USER_PASS },
  });
  const auth = await api("POST", "/api/collections/users/auth-with-password", {
    body: { identity: USER_EMAIL, password: USER_PASS },
  });
  if (!auth.json?.token) throw new Error("user auth failed");

  console.log("→ Loading /todo, poisoning the stored build tag…");
  browser = await chromium.launch({ headless: true, args: ["--no-sandbox"] });
  const context = await browser.newContext();
  await context.addCookies([{ name: "gogogo_auth", value: auth.json.token, url: BASE + "/" }]);

  const pageErrors = [];
  const page = await context.newPage();
  page.on("pageerror", (err) => pageErrors.push(String(err)));

  await page.goto(BASE + "/todo", { waitUntil: "load", timeout: 20000 });
  await page.waitForTimeout(1200); // first check stores the real tag

  // Poison: pretend this tab rendered under an older build.
  await page.evaluate(() => sessionStorage.setItem("gogogo_build", "stale/deadbeef"));

  let navigations = 0;
  page.on("framenavigated", () => {
    navigations += 1;
  });
  // Fire the same trigger a returning tab fires.
  await page.evaluate(() => document.dispatchEvent(new Event("visibilitychange")));
  await page.waitForTimeout(4000);
  if (navigations !== 1) {
    fail(`stale tag caused ${navigations} reloads, want exactly 1 (loop or no reload)`);
  } else {
    console.log("  ✓ stale tag reloaded exactly once");
  }
  // Stability: no further reloads once current.
  await page.waitForTimeout(3000);
  if (navigations !== 1) {
    fail(`tab kept reloading after settling (${navigations} total) — refresh loop`);
  } else {
    console.log("  ✓ tab stable after settling (no refresh loop)");
  }

  if (pageErrors.length > 0) fail(`uncaught JS errors: ${pageErrors.join(" | ")}`);
  else console.log("  ✓ zero uncaught JS errors");

  await context.close();
} catch (e) {
  fail(e.stack || String(e));
} finally {
  if (browser) await browser.close().catch(() => {});
  if (server) server.kill("SIGKILL");
  rmSync(tmp, { recursive: true, force: true });
}

if (process.exitCode === 1) {
  console.error("\n❌ build-tag reload PoC FAILED");
} else {
  console.log("\n✅ build-tag reload PoC passed (one reload, then stable)");
}
