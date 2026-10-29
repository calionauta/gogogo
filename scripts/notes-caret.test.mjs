// Notes client regression tests — remote-caret geometry under lag, and the
// op outbox when the app is unreachable (a transient 5xx must be kept, not
// dropped as a poison batch).
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
//   Build it with: go build -o "$TMPDIR/gogogo-ci-local-web" ./cmd/web/

import { chromium } from "playwright";
import { spawn } from "node:child_process";
import { mkdtempSync, mkdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

// SMOKE_BIN overrides; otherwise the OS temp dir (per-OS correct via
// tmpdir() — never a hardcoded /tmp, which is wrong on Windows and
// ignores $TMPDIR elsewhere). Matches the Makefile SMOKE_BIN default.
const BIN = process.env.SMOKE_BIN || join(tmpdir(), "gogogo-ci-local-web");

// A leaked browser is not a hypothetical here: this suite runs on a shared
// 8 GB host where an abandoned Chromium tree (~600 MB per renderer) fills
// swap and takes the co-tenant daemons down with it. So the handle lives at
// module scope and is closed in the .finally() below, and the args drop the
// GPU/swiftshader paths that burn CPU without affecting the layout geometry
// these assertions measure. Per-context process isolation is deliberately
// left alone: the two-user tests depend on it.
const BROWSER_ARGS = [
  "--no-sandbox",
  "--disable-dev-shm-usage",
  "--disable-gpu",
  "--disable-software-rasterizer",
  "--disable-extensions",
  "--js-flags=--max-old-space-size=512",
];

let browser;
const PORT = Number(process.env.NOTES_TEST_PORT || 8132);
const BASE = `http://127.0.0.1:${PORT}`;
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
const runtimeDir = join(tmp, "runtime");
mkdirSync(runtimeDir, { recursive: true });
const env = {
  ...process.env,
  // A distinct APP_NAME keeps secrets.Load from decrypting the HOST's
  // production env file (which closes signup) on a developer machine.
  APP_NAME: "gogogo-notes-caret",
  ENVIRONMENT: "development",
  SIGNUP_ENABLED: "true",
  DATA_DIR: runtimeDir,
  DATABASE_PATH: join(runtimeDir, "app.db"),
  NATS_ENABLED: "false",
  DAGNATS_ENABLED: "false",
  GOGOGO_NO_BROWSER: "1",
};
// The flat binary has no CLI subcommands (no `serve`, no `--dir`): the
// listen address and the data dir ride env vars, exactly as smoke.mjs
// boots it.
const server = spawn(BIN, [], {
  stdio: "ignore",
  env: {
    ...env,
    HOST: "127.0.0.1",
    PORT: String(PORT),
    TURSO_DB_PATH: join(runtimeDir, "app.db"),
  },
});

// The owned auth mints the session cookie at signup — the same path
// smoke.mjs drives, and the only one there is (no superuser CLI, no
// records API). Returns the gogogo_auth cookie value, "" on failure.
async function signupCookie(email, password) {
  const res = await fetch(BASE + "/api/auth/signup/credential", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password, name: "Notes Caret Test" }),
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
    const cookie = await signupCookie(email, PASS);
    if (cookie) return cookie;
    await sleep(500); // poll interval: the first signup can race the migration
  }
  throw new Error(`signup failed for ${email}: no session cookie`);
}

async function waitForServer() {
  for (let i = 0; i < 80; i++) {
    try {
      if ((await fetch(BASE + "/health")).ok) return true;
    } catch {}
    await sleep(500); // poll interval: bounded health probe
  }
  return false;
}


// Navigation helper: goto with waitUntil "load" (like smoke.mjs), then wait
// for the textarea explicitly. NEVER "networkidle" here: notes.js opens a
// long-lived SSE EventSource on script eval, and networkidle waits for 500ms
// with zero open connections — an open SSE stream never lets that happen, so
// the goto can time out after 30s under CI load. Same latent bug as the
// whiteboard harness (which flaked 3x before its fix); notes only has 2
// navigations, so it had not flaked yet.
async function gotoNote(pg, url) {
  await pg.goto(url, { waitUntil: "load", timeout: 20000 });
  await pg.locator("#note-text").waitFor({ state: "visible", timeout: 15000 });
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
  let tokenA = "";
  let tokenB = "";
  try {
    tokenA = await signupSession(A_EMAIL);
    tokenB = await signupSession(B_EMAIL);
  } catch (e) {
    bad(e.message || String(e));
    return;
  }

  browser = await chromium.launch({ headless: true, args: BROWSER_ARGS });
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
  await gotoNote(A, `${BASE}/notes/${docID}`);
  await gotoNote(B, `${BASE}/notes/${docID}`);


  // A authors three short lines (no wrapping: line number == visual row).
  await A.locator("#note-text").click();
  await typeText(A, "aaaa\nbbbb\ncccc", 25);
  // Wait for the mirror, not a fixed nap: B's textarea converges.
  await B.waitForFunction(
    () => document.getElementById("note-text").value === "aaaa\nbbbb\ncccc",
    null,
    { timeout: 10000 },
  );
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
  // B must have mirrored X before typing Z: position 10 only means
  // "after bbbb" in the X-inclusive text. Wait for B's mirror (not A's
  // local keystroke, which is instant) — same choreography as the old
  // fixed nap, with no idle tail and no slow-box flake.
  await B.waitForFunction(
    () => document.getElementById("note-text").value.includes("X"),
    null,
    { timeout: 10000 },
  );
  await B.evaluate(() => {
    const ta = document.getElementById("note-text");
    ta.focus();
    ta.setSelectionRange(10, 10);
  });
  await B.keyboard.type("Z");
  // Wait for convergence + first caret render, not a fixed 2500ms: the
  // assertions below need a rendered dot and settled text, whenever
  // the lag actually resolves.
  await A.waitForFunction(
    () => (window.__caretRenders || []).length > 0 &&
      document.getElementById("note-text").value === "aaaaX\nbbbbZ\ncccc",
    null,
    { timeout: 15000 },
  );

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

  // ---------------------------------------------------------------------------
  // Same user, two tabs: the peer must see TWO dots, not one.
  //
  // WHY THIS EXISTS: presence/caret events carry the display name (email),
  // which is identical for two tabs of one account. Keying the caret map by
  // that name merged both tabs under a single dot: each tab's report
  // overwrote the other's, so the peer's caret jumped lines while they
  // typed. The fix keys by the server-stamped per-tab id (msg.from) and
  // renders the name as the label.
  //
  // RED-PROOFED: with name keying, A1's and A2's reports overwrite each
  // other under one key and the observer never counts 2 dots, so the wait
  // below times out and fails.
  // ---------------------------------------------------------------------------
  const doc2 = "notes-sameuser-" + process.pid;
  const A1 = await ctxA.newPage();
  const A2 = await ctxA.newPage(); // SAME context = SAME user as A1
  const B2 = await ctxB.newPage(); // observer: a different user
  A1.on("pageerror", (e) => pageErrors.push("A1: " + String(e)));
  A2.on("pageerror", (e) => pageErrors.push("A2: " + String(e)));
  B2.on("pageerror", (e) => pageErrors.push("B2: " + String(e)));
  await gotoNote(A1, `${BASE}/notes/${doc2}`);
  await gotoNote(B2, `${BASE}/notes/${doc2}`);
  await A1.locator("#note-text").click();
  await typeText(A1, "aaa", 25);
  await B2.waitForFunction(
    () => document.getElementById("note-text").value.includes("aaa"),
    null,
    { timeout: 10000 },
  );
  await gotoNote(A2, `${BASE}/notes/${doc2}`);
  await A2.evaluate(() => {
    const ta = document.getElementById("note-text");
    ta.focus();
    ta.setSelectionRange(ta.value.length, ta.value.length);
  });
  await typeText(A2, "\nbbb", 25);
  await B2.waitForFunction(
    () => document.getElementById("note-text").value === "aaa\nbbb",
    null,
    { timeout: 10000 },
  );
  // A1's caret sits at end of "aaa" (line 1), A2's at end of "bbb"
  // (line 2). The observer must render one dot per TAB.
  let sameUserDots = [];
  try {
    await B2.waitForFunction(
      () => [...document.querySelectorAll("#caret-layer > span")]
        .filter((d) => d.style.width === "3px").length === 2,
      null,
      { timeout: 12000 },
    );
    sameUserDots = await B2.evaluate(() => [...document.querySelectorAll("#caret-layer > span")]
      .filter((d) => d.style.width === "3px").map((d) => d.title));
  } catch {
    sameUserDots = await B2.evaluate(() => [...document.querySelectorAll("#caret-layer > span")]
      .filter((d) => d.style.width === "3px").map((d) => d.title));
    bad(`same-user tabs merged into ${sameUserDots.length} dot(s), want 2 (titles: ${JSON.stringify(sameUserDots)})`);
  }
  if (sameUserDots.length === 2) {
    ok("same-user tabs render as two dots on the peer");
    const hasLine1 = sameUserDots.some((t) => /line 1/.test(t || ""));
    const hasLine2 = sameUserDots.some((t) => /line 2/.test(t || ""));
    if (hasLine1 && hasLine2) ok("each dot sits on its own tab's line (1 and 2)");
    else bad(`dots do not track their tabs' lines: ${JSON.stringify(sameUserDots)}`);
  }

  // ---------------------------------------------------------------------------
  // THE IDLE-TAB BUG: a tab that never touched the textarea must never
  // publish a caret.
  //
  // WHY THIS EXISTS: the 5s heartbeat forced a caret report and read
  // ta.selectionStart unconditionally. An unfocused textarea's selection is
  // not the user's — the browser leaves it at the END of whatever value was
  // last assigned, and this client reassigns ta.value on every peer op. So
  // every heartbeat during the other user's typing republished the idle tab
  // at the current end of the text, and the peer watched that dot glide
  // along while they typed: the reported "demo2's cursor keeps moving inside
  // my demo1 box".
  //
  // RED-PROOFED against the old heartbeat (`caretSent = -1; maybeCaret(true)`
  // with the forced selectionStart read): B posts caret frames here and its
  // dot appears on A.
  // ---------------------------------------------------------------------------
  const doc3 = "notes-idle-tab-" + process.pid;
  const A3 = await ctxA.newPage(); // types
  const B3 = await ctxB.newPage(); // opens the note and NEVER clicks
  A3.on("pageerror", (e) => pageErrors.push("A3: " + String(e)));
  B3.on("pageerror", (e) => pageErrors.push("B3: " + String(e)));

  // Count B3's caret POSTs (it should make none: it never placed a caret)
  // and record every dot A3 renders for B3.
  const idleCarets = [];
  B3.on("request", (req) => {
    if (req.url().includes("/caret")) idleCarets.push(req.postData() || "");
  });
  await gotoNote(A3, `${BASE}/notes/${doc3}`);
  await gotoNote(B3, `${BASE}/notes/${doc3}`);
  await installCaretProbe(A3);

  await A3.locator("#note-text").click();
  // Type across >2 heartbeat windows (5s each) so the old code would have
  // forced at least two idle-tab reports.
  await typeText(A3, "typing a long enough line to cross heartbeats", 120);
  // Let one more heartbeat fire after the last keystroke. KEPT: time is
  // the mechanism here — the bug fired ON the 5s heartbeat tick, so the
  // absence check has to span one full interval with the tab idle. There is
  // no observable state to poll: the assertion is that NOTHING arrives.
  await sleep(6000); // KEPT: absence check across a heartbeat interval

  if (idleCarets.length === 0) {
    ok("an idle tab publishes no caret at all");
  } else {
    bad(`idle tab posted ${idleCarets.length} caret frame(s): ${JSON.stringify(idleCarets.slice(0, 3))}`);
  }
  // And nothing should be painted for it either (it never reported).
  const idleDots = await A3.evaluate(() => [...document.querySelectorAll("#caret-layer > span")]
    .filter((d) => d.style.width === "3px").map((d) => d.title));
  // A3's own caret is never rendered locally, so ANY dot means a peer
  // published one. The only peer is the idle tab.
  if (idleDots.length === 0) {
    ok("no peer dot is painted on the typist for an idle tab");
  } else {
    bad(`an idle tab still painted a dot on the typist: ${JSON.stringify(idleDots)}`);
  }

  // A focused reader's dot must still survive (presence is not lost along
  // with the borrowed-selection bug): B3 focuses and puts the caret on line
  // 1, then idles past a heartbeat.
  await B3.locator("#note-text").click();
  await B3.evaluate(() => {
    const ta = document.getElementById("note-text");
    ta.setSelectionRange(5, 5);
  });
  // selectionchange fires from setSelectionRange; give the throttle a beat.
  await sleep(400);
  try {
    await A3.waitForFunction(
      () => [...document.querySelectorAll("#caret-layer > span")]
        .filter((d) => d.style.width === "3px").length >= 1,
      null,
      { timeout: 12000 },
    );
    ok("a focused reader's caret still reaches the typist");
  } catch {
    bad("a focused reader's caret never reached the typist (presence lost)");
  }

  // ---------------------------------------------------------------------------
  // pagehide announces the leave: a closed tab must not leave a ghost dot
  // waiting out the 8s TTL, and the EventSource race must not swallow it.
  // ---------------------------------------------------------------------------
  const dotsBefore = await A3.evaluate(() => [...document.querySelectorAll("#caret-layer > span")]
    .filter((d) => d.style.width === "3px").length);
  if (dotsBefore < 1) {
    bad("no dot to retire before closing the reader tab (cannot test the leave)");
  } else {
    const gone = await B3.close({ runBeforeUnload: true }).then(() => true).catch(() => false);
    try {
      await A3.waitForFunction(
        () => [...document.querySelectorAll("#caret-layer > span")]
          .filter((d) => d.style.width === "3px").length === 0,
        null,
        { timeout: 3000 },
      );
      ok("closing a tab retires its dot well inside the 8s TTL");
    } catch {
      const left = await A3.evaluate(() => [...document.querySelectorAll("#caret-layer > span")]
        .filter((d) => d.style.width === "3px").map((d) => d.title));
      bad(`the closed tab's dot lingered past 3s (pagehide leave not delivered): ${JSON.stringify(left)} (close ok: ${gone})`);
    }
  }

  // ---------------------------------------------------------------------------
  // THE SOFT-WRAP BOUNDARY: a caret that sits exactly where a line wraps
  // must paint on the line the writer's caret is on (the next one).
  //
  // WHY THIS EXISTS: caretXY used to measure the caret with a zero-width-space
  // marker inside the mirror. "Zero width" is not "no character" — a ZWSP is a
  // break opportunity, so at a wrap boundary the marker stayed on the PREVIOUS
  // line (measured: marker top = 10px, range at the same offset = 32px). Every
  // time the writer's caret landed exactly on a boundary — once per typed line —
  // the peer's dot blinked up a row and came back on the next keystroke, which
  // is the reported "depois de um tempo escrevendo, o cursor pisca em outro
  // lugar". The fix measures with a Range, which inserts nothing.
  //
  // The expectation below is computed independently, in the observing tab, with
  // a Range over a mirror that holds the SAME text: the row of an offset is the
  // browser's own wrap of that text.
  //
  // RED-PROOFED: with the marker measurement this fails at every boundary
  // offset (the dot paints one line up); with the Range it passes.
  // ---------------------------------------------------------------------------
  const doc4 = "notes-wrap-boundary-" + process.pid;
  const A4 = await ctxA.newPage();
  const B4 = await ctxB.newPage();
  A4.on("pageerror", (e) => pageErrors.push("A4: " + String(e)));
  B4.on("pageerror", (e) => pageErrors.push("B4: " + String(e)));
  await gotoNote(A4, `${BASE}/notes/${doc4}`);
  await gotoNote(B4, `${BASE}/notes/${doc4}`);
  await A4.locator("#note-text").click();
  const wrapText = ("the quick brown fox jumps over the lazy dog and keeps running through the field ".repeat(3)).trim();
  await typeText(A4, wrapText, 8);
  await B4.waitForFunction(
    (t) => document.getElementById("note-text").value === t,
    wrapText,
    { timeout: 15000 },
  );
  const wrapped = await B4.evaluate((text) => {
    const ta = document.getElementById("note-text");
    const cs = getComputedStyle(ta);
    const mirror = document.createElement("div");
    for (const p of ["fontFamily", "fontSize", "fontWeight", "lineHeight", "letterSpacing", "textTransform", "textIndent", "paddingTop", "paddingRight", "paddingBottom", "paddingLeft", "boxSizing", "whiteSpace", "wordWrap", "overflowWrap", "wordBreak", "tabSize"]) {
      try { mirror.style[p] = cs[p]; } catch (e) { /* unsupported prop */ }
    }
    mirror.style.whiteSpace = "pre-wrap";
    mirror.style.wordWrap = "break-word";
    mirror.style.boxSizing = "border-box";
    mirror.style.border = "0";
    mirror.style.position = "absolute";
    mirror.style.visibility = "hidden";
    mirror.style.top = "0";
    mirror.style.left = "0";
    mirror.style.width = ta.clientWidth + "px";
    mirror.textContent = text;
    document.getElementById("note-wrap").appendChild(mirror);
    const origin = mirror.getBoundingClientRect();
    const lineH = parseFloat(cs.lineHeight);
    const node = mirror.firstChild;
    const rows = [];
    for (let at = 0; at <= text.length; at++) {
      const range = document.createRange();
      range.setStart(node, at);
      range.collapse(true);
      const rects = range.getClientRects();
      const rect = rects.length ? rects[rects.length - 1] : range.getBoundingClientRect();
      rows.push(Math.round((rect.top - origin.top) / lineH));
    }
    mirror.remove();
    const boundaries = [];
    for (let i = 1; i < rows.length; i++) if (rows[i] !== rows[i - 1]) boundaries.push(i);
    return { rows, lineH, boundaries };
  }, wrapText);
  if (wrapped.boundaries.length === 0) {
    bad("the wrap-boundary case never wrapped (text too short for this viewport)");
  } else {
    // Every boundary offset plus its two neighbours: the exact landing spot and
    // the keystrokes immediately around it.
    let worst = 0;
    for (const at of wrapped.boundaries.slice(0, 4)) {
      for (const off of [at - 1, at, at + 1]) {
        await A4.evaluate((o) => {
          const ta = document.getElementById("note-text");
          ta.focus();
          ta.setSelectionRange(o, o);
        }, off);
        const want = Math.round(wrapped.rows[off] * wrapped.lineH);
        let got = null;
        const deadline = Date.now() + 5000;
        while (Date.now() < deadline) {
          got = await B4.evaluate(() => {
            const ta = document.getElementById("note-text");
            const padTop = parseFloat(getComputedStyle(ta).paddingTop) || 0;
            const dots = [...document.querySelectorAll("#caret-layer > span")]
              .filter((d) => d.style.width === "3px");
            if (!dots.length) return null;
            const r = dots[dots.length - 1].getBoundingClientRect();
            const tr = ta.getBoundingClientRect();
            return Math.round(r.top - tr.top - padTop + ta.scrollTop);
          });
          if (got !== null && Math.abs(got - want) <= 4) break;
          await sleep(120);
        }
        if (got === null || Math.abs(got - want) > 4) {
          worst = worst || (got === null ? 999 : Math.abs(got - want));
          bad(`caret at offset ${off} (wrapped row ${wrapped.rows[off]}) painted at y=${got}, want y≈${want}`);
        }
      }
    }
    if (!worst) ok("wrapped carets paint on the line the writer's caret is on");
  }

  // ---------------------------------------------------------------------------
  // OFFLINE / UNREACHABLE APP: an op batch answered by a transient status
  // (5xx, 429, 408) must be KEPT and retried, never treated as a poison op.
  //
  // WHY THIS EXISTS: notes.js drops any status that is not 200/409/401/403 as
  // "poison". A gateway with the origin down answers 521/522 (Cloudflare), a
  // dead upstream 502, an overloaded edge 503 — none of them is a verdict on
  // the ops, and the drop silently ate the user's typing while the app was
  // merely unreachable. The Service Worker's todo queue already treats the
  // same statuses as transient; the two must not disagree.
  //
  // RED-PROOFED: with the drop branch the outbox is empty after the failed
  // send and the fresh-context read below never sees the tail.
  // ---------------------------------------------------------------------------
  const docOffline = "notes-offline-" + process.pid;
  // Close every earlier page first. Each note page holds ONE long-lived SSE
  // stream, and this server is HTTP/1.1: six of them saturate the per-origin
  // connection pool, after which any fetch hangs forever (no request reaches
  // the server, no error either). The earlier phases are finished, so their
  // streams are only stealing sockets from this one.
  for (const pg of await ctxA.pages()) await pg.close();
  for (const pg of await ctxB.pages()) await pg.close();
  const A5 = await ctxA.newPage();
  A5.on("pageerror", (e) => pageErrors.push("A5: " + String(e)));
  await gotoNote(A5, `${BASE}/notes/${docOffline}`);
  await A5.locator("#note-text").click();
  await typeText(A5, "base line", 10);
  const outKey = "notes-outbox:" + docOffline;
  // Wait for the round trip, not a nap: #rtt-pill only shows a duration once
  // the server answered the batch (setPending swaps "saving…" for the last
  // RTT on success). Polling the outbox alone would pass before the 100ms
  // debounce even queued the first batch.
  try {
    await A5.waitForFunction(
      () => (document.getElementById("rtt-pill")?.textContent || "").includes("ms"),
      null,
      { timeout: 15000 },
    );
  } catch {
    bad("the baseline note text never reached the server (cannot test the transient path)");
  }
  try {
    await A5.waitForFunction(
      (key) => !JSON.parse(localStorage.getItem(key) || "[]").length,
      outKey,
      { timeout: 5000 },
    );
  } catch {
    bad("the baseline batch stayed in the outbox after its round trip");
  }
  await A5.evaluate((doc) => {
    const orig = window.fetch;
    window.__notesEdgeDown = true;
    window.fetch = function (input, init) {
      const url = typeof input === "string" ? input : input.url;
      if (window.__notesEdgeDown && url.indexOf("/api/notes/" + encodeURIComponent(doc) + "/op") !== -1) {
        return Promise.resolve(new Response("origin down", { status: 521 }));
      }
      return orig.apply(window, arguments);
    };
  }, docOffline);
  const tail = " tail-survives";
  await typeText(A5, tail, 10);
  // Wait for the FAILED send, not a nap: the transient branch reveals
  // #net-status ("reconnecting…") when the 521 comes back and the batch is
  // kept. That is the exact moment the assertions below describe.
  try {
    await A5.waitForFunction(
      () => !document.getElementById("net-status").classList.contains("hidden"),
      null,
      { timeout: 15000 },
    );
  } catch {
    bad("the transient failure never surfaced (no reconnecting hint)");
  }
  const kept = await A5.evaluate((key) => ({
    text: document.getElementById("note-text").value,
    outbox: JSON.parse(localStorage.getItem(key) || "[]").length,
    net: !document.getElementById("net-status").classList.contains("hidden"),
  }), outKey);
  if (kept.text === "base line" + tail) {
    ok("typing survives a transient status in the open editor");
  } else {
    bad(`typing was lost locally on a transient status: ${JSON.stringify(kept.text)}`);
  }
  if (kept.outbox > 0) {
    ok("a transient status keeps the op batch queued for retry");
  } else {
    bad("a transient status dropped the op batch (nothing left to retry)");
  }
  if (kept.net) {
    ok("the editor says it is reconnecting during a transient failure");
  } else {
    bad("no reconnecting hint while the ops could not be sent");
  }
  // The app answers again: the retained batch must flush on its own (no
  // synthetic online event — the outage never dropped the link).
  await A5.evaluate(() => { window.__notesEdgeDown = false; });
  try {
    await A5.waitForFunction(
      (key) => !JSON.parse(localStorage.getItem(key) || "[]").length,
      outKey,
      { timeout: 15000 },
    );
    ok("the retained batch drains once the app answers again");
  } catch {
    const diag = await A5.evaluate((key) => ({
      outbox: localStorage.getItem(key),
      net: document.getElementById("net-status").className,
      busy: document.getElementById("note-text").getAttribute("aria-busy"),
      value: document.getElementById("note-text").value,
      online: navigator.onLine,
    }), outKey);
    bad(`the retained batch never drained after the app recovered: ${JSON.stringify(diag)}`);
  }
  // Read it from a FRESH context (no localStorage draft to fall back on): only
  // the server having the text proves the batch was applied.
  const ctxFresh = await browser.newContext();
  await ctxFresh.addCookies([{ name: "gogogo_auth", value: tokenA, url: BASE }]);
  const F = await ctxFresh.newPage();
  await gotoNote(F, `${BASE}/notes/${docOffline}`);
  const serverText = await F.locator("#note-text").inputValue();
  if (serverText === "base line" + tail) {
    ok("the server has the text typed while the app was unreachable");
  } else {
    bad(`the server never received the offline tail: ${JSON.stringify(serverText)}`);
  }
  await ctxFresh.close();

  if (pageErrors.length) {
    bad(`uncaught page errors: ${pageErrors.slice(0, 3).join(" | ")}`);
  } else {
    ok("no uncaught page errors");
  }

  await browser.close();
}

main()
  .catch((e) => bad("unexpected error: " + (e.stack || e.message)))
  .finally(async () => {
    // Close the browser FIRST and unconditionally: this is the only place a
    // failed run gets here (the happy path calls browser.close() itself), so
    // skipping it leaks the whole Chromium process tree onto the shared host.
    await browser?.close().catch(() => {});
    server.kill("SIGKILL");
    rmSync(tmp, { recursive: true, force: true });
    console.log(
      failures === 0
        ? "\n\u2705 notes client tests passed"
        : `\n\u274c notes client tests failed (${failures})`,
    );
  });
