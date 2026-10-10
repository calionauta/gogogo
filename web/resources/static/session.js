// Shared session banner controller — ONE implementation for every page.
//
// Two signal sources feed the same banner:
//   - live SSE streams (notes, whiteboard) receive a {type:"session"} frame
//     and call GogogoSession.set(authed) immediately;
//   - pages without a raw stream (todo, room, config) get the same signal
//     from the /api/session probe below.
//
// A tab only warns when the page RENDERED authed and the session is later
// gone (the banner element carries data-authed with the render-time state).
// A public page — landing, or a logged-out visitor — never nags about a
// session it never had. The banner never auto-redirects: that would strand
// unsent textarea/board content; the user re-logs in and keeps working.
(function () {
  "use strict";

  function el() {
    return document.getElementById("session-banner");
  }

  // set shows (authed=false) or hides (authed=true) the banner. Safe to
  // call when the page has no banner — the call is simply a no-op.
  function set(authed) {
    var b = el();
    if (!b) return;
    if (authed) b.classList.add("hidden");
    else b.classList.remove("hidden");
  }

  // probe asks the server whether the request still carries a valid app
  // cookie. Used by pages that have no SSE session frame of their own.
  function probe() {
    fetch("/api/session", { headers: { Accept: "application/json" } })
      .then(function (r) { return r.ok ? r.json() : null; })
      .then(function (d) {
        if (d && typeof d.authed === "boolean") set(d.authed);
      })
      .catch(function () { /* offline: leave the banner as it is */ });
  }

  window.GogogoSession = { set: set, probe: probe };

  // Stale-tab reload: a tab open across a deploy holds a DOM whose
  // element IDs and signals belong to the old binary — morphs land
  // nowhere and actions send stale shapes. The server stamps every
  // /health response with X-Gogogo-Build; the first check stores it,
  // a later mismatch reloads so the tab re-renders from the new server.
  // Same triggers as the session probe (authed pages only, load +
  // foreground + heartbeat), same offline tolerance (fetch rejects →
  // skip). In dev the tag is a constant ("dev/"), so Air rebuilds never
  // cause reloads. Single-binary deploys only: under a rolling deploy
  // alternating builds, each flip would reload once.
  function buildCheck() {
    fetch("/health", { headers: { Accept: "text/plain" } })
      .then(function (r) {
        var tag = r.headers.get("X-Gogogo-Build");
        if (!tag) return;
        var known = null;
        try { known = sessionStorage.getItem("gogogo_build"); } catch (_) {}
        if (known === null) {
          try { sessionStorage.setItem("gogogo_build", tag); } catch (_) {}
        } else if (known !== tag) {
          location.reload();
        }
      })
      .catch(function () { /* offline: leave the tab as it is */ });
  }

  var b = el();
  if (b && b.dataset.authed === "true") {
    // Only a page that rendered authed can expire. Probe on load, on
    // return-to-foreground (the moment a long-open tab most needs it),
    // and on a slow heartbeat so a tab left open for days still notices.
    probe();
    buildCheck();
    document.addEventListener("visibilitychange", function () {
      if (!document.hidden) { probe(); buildCheck(); }
    });
    setInterval(probe, 60000);
  }
})();
