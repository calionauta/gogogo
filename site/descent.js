/* Descent: scroll drives one active level; the stage and the chapter share it.
   No dependencies. Content lives in the HTML; this file only owns state. */
(function () {
  "use strict";

  const root = document.documentElement;
  root.classList.add("js");

  const stage = document.getElementById("stage");
  const levelEl = document.getElementById("stage-level");
  const railEl = document.getElementById("stage-rail");
  const nameEl = document.getElementById("stage-name");
  const lineEl = document.getElementById("stage-line");
  const metaEl = document.getElementById("stage-meta");
  const bands = Array.from(document.querySelectorAll(".band[data-layer]"));
  const chapters = Array.from(document.querySelectorAll(".chapter[data-layer]"));
  const chips = Array.from(document.querySelectorAll(".chips [data-chip]"));
  let active = "";

  function setLevel(index) {
    if (levelEl) levelEl.textContent = String(index + 1).padStart(2, "0");
    if (railEl) railEl.style.setProperty("--level", String((index + 1) / Math.max(1, chapters.length)));
  }

  function setActive(id) {
    if (!id || id === active) return;
    const chapter = chapters.find((c) => c.dataset.layer === id);
    if (!chapter) return;

    active = id;
    chapters.forEach((c) => c.classList.toggle("is-active", c === chapter));
    bands.forEach((b) => b.toggleAttribute("data-active", b.dataset.layer === id));
    chips.forEach((c) => c.setAttribute("aria-current", c.dataset.chip === id ? "true" : "false"));

    stage.setAttribute("data-active", id);
    nameEl.textContent = chapter.dataset.name;
    lineEl.textContent = chapter.dataset.line;
    metaEl.textContent = chapter.dataset.scope;

    const index = chapters.indexOf(chapter);
    setLevel(index);
    stage.style.setProperty("--p", String(index / Math.max(1, chapters.length - 1)));
  }

  function pick() {
    const mid = window.innerHeight * 0.52;
    let best = null;
    // The chapter under the reading line wins. The old nearest-center rule
    // kept a short previous chapter active while a tall next chapter filled
    // the viewport (and made a chip jump report the wrong level).
    for (const chapter of chapters) {
      const rect = chapter.getBoundingClientRect();
      if (rect.top <= mid && rect.bottom > mid) {
        best = chapter;
        break;
      }
    }
    if (!best) {
      let bestDistance = Infinity;
      for (const chapter of chapters) {
        const rect = chapter.getBoundingClientRect();
        const distance = Math.abs(rect.top + rect.height / 2 - mid);
        if (distance < bestDistance) {
          bestDistance = distance;
          best = chapter;
        }
      }
    }
    if (best) setActive(best.dataset.layer);
  }

  let ticking = false;
  function onScroll() {
    if (ticking) return;
    ticking = true;
    window.requestAnimationFrame(function () {
      ticking = false;
      pick();
    });
  }

  window.addEventListener("scroll", onScroll, { passive: true });
  window.addEventListener("resize", onScroll, { passive: true });
  pick();

  /* Core: the refusal is the point. */
  const tryRemove = document.getElementById("try-remove");
  const refusal = document.getElementById("refusal");
  const pillars = document.querySelector(".pillars");
  let refusalTimer = 0;

  if (tryRemove && refusal && pillars) {
    tryRemove.addEventListener("click", function () {
      pillars.classList.remove("shake");
      void pillars.offsetWidth;
      pillars.classList.add("shake");
      refusal.hidden = false;
      window.clearTimeout(refusalTimer);
      refusalTimer = window.setTimeout(function () {
        refusal.hidden = true;
      }, 3400);
    });
  }

  /* Gates: each chip explains what it catches. */
  const gateNote = document.getElementById("gate-note");
  const gates = Array.from(document.querySelectorAll(".gate[data-note]"));
  const gateDefault = gateNote ? gateNote.textContent : "";

  gates.forEach(function (gate) {
    function show() {
      if (gateNote) gateNote.textContent = gate.dataset.note;
    }
    function reset() {
      if (gateNote) gateNote.textContent = gateDefault;
    }
    gate.addEventListener("pointerenter", show);
    gate.addEventListener("pointerleave", reset);
    gate.addEventListener("focus", show);
    gate.addEventListener("blur", reset);
  });

  /* Plugins: the checkboxes change a plan, not the product. */
  const planInputs = Array.from(document.querySelectorAll(".plan-row input"));
  const trimCount = document.getElementById("trim-count");
  const trimSummary = document.getElementById("trim-summary");

  function updateTrim() {
    if (!planInputs.length) return;
    const kept = planInputs.filter(function (input) { return input.checked; });
    const dropped = planInputs.filter(function (input) { return !input.checked; });
    if (trimCount) trimCount.textContent = kept.length + " of " + planInputs.length + " kept";
    if (trimSummary) {
      trimSummary.textContent = dropped.length
        ? dropped.map(function (input) { return input.dataset.cap; }).join(", ") + " off. The binary still builds."
        : "Every capability on. The binary still builds.";
    }
  }

  planInputs.forEach(function (input) {
    input.addEventListener("change", updateTrim);
  });
  updateTrim();
})();
