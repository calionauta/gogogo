// Shared session banner controller — ONE implementation for every page.
//
// Two signal sources feed the same banner:
//   - live SSE streams (notes, whiteboard) receive a {type:"session"} frame
//     and call GogogoSession.set(authed) immediately;
//   - ONE VISIBLE tab of the browser holds /api/session/stream open and
//     tells the others through a BroadcastChannel. That stream replaces the
//     60s /api/session probe: no tab schedules a request, it hears about
//     CHANGES.
//
// Why one tab and not one per tab: an SSE stream holds its HTTP connection
// for the tab's lifetime and browsers cap connections per host (6 in
// Chromium, HTTP/1.1). A stream per tab halves how many tabs the app can
// run before requests queue forever — measured: three tabs, each opening
// its feature stream plus this one, and the next POST never left the
// browser. Web Locks elects a leader; when its tab closes, the browser
// releases the lock and the next waiting tab takes over; a leader that goes
// hidden releases it too, so a FROZEN background leader can never leave the
// visible tab waiting (the browser's tab-freezing would otherwise stall the
// answer). Without BroadcastChannel + Web Locks the tab runs its own stream
// (the old per-tab behavior) instead of going silent.
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

  // bus carries the leader's answer to the other tabs of this browser.
  var bus = null;
  try {
    bus = window.BroadcastChannel ? new BroadcastChannel("gogogo-session") : null;
  } catch (_) {
    bus = null;
  }

  // publish is the leader's side: update this tab and tell the others.
  function publish(authed) {
    set(authed);
    if (!bus) return;
    try { bus.postMessage({ type: "session", authed: authed }); } catch (_) {}
  }

  // ownStream: this page has no feature stream, so the session banner needs
  // its own connection. Pages that already hold one (the Datastar openers and
  // the raw-EventSource doc pages) must NOT add a second long-lived
  // connection: browsers cap connections per host, and the extra one starves
  // a later same-tab navigation (measured: the smoke's seventh page stalled
  // for its full timeout). Those pages carry the session state on their own
  // stream; `data-stream-page` marks the raw-EventSource pages.
  var ownStream = !document.querySelector("[data-stream-opener], [data-stream-page]");

  // stream is the leader tab's EventSource (nil while none is open).
  var stream = null;
  var leader = false;
  var pending = false; // a lock request is queued (one per tab, never a pile)
  var release = null;  // resolves the Web Lock callback, handing leadership on

  function closeStream() {
    if (!stream) return;
    try { stream.close(); } catch (_) { /* already closed */ }
    stream = null;
  }

  // openStream starts (or keeps) the leader's session stream. Cheap to call
  // repeatedly: while the stream is open it does nothing.
  function openStream() {
    if (stream || !window.EventSource) return;
    try {
      stream = new EventSource("/api/session/stream");
    } catch (_) {
      stream = null;
      return;
    }
    stream.onmessage = function (e) {
      if (!e.data || e.data.charAt(0) === ":") return;
      var m;
      try { m = JSON.parse(e.data); } catch (_) { return; }
      if (m.type !== "session" || typeof m.authed !== "boolean") return;
      publish(m.authed);
      if (!m.authed) {
        // The server closes after authed:false. Close here as well, or
        // EventSource's retry loop would turn "expired" into a request every
        // few seconds — the poll this stream exists to remove. Handing the
        // lock on lets the next tab ask ONCE (bounded by tab count) instead
        // of queueing behind a leader that has nothing left to watch.
        releaseLeader();
      }
    };
  }

  // releaseLeader stops watching and passes the lock to the next tab. The
  // session banner state itself is not reset: the page keeps showing what it
  // knows until a fresh frame says otherwise.
  function releaseLeader() {
    if (!leader) return;
    leader = false;
    closeStream();
    var r = release;
    release = null;
    if (r) r();
  }

  // acquireLeader takes (or waits for) the right to hold the stream. Only a
  // VISIBLE tab asks, so a background tab can never own the socket.
  function acquireLeader() {
    if (!ownStream || document.hidden) return;
    if (leader) {
      openStream();
      return;
    }
    if (pending) return; // already queued; one request per tab
    if (!bus || !navigator.locks) {
      // No coordination available: run the stream locally rather than
      // leaving the banner blind. Costs a connection per tab, as before.
      leader = true;
      openStream();
      return;
    }
    pending = true;
    navigator.locks
      .request("gogogo-session-leader", { mode: "exclusive" }, function () {
        pending = false;
        // A queued request can be granted after the tab went hidden: hand the
        // lock straight on instead of watching from the background.
        if (document.hidden) return;
        leader = true;
        openStream();
        return new Promise(function (resolve) { release = resolve; });
      })
      .catch(function () {
        pending = false;
        leader = true;
        openStream();
      });
  }

  if (bus) {
    bus.onmessage = function (e) {
      var m = e.data;
      if (!m || m.type !== "session" || typeof m.authed !== "boolean") return;
      set(m.authed);
    };
  }

  window.GogogoSession = { set: set, reopen: acquireLeader };

  // Stale-tab reload: a tab open across a deploy holds a DOM whose
  // element IDs and signals belong to the old binary — morphs land
  // nowhere and actions send stale shapes. The server stamps every
  // /health response with X-Gogogo-Build; the first check stores it.
  // On mismatch, STORE FIRST, then reload exactly once: without the
  // store the fresh page would see the same mismatch and reload
  // forever (lived bug: endless refresh loop after every deploy).
  // cache:"no-store" keeps intermediaries from serving a stale tag,
  // which would fake a mismatch the other way.
  function buildCheck() {
    fetch("/health", { headers: { Accept: "text/plain" }, cache: "no-store" })
      .then(function (r) {
        var tag = r.headers.get("X-Gogogo-Build");
        if (!tag) return;
        var known = null;
        try { known = sessionStorage.getItem("gogogo_build"); } catch (_) {}
        if (known === null) {
          try { sessionStorage.setItem("gogogo_build", tag); } catch (_) {}
        } else if (known !== tag) {
          try { sessionStorage.setItem("gogogo_build", tag); } catch (_) {}
          location.reload();
        }
      })
      .catch(function () { /* offline: leave the tab as it is */ });
  }

  var b = el();
  if (b && b.dataset.authed === "true") {
    // Only a page that rendered authed can expire. A visible tab holds the
    // stream; going hidden releases it (so a frozen background tab never
    // stalls the answer) and coming back re-acquires it, which is also the
    // moment a stream closed by expiry or a deploy gets a fresh cookie.
    //
    // Open it AFTER the page's own load, never during it. An SSE stream holds
    // an HTTP/1.1 connection and the browser caps connections per host
    // (6 in Chromium); opening it while the page is still fetching its
    // subresources starves the load event. Measured: the smoke's seventh
    // navigation (/review) stalled for its full 20s timeout with the stream
    // open, and loaded in ~100ms without it. Nothing can expire mid-load, so
    // deferring costs nothing.
    buildCheck();
    if (document.readyState === "complete") {
      acquireLeader();
    } else {
      window.addEventListener("load", acquireLeader, { once: true });
    }
    document.addEventListener("visibilitychange", function () {
      if (document.hidden) {
        releaseLeader();
        return;
      }
      acquireLeader();
      buildCheck();
    });
    window.addEventListener("online", acquireLeader);
    // pagehide is the reliable document-teardown signal (visibilitychange is
    // not fired for every same-tab navigation, and a page entering the
    // back/forward cache keeps its JS alive). Without closing here the
    // EventSource leaks one held HTTP/1.1 connection per visited page: measured
    // 1→2→2→3→3→4 across seven navigations in one tab, and the seventh page
    // (/review) then starved its own load. A bfcache restore re-acquires.
    window.addEventListener("pagehide", releaseLeader);
    window.addEventListener("pageshow", function (e) {
      if (e.persisted && !document.hidden) acquireLeader();
    });
  }
})();
