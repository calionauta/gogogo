#!/usr/bin/env node
// check-browser-cleanup.mjs — no Playwright suite may leak its browser tree.
//
// WHY: see scripts/lib/browser.mjs. Short version: a suite that throws before
// its happy-path close() abandons a Chromium tree at ~600 MB per renderer, it
// goes into swap, and on this host an empty swap parks co-tenant daemons in
// D-state. That is the 2026-10-28 outage. The same defect existed twice
// (notes-caret, whiteboard-client) because each script owned its own lifecycle.
//
// CONTRACT: a file that launches Chromium must guarantee closure of the handle
// it launched, in a finally block — either a bare `try { } finally { }` or a
// `.finally()` on a promise chain — or delegate to `withBrowser` from
// scripts/lib/browser.mjs, which closes in a finally for it.
//
// Deliberately NOT accepted: a close() on the happy path only. That is the
// original bug, and it is what this gate exists to catch.
//
// Usage: node scripts/check-browser-cleanup.mjs [dir]     (default: scripts)
//        node scripts/check-browser-cleanup.mjs --self-test
//
// Limits: brace matching runs on source whose comments and string literals are
// blanked, but regex literals are not tokenised. A regex containing an
// unbalanced brace would confuse the block finder; none exist here today.
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";

const LAUNCH = /\b([A-Za-z_$][\w$]*)\s*=\s*(?:await\s+)?chromium\.(?:launch|connect)\s*\(/g;
const HAS_HELPER = /\bwithBrowser\b/;

// Blank comments and string/template literals, preserving offsets and newlines,
// so brace counting only ever sees structural code.
function maskNonCode(src) {
  const out = src.split("");
  const n = src.length;
  const blank = (a, b) => {
    for (let k = a; k < b && k < n; k++) if (out[k] !== "\n") out[k] = " ";
  };
  let i = 0;
  while (i < n) {
    const c = src[i];
    const d = src[i + 1];
    if (c === "/" && d === "/") {
      const s = i;
      while (i < n && src[i] !== "\n") i++;
      blank(s, i);
      continue;
    }
    if (c === "/" && d === "*") {
      const s = i;
      i += 2;
      while (i < n && !(src[i] === "*" && src[i + 1] === "/")) i++;
      i = Math.min(i + 2, n);
      blank(s, i);
      continue;
    }
    if (c === '"' || c === "'" || c === "`") {
      const q = c;
      const s = i;
      i++;
      while (i < n) {
        if (src[i] === "\\") {
          i += 2;
          continue;
        }
        if (src[i] === q) {
          i++;
          break;
        }
        i++;
      }
      blank(s, i);
      continue;
    }
    i++;
  }
  return out.join("");
}

function blockEnd(src, openIdx) {
  let depth = 0;
  for (let i = openIdx; i < src.length; i++) {
    if (src[i] === "{") depth++;
    else if (src[i] === "}") {
      depth--;
      if (depth === 0) return i;
    }
  }
  return -1;
}

function finallyBodies(masked) {
  const bodies = [];
  const patterns = [
    /\}\s*finally\s*\{/g, // bare try/finally
    /\.finally\s*\(\s*(?:async\s*)?\(\s*\)\s*=>\s*\{/g, // promise .finally()
  ];
  for (const re of patterns) {
    let m;
    while ((m = re.exec(masked))) {
      const open = m.index + m[0].length - 1;
      const end = blockEnd(masked, open);
      if (end > open) bodies.push(masked.slice(open, end + 1));
    }
  }
  return bodies;
}

// Returns an array of human-readable problems for one source string.
export function auditSource(src) {
  if (!/chromium\.(launch|connect)\s*\(/.test(maskNonCode(src))) return [];

  const masked = maskNonCode(src);
  const problems = [];

  const ids = new Set();
  let m;
  LAUNCH.lastIndex = 0;
  while ((m = LAUNCH.exec(masked))) ids.add(m[1]);

  // A file may delegate entirely to the helper and have no raw handle.
  if (ids.size === 0) {
    return HAS_HELPER.test(masked) ? [] : ["launches Chromium but binds no handle (use withBrowser)"];
  }

  const bodies = finallyBodies(masked);
  for (const id of ids) {
    const closeRe = new RegExp("\\b" + id.replace(/\$/g, "\\$") + "\\s*\\??\\.\\s*close\\s*\\(");
    const closedInFinally = bodies.some((b) => closeRe.test(b));
    if (!closedInFinally) {
      problems.push(`${id}.close() is never called in a finally block (a throw leaks the tree)`);
    }
  }
  return problems;
}

function walk(dir, acc = []) {
  for (const entry of readdirSync(dir)) {
    if (entry === "node_modules" || entry.startsWith(".")) continue;
    const full = join(dir, entry);
    const st = statSync(full);
    if (st.isDirectory()) walk(full, acc);
    else if (entry.endsWith(".mjs") || entry.endsWith(".js")) acc.push(full);
  }
  return acc;
}

function selfTest() {
  const leaking = `
    async function main() {
      const browser = await chromium.launch({ headless: true });
      const page = await browser.newPage();
      await page.goto("http://x");
      await browser.close();
    }
    main().catch(() => {}).finally(() => { console.log("done"); });
  `;
  const safeBare = `
    let browser;
    try {
      browser = await chromium.launch({ headless: true });
      await browser.newContext();
    } finally {
      if (browser) await browser.close().catch(() => {});
    }
  `;
  const safeHelper = `
    import { withBrowser } from "./lib/browser.mjs";
    await withBrowser(async (b) => { await b.newPage(); });
  `;
  const cases = [
    ["leaking happy-path close", leaking, true],
    ["bare try/finally", safeBare, false],
    ["withBrowser helper", safeHelper, false],
  ];
  let failed = 0;
  for (const [name, src, shouldFail] of cases) {
    const problems = auditSource(src);
    const didFail = problems.length > 0;
    const ok = didFail === shouldFail;
    console.log(`  ${ok ? "ok  " : "FAIL"}  ${name} (expected ${shouldFail ? "fail" : "pass"})`);
    if (!ok) failed++;
  }
  return failed;
}

const arg = process.argv[2] || "scripts";
if (process.argv.includes("--self-test")) {
  const failed = selfTest();
  process.exit(failed ? 1 : 0);
}

const offenders = [];
for (const file of walk(arg)) {
  const src = readFileSync(file, "utf8");
  const problems = auditSource(src);
  if (problems.length) offenders.push({ file, problems });
}

if (offenders.length === 0) {
  console.log("browser cleanup: every Playwright suite closes its browser in a finally");
  process.exit(0);
}

console.error("browser cleanup: leaked-browser risk\n");
for (const o of offenders) {
  console.error(`  ${o.file}`);
  for (const p of o.problems) console.error(`    - ${p}`);
}
console.error("\nFix: import { withBrowser } from './lib/browser.mjs', or close the handle in a finally.");
process.exit(1);
