// Structural UI checks that need no dependency and no rebuild.
//
// Three families, each picked because a 2026 study says the class of defect is
// real and the check is deterministic:
//
//   accessibleNames — W4A '25: LLM-written UI perpetuates accessibility
//     barriers, and the repair study found the violations cluster in semantic
//     labeling (missing names, labels). Names come from aria-*, a <label>, a
//     title, or a visible placeholder; a placeholder alone passes here and is
//     reported by `--axe` (the WCAG-grade run) rather than by this gate.
//
//   noHorizontalOverflow — DesignBench names "layout disorder" as the dominant
//     failure; a page that scrolls sideways at a phone width is the cheapest
//     detectable form of it.
//
//   demoPanelInsideViewport — the bug that started this: an anchored panel that
//     opens the wrong way renders perfectly and lands off-screen.
//
// These are INVARIANTS, not pixel baselines: the visual-regression study (2026)
// found pixel diffs cost 3.8x longer review with 10x the comments and flag
// differences nobody can see, so we assert the property, not the picture.

/**
 * Check accessible names for form controls, interactive elements and images.
 * @returns {Promise<string[]>} problems, empty when the page is clean
 */
export async function accessibleNameProblems(page) {
  return page.evaluate(() => {
    // Name resolution, in the order assistive tech applies it. Inline (not a
    // string + eval) because the page runs under a CSP: CDP evaluation bypasses
    // it, but a check that would break if it ever became a real script is a
    // check waiting to lie.
    const name = (el) => {
      const labelled = el.getAttribute && el.getAttribute("aria-labelledby");
      if (labelled) {
        const text = labelled.split(/\s+/).map((id) => document.getElementById(id)?.textContent || "").join(" ");
        if (text.trim()) return text;
      }
      if (el.getAttribute && el.getAttribute("aria-label")?.trim()) return el.getAttribute("aria-label");
      if (el.labels && el.labels.length) {
        const text = [...el.labels].map((l) => l.textContent).join(" ");
        if (text.trim()) return text;
      }
      return (el.getAttribute && el.getAttribute("title")) || "";
    };
    const visible = (el) => {
      const rect = el.getBoundingClientRect();
      const style = getComputedStyle(el);
      return rect.width > 0 && rect.height > 0 && style.visibility !== "hidden" &&
        style.display !== "none" && style.opacity !== "0";
    };
    const problems = [];

    document.querySelectorAll("input, select, textarea").forEach((el) => {
      if (el.type === "hidden" || !visible(el)) return;
      if (name(el).trim()) return;
      if ((el.getAttribute("placeholder") || "").trim()) return;
      problems.push(`<${el.tagName.toLowerCase()}${el.type ? `[type=${el.type}]` : ""}> has no accessible name`);
    });

    document.querySelectorAll("button, a[href]").forEach((el) => {
      if (!visible(el)) return;
      if ((el.textContent || "").trim() || name(el).trim()) return;
      if (el.querySelector("svg title")) return;
      problems.push(`<${el.tagName.toLowerCase()}> has no accessible name`);
    });

    document.querySelectorAll("img").forEach((el) => {
      if (!el.hasAttribute("alt")) problems.push("<img> has no alt attribute");
    });

    return problems;
  });
}

/**
 * A page must not scroll sideways: layout disorder, the cheapest detectable
 * form. One pixel of tolerance absorbs sub-pixel rounding.
 */
export async function horizontalOverflow(page) {
  return page.evaluate(() => {
    const doc = document.documentElement;
    const overflow = doc.scrollWidth - doc.clientWidth;
    if (overflow <= 1) return null;
    // Name the widest offender so the failure is actionable.
    let widest = null;
    let worst = 0;
    document.querySelectorAll("body *").forEach((el) => {
      const rect = el.getBoundingClientRect();
      if (rect.right > worst) {
        worst = rect.right;
        widest = el;
      }
    });
    const label = widest
      ? `${widest.tagName.toLowerCase()}${widest.id ? "#" + widest.id : ""}${widest.className && typeof widest.className === "string" ? "." + widest.className.trim().split(/\\s+/).slice(0, 2).join(".") : ""}`
      : "unknown element";
    return `horizontal overflow of ${overflow}px, widest element: ${label}`;
  });
}

/**
 * Assert a dropdown/disclosure panel opened by a trigger sits inside the
 * viewport and on the side it claims. Used for the account menu and the demo
 * panel: both are anchored to an edge and both once opened off-screen.
 */
export async function panelGeometry(page, triggerSelector, panelSelector, { expectAbove = true } = {}) {
  await page.locator(triggerSelector).first().click();
  const panel = page.locator(panelSelector).first();
  await panel.waitFor({ state: "visible", timeout: 5000 });
  const box = await panel.boundingBox();
  const trigger = await page.locator(triggerSelector).first().boundingBox();
  const viewport = page.viewportSize();
  if (!box || !trigger || !viewport) return ["panel geometry unavailable"];
  const problems = [];
  if (box.y < -1 || box.y + box.height > viewport.height + 1) {
    problems.push(`panel is outside the viewport: y=${Math.round(box.y)} h=${Math.round(box.height)} of ${viewport.height}`);
  }
  if (expectAbove && box.y + box.height > trigger.y + 2) {
    problems.push("panel did not open above its trigger");
  }
  return problems;
}

/**
 * Opt-in axe run: the WCAG-grade pass, which this repo keeps optional so the
 * template ships without the dependency.
 *
 * A SKIP IS NEVER SILENT: a check that quietly does nothing is worse than no
 * check, because the suite still goes green. The reason is returned and the
 * smoke prints it.
 * @returns {Promise<{ran: boolean, reason: string, problems: string[]}>}
 */
export async function axeProblems(page, ruleSet = "wcag2a") {
  if (process.env.SMOKE_A11Y !== "1") {
    return { ran: false, reason: "SMOKE_A11Y is not 1", problems: [] };
  }
  let AxeBuilder;
  try {
    ({ default: AxeBuilder } = await import("@axe-core/playwright"));
  } catch (err) {
    return { ran: false, reason: `@axe-core/playwright not installed (${String(err).slice(0, 80)})`, problems: [] };
  }
  const result = await new AxeBuilder({ page }).withTags([ruleSet]).analyze();
  const problems = result.violations
    .filter((v) => v.impact === "serious" || v.impact === "critical")
    .map((v) => `${v.id} (${v.impact}): ${v.nodes.length} node(s)`);
  return { ran: true, reason: "", problems };
}
