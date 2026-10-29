// offline-policy.js — the ONE definition of "the request did not complete"
// and of how a retry backs off, shared by the three offline writers:
//
//   - the todo outbox                   web/resources/static/offline-outbox.js
//   - the notes op outbox               features/notes/static/notes.js
//   - the whiteboard op outbox          features/whiteboard/static/whiteboard.js
//
// The three keep their own QUEUES on purpose: todo entries are independent
// requests, while notes and whiteboard carry ops that need their own storage
// and conflict rules (Loro merge, 409 correction). The POLICY must not fork,
// which is how a gateway 5xx was transient for one queue but a "poison batch"
// for the notes client — the same answer ate the user's typing in one place
// and retried in the other.
//
// Three things are shared here beyond the retry vocabulary: the cross-tab
// drain lock (withLock), the report for a queue that could not be persisted
// (reportStorageDrop), and the banner's persisted lost-change notice.
//
// The Service Worker does NOT import this file: it no longer intercepts the
// API, so there is no worker-side policy to keep in step and no version
// handshake between the page's copy and the worker's.
(function (global) {
  "use strict";

  // isTransientStatus: the request never completed, so it is NOT a verdict on
  // the mutation and must be kept and retried.
  //
  //   - 5xx — the gateway or the app did not answer. Behind Cloudflare an
  //     unreachable origin is 520/521/522/523/524 and a dead upstream is 502;
  //     the handler never ran. (A plain 500 is the app failing mid-flight: a
  //     todo create is still safe because it carries idem_key and the replay
  //     is deduped server-side.)
  //   - 429 — rate limited before the handler ran.
  //   - 408 — the edge gave up waiting before the handler ran.
  //
  // Anything else (2xx/3xx and the remaining 4xx) is a DEFINITIVE answer: the
  // mutation either landed or was refused.
  function isTransientStatus(status) {
    return status === 408 || status === 429 || status >= 500;
  }

  // isAuthStatus: the ops are fine, the SESSION is missing or refused. Keep
  // them — dropping would eat the user's work — but retrying cannot fix it
  // until someone signs in, so callers that can distinguish should not treat
  // it like a generic transient answer.
  function isAuthStatus(status) {
    return status === 401 || status === 403;
  }

  // retryDelay: doubles the current wait from `initial`, never past `cap`.
  // ONE curve, because both ends are defects: a fixed 300ms loop against a
  // failing edge is a request storm, and a floor so high that a recovered app
  // still looks broken is a lie about the state.
  function retryDelay(current, initial, cap) {
    var base = current || initial;
    var next = base * 2;
    return next > cap ? cap : next;
  }

  // withLock runs fn under a Web Lock named by the caller, WAITING for the
  // holder to finish. Every local queue is per-browser, not per-tab: two tabs of
  // the same note/board share one localStorage/IndexedDB queue, and without a
  // lock both tabs replay the SAME batch — the notes wire repeats an insert (ops
  // are not idempotent) and the board sends one op twice. The todo outbox
  // already serialised its drain this way; this is the same primitive for the
  // two feature queues.
  //
  // WAITING, not ifAvailable: the caller re-reads the store under the lock and
  // stops when a peer already delivered everything, which is the correct answer.
  // Skipping would leave this tab's in-memory view of the queue stale. The
  // caller must therefore NOT start another drain from inside fn — a same-agent
  // re-request queues behind itself and never resolves.
  //
  // No Web Locks (older Safari) = no cross-tab exclusion: running fn is the
  // pre-existing behaviour, and a false lock would be worse than none.
  function withLock(name, fn) {
    if (!global.navigator || !global.navigator.locks) {
      return Promise.resolve().then(fn);
    }
    return global.navigator.locks.request(name, fn);
  }

  // ---- lost-change notice -------------------------------------------------
  //
  // A storage failure is a DROPPED mutation: the queue could not keep the
  // intent, so it exists only in memory and dies with the tab. Swallowing that
  // is the "silent catch" the coding standards forbid, and the user has no way
  // to tell a saved change from a lost one. The banner already renders drops;
  // this reports one through the same event and persists it, because the page
  // that discovered the failure may be the one that reloads next.
  var DROP_KEY = "gogogo:drop-notice";
  var reported = {};

  function reportStorageDrop(scope, err) {
    if (global.console && console.warn) {
      console.warn("offline: could not persist a queued change (" + scope + ")", err);
    }
    // One notice per scope per page: a failing quota would otherwise fire on
    // every keystroke and turn the banner into a counter of retries.
    if (reported[scope]) return;
    reported[scope] = true;
    try {
      global.dispatchEvent(new CustomEvent("gogogo:outbox", {
        detail: { dropped: 1, reason: "storage" }
      }));
    } catch (_) { /* no window: nothing to tell */ }
  }

  function rememberDrop(n, why) {
    try {
      global.localStorage.setItem(DROP_KEY, JSON.stringify({ n: n, why: why || "" }));
    } catch (_) { /* storage blocked: the banner still shows this page */ }
  }

  function readDrop() {
    try {
      var raw = global.localStorage.getItem(DROP_KEY);
      if (!raw) return null;
      var d = JSON.parse(raw);
      if (!d || typeof d.n !== "number" || d.n <= 0) return null;
      return { n: d.n, why: typeof d.why === "string" ? d.why : "" };
    } catch (_) {
      return null;
    }
  }

  function forgetDrop() {
    try { global.localStorage.removeItem(DROP_KEY); } catch (_) { /* blocked */ }
  }

  global.GogogoOffline = {
    isTransientStatus: isTransientStatus,
    isAuthStatus: isAuthStatus,
    retryDelay: retryDelay,
    withLock: withLock,
    reportStorageDrop: reportStorageDrop,
    rememberDrop: rememberDrop,
    readDrop: readDrop,
    forgetDrop: forgetDrop
  };
})(typeof self !== "undefined" ? self : this);
