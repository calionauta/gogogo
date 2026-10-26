// Service Worker — offline cache + Background Sync for app CRUD.
//
// This SW intercepts every fetch to /api/* (except SSE streams and
// whiteboard endpoints) and:
//   - GET:  stale-while-revalidate — serve cached, update from network
//   - POST/PUT/PATCH/DELETE: network-first — queue in IndexedDB if offline
//   - On reconnect: Background Sync replays queued mutations in order
//
// Record SSE streams (EventSource) are NOT intercepted — the browser
// manages them natively, so they reconnect automatically when online.
// Whiteboard endpoints are also skipped (they have their own Loro-based
// offline mechanism via IndexedDB outbox in whiteboard.js).
//
// Idempotency: the UI generates a fresh UUID per click and sends it
// as the `idem_key` form field on every create POST. The SW forwards
// the form body verbatim on replay. The server (features/todo/handlers
// mutations) looks up the todo by (owner, idem_key) and returns the
// original instead of creating a duplicate; the (owner, idem_key)
// unique index in internal/dbschema/migrations backs the dedup at the DB
// layer for the race window. Single-instance and multi-instance both
// work because the dedup state lives in the database. See
// docs/decisions.md for the full rationale.

var CACHE_NAME = "flat-api-v1";
var PAGE_CACHE = "gogogo-pages-v1";
var API_PREFIX = "/api/";

// SSE and whiteboard paths that must NOT be intercepted.
var SKIP_PATTERNS = [
  "/api/todos/stream", // todo SSE stream (EventSource)
  "/api/notes/", // notes SSE stream + ops (EventSource + own convergence)
  "/api/whiteboard/" // whiteboard ops + stream (own offline mechanism)
];

// ---- Install & Activate ----
self.addEventListener("install", function (e) {
  // Take control immediately — don't wait for page reload.
  self.skipWaiting();
});

self.addEventListener("activate", function (e) {
  e.waitUntil(
    Promise.all([
      // Claim all clients so the SW controls pages opened before install.
      clients.claim(),
      // Purge old caches from previous SW versions.
      caches.keys().then(function (keys) {
        return Promise.all(
          keys
            .filter(function (k) { return k !== CACHE_NAME && k !== PAGE_CACHE; })
            .map(function (k) { return caches.delete(k); })
        );
      })
    ])
  );
});

// ---- Notify clients of transport state ----
// offline-banner.js (rendered by internal/components/offline_banner.templ)
// listens for these messages: sync-start (a replay pass is running),
// sync-end (queue drained), sync-error (still queued: offline, or a
// transient failure), sync-auth (401: paused until sign-in), queued-item
// (what was just queued, for the feature's optimistic UI), queue-lost (an
// entry will never be saved: foreign account, stale, rejected, queue full)
// and queue-status (the page's count request).
function notifyClients(state, extra) {
  var message = { type: state };
  if (extra) {
    Object.keys(extra).forEach(function (key) { message[key] = extra[key]; });
  }
  return self.clients.matchAll({ includeUncontrolled: true, type: "window" })
    .then(function (clientsList) {
      clientsList.forEach(function (client) {
        client.postMessage(message);
      });
    })
    .catch(function () { /* no clients to notify */ });
}

// ---- Fetch Intercept ----
self.addEventListener("fetch", function (e) {
  var url = new URL(e.request.url);

  if (url.origin !== self.location.origin) return;

  // Navigation requests (HTML page loads): network-first with cache
  // fallback so previously visited pages work offline.
  if (e.request.mode === "navigate") {
    e.respondWith(networkFirstPage(e.request));
    return;
  }

  if (!url.pathname.startsWith(API_PREFIX)) return;

  // Skip SSE/stream/whiteboard endpoints.
  for (var i = 0; i < SKIP_PATTERNS.length; i++) {
    if (url.pathname.indexOf(SKIP_PATTERNS[i]) === 0) {
      return; // let the request pass through unhandled
    }
  }

  // GET → stale-while-revalidate
  if (e.request.method === "GET") {
    e.respondWith(staleWhileRevalidate(e.request));
    return;
  }

  // POST/PUT/PATCH/DELETE → network-first with offline queue
  if (["POST", "PUT", "PATCH", "DELETE"].indexOf(e.request.method) !== -1) {
    e.respondWith(networkFirstWithQueue(e.request));
    return;
  }

  // Other methods (HEAD, OPTIONS, etc.) pass through.
});

// ---- GET: stale-while-revalidate ----
async function staleWhileRevalidate(request) {
  var cached;
  try {
    cached = await caches.match(request);
  } catch (_) {
    // Cache API unavailable
  }

  try {
    var network = await fetch(request);
    // Clone before caching — response body can only be read once.
    var clone = network.clone();
    caches.open(CACHE_NAME).then(function (cache) {
      cache.put(request, clone);
    }).catch(function () { /* best-effort cache update */ });
    return network;
  } catch (_) {
    // Network failed — return cached if available.
    if (cached) return cached;
    // No cache and no network: return a generic offline response.
    return new Response(
      JSON.stringify({ error: "offline", message: "You are offline. Cached data may be stale." }),
      { status: 503, headers: { "Content-Type": "application/json" } }
    );
  }
}

// ---- Navigation (HTML pages): network-first with cache fallback ----
// Navigation requests are NOT intercepted by the API handlers above, so
// when the browser is offline and the user navigates to a new URL they
// get ERR_INTERNET_DISCONNECTED. This handler caches each page on first
// visit and serves the cached copy when offline. Pages never visited
// before show a generic offline message instead of a browser error page.
//
// Cache is NOT shared across users: we only cache basic responses (not
// redirects to /login), and the client clears the cache on logout via
// the "clear-pages" postMessage (see the message handler below).

var OFFLINE_PAGE = '<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Offline</title><style>body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#1d232a;color:#a6adbb;text-align:center;padding:1rem}@media(prefers-color-scheme:light){body{background:#fff;color:#374151}}h1{font-size:1.5rem;margin-bottom:.5rem}p{font-size:.875rem;line-height:1.5;max-width:24rem}</style></head><body><div><h1>You are offline</h1><p>This page has not been visited before, or the cached version expired. Connect to the internet and try again.</p></div></body></html>';

function networkFirstPage(request) {
  return fetch(request)
    .then(function (response) {
      // Cache any successful same-origin HTML navigation. We previously
      // restricted this to response.type === "basic", but a SW re-fetch of a
      // navigation request can report a different type in some setups; the
      // goal is simply offline availability of visited pages.
      // Cache any successful same-origin HTML navigation so it is available
      // offline. (A SW re-fetch of a navigation request may report a type
      // other than "basic", so we gate on response.ok rather than the type.)
      if (response.ok) {
        var copy = response.clone();
        caches.open(PAGE_CACHE).then(function (cache) {
          cache.put(request.url, copy);
        }).catch(function () {});
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

// ---- POST/PUT/PATCH/DELETE: network-first with offline queue ----
//
// Offline-first UX: when the network is down the mutation is queued and the
// FEATURE draws its optimistic state. This worker owns the queue and knows
// nothing about todo markup, routes or ids: it reports the queued request
// (queued-item message) and answers 204 so Datastar's action completes
// without patching anything. The server's realtime broadcast replaces the
// optimistic state later; the todo renderer lives in
// web/resources/static/todo-offline.js.
async function networkFirstWithQueue(request) {
  // Clone before fetch consumes the request body. Cloning in the catch block
  // loses form mutations because a failed fetch can still mark the original
  // body as used, making request.clone() throw before IndexedDB is reached.
  var replayable = request.clone();
  try {
    // Try the real request first.
    return await fetch(request);
  } catch (_) {
    // Network unavailable — fall through to the queue.
  }
  try {
    // Only claim "accepted" when the entry is actually PERSISTENT. A 204
    // after a failed write is a lie: the page resets its optimistic state on
    // a message the worker never sent, so the Add button stayed in its
    // loading state forever and the mutation was silently lost.
    var entry = await withTimeout(queueRequest(replayable), QUEUE_TIMEOUT_MS);
    // Tell the feature what was queued BEFORE the action resolves, so the
    // optimistic row is in the DOM by the time Datastar clears the form.
    await notifyClients("queued-item", {
      item: { url: entry.url, method: entry.method, body: entry.body }
    });
    await notifyClients("sync-error");
    // Register a Background Sync event if supported.
    if (self.registration && self.registration.sync) {
      self.registration.sync.register("gogogo-sync").catch(function () {});
    }
    // 204 = "accepted, nothing to patch": Datastar completes the action,
    // does not retry, and does not replace any DOM node.
    return new Response(null, { status: 204, headers: { "Cache-Control": "no-store" } });
  } catch (_) {
    // Storage unavailable (private mode, disk full, blocked upgrade) or the
    // write did not settle in time. Tell the user the change is lost and
    // answer 503 so the action FAILS instead of looking queued.
    await notifyClients("queue-lost", { count: 1, reason: "storage" });
    return new Response("offline queue unavailable", {
      status: 503,
      headers: {
        "Content-Type": "text/plain; charset=utf-8",
        "Cache-Control": "no-store"
      }
    });
  }
}

// ---- IndexedDB queue ----
var DB_NAME = "gogogo-offline-queue";
var DB_VERSION = 2; // v2 adds the meta store (queue owner)
var STORE_NAME = "pending";
var META_STORE = "meta";
var OWNER_KEY = "owner";
// A queued mutation is unsaved work, not an archive: replaying a week-old
// intent against a row that changed since is usually wrong. The cap protects
// browser storage from a client that queues forever.
var MAX_QUEUE_AGE_MS = 7 * 24 * 60 * 60 * 1000;
var MAX_QUEUE_ITEMS = 100;

// QUEUE_TIMEOUT_MS bounds the IndexedDB hand-off. Every intercepted mutation
// awaits the queue write before answering the page; a write that cannot even
// start (blocked upgrade, storage disabled) must not hold that response — the
// page's action would never settle and its form would stay in the loading
// state forever. Past this budget the worker answers 503 and says the change
// was lost, which is recoverable; a spinner that never moves is not.
var QUEUE_TIMEOUT_MS = 5000;

function withTimeout(promise, ms) {
  return new Promise(function (resolve, reject) {
    var timer = setTimeout(function () {
      reject(new Error("offline queue: timed out"));
    }, ms);
    promise.then(function (value) {
      clearTimeout(timer);
      resolve(value);
    }, function (err) {
      clearTimeout(timer);
      reject(err);
    });
  });
}

function idbOpen() {
  return new Promise(function (resolve, reject) {
    var settled = false;
    var timer = setTimeout(function () {
      finish(reject, new Error("offline queue: open timed out"));
    }, QUEUE_TIMEOUT_MS);
    function finish(fn, value) {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      fn(value);
    }
    var req;
    try {
      req = indexedDB.open(DB_NAME, DB_VERSION);
    } catch (err) {
      finish(reject, err);
      return;
    }
    req.onupgradeneeded = function () {
      var db = req.result;
      if (!db.objectStoreNames.contains(STORE_NAME)) {
        db.createObjectStore(STORE_NAME, { keyPath: "id", autoIncrement: true });
      }
      if (!db.objectStoreNames.contains(META_STORE)) {
        db.createObjectStore(META_STORE, { keyPath: "key" });
      }
    };
    req.onsuccess = function () {
      // A late success after the timeout/blocked path must not leak the
      // connection: an open handle is what blocks the NEXT version upgrade.
      if (settled) {
        req.result.close();
        return;
      }
      finish(resolve, req.result);
    };
    req.onerror = function () { finish(reject, req.error); };
    // onblocked fires when another connection still holds an older version.
    // Without a handler the open request neither succeeds nor errors, so the
    // queued mutation would hang the intercepted request forever.
    req.onblocked = function () {
      finish(reject, new Error("offline queue: open blocked"));
    };
  });
}

async function withStore(store, mode, fn) {
  var db = await idbOpen();
  return new Promise(function (resolve, reject) {
    var tx = db.transaction(store, mode);
    var request = fn(tx.objectStore(store));
    request.onsuccess = function () { db.close(); resolve(request.result); };
    request.onerror = function () { db.close(); reject(request.error); };
  });
}

async function addPending(entry) {
  var db = await idbOpen();
  return new Promise(function (resolve, reject) {
    var tx = db.transaction(STORE_NAME, "readwrite");
    tx.objectStore(STORE_NAME).add(entry);
    tx.oncomplete = function () { db.close(); resolve(); };
    tx.onerror = function () { db.close(); reject(tx.error); };
  });
}

function loadAllPending() {
  return withStore(STORE_NAME, "readonly", function (store) { return store.getAll(); });
}

function deletePending(id) {
  return withStore(STORE_NAME, "readwrite", function (store) { return store.delete(id); });
}

function metaGet(key) {
  return withStore(META_STORE, "readonly", function (store) { return store.get(key); });
}

async function metaSet(key, value) {
  var db = await idbOpen();
  return new Promise(function (resolve, reject) {
    var tx = db.transaction(META_STORE, "readwrite");
    tx.objectStore(META_STORE).put({ key: key, value: value });
    tx.oncomplete = function () { db.close(); resolve(); };
    tx.onerror = function () { db.close(); reject(tx.error); };
  });
}

// queueOwner is the account key the page computed (SHA-256 of the signed-in
// email, see offline-banner.js). Entries are stamped at queue time and a
// replay only touches entries for the CURRENT owner: a mutation queued by one
// person can never be written into another person's account after a
// sign-out / sign-in on a shared device. Entries from before v2 carry no
// owner and are replayed (same browser, pre-migration).
async function queueOwner() {
  try {
    var row = await metaGet(OWNER_KEY);
    return (row && row.value) || "";
  } catch (_) {
    return "";
  }
}

function setQueueOwner(owner) {
  return metaSet(OWNER_KEY, String(owner || ""));
}

async function queueRequest(request) {
  // Read body + headers BEFORE the transaction to avoid an IndexedDB
  // auto-commit race: transactions commit when the synchronous block
  // finishes, so an async body read inside the Promise could fire after
  // the transaction is already closed, silently losing the mutation.
  var body = await request.clone().text().catch(function () { return ""; });
  var headers = Array.from(request.headers.entries());
  var items = await loadAllPending();
  if (items.length >= MAX_QUEUE_ITEMS) {
    // Drop the OLDEST intent (least likely to still be wanted) and say so,
    // instead of failing every future mutation when the queue is full.
    await deletePending(items[0].id);
    await notifyClients("queue-lost", { count: 1, reason: "queue-full" });
  }
  var entry = {
    url: request.url,
    method: request.method,
    headers: headers,
    body: body,
    owner: await queueOwner(),
    queuedAt: Date.now()
  };
  await addPending(entry);
  return entry;
}

// ---- Background Sync ----
// Background Sync is best-effort and is not available in every browser.
// Serialize every trigger so a browser sync event and an explicit page
// reconnect message cannot replay the same queue concurrently.
var replayPromise = null;

function requestReplay() {
  if (!replayPromise) {
    replayPromise = replayQueue().finally(function () {
      replayPromise = null;
    });
  }
  return replayPromise;
}

self.addEventListener("sync", function (e) {
  if (e.tag === "gogogo-sync") {
    e.waitUntil(requestReplay());
  }
});

// The window receives reliable online events; ServiceWorkerGlobalScope does
// not. Pages explicitly request a replay on reconnect, covering browsers and
// headless contexts where Background Sync is absent or delayed.
self.addEventListener("message", function (e) {
  var data = e.data || {};
  if (data.type === "owner") {
    e.waitUntil(setQueueOwner(data.owner));
    return;
  }
  if (data.type === "replay-queue") {
    e.waitUntil((async function () {
      // The page sends the owner with the kick, so a replay can never start
      // against the PREVIOUS account while the owner write is still in
      // flight (messages are independent async tasks).
      if (data.owner) await setQueueOwner(data.owner);
      await requestReplay();
    })());
    return;
  }
  if (data.type === "queue-status") {
    // The PAGE never opens the queue database. It asks the owner of the
    // queue (this worker) for the count, so the schema lives in exactly one
    // place: a second opener without the upgrade handler once created a v1
    // database with no store, after which every queued mutation was lost.
    e.waitUntil(queueCount().then(function (count) {
      if (e.source && e.source.postMessage) {
        e.source.postMessage({ type: "queue-status", id: data.id, count: count });
      }
    }));
    return;
  }
  // Sign-out discards local user data: cached pages (a different user on a
  // shared device must not receive them) AND the queued mutations (an intent
  // queued by the previous account must never replay under the next one).
  // The page asks for the count first and confirms, so this is an explicit
  // user decision, not a silent loss. clear-pages stays as the page-cache-only
  // message for callers that have nothing queued.
  if (data.type === "clear-pages") {
    caches.delete(PAGE_CACHE).catch(function () {});
    return;
  }
  if (data.type === "clear-user-data") {
    e.waitUntil((async function () {
      try {
        var items = await loadAllPending();
        for (var i = 0; i < items.length; i++) {
          await deletePending(items[i].id);
        }
      } catch (_) {
        // IndexedDB unavailable: nothing to clear.
      }
      await caches.delete(PAGE_CACHE).catch(function () {});
    })());
  }
});

// REPLAY_BACKOFF_MS is the transient-failure budget for ONE queued request.
// The `online` event fires when the OS reports a link, not when packets move:
// the first replay fetch can (and on a real machine does) fail with
// "Failed to fetch" a few hundred ms before the network is usable. A
// single-shot loop treated that as "still offline", stopped, and left the
// mutation in IndexedDB with nothing scheduled to retry it — the optimistic
// row stuck forever, other tabs never told. A bounded backoff rides out that
// window; a still-failing queue stays queued and registers Background Sync
// again, never silently dropped.
var REPLAY_BACKOFF_MS = [500, 1000, 2000, 4000];

// One outcome vocabulary for a replayed item, so the caller never has to
// infer policy from a boolean pair:
//   OK       settled — drop it (2xx, or 404 = already gone)
//   RETRY    transient — keep it and stop the pass (network, 5xx, 429)
//   AUTH     session expired — keep it, pause the whole queue, ask to sign in
//   REJECT   permanent 4xx — replaying cannot fix it, drop it with a notice
var REPLAY_OK = "ok";
var REPLAY_RETRY = "retry";
var REPLAY_AUTH = "auth";
var REPLAY_REJECT = "reject";

// MAX_RETRY_AFTER_MS bounds a server-provided Retry-After: a queue that
// waits longer than this is the Background Sync layer's job, not one
// page's pass.
var MAX_RETRY_AFTER_MS = 5000;

function sleep(ms) {
  return new Promise(function (resolve) { setTimeout(resolve, ms); });
}

// retryAfterMs reads the Retry-After header (seconds or HTTP date). Returns
// null when absent or unparseable.
function retryAfterMs(resp) {
  var raw = resp.headers.get("Retry-After");
  if (!raw) return null;
  var secs = Number(raw);
  if (!Number.isNaN(secs)) return Math.max(0, secs * 1000);
  var when = Date.parse(raw);
  if (Number.isNaN(when)) return null;
  return Math.max(0, when - Date.now());
}

// replayOne sends one queued mutation, retrying transient failures with
// backoff, and reports the POLICY rather than a bare pass/fail. A 401 is not
// "retry forever": no amount of retrying signs the user in, so it pauses the
// queue. A 403 is not "retry after signing in" either — the policy refused
// the action — so it is permanent like other 4xx. Retrying either one in the
// background is how a queue hammers a server it can never satisfy.
async function replayOne(item) {
  var headers = new Headers();
  (item.headers || []).forEach(function (pair) {
    headers.append(pair[0], pair[1]);
  });
  // Tag replayed requests so the server can return a lightweight
  // response (no Datastar SSE body) — the tab still learns of the
  // change via the hub record broadcast, which replays never exclude
  // (the queued clientID rides along verbatim, but a background
  // replay is not the originating interaction, so the no-echo rule
  // does not apply to it).
  headers.append("X-Offline-Replay", "1");
  var opts = { method: item.method, headers: headers };
  if (item.body && item.method !== "GET" && item.method !== "HEAD") {
    opts.body = item.body;
  }

  for (var attempt = 0; ; attempt++) {
    try {
      var resp = await fetch(item.url, opts);
      if (resp.ok || resp.status === 404) {
        return REPLAY_OK;
      }
      if (resp.status === 401) {
        return REPLAY_AUTH;
      }
      if (resp.status === 429) {
        var wait = retryAfterMs(resp);
        if (wait !== null && wait <= MAX_RETRY_AFTER_MS) {
          await sleep(wait);
        } else {
          return REPLAY_RETRY;
        }
      } else if (resp.status >= 400 && resp.status < 500) {
        console.warn("sw: dropping un-replayable mutation", resp.status, item.url);
        return REPLAY_REJECT;
      }
      // 5xx: the server is unhealthy; back off within the item budget.
    } catch (_) {
      // Network not usable yet (or still offline) — fall through to backoff.
    }
    if (attempt >= REPLAY_BACKOFF_MS.length) return REPLAY_RETRY;
    await sleep(REPLAY_BACKOFF_MS[attempt]);
  }
}

// usablePending drops entries that must never replay and returns the rest:
// stale intents (older than a week) and another account's mutations. Both
// deletions are reported to the page so the user is not silently ignored.
async function usablePending(items, owner) {
  var usable = [];
  var stale = 0;
  var foreign = 0;
  var now = Date.now();
  for (var i = 0; i < items.length; i++) {
    var item = items[i];
    if (item.queuedAt && now - item.queuedAt > MAX_QUEUE_AGE_MS) {
      await deletePending(item.id);
      stale++;
      continue;
    }
    if (item.owner && owner && item.owner !== owner) {
      await deletePending(item.id);
      foreign++;
      continue;
    }
    usable.push(item);
  }
  if (stale) await notifyClients("queue-lost", { count: stale, reason: "stale" });
  if (foreign) await notifyClients("queue-lost", { count: foreign, reason: "other-account" });
  return usable;
}

async function replayQueue() {
  var items;
  var owner;
  try {
    items = await loadAllPending();
    owner = await queueOwner();
  } catch (_) {
    return; // IndexedDB unavailable — try again later.
  }
  if (items.length === 0) return;

  items = await usablePending(items, owner);
  if (items.length === 0) {
    // Everything was stale or belonged to another account: tell the UI the
    // queue is settled (the lost notices already explained what was dropped).
    await notifyClients("sync-end");
    return;
  }

  // Replay starting — switch the UI to the "syncing" state.
  await notifyClients("sync-start");

  var remaining = 0;
  var needsAuth = false;
  var rejected = 0;
  for (var i = 0; i < items.length; i++) {
    var outcome = await replayOne(items[i]);
    if (outcome === REPLAY_OK) {
      await deletePending(items[i].id);
      continue;
    }
    if (outcome === REPLAY_REJECT) {
      await deletePending(items[i].id);
      rejected++;
      continue;
    }
    if (outcome === REPLAY_AUTH) needsAuth = true;
    // RETRY means the network or the server is not usable right now. Every
    // item behind this one shares that condition, so retrying them here only
    // repeats the same failure: keep them all and end the pass.
    remaining += items.length - i;
    break;
  }
  if (rejected) {
    await notifyClients("queue-lost", { count: rejected, reason: "rejected" });
  }

  // Background Sync is the retry for a transient failure. An auth pause is
  // NOT: it would fire forever against a session that needs a human. A
  // rejected item is already dropped, so nothing is left to schedule.
  if (remaining > 0 && !needsAuth && self.registration && self.registration.sync) {
    self.registration.sync.register("gogogo-sync").catch(function () {});
  }

  await notifyClients(needsAuth ? "sync-auth" : remaining === 0 ? "sync-end" : "sync-error");
}

// queueCount reports how many mutations are waiting. The page asks for this
// instead of opening IndexedDB itself (see the queue-status message).
async function queueCount() {
  try {
    return (await loadAllPending()).length;
  } catch (_) {
    return 0;
  }
}
