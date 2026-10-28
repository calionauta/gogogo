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

  global.GogogoOffline = {
    isTransientStatus: isTransientStatus,
    isAuthStatus: isAuthStatus,
    retryDelay: retryDelay
  };
})(typeof self !== "undefined" ? self : this);
