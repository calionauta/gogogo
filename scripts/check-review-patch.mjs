// Browser check for the review result's patch target.
//
// WHY THIS EXISTS AS A BROWSER CHECK. The server always emitted the patch; the
// bug was entirely client-side. Datastar's default `outer` mode morphs the
// target and SYNCS ATTRIBUTES FROM THE FRAGMENT, so a verdict fragment whose
// root element was not the target stripped `id="review-result"` off the live
// node — and every later run then matched nothing (PatchElementsNoTargetsFound)
// while the previous verdict stayed on screen. The user reported exactly that:
// "the result does not change and references the previous result".
//
// A Go test pins that the component's root carries the id. This check pins the
// half no Go test can: that two successive patches through the real Datastar
// runtime on a real page leave the region replaceable. It needs no LLM, so it
// can run anywhere — the fragments it patches are the ones the Go test asserts.
//
// Usage: node scripts/check-review-patch.mjs <base-url>
import { chromium } from "playwright";

const BASE = process.argv[2] || "http://127.0.0.1:8099";

const failures = [];
function check(cond, label) {
  console.log(`  ${cond ? "✓" : "❌"} ${label}`);
  if (!cond) failures.push(label);
}

// verdictFragment mirrors reviewcomponents.ReviewResult's root contract: the
// element carrying `id="review-result"` IS the fragment root (that is the
// assurance features/review/components/review_contract_test.go pins, failing
// the build if the id is dropped). Built here rather than fetched so this check
// needs no configured LLM.
const verdictFragment = (verdict) =>
  `<div id="review-result" class="card bg-base-200 shadow"><div class="card-body">` +
  `<h2 class="card-title">Verdict: ${verdict}</h2><p class="text-sm">rationale ${verdict}</p>` +
  `<h3 class="font-semibold mt-3 mb-1">Proposal</h3><pre>proposal ${verdict}</pre>` +
  `</div></div>`;

// verdictText reads the region's text, returning "" when the region is GONE
// rather than throwing. A missing region IS the bug this check exists for, so
// it has to read as a failed assertion (with a clear label) and not as a
// timeout stack trace — a check that crashes still "fails", but the operator
// cannot tell which guarantee broke.
async function verdictText(page) {
  const el = page.locator("#review-result");
  if ((await el.count()) === 0) return "";
  return el.innerText({ timeout: 5000 }).catch(() => "");
}

async function signup(page) {
  const email = `patchcheck-${Date.now()}@example.com`;
  const res = await page.request.post(BASE + "/api/auth/signup/credential", {
    data: { email, password: "Correct-horse-99!", name: "Patch Check" },
  });
  if (!res.ok()) throw new Error(`signup ${res.status()}: ${await res.text()}`);
  const setCookie = res.headers()["set-cookie"] || "";
  const value = /gogogo_auth=([^;]+)/.exec(setCookie)?.[1];
  if (!value) throw new Error(`no gogogo_auth in: ${setCookie}`);
  const { hostname } = new URL(BASE);
  await page.context().addCookies([{ name: "gogogo_auth", value, domain: hostname, path: "/" }]);
}

const browser = await chromium.launch();
const page = await browser.newPage();
const consoleErrors = [];
page.on("console", (m) => {
  if (m.type() === "error") consoleErrors.push(m.text());
});

try {
  await signup(page);
  const res = await page.goto(BASE + "/review", { waitUntil: "load" });
  if (res.status() !== 200) throw new Error(`GET /review = ${res.status()}`);

  // Wait for Datastar to have bound its listener, or the dispatched event is
  // dropped and this check would pass vacuously.
  await page.waitForFunction(() => !!document.documentElement.dataset.signals, { timeout: 15000 });

  check(
    (await page.locator("#review-result").count()) === 1,
    "the page ships an empty #review-result target",
  );

  // Fire the patch exactly the way the SSE reader does for a real run.
  const patch = (html) =>
    page.evaluate((elements) => {
      document.dispatchEvent(
        new CustomEvent("datastar-fetch", {
          detail: {
            type: "datastar-patch-elements",
            argsRaw: { selector: "#review-result", mode: "outer", elements },
          },
        }),
      );
    }, html);

  await patch(verdictFragment("first"));
  const afterFirst = await verdictText(page);
  check(afterFirst.includes("proposal first"), "run 1 renders its verdict");
  check(
    (await page.locator("#review-result").count()) === 1,
    "the target id survives run 1's morph (the reported bug's root cause)",
  );

  // THE REPORTED BUG: a second patch must replace the card.
  await patch(verdictFragment("second"));
  const afterSecond = await verdictText(page);
  check(
    afterSecond.includes("proposal second") && !afterSecond.includes("proposal first"),
    "a second patch REPLACES the first verdict",
  );
  check(
    (await page.locator("#review-result").count()) === 1,
    "the target id survives run 2 too (so run 3 has a target)",
  );

  const noTargets = consoleErrors.filter((e) => /NoTargetsFound/i.test(e));
  check(noTargets.length === 0, "no PatchElementsNoTargetsFound on the console");
  if (noTargets.length) console.log("    ", noTargets);
} finally {
  await browser.close();
}

console.log(failures.length ? `\nFAILED: ${failures.length}` : "\nall checks passed");
process.exit(failures.length ? 1 : 0);
