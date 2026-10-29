// todo-offline.js — the todo feature's optimistic state while a write is queued.
//
// The queue reports each pending request (gogogo:queued, detail = {url,
// method, body}). This file owns the TODO DOM and routes, so the queue stays
// feature-agnostic: a second feature adds its own listener instead of teaching
// the transport its markup.
//
// The row itself is a CLONE of the server-rendered <template
// id="todo-pending-row"> (features/todo/components/pending_row.templ), never
// markup built here. The classes, the id scheme, the disabled controls and the
// data-show clauses live in the template package, where
// TestPendingRowPrototypeMatchesTodoItem holds them to TodoItem.
(function () {
  function list() {
    return document.getElementById("todo-list");
  }

  function row(id) {
    return document.getElementById("todo-" + id);
  }

  // prototype is the server-owned shape (features/todo/components/
  // pending_row.templ). The optimistic row is a CLONE, never markup built
  // here: the classes, the id scheme and the four data-show clauses live in
  // the template package, and TestPendingRowPrototypeMatchesTodoItem fails
  // when the prototype and TodoItem drift. A renderer without the prototype
  // draws nothing rather than a row that looks foreign to the list.
  function prototype() {
    var tpl = document.getElementById("todo-pending-row");
    if (!tpl || !tpl.content) return null;
    var first = tpl.content.firstElementChild;
    return first ? first.cloneNode(true) : null;
  }

  function ensureEmptyState() {
    var host = list();
    if (!host || host.querySelector(".todo-item")) return;
    if (document.getElementById("todo-empty-state")) return;
    var empty = document.createElement("div");
    empty.id = "todo-empty-state";
    empty.className = "py-8 text-center text-base-content/40";
    var p = document.createElement("p");
    p.textContent = "No todos here yet.";
    empty.appendChild(p);
    host.appendChild(empty);
  }

  // create draws the placeholder a queued create is holding.
  //
  // The clone keeps every server contract for free: the pending dimming and
  // marker, the disabled controls, the newest-first slot, the shared badge and
  // the visibility guard. Only the values the server cannot know at render
  // time are written here — the row id, its creation date, the title text and
  // whether the badge shows. User-controlled text goes through textContent,
  // so a title can never become markup.
  function create(url, body) {
    var host = list();
    if (!host) return;
    var item = prototype();
    if (!item) return;
    var params = new URLSearchParams(body || "");
    var title = params.get("title") || "";
    // The create form names the visibility field only when it is checked.
    var shared = params.get("visibility") === "public";
    var idem = url.searchParams.get("idem_key") || ("tmp-" + Date.now());
    if (row(idem)) return; // duplicate event (retry): one placeholder only

    var existingEmpty = document.getElementById("todo-empty-state");
    if (existingEmpty) existingEmpty.remove();

    item.id = "todo-" + idem;
    item.dataset.created = new Date().toISOString().slice(0, 10);
    var label = item.querySelector(".todo-title");
    if (label) label.textContent = title;
    var badge = item.querySelector("[data-pending-shared]");
    if (badge && !shared) badge.remove();
    // The list is NEWEST FIRST (ListVisibleTodos: created DESC, id DESC), so
    // the optimistic row belongs at the TOP. Appending it made the placeholder
    // the last row, and the replay's re-render then moved the same todo to
    // first — a visible jump for a row that never changed its meaning.
    host.insertBefore(item, host.firstChild);
    // The row can be hidden by the active filter (completed tab, an active
    // search, scope=shared). Datastar applies data-show on insertion; if it
    // stayed hidden, put the empty state back instead of leaving a blank list.
    if (existingEmpty) {
      setTimeout(function () {
        if (getComputedStyle(item).display === "none") {
          host.insertBefore(existingEmpty, item.nextSibling);
        }
      }, 0);
    }
  }

  function remove(id) {
    var el = row(id);
    if (!el) return;
    el.remove();
    ensureEmptyState();
  }

  // A create queued in an earlier session has no row on the page — it was never
  // on the server. Draw the placeholders the queue is holding so the list says
  // what is actually pending.
  //
  // Re-applied after every list render: the SSE resync replaces #todo-list, so
  // a placeholder drawn once at load is wiped by the first fragment. The queue
  // is the source of truth; the server render is what it is waiting for.
  var restoreScheduled = false;

  // pageStartedAt separates a leftover intent (queued by an earlier session)
  // from one this page owns. It is the whole duplicate rule below.
  var pageStartedAt = Date.now();

  // restorePending draws the intents the queue is holding. An intent from an
  // EARLIER session has no row yet — even while its replay is in flight — so a
  // list patch must bring it back. An intent THIS page created is drawn by
  // onQueued; if the drain is already delivering it, drawing it again would
  // show the same todo twice (the patch that follows is its own row).
  function restorePending() {
    var queue = window.GogogoOutbox;
    if (!queue || typeof queue.list !== "function") return;
    queue.list().then(function (items) {
      for (var i = 0; i < items.length; i++) {
        var item = items[i];
        var fromThisPage = item.queuedAt && item.queuedAt >= pageStartedAt;
        if (fromThisPage && item.state === "inflight") continue;
        var parsed;
        try { parsed = new URL(item.url, location.href); } catch (_) { continue; }
        if (parsed.pathname === "/api/todos" || parsed.pathname === "/api/todos/") {
          create(parsed, item.body);
        }
      }
    }).catch(function () {});
  }

  function scheduleRestore() {
    if (restoreScheduled) return;
    restoreScheduled = true;
    setTimeout(function () {
      restoreScheduled = false;
      restorePending();
    }, 0);
  }

  function toggle(id) {
    var el = row(id);
    if (!el) return;
    var box = el.querySelector("input[type=checkbox]");
    if (box) box.checked = !box.checked;
    var label = el.querySelector(".flex-1");
    if (label) {
      label.classList.toggle("line-through");
      label.classList.toggle("text-base-content/50");
    }
  }

  function clearCompleted() {
    var host = list();
    if (!host) return;
    // Only the completed rows: the button says "clear my completed", and the
    // list can be showing active rows at the same time (a queued clear used to
    // wipe every row until the server re-rendered). A completed server row
    // carries the line-through class on its title.
    var items = host.querySelectorAll(".todo-item");
    for (var i = 0; i < items.length; i++) {
      if (items[i].querySelector(".line-through")) items[i].remove();
    }
    ensureEmptyState();
  }

  // applyIntent folds ONE queued intent into the rendered list: the same
  // function the queued event uses, because that is the only place the shape of
  // an intent is interpreted. A restore of a CACHED view replays the whole
  // queue through it, so a tab switch offline cannot show a row the user has
  // already deleted (or hide one they created).
  function applyIntent(item) {
    if (!item || !item.url) return;
    if ((item.method || "").toUpperCase() !== "POST") return;
    var url;
    try {
      url = new URL(item.url, location.href);
    } catch (_) {
      return;
    }
    var path = url.pathname;
    if (path === "/api/todos" || path === "/api/todos/") {
      create(url, item.body);
      return;
    }
    if (path === "/api/todos/completed/delete" || path === "/api/todos/completed/delete/") {
      clearCompleted();
      return;
    }
    var del = /\/api\/todos\/([^/]+)\/delete\/?$/.exec(path);
    if (del) {
      remove(decodeURIComponent(del[1]));
      return;
    }
    var tog = /\/api\/todos\/([^/]+)\/toggle\/?$/.exec(path);
    if (tog) toggle(decodeURIComponent(tog[1]));
  }

  function onQueued(evt) {
    applyIntent(evt && evt.detail);
  }

  // applyQueued re-applies every stored intent, oldest first, so a restored
  // fragment agrees with the queue that the server has not seen yet.
  function applyQueued() {
    var queue = window.GogogoOutbox;
    if (!queue || typeof queue.list !== "function") return;
    queue.list().then(function (items) {
      for (var i = 0; i < items.length; i++) applyIntent(items[i]);
    }).catch(function () {});
  }

  // A mutation that came back with an ERROR STATUS is not a success: the
  // outbox must keep the intent so the drain can classify it (a 5xx retries,
  // a 4xx is dropped with a notice). Datastar resolves the action for any HTTP
  // answer, so the page reads the status off the error event (argsRaw.status,
  // set by the runtime) and stamps it on the element todoMutateOn started.
  document.addEventListener("datastar-fetch", function (evt) {
    var d = evt.detail || {};
    if (d.type !== "error") return;
    if (!d.el || !d.el.dataset || !d.el.dataset.opKey) return;
    d.el.dataset.opStatus = String((d.argsRaw && d.argsRaw.status) || "0");
  });

  window.addEventListener("gogogo:queued", onQueued);

  // ---- per-view fragment cache -------------------------------------------
  //
  // The list is server-rendered, so offline the only honest thing to show is
  // the LAST render of the view the user is asking for. This captures the
  // region after every online render and restores it (with its age) when the
  // browser is offline. It is not a second read path: the server still owns
  // the query, and the next re-list replaces whatever was restored.
  //
  // The view tuple is the four things the server's query depends on. `mode`
  // (search ranking) is deliberately absent: it is a display variant of the
  // same rows, and giving it its own entry would double the cache for a
  // difference the badge already covers.
  var view = { filter: "all", scope: "all", q: "", after: "" };
  var captureTimer = null;

  function normalize(next) {
    return {
      filter: String((next && next.filter) || "all"),
      scope: String((next && next.scope) || "all"),
      q: String((next && next.q) || ""),
      after: String((next && next.after) || ""),
    };
  }

  // seedView reads the view the page was RENDERED with: data-signals for the
  // server-owned filter/scope, the bound inputs for the client-owned query and
  // date. Reading them once is enough; later changes arrive as signal patches.
  function seedView() {
    var host = document.querySelector("[data-signals]");
    if (host) {
      try {
        var seed = JSON.parse(host.getAttribute("data-signals") || "{}");
        if (typeof seed.filter === "string") view.filter = seed.filter;
        if (typeof seed.scope === "string") view.scope = seed.scope;
        if (typeof seed.createdAfter === "string") view.after = seed.createdAfter;
      } catch (_) { /* keep the defaults */ }
    }
    var box = document.getElementById("todo-search-input");
    if (box && typeof box.value === "string") view.q = box.value;
  }

  document.addEventListener("datastar-signal-patch", function (evt) {
    var d = evt.detail || {};
    var changed = false;
    if (typeof d.filter === "string") { view.filter = d.filter; changed = true; }
    if (typeof d.scope === "string") { view.scope = d.scope; changed = true; }
    if (typeof d.searchQuery === "string") { view.q = d.searchQuery; changed = true; }
    if (typeof d.createdAfter === "string") { view.after = d.createdAfter; changed = true; }
    if (changed) scheduleCapture();
  });

  function capture() {
    // A restored view must never re-date itself: offline, the timestamp in the
    // badge is the truth about the HTML on screen.
    if (!navigator.onLine) return;
    var host = list();
    if (!host || !window.GogogoViews) return;
    // Pending rows are placeholders this browser owns; restorePending rebuilds
    // them from the queue, so caching them would draw every queued row twice.
    var copy = host.cloneNode(true);
    var pending = copy.querySelectorAll(".todo-item[data-pending]");
    for (var i = 0; i < pending.length; i++) pending[i].remove();
    window.GogogoViews.save("todo", view, copy.outerHTML);
  }

  function scheduleCapture() {
    if (!navigator.onLine) return;
    clearTimeout(captureTimer);
    captureTimer = setTimeout(capture, 600);
  }

  function hideNote() {
    var el = document.getElementById("todo-view-cache-note");
    if (el) el.classList.add("hidden");
  }

  function showNote(at) {
    var el = document.getElementById("todo-view-cache-note");
    if (!el) return;
    var d = new Date(at);
    var hh = ("0" + d.getHours()).slice(-2);
    var mm = ("0" + d.getMinutes()).slice(-2);
    el.textContent = "Offline — showing this view as it was at " + hh + ":" + mm + ". It refreshes when you reconnect.";
    el.classList.remove("hidden");
  }

  // restore swaps the region for the cached render of the REQUESTED view and
  // returns whether it did. The caller applies the signals only on true, so a
  // miss can never leave a tab/scope claiming a view the list is not showing.
  function restore(next) {
    if (navigator.onLine) return false;
    var host = list();
    var cache = window.GogogoViews;
    if (!host || !cache) return false;
    // The view model only moves when the restore SUCCEEDS: a miss must not
    // leave the next capture filing this list under a view it does not show.
    var target = normalize(next);
    var entry = cache.load("todo", target);
    if (!entry) return false;
    view = target;
    host.outerHTML = entry.html;
    watch();
    showNote(entry.at);
    // The cached render predates every queued mutation: draw the creates the
    // queue is holding, then fold the whole queue in (toggle/delete/clear), so
    // the view the user sees matches the work the server has not seen.
    restorePending();
    applyQueued();
    return true;
  }

  // watch re-attaches after a restore: outerHTML replaces the node the old
  // observer was watching, so a new region needs its own observer.
  var observer = null;
  function watch() {
    var host = list();
    if (!host || !window.MutationObserver) return;
    if (observer) observer.disconnect();
    observer = new MutationObserver(function () {
      if (navigator.onLine) hideNote();
      scheduleCapture();
      scheduleRestore();
    });
    observer.observe(host, { childList: true });
  }

  // A queued create from an earlier session has no row here; draw it before the
  // replay lands (the shared outbox started draining on page load), and again
  // whenever a server render replaces the list.
  seedView();
  restorePending();
  // A cold offline start shows the last render of THIS view; online this is a
  // no-op, because the server render is newer than any cache entry.
  if (!navigator.onLine) restore(view);
  watch();
  // Seed the cache with the SERVER render of this view: a user who only reads
  // the page (no mutation) must still find it offline. Online only — a restored
  // fragment must not be re-dated as if it came from the server.
  scheduleCapture();
  window.addEventListener("pagehide", function () { if (navigator.onLine) capture(); });
  window.GogogoTodoViews = { restore: restore, capture: capture };
})();
