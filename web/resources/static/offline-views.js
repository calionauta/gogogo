// offline-views.js — bounded per-view cache of a SERVER-RENDERED region.
//
// What it is NOT: a local store. Reads still come from the server; the page is
// still rendered from the database, and the cache never answers a request. It
// keeps the LAST HTML of a view the user already saw, so an offline reload or
// an offline view switch shows something true — labelled with WHEN it was true
// — instead of an empty shell.
//
// That distinction is the 2026 line between "offline-first" (a second read
// path, which needs a client database) and "online-first with caching" (one
// read path, a bounded copy of its output). The writes are already durable in
// the outbox, so what was missing offline was the VIEW, not the data.
//
// The three rules that make a fragment cache honest:
//
//   1. BOUNDED — 8 views, 7 days, oldest evicted first. localStorage is ~5 MB
//      and shared with the draft and the outbox journal.
//   2. DATED — every restore is labelled "as of HH:MM". Undated stale content
//      passes for live content; that is the failure that discredits the cache.
//   3. REPLACED, NEVER MERGED — the next server render (re-list or stream)
//      overwrites the restored fragment. The cache never invents a row.
//
// A view is a small flat object that identifies what the server rendered
// (filter/scope/query/date). The caller decides which fields matter.
(function (global) {
  "use strict";

  var PREFIX = "gogogo:view:";
  var MAX_ENTRIES = 8;
  var MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000;
  var warned = false;

  // key is canonical (fields sorted, values escaped) so the same view always
  // maps to the same entry regardless of object construction order.
  function key(name, view) {
    var parts = [];
    for (var k in view) {
      if (!Object.prototype.hasOwnProperty.call(view, k)) continue;
      var v = view[k] == null ? "" : String(view[k]);
      parts.push(encodeURIComponent(k) + "=" + encodeURIComponent(v));
    }
    parts.sort();
    return PREFIX + name + "?" + parts.join("&");
  }

  function entries() {
    var out = [];
    try {
      for (var i = 0; i < global.localStorage.length; i++) {
        var k = global.localStorage.key(i);
        if (!k || k.indexOf(PREFIX) !== 0) continue;
        var at = 0;
        try { at = (JSON.parse(global.localStorage.getItem(k)) || {}).at || 0; }
        catch (_) { at = 0; }
        out.push({ key: k, at: at });
      }
    } catch (_) { /* storage blocked: nothing to prune */ }
    return out;
  }

  // prune keeps the cache bounded. Expired entries go first, then the oldest
  // until MAX_ENTRIES remain — a cache that grows forever is a quota bug that
  // surfaces as a broken draft or queue, which is far worse than a cold view.
  function prune() {
    var all = entries().sort(function (a, b) { return a.at - b.at; });
    var now = Date.now();
    var excess = all.length - MAX_ENTRIES;
    for (var i = 0; i < all.length; i++) {
      if (now - all[i].at > MAX_AGE_MS || i < excess) {
        try { global.localStorage.removeItem(all[i].key); } catch (_) { /* blocked */ }
      }
    }
  }

  function save(name, view, html) {
    if (!html) return false;
    try {
      global.localStorage.setItem(key(name, view), JSON.stringify({ html: html, at: Date.now() }));
    } catch (err) {
      // Best effort by design: losing the cache loses no user data. It is still
      // reported, because the next offline view is empty and nothing else
      // would explain why.
      if (!warned && global.console && console.warn) {
        warned = true;
        console.warn("views: could not cache the rendered view", err);
      }
      return false;
    }
    prune();
    return true;
  }

  function load(name, view) {
    var k = key(name, view);
    try {
      var raw = global.localStorage.getItem(k);
      if (!raw) return null;
      var entry = JSON.parse(raw);
      if (!entry || typeof entry.html !== "string" || !entry.html) return null;
      if (!entry.at || Date.now() - entry.at > MAX_AGE_MS) {
        global.localStorage.removeItem(k);
        return null;
      }
      return entry;
    } catch (_) {
      return null;
    }
  }

  // clear drops every cached view. Sign-out uses it as a second belt to
  // Clear-Site-Data: another account must never be shown this one's rows.
  function clear() {
    var all = entries();
    for (var i = 0; i < all.length; i++) {
      try { global.localStorage.removeItem(all[i].key); } catch (_) { /* blocked */ }
    }
  }

  global.GogogoViews = {
    key: key,
    save: save,
    load: load,
    clear: clear
  };
})(typeof self !== "undefined" ? self : this);
