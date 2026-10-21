// gogogo docs site builder — zero dependencies (node stdlib only).
//
// Inputs:  docs/**/*.md listed in the MANIFEST below (explicit list = stable slugs),
//          plus CHANGELOG.md sections (see RELEASE_SOURCES).
// Outputs: site/docs/<slug>/index.html, site/docs/index.html,
//          site/releases/index.html + site/releases/<version>.html,
//          site/llms.txt, site/llms-full.txt, site/sitemap.xml.
//
// Releases pattern: skill cali-ops-changelog-site (agent-sync-public);
// site/releases.mjs is a byte-identical vendored copy of its script.
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
import { checkReleases, buildReleases } from "./releases.mjs";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const DOCS = join(ROOT, "docs");
const OUT = join(ROOT, "site");
const NAME = "gogogo";
const BASE = `https://calionauta.github.io/${NAME}`;

// emitting page context for link rewriting (set per page in build())
let CUR_SLUG = "";

// CHANGELOG.md sections → /releases/ article pages (pattern: skill
// cali-ops-changelog-site). Single source; versions share one slug namespace.
const RELEASE_SOURCES = [
  { file: "CHANGELOG.md", origin: "", repo: "calionauta/gogogo", tagPrefix: "v" },
];

// slug, source file (under docs/), one-line description (also feeds llms.txt)
const MANIFEST = [
  { group: "Get started", slug: "overview", file: "overview.md",
    desc: "What gogogo is, who it is for, and the seven async layers it ships." },
  { group: "Get started", slug: "motivation", file: "motivation.md",
    desc: "The thesis behind the stack: Go for almost everything, Zig as the named exception, and how to choose without hype." },
  { group: "Get started", slug: "architecture-decision", file: "architecture-decision.md",
    desc: "The flat-stack decision record: what it serves, what it replaced, and the migration it completed." },
  { group: "Get started", slug: "inspirations", file: "inspirations.md",
    desc: "What gogogo learned from other projects, credited rather than vendored." },
  { group: "Get started", slug: "getting-started", file: "getting-started.md",
    desc: "Clone, run, first five minutes, and the commands you need." },
  { group: "Agents", slug: "cli", file: "cli.md",
    desc: "The installer as scriptable commands: advise, preview, apply, add, check." },
  { group: "Agents", slug: "mcp", file: "mcp.md",
    desc: "Install the MCP server, wire it into your client, and the six tools." },
  { group: "Core", slug: "architecture", file: "architecture.md",
    desc: "Directory layout, dependency direction, entry points, and route wiring gotchas." },
  { group: "Core", slug: "stack-layers", file: "stack-layers.md",
    desc: "Every dependency and what it buys, layer by layer." },
  { group: "Core", slug: "stack-tradeoffs", file: "stack-tradeoffs.md",
    desc: "What each choice rejected, what it costs to keep, and when to revisit: Turso vs SQLite vs Postgres, Datastar vs React, and the rest." },
  { group: "Core", slug: "features", file: "features.md",
    desc: "Every capability and its runtime opt-out." },
  { group: "Core", slug: "api-routes", file: "api-routes.md",
    desc: "Generated inventory of every API route, pinned by tests — the trim and hook gate that proves no route is lost." },
  { group: "Core", slug: "unit-contracts", file: "unit-contracts.md",
    desc: "Generated card per capability: routes it registers, generated queries it calls, runtime switch, and what removing it takes." },
  { group: "Core", slug: "no-auth-no-db-recipe", file: "no-auth-no-db-recipe.md",
    desc: "Recipe for stripping auth and the database out of a clone that needs neither." },
  { group: "Core", slug: "use-cases", file: "use-cases.md",
    desc: "What each core, plugin, and feature is for, in business language." },
  { group: "Core", slug: "scope-taxonomy", file: "scope-taxonomy.md",
    desc: "Core / Plugin / Feature: the rule for deciding what is safe to delete." },
  { group: "Core", slug: "db-concurrency", file: "db-concurrency.md",
    desc: "Turso single-writer stance, the probed MVCC path, and revisit triggers." },
  { group: "Core", slug: "configuration", file: "configuration.md",
    desc: "Every environment variable and runtime constant." },
  { group: "Core", slug: "rbac", file: "rbac.md",
    desc: "Casbin RBAC: role vocabulary, the feature checklist, hint-vs-enforcement, and the two invariants that bite (authentication is not authorization; policy objects are paths, not regex)." },
  { group: "Core", slug: "exception-to-go", file: "exception-to-go.md",
    desc: "Go-first policy: why Zig is an exceptional native boundary, the agent decision procedure, and the isolation, testing, and removal rules." },
  { group: "Core", slug: "native-language-tradeoffs", file: "native-language-tradeoffs.md",
    desc: "Evidence-backed tradeoffs of Zig, Rust, Nim, Odin, and Mojo as native exception languages, and what a non-Zig proposal must rebut." },
  { group: "Async", slug: "async-layers", file: "async-layers.md",
    desc: "The seven async layers, SSE Hub record-sync vs ephemeral signals, cross-instance and offline sync." },
  { group: "Async", slug: "realtime-recipe", file: "realtime-recipe.md",
    desc: "How to add live UI: client/server patterns, checklist, no invented transports." },
  { group: "Async", slug: "dagnats",
    file: "dagnats.md",
    desc: "DagNats capability table, version bounds, learned gotchas, and the add-a-workflow path for LLM agents." },
  { group: "AI", slug: "llm-and-credits", file: "llm-and-credits.md",
    desc: "GoAI configuration and the optional ai-credits / BYOK plugin." },
  { group: "AI", slug: "review", file: "review.md",
    desc: "The review loop: Genkit flows, goai generation, and a Jev-compatible classifier judge." },
  { group: "Frontend", slug: "todo-example", file: "todo-example.md",
    desc: "The Todo reference implementation and the contract to imitate." },
  { group: "Frontend", slug: "ui-skins", file: "ui-skins.md",
    desc: "Pluggable DaisyUI / Basecoat / shadcn skins and the plugin contract." },
  { group: "Frontend", slug: "ui-sounds", file: "ui-sounds.md",
    desc: "Vendored cuelume sound feedback and its accessibility contract." },
  { group: "Research", slug: "research", file: "research/index.md",
    desc: "One place for every research note, comparison and tradeoff decision in this repo: what was evaluated, when, and what was chosen." },
  { group: "Research", slug: "ui-engine-research", file: "research/ui-engine-research.md",
    desc: "The 2026-10-21 engine and component-library evaluation behind the UI stack, with local benchmarks and the Rocket assessment." },
  { group: "Research", slug: "ui-components", file: "research/ui-components-2026-10-21.md",
    desc: "The 2026-10-21 comparison of DaisyUI, Starbase, datastar-ui and dsx on Datastar, SSE, lightness, performance, functionality and forkability." },
  { group: "Ship", slug: "deploy", file: "deploy.md",
    desc: "Server layout, first-time setup, deploy workflow, build pipeline and version badge." },
  { group: "Ship", slug: "desktop-mobile", file: "desktop-mobile.md",
    desc: "Wails v3 desktop/mobile, edge sync, and the native window." },
  { group: "Ship", slug: "admin-dashboard", file: "admin-dashboard.md",
    desc: "Admin surfaces, the single-cookie session, and the DagNats console." },
  { group: "Operate", slug: "local-ci", file: "local-ci.md",
    desc: "The five-tier feedback loop and make signoff." },
  { group: "Operate", slug: "ci-triage", file: "ci-triage.md",
    desc: "When CI fails on something variable: the 60-second triage, flake diagnosis, quarantine with an expiry, and why 100% is not the target." },
  { group: "Operate", slug: "code-quality", file: "code-quality.md",
    desc: "The 33 linters, how to scope them, the Datastar-specific rules, and the ruleguard rules that encode project footguns as CI failures." },
  { group: "Operate", slug: "troubleshooting", file: "troubleshooting.md",
    desc: "Symptoms and where they actually come from." },
  { group: "Operate", slug: "duckdb", file: "duckdb.md",
    desc: "Query JSONL logs and the Turso file with DuckDB: local operator recipe." },
];

// Page CSS, inlined into every generated page (there is no stylesheet request
// and no CSS build step for the docs site). Written one rule per line because a
// single-line string makes every edit an all-or-nothing diff.
//
// Rules that carry a non-obvious reason:
//
//   main table / no min-width     A markdown table that sets min-width:max-content
//                                 refuses to wrap any cell and takes its full
//                                 intrinsic width. With a cell that holds a real
//                                 paragraph (docs/configuration.md's `Purpose`
//                                 cells run to ~750 characters) that is thousands
//                                 of pixels wide inside a 640px column, so the
//                                 table scrolls its own box for every reader at
//                                 every viewport and the last column is never on
//                                 screen with the first. Letting the table shrink
//                                 and its cells wrap is what makes the table
//                                 readable at rest; overflow-x on .table-wrap then
//                                 covers the narrow viewports where a genuine
//                                 data table cannot fit.
//
//   --measure                     Prose keeps a ~70-character line (typographic
//                                 measure). Tables and code blocks are exempt so
//                                 they can use the wider container: narrow prose,
//                                 wide data. Widening the prose column to fix a
//                                 table costs readability on every page to win
//                                 pixels on a few, so it is not the trade to make.
//
//   overflow-wrap                 Inline code breaks anywhere: an env value or a
//                                 URL in a sentence is one long token, and without
//                                 this it pushes the whole page sideways on a
//                                 phone. Code inside a table cell does NOT: there
//                                 the break-word default keeps intrinsic column
//                                 widths sane (the `Variable` column stays wide
//                                 enough for `ENVIRONMENT`). The exception is the
//                                 LAST column of a >=761px viewport: it holds the
//                                 prose (Purpose / Notes / What it buys), whose
//                                 max-content is huge and whose min-content (one
//                                 long word) is what pushed an env table past the
//                                 content column on a desktop. `anywhere` there
//                                 lets that column absorb the shrink instead of
//                                 scrolling. On a phone the same rule would squeeze
//                                 prose into ~100px, so the table keeps word
//                                 wrapping and scrolls, which is what the fade
//                                 affordance is for.
const CSS = `
:root{--measure:640px}
*{box-sizing:border-box}
body{margin:0;font:16px/1.65 system-ui,-apple-system,sans-serif;color:#1a1a1a;background:#fff}
.wrap{display:flex;max-width:1200px;margin:0 auto}
nav.side{width:250px;flex-shrink:0;padding:32px 24px;border-right:1px solid #e5e5e5;position:sticky;top:0;align-self:flex-start;max-height:100vh;overflow:auto}
nav.side .home{display:block;font-weight:700;margin-bottom:16px;color:#1a1a1a;text-decoration:none}
nav.side h4{font-size:12px;text-transform:uppercase;letter-spacing:.06em;color:#666;margin:16px 0 4px}
nav.side a{display:block;padding:3px 0;color:#333;text-decoration:none;font-size:14px}
nav.side a.cur{font-weight:700}
nav.side a:hover{text-decoration:underline}
main{flex:1;min-width:0;padding:32px 40px}
main p,main li,main h1,main h2,main h3,main h4,main blockquote,main hr,main ul,main ol,main .prevnext{max-width:var(--measure)}
main h1{font-size:30px;line-height:1.25;margin:0 0 16px}
main h2{font-size:22px;margin:32px 0 8px;border-bottom:1px solid #eee;padding-bottom:6px}
main h3{font-size:17px;margin:24px 0 8px}
main p,main li{color:#222}
main a{color:#0b5fff}
main code{font:13px ui-monospace,monospace;background:#f4f4f5;padding:2px 5px;border-radius:4px;overflow-wrap:anywhere}
main th code,main td code{overflow-wrap:break-word}
main pre{background:#111;color:#eee;padding:14px 16px;border-radius:8px;overflow:auto;position:relative}
main pre code{background:none;padding:0;color:inherit;overflow-wrap:normal}
.copy-btn{position:absolute;top:14px;right:16px;font-size:12px;line-height:1;padding:5px 10px;border:1px solid #444;border-radius:6px;background:#fff;color:#555;cursor:pointer;white-space:nowrap}
.copy-btn:hover{color:#111;border-color:#999}
.copy-btn.copied{color:#0b5fff;border-color:#0b5fff}
.table-wrap{overflow-x:auto;margin:16px 0;-webkit-overflow-scrolling:touch;background-repeat:no-repeat;background-position:left center,right center;background-size:18px 100%,18px 100%;background-image:radial-gradient(farthest-side at 0 50%,rgba(0,0,0,var(--fade-l,0)),rgba(0,0,0,0)),radial-gradient(farthest-side at 100% 50%,rgba(0,0,0,var(--fade-r,0)),rgba(0,0,0,0))}
.table-wrap:focus-visible{outline:2px solid #0b5fff;outline-offset:2px;border-radius:4px}
main table{border-collapse:collapse;width:100%;min-width:0;margin:0;font-size:14px}
main th,main td{border:1px solid #ddd;padding:8px 10px;text-align:left;vertical-align:top;overflow-wrap:break-word}
@media(min-width:761px){
main td:last-child{overflow-wrap:anywhere}
main td:last-child code{overflow-wrap:anywhere}
}
main th{background:#f7f7f8}
main blockquote{border-left:3px solid #0b5fff;margin:16px 0;padding:4px 16px;background:#f5f8ff}
main hr{border:none;border-top:1px solid #e5e5e5;margin:32px 0}
.prevnext{display:flex;justify-content:space-between;gap:16px;margin-top:40px;padding-top:16px;border-top:1px solid #eee;font-size:14px}
.cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:14px;margin:16px 0;padding:0;list-style:none}
.card{display:flex;gap:12px;border:1px solid #e5e5e5;border-radius:12px;padding:14px 16px;background:#fafafa}
.card .ic{font-size:22px;line-height:1.3}
.card .card-body{min-width:0;font-size:14.5px;color:#333}
.card .card-body strong:first-child{display:block;margin-bottom:2px;color:#111}
@media(max-width:900px){
nav.side{display:none}
main{padding:24px 20px}
main pre{font-size:12.5px}
.cards{grid-template-columns:1fr}
}
`;

// A list whose every item opens with an emoji renders as a card grid on the
// site and stays a plain list on GitHub / in llms-full.txt. Content decides the
// layout, so the markdown needs no special syntax to stay portable.
const EMOJI_RE = /^(\p{Extended_Pictographic}(?:\uFE0F|\u200D\p{Extended_Pictographic})*)/u;

function esc(s) {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

// Text going into an ATTRIBUTE (an aria-label built from a heading) must also
// escape the quote: a heading like `What "native" means` would otherwise close
// the attribute one word in and produce broken markup.
function escAttr(s) {
  return esc(s).replace(/"/g, "&quot;");
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
  // Nearest preceding heading, used to label each table's scroll region so a
  // screen reader announces WHICH table the region holds (a bare
  // role="region" with no name is announced as "region" and adds nothing).
  let lastHeading = "";
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
      lastHeading = raw;
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
      const texts = [];
      while (i < lines.length && /^\s*([-*]|\d+\.)\s+/.test(lines[i])) {
        const parts = [lines[i].replace(/^\s*([-*]|\d+\.)\s+/, "")];
        i++;
        // Lazy continuation lines belong to the same item. A blank line,
        // a new bullet, or any other block opener ends the item text.
        while (i < lines.length && lines[i].trim() !== ""
          && !/^\s*([-*]|\d+\.)\s+/.test(lines[i])
          && !/^(#{1,6}\s+>|>|\||---+\s*$|```)/.test(lines[i].trim())) {
          parts.push(lines[i].trim());
          i++;
        }
        texts.push(parts.join(" "));
      }
      if (!ordered && texts.every((t) => EMOJI_RE.test(t))) {
        out.push(`<div class="cards">${texts.map((t) => {
          const m = t.match(EMOJI_RE);
          const rest = t.slice(m[0].length).trim();
          return `<div class="card"><span class="ic" aria-hidden="true">${esc(m[0])}</span>` +
            `<div class="card-body">${inline(rest, pageDir)}</div></div>`;
        }).join("")}</div>`);
        continue;
      }
      const items = texts.map((t) => `<li>${inline(t, pageDir)}</li>`);
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
      // role="region" + tabindex="0" make the scroll box reachable by keyboard:
      // an overflow container that cannot take focus cannot be scrolled with the
      // keyboard at all, which is WCAG 2.1.1 (Keyboard) — the horizontal scroll
      // existed for keyboard users but was unusable by them. tabindex on the
      // wrapper (not the table) keeps the table's own semantics intact.
      const label = lastHeading ? `${lastHeading} table` : "Table";
      out.push(`<div class="table-wrap" role="region" tabindex="0" aria-label="${escAttr(label)}"><table><thead><tr>${head.map((c) => `<th scope="col">${inline(c, pageDir)}</th>`).join("")}</tr></thead><tbody>${
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
  // Releases live outside MANIFEST (CHANGELOG sections); the href is computed
  // for the caller's depth so flat and nested slugs alike resolve to /releases/.
  html += `<h4>Releases</h4><a href="${"../".repeat(cur.split("/").length + 1)}releases/">All releases</a>`;
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
<script>
(function () {
  function copyText(text, done) {
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(text).then(done, function () { fallback(); });
    } else {
      fallback();
    }
    function fallback() {
      var ta = document.createElement("textarea");
      ta.value = text;
      ta.style.position = "fixed";
      ta.style.opacity = "0";
      document.body.appendChild(ta);
      ta.select();
      try { document.execCommand("copy"); } catch (e) {}
      document.body.removeChild(ta);
      done();
    }
  }
  document.querySelectorAll("main pre").forEach(function (pre) {
    var code = pre.querySelector("code");
    if (!code) return;
    var btn = document.createElement("button");
    btn.type = "button";
    btn.className = "copy-btn";
    btn.textContent = "copy";
    btn.setAttribute("aria-label", "Copy command to clipboard");
    btn.addEventListener("click", function () {
      copyText(code.innerText, function () {
        btn.textContent = "copied";
        btn.classList.add("copied");
        setTimeout(function () {
          btn.textContent = "copy";
          btn.classList.remove("copied");
        }, 1400);
      });
    });
    pre.appendChild(btn);
  });

  // ---- scroll affordance on wide tables ------------------------------------
  // A horizontally scrollable box shows nothing to indicate there is more to
  // the right; on macOS the scrollbar is hidden until you already scroll, so a
  // clipped table reads as a truncated table (that is exactly how it was
  // reported: "pages are cut off"). A fade edge that appears only while content
  // is actually off-screen is the cheapest honest signal, and it costs no
  // layout: the gradients paint in the wrapper's own background at its edges.
  function paint(el) {
    var over = el.scrollWidth - el.clientWidth - 1;
    if (over <= 0) {
      el.style.removeProperty("--fade-l");
      el.style.removeProperty("--fade-r");
      return;
    }
    el.style.setProperty("--fade-l", el.scrollLeft > 1 ? "0.28" : "0");
    el.style.setProperty("--fade-r", el.scrollLeft < over ? "0.28" : "0");
  }
  var wraps = document.querySelectorAll(".table-wrap");
  wraps.forEach(function (el) {
    paint(el);
    el.addEventListener("scroll", function () { paint(el); }, { passive: true });
  });
  // Width changes on resize/rotation: repaint once the layout has settled.
  if (wraps.length) {
    var raf = 0;
    window.addEventListener("resize", function () {
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(function () { wraps.forEach(paint); });
    }, { passive: true });
  }
})();
</script>
</body>
</html>`;
}

function check() {
  let failed = 0;
  checkReleases(ROOT, RELEASE_SOURCES);
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
    if (p.group !== group) {
      // Close the previous group BEFORE opening the next one. Every <ul> used
      // to stay open until the end of the loop and was closed by a repeat() at
      // the bottom, which nested each group's list inside the previous group's
      // last <li>: the browser indents every one of them (and a phone eventually
      // overflows the page). One <ul> holds one group, closed where it ends.
      if (group) groups += "</ul>\n";
      group = p.group;
      groups += `<h2>${esc(group)}</h2>\n<ul>`;
    }
    groups += `<li><a href="./${p.slug}/">${esc(p.title)}</a> — ${esc(p.desc)}</li>`;
  }
  if (group) groups += "</ul>\n";
  groups += `<h2>Releases</h2>\n<ul><li><a href="../releases/">All releases</a> — every CHANGELOG.md section as its own article page.</li></ul>`;
  let idxNav = `<a class="home" href="../">← ${NAME}</a>`;
  let idxGroup = "";
  for (const p of MANIFEST) {
    if (p.group !== idxGroup) { idxGroup = p.group; idxNav += `<h4>${esc(p.group)}</h4>`; }
    idxNav += `<a href="./${p.slug}/">${esc(p.title)}</a>`;
  }
  idxNav += `<h4>Releases</h4><a href="../releases/">All releases</a>`;
  mkdirSync(join(OUT, "docs"), { recursive: true });
  writeFileSync(join(OUT, "docs", "index.html"),
    pageShell("Docs", "", `<h1>${esc(NAME)} docs</h1>\n${groups}`, `<a href="../">← ${NAME} home</a>`, idxNav));
  // llms.txt (stable map: one line per page)
  const llms = `# ${NAME} docs\n\n> Full-stack Go web app template that ships as one binary: Turso/SQLite + Templ + Datastar + Tailwind, plus seven complementary async layers (goqite, DagNats, Loro CRDT, SSE Hub record-sync, SSE Hub ephemeral signals, JetStream, GoAkt grains). Full map below; complete texts in llms-full.txt.\n\n` +
    MANIFEST.map((p) => `## ${p.title}\n${p.desc}\n${BASE}/docs/${p.slug}/\n`).join("\n");
  writeFileSync(join(OUT, "llms.txt"), llms);
  // llms-full.txt (concatenated sources)
  const full = MANIFEST.map((p) => `# ${p.title}\n\nSource: docs/${p.file} — ${BASE}/docs/${p.slug}/\n\n${p.md.trim()}\n`).join("\n---\n\n");
  writeFileSync(join(OUT, "llms-full.txt"), `# ${NAME} docs (full)\n\n${full}`);
  // sitemap
  const releaseUrls = buildReleases({ ROOT, OUT, NAME, SOURCES: RELEASE_SOURCES, h: { esc, renderBody, pageShell } });
  const urls = ["", "docs/", ...MANIFEST.map((p) => `docs/${p.slug}/`), ...releaseUrls];
  writeFileSync(join(OUT, "sitemap.xml"),
    `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n` +
    urls.map((u) => `  <url><loc>${BASE}/${u}</loc></url>`).join("\n") + `\n</urlset>\n`);
  console.log(`built ${MANIFEST.length} pages + index + llms.txt + llms-full.txt + sitemap.xml + releases (${releaseUrls.length - 1} versions)`);
}

if (process.argv.includes("--check")) check();
else build();