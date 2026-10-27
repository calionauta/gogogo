// todo-offline.js — the todo feature's optimistic state while offline.
//
// The Service Worker owns the queue and reports each queued request
// (gogogo:queued, detail = {url, method, body}). This file owns the TODO
// DOM and routes, so sw.js stays feature-agnostic: a second feature adds its
// own listener instead of teaching the worker its markup.
//
// The row is a placeholder: it carries data-pending="true" and disabled
// controls until the replay lands, at which point the record broadcast (or
// the gogogo:synced resync) replaces it with the server row.
(function () {
  var BIN_SVG = '<svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M9 2a1 1 0 00-.894.553L7.382 4H4a1 1 0 000 2v10a2 2 0 002 2h8a2 2 0 002-2V6a1 1 0 100-2h-3.382l-.724-1.447A1 1 0 0011 2H9zM7 8a1 1 0 012 0v6a1 1 0 11-2 0V8zm5-1a1 1 0 00-1 1v6a1 1 0 102 0V8a1 1 0 00-1-1z" clip-rule="evenodd"/></svg>';

  function list() {
    return document.getElementById("todo-list");
  }

  function row(id) {
    return document.getElementById("todo-" + id);
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

  // create mirrors the server row's structure — classes, id scheme, the
  // date attribute AND the visibility contract — so the placeholder looks
  // like a todo, lands in the slot the server will use, and disappears under
  // the same filters the server applies. Anything it does not mirror becomes
  // a row that exists in a view it does not belong to, or moves when the
  // replay re-renders.
  function create(url, body) {
    var host = list();
    if (!host) return;
    var params = new URLSearchParams(body || "");
    var title = params.get("title") || "";
    // The create form names the visibility field only when it is checked.
    var shared = params.get("visibility") === "public";
    var idem = url.searchParams.get("idem_key") || ("tmp-" + Date.now());
    if (row(idem)) return; // duplicate event (retry): one placeholder only

    var existingEmpty = document.getElementById("todo-empty-state");
    if (existingEmpty) existingEmpty.remove();

    var item = document.createElement("div");
    item.id = "todo-" + idem;
    item.className = "todo-item flex items-center gap-3 p-3 border-b border-base-300 hover:bg-base-200 transition-colors opacity-70";
    item.setAttribute("data-pending", "true");
    // Same date contract as TodoItem, and the three client-side filters the
    // SERVER expresses as separate queries: a todo created now is active,
    // owned by me, and is not part of any search result.
    //   - filter=completed  → it is active, so it does not belong;
    //   - scope=shared      → "other people's shared rows" (ListVisibleTodos
    //                         skips owner == me even when public);
    //   - an active search  → the search endpoint cannot know a row that does
    //                         not exist yet.
    // Datastar evaluates data-show on insertion (it observes the document),
    // so this follows the tab/scope/search state live.
    item.dataset.created = new Date().toISOString().slice(0, 10);
    item.setAttribute("data-show", [
      "($createdAfter === '' || el.dataset.created >= $createdAfter)",
      "($filter || 'all') !== 'completed'",
      "!($searchQuery || '')",
      "($scope || 'all') !== 'shared'",
    ].join(" && "));

    var box = document.createElement("input");
    box.type = "checkbox";
    box.className = "checkbox checkbox-primary checkbox-sm";
    box.disabled = true;

    var label = document.createElement("span");
    label.className = "todo-title flex-1";
    label.textContent = title;

    var when = document.createElement("span");
    when.className = "todo-age text-xs text-base-content/40";
    when.textContent = "now";

    var del = document.createElement("button");
    del.type = "button";
    del.className = "btn btn-ghost btn-xs text-error";
    del.disabled = true;
    del.setAttribute("aria-label", "Delete todo");
    // Static icon markup only; the user-controlled title went through
    // textContent above.
    del.innerHTML = BIN_SVG;

    item.appendChild(box);
    item.appendChild(label);
    if (shared) {
      var badge = document.createElement("span");
      badge.className = "badge badge-info badge-xs";
      badge.title = "Shared: every login sees this todo";
      badge.textContent = "Shared";
      item.appendChild(badge);
    }
    item.appendChild(when);
    item.appendChild(del);
    // The list is NEWEST FIRST (ListVisibleTodos: created DESC, id DESC), so
    // the optimistic row belongs at the TOP. Appending it made the placeholder
    // the last row, and the replay's re-render then moved the same todo to
    // first — a visible jump for a row that never changed its meaning.
    host.insertBefore(item, host.firstChild);
  }

  function remove(id) {
    var el = row(id);
    if (!el) return;
    el.remove();
    ensureEmptyState();
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
    var items = host.querySelectorAll(".todo-item");
    for (var i = 0; i < items.length; i++) items[i].remove();
    ensureEmptyState();
  }

  function onQueued(evt) {
    var item = evt && evt.detail;
    if (!item || !item.url) return;
    if ((item.method || "").toUpperCase() !== "POST") return;
    var url;
    try {
      url = new URL(item.url);
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

  window.addEventListener("gogogo:queued", onQueued);
})();
