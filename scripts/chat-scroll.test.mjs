// SCOPE:core - Behavioral test: a new AI answer scrolls itself into view.
//
// Boots the real binary with the keyless simulated LLM, signs a user up
// (the cookie IS the session), and drives BOTH AI legs through their real
// forms on a deliberately short viewport so the newest answer is
// off-screen when it lands. It then asserts the page scrolled — which is
// the whole user-visible contract of the fix.
//
// Measured while writing this: the harness first looked for a
// `data-scroll` attribute (none exists — there is no such Datastar
// plugin) and then for a vanilla `addEventListener("datastar-patch-elements")`
// detector (which would have been a second scroll mechanism racing the
// real one). Both were removed. The assertion is now the observable
// effect — scrollY strictly increased — so the mechanism stays an
// implementation detail and the test cannot pass by watching a hook that
// does nothing.
//
// Run: node scripts/chat-scroll.test.mjs (SMOKE_BIN reuses a build,
// SMOKE_PORT overrides the port, SMOKE_TRACE writes a Playwright trace).
import { chromium } from "playwright";
import { spawn, execSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const PORT = Number(process.env.SMOKE_PORT || 18093);
const BASE = `http://127.0.0.1:${PORT}`;
const USER_EMAIL = "chat-scroll@local.dev";
const USER_PASS = "ChatScrollPass!123";

const fail = (msg) => {
  console.error("❌ " + msg);
  process.exitCode = 1;
};

const tmp = mkdtempSync(join(tmpdir(), "gogogo-chatscroll-"));
const runtimeDir = join(tmp, "runtime");
mkdirSync(runtimeDir, { recursive: true });
const runtimeEnv = {
  ...process.env,
  DATA_DIR: runtimeDir,
  DATABASE_PATH: join(runtimeDir, "app.db"),
  NATS_ENABLED: "false",
  DAGNATS_ENABLED: "false",
  // The keyless showcase: deterministic answers, no provider key. This is
  // what makes the test hermetic — the "model" is the same binary.
  SIMULATE_LLM: "true",
  GOGOGO_NO_BROWSER: "1",
};
const providedBin = process.env.SMOKE_BIN;
const bin = providedBin ? resolve(providedBin) : join(tmp, "web");

let server = null;
let browser = null;
let context = null;
// Module scope, not function scope: the finally below stops the trace on
// the failure path too, and a `const` inside try is invisible there
// (measured — the happy path crashed on this reference after passing).
let tracePath = "";

async function signupCookie(email, password) {
  const res = await fetch(BASE + "/api/auth/signup/credential", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password, name: "Chat Scroll" }),
  });
  if (!res.ok) return "";
  const setCookies = res.headers.getSetCookie
    ? res.headers.getSetCookie()
    : [res.headers.get("set-cookie")];
  const line = (setCookies || []).find((c) => c && c.startsWith("gogogo_auth="));
  if (!line) return "";
  return line.split(";")[0].slice("gogogo_auth=".length);
}

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
      /* not up yet */
    }
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error("server did not become healthy in " + timeoutMs + "ms");
}

// scrollTop reads the scroll position after a smooth scroll settles.
// scrollIntoView({behavior:'smooth'}) is asynchronous, so polling for a
// MOVED position (not a fixed nap) is the only non-flaky read.
async function scrolledFrom(page, before, label, timeoutMs = 8000) {
  try {
    await page.waitForFunction(
      (prev) => window.scrollY > prev && window.scrollY > 0,
      before,
      { timeout: timeoutMs, polling: 100 },
    );
    return await page.evaluate(() => window.scrollY);
  } catch {
    const now = await page.evaluate(() => window.scrollY);
    throw new Error(`${label}: page did not scroll (scrollY ${before} → ${now})`);
  }
}

// fillAndSubmit types into a composer and submits it. Both AI composers
// are inside a Datastar data-on:submit__prevent form, so a native
// form.submit() bypasses Datastar — the Enter key / real click is used
// instead, exactly as a user does it.
async function fillAndSubmit(page, selector, text) {
  const input = page.locator(selector);
  await input.waitFor({ state: "visible", timeout: 15000 });
  await input.fill(text);
  await input.press("Enter");
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
    env: {
      ...runtimeEnv,
      HOST: "127.0.0.1",
      PORT: String(PORT),
      TURSO_DB_PATH: join(runtimeDir, "app.db"),
    },
  });
  await waitForHealth();

  const cookie = await signupSession(USER_EMAIL);

  browser = await chromium.launch({ headless: true, args: ["--no-sandbox"] });
  context = await browser.newContext({
    // Short enough that the first answer already overflows the fold: the
    // bug being fixed is "the newest answer lands off-screen".
    viewport: { width: 900, height: 420 },
  });
  await context.addCookies([{ name: "gogogo_auth", value: cookie, url: BASE + "/" }]);
  tracePath = process.env.SMOKE_TRACE || "";
  if (tracePath) await context.tracing.start({ screenshots: true, snapshots: true });

  const pageErrors = [];
  const page = await context.newPage();
  page.on("pageerror", (err) => pageErrors.push(String(err)));

  // ---- Chat leg -----------------------------------------------------
  console.log("→ Chat: sending a message on a 420px viewport…");
  await page.goto(BASE + "/chat", { waitUntil: "load", timeout: 20000 });
  await page.waitForResponse((res) => res.url().includes("/api/chat/stream"), { timeout: 15000 });
  const chatBefore = await page.evaluate(() => window.scrollY);
  await fillAndSubmit(page, "#chat-prompt", "plan my week");
  // The answer is a REAL round-trip (queue → worker → SSE append), so wait
  // for it to be in the DOM before judging the scroll: the anchor rides
  // that same patch.
  await page.waitForFunction(
    () => document.querySelectorAll("#chat-messages .chat-start").length > 0,
    null,
    { timeout: 25000 },
  );
  const chatAfter = await scrolledFrom(page, chatBefore, "chat answer");
  console.log(`  ✓ chat scrolled to the answer (scrollY ${chatBefore} → ${chatAfter})`);

  // ---- Ask leg ------------------------------------------------------
  console.log("→ Ask AI: asking on the same viewport…");
  await page.goto(BASE + "/genui", { waitUntil: "load", timeout: 20000 });
  await page.waitForResponse((res) => res.url().includes("/api/genui/stream"), { timeout: 15000 });
  // The empty state must NOT carry the anchor: loading the page must not
  // jump. Pinned here (not only in the unit test) because it is a
  // page-level regression a render test cannot see.
  const askLoadY = await page.evaluate(() => window.scrollY);
  if (askLoadY !== 0) fail(`Ask AI page scrolled to ${askLoadY} on load with no answer`);
  else console.log("  ✓ Ask AI page did not jump on load");

  const askBefore = await page.evaluate(() => window.scrollY);
  await fillAndSubmit(page, "#genui-prompt", "plan my day");
  await page.waitForFunction(
    () => {
      const r = document.getElementById("genui-result");
      return Boolean(r && r.querySelector(".card, table, [role=img], .genui-html"));
    },
    null,
    { timeout: 25000 },
  );
  const askAfter = await scrolledFrom(page, askBefore, "ask answer");
  console.log(`  ✓ ask scrolled to the answer (scrollY ${askBefore} → ${askAfter})`);

  if (pageErrors.length > 0) fail(`uncaught JS errors: ${pageErrors.join(" | ")}`);
  else console.log("  ✓ zero uncaught JS errors");

  if (tracePath) {
    await context.tracing.stop({ path: tracePath });
    console.log(`  ⏱ trace saved: ${tracePath}`);
  }
} catch (e) {
  fail(e.stack || String(e));
} finally {
  if (tracePath) {
    try {
      await context.tracing.stop({ path: tracePath });
    } catch {
      /* already stopped on the happy path */
    }
  }
  if (browser) await browser.close().catch(() => {});
  if (server) server.kill("SIGKILL");
  rmSync(tmp, { recursive: true, force: true });
}

if (process.exitCode === 1) {
  console.error("\n❌ chat scroll test FAILED");
} else {
  console.log("\n✅ chat scroll test passed (both AI legs scroll to the newest answer)");
}
