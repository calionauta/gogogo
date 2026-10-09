// Whiteboard client regression tests — response ordering and conflict handling.
//
// WHY THIS EXISTS: the Go side of the whiteboard has 15 tests, but the CLIENT
// had none — no test runner, no *.test.js, and scripts/smoke.mjs only loads the
// /whiteboard route without ever drawing on it. That blind spot is exactly where
// a real regression lived: adopting the server's whole shape list from a POST
// response (`shapes = body.shapes`) drops a shape the user just drew, because
// concurrent POST responses are NOT ordered relative to each other.
//
// These tests drive the REAL page in a real browser and assert on canvas PIXELS
// in specific REGIONS — the same thing the user sees. Not on internal state: the
// client is an IIFE with no exported state, and adding an export purely for a
// test would weaken the code it is meant to protect.
//
// Both tests are RED-PROOFED: with the old behaviour reintroduced they fail.
// That matters here because the first version of this file passed against the
// broken code — it delayed the REQUEST rather than the RESPONSE, so the "late"
// response already carried the newer state and proved nothing. It also asserted
// only "some ink exists", which a whole-list replacement still satisfies. Both
// mistakes are fixed below; see the comments on each mechanism.
//
// Run: node scripts/whiteboard-client.test.mjs
//   Needs a prebuilt binary, like scripts/smoke.mjs: SMOKE_BIN=... node ...
//   Build it with: go build -o /tmp/gogogo-ci-local-web ./cmd/web/

import { chromium } from "playwright";
import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const BIN = process.env.SMOKE_BIN || "/tmp/gogogo-ci-local-web";
const PORT = Number(process.env.WB_TEST_PORT || 8131);
const BASE = `http://127.0.0.1:${PORT}`;
const SU_EMAIL = "wb-test-su@local.dev";
const SU_PASS = "WbTestSuperPass!123";
const USER_EMAIL = "wb-test-user@local.dev";
const USER_PASS = "WbTestUserPass!123";

let failures = 0;
const ok = (msg) => console.log("  \u2713 " + msg);
const bad = (msg) => {
  console.error("  \u2717 " + msg);
  failures++;
  process.exitCode = 1;
};
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const tmp = mkdtempSync(join(tmpdir(), "wb-client-test-"));
const pbDir = join(tmp, "pb");
const runtimeDir = join(tmp, "runtime");
mkdirSync(runtimeDir, { recursive: true });
const runtimeEnv = {
  ...process.env,
  DATA_DIR: runtimeDir,
  DATABASE_PATH: join(runtimeDir, "app.db"),
  NATS_ENABLED: "false",
  DAGNATS_ENABLED: "false",
  GOGOGO_NO_BROWSER: "1",
};

const server = spawn(BIN, ["serve", "--http", `127.0.0.1:${PORT}`, "--dir", pbDir], {
  env: runtimeEnv,
  stdio: "ignore",
});

async function api(method, path, { token, body } = {}) {
  const res = await fetch(BASE + path, {
    method,
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: token } : {}),
    },
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
      const r = await fetch(BASE + "/health");
      if (r.ok) return true;
    } catch {}
    await sleep(500);
  }
  return false;
}

// Counts non-blank pixels inside a CSS-pixel region of the canvas.
//
// A REGION, not the whole canvas, is what makes these assertions decisive: a
// whole-list replacement leaves one shape painted, so "total ink > 0" cannot
// tell a correct merge from a destructive replace. Asking "is shape 1 still
// painted where I drew it?" can.
const inkInRegion = (x, y, w, h) => `(() => {
  const c = document.getElementById("wb-canvas");
  if (!c) return -1;
  const rect = c.getBoundingClientRect();
  const scale = c.width / Math.max(1, rect.width); // device pixels per CSS px
  const g = c.getContext("2d");
  const X = Math.max(0, Math.floor(${x} * scale));
  const Y = Math.max(0, Math.floor(${y} * scale));
  const W = Math.min(c.width - X, Math.ceil(${w} * scale));
  const H = Math.min(c.height - Y, Math.ceil(${h} * scale));
  if (W <= 0 || H <= 0) return -1;
  const d = g.getImageData(X, Y, W, H).data;
  let n = 0;
  for (let i = 3; i < d.length; i += 4) if (d[i] > 8) n++;
  return n;
})()`;

// Draws a rectangle by dragging on the canvas, in CSS pixels relative to it.
async function drawRect(page, x1, y1, x2, y2) {
  const box = await page.locator("#wb-canvas").boundingBox();
  await page.mouse.move(box.x + x1, box.y + y1);
  await page.mouse.down();
  await page.mouse.move(box.x + x2, box.y + y2, { steps: 6 });
  await page.mouse.up();
}

// Region around the first shape drawn in test 1 (CSS px), generously padded so
// anti-aliased rough.js strokes are inside it.
const FIRST = { x: 20, y: 20, w: 110, h: 100 };
const SECOND = { x: 150, y: 130, w: 120, h: 100 };

async function main() {
  if (!(await waitForServer())) {
    bad("server never came up");
    return;
  }

  // Create the account through the same path smoke.mjs uses: superuser CLI, then
  // the API. Driving the login FORM is flakier and would test auth, not the
  // whiteboard.
  const upsert = spawnSync(BIN, ["superuser", "upsert", "--dir", pbDir, SU_EMAIL, SU_PASS], {
    encoding: "utf8",
    env: runtimeEnv,
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

  for (let i = 0; i < 10; i++) {
    const r = await api("POST", "/api/collections/users/records", {
      token: su.json.token,
      body: { email: USER_EMAIL, password: USER_PASS, passwordConfirm: USER_PASS },
    });
    if (r.status === 200 || r.status === 400) break; // 400 = already exists
    await sleep(500);
  }
  const auth = await api("POST", "/api/collections/users/auth-with-password", {
    body: { identity: USER_EMAIL, password: USER_PASS },
  });
  if (!auth.json?.token) {
    bad("user auth failed");
    return;
  }

  const browser = await chromium.launch({ headless: true, args: ["--no-sandbox"] });
  const context = await browser.newContext();
  await context.addCookies([{ name: "gogogo_auth", value: auth.json.token, url: BASE + "/" }]);
  const page = await context.newPage();

  const pageErrors = [];
  page.on("pageerror", (e) => pageErrors.push(String(e)));

  const docID = "wb-order-" + process.pid;
  await page.goto(`${BASE}/whiteboard/${docID}`, { waitUntil: "networkidle" });
  await sleep(600); // let the SSE stream connect and the canvas size settle

  // ---------------------------------------------------------------------------
  // 1. A late response for an EARLIER op must not drop a later shape.
  //
  // The mechanism must delay the RESPONSE, not the request. Delaying the request
  // (an earlier version of this test) lets the server process op 1 after op 2, so
  // op 1's payload already contains both shapes and the test proves nothing —
  // it passed against the broken code. `route.fetch()` sends the request
  // immediately (so the server's answer really is the older, single-shape state)
  // and we then hold that captured response back past the second one.
  //
  // Timeline: draw X -> POST1 answered [X], held 1200ms; draw Z -> POST2 answered
  // [X,Z], delivered at once. Then POST1's stale [X] lands. Old code
  // (`shapes = body.shapes`) makes X reappear alone; the fix copies only the
  // version, so both stay.
  // ---------------------------------------------------------------------------
  let updateCount = 0;
  await page.route("**/api/whiteboard/**/update*", async (route) => {
    updateCount++;
    if (updateCount === 1) {
      const response = await route.fetch(); // request goes now; answer is [X]
      await sleep(1200); // ...but deliver it AFTER op 2's response
      await route.fulfill({ response });
      return;
    }
    await route.continue();
  });

  await drawRect(page, 30, 30, 110, 100); // shape 1
  await sleep(150);
  await drawRect(page, 160, 140, 250, 210); // shape 2, while op 1 is held
  await sleep(2600); // past the held response plus render time

  const firstInk = await page.evaluate(inkInRegion(FIRST.x, FIRST.y, FIRST.w, FIRST.h));
  const secondInk = await page.evaluate(inkInRegion(SECOND.x, SECOND.y, SECOND.w, SECOND.h));
  if (firstInk > 0 && secondInk > 0) {
    ok(`both shapes painted after a stale response (ink ${firstInk} / ${secondInk})`);
  } else {
    bad(
      `a shape was dropped by the stale response (shape1 ink=${firstInk}, shape2 ink=${secondInk})`
    );
  }

  // The server is the authority; it must hold both regardless of the client.
  const snapCount = await page.evaluate(async (doc) => {
    const r = await fetch(`/api/whiteboard/${doc}/snapshot`, { credentials: "same-origin" });
    if (!r.ok) return null;
    const j = await r.json();
    return Array.isArray(j) ? j.length : -1;
  }, docID);
  if (snapCount === 2) {
    ok("server persisted both shapes (2)");
  } else {
    bad(`server has ${snapCount} shapes, want 2`);
  }

  await page.unroute("**/api/whiteboard/**/update*");

  // ---------------------------------------------------------------------------
  // 2. A 409 must correct only the CONFLICTING shape, not blank the canvas.
  //
  // Answer 409 with a list containing ONLY the conflicted shape, exactly as the
  // server does. Then assert the EARLIER shapes are still painted where they
  // were. "Some ink exists" is not enough: the old whole-list replacement leaves
  // the single conflicting shape painted, so only a region check catches it.
  // ---------------------------------------------------------------------------
  await page.route("**/api/whiteboard/**/update*", async (route) => {
    let id = "unknown";
    try {
      id = JSON.parse(route.request().postData() || "{}").shape?.id ?? id;
    } catch {}
    await route.fulfill({
      status: 409,
      contentType: "application/json",
      body: JSON.stringify({
        ok: false,
        error: "stale",
        shapes: [{ id, type: "rect", x: 400, y: 400, w: 40, h: 40, color: "#111", version: 9 }],
      }),
    });
  });

  await drawRect(page, 300, 300, 360, 360);
  await sleep(1500);

  const firstAfter409 = await page.evaluate(inkInRegion(FIRST.x, FIRST.y, FIRST.w, FIRST.h));
  const secondAfter409 = await page.evaluate(inkInRegion(SECOND.x, SECOND.y, SECOND.w, SECOND.h));
  if (firstAfter409 > 0 && secondAfter409 > 0) {
    ok(
      `earlier shapes survived the 409 (ink ${firstAfter409} / ${secondAfter409}) — only the conflicting one was corrected`
    );
  } else {
    bad(
      `a 409 wiped the other shapes (shape1 ink=${firstAfter409}, shape2 ink=${secondAfter409}) — the whole list was replaced`
    );
  }

  // ---------------------------------------------------------------------------
  // 3. A "count" broadcast (someone joins/leaves) must NOT move remote cursors.
  //
  // The server re-broadcasts the authoritative peer set on every join and
  // leave. The client used to rebuild its peer map from scratch, seeding each
  // peer at the canvas CENTER — so every remote cursor jumped to the middle the
  // instant anyone came or went, with nobody moving. Known positions must be
  // carried over; only peers never seen start at center.
  //
  // RED-PROOFED: with the whole-map rebuild, `after` becomes the seeded 50%
  // and both assertions below fail.
  // ---------------------------------------------------------------------------
  const doc3 = "wb-count-" + process.pid;
  const makeCtx = async (label) => {
    const c = await browser.newContext();
    await c.addCookies([{ name: "gogogo_auth", value: auth.json.token, url: BASE + "/" }]);
    const p = await c.newPage();
    p.on("pageerror", (e) => pageErrors.push(`${label}: ${String(e)}`));
    return p;
  };
  const pageA = await makeCtx("A");
  const pageB = await makeCtx("B");
  await pageA.goto(`${BASE}/whiteboard/${doc3}?clientID=wb-alpha`, { waitUntil: "networkidle" });
  await pageB.goto(`${BASE}/whiteboard/${doc3}?clientID=wb-bravo`, { waitUntil: "networkidle" });
  await sleep(900);
  // B moves its pointer to a known off-center spot (canvas-relative CSS px).
  const box3 = await pageB.locator("#wb-canvas").boundingBox();
  await pageB.mouse.move(box3.x + box3.width * 0.2, box3.y + box3.height * 0.3);
  await sleep(700);

  // Cursor events are re-stamped by the SERVER with the authenticated email
  // (anti-spoof), so the rendered badge carries USER_EMAIL, not the clientID.
  const readCursor = (pg) =>
    pg.evaluate((who) => {
      const el = [...document.querySelectorAll("#cursors > div")].find(
        (d) => (d.querySelector(".badge")?.textContent || "") === who,
      );
      return el ? { left: el.style.left, top: el.style.top } : null;
    }, USER_EMAIL);

  const before = await readCursor(pageA);
  const wrapW = await pageA.evaluate(
    () => document.getElementById("canvas-wrap").getBoundingClientRect().width,
  );

  // A third client connects -> the server broadcasts a fresh "count" to all.
  const pageC = await makeCtx("C");
  await pageC.goto(`${BASE}/whiteboard/${doc3}?clientID=wb-charlie`, { waitUntil: "networkidle" });
  await sleep(900);

  const after = await readCursor(pageA);
  if (before && after && before.left === after.left && before.top === after.top) {
    ok(`remote cursor held its position across a peer join (left ${after.left})`);
  } else {
    bad(`a peer join moved a remote cursor: before=${JSON.stringify(before)} after=${JSON.stringify(after)}`);
  }
  // And it must sit where B actually pointed (~20% width), not the canvas
  // center a naive roster rebuild would seed it with.
  const leftPct = after && wrapW ? parseFloat(after.left) / wrapW : -1;
  if (Math.abs(leftPct - 0.2) < 0.08) {
    ok(`cursor stayed where the peer pointed (left ${(leftPct * 100).toFixed(0)}%)`);
  } else {
    bad(`cursor did not land on the peer's spot (left=${after && after.left}, wrap=${wrapW})`);
  }

  // ---------------------------------------------------------------------------
  // 4. An IN-PROGRESS shape must be visible to peers before the pointer is
  //    released (the live "draft" channel). Without it a peer sees only the
  //    cursor moving and the whole shape pops in on pointer-up.
  //
  // RED-PROOFED: with the draft relay removed the peer's canvas has ZERO ink
  // in the region while the draw is still held, so the first assertion fails.
  // ---------------------------------------------------------------------------
  const doc4 = "wb-draft-" + process.pid;
  const drawA = await makeCtx("D-A");
  const drawB = await makeCtx("D-B");
  await drawA.goto(`${BASE}/whiteboard/${doc4}?clientID=wb-live-a`, { waitUntil: "networkidle" });
  await drawB.goto(`${BASE}/whiteboard/${doc4}?clientID=wb-live-b`, { waitUntil: "networkidle" });
  await sleep(900);

  // Region where the held rectangle is being drawn (CSS px, canvas-relative).
  const DRAFT_REGION = { x: 50, y: 50, w: 180, h: 160 };
  const beforeInk = await drawA.evaluate(inkInRegion(DRAFT_REGION.x, DRAFT_REGION.y, DRAFT_REGION.w, DRAFT_REGION.h));
  if (beforeInk === 0) ok("peer canvas starts empty in the draw region");
  else bad(`region was not empty before drawing (ink ${beforeInk})`);

  const bbox = await drawB.locator("#wb-canvas").boundingBox();
  await drawB.mouse.move(bbox.x + 60, bbox.y + 60);
  await drawB.mouse.down();
  await drawB.mouse.move(bbox.x + 200, bbox.y + 180, { steps: 10 });
  await sleep(600); // still HOLDING — let draft frames propagate

  const midInk = await drawA.evaluate(inkInRegion(DRAFT_REGION.x, DRAFT_REGION.y, DRAFT_REGION.w, DRAFT_REGION.h));
  if (midInk > 0) {
    ok(`peer sees the shape while it is still being drawn (ink ${midInk})`);
  } else {
    bad("peer saw nothing until the pointer was released (no draft relay)");
  }

  await drawB.mouse.up();
  await sleep(900);
  const afterInk = await drawA.evaluate(inkInRegion(DRAFT_REGION.x, DRAFT_REGION.y, DRAFT_REGION.w, DRAFT_REGION.h));
  if (afterInk > 0) ok(`committed shape still painted after release (ink ${afterInk})`);
  else bad(`shape vanished after release (ink ${afterInk})`);

  // ---------------------------------------------------------------------------
  // 5. While a shape is HELD mid-draw, the peer's cursor dot must sit on the
  //    live tip. The fused frame carries cursor and shape from the same
  //    pointer event, so they cannot skew apart the way two throttles at
  //    different rates did (dot trailing mid-edge while the corner moved on).
  //
  // RED-PROOFED: with the cursor dropped from the draft frame the dot freezes
  // at its pre-draw position while the shape grows, so the assertion fails.
  // ---------------------------------------------------------------------------
  const doc5 = "wb-tip-" + process.pid;
  const tipA = await makeCtx("T-A");
  const tipB = await makeCtx("T-B");
  await tipA.goto(`${BASE}/whiteboard/${doc5}?clientID=wb-tip-a`, { waitUntil: "networkidle" });
  await tipB.goto(`${BASE}/whiteboard/${doc5}?clientID=wb-tip-b`, { waitUntil: "networkidle" });
  await sleep(900);

  const tipBox = await tipB.locator("#wb-canvas").boundingBox();
  const TIP = { x: 220, y: 180 }; // canvas-relative CSS px: the held corner
  await tipB.mouse.move(tipBox.x + 60, tipBox.y + 60);
  await sleep(300);
  await tipB.mouse.down();
  await tipB.mouse.move(tipBox.x + TIP.x, tipBox.y + TIP.y, { steps: 10 });
  await sleep(800); // still HOLDING — trailing fused frames flushed

  const dot5 = await readCursor(tipA);
  const wrap5 = await tipA.evaluate(() => {
    const r = document.getElementById("canvas-wrap").getBoundingClientRect();
    return { w: r.width, h: r.height };
  });
  const expX = TIP.x / tipBox.width;
  const expY = TIP.y / tipBox.height;
  const gotX = dot5 ? parseFloat(dot5.left) / wrap5.w : -1;
  const gotY = dot5 ? parseFloat(dot5.top) / wrap5.h : -1;
  if (dot5 && Math.abs(gotX - expX) < 0.02 && Math.abs(gotY - expY) < 0.02) {
    ok(`peer's dot rides the live tip while held (dot ${(gotX * 100).toFixed(1)}%,${(gotY * 100).toFixed(1)}% vs tip ${(expX * 100).toFixed(1)}%,${(expY * 100).toFixed(1)}%)`);
  } else {
    bad(`peer's dot is off the live tip: dot=${JSON.stringify(dot5)} tip=${expX.toFixed(3)},${expY.toFixed(3)}`);
  }
  await tipB.mouse.up();

  // ---------------------------------------------------------------------------
  // 6. A retried (stale) frame must not drag the dot back in time.
  //
  // Abort the first fused frame mid-drag: the client retries it ~300ms later
  // with its ORIGINAL capture time, while newer frames keep flowing. The
  // receiver's cts guard must drop the late retry — the dot stays at the live
  // tip instead of jumping back to the aborted position.
  //
  // RED-PROOFED: with the cts guard removed the stale retry applies last and
  // the dot ends at the aborted position, so the assertion fails.
  // ---------------------------------------------------------------------------
  const doc6 = "wb-stale-" + process.pid;
  const stA = await makeCtx("S-A");
  const stB = await makeCtx("S-B");
  await stA.goto(`${BASE}/whiteboard/${doc6}?clientID=wb-stale-a`, { waitUntil: "networkidle" });
  await stB.goto(`${BASE}/whiteboard/${doc6}?clientID=wb-stale-b`, { waitUntil: "networkidle" });
  await sleep(900);

  let aborted = 0;
  await stB.route("**/api/whiteboard/**/presence*", async (route) => {
    const body = route.request().postData() || "";
    if (aborted === 0 && body.includes('"draft"')) {
      aborted++;
      await route.abort(); // client retries ~300ms later with original cts
      return;
    }
    await route.continue();
  });

  const stBox = await stB.locator("#wb-canvas").boundingBox();
  const T2 = { x: 240, y: 200 }; // final held corner (canvas-relative CSS px)
  await stB.mouse.move(stBox.x + 60, stBox.y + 60);
  await stB.mouse.down();
  await stB.mouse.move(stBox.x + 120, stBox.y + 100, { steps: 5 });
  await sleep(150);
  await stB.mouse.move(stBox.x + T2.x, stBox.y + T2.y, { steps: 5 });
  await sleep(1200); // HOLDING: fresh T2 frames plus the stale retry landed

  if (aborted === 0) {
    bad("no draft frame was aborted — the stale-retry path was never exercised");
  } else {
    ok("one draft frame aborted mid-drag (retry path exercised)");
  }
  const dot6 = await readCursor(stA);
  const wrap6 = await stA.evaluate(() => {
    const r = document.getElementById("canvas-wrap").getBoundingClientRect();
    return { w: r.width, h: r.height };
  });
  const exp6X = T2.x / stBox.width;
  const exp6Y = T2.y / stBox.height;
  const got6X = dot6 ? parseFloat(dot6.left) / wrap6.w : -1;
  const got6Y = dot6 ? parseFloat(dot6.top) / wrap6.h : -1;
  if (dot6 && Math.abs(got6X - exp6X) < 0.02 && Math.abs(got6Y - exp6Y) < 0.02) {
    ok(`stale retry did not move the dot back (dot ${(got6X * 100).toFixed(1)}%,${(got6Y * 100).toFixed(1)}% vs tip ${(exp6X * 100).toFixed(1)}%,${(exp6Y * 100).toFixed(1)}%)`);
  } else {
    bad(`stale retry dragged the dot back: dot=${JSON.stringify(dot6)} tip=${exp6X.toFixed(3)},${exp6Y.toFixed(3)}`);
  }
  await stB.mouse.up();
  await stB.unroute("**/api/whiteboard/**/presence*");

  // ---------------------------------------------------------------------------
  // 7. A stale draft-end must not wipe a newer live draft.
  //
  // B draws a real rect and HOLDS STILL (quiescent: trailing frames flushed,
  // no motion, so nothing new is in flight). A hand-crafted draft-end with an
  // ANCIENT cts is then injected through B's own session — the server
  // re-stamps it to B's user, the same key as the live draft. The cts guard
  // must drop it and the live draft stays painted. Without the guard the end
  // deletes the draft and, with nobody moving, nothing repaints it.
  //
  // RED-PROOFED: with the guard removed from draft-end the injected end wipes
  // the live shape, so the ink assertion fails.
  // ---------------------------------------------------------------------------
  const doc7 = "wb-end-" + process.pid;
  const endA = await makeCtx("E-A");
  const endB = await makeCtx("E-B");
  await endA.goto(`${BASE}/whiteboard/${doc7}?clientID=wb-end-a`, { waitUntil: "networkidle" });
  await endB.goto(`${BASE}/whiteboard/${doc7}?clientID=wb-end-b`, { waitUntil: "networkidle" });
  await sleep(900);

  const END_REGION = { x: 50, y: 50, w: 190, h: 150 };
  const endBox = await endB.locator("#wb-canvas").boundingBox();
  await endB.mouse.move(endBox.x + 60, endBox.y + 60);
  await endB.mouse.down();
  await endB.mouse.move(endBox.x + 220, endBox.y + 180, { steps: 10 });
  await sleep(1000); // HOLDING STILL — quiescent, trailing frames flushed

  const heldInk = await endA.evaluate(inkInRegion(END_REGION.x, END_REGION.y, END_REGION.w, END_REGION.h));
  if (heldInk === 0) {
    bad("setup failed: no live draft painted before the stale-end injection");
  } else {
    ok(`live draft held on peer canvas (ink ${heldInk})`);
    await endB.evaluate(async ({ doc, clientID }) => {
      await fetch(`/api/whiteboard/${doc}/presence?clientID=${clientID}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify({ type: "draft-end", doc, user: "anyone", cts: 1 }),
      });
    }, { doc: doc7, clientID: "wb-end-b" });
    await sleep(600); // an unguarded end would have wiped the draft by now
    const keptInk = await endA.evaluate(inkInRegion(END_REGION.x, END_REGION.y, END_REGION.w, END_REGION.h));
    if (keptInk > 0) {
      ok(`stale draft-end did not wipe the live draft (ink ${keptInk})`);
    } else {
      bad("stale draft-end wiped a newer live draft");
    }
  }
  await endB.mouse.up();

  // ---------------------------------------------------------------------------
  // 8. Skew immunity: delaying the CURSOR channel must not detach the dot.
  //
  // Production skew (tunnel latency, QUIC blips) delays cursor and draft
  // frames independently — the old two-throttle design let the dot lag the
  // live corner by hundreds of ms. Here we delay ONLY cursor-type frames by
  // 400ms while drafts flow instantly. With the fused frame the dot rides
  // the draft (same instant as the tip), so it must sit on the tip LONG
  // BEFORE the delayed cursor frames land. With separate channels the dot
  // would still be at the pre-drag position at sample time.
  //
  // Mechanism matters: the delay MUST hold the REQUEST (sleep, then
  // continue), so the server receives and broadcasts late. Delaying the
  // RESPONSE (fetch now, fulfill later) proves nothing — the sender ignores
  // responses (fire-and-forget) while the server already broadcast on time.
  // That vacuous variant passed against broken code; this one does not.
  //
  // Sampling: 200ms after motion ends. Fused trailing flushes in <=80ms
  // (+localhost ms), so the dot is on the tip; a 400ms-delayed cursor is
  // still in flight. A second sample at 900ms proves the setup converges
  // (dot appears at all) so a failure at 200ms means skew, not absence.
  //
  // RED-PROOFED: on the pre-fuse code the mid sample still shows the
  // pre-drag position (cursor channel detached), so it fails; the late
  // sample converges on both.
  // ---------------------------------------------------------------------------
  const doc8 = "wb-skew-" + process.pid;
  const skA = await makeCtx("K-A");
  const skB = await makeCtx("K-B");
  await skA.goto(`${BASE}/whiteboard/${doc8}?clientID=wb-skew-a`, { waitUntil: "networkidle" });
  await skB.goto(`${BASE}/whiteboard/${doc8}?clientID=wb-skew-b`, { waitUntil: "networkidle" });
  await sleep(900);

  let delayed = 0;
  await skB.route("**/api/whiteboard/**/presence*", async (route) => {
    const body = route.request().postData() || "";
    if (body.includes('"cursor"') && !body.includes('"draft"')) {
      delayed++;
      await sleep(400); // hold the REQUEST: server receives late, peers see it late
      await route.continue();
      return;
    }
    await route.continue();
  });

  const skBox = await skB.locator("#wb-canvas").boundingBox();
  const SKTIP = { x: 240, y: 200 };
  await skB.mouse.move(skBox.x + 60, skBox.y + 60);
  await sleep(300);
  await skB.mouse.down();
  await skB.mouse.move(skBox.x + SKTIP.x, skBox.y + SKTIP.y, { steps: 10 });
  // still HOLDING: sample mid-skew-window, then after convergence
  await sleep(200);
  const readDotPct = async (pg) => {
    const d = await readCursor(pg);
    const w = await pg.evaluate(() => {
      const r = document.getElementById("canvas-wrap").getBoundingClientRect();
      return { w: r.width, h: r.height };
    });
    return d ? { x: parseFloat(d.left) / w.w, y: parseFloat(d.top) / w.h } : null;
  };
  const skExp = { x: SKTIP.x / skBox.width, y: SKTIP.y / skBox.height };
  const mid = await readDotPct(skA);
  await sleep(700); // past the 400ms delayed cursor landing
  const late = await readDotPct(skA);

  if (delayed === 0) {
    bad("no cursor frame was delayed — the skew was never injected");
  } else {
    ok(`${delayed} cursor frame(s) delayed 400ms (skew injected)`);
  }
  const close = (p) => p && Math.abs(p.x - skExp.x) < 0.03 && Math.abs(p.y - skExp.y) < 0.03;
  if (late && close(late)) {
    ok(`dot converges on the tip (late sample ${(late.x * 100).toFixed(1)}%,${(late.y * 100).toFixed(1)}%)`);
  } else {
    bad(`dot never reached the tip: late=${JSON.stringify(late)} tip=${skExp.x.toFixed(3)},${skExp.y.toFixed(3)}`);
  }
  if (mid && close(mid)) {
    ok(`dot rides the tip DURING the skew window (mid sample ${(mid.x * 100).toFixed(1)}%,${(mid.y * 100).toFixed(1)}%)`);
  } else {
    bad(`dot detached from the tip while the cursor channel lagged: mid=${JSON.stringify(mid)} tip=${skExp.x.toFixed(3)},${skExp.y.toFixed(3)}`);
  }
  await skB.mouse.up();
  await skB.unroute("**/api/whiteboard/**/presence*");

  // ---------------------------------------------------------------------------
  // 9. Negative drag (up-left): the ink must be where the tip is.
  //
  // Dragging up-left makes w,h negative. If the renderer mangles negative
  // dimensions, the painted shape detaches from the logical corner while the
  // dot (logical tip) stays put — dot-vs-shape split with ZERO channel skew.
  // The dot assertion must hold regardless; the ink-near-tip assertion pins
  // the geometry.
  // ---------------------------------------------------------------------------
  const doc9 = "wb-neg-" + process.pid;
  const ngA = await makeCtx("N-A");
  const ngB = await makeCtx("N-B");
  await ngA.goto(`${BASE}/whiteboard/${doc9}?clientID=wb-neg-a`, { waitUntil: "networkidle" });
  await ngB.goto(`${BASE}/whiteboard/${doc9}?clientID=wb-neg-b`, { waitUntil: "networkidle" });
  await sleep(900);

  const ngBox = await ngB.locator("#wb-canvas").boundingBox();
  const NGTIP = { x: 60, y: 60 }; // dragged UP-LEFT to here; w,h negative
  await ngB.mouse.move(ngBox.x + 220, ngBox.y + 180);
  await sleep(300);
  await ngB.mouse.down();
  await ngB.mouse.move(ngBox.x + NGTIP.x, ngBox.y + NGTIP.y, { steps: 10 });
  await sleep(800); // HOLDING

  const ngDot = await readCursor(ngA);
  const ngWrap = await ngA.evaluate(() => {
    const r = document.getElementById("canvas-wrap").getBoundingClientRect();
    return { w: r.width, h: r.height };
  });
  const ngExp = { x: NGTIP.x / ngBox.width, y: NGTIP.y / ngBox.height };
  const ngGot = ngDot ? { x: parseFloat(ngDot.left) / ngWrap.w, y: parseFloat(ngDot.top) / ngWrap.h } : null;
  if (ngGot && Math.abs(ngGot.x - ngExp.x) < 0.02 && Math.abs(ngGot.y - ngExp.y) < 0.02) {
    ok(`dot on tip for negative drag (dot ${(ngGot.x * 100).toFixed(1)}%,${(ngGot.y * 100).toFixed(1)}%)`);
  } else {
    bad(`dot off tip on negative drag: dot=${JSON.stringify(ngDot)} tip=${ngExp.x.toFixed(3)},${ngExp.y.toFixed(3)}`);
  }
  // Ink must exist NEAR the tip corner (generous 30px box: rough.js wobbles,
  // but a mangled negative rect paints nowhere near its corner).
  const ngInk = await ngA.evaluate(
    (tip) => {
      const c = document.getElementById("wb-canvas");
      const rect = c.getBoundingClientRect();
      const scale = c.width / Math.max(1, rect.width);
      const g = c.getContext("2d");
      const S = 30 * scale;
      const X = Math.max(0, Math.floor((tip.x - 15) * scale));
      const Y = Math.max(0, Math.floor((tip.y - 15) * scale));
      const d = g.getImageData(X, Y, Math.min(S, c.width - X), Math.min(S, c.height - Y)).data;
      let n = 0;
      for (let i = 3; i < d.length; i += 4) if (d[i] > 8) n++;
      return n;
    },
    { x: NGTIP.x, y: NGTIP.y },
  );
  if (ngInk > 0) {
    ok(`ink painted at the negative-drag tip (ink ${ngInk})`);
  } else {
    bad("no ink near the negative-drag tip — renderer mangles negative w/h");
  }
  await ngB.mouse.up();

  if (pageErrors.length) {
    bad(`uncaught page errors: ${pageErrors.slice(0, 3).join(" | ")}`);
  } else {
    ok("no uncaught page errors");
  }

  await browser.close();
}

main()
  .catch((e) => bad("unexpected error: " + e.message))
  .finally(() => {
    server.kill("SIGKILL");
    rmSync(tmp, { recursive: true, force: true });
    console.log(
      failures === 0
        ? "\n\u2705 whiteboard client tests passed"
        : `\n\u274c whiteboard client tests failed (${failures})`
    );
  });
