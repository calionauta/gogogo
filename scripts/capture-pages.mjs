// Capture every page at desktop and mobile, light and dark, for a visual review.
// Usage: SMOKE_BIN=/tmp/web node scripts/capture-pages.mjs <outdir>
import { chromium } from "playwright";
import { spawn } from "node:child_process";
import { mkdirSync } from "node:fs";

const BIN = process.env.SMOKE_BIN || "./gogogo";
const OUT = process.argv[2] || "/tmp/shots";
const ONLY = (process.argv[3] || "").split(",").filter(Boolean);
const PORT = 19999;
const BASE = `http://127.0.0.1:${PORT}`;

mkdirSync(OUT, { recursive: true });

const server = spawn(BIN, [], {
  env: {
    ...process.env,
    HOST: "127.0.0.1",
    PORT: String(PORT),
    TURSO_DB_PATH: `${OUT}/cap-${process.pid}.db`,
    DATA_DIR: `${OUT}/data`,
    GOGOGO_NO_BROWSER: "1",
    RBAC_ENABLED: "false",
  },
  stdio: "ignore",
});

async function ready() {
  for (let i = 0; i < 120; i++) {
    try {
      const r = await fetch(`${BASE}/health`);
      if (r.ok) return true;
    } catch {}
    await new Promise((r) => setTimeout(r, 250));
  }
  return false;
}

const PAGES = [
  ["landing", "/"],
  ["login", "/login"],
  ["todo", "/todo"],
  ["chat", "/chat"],
  ["genui", "/genui"],
  ["review", "/review"],
  ["room", "/room"],
  ["notes", "/notes"],
  ["whiteboard", "/whiteboard"],
  ["config", "/config"],
];

try {
  if (!(await ready())) throw new Error("server never became ready");
  console.log("server ready");
  const browser = await chromium.launch();
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });

  // sign up a throwaway account and inject its session cookie: the seeded demo
  // password is deliberately not in this file.
  const res = await fetch(`${BASE}/api/auth/signup/credential`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      email: `visual-check-${process.pid}@example.com`,
      password: "visual-check-password-123",
      name: "Visual Check",
    }),
  });
  if (!res.ok) throw new Error(`signup failed: ${res.status}`);
  const cookie = (res.headers.getSetCookie() || [])
    .map((c) => c.split(";")[0])
    .find((c) => c.startsWith("gogogo_auth="));
  if (!cookie) throw new Error("signup set no session cookie");
  const [name, value] = cookie.split("=");
  await context.addCookies([{ name, value, domain: "127.0.0.1", path: "/" }]);
  console.log("signed up");
  const page = await context.newPage();

  for (const [name, path] of PAGES.filter(([n]) => ONLY.length === 0 || ONLY.includes(n))) {
    console.log("capture", name);
    await page.goto(BASE + path, { waitUntil: "load", timeout: 20000 });
    await page.locator("main, header").first().waitFor({ state: "visible", timeout: 3000 }).catch(() => {});
    await page.screenshot({ path: `${OUT}/${name}.png`, fullPage: false });

    // dark: drive the signal the app owns (localStorage + reload). Writing
    // data-theme by hand is reverted: Datastar binds it to $theme.
    await page.evaluate(() => localStorage.setItem("themeMode", "dark"));
    await page.reload({ waitUntil: "load" });
    await page.waitForFunction(() => document.documentElement.classList.contains("dark"), null, {
      timeout: 5000,
    }).catch(() => {});
    await page.screenshot({ path: `${OUT}/${name}-dark.png`, fullPage: false });
    await page.evaluate(() => localStorage.setItem("themeMode", "light"));
    await page.reload({ waitUntil: "load" });
  }

  // demo mode on /todo
  await page.goto(`${BASE}/todo`, { waitUntil: "load" });
  await page.click(".app-demo-toggle", { timeout: 5000 }).catch(() => {});
  await page.waitForSelector("#state-panel", { state: "visible", timeout: 5000 }).catch(() => {});
  await page.screenshot({ path: `${OUT}/todo-demo.png`, fullPage: false });

  // mobile
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${BASE}/todo`, { waitUntil: "load" });
  await page.waitForSelector(".app-rail-toggle", { state: "visible" });
  await page.screenshot({ path: `${OUT}/todo-mobile.png`, fullPage: false });
  await page.click(".app-rail-toggle", { timeout: 5000 }).catch(() => {});
  await page.waitForSelector("#app-rail.is-open", { state: "visible", timeout: 5000 }).catch(() => {});
  await page.screenshot({ path: `${OUT}/todo-mobile-drawer.png`, fullPage: false });

  await browser.close();
  console.log("captured", PAGES.length, "pages to", OUT);
} finally {
  server.kill("SIGTERM");
}
