// Service Worker — app-shell cache only.
//
// WHAT THIS WORKER DOES: it keeps a visited page available when the network is
// gone (network-first navigation with a cache fallback) and purges caches from
// previous versions. Nothing else.
//
// WHAT IT DELIBERATELY DOES NOT DO: it never touches /api/*. Offline writes are
// owned by web/resources/static/offline-outbox.js, which stores the user's
// intent BEFORE the request leaves the page and replays it with an
// Idempotency-Key the server dedupes. That split is the whole design:
//
//   - a fetch interceptor can only queue what the browser decided to send, and
//     it cannot see an intent that a hanging request never delivered;
//   - a page-side outbox sees the click, so it can persist the intent first;
//   - two queues (worker + page) meant two schemas, two retry policies and a
//     version handshake between them.
//
// The consequence is honest: a queued write replays while the app is open (on
// the next load, on `online`, or on visibility), not while every tab is
// closed. Background Sync — the only API that could do otherwise — is
// Chromium-only (Firefox has a negative position, Safari does not implement
// it), so it was never a cross-browser guarantee.
//
// CACHE IS NOT SHARED ACROSS USERS: only successful same-origin HTML
// navigations are cached, and sign-out clears the origin's storage with the
// Clear-Site-Data header (see the logout handler), which drops this cache and
// the outbox together.

var PAGE_CACHE = "gogogo-pages-v2";

// OFFLINE_PAGE is what a never-visited route shows while offline. It carries no
// user data and no script: a cached page is the only thing that can render the
// app, and this is the honest fallback when there is none.
var OFFLINE_PAGE = '<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Offline</title><style>body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#1d232a;color:#a6adbb;text-align:center;padding:1rem}@media(prefers-color-scheme:light){body{background:#fff;color:#374151}}h1{font-size:1.5rem;margin-bottom:.5rem}p{font-size:.875rem;line-height:1.5;max-width:24rem}</style></head><body><div><h1>You are offline</h1><p>This page has not been visited before, or the cached version expired. Connect to the internet and try again.</p></div></body></html>';

self.addEventListener("install", function () {
  // Take control immediately — don't wait for a page reload.
  self.skipWaiting();
});

self.addEventListener("activate", function (e) {
  e.waitUntil(
    Promise.all([
      clients.claim(),
      // Purge caches from previous SW versions. Only this version's page cache
      // survives.
      caches.keys().then(function (keys) {
        return Promise.all(
          keys
            .filter(function (k) { return k !== PAGE_CACHE; })
            .map(function (k) { return caches.delete(k); })
        );
      })
    ])
  );
});

self.addEventListener("fetch", function (e) {
  // Only navigations are handled. Static assets and API traffic are left
  // alone: the asset URLs are content-hashed and immutable, and every write
  // carries a durable intent in the page's outbox.
  if (e.request.mode !== "navigate") return;
  var url = new URL(e.request.url);
  if (url.origin !== self.location.origin) return;
  e.respondWith(networkFirstPage(e.request));
});

function networkFirstPage(request) {
  return fetch(request)
    .then(function (response) {
      // Cache any successful same-origin HTML navigation so it is available
      // offline. A SW re-fetch of a navigation can report a type other than
      // "basic", so the gate is response.ok rather than the type.
      if (response.ok) {
        var copy = response.clone();
        caches.open(PAGE_CACHE).then(function (cache) {
          cache.put(request.url, copy);
        }).catch(function () { /* best-effort */ });
      }
      return response;
    })
    .catch(function () {
      return caches.match(request.url).then(function (cached) {
        if (cached) return cached;
        return new Response(OFFLINE_PAGE, {
          status: 200,
          headers: { "Content-Type": "text/html; charset=utf-8" }
        });
      });
    });
}
