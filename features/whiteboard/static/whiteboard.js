// Whiteboard client — minimal collaborative canvas.
//
// Transport:
//   - SSE stream (`/api/whiteboard/<doc>/stream`) carries five event kinds:
//       {type:"shapes", shapes:[...]}      -> re-render the whole shape set
//       {type:"count", peers:[...]}         -> authoritative peer set
//       {type:"snapshot", peers:[...]}      -> existing peers for new clients
//       {type:"cursor"|"join"|"leave", user,x,y} -> remote presence
//   - Drawing POSTs a shape op to `/api/whiteboard/<doc>/update`; the
//     server merges it into the Loro CRDT, persists, and broadcasts the
//     resolved shapes back to every OTHER client (exclude-origin).
//   - Mouse moves POST cursor presence to `/api/whiteboard/<doc>/presence`.
//
// No JS CRDT dependency: the server owns the Loro doc and ships plain
// JSON shapes. rough.js (loaded from CDN in the page) gives the
// hand-drawn look.

(function () {
  "use strict";

  const docID = window.WB_DOC_ID;
  if (!docID) {
    console.error("WB_DOC_ID missing");
    return;
  }
  const clientID =
    new URLSearchParams(location.search).get("clientID") ||
    "wb-" + Math.random().toString(36).slice(2, 10);
  // Identity used for presence: the same clientID the server tags join/
  // leave/cursor events with, so we can ignore our own echoes and count
  // peers consistently with every other tab.
  const user = clientID;

  const canvas = document.getElementById("wb-canvas");
  const wrap = document.getElementById("canvas-wrap");
  const ctx = canvas.getContext("2d");
  const cursorsEl = document.getElementById("cursors");

  let shapes = []; // authoritative shape list from server
  let tool = "rect";
  let color = "#1f2937";
  let drawing = null; // in-progress shape
  let rc = null;

  function fitCanvas() {
    const r = wrap.getBoundingClientRect();
    const dpr = window.devicePixelRatio || 1;
    canvas.width = Math.max(1, Math.floor(r.width * dpr));
    canvas.height = Math.max(1, Math.floor(r.height * dpr));
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    if (window.rough) rc = rough.canvas(canvas);
    render();
  }

  // ---- SSE stream ----
  const stream = new EventSource(
    "/api/whiteboard/" + encodeURIComponent(docID) + "/stream?clientID=" + encodeURIComponent(clientID)
  );
  stream.onmessage = function (ev) {
    let msg;
    try {
      msg = JSON.parse(ev.data);
    } catch (e) {
      return;
    }
    if (msg.type === "shapes") {
      shapes = msg.shapes || [];
      render();
    } else if (["cursor", "join", "leave", "count", "snapshot"].indexOf(msg.type) !== -1) {
      handlePresence(msg);
    }
  };
  stream.onerror = function () {
    /* EventSource auto-reconnects; flush any buffered ops when it does. */
    updateNetStatus();
  };
  stream.onopen = function () {
    flushOutbox();
    updateNetStatus();
    // NOTE: presence join/leave is owned by the SERVER (handleStream
    // broadcasts a join to peers on connect and a leave on disconnect,
    // plus a snapshot of existing peers to the newcomer). The client must
    // NOT post its own join here — doing so double-counted peers and made
    // the "X online" number wrong. Cursors are still posted on mousemove.
  };

  // ---- network status indicator ----
  function updateNetStatus() {
    const el = document.getElementById("net-status");
    if (!el) return;
    const online = navigator.onLine;
    if (online) {
      el.classList.add("hidden");
    } else {
      el.classList.remove("hidden");
      el.textContent = "offline — drawing is buffered";
    }
  }
  window.addEventListener("online", function () { updateNetStatus(); flushOutbox(); });
  window.addEventListener("offline", updateNetStatus);
  updateNetStatus();

  // ---- offline-first outbox (IndexedDB-persisted) ----
  // Ops drawn while offline are buffered here and replayed on reconnect.
  // The outbox is persisted to IndexedDB so it survives page close/reload.
  // The server merges each op into the shared Loro doc, so replay is
  // convergent and safe regardless of order.
  //
  // IndexedDB schema: database "whiteboard-outbox", store "pending_ops"
  // with auto-increment id. Each entry is a shape op { op, shape }.
  var DB_NAME = "whiteboard-outbox";
  var DB_VERSION = 1;
  var STORE_NAME = "pending_ops";

  function idbOpen() {
    return new Promise(function (resolve, reject) {
      var req = indexedDB.open(DB_NAME, DB_VERSION);
      req.onupgradeneeded = function () {
        if (!req.result.objectStoreNames.contains(STORE_NAME)) {
          req.result.createObjectStore(STORE_NAME, { keyPath: "id", autoIncrement: true });
        }
      };
      req.onsuccess = function () { resolve(req.result); };
      req.onerror = function () { reject(req.error); };
    });
  }

  function idbSaveOp(op) {
    return idbOpen().then(function (db) {
      return new Promise(function (resolve, reject) {
        var tx = db.transaction(STORE_NAME, "readwrite");
        tx.objectStore(STORE_NAME).add(op);
        tx.oncomplete = function () { db.close(); resolve(); };
        tx.onerror = function () { db.close(); reject(tx.error); };
      });
    });
  }

  function idbLoadAll() {
    return idbOpen().then(function (db) {
      return new Promise(function (resolve, reject) {
        var tx = db.transaction(STORE_NAME, "readonly");
        var req = tx.objectStore(STORE_NAME).getAll();
        req.onsuccess = function () { db.close(); resolve(req.result); };
        req.onerror = function () { db.close(); reject(req.error); };
      });
    });
  }

  function idbClear() {
    return idbOpen().then(function (db) {
      return new Promise(function (resolve, reject) {
        var tx = db.transaction(STORE_NAME, "readwrite");
        tx.objectStore(STORE_NAME).clear();
        tx.oncomplete = function () { db.close(); resolve(); };
        tx.onerror = function () { db.close(); reject(tx.error); };
      });
    });
  }

  // In-memory outbox for fast runtime access; IndexedDB for persistence.
  var outbox = [];
  var flushing = false;

  // Restore any pending ops from IndexedDB on first load (page reload
  // or cold open). Don't await — let the canvas render first, replay
  // ops asynchronously when they arrive.
  idbLoadAll().then(function (ops) {
    if (ops.length > 0) {
      outbox = ops;
      flushOutbox();
    }
  }).catch(function () {
    // IndexedDB unavailable — fall back to in-memory only.
  });

  // ---- conflict handling ----
  // The server version-checks every shape write. A 409 means this op was based
  // on a revision that has since advanced: another writer got there first. The
  // response carries the authoritative shape list, so we adopt it (the same way
  // a reconnect does) and drop the losing op. Without this the edit would either
  // be applied blind — silently overwriting the peer — or retried forever.
  //
  // Dropping is the honest resolution for a canvas: the peer's newer position is
  // kept, and our local optimistic render is corrected on the spot, so the user
  // sees their shape snap to the winning position instead of believing their own
  // move stuck.
  //
  // Only the CONFLICTING shape is corrected, not the whole list. This is not a
  // reconnect (where the client is simply behind and a full replace is right):
  // it happens mid-session, so the client may hold shapes the server has not
  // reflected in this response yet. Concurrent POST responses are not ordered
  // relative to each other, so replacing the list here could drop a shape the
  // user drew after the rejected op. The conflict is about exactly one shape, so
  // only that one is taken from the response.
  function handleConflict(resp, op) {
    if (resp.status !== 409) return false;
    resp.json().then(function (body) {
      if (body && body.shapes && op && op.shape) {
        var id = op.shape.id;
        var winner = null;
        for (var i = 0; i < body.shapes.length; i++) {
          if (body.shapes[i].id === id) { winner = body.shapes[i]; break; }
        }
        var next = [];
        for (var j = 0; j < shapes.length; j++) {
          if (shapes[j].id !== id) {
            next.push(shapes[j]);
          } else if (winner) {
            next.push(winner); // adopt the peer's winning revision
          }
        }
        shapes = next;
        render();
      }
      console.warn("op rejected as stale (shape changed elsewhere); resynced");
    }).catch(function () {});
    return true;
  }

  function flushOutbox() {
    if (flushing || outbox.length === 0) return;
    if (!navigator.onLine) return;
    flushing = true;
    var pending = outbox.splice(0, outbox.length);
    // Clear IndexedDB — pending ops are now in memory; if replay fails
    // they are re-persisted individually below.
    idbClear().catch(function () {});
    Promise.all(pending.map(function (op) {
      return fetch(
        "/api/whiteboard/" + encodeURIComponent(docID) + "/update?clientID=" + encodeURIComponent(clientID),
        { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(op) }
      ).then(function (resp) {
        // A stale replay must NOT be requeued: retrying it would either fail
        // forever or, if it ever won, clobber the peer's newer edit.
        if (handleConflict(resp, op)) return;
        if (!resp.ok) {
          outbox.push(op);
          idbSaveOp(op).catch(function () {});
        }
      }).catch(function (e) {
        outbox.push(op);
        idbSaveOp(op).catch(function () {});
        console.warn("op replay failed, requeued and re-persisted", e);
      });
    })).finally(function () { flushing = false; if (outbox.length) setTimeout(flushOutbox, 300); });
  }
  // ---- POST helpers ----
  function postOp(op) {
    if (!navigator.onLine) {
      outbox.push(op);
      idbSaveOp(op).catch(function () {});
      return;
    }
    fetch(
      "/api/whiteboard/" + encodeURIComponent(docID) + "/update?clientID=" + encodeURIComponent(clientID),
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(op),
      }
    ).then(function (resp) {
      if (handleConflict(resp, op)) return;
      // On success, adopt the VERSION the server assigned to the shape we just
      // sent, in place — do NOT replace the whole list with body.shapes.
      //
      // The list is only authoritative when it arrives over the SSE stream,
      // which is a single ordered channel. Concurrent POST responses are NOT
      // ordered relative to each other, so replacing the list here would let a
      // slow response for an older op overwrite a newer local shape (draw X,
      // draw Z, X's response lands last -> Z disappears locally until the next
      // SSE event). That is the same reason the server uses no-echo: the
      // originator already holds its own optimistic state.
      //
      // All the client needs back is the assigned revision, so a later edit can
      // send it as baseVersion. Without that, an edit built from a shape whose
      // version we never learned would be rejected as stale on every attempt.
      if (resp.ok) {
        resp.json().then(function (body) {
          if (!body || !body.shapes) return;
          var byId = {};
          for (var i = 0; i < body.shapes.length; i++) {
            byId[body.shapes[i].id] = body.shapes[i].version || 0;
          }
          for (var j = 0; j < shapes.length; j++) {
            if (byId[shapes[j].id] !== undefined) shapes[j].version = byId[shapes[j].id];
          }
          render();
        }).catch(function () {});
      }
    }).catch(function (e) {
      outbox.push(op);
      idbSaveOp(op).catch(function () {});
      console.warn("op post failed, buffered and persisted for replay", e);
    });
  }

  // POST a cursor presence event. The whiteboard streams over the
  // Cloudflare tunnel, which can occasionally reset the underlying
  // HTTP/3 (QUIC) connection (ERR_QUIC_PROTOCOL_ERROR) — a transient
  // transport blip, not an app error. We retry a couple of times so a
  // single dropped POST does not permanently kill the remote cursor.
  // fail is intentionally quiet: a missed cursor frame is cosmetic, and
  // the next mouse move re-sends it.
  function postPresence(x, y) {
    const url =
      "/api/whiteboard/" +
      encodeURIComponent(docID) +
      "/presence?clientID=" +
      encodeURIComponent(clientID);
    const body = JSON.stringify({
      type: "cursor",
      doc: docID,
      user: user,
      x: x,
      y: y,
      ts: Date.now(),
    });
    let attempt = 0;
    function send() {
      fetch(url, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: body,
      }).catch(function () {
        if (attempt < 2) {
          attempt++;
          setTimeout(send, 300 * attempt);
        }
      });
    }
    send();
  }

  // ---- drawing ----
  function localPos(e) {
    const r = canvas.getBoundingClientRect();
    return { x: e.clientX - r.left, y: e.clientY - r.top };
  }

  canvas.addEventListener("pointerdown", function (e) {
    const p = localPos(e);
    canvas.setPointerCapture(e.pointerId);
    if (tool === "pen") {
      drawing = { id: "s-" + Math.random().toString(36).slice(2, 9), type: "pen", points: [p.x, p.y], color: color };
    } else {
      drawing = { id: "s-" + Math.random().toString(36).slice(2, 9), type: tool, x: p.x, y: p.y, w: 0, h: 0, color: color };
    }
  });

  canvas.addEventListener("pointermove", function (e) {
    const p = localPos(e);
    const r = canvas.getBoundingClientRect();
    postPresence(parseFloat((p.x / r.width).toFixed(4)), parseFloat((p.y / r.height).toFixed(4)));
    if (!drawing) return;
    if (tool === "pen") {
      drawing.points.push(p.x, p.y);
    } else {
      drawing.w = p.x - drawing.x;
      drawing.h = p.y - drawing.y;
    }
    render();
  });

  canvas.addEventListener("pointerup", function (e) {
    if (!drawing) return;
    // ignore degenerate shapes
    const tiny =
      tool === "pen"
        ? drawing.points.length < 4
        : Math.abs(drawing.w) < 4 && Math.abs(drawing.h) < 4;
    const done = drawing;
    drawing = null;
    if (tiny) {
      render();
      return;
    }
    // Optimistically add the shape to our local list so it stays visible
    // immediately. The server uses the CRDT "no-echo" broadcast pattern
    // (BroadcastExcept), so it does NOT echo our shape back to us — thats
    // the standard for collaborative editors (Yjs, Liveblocks, tldraw).
    // We trust the HTTP 200 response + the fact that on reconnect the
    // initial shapes message carries the authoritative server state.
    shapes = shapes.concat([done]);
    render();
    postOp({ op: "add", shape: done });
  });

  // toolbar (Datastar sets window signals; mirror here for plain JS)
  document.querySelectorAll("[data-tool]").forEach(function (btn) {
    btn.addEventListener("click", function () {
      tool = btn.getAttribute("data-tool");
      document.querySelectorAll("[data-tool]").forEach(function (b) {
        b.classList.toggle("btn-primary", b === btn);
      });
    });
  });
  const colorInput = document.querySelector('input[data-bind="color"]');
  if (colorInput) {
    colorInput.addEventListener("input", function () {
      color = colorInput.value;
    });
  }

  // ---- rendering ----
  function render() {
    if (!ctx) return;
    const r = wrap.getBoundingClientRect();
    ctx.clearRect(0, 0, r.width, r.height);
    const all = drawing ? shapes.concat([drawing]) : shapes;
    for (const s of all) drawShape(s);
  }

  function drawShape(s) {
    if (!rc) {
      // fallback: plain stroke
      ctx.strokeStyle = s.color || "#1f2937";
      ctx.lineWidth = 2;
      ctx.strokeRect(s.x, s.y, s.w, s.h);
      return;
    }
    const opts = { stroke: s.color || "#1f2937", roughness: 1.4, seed: hashSeed(s.id) };
    if (s.type === "rect") {
      rc.rectangle(s.x, s.y, s.w, s.h, opts);
    } else if (s.type === "ellipse") {
      rc.ellipse(s.x + s.w / 2, s.y + s.h / 2, Math.abs(s.w), Math.abs(s.h), opts);
    } else if (s.type === "line") {
      rc.line(s.x, s.y, s.x + s.w, s.y + s.h, opts);
    } else if (s.type === "pen" && s.points && s.points.length >= 4) {
      const pts = [];
      for (let i = 0; i < s.points.length; i += 2) pts.push({ x: s.points[i], y: s.points[i + 1] });
      rc.curve(pts, opts);
    }
  }

  function hashSeed(id) {
    let h = 0;
    for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) | 0;
    return Math.abs(h) % 100000;
  }

  // ---- presence ----
  let peers = {}; // user -> {x,y,ts}
  function updatePeerCount() {
    const el = document.getElementById("peer-count");
    if (el) el.textContent = String(Object.keys(peers).length + 1); // +self
  }
  function handlePresence(msg) {
    if (msg.user === user) return; // ignore our own echoes
    if (msg.type === "count") {
      // Authoritative peer set from the server: seed the peer map (minus
      // self) so the "X online" count and remote cursors stay consistent
      // across every tab — even after reconnects or a missed leave. The
      // server broadcasts the full set (including us); we drop self.
      peers = {};
      (msg.peers || []).forEach(function (p) {
        if (p !== user) peers[p] = peers[p] || { x: 0.5, y: 0.5, ts: Date.now() };
      });
      updatePeerCount();
      renderCursors();
      return;
    }
    if (msg.type === "snapshot") {
      // Seed the peer set from the server's list of already-connected
      // clients (we were the last to arrive, so we missed their joins).
      (msg.peers || []).forEach(function (p) {
        if (p !== user) peers[p] = peers[p] || { x: 0.5, y: 0.5, ts: Date.now() };
      });
      updatePeerCount();
      renderCursors();
      return;
    }
    // A leave from a peer we never tracked is harmless.
    if (msg.type === "join") {
      peers[msg.user] = msg;
    } else if (msg.type === "leave") {
      delete peers[msg.user];
    } else {
      peers[msg.user] = msg;
    }
    updatePeerCount();
    renderCursors();
  }
  function renderCursors() {
    const r = wrap.getBoundingClientRect();
    cursorsEl.innerHTML = "";
    Object.keys(peers).forEach(function (u) {
      const p = peers[u];
      const el = document.createElement("div");
      el.style.position = "absolute";
      el.style.left = (p.x * r.width) + "px";
      el.style.top = (p.y * r.height) + "px";
      el.style.transform = "translate(-2px,-2px)";
      el.style.pointerEvents = "none";
      el.innerHTML =
        '<svg width="16" height="16" viewBox="0 0 16 16"><path d="M0 0 L0 12 L4 9 L7 14 L9 13 L6 8 L11 8 Z" fill="#ef4444"/></svg>' +
        '<span class="badge badge-sm ml-1" style="background:#ef4444;color:#fff">' +
        escapeHtml(u) +
        "</span>";
      cursorsEl.appendChild(el);
    });
  }
  function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }

  window.addEventListener("resize", fitCanvas);
  fitCanvas();
  setInterval(renderCursors, 4000); // prune stale handled server-side too
})();
