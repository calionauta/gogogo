// Generates the unit/gate/weight fragments of the Descent page from the real
// sources: docs/unit-contracts.md (rendered from internal/capabilities) and
// .lefthook.yml. The page is committed; rerun this after either source changes.
//
//   node site/build-landing.mjs           # write
//   node site/build-landing.mjs --check   # verify the committed page
import { readFileSync, writeFileSync } from "node:fs";

const here = new URL("./", import.meta.url);
const root = new URL("../", here);
const pageURL = new URL("index.html", here);

const esc = (s) =>
  String(s)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");

function parseUnits(md) {
  const units = [];
  let cur = null;
  for (const line of md.split("\n")) {
    const h = line.match(/^## `([^`]+)` — (core|plugin|feature)\s*$/);
    if (h) {
      cur = { id: h[1], kind: h[2], summary: "", owned: [], runtimeOff: "", trim: "" };
      units.push(cur);
      continue;
    }
    if (!cur) continue;
    if (!cur.summary && line.trim() && !line.startsWith("- ") && !line.startsWith("## ")) {
      cur.summary = line.trim();
      continue;
    }
    const f = line.match(/^- \*\*([^*]+):\*\*\s*(.*)$/);
    if (!f) continue;
    const key = f[1].toLowerCase();
    if (key === "owned") {
      cur.owned = [...f[2].matchAll(/`([^`]+)`/g)].map((m) => m[1]);
    } else if (key === "runtime off") {
      cur.runtimeOff = f[2].replace(/`/g, "").trim();
    } else if (key === "trim") {
      cur.trim = f[2].replace(/`/g, "").replace(/\s+/g, " ").trim();
    }
  }
  return units;
}

function parseLefthook(yaml) {
  const sections = { "pre-commit": [], "pre-push": [], "post-merge": [] };
  let sec = "";
  for (const line of yaml.split("\n")) {
    const s = line.match(/^([a-z-]+):\s*$/);
    if (s && sections[s[1]]) {
      sec = s[1];
      continue;
    }
    const n = line.match(/^\s+- name:\s*(.+)$/);
    if (n && sec) sections[sec].push(n[1].trim());
  }
  return sections;
}

const units = parseUnits(readFileSync(new URL("docs/unit-contracts.md", root), "utf8"));
const hooks = parseLefthook(readFileSync(new URL(".lefthook.yml", root), "utf8"));
const page = readFileSync(pageURL, "utf8");

const byID = new Map(units.map((u) => [u.id, u]));
const unit = (id) => {
  const u = byID.get(id);
  if (!u) throw new Error(`missing unit: ${id}`);
  return u;
};
const pathOf = (u) => (u.owned[0] || "").replace(/^internal\//, "").replace(/\/$/, "");

const display = {
  appconfig: "App config", auth: "Auth", crudwire: "CRUD wire", database: "Database",
  "demo-seed": "Demo seed", hostgate: "Host gate", instance: "Instance",
  observability: "Observability", queue: "Queue", rbac: "RBAC", secrets: "Secrets",
  server: "Server", shell: "Shell",
  app: "App", capabilities: "Capabilities", collab: "Collab", components: "Components",
  credits: "Credits", dagnats: "DagNats", datastar: "Datastar", genkit: "Genkit",
  genui: "GenUI", goakt: "GoAkt", llm: "LLM", nats: "NATS", "offline-sync": "Offline sync",
  search: "Search", sounds: "Sounds",
  account: "Account", chat: "Chat", "config-view": "Config", landing: "Landing",
  notes: "Notes", notifications: "Inbox", review: "Review", room: "Room", todo: "Todo",
  whiteboard: "Whiteboard",
};
const nameOf = (u) => display[u.id] || u.id;

// ---------- features: ten units, four with a hand-built mini-UI ----------

const featureOrder = ["todo", "notes", "whiteboard", "chat", "review", "room", "notifications", "landing", "config-view", "account"];
const featureLinks = {
  todo: "./docs/todo-example/",
  notes: "./docs/features/#what-each-capability-does",
  whiteboard: "./docs/features/#what-each-capability-does",
  chat: "./docs/features/#chat-one-demo-one-vocabulary",
  review: "./docs/review/",
  room: "./docs/features/#what-each-capability-does",
  notifications: "./docs/notifications/",
  landing: "./docs/features/",
  "config-view": "./docs/features/",
  account: "./docs/security/",
};
const featureNotes = {
  todo: "reference · offline-first",
  notes: "text CRDT · live",
  whiteboard: "CRDT · live",
  chat: "generative UI",
  review: "writer · critic · judge",
  room: "roster · presenter lock",
  notifications: "durable · live",
  landing: "this page is a feature",
  "config-view": "operator surface",
  account: "security page",
};
const featureIcons = {
  review: '<svg viewBox="0 0 24 24"><path d="M20 11a8 8 0 1 0-2.3 5.7"/><path d="M20 4v5h-5"/><path d="m9 12 2 2 4-4"/></svg>',
  room: '<svg viewBox="0 0 24 24"><circle cx="9" cy="8" r="3"/><path d="M3 20a6 6 0 0 1 12 0"/><circle cx="17.5" cy="10" r="2.5"/><path d="M15 20a5 5 0 0 1 6.5-3.4"/></svg>',
  notifications: '<svg viewBox="0 0 24 24"><path d="M6 9a6 6 0 0 1 12 0c0 5 2 6 2 6H4s2-1 2-6"/><path d="M10 18a2 2 0 0 0 4 0"/></svg>',
  landing: '<svg viewBox="0 0 24 24"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 9h18"/><path d="M7 6.5h.01M10 6.5h.01"/></svg>',
  "config-view": '<svg viewBox="0 0 24 24"><path d="M4 7h10M18 7h2M4 17h4M12 17h8"/><circle cx="16" cy="7" r="2"/><circle cx="10" cy="17" r="2"/></svg>',
  account: '<svg viewBox="0 0 24 24"><circle cx="12" cy="8" r="3.5"/><path d="M4.5 20a7.5 7.5 0 0 1 15 0"/></svg>',
};
const richFeatures = {
  todo: `<span class="tile-ui todo-ui">
            <span class="todo-row"><i class="box checked">✓</i><i class="bar"></i></span>
            <span class="todo-row"><i class="box"></i><i class="bar"></i></span>
            <span class="todo-row"><i class="box"></i><i class="bar short"></i></span>
          </span>`,
  notes: `<span class="tile-ui notes-ui">
            <span class="text-line"></span><span class="text-line"></span><span class="text-line short"></span>
            <span class="caret" aria-hidden="true"></span>
          </span>`,
  whiteboard: `<span class="tile-ui board-ui">
            <svg viewBox="0 0 100 40" aria-hidden="true">
              <path d="M4 28 C 18 4, 30 36, 44 18 S 68 30, 82 10" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
              <circle cx="20" cy="12" r="2.4"/><circle cx="72" cy="26" r="2.4"/>
            </svg>
          </span>`,
  chat: `<span class="tile-ui chat-ui">
            <span class="bubble left"><i></i><i></i></span>
            <span class="bubble right"><i></i></span>
          </span>`,
};

const features = featureOrder
  .map((id) => {
    const u = unit(id);
    const rich = richFeatures[id];
    const ghost = id === "notes" ? " tile-ghost" : "";
    const dot = " live";
    return `<a class="tile${ghost}" href="${featureLinks[id]}">
          <span class="tile-top"><span class="tile-dot${dot}"></span> ${esc(nameOf(u))}<span class="tile-arrow" aria-hidden="true">↗</span></span>
          ${rich || `<span class="tile-ui generic-ui">${featureIcons[id] || ""}</span>`}
          <span class="tile-desc">${esc(u.summary)}</span>
          <span class="tile-note">${esc(featureNotes[id] || pathOf(u))}</span>
        </a>`;
  })
  .join("\n        ");

const weights = `<div class="weights">
        <p class="weights-title">Client weight <span>measured, not estimated</span></p>
        <p class="weight"><span>Chat diagrams</span><span data-weight="Chat diagrams">~695 KB raw · ~269 KB gzip</span></p>
        <p class="weight"><span>Shared notes</span><span data-weight="Shared notes">~36 KB raw · ~13 KB gzip</span></p>
        <p class="weight"><span>Whiteboard</span><span data-weight="Whiteboard">~56 KB raw · ~20 KB gzip</span></p>
        <p class="weight"><span>Shared shell (every page)</span><span data-weight="Shared shell (every page)">~48 KB raw · ~20 KB gzip</span></p>
        <p class="weight zero"><span>Todo · Room · Landing · Config · Account</span><span>0 KB extra</span></p>
      </div>`;

// ---------- plugins: all fifteen, grouped by the common opt-outs ----------

const commonPlugins = ["nats", "dagnats", "goakt", "llm", "collab", "offline-sync", "search"];
const pluginOrder = [...commonPlugins, ...units.filter((u) => u.kind === "plugin" && !commonPlugins.includes(u.id)).map((u) => u.id)];

function planRow(id) {
  const u = unit(id);
  const env = u.runtimeOff || (u.trim && u.trim.startsWith("offered") ? "installer" : "manual");
  const tag = id === "offline-sync" ? '<span class="plan-tag">async mode</span>' : id === "datastar" ? '<span class="plan-tag">Go seam</span>' : "";
  return `<label class="plan-row">
          <input type="checkbox" checked data-cap="${esc(u.id)}">
          <span class="plan-box" aria-hidden="true"></span>
          <span class="plan-main"><span class="plan-name">${esc(nameOf(u))}${tag}</span><span class="plan-desc">${esc(u.summary)}</span></span>
          <span class="plan-env">${esc(env)}</span>
        </label>`;
}

const planCommon = commonPlugins.map(planRow).join("\n        ");
const planRest = pluginOrder.filter((id) => !commonPlugins.includes(id)).map(planRow).join("\n        ");

const plugins = `<div class="trim-head">
          <span class="trim-title">Trim plan</span>
          <span class="trim-count" id="trim-count" aria-live="polite">${pluginOrder.length} of ${pluginOrder.length} kept</span>
        </div>
        <p class="plan-group">Common opt-outs</p>
        ${planCommon}
        <p class="plan-group">The rest</p>
        ${planRest}
        <p class="trim-summary" id="trim-summary" aria-live="polite">Every plugin unit on. The binary still builds.</p>
        <p class="trim-note">Every plugin unit from the unit contracts. A preview, not a live switch; the real switch is the env var or the trim.</p>`;

// ---------- core: four anchors + nine bricks ----------

const coreAnchors = ["database", "auth", "server", "queue"];
const coreOrder = [...coreAnchors, ...units.filter((u) => u.kind === "core" && !coreAnchors.includes(u.id)).map((u) => u.id)];
const coreIcons = {
  database: '<svg viewBox="0 0 24 24"><ellipse cx="12" cy="5" rx="7" ry="3"/><path d="M5 5v6c0 1.7 3.1 3 7 3s7-1.3 7-3V5"/><path d="M5 11v6c0 1.7 3.1 3 7 3s7-1.3 7-3v-6"/></svg>',
  auth: '<svg viewBox="0 0 24 24"><rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/></svg>',
  server: '<svg viewBox="0 0 24 24"><rect x="3" y="13" width="18" height="7" rx="2"/><path d="M7 16h.01M11 16h.01M7 9a5 5 0 0 1 10 0"/></svg>',
  queue: '<svg viewBox="0 0 24 24"><path d="M4 6h16M4 12h16M4 18h10"/></svg>',
};

const anchors = coreAnchors
  .map((id) => {
    const u = unit(id);
    return `<div class="pillar">
            ${coreIcons[id]}
            <span class="pillar-name">${esc(nameOf(u))}</span>
            <span class="pillar-lock">anchor · locked</span>
            <span class="pillar-desc">${esc(u.summary)}</span>
          </div>`;
  })
  .join("\n          ");

const bricks = coreOrder
  .filter((id) => !coreAnchors.includes(id))
  .map((id) => {
    const u = unit(id);
    return `<div class="brick"><span class="brick-name">${esc(nameOf(u))}</span><span class="brick-desc">${esc(u.summary)}</span><span class="brick-path">${esc(pathOf(u))}</span></div>`;
  })
  .join("\n          ");

const core = `<div class="pillars">
          ${anchors}
        </div>
        <div class="bricks">
          ${bricks}
        </div>
        <div class="core-actions">
          <button class="try-remove" type="button" id="try-remove">Try to remove one</button>
          <p class="refusal" id="refusal" hidden>It won't build. That is the line between trimming and forking.</p>
        </div>`;

// ---------- gates: every hook, grouped by when it runs ----------

const gateNotes = {
  "file-sizes": "a Go file over the size limit",
  "fmt-gofumpt": "unformatted Go",
  "mod-tidy": "a dirty go.mod/go.sum",
  govulncheck: "a known CVE in a dependency",
  "ci-deps": "a workflow that uses Node or Go without installing it",
  "install-sh": "an installer that plants a go entry in $BIN_DIR",
  "scope-lint": "a file without a SCOPE line",
  "class-integrity": "a DaisyUI class not in the registry",
  "daisyui-lint": "a retired or typo'd DaisyUI class",
  "flatdeps-lint": "a dependency that breaks the flat stack",
  "test-value": "a test that cannot fail",
  "docs-claims": "a hand-written count or a dead path",
  "secrets-env": "a secret read outside the loader",
  "rbac-policy": "a route with no user row in the policy",
  "dagnats-pin": "a DagNats version outside the pin",
  "secrets-scan": "a key in a commit",
  "skill-frontmatter": "a skill not registered for the hosts",
  "datastar-lint": "a Datastar attribute that does nothing",
  "api-routes": "a route missing from the generated inventory",
  "mcp-tests": "an MCP tool whose test stopped pinning it",
  "registry-conformance": "a unit that left the registry",
  "generated-templ": "a committed _templ.go that drifted",
  "css-check": "a stale app.min.css",
  "no-jsx": "a template that slipped into JSX",
  "golangci-lint": "unchecked errors, leaked contexts, template mistakes",
  "agents-md-staleness": "AGENTS.md drifted from the tree",
  "stelow-drift": "the vendored stelow skill drifted",
  "daisyui-drift": "the vendored DaisyUI skill drifted",
  "docs-staleness": "docs that did not move with the code",
  deadcode: "code no test or route reaches",
  "regen-assets": "templ/css artifacts out of date",
};

function gateChips(names, offset) {
  return names
    .map((name, i) => {
      const note = gateNotes[name] || "a hook in .lefthook.yml";
      const idx = offset + i;
      return `<button class="gate" type="button" aria-describedby="gate-note" style="--i:${idx}" data-note="${esc(note)}">${esc(name)}</button>`;
    })
    .join("\n          ");
}

const gates = `<p class="gate-note" id="gate-note" aria-live="polite">Focus or tap a gate to see what it catches.</p>
        <p class="gate-group">pre-commit</p>
        <div class="gate-row">
          ${gateChips(hooks["pre-commit"], 0)}
        </div>
        <p class="gate-group">pre-push</p>
        <div class="gate-row">
          ${gateChips(hooks["pre-push"], hooks["pre-commit"].length)}
        </div>
        <p class="gate-group">post-merge</p>
        <div class="gate-row">
          ${gateChips(hooks["post-merge"], hooks["pre-commit"].length + hooks["pre-push"].length)}
          <span class="gate gate-merge">merge</span>
        </div>
`;

// ---------- inject ----------

function between(source, start, end, body) {
  const i = source.indexOf(start);
  const j = source.indexOf(end);
  if (i < 0 || j < 0 || j < i) throw new Error(`missing markers ${start} / ${end}`);
  return source.slice(0, i + start.length) + "\n        " + body + "\n        " + source.slice(j);
}

let out = page;
out = between(out, "<!--FEATURES:START-->", "<!--FEATURES:END-->", features);
out = between(out, "<!--WEIGHTS:START-->", "<!--WEIGHTS:END-->", weights);
out = between(out, "<!--PLUGINS:START-->", "<!--PLUGINS:END-->", plugins);
out = between(out, "<!--CORE:START-->", "<!--CORE:END-->", core);
out = between(out, "<!--GATES:START-->", "<!--GATES:END-->", gates);

const check = process.argv.includes("--check");
if (check) {
  if (out !== page) {
    console.error("site/index.html is stale: run `node site/build-landing.mjs` and commit the page.");
    process.exit(1);
  }
  console.log("landing ok: generated blocks match the unit contracts and .lefthook.yml");
} else {
  writeFileSync(pageURL, out);
  console.log("wrote site/index.html");
}
console.log(`units ${units.length} (core ${units.filter((u) => u.kind === "core").length}, plugin ${units.filter((u) => u.kind === "plugin").length}, feature ${units.filter((u) => u.kind === "feature").length})`);
console.log(`gates ${hooks["pre-commit"].length + hooks["pre-push"].length + hooks["post-merge"].length}`);
