// Capture every page at desktop and mobile, light and dark, for a visual review.
// Usage: SMOKE_BIN=/tmp/web node scripts/capture-pages.mjs <outdir>
import { chromium } from "playwright";
import { spawn } from "node:child_process";
import { mkdirSync } from "node:fs";

const BIN = process.env.SMOKE_BIN || "./gogogo";
const OUT = process.argv[2] || "/tmp/shots";
const PORT = 19999;
const BASE = `http://127.0.0.1:${PORT}`;

mkdirSync(OUT, { recursive: true });

const server = spawn(BIN, [], {
  env: {
    ...process.env,
    HOST: "127.0.0.1",
    PORT: String(PORT),
    TURSO_DB_PATH: `${OUT}/cap.db`,
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
  const browser = await chromium.launch();
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });

  // sign up a throwaway account and inject its session cookie: the seeded demo
  // password is deliberately not in this file.
  const res = await fetch(`${BASE}/api/auth/signup/credential`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      email: "visual-check@example.com",
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
  const page = await context.newPage();

  for (const [name, path] of PAGES) {
    await page.goto(BASE + path, { waitUntil: "load", timeout: 20000 });
    await page.locator("main, header").first().waitFor({ state: "visible" });
    await page.screenshot({ path: `${OUT}/${name}.png`, fullPage: false });

    // dark
    await page.evaluate(() => {
      document.documentElement.dataset.theme = "dark";
      document.documentElement.classList.add("dark");
    });
    await page.waitForFunction(() => document.documentElement.classList.contains("dark"));
    await page.screenshot({ path: `${OUT}/${name}-dark.png`, fullPage: false });
    await page.evaluate(() => {
      document.documentElement.dataset.theme = "light";
      document.documentElement.classList.remove("dark");
    });
  }

  // demo mode on /todo
  await page.goto(`${BASE}/todo`, { waitUntil: "load" });
  await page.click(".app-demo-toggle");
  await page.waitForSelector("#state-panel", { state: "visible" });
  await page.screenshot({ path: `${OUT}/todo-demo.png`, fullPage: false });

  // mobile
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${BASE}/todo`, { waitUntil: "load" });
  await page.waitForSelector(".app-rail-toggle", { state: "visible" });
  await page.screenshot({ path: `${OUT}/todo-mobile.png`, fullPage: false });
  await page.click(".app-rail-toggle");
  await page.waitForSelector("#app-rail.is-open", { state: "visible" });
  await page.screenshot({ path: `${OUT}/todo-mobile-drawer.png`, fullPage: false });

  await browser.close();
  console.log("captured", PAGES.length, "pages to", OUT);
} finally {
  server.kill("SIGTERM");
}
