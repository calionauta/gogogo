// offline-policy.js — the ONE definition of "the request did not complete"
// and of how a retry backs off, shared by the three offline writers:
//
//   - the Service Worker queue          web/resources/static/sw.js
//   - the notes op outbox               features/notes/static/notes.js
//   - the whiteboard op outbox          features/whiteboard/static/whiteboard.js
//
// The three keep their own QUEUES on purpose: the worker survives a closed tab
// and owns the todo endpoints, while notes and whiteboard carry ops that need
// their own storage and conflict rules (Loro merge, 409 correction). The
// POLICY must not fork, which is how a gateway 5xx was transient for the
// worker's replay but a "poison batch" for the notes client — the same answer
// ate the user's typing in one place and retried in the other.
//
// MAINTENANCE: an installed worker re-fetches an imported script only while
// it installs, and it reinstalls only when sw.js itself changes bytes. A change
// to this policy therefore ships with a touch in sw.js (a comment is enough);
// the page-side clients load the bare URL and pick it up on their next load.
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

  // POLICY_VERSION is the CONTENT FINGERPRINT of this file: the first 6 hex of
  // the sha256 of its bytes with the line below blanked. It exists so a PAGE
  // can tell whether the installed WORKER imported this same policy — a worker
  // keeps the copy it imported at install time, so a browser can run a worker
  // from an older deploy for hours after the page updated. The page's copy and
  // the worker's copy answering different values is exactly that case, and the
  // banner says "reload". TestOfflinePolicyVersionMatchesItsDigest recomputes
  // the value, so editing this file without updating it fails CI.
  var POLICY_VERSION = "0d57ff";

  global.GogogoOffline = {
    POLICY_VERSION: POLICY_VERSION,
    isTransientStatus: isTransientStatus,
    isAuthStatus: isAuthStatus,
    retryDelay: retryDelay
  };
})(typeof self !== "undefined" ? self : this);
