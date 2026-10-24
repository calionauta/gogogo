// diagram.js — Pele mount helper for GenuiDiagram (<pre class="mermaid">).
// Progressive enhancement: without JS or without Pele the source stays
// readable as code (the <details> fallback in the template). With Pele
// vendored (pele.global.js) each pre becomes an SVG, themed by CSS vars,
// with zoom/pan when it shrinks to fit. Errors render as text via
// textContent — never innerHTML from diagram text.
(function () {
  function mountOne(pre) {
    if (!pre || pre.dataset.diagramMounted) return;
    var pele = window.pele;
    if (!pele || typeof pele.mount !== "function") return;
    try {
      pele.mount(pre, pre.textContent);
      pre.dataset.diagramMounted = "1";
    } catch (e) {
      // Leave the code readable; the details fallback carries the source.
      pre.dataset.diagramMounted = "error";
    }
  }

  function mountAll(root) {
    var scope = root || document;
    var nodes = scope.querySelectorAll
      ? scope.querySelectorAll('pre.mermaid[data-diagram="pele"]:not([data-diagram-mounted])')
      : [];
    for (var i = 0; i < nodes.length; i++) mountOne(nodes[i]);
  }

  // Initial pass + SSE/Datastar patches (MutationObserver survives morphs).
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", function () { mountAll(document); });
  } else {
    mountAll(document);
  }
  if (typeof MutationObserver !== "undefined") {
    var obs = new MutationObserver(function (muts) {
      for (var i = 0; i < muts.length; i++) {
        var m = muts[i];
        if (m.type === "childList") {
          for (var j = 0; j < m.addedNodes.length; j++) {
            var n = m.addedNodes[j];
            if (n.nodeType !== 1) continue;
            if (n.matches && n.matches('pre.mermaid[data-diagram="pele"]')) mountOne(n);
            mountAll(n);
          }
        }
      }
    });
    obs.observe(document.documentElement, { childList: true, subtree: true });
  }
  window.__gogogoMountDiagrams = mountAll;
})();
