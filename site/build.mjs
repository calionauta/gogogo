// gogogo docs site builder — zero dependencies (node stdlib only).
//
// Inputs:  docs/**/*.md listed in the MANIFEST below (explicit list = stable slugs).
// Outputs: site/docs/<slug>/index.html, site/docs/index.html, site/llms.txt,
//          site/llms-full.txt, site/sitemap.xml.
//
// Refresh procedure: to add a page, append one MANIFEST row with a one-line
// description, then run `node site/build.mjs`. Slugs are stable URLs — never
// rename one without a redirect note.
//
// Usage: node site/build.mjs [--check]
//   --check verifies every manifest file exists and every internal .md link
//   resolves, without writing output. CI runs the build; reviewers run --check.

import { readFileSync, writeFileSync, mkdirSync, existsSync } from "node:fs";
import { join, dirname, resolve, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const DOCS = join(ROOT, "docs");
const OUT = join(ROOT, "site");
const NAME = "gogogo";
const BASE = `https://calionauta.github.io/${NAME}`;

// emitting page context for link rewriting (set per page in build())
let CUR_SLUG = "";

// slug, source file (under docs/), one-line description (also feeds llms.txt)
const MANIFEST = [
  { group: "Get started", slug: "overview", file: "overview.md",
    desc: "What gogogo is, who it is for, and the six async layers it ships." },
  { group: "Get started", slug: "getting-started", file: "getting-started.md",
    desc: "Clone, run, first five minutes, and the commands you need." },
  { group: "Core", slug: "architecture", file: "architecture.md",
    desc: "Directory layout, dependency direction, entry points, and route wiring gotchas." },
  { group: "Core", slug: "stack-layers", file: "stack-layers.md",
    desc: "Every dependency and why it is in the box." },
  { group: "Core", slug: "async-layers", file: "async-layers.md",
    desc: "The six async layers, PB realtime vs SSE Hub, cross-instance and offline sync." },
  { group: "Core", slug: "features", file: "features.md",
    desc: "Every capability and its runtime opt-out." },
  { group: "Core", slug: "scope-taxonomy", file: "scope-taxonomy.md",
    desc: "Core / Plugin / Feature: the rule for deciding what is safe to delete." },
  { group: "Core", slug: "configuration", file: "configuration.md",
    desc: "Every environment variable and runtime constant." },
  { group: "Core", slug: "native-zig", file: "native-zig.md",
    desc: "Go-first policy: why Zig is an exceptional native boundary, the agent decision procedure, and the isolation, testing, and removal rules." },
  { group: "Frontend", slug: "todo-example", file: "todo-example.md",
    desc: "The Todo reference implementation and the contract to imitate." },
  { group: "Frontend", slug: "ui-skins", file: "ui-skins.md",
    desc: "Pluggable DaisyUI / Basecoat / Morpheus skins and the plugin contract." },
  { group: "Frontend", slug: "ui-sounds", file: "ui-sounds.md",
    desc: "Vendored cuelume sound feedback and its accessibility contract." },
  { group: "Ship", slug: "deploy", file: "deploy.md",
    desc: "Server layout, first-time setup, deploy workflow, build pipeline and version badge." },
  { group: "Ship", slug: "desktop-mobile", file: "desktop-mobile.md",
    desc: "Wails v3 desktop/mobile, edge sync, and the native window PoC." },
  { group: "Ship", slug: "admin-dashboard", file: "admin-dashboard.md",
    desc: "Admin surfaces, the two-cookie rule, and the DagNats console." },
  { group: "Ship", slug: "llm-and-credits", file: "llm-and-credits.md",
    desc: "GoAI configuration and the optional ai-credits / BYOK plugin." },
  { group: "Operate", slug: "local-ci", file: "local-ci.md",
    desc: "The five-tier feedback loop and make signoff." },
  { group: "Operate", slug: "code-quality", file: "code-quality.md",
    desc: "The 27 linters, how to scope them, and the Datastar-specific rules." },
  { group: "Operate", slug: "troubleshooting", file: "troubleshooting.md",
    desc: "Symptoms and where they actually come from." },
  { group: "Operate", slug: "dagnats-bootstrap-workaround",
    file: "dagnats-bootstrap-workaround.md",
    desc: "The upstream DagNats bug that breaks the trigger console on a fresh install, and the removable seed that works around it." },
];

const CSS = `*{box-sizing:border-box}body{margin:0;font:16px/1.65 system-ui,-apple-system,sans-serif;color:#1a1a1a;background:#fff}.wrap{display:flex;max-width:1080px;margin:0 auto}nav.side{width:250px;flex-shrink:0;padding:32px 24px;border-right:1px solid #e5e5e5;position:sticky;top:0;align-self:flex-start;max-height:100vh;overflow:auto}nav.side .home{display:block;font-weight:700;margin-bottom:16px;color:#1a1a1a;text-decoration:none}nav.side h4{font-size:12px;text-transform:uppercase;letter-spacing:.06em;color:#666;margin:16px 0 4px}nav.side a{display:block;padding:3px 0;color:#333;text-decoration:none;font-size:14px}nav.side a.cur{font-weight:700}nav.side a:hover{text-decoration:underline}main{flex:1;min-width:0;padding:32px 40px;max-width:720px}main h1{font-size:30px;line-height:1.25;margin:0 0 16px}main h2{font-size:22px;margin:32px 0 8px;border-bottom:1px solid #eee;padding-bottom:6px}main h3{font-size:17px;margin:24px 0 8px}main p,main li{color:#222}main a{color:#0b5fff}main code{font:13px ui-monospace,monospace;background:#f4f4f5;padding:2px 5px;border-radius:4px}main pre{background:#111;color:#eee;padding:14px 16px;border-radius:8px;overflow:auto}main pre code{background:none;padding:0;color:inherit}.table-wrap{overflow-x:auto;margin:16px 0;-webkit-overflow-scrolling:touch}main table{border-collapse:collapse;width:100%;min-width:max-content;margin:0;font-size:14px}main th,main td{border:1px solid #ddd;padding:8px 10px;text-align:left;vertical-align:top}main th{background:#f7f7f8}main blockquote{border-left:3px solid #0b5fff;margin:16px 0;padding:4px 16px;background:#f5f8ff}main hr{border:none;border-top:1px solid #e5e5e5;margin:32px 0}.prevnext{display:flex;justify-content:space-between;gap:16px;margin-top:40px;padding-top:16px;border-top:1px solid #eee;font-size:14px}@media(max-width:760px){nav.side{display:none}main{padding:24px 20px;max-width:100%}main pre{font-size:12.5px}}`;

function esc(s) {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function inline(s, pageDir) {
  // images first
  s = s.replace(/!\[([^\]]*)\]\(([^)]+)\)/g, (_, alt, src) => `<img alt="${esc(alt)}" src="${esc(rewrite(src, pageDir))}">`);
  s = s.replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_, text, href) => {
    if (/^(https?:|mailto:|#)/.test(href)) return `<a href="${esc(href)}">${esc(text)}</a>`;
    return `<a href="${esc(rewrite(href, pageDir))}">${esc(text)}</a>`;
  });
  s = s.replace(/`([^`]+)`/g, (_, code) => `<code>${esc(code)}</code>`);
  s = s.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  s = s.replace(/(^|\W)\*([^*\n]+)\*/g, "$1<em>$2</em>");
  return s;
}

// .md links become directory URLs relative to the emitting page when the target
// is a manifest page; anything else (repo files outside site nav, e.g.
// ARCHITECTURE.md at the root) links to the GitHub blob so it never 404s.
function rewrite(href, pageDir) {
  const hashIdx = href.indexOf("#");
  const hash = hashIdx >= 0 ? href.slice(hashIdx) : "";
  const path = hashIdx >= 0 ? href.slice(0, hashIdx) : href;
  if (!path.endsWith(".md")) return href;
  const abs = resolve(DOCS, pageDir, path);
  const relToDocs = relative(DOCS, abs).split(sep).join("/");
  const hit = MANIFEST.find((p) => p.file === relToDocs);
  if (hit) return rel(CUR_SLUG, hit.slug) + hash;
  // A path that escapes docs/ (e.g. ../ARCHITECTURE.md) points at a repo file
  // outside the docs tree, so it becomes a GitHub blob URL. Resolve it against
  // the REPO ROOT, not docs/ — otherwise the URL keeps a literal `..` segment
  // (`blob/master/../ARCHITECTURE.md`). GitHub happens to normalise that and
  // return 200, which is exactly why the mistake survives unnoticed: the link
  // works by accident and reads as broken to anyone inspecting the markup.
  const relToRoot = relative(ROOT, abs).split(sep).join("/");
  const blob = `https://github.com/calionauta/${NAME}/blob/master/${relToRoot}`;
  return blob + hash;
}

function renderBody(md, pageDir) {
  const lines = md.split("\n");
  const out = [];
  let i = 0;
  const flushPara = (buf) => {
    if (buf.length) { out.push(`<p>${inline(buf.join(" "), pageDir)}</p>`); buf.length = 0; }
  };
  let para = [];
  while (i < lines.length) {
    const line = lines[i];
    if (/^```/.test(line)) {
      flushPara(para);
      const fence = [line];
      i++;
      while (i < lines.length && !/^```/.test(lines[i])) { fence.push(lines[i]); i++; }
      i++; // closing fence
      const code = fence.slice(1).join("\n");
      out.push(`<pre><code>${esc(code.replace(/^\n/, ""))}</code></pre>`);
      continue;
    }
    const h = line.match(/^(#{1,4})\s+(.*)/);
    if (h) {
      flushPara(para);
      // heading carries a GitHub-compatible id so #fragment links resolve
      const raw = h[2].replace(/\[([^\]]+)\]\([^)]+\)/g, "$1"); // strip links
      const id = slugify(raw);
      out.push(`<h${h[1].length}${id ? ` id="${esc(id)}"` : ""}>${inline(h[2], pageDir)}</h${h[1].length}>`);
      i++;
      continue;
    }
    if (/^---+$/.test(line.trim())) {
      flushPara(para);
      out.push("<hr>");
      i++;
      continue;
    }
    if (/^>\s?/.test(line)) {
      flushPara(para);
      const quote = [];
      while (i < lines.length && /^>\s?/.test(lines[i])) { quote.push(lines[i].replace(/^>\s?/, "")); i++; }
      out.push(`<blockquote>${inline(quote.join(" "), pageDir)}</blockquote>`);
      continue;
    }
    if (/^\s*([-*]|\d+\.)\s+/.test(line)) {
      flushPara(para);
      const ordered = /^\s*\d+\.\s+/.test(line);
      const items = [];
      while (i < lines.length && /^\s*([-*]|\d+\.)\s+/.test(lines[i])) {
        items.push(`<li>${inline(lines[i].replace(/^\s*([-*]|\d+\.)\s+/, ""), pageDir)}</li>`);
        i++;
      }
      out.push(ordered ? `<ol>${items.join("")}</ol>` : `<ul>${items.join("")}</ul>`);
      continue;
    }
    if (/^\|.*\|$/.test(line.trim())) {
      flushPara(para);
      const rows = [];
      while (i < lines.length && /^\|.*\|$/.test(lines[i].trim())) { rows.push(lines[i].trim()); i++; }
      const cells = (r) => r.split("|").slice(1, -1).map((c) => c.trim());
      const head = cells(rows[0]);
      const body = rows.slice(1).filter((r) => !/^[\s|:|-]+$/.test(r));
      out.push(`<div class="table-wrap"><table><thead><tr>${head.map((c) => `<th>${inline(c, pageDir)}</th>`).join("")}</tr></thead><tbody>${
        body.map((r) => `<tr>${cells(r).map((c) => `<td>${inline(c, pageDir)}</td>`).join("")}</tr>`).join("")
      }</tbody></table></div>`);
      continue;
    }
    if (/^\s*$/.test(line)) {
      flushPara(para);
      i++;
      continue;
    }
    para.push(line.trim());
    i++;
  }
  flushPara(para);
  return out.join("\n");
}

function titleOf(md, fallback) {
  const m = md.match(/^#\s+(.*)/m);
  return m ? m[1].trim() : fallback;
}

// GitHub-style heading slug: lowercase, strip punctuation, spaces -> hyphens.
function slugify(s) {
  return s
    .replace(/`/g, "")
    .trim()
    .toLowerCase()
    .replace(/[^\w\s-]/g, "")
    .replace(/\s+/g, "-")
    .replace(/-+/g, "-");
}

function sidebar(cur) {
  let html = `<a class="home" href="${up(cur)}">← ${NAME}</a>`;
  let group = "";
  for (const p of MANIFEST) {
    if (p.group !== group) { group = p.group; html += `<h4>${esc(group)}</h4>`; }
    const cls = p.slug === cur ? ` class="cur"` : "";
    html += `<a${cls} href="${rel(cur, p.slug)}">${esc(p.title)}</a>`;
  }
  return html;
}

// relative URL from the page at fromSlug to the page at toSlug (both dir URLs).
// fromSlug segments ARE the directory (page lives at docs/<slug>/index.html).
function rel(fromSlug, toSlug) {
  const fromDir = fromSlug.split("/");
  const to = toSlug.split("/");
  while (fromDir.length && to.length && fromDir[0] === to[0]) { fromDir.shift(); to.shift(); }
  if (!to.length) return "./";
  return "../".repeat(fromDir.length) + to.join("/") + "/";
}

// relative URL from the page at slug back to the site root
function up(slug) {
  return "../".repeat(slug.split("/").length + 1);
}

function pageShell(title, cur, body, prevNext, navOverride) {
  const nav = navOverride !== undefined ? navOverride : sidebar(cur);
  return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${esc(title)} — ${NAME} docs</title>
<style>${CSS}</style>
</head>
<body>
<div class="wrap">
<nav class="side">${nav}</nav>
<main>
${body}
<div class="prevnext">${prevNext}</div>
</main>
</div>
</body>
</html>`;
}

function check() {
  let failed = 0;
  for (const p of MANIFEST) {
    const path = join(DOCS, p.file);
    if (!existsSync(path)) { console.error(`missing manifest file: ${p.file}`); failed++; }
  }
  if (failed) { console.error(`${failed} check failure(s)`); process.exit(1); }
  // every internal .md link must resolve (to a file, or to a manifest slug)
  for (const p of MANIFEST) {
    const md = readFileSync(join(DOCS, p.file), "utf8");
    const pageDir = dirname(p.file) === "." ? "" : dirname(p.file);
    for (const m of md.matchAll(/\]\(([^)#]+)(#[^)]*)?\)/g)) {
      const target = m[1];
      if (/^(https?:|mailto:)/.test(target)) continue;
      const abs = resolve(DOCS, pageDir, target);
      if (!existsSync(abs)) { console.error(`broken link in ${p.file}: ${target}`); failed++; }
    }
  }
  // a manifest slug must never be shadowed by a bare .md link that the
  // manifest does not own — an unmanifested .md INSIDE docs/ silently
  // renders as a raw blob URL. Files OUTSIDE docs/ (repo root, e.g.
  // ../ARCHITECTURE.md) are legitimately blob links; only flag docs-internal.
  for (const p of MANIFEST) {
    const md = readFileSync(join(DOCS, p.file), "utf8");
    const pageDir = dirname(p.file) === "." ? "" : dirname(p.file);
    for (const m of md.matchAll(/\]\(([^)#]+\.md)(#[^)]*)?\)/g)) {
      const target = m[1];
      if (/^(https?:|mailto:)/.test(target)) continue;
      const abs = resolve(DOCS, pageDir, target);
      const relToDocs = relative(DOCS, abs).split(sep).join("/");
      if (relToDocs.startsWith("..")) continue; // outside docs/ => blob link, fine
      const hit = MANIFEST.find((x) => x.file === relToDocs);
      if (!hit) { console.error(`unmanifested .md link in ${p.file}: ${target} (inside docs/, not in MANIFEST)`); failed++; }
    }
  }
  if (failed) { console.error(`${failed} check failure(s)`); process.exit(1); }

  // every #fragment in a cross-page link must match a heading slug on the
  // target page — headings emit GitHub-style ids, so drift is a real bug.
  const anchorsOf = new Map();
  for (const p of MANIFEST) {
    const md = readFileSync(join(DOCS, p.file), "utf8");
    const ids = new Set();
    for (const m of md.matchAll(/^#{1,4}\s+(.*)$/gm)) {
      ids.add(slugify(m[1].replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")));
    }
    anchorsOf.set(p.file, ids);
  }
  const byFile = new Map(MANIFEST.map((p) => [p.slug, p.file]));
  for (const p of MANIFEST) {
    const md = readFileSync(join(DOCS, p.file), "utf8");
    const pageDir = dirname(p.file) === "." ? "" : dirname(p.file);
    for (const m of md.matchAll(/\]\(([^)#]+\.md)#([^)]+)\)/g)) {
      const target = m[1];
      const frag = m[2];
      if (/^(https?:|mailto:)/.test(target)) continue;
      const abs = resolve(DOCS, pageDir, target);
      const relToDocs = relative(DOCS, abs).split(sep).join("/");
      const hit = MANIFEST.find((x) => x.file === relToDocs);
      if (!hit) continue;
      const ids = anchorsOf.get(byFile.get(hit.slug));
      if (!ids.has(frag)) {
        console.error(`missing anchor in ${p.file}: ${target}#${frag}`);
        failed++;
      }
    }
    // same-page fragments
    for (const m of md.matchAll(/\]\((#[^)]+)\)/g)) {
      const frag = m[1].slice(1);
      const ids = anchorsOf.get(p.file);
      if (!ids.has(frag)) { console.error(`missing anchor in ${p.file}: ${m[1]}`); failed++; }
    }
  }
  if (failed) { console.error(`${failed} check failure(s)`); process.exit(1); }
  console.log(`check ok: ${MANIFEST.length} pages, all links and anchors resolve`);
}

function build() {
  check();
  for (const p of MANIFEST) {
    CUR_SLUG = p.slug;
    p.md = readFileSync(join(DOCS, p.file), "utf8");
    p.title = titleOf(p.md, p.slug);
  }
  // docs pages
  MANIFEST.forEach((p, idx) => {
    CUR_SLUG = p.slug;
    const pageDir = dirname(p.file) === "." ? "" : dirname(p.file);
    const prev = MANIFEST[idx - 1];
    const next = MANIFEST[idx + 1];
    const nav = `${prev ? `<a href="${rel(p.slug, prev.slug)}">← ${esc(prev.title)}</a>` : "<span></span>"}${next ? `<a href="${rel(p.slug, next.slug)}">${esc(next.title)} →</a>` : "<span></span>"}`;
    const body = renderBody(p.md, pageDir);
    const dir = join(OUT, "docs", p.slug);
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "index.html"), pageShell(p.title, p.slug, body, nav));
  });
  // docs index
  let groups = "";
  let group = "";
  for (const p of MANIFEST) {
    if (p.group !== group) { group = p.group; groups += `<h2>${esc(group)}</h2>\n<ul>`; }
    groups += `<li><a href="./${p.slug}/">${esc(p.title)}</a> — ${esc(p.desc)}</li>`;
  }
  groups += "</ul>".repeat(new Set(MANIFEST.map((p) => p.group)).size);
  let idxNav = `<a class="home" href="../">← ${NAME}</a>`;
  let idxGroup = "";
  for (const p of MANIFEST) {
    if (p.group !== idxGroup) { idxGroup = p.group; idxNav += `<h4>${esc(p.group)}</h4>`; }
    idxNav += `<a href="./${p.slug}/">${esc(p.title)}</a>`;
  }
  mkdirSync(join(OUT, "docs"), { recursive: true });
  writeFileSync(join(OUT, "docs", "index.html"),
    pageShell("Docs", "", `<h1>${esc(NAME)} docs</h1>\n${groups}`, `<a href="../">← ${NAME} home</a>`, idxNav));
  // llms.txt (stable map: one line per page)
  const llms = `# ${NAME} docs\n\n> Full-stack Go web app template that ships as one binary: PocketBase + Templ + Datastar + Tailwind, plus six complementary async layers (goqite, DagNats, Loro CRDT, PocketBase realtime, SSE Hub, JetStream). Full map below; complete texts in llms-full.txt.\n\n` +
    MANIFEST.map((p) => `## ${p.title}\n${p.desc}\n${BASE}/docs/${p.slug}/\n`).join("\n");
  writeFileSync(join(OUT, "llms.txt"), llms);
  // llms-full.txt (concatenated sources)
  const full = MANIFEST.map((p) => `# ${p.title}\n\nSource: docs/${p.file} — ${BASE}/docs/${p.slug}/\n\n${p.md.trim()}\n`).join("\n---\n\n");
  writeFileSync(join(OUT, "llms-full.txt"), `# ${NAME} docs (full)\n\n${full}`);
  // sitemap
  const urls = ["", "docs/", ...MANIFEST.map((p) => `docs/${p.slug}/`)];
  writeFileSync(join(OUT, "sitemap.xml"),
    `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n` +
    urls.map((u) => `  <url><loc>${BASE}/${u}</loc></url>`).join("\n") + `\n</urlset>\n`);
  console.log(`built ${MANIFEST.length} pages + index + llms.txt + llms-full.txt + sitemap.xml`);
}

if (process.argv.includes("--check")) check();
else build();