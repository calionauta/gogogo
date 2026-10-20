// sse-reopen.test.mjs — the SSE opener self-heals instead of dying silent.
//
// Covers the verified-open contract (see web/resources/static/sse-open.js):
// with Datastar unable to load, the opener must fail LOUDLY (bounded,
// surfaced) rather than leave a dead stream nobody notices. On the old
// opener the same scenario dies silent (300ms fallback clicks into the
// void and poisons dataset.opened), so this test FAILS there (red-proof
// by git stash) and passes here.
//
// Usage: SMOKE_BIN=<binary> node scripts/sse-reopen.test.mjs
// (CI builds the binary once; scripts/smoke.mjs documents the pattern.)
import { chromium } from "playwright";
import { spawn } from "node:child_process";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const providedBin = process.env.SMOKE_BIN;
const bin = providedBin ? resolve(providedBin) : join(tmpdir(), "sse-reopen-web");
const PORT = Number(process.env.SSE_PROBE_PORT || 8133);
const BASE = `http://127.0.0.1:${PORT}`;

let server = null;
let browser = null;

async function signupCookie(email) {
  const res = await fetch(BASE + "/api/auth/signup/credential", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password: "sse-probe-pass-123", name: "SSE Probe" }),
  });
  if (!res.ok) throw new Error(`signup failed: ${res.status} ${await res.text()}`);
  const setCookies = res.headers.getSetCookie
    ? res.headers.getSetCookie()
    : [res.headers.get("set-cookie")];
  const line = (setCookies || []).find((c) => c && c.startsWith("gogogo_auth="));
  if (!line) throw new Error("signup response carried no session cookie");
  // Same extraction scripts/smoke.mjs uses: strip attributes first.
  return line.split(";")[0].slice("gogogo_auth=".length);
}


try {
  if (!providedBin) {
    console.log("→ Building binary (./cmd/web)…");
    const { execSync } = await import("node:child_process");
    execSync(`go build -o ${JSON.stringify(bin)} ./cmd/web`, { stdio: "inherit", timeout: 240000 });
  } else {
    console.log(`→ Using prebuilt binary ${bin}…`);
  }
  const runtimeDir = mkdtempSync(join(tmpdir(), "gogogo-sse-probe-"));
  server = spawn(bin, [], {
    stdio: "ignore",
    env: {
      ...process.env,
      GOGOGO_NO_BROWSER: "1",
      HOST: "127.0.0.1",
      PORT: String(PORT),
      TURSO_DB_PATH: join(runtimeDir, "app.db"),
      GOAKT_ENABLED: "false",
    },
  });
  for (let i = 0; i < 60; i++) {
    try {
      const r = await fetch(BASE + "/health");
      if ((await r.text()).trim() === "ok") break;
    } catch {}
    await new Promise((r) => setTimeout(r, 500));
  }

  browser = await chromium.launch({ headless: true, args: ["--no-sandbox"] });

  // Case 1 — Datastar can never load: the opener must fail LOUDLY
  // (bounded openState=failed + no stream request), never silent-dead.
  {
    const context = await browser.newContext();
    const token = await signupCookie(`sse-probe-${Date.now()}-a@local.dev`);
    await context.addCookies([{ name: "gogogo_auth", value: token, url: BASE + "/" }]);
    const page = await context.newPage();
    await page.route("**/static/datastar.js", (route) => route.abort());
    let streamSeen = false;
    page.on("request", (req) => {
      if (req.url().includes("/api/todos/stream")) streamSeen = true;
    });
    await page.goto(BASE + "/todo", { waitUntil: "load", timeout: 20000 });
    try {
      await page.waitForFunction(() => window.__gogogoSSEOpenState === "failed", null, { timeout: 40000 });
    } catch {
      throw new Error("opener stayed silent with Datastar unloadable (expected loud openState=failed)");
    }
    if (streamSeen) throw new Error("stream opened without Datastar — unexpected");
    console.log("  ✓ unloadable Datastar fails loud (no silent-dead stream)");
    await context.close();
  }

  // Case 2 — control: normal load registers the stream promptly, and the
  // session banner costs the BROWSER one connection, not one per tab.
  //
  // WHY THIS EXISTS: a per-tab session EventSource halved the HTTP/1.1
  // budget (6 per host). Three tabs x (feature stream + session stream)
  // filled it and the next POST never left the browser — every request
  // hung, not just the new stream. Web Locks elects one visible tab to
  // hold the session stream; the others hear it over a BroadcastChannel.
  {
    const context = await browser.newContext();
    const token = await signupCookie(`sse-probe-${Date.now()}-b@local.dev`);
    await context.addCookies([{ name: "gogogo_auth", value: token, url: BASE + "/" }]);
    let sessionStreams = 0;
    let sessionProbes = 0;
    context.on("request", (req) => {
      const u = req.url();
      if (u.includes("/api/session/stream")) sessionStreams++;
      else if (u.includes("/api/session")) sessionProbes++;
    });
    const page = await context.newPage();
    await page.goto(BASE + "/todo", { waitUntil: "load", timeout: 20000 });
    try {
      await page.waitForFunction(() => window.__gogogoSSEOpenState === "open", null, { timeout: 20000 });
    } catch {
      throw new Error("stream never registered on a normal load");
    }
    console.log("  ✓ normal load registers the stream (verified open)");

    const extra = [];
    for (let i = 0; i < 2; i++) {
      const p = await context.newPage();
      await p.goto(BASE + "/todo", { waitUntil: "load", timeout: 20000 });
      extra.push(p);
    }
    await extra[1].evaluate(
      () => Promise.race([
        fetch("/health").then((r) => r.status),
        new Promise((_, reject) => setTimeout(() => reject(new Error("request queued")), 4000)),
      ]),
    );
    if (sessionStreams > 2) {
      throw new Error(`3 tabs opened ${sessionStreams} session streams (want one, pooled per browser)`);
    }
    if (sessionProbes > 0) {
      throw new Error(`session banner still probes /api/session (${sessionProbes} requests)`);
    }
    console.log(`  ✓ one session stream for 3 tabs (${sessionStreams} request(s)), no poll, requests still flow`);
    await context.close();
  }

  // Case 3 — helper itself unloadable: the declarative opener must not
  // throw ExecuteExpression (helper undefined); the fallback waits for it
  // bounded and then fails loud with the helper message instead.
  {
    const context = await browser.newContext();
    const token = await signupCookie(`sse-probe-${Date.now()}-c@local.dev`);
    await context.addCookies([{ name: "gogogo_auth", value: token, url: BASE + "/" }]);
    const page = await context.newPage();
    const errors = [];
    page.on("pageerror", (err) => errors.push(String(err && err.message || err)));
    await page.route("**/static/sse-open.js", (route) => route.abort());
    await page.goto(BASE + "/todo", { waitUntil: "load", timeout: 20000 });
    try {
      await page.waitForFunction(
        () => window.__gogogoSSEOpenState === "failed" || document.querySelector("#todo-list") !== null,
        null, { timeout: 1000 },
      );
    } catch {}
    await new Promise((r) => setTimeout(r, 16000));
    if (errors.some((m) => m.includes("ExecuteExpression") || m.includes("is not a function"))) {
      throw new Error("declarative opener threw with helper absent (expected guarded no-op + loud fallback failure)");
    }
    if (!errors.some((m) => m.includes("sse-open.js never loaded"))) {
      throw new Error("fallback did not fail loud on missing helper (errors seen: " + JSON.stringify(errors) + ")");
    }
    console.log("  ✓ helper-unloadable fails loud with the helper message, never ExecuteExpression");
    await context.close();
  }

  console.log("✅ SSE reopen verification passed");
} finally {
  if (browser) await browser.close().catch(() => {});
  if (server) server.kill();
}
