// offline-outbox.js — a durable PAGE-side queue for independent requests.
//
// WHY THIS EXISTS. The Service Worker owns the todo queue, but it is not always
// there: a browser can block or lose the registration, a private window can
// refuse it, a webview can ship without it. In that state the write fails, the
// feature keeps the text and says "not saved" — true, but avoidable: the page
// can hold the request itself.
//
// WHAT IS REUSABLE (and what is not). This engine is for INDEPENDENT requests:
// each item is retried, dropped or delivered on its own. That fits todo
// creates. It does NOT fit notes or whiteboard: a 409 there rewrites the whole
// QUEUED TAIL against the server's text/shapes (adopt + recompute), which is a
// queue-level decision, not a per-item retry. Those two keep their own queues
// and share only the policy (web/resources/static/offline-policy.js).
//
// Contract:
//   var q = GogogoOutbox.create({ deliver?, onState?, onQueued?, onDropped? })
//   q.enqueue({ url, method, body, headers })   -> Promise
//   q.drain()                                   -> Promise (serialized)
//   q.count()                                   -> Promise<number>
//   q.start()                                   -> wires online/visibility/timer
//
// Every instance publishes the GLOBAL pending count through a
// "gogogo:outbox" window event, which the offline banner reads: with no
// Service Worker, "changes are queued" is only true if this queue holds them.
(function (global) {
  "use strict";

  var DB_NAME = "gogogo-page-outbox";
  var DB_VERSION = 1;
  var STORE = "pending";
  var RETRY_INITIAL_MS = 2000;
  var RETRY_MAX_MS = 30000;

  var totals = {};
  var instances = 0;

  function publish() {
    var sum = 0;
    for (var key in totals) {
      if (Object.prototype.hasOwnProperty.call(totals, key)) sum += totals[key];
    }
    try {
      global.dispatchEvent(new CustomEvent("gogogo:outbox", { detail: { count: sum } }));
    } catch (_) {
      /* no window (tests) */
    }
  }

  function open() {
    return new Promise(function (resolve, reject) {
      var req;
      try {
        req = indexedDB.open(DB_NAME, DB_VERSION);
      } catch (err) {
        reject(err);
        return;
      }
      req.onupgradeneeded = function () {
        var db = req.result;
        if (!db.objectStoreNames.contains(STORE)) {
          db.createObjectStore(STORE, { keyPath: "id", autoIncrement: true });
        }
      };
      req.onsuccess = function () { resolve(req.result); };
      req.onerror = function () { reject(req.error); };
    });
  }

  function withStore(mode, fn) {
    return open().then(function (db) {
      return new Promise(function (resolve, reject) {
        var tx;
        function settle(err, value) {
          db.close();
          if (err) reject(err); else resolve(value);
        }
        try {
          tx = db.transaction(STORE, mode);
          fn(tx.objectStore(STORE));
        } catch (err) {
          settle(err);
          return;
        }
        tx.oncomplete = function () { settle(null); };
        tx.onerror = function () { settle(tx.error); };
        tx.onabort = function () { settle(tx.error || new Error("outbox: transaction aborted")); };
      });
    });
  }

  function add(item) {
    return withStore("readwrite", function (store) { store.add(item); });
  }

  function remove(id) {
    return withStore("readwrite", function (store) { store.delete(id); });
  }

  function all() {
    return new Promise(function (resolve, reject) {
      open().then(function (db) {
        var items = [];
        var tx = db.transaction(STORE, "readonly");
        var cursor = tx.objectStore(STORE).openCursor();
        cursor.onsuccess = function () {
          var c = cursor.result;
          if (c) { items.push(c.value); c.continue(); }
        };
        tx.oncomplete = function () { db.close(); resolve(items); };
        tx.onerror = function () { db.close(); reject(tx.error); };
      }, reject);
    });
  }

  function defaultDeliver(item) {
    var init = { method: item.method || "POST" };
    if (item.headers) init.headers = item.headers;
    if (item.body && init.method !== "GET" && init.method !== "HEAD") init.body = item.body;
    return fetch(item.url, init);
  }

  // attempt classifies ONE delivery with the shared vocabulary. "auth" is kept
  // (the ops are fine; a sign-in fixes it) but stops the pass, like the worker.
  async function attempt(item, deliver) {
    var resp;
    try {
      resp = await deliver(item);
    } catch (_) {
      return "retry";
    }
    if (!resp) return "retry";
    if (resp.ok || resp.status === 404) return "ok";
    var policy = global.GogogoOffline || {};
    if (typeof policy.isAuthStatus === "function" && policy.isAuthStatus(resp.status)) return "auth";
    if (typeof policy.isTransientStatus === "function" && policy.isTransientStatus(resp.status)) return "retry";
    if (resp.status >= 400 && resp.status < 500) return "reject";
    return "retry";
  }

  function create(options) {
    var opts = options || {};
    var deliver = opts.deliver || defaultDeliver;
    var retryDelay = RETRY_INITIAL_MS;
    var draining = null;
    var timer = null;
    var id = ++instances;

    function report(count) {
      totals[id] = count;
      publish();
      if (typeof opts.onState === "function") opts.onState(count);
    }

    function count() {
      return all().then(function (items) {
        report(items.length);
        return items.length;
      }, function () {
        report(0);
        return 0;
      });
    }

    function schedule() {
      if (timer) return;
      timer = setTimeout(function () {
        timer = null;
        drain().then(function (n) {
          if (n > 0) {
            retryDelay = Math.min(retryDelay * 2, RETRY_MAX_MS);
            schedule();
          }
        });
      }, retryDelay);
    }

    function drain() {
      if (draining) return draining;
      draining = (async function () {
        var items = await all();
        var remaining = 0;
        var dropped = 0;
        var delivered = 0;
        for (var i = 0; i < items.length; i++) {
          var item = items[i];
          var outcome = await attempt(item, deliver);
          if (outcome === "ok") {
            try { await remove(item.id); } catch (_) { remaining += items.length - i; break; }
            delivered++;
            continue;
          }
          if (outcome === "reject") {
            try { await remove(item.id); } catch (_) { remaining += items.length - i; break; }
            dropped++;
            continue;
          }
          // retry / auth: everything behind this shares the condition.
          remaining += items.length - i;
          break;
        }
        if (dropped && typeof opts.onDropped === "function") opts.onDropped(dropped);
        await count();
        if (delivered > 0) {
          // The pages re-list on the same event the worker's sync-end uses: a
          // replay that lands while the tab's stream is still connecting would
          // otherwise leave the list without the row it just saved.
          try { global.dispatchEvent(new CustomEvent("gogogo:synced")); } catch (_) { /* no window */ }
        }
        if (remaining === 0) {
          retryDelay = RETRY_INITIAL_MS;
        } else if (navigator.onLine) {
          // A transient failure (5xx, a request that threw) leaves items: retry
          // on a backoff. Without this the queue waited for a visibility event
          // or a reload, which is "never" for a tab the user is watching.
          schedule();
        }
        return remaining;
      })().finally(function () { draining = null; });
      return draining;
    }

    function enqueue(item) {
      var entry = {
        url: item.url,
        method: item.method || "POST",
        headers: item.headers || null,
        body: item.body || "",
        queuedAt: Date.now()
      };
      return add(entry).then(function () {
        if (typeof opts.onQueued === "function") opts.onQueued(entry);
        return count().then(function (n) {
          // Kick a pass when the link is up; offline the online event owns it.
          if (navigator.onLine) drain();
          return n;
        });
      });
    }

    function start() {
      count();
      global.addEventListener("online", function () {
        retryDelay = RETRY_INITIAL_MS;
        drain();
      });
      global.addEventListener("offline", function () { if (timer) { clearTimeout(timer); timer = null; } });
      document.addEventListener("visibilitychange", function () {
        if (document.visibilityState === "visible" && navigator.onLine) drain();
      });
      if (navigator.onLine) drain();
      return api;
    }

    var api = { enqueue: enqueue, drain: drain, count: count, start: start, schedule: schedule };
    return api;
  }

  // The DEFAULT instance: one per page, started as soon as the module loads,
  // so a request queued in an earlier session drains even if the feature that
  // queued it is not the page the user opened. Features enqueue through it and
  // listen for "gogogo:queued", so the optimistic UI is feature-owned and the
  // queue is not.
  var sharedInstance = null;

  function shared() {
    if (sharedInstance) return sharedInstance;
    sharedInstance = create({
      onQueued: function (entry) {
        try {
          global.dispatchEvent(new CustomEvent("gogogo:queued", {
            detail: { url: entry.url, method: entry.method, body: entry.body }
          }));
        } catch (_) {
          /* no window (tests) */
        }
      }
    });
    sharedInstance.start();
    return sharedInstance;
  }

  global.GogogoOutbox = {
    create: create,
    shared: shared,
    enqueue: function (item) { return shared().enqueue(item); },
    list: all,
    pending: function () { return totals; }
  };

  // Start on load, not on first use: a request queued before a reload must
  // drain even if the page the user opened never enqueues anything.
  if (typeof indexedDB !== "undefined") {
    try { shared(); } catch (_) { /* storage blocked: features still work */ }
  }
})(typeof self !== "undefined" ? self : this);
