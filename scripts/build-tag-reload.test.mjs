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
//
// REVIVED FROM POCKETBASE ERA. This harness could not boot: it spawned
// `serve --http <addr> --dir <dir>` and then registered its user with
// `superuser upsert` + `/api/collections/_superusers/auth-with-password` +
// `/api/collections/users/records`. The flat binary takes NO CLI subcommands
// (address and data dir ride env vars) and has no PocketBase collections, so
// the process came up on a different port than the one polled and the run died
// with "server did not become healthy" — for as long as the stack has been
// flat, because nothing invoked this file. It is now on the flat boot + owned
// auth path (the `gogogo_auth` cookie), matching the other harnesses.
import { chromium } from "playwright";
import { spawn, execSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const PORT = Number(process.env.SMOKE_PORT || 18094);
const BASE = `http://127.0.0.1:${PORT}`;
const USER_EMAIL = "buildtag-user@local.dev";
const USER_PASS = "BuildtagUserPass!123";

const fail = (msg) => {
  console.error("❌ " + msg);
  process.exitCode = 1;
};

const tmp = mkdtempSync(join(tmpdir(), "gogogo-buildtag-"));
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
  // The flat binary reads these, not argv (see the header note).
  HOST: "127.0.0.1",
  PORT: String(PORT),
  TURSO_DB_PATH: join(runtimeDir, "app.db"),
};
const providedBin = process.env.SMOKE_BIN;
const bin = providedBin ? resolve(providedBin) : join(tmp, "web");

let server = null;
let browser = null;

// signupCookie registers the user and returns the `gogogo_auth` cookie the
// owned auth mints — the same cookie a browser login would carry, so the page
// context authenticates with no token plumbing. Idempotent enough for a rerun
// (a conflict means the user exists, which is all this needs).
async function signupCookie(email, password) {
  const res = await fetch(BASE + "/api/auth/signup/credential", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password, name: "Build Tag" }),
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
    await new Promise((r) => setTimeout(r, 500)); // poll interval
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
  server = spawn(bin, [], {
    stdio: "ignore",
    env: runtimeEnv,
  });
  await waitForHealth();

  const authCookie = await signupCookie(USER_EMAIL, USER_PASS);
  if (!authCookie) throw new Error("signup did not return a gogogo_auth cookie");

  console.log("→ Loading /todo, poisoning the stored build tag…");
  browser = await chromium.launch({ headless: true, args: ["--no-sandbox"] });
  const context = await browser.newContext();
  await context.addCookies([{ name: "gogogo_auth", value: authCookie, url: BASE + "/" }]);

  const pageErrors = [];
  const page = await context.newPage();
  page.on("pageerror", (err) => pageErrors.push(String(err)));

  await page.goto(BASE + "/todo", { waitUntil: "load", timeout: 20000 });
  // Wait for the FIRST build check to have stored the current tag, instead of
  // a 1200ms nap. The check writes the tag into sessionStorage; until it has,
  // poisoning that key would be overwritten by the real value and the test
  // would assert nothing. Polling the stored value is the readiness signal —
  // the same signal the code under test produces, not a proxy for it.
  await page.waitForFunction(() => sessionStorage.getItem("gogogo_build") !== null, null, {
    timeout: 10000,
  });

  // Poison: pretend this tab rendered under an older build.
  await page.evaluate(() => sessionStorage.setItem("gogogo_build", "stale/deadbeef"));

  let navigations = 0;
  page.on("framenavigated", () => {
    navigations += 1;
  });
  // Fire the same trigger a returning tab fires.
  await page.evaluate(() => document.dispatchEvent(new Event("visibilitychange")));
  // Wait for the reload to be OBSERVED rather than for a 4s window to pass: the
  // assertion wants exactly one navigation, so the navigation itself is the
  // event to await. This resolves with the first one and times out if the bug
  // (no reload at all) reproduces — a failure either way, but with a useful
  // message instead of a silent count.
  await page.waitForEvent("framenavigated", { timeout: 15000 }).catch(() => {});
  if (navigations !== 1) {
    fail(`stale tag caused ${navigations} reloads, want exactly 1 (loop or no reload)`);
  } else {
    console.log("  ✓ stale tag reloaded exactly once");
  }
  // Stability: no FURTHER reloads once current. This is an ABSENCE assertion,
  // so a window is the only way to make it — there is no event that means
  // "nothing else happened". Kept as a bounded settle window, and labelled.
  await page.waitForTimeout(3000); // settle window: absence check, no event to await
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
