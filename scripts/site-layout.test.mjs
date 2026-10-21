// SCOPE:feature - Behavioral proof: the docs site never clips its content.
//
// The reported bug (issue #189): docs pages whose tables are wider than the
// content column are cut off, with no sign that the rest is scrollable. The
// root cause was `table{min-width:max-content}`: a cell holding a real
// paragraph made one "column" ~5000px wide, so the table scrolled inside its
// box on EVERY viewport and the last column was never on screen with the
// first. Widening the page (the issue's suggestion) only moved the ratio from
// 8.9x to 7.6x, so the fix is the opposite: tables shrink and wrap, prose keeps
// its typographic measure, and the few genuinely wide data tables scroll with a
// visible edge.
//
// This asserts the OUTCOME, not the CSS: it builds the same site CI publishes,
// loads every page in Chromium at seven widths, and measures what a reader
// would have seen. Against the pre-fix builder the 1103px assertion fails on 20
// pages (features needs 15.5x its column); against a widened prose column the
// measure assertion fails.
//
// Run: node scripts/site-layout.test.mjs
// Needs: npm ci (playwright) + npx playwright install chromium.
import { chromium } from "playwright";
import { spawnSync } from "node:child_process";
import { createReadStream, readFileSync, statSync } from "node:fs";
import { createServer } from "node:http";
import { extname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = resolve(fileURLToPath(import.meta.url), "../..");
const SITE = join(ROOT, "site");

// Widths a reader actually uses: phone, tablet (nav collapses at 900), the
// reporter's 1103x850 laptop, a small desktop, and two larges.
const VIEWPORTS = [390, 768, 1024, 1103, 1280, 1440, 1920];
// Below this a genuinely wide data table is expected to scroll (it keeps
// identifiers readable instead of breaking them at every character); above it
// the table must fit.
const FIT_VIEWPORT = 1440;
// In the band above 1024 a data table may still scroll, but only a little: a
// table that needs a quarter of its box again is a layout bug, not data. The
// pre-fix site needed 1550%.
const MAX_SCROLL_RATIO = 0.25;
const MEASURE = { min: 560, max: 700 }; // prose line box, px (see --measure)

const MIME = {
  ".html": "text/html; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".txt": "text/plain; charset=utf-8",
  ".xml": "application/xml; charset=utf-8",
  ".png": "image/png",
  ".svg": "image/svg+xml",
};

function build() {
  const r = spawnSync(process.execPath, [join(ROOT, "site/build.mjs")], {
    cwd: ROOT,
    stdio: "inherit",
  });
  if (r.status !== 0) throw new Error("site/build.mjs failed");
}

// serve() answers from site/ only, on an ephemeral port. The builder emits
// directory URLs (/docs/features/), so a directory resolves to index.html.
function serve() {
  const server = createServer((req, res) => {
    const url = decodeURIComponent((req.url || "/").split("?")[0]);
    let path = join(SITE, url);
    if (!resolve(path).startsWith(SITE)) {
      res.writeHead(403).end();
      return;
    }
    try {
      if (statSync(path).isDirectory()) path = join(path, "index.html");
      statSync(path);
    } catch {
      res.writeHead(404, { "content-type": "text/plain" }).end("not found");
      return;
    }
    res.writeHead(200, { "content-type": MIME[extname(path)] || "application/octet-stream" });
    createReadStream(path).pipe(res);
  });
  return new Promise((done) => {
    server.listen(0, "127.0.0.1", () => {
      const { port } = server.address();
      done({ server, base: `http://127.0.0.1:${port}` });
    });
  });
}

// Every published docs page, read from the sitemap the builder just wrote, so a
// page added to the MANIFEST is covered without touching this file. The landing
// page has its own hand-written stylesheet; it gets the overflow assertion only.
function pages() {
  const sitemap = readFileSync(join(SITE, "sitemap.xml"), "utf8");
  const docs = [...sitemap.matchAll(/<loc>([^<]+)<\/loc>/g)]
    .map((m) => m[1].replace(/^https?:\/\/[^/]+\/gogogo/, ""))
    .filter((u) => /^\/docs\/[^/]+\/$/.test(u))
    .sort();
  return [{ path: "/", prose: false }, { path: "/docs/", prose: false }, ...docs.map((p) => ({ path: p, prose: true }))];
}

// measurePage() runs in the page: returns whatever the assertions need in one trip.
function measurePage(page) {
  return page.evaluate(() => {
    const de = document.documentElement;
    const wraps = [...document.querySelectorAll(".table-wrap")].map((w) => ({
      label: w.getAttribute("aria-label"),
      role: w.getAttribute("role"),
      tab: w.getAttribute("tabindex"),
      wrap: w.clientWidth,
      over: w.scrollWidth - w.clientWidth - 1,
      fadeRight: w.style.getPropertyValue("--fade-r"),
    }));
    const tables = [...document.querySelectorAll("table")].map((t) => ({
      wrapped: t.parentElement && t.parentElement.classList.contains("table-wrap"),
      firstCellScope: t.querySelector("th") ? t.querySelector("th").getAttribute("scope") : null,
    }));
    const p = document.querySelector("main p");
    return {
      pageOver: de.scrollWidth - de.clientWidth,
      wraps,
      tables,
      proseWidth: p ? Math.round(p.getBoundingClientRect().width) : 0,
    };
  });
}

const failures = [];
const fail = (msg) => failures.push(msg);

build();
const { server, base } = await serve();
const browser = await chromium.launch();
const list = pages();

try {
  for (const vw of VIEWPORTS) {
    const page = await browser.newPage({ viewport: { width: vw, height: 900 } });
    let checked = 0;
    for (const { path, prose } of list) {
      await page.goto(base + path, { waitUntil: "load" });
      const r = await measurePage(page);
      const where = `${path} @ ${vw}px`;
      checked++;
      if (r.pageOver > 1) fail(`${where}: page scrolls horizontally by ${r.pageOver}px`);
      for (const w of r.wraps) {
        if (vw >= FIT_VIEWPORT && w.over > 0) {
          fail(`${where}: table "${w.label}" needs ${w.over}px more than its ${w.wrap}px column`);
        } else if (vw >= 1024 && w.over > w.wrap * MAX_SCROLL_RATIO) {
          fail(`${where}: table "${w.label}" scrolls ${Math.round((w.over / w.wrap) * 100)}% (max ${MAX_SCROLL_RATIO * 100}%)`);
        }
        // A scroll box with no affordance is the reported bug: content is
        // clipped and nothing says there is more.
        if (vw === 390 && w.over > 0 && w.fadeRight !== "0.28") {
          fail(`${where}: table "${w.label}" scrolls but shows no right-edge fade`);
        }
      }
      // Structure once per page is enough; the attributes come from the builder.
      if (vw === 1440) {
        for (const w of r.wraps) {
          if (w.role !== "region") fail(`${where}: table is not a labelled region (role=${w.role})`);
          if (w.tab !== "0") fail(`${where}: table region is not keyboard reachable (tabindex=${w.tab})`);
          if (!w.label || !w.label.trim()) fail(`${where}: table region has no aria-label`);
        }
        for (const t of r.tables) {
          if (!t.wrapped) fail(`${where}: a table is outside a .table-wrap scroll box`);
          if (t.firstCellScope !== "col") fail(`${where}: a table header has no scope="col"`);
        }
        if (prose) {
          if (r.proseWidth < MEASURE.min || r.proseWidth > MEASURE.max) {
            fail(`${where}: prose line box is ${r.proseWidth}px, outside ${MEASURE.min}-${MEASURE.max}px (widening the column to fit tables costs every page's readability)`);
          }
        }
      }
    }
    console.log(`  ${vw}px: ${checked} page(s) measured`);
    await page.close();
  }
} finally {
  await browser.close();
  server.close();
}

if (failures.length) {
  console.error(`\n❌ ${failures.length} layout failure(s):`);
  for (const f of failures.slice(0, 40)) console.error("   " + f);
  if (failures.length > 40) console.error(`   ... and ${failures.length - 40} more`);
  process.exit(1);
}
console.log(`✓ docs site layout: ${list.length} pages × ${VIEWPORTS.length} viewports, no clipping, prose ${MEASURE.min}-${MEASURE.max}px, every table region labelled and keyboard reachable`);
