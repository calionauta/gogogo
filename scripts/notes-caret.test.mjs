// Notes remote-caret regression tests — cross-viewport positioning under lag.
//
// WHY THIS EXISTS: the notes client draws every peer caret with a mirror-div
// (see caretXY in features/notes/static/notes.js). The mirror must be measured
// against the SAME text the reporter measured their offset against. The client
// used to measure against its own ta.value, which is stale for a beat while a
// peer's op is still in flight: a peer whose caret sat at the END of line 2
// (offset N) landed on the START of line 3 in a copy one character behind —
// the reported "the cursor blinks at the start of the line below, then snaps
// back". The fix stores the reporter's text snapshot with the caret and refuses
// to place a dot whose snapshot cannot justify its reported line.
//
// These tests drive the REAL pages in a real browser (two contexts = two
// authenticated users) and assert on the RENDERED dot geometry, not internal
// state — the client is an IIFE with no exported state, and adding one for a
// test would weaken the code it protects.
//
// The line assertion is RED-PROOFED: against the old measurement basis this
// test fails (a dot titled "line 2" paints at line 3's y).
//
// Run: node scripts/notes-caret.test.mjs
//   Needs a prebuilt binary, like scripts/smoke.mjs: SMOKE_BIN=... node ...
//   Build it with: go build -o /tmp/gogogo-ci-local-web ./cmd/web/

import { chromium } from "playwright";
import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const BIN = process.env.SMOKE_BIN || "/tmp/gogogo-ci-local-web";
const PORT = Number(process.env.NOTES_TEST_PORT || 8132);
const BASE = `http://127.0.0.1:${PORT}`;
const SU_EMAIL = "notes-test-su@local.dev";
const SU_PASS = "NotesTestSuperPass!123";
const A_EMAIL = "notes-test-a@local.dev";
const B_EMAIL = "notes-test-b@local.dev";
const PASS = "NotesTestUserPass!123";

let failures = 0;
const ok = (msg) => console.log("  \u2713 " + msg);
const bad = (msg) => {
  console.error("  \u2717 " + msg);
  failures++;
  process.exitCode = 1;
};
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const tmp = mkdtempSync(join(tmpdir(), "notes-caret-test-"));
const pbDir = join(tmp, "pb");
const runtimeDir = join(tmp, "runtime");
mkdirSync(runtimeDir, { recursive: true });
const env = {
  ...process.env,
  DATA_DIR: runtimeDir,
  DATABASE_PATH: join(runtimeDir, "app.db"),
  NATS_ENABLED: "false",
  DAGNATS_ENABLED: "false",
  GOGOGO_NO_BROWSER: "1",
};
const server = spawn(BIN, ["serve", "--http", `127.0.0.1:${PORT}`, "--dir", pbDir], {
  env,
  stdio: "ignore",
});

async function api(method, path, { token, body } = {}) {
  const res = await fetch(BASE + path, {
    method,
    headers: { "Content-Type": "application/json", ...(token ? { Authorization: token } : {}) },
    body: body ? JSON.stringify(body) : undefined,
  });
  let json = null;
  try {
    json = await res.json();
  } catch {}
  return { status: res.status, json };
}

async function waitForServer() {
  for (let i = 0; i < 80; i++) {
    try {
      if ((await fetch(BASE + "/health")).ok) return true;
    } catch {}
    await sleep(500);
  }
  return false;
}

async function userToken(suToken, email) {
  for (let i = 0; i < 10; i++) {
    const r = await api("POST", "/api/collections/users/records", {
      token: suToken,
      body: { email, password: PASS, passwordConfirm: PASS },
    });
    if (r.status === 200 || r.status === 400) break;
    await sleep(400);
  }
  const a = await api("POST", "/api/collections/users/auth-with-password", {
    body: { identity: email, password: PASS },
  });
  if (!a.json?.token) throw new Error(`auth failed for ${email}: ${JSON.stringify(a.json)}`);
  return a.json.token;
}

// Records every caret-layer render: each dot's title (carries the reported
// line) and its y offset relative to the textarea, plus the textarea metrics
// needed to invert that y back into a line number.
async function installCaretProbe(page) {
  await page.evaluate(() => {
    window.__caretRenders = [];
    const layer = document.getElementById("caret-layer");
    const ta = document.getElementById("note-text");
    const rec = () => {
      const cs = getComputedStyle(ta);
      const lineH = parseFloat(cs.lineHeight) || 0;
      const padTop = parseFloat(cs.paddingTop) || 0;
      const tr = ta.getBoundingClientRect();
      for (const dot of [...layer.querySelectorAll("span")]) {
        if (dot.style.width !== "3px") continue; // dots only, not name flags
        const r = dot.getBoundingClientRect();
        window.__caretRenders.push({
          title: dot.title,
          topCss: Math.round(r.top - tr.top),
          padTop: padTop,
          lineH: lineH,
        });
      }
    };
    new MutationObserver(rec).observe(layer, { childList: true });
    rec();
  });
}

async function typeText(page, text, perCharMs) {
  for (const ch of text) {
    await page.keyboard.type(ch);
    await sleep(perCharMs);
  }
}

async function main() {
  if (!(await waitForServer())) {
    bad("server never came up");
    return;
  }
  const upsert = spawnSync(BIN, ["superuser", "upsert", "--dir", pbDir, SU_EMAIL, SU_PASS], {
    encoding: "utf8",
    env,
  });
  if (upsert.status !== 0) {
    bad("superuser upsert failed: " + (upsert.stderr || upsert.stdout));
    return;
  }
  const su = await api("POST", "/api/collections/_superusers/auth-with-password", {
    body: { identity: SU_EMAIL, password: SU_PASS },
  });
  if (!su.json?.token) {
    bad("superuser auth failed");
    return;
  }
  const tokenA = await userToken(su.json.token, A_EMAIL);
  const tokenB = await userToken(su.json.token, B_EMAIL);

  const browser = await chromium.launch({ headless: true, args: ["--no-sandbox"] });
  const ctxA = await browser.newContext();
  const ctxB = await browser.newContext();
  await ctxA.addCookies([{ name: "gogogo_auth", value: tokenA, url: BASE }]);
  await ctxB.addCookies([{ name: "gogogo_auth", value: tokenB, url: BASE }]);
  const A = await ctxA.newPage();
  const B = await ctxB.newPage();
  const pageErrors = [];
  A.on("pageerror", (e) => pageErrors.push("A: " + String(e)));
  B.on("pageerror", (e) => pageErrors.push("B: " + String(e)));

  const docID = "notes-caret-" + process.pid;
  await A.goto(`${BASE}/notes/${docID}`, { waitUntil: "domcontentloaded" });
  await B.goto(`${BASE}/notes/${docID}`, { waitUntil: "domcontentloaded" });
  await sleep(1000);

  // A authors three short lines (no wrapping: line number == visual row).
  await A.locator("#note-text").click();
  await typeText(A, "aaaa\nbbbb\ncccc", 25);
  await sleep(1200);
  const bSees = await B.evaluate(() => document.getElementById("note-text").value);
  if (bSees === "aaaa\nbbbb\ncccc") {
    ok("initial text mirrored to the peer");
  } else {
    bad(`peer did not mirror the initial text: ${JSON.stringify(bSees)}`);
  }

  await installCaretProbe(A);

  // Concurrent typing: A types (so its adoption of B's op is deferred inside
  // the focused-typist window), then B types on the line above, so B's caret
  // report lands while A's copy is a beat behind. Old code painted B's
  // end-of-line-2 caret on line 3; the fix paints it on line 2.
  await A.evaluate(() => {
    const ta = document.getElementById("note-text");
    ta.focus();
    ta.setSelectionRange(4, 4);
  });
  await A.keyboard.type("X");
  await sleep(150);
  await B.evaluate(() => {
    const ta = document.getElementById("note-text");
    ta.focus();
    ta.setSelectionRange(10, 10);
  });
  await B.keyboard.type("Z");
  await sleep(2500);

  const renders = await A.evaluate(() => window.__caretRenders);
  if (!renders.length) {
    bad("no caret dot was ever rendered on the observing page");
  } else {
    ok(`peer caret rendered ${renders.length} time(s) under lag`);
  }

  // INVARIANT: a dot titled "line N" must paint on line N. The title is the
  // reporter's exact, dimension-independent line; the y is what the user sees.
  let wrong = null;
  for (const r of renders) {
    const m = /line (\d+)/.exec(r.title || "");
    if (!m || !r.lineH) continue;
    const paintedLine = Math.round((r.topCss - r.padTop) / r.lineH) + 1;
    if (paintedLine !== Number(m[1])) {
      wrong = { ...r, reportedLine: Number(m[1]), paintedLine };
      break;
    }
  }
  if (wrong) {
    bad(
      `a caret titled "line ${wrong.reportedLine}" painted on line ${wrong.paintedLine} ` +
        `(top=${wrong.topCss}px) — the dot jumped a line`,
    );
  } else {
    ok("every rendered dot painted on the line it reported");
  }

  // The observing tab's text must CONVERGE to the peer's after the pause —
  // a deferred adopt that is never retried left it stale forever.
  const bText = await B.evaluate(() => document.getElementById("note-text").value);
  const aText = await A.evaluate(() => document.getElementById("note-text").value);
  if (aText === bText && aText === "aaaaX\nbbbbZ\ncccc") {
    ok(`texts converged (${JSON.stringify(aText)})`);
  } else {
    bad(`texts did not converge: A=${JSON.stringify(aText)} B=${JSON.stringify(bText)}`);
  }

  const last = renders[renders.length - 1];
  if (last && /line 2/.test(last.title) && last.lineH) {
    const paintedLine = Math.round((last.topCss - last.padTop) / last.lineH) + 1;
    if (paintedLine === 2) ok("final caret sits on the peer's real line (2)");
    else bad(`final caret painted on line ${paintedLine}, want 2`);
  }

  if (pageErrors.length) {
    bad(`uncaught page errors: ${pageErrors.slice(0, 3).join(" | ")}`);
  } else {
    ok("no uncaught page errors");
  }

  await browser.close();
}

main()
  .catch((e) => bad("unexpected error: " + (e.stack || e.message)))
  .finally(() => {
    server.kill("SIGKILL");
    rmSync(tmp, { recursive: true, force: true });
    console.log(
      failures === 0
        ? "\n\u2705 notes caret tests passed"
        : `\n\u274c notes caret tests failed (${failures})`,
    );
  });
