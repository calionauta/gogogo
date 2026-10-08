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

  var b = el();
  if (b && b.dataset.authed === "true") {
    // Only a page that rendered authed can expire. Probe on load, on
    // return-to-foreground (the moment a long-open tab most needs it),
    // and on a slow heartbeat so a tab left open for days still notices.
    probe();
    document.addEventListener("visibilitychange", function () {
      if (!document.hidden) probe();
    });
    setInterval(probe, 60000);
  }
})();
