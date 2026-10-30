// offline-outbox.js — the ONE durable outbox for page mutations.
//
// WHY THIS EXISTS. An offline write is not a failed request to retry: it is a
// user intent that has not been delivered yet. The intent must therefore be
// stored BEFORE the network is attempted, or a tab closed during a hanging
// request loses it — the failure this file exists to prevent.
//
// TWO STORES, ON PURPOSE.
//   - localStorage is SYNCHRONOUS. begin() writes the intent there and returns
//     the idempotency key in the same tick, so the durable copy exists before
//     the request leaves the page. No await, no window.
//   - IndexedDB is the durable, queryable store the drain reads. The write is
//     started immediately and the localStorage mirror is cleared only after it
//     commits. A tab that dies mid-commit keeps the mirror, and recover()
//     merges it back on the next load.
// Both live in the same browser bucket, so an eviction takes them together;
// the mirror is a crash journal, not a second source of truth.
//
// AT-LEAST-ONCE IS THE CONTRACT. A response can be lost after the server
// committed, and the drain will resend the SAME intent. Every request carries
// its key as Idempotency-Key; the server records the key in the same
// transaction as the mutation (op_receipts), so a replay is a no-op. That is
// what makes a read-then-flip toggle safe to retry.
//
// ONE DRAINER PER ORIGIN. Web Locks elects it. Without the lock (an older
// browser) two tabs could replay the same entry at once; the receipt makes
// that harmless, but the lock keeps it rare.
//
// Contract:
//   GogogoOutbox.begin({url, method, body, headers}) -> key   (synchronous)
//   GogogoOutbox.ack(key)        the request succeeded: forget the intent
//   GogogoOutbox.fail(key)       the request did not complete: replay later
//   GogogoOutbox.enqueue({...})  persist an intent with no sender (replay only)
//   GogogoOutbox.drain()         replay pending intents (serialized)
//   GogogoOutbox.start()         wire load/online/visibility/lock events
//   GogogoOutbox.count()         pending + in-flight intents
(function (global) {
  "use strict";

  var DB_NAME = "gogogo-outbox";
  var DB_VERSION = 1;
  var STORE = "ops";
  var MIRROR_KEY = "gogogo-outbox-mirror";
  var OWNER_KEY = "gogogo-outbox-owner";
  var LOCK_NAME = "gogogo-outbox-drain";

  // A queued mutation is unsaved work, not an archive: replaying a week-old
  // intent against a row that changed since is usually wrong. The cap protects
  // browser storage from a client that queues forever.
  var MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000;
  var MAX_ITEMS = 100;

  // INFLIGHT_LEASE_MS bounds "a request is on the wire right now". Past it the
  // tab that sent it is presumed gone (closed, crashed, frozen) and the drain
  // may replay. Without the lease, a live request and the drain would both send
  // the same intent — harmless with the receipt, but wasteful and noisy.
  var INFLIGHT_LEASE_MS = 30 * 1000;

  var RETRY_INITIAL_MS = 2000;
  var RETRY_MAX_MS = 30000;

  var dbPromise = null;
  // inflightMeta remembers what begin() accepted, so fail() can tell the
  // feature what was queued without reading the store back.
  var inflightMeta = {};
  var draining = null;
  var timer = null;
  var retryDelay = RETRY_INITIAL_MS;
  var owner = "";
  // ownerKnown gates the drain. The owner is only trusted after the page
  // confirms the SIGNED-IN account (setOwner). Draining before that could
  // replay the previous user's intents into the next user's session on a
  // shared device — the exact leak the owner stamp exists to prevent.
  var ownerKnown = false;
  var started = false;
  // authPaused stops the retry loop after a 401/403: no amount of replaying
  // signs a user in, and a background loop against an expired session is a
  // poll with extra steps. An EXPLICIT kick (online, visible, page load,
  // sign-in) resumes it; the internal backoff does not.
  var authPaused = false;
  var bus = null;
  try {
    bus = global.BroadcastChannel ? new BroadcastChannel("gogogo-outbox") : null;
  } catch (_) {
    bus = null;
  }

  function uuid() {
    if (global.crypto && global.crypto.randomUUID) return global.crypto.randomUUID();
    return "op-" + Date.now() + "-" + Math.random().toString(36).slice(2, 12);
  }

  // ---- localStorage mirror (synchronous crash journal) --------------------

  function mirrorRead() {
    try {
      var raw = global.localStorage.getItem(MIRROR_KEY);
      return raw ? JSON.parse(raw) : [];
    } catch (_) {
      return []; // private mode / disabled storage: IDB is the only copy
    }
  }

  function mirrorWrite(list) {
    try {
      global.localStorage.setItem(MIRROR_KEY, JSON.stringify(list));
    } catch (_) {
      /* quota or private mode: the IDB write below is still attempted */
    }
  }

  function mirrorAdd(entry) {
    var list = mirrorRead();
    list.push(entry);
    mirrorWrite(list);
  }

  function mirrorRemove(key) {
    var list = mirrorRead();
    var kept = [];
    for (var i = 0; i < list.length; i++) {
      if (list[i].key !== key) kept.push(list[i]);
    }
    mirrorWrite(kept);
  }

  // ---- IndexedDB ---------------------------------------------------------

  function open() {
    if (dbPromise) return dbPromise;
    dbPromise = new Promise(function (resolve, reject) {
      var req;
      try {
        req = global.indexedDB.open(DB_NAME, DB_VERSION);
      } catch (err) {
        reject(err);
        return;
      }
      req.onupgradeneeded = function () {
        var db = req.result;
        if (!db.objectStoreNames.contains(STORE)) {
          var store = db.createObjectStore(STORE, { keyPath: "seq", autoIncrement: true });
          // One row per intent: a mirror merge that races the IDB write
          // cannot create a second copy of the same op.
          store.createIndex("key", "key", { unique: true });
        }
      };
      req.onsuccess = function () { resolve(req.result); };
      req.onerror = function () { reject(req.error); };
    });
    return dbPromise;
  }

  // transact runs one readwrite/readonly unit and settles on COMMIT. The
  // callback receives the store and a `done(value)` setter; it must attach its
  // own request handlers. A generic withStore that assigned request.onsuccess
  // itself silently CLOBBERED a cursor handler — the delete/update never ran,
  // and an intent stayed "inflight" forever with nothing on the wire.
  function transact(mode, fn) {
    return open().then(function (db) {
      return new Promise(function (resolve, reject) {
        var tx;
        try {
          tx = db.transaction(STORE, mode);
        } catch (err) {
          reject(err);
          return;
        }
        var out;
        try {
          fn(tx.objectStore(STORE), function (value) { out = value; });
        } catch (err) {
          reject(err);
          return;
        }
        tx.oncomplete = function () { resolve(out); };
        tx.onerror = function () { reject(tx.error); };
        tx.onabort = function () { reject(tx.error || new Error("outbox: transaction aborted")); };
      });
    });
  }

  function addToIDB(entry) {
    return transact("readwrite", function (store) { store.add(entry); });
  }

  // deleteByKey and updateByKey get and write inside ONE transaction: the
  // value cannot be re-read outside it without a window where a re-inserted
  // row is deleted by a stale key.
  function deleteByKey(key) {
    return transact("readwrite", function (store) {
      var req = store.index("key").get(key);
      req.onsuccess = function () {
        if (req.result) store.delete(req.result.seq);
      };
    });
  }

  function updateByKey(key, patch) {
    return transact("readwrite", function (store) {
      var req = store.index("key").get(key);
      req.onsuccess = function () {
        if (!req.result) return;
        var value = req.result;
        for (var k in patch) {
          if (Object.prototype.hasOwnProperty.call(patch, k)) value[k] = patch[k];
        }
        store.put(value);
      };
    });
  }

  function all() {
    return transact("readonly", function (store, done) {
      var req = store.getAll();
      req.onsuccess = function () { done(req.result); };
    });
  }

  // ---- counts + events ---------------------------------------------------

  // emitLocal updates THIS page's banner. announce also tells the other tabs.
  // Keeping the two apart is not cosmetic: a handler that calls announce() from
  // bus.onmessage re-broadcasts every message it receives, so three tabs ping
  // each other forever. That storm pegged the renderer and hung the notes suite
  // (the outbox runs on every page, not just /todo).
  function emitLocal(detail) {
    try { global.dispatchEvent(new CustomEvent("gogogo:outbox", { detail: detail })); } catch (_) { /* no window */ }
  }

  function announce(count, extra) {
    var detail = { count: count };
    if (extra) {
      for (var k in extra) {
        if (Object.prototype.hasOwnProperty.call(extra, k)) detail[k] = extra[k];
      }
    }
    emitLocal(detail);
    if (bus) {
      try { bus.postMessage(detail); } catch (_) { /* closed */ }
    }
  }

  function count() {
    return all().then(function (items) {
      announce(items.length);
      return items.length;
    }, function () {
      announce(0);
      return 0;
    });
  }

  // ---- write path --------------------------------------------------------

  function normalize(op) {
    return {
      key: (op && op.key) || uuid(),
      url: String((op && op.url) || ""),
      method: String((op && op.method) || "POST").toUpperCase(),
      headers: (op && op.headers) || null,
      body: (op && op.body) || "",
      owner: owner,
      queuedAt: Date.now(),
      state: "inflight",
      inflightAt: Date.now(),
      tries: 0,
      nextAt: 0
    };
  }

  // begin stores the intent and returns its key in the SAME TICK, so the
  // caller can start the request without waiting on storage.
  function begin(op) {
    var entry = normalize(op);
    if (!entry.url) return "";
    inflightMeta[entry.key] = entry;
    mirrorAdd(entry);
    addToIDB(entry).then(function () {
      // The durable copy exists; the journal entry is now redundant.
      mirrorRemove(entry.key);
    }).catch(function () {
      // Storage unavailable (private mode, blocked upgrade). The mirror keeps
      // the intent for the next page load; drain reports 0 meanwhile.
    });
    // NO announcement here. begin() means "the feature's own request is about
    // to go out": the intent is durable, but nothing has failed yet, and
    // announcing count=1 painted the amber "queued" bar for the duration of a
    // perfectly healthy create (the thin bar that flashed on every online add).
    // fail() announces when the request actually did not land; a reload
    // recovers the intent and the drain announces then.
    return entry.key;
  }

  function ack(key) {
    delete inflightMeta[key];
    mirrorRemove(key);
    return deleteByKey(key).then(function () {
      return count();
    }).catch(function () { return 0; });
  }

  // fail marks the intent pending AND tells the feature what was queued, so
  // the optimistic row and the form reset are feature-owned (this file knows
  // nothing about todo markup). The notice is only sent when the update
  // committed: claiming "queued" for a write that never landed is the lie the
  // old Service Worker path was careful to avoid.
  function fail(key) {
    return updateByKey(key, { state: "pending", nextAt: 0 }).then(function () {
      var entry = inflightMeta[key];
      delete inflightMeta[key];
      if (entry) {
        try {
          global.dispatchEvent(new CustomEvent("gogogo:queued", {
            detail: { url: entry.url, method: entry.method, body: entry.body }
          }));
        } catch (_) { /* no window (tests) */ }
      }
      // The request failed for a reason that may not be connectivity (a
      // timeout, a gateway 5xx). Kick a pass now: if it fails again the drain's
      // own backoff takes over.
      if (navigator.onLine) drain();
      return count();
    }).catch(function () { return 0; });
  }

  // enqueue is begin() for a caller with no request to send: the intent is
  // persisted as pending and the drain owns it from the start.
  function enqueue(op) {
    var entry = normalize(op);
    if (!entry.url) return Promise.resolve(0);
    entry.state = "pending";
    mirrorAdd(entry);
    return addToIDB(entry).then(function () {
      mirrorRemove(entry.key);
      try {
        global.dispatchEvent(new CustomEvent("gogogo:queued", {
          detail: { url: entry.url, method: entry.method, body: entry.body }
        }));
      } catch (_) { /* no window (tests) */ }
      return count();
    });
  }

  // ---- owner -------------------------------------------------------------

  function setOwner(next) {
    authPaused = false;
    owner = String(next || "");
    ownerKnown = true;
    try { global.localStorage.setItem(OWNER_KEY, owner); } catch (_) { /* private mode */ }
    // The page has just told us who is signed in; this is the first moment a
    // replay is safe, so it is also what wakes the queue. Deferred by one
    // macrotask so the feature renderers (deferred scripts that run AFTER the
    // Datastar module) can draw their pending rows before the drain starts
    // marking entries inflight.
    setTimeout(function () { drain(true); }, 0);
    return Promise.resolve(0);
  }

  // ---- recover -----------------------------------------------------------

  // recover merges the crash journal into IndexedDB. Idempotent by key: an
  // entry that already committed is dropped from the mirror, not duplicated.
  function recover() {
    var list = mirrorRead();
    if (!list.length) return Promise.resolve();
    var chain = Promise.resolve();
    list.forEach(function (entry) {
      chain = chain.then(function () {
        var copy = entry;
        copy.state = "pending";
        copy.inflightAt = 0;
        return addToIDB(copy).then(function () {
          mirrorRemove(copy.key);
        }, function () {
          // Duplicate key = the IDB write did commit before the tab died.
          mirrorRemove(copy.key);
        });
      });
    });
    return chain.then(count, function () { return 0; });
  }

  // ---- replay ------------------------------------------------------------

  var REPLAY_OK = "ok";
  var REPLAY_RETRY = "retry";
  var REPLAY_AUTH = "auth";
  var REPLAY_REJECT = "reject";

  // ONE definition of "the request did not complete" — the shared policy the
  // notes and whiteboard outboxes also read (offline-policy.js, loaded before
  // this file in Assets). The inline arm covers a page that trimmed the policy
  // script; it must stay identical to that file's function.
  function isTransient(status) {
    var p = global.GogogoOffline;
    if (p && typeof p.isTransientStatus === "function") return p.isTransientStatus(status);
    return status === 408 || status === 429 || status >= 500;
  }

  function replayable(items, now, force) {
    var out = [];
    var expired = 0;
    var foreign = 0;
    for (var i = 0; i < items.length; i++) {
      var item = items[i];
      if (item.queuedAt && now - item.queuedAt > MAX_AGE_MS) { expired++; continue; }
      if (item.owner && owner && item.owner !== owner) { foreign++; continue; }
      if (item.state === "inflight" && item.inflightAt && now - item.inflightAt < INFLIGHT_LEASE_MS) continue;
      // An explicit kick (online, visible, page load, sign-in) means "try now":
      // the backoff exists to stop a background loop, not to ignore a user
      // action or a network change.
      if (!force && item.nextAt && now < item.nextAt) continue;
      out.push(item);
    }
    if (expired) announce(items.length, { dropped: expired, reason: "stale" });
    if (foreign) announce(items.length, { dropped: foreign, reason: "other-account" });
    return out;
  }

  function replayOne(item) {
    var headers = new Headers();
    (item.headers ? Object.keys(item.headers) : []).forEach(function (name) {
      headers.append(name, item.headers[name]);
    });
    // The server stores this key with the mutation, so a replay that arrives
    // after a lost response is answered, not re-applied.
    headers.set("Idempotency-Key", item.key);
    // Ask for the lightweight answer: the tab still learns of the change
    // through the hub record broadcast.
    headers.set("X-Offline-Replay", "1");
    var init = { method: item.method || "POST", headers: headers };
    if (item.body && item.method !== "GET" && item.method !== "HEAD") init.body = item.body;
    return fetch(item.url, init).then(function (resp) {
      if (resp.ok || resp.status === 404) return REPLAY_OK;
      if (resp.status === 401 || resp.status === 403) return REPLAY_AUTH;
      if (isTransient(resp.status)) return REPLAY_RETRY;
      if (resp.status >= 400) return REPLAY_REJECT;
      return REPLAY_RETRY;
    }, function () {
      return REPLAY_RETRY;
    });
  }

  function schedule() {
    if (timer || authPaused) return;
    timer = setTimeout(function () {
      timer = null;
      drain().then(function (remaining) {
        if (remaining > 0) {
          retryDelay = Math.min(retryDelay * 2, RETRY_MAX_MS);
          schedule();
        }
      });
    }, retryDelay);
  }

  function withLock(fn) {
    if (!global.navigator || !navigator.locks) return fn();
    return navigator.locks.request(LOCK_NAME, { ifAvailable: true }, function (lock) {
      if (!lock) return 0; // another tab is draining
      return fn();
    });
  }

  // A pass that finishes inside this window is the live request's OWN intent
  // (the todo the user just added), not a replay worth watching. Announcing
  // "syncing" on entry painted a blue bar for a few hundred milliseconds on
  // every online add — noise that reads as "something went wrong".
  var SYNCING_DELAY_MS = 500;
  var syncingTimer = null;

  function startSyncing(n) {
    if (syncingTimer) return;
    syncingTimer = setTimeout(function () {
      syncingTimer = null;
      announce(n, { replaying: true });
    }, SYNCING_DELAY_MS);
  }

  // Always close the state, even when the pass stopped on a retry: without
  // this the banner kept saying "Syncing changes…" until some unrelated event.
  function stopSyncing(n) {
    if (syncingTimer) { clearTimeout(syncingTimer); syncingTimer = null; }
    announce(n, { replaying: false });
  }

  function drainPass(force) {
    if (!navigator.onLine) return Promise.resolve(0);
    return all().then(function (items) {
      var now = Date.now();
      if (items.length >= MAX_ITEMS) {
        // Drop the OLDEST intent rather than refuse every future write.
        items.sort(function (a, b) { return (a.seq || 0) - (b.seq || 0); });
        var excess = items.length - MAX_ITEMS;
        var chain = Promise.resolve();
        for (var i = 0; i < excess; i++) {
          (function (old) {
            chain = chain.then(function () { return deleteByKey(old.key); });
          })(items[i]);
        }
        return chain.then(function () { return drainPass(force); });
      }
      var queue = replayable(items, now, force);
      if (!queue.length) return count();
      startSyncing(items.length);
      var remaining = 0;
      var chain = Promise.resolve();
      queue.forEach(function (item) {
        chain = chain.then(function () {
          if (remaining > 0) return null; // stop after the first non-ok
          // Mark the intent inflight for the duration of the send: a renderer
          // that restores placeholders must not redraw a row the drain is
          // already delivering (it showed the same todo twice — one pending,
          // one real — while the list patch raced the deletion).
          return updateByKey(item.key, { state: "inflight", inflightAt: Date.now() }).then(function () {
            return replayOne(item);
          }).then(function (outcome) {
            if (outcome === REPLAY_OK) {
              return deleteByKey(item.key);
            }
            if (outcome === REPLAY_REJECT) {
              return deleteByKey(item.key).then(function () {
                announce(items.length, { dropped: 1, reason: "rejected" });
              });
            }
            remaining++;
            if (outcome === REPLAY_AUTH) {
              // The session is gone: keep the intent, STOP the pass and pause
              // the loop, and tell the banner to ask for a sign-in.
              authPaused = true;
              announce(items.length, { auth: true });
              return updateByKey(item.key, { state: "pending" });
            }
            if (outcome === REPLAY_RETRY) {
              return updateByKey(item.key, {
                state: "pending", tries: (item.tries || 0) + 1, nextAt: Date.now() + retryDelay
              });
            }
            return updateByKey(item.key, { state: "pending" });
          });
        });
      });
      return chain.then(function () {
        return count().then(function (n) {
          stopSyncing(n);
          if (remaining === 0) {
            retryDelay = RETRY_INITIAL_MS;
            try { global.dispatchEvent(new CustomEvent("gogogo:synced")); } catch (_) { /* no window */ }
          } else if (navigator.onLine) {
            schedule();
          }
          return n;
        });
      });
    }).catch(function () {
      // The chain can reject before the final then (a storage failure mid-pass).
      // Close the state here too: the delayed "syncing" timer would otherwise
      // fire after the failure and leave the bar up with nobody to lower it.
      // Report the real remaining count, not a made-up zero.
      return count().then(function (n) {
        stopSyncing(n);
        return 0;
      }, function () {
        stopSyncing(0);
        return 0;
      });
    });
  }

  // drain(force) runs one pass. Only an EXPLICIT caller (online, visible,
  // page load, sign-in) passes true: that is what resumes a queue paused by an
  // expired session. The internal backoff never resumes it.
  function drain(force) {
    if (force) authPaused = false;
    if (!ownerKnown) return Promise.resolve(0);
    if (authPaused) return Promise.resolve(0);
    if (draining) return draining;
    draining = withLock(function () { return drainPass(force); }).finally(function () { draining = null; });
    return draining;
  }

  // ---- lifecycle ---------------------------------------------------------

  function start() {
    if (started) return api;
    started = true;
    // Recover the crash journal, but do NOT drain: the owner is unknown until
    // the page calls setOwner (the banner runs on every authed page).
    recover();
    global.addEventListener("online", function () {
      retryDelay = RETRY_INITIAL_MS;
      drain();
    });
    global.addEventListener("offline", function () {
      if (timer) { clearTimeout(timer); timer = null; }
    });
    global.addEventListener("pagehide", function () {
      // Best effort: the journal already holds anything unsent, so this only
      // shortens the wait on the next load.
      if (timer) { clearTimeout(timer); timer = null; }
    });
    document.addEventListener("visibilitychange", function () {
      if (document.visibilityState === "visible" && navigator.onLine) drain();
    });
    if (bus) {
      bus.onmessage = function (e) {
        var m = e.data || {};
        // Local only: re-broadcasting here is the storm emitLocal avoids.
        if (typeof m.count === "number") emitLocal(m);
      };
    }
    return api;
  }

  var api = {
    begin: begin,
    ack: ack,
    fail: fail,
    enqueue: enqueue,
    drain: drain,
    count: count,
    list: all,
    start: start,
    setOwner: setOwner
  };
  global.GogogoOutbox = api;

  // Start on load, not on first use: an intent queued before a reload must
  // drain even if the page the user opened never enqueues anything.
  if (typeof indexedDB !== "undefined") {
    try { start(); } catch (_) { /* storage blocked: features still work */ }
  }
})(typeof self !== "undefined" ? self : this);
