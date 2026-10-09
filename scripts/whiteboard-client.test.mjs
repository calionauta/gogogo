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
