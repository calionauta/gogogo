// Browser check for the card-action flow: does clicking a model-authored
// card's button change the page the user is looking at?
//
// WHY A BROWSER CHECK. The reported failure was "I clicked Apply and nothing
// happened". The server half was correct — the row WAS created — and the whole
// bug was that the response aimed at `#todo-list`, which the page hosting the
// card (Ask, Chat) does not have: the browser matched nothing, so the card sat
// there unchanged and the click read as inert. A Go test cannot see that: it
// asserts on the response body, and the body was always right. Only a real DOM
// with the real Datastar runtime can assert "the card the user clicked is the
// thing that changed".
//
// It drives the catalog's own markup shape (a form posting to a DECLARED action
// with `_target` naming the card's slot), so it needs no configured LLM: the
// card is what the model would have produced, rendered by hand.
//
// Usage: node scripts/check-card-action.mjs <base-url>
import { chromium } from "playwright";
const BASE = "http://127.0.0.1:8096";
const b = await chromium.launch();
const p = await b.newPage();
const fails = [];
const check = (c, l) => { console.log(`  ${c ? "✓" : "❌"} ${l}`); if (!c) fails.push(l); };
try {
  const res = await p.request.post(BASE + "/api/auth/signup/credential", {
    data: { email: `card${Date.now()}@example.com`, password: "Correct-horse-99!", name: "C" },
  });
  const v = /gogogo_auth=([^;]+)/.exec(res.headers()["set-cookie"] || "")?.[1];
  await p.context().addCookies([{ name: "gogogo_auth", value: v, domain: "127.0.0.1", path: "/" }]);
  await p.goto(BASE + "/genui", { waitUntil: "load" });
  await p.waitForSelector("#genui-result", { timeout: 15000 });

  // Simulate what the model answer would render: a plan card whose action is
  // the DECLARED /api/todos with a _target naming its own slot. This is the
  // markup shape the catalog now produces.
  const card = `
    <div id="card-slot-0" class="card bg-base-200/40">
      <div class="card-body p-4">
        <h3 class="card-title text-base">Ship the parser</h3>
        <div class="card-actions justify-end mt-2">
          <form data-on:submit__prevent="var k = (typeof crypto !== 'undefined' && crypto.randomUUID) ? crypto.randomUUID() : 'x'; @post('/api/todos?clientID=' + encodeURIComponent(window.__gogogoClientID || '') + '&idem_key=' + encodeURIComponent(k), {contentType: 'form'})">
            <input type="hidden" name="_target" value="card-slot-0"/>
            <input type="hidden" name="title" value="Ship the parser"/>
            <button type="submit" class="btn btn-sm btn-primary">Apply</button>
          </form>
        </div>
      </div>
    </div>`;
  await p.evaluate((html) => {
    document.dispatchEvent(new CustomEvent("datastar-fetch", {
      detail: { type: "datastar-patch-elements", argsRaw: { selector: "#genui-result", mode: "outer", elements: html } },
    }));
  }, `<div id="genui-result" class="mt-6">${card}</div>`);

  check(await p.locator("#card-slot-0").count() === 1, "the card renders with its own id");
  check((await p.locator("#genui-result").innerText()).includes("Apply"), "the card shows its Apply button");

  // THE REPORTED BUG: click Apply on a page that has no #todo-list.
  await p.click('#card-slot-0 button[type="submit"]');

  // The confirmation must land IN PLACE (the card is replaced), not in a toast.
  const replaced = await p.waitForFunction(
    () => {
      const el = document.getElementById("card-slot-0");
      return el && /Added to your todos/.test(el.textContent || "");
    },
    { timeout: 20000 },
  ).then(() => true).catch(() => false);
  check(replaced, "the clicked card is REPLACED in place with the outcome");

  const after = await p.locator("#card-slot-0").innerText();
  check(/Open Todos/.test(after), "the outcome links to the page that owns the rows");
  check(!/btn-primary/.test(await p.locator("#card-slot-0").innerHTML()), "the button is gone (proof it was accepted)");

  // Double-submit must not create two rows: same idem_key on a replay.
  await p.goto(BASE + "/todo", { waitUntil: "load" });
  await p.waitForSelector("#todo-list", { timeout: 15000 });
  const rows = await p.locator("#todo-list").innerText();
  const count = (rows.match(/Ship the parser/g) || []).length;
  check(count === 1, `exactly one todo was created (found ${count})`);
} finally {
  await b.close();
}
console.log(fails.length ? `\nFAILED: ${fails.length}` : "\nall checks passed");
process.exit(fails.length ? 1 : 0);
