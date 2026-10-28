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

  function onQueued(evt) {
    var item = evt && evt.detail;
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
  // A queued create from an earlier session has no row here; draw it before the
  // replay lands (the shared outbox started draining on page load), and again
  // whenever a server render replaces the list.
  restorePending();
  var listHost = list();
  if (listHost && window.MutationObserver) {
    new MutationObserver(scheduleRestore).observe(listHost, { childList: true });
  }
})();
