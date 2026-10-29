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
//   - Every pointermove POSTs ONE fused ephemeral frame to
//     `/api/whiteboard/<doc>/presence` (volatile: never persisted, never
//     merged, never sent to NATS): normalized cursor (x,y 0..1) plus, while
//     the pointer is down, the whole in-progress shape. Peers render the dot
//     and the live corner from the same instant, so they cannot skew apart.
//     Frames carry the sender-captured `cts`; a stale (retried) frame never
//     moves the dot back.
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

  // Seeded from the server-rendered page (data-shapes): the stream is the live
  // channel, but an offline reload never reaches it, and an empty canvas
  // looked like the board had been erased. The stream replaces this list on
  // connect, so a stale seed never outlives a reconnect.
  let shapes = [];
  try {
    const seeded = document.getElementById("canvas-wrap")?.dataset?.shapes;
    if (seeded) shapes = JSON.parse(seeded) || [];
    if (shapes.length) setTimeout(maybeFit, 0);
  } catch (_) {
    shapes = [];
  }
  // Live in-progress shapes from peers, keyed by the peer's display name
  // (one pointer, one draft): {shape, ts}. Ephemeral view state only — the
  // CRDT/`shapes` list stays the single source of truth for committed
  // shapes. See the draft transport in the pointermove section.
  let drafts = {};
  let tool = "rect";
  let color = "#1f2937";
  let drawing = null; // in-progress shape
  let rc = null;

  // ---- viewport (world -> screen) ----
  //
  // Shapes are stored in WORLD coordinates (the canvas CSS px they were drawn
  // at). The view maps the world onto this canvas, so a board drawn on a laptop
  // can be panned/zoomed on a phone instead of showing an empty white area with
  // every shape parked to the right of a 390 px screen.
  //
  // viewTouched means the user moved the view: a later authoritative shapes
  // frame must never snap it back (a peer's join or a reconnect would undo the
  // pan). The palette: wheel zooms, middle-drag pans, two fingers pan/zoom.
  var view = { scale: 1, x: 0, y: 0 };
  var viewTouched = false;
  // Low enough that a very large board still fits on a phone (a 1 % overview
  // is unreadable but navigable, which beats an empty canvas).
  var MIN_SCALE = 0.02;
  var MAX_SCALE = 8;
  var FIT_PAD = 24;

  function toScreen(x, y) {
    return { x: x * view.scale + view.x, y: y * view.scale + view.y };
  }

  function fitCanvas() {
    const r = wrap.getBoundingClientRect();
    const dpr = window.devicePixelRatio || 1;
    canvas.width = Math.max(1, Math.floor(r.width * dpr));
    canvas.height = Math.max(1, Math.floor(r.height * dpr));
    if (window.rough) rc = rough.canvas(canvas);
    render();
    renderCursors();
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
    // Doc guard (defense in depth — the hub already scopes fan-out per
    // doc): a shapes/presence event for another board must never touch
    // this board's view. Without it one board's broadcast overwrote the
    // other's shapes, "erasing" its drawings.
    if (msg.doc && msg.doc !== docID) return;
    if (msg.type === "shapes") {
      shapes = msg.shapes || [];
      maybeFit();
      // The list is authoritative for everything the SERVER has. A queued op is
      // not on the server yet, so folding it back in is what stops a reconnect
      // (or a reload) from erasing a drawing that is still waiting to replay.
      for (var q = 0; q < outbox.length; q++) {
        if (outbox[q] && outbox[q].op) applyLocal(outbox[q].op);
      }
      dropCommittedDrafts();
      render();
    } else if (msg.type === "session") {
      // First frame of the stream: the shared session banner shows when
      // the connection is no longer authed, so a peer's cursor stops
      // silently degrading to a raw client id. Falls through harmlessly
      // when the shared controller is absent.
      if (window.GogogoSession && window.GogogoSession.set) {
        window.GogogoSession.set(!!msg.authed);
      }
    } else if (["cursor", "join", "leave", "count", "snapshot", "draft", "draft-end"].indexOf(msg.type) !== -1) {
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
      // Date the seeded state: offline the canvas shows the shapes the page
      // was RENDERED with (data-shapes) plus whatever is still queued, so the
      // view is not live and must not pass for live.
      var at = "";
      try {
        var seeded = new Date(wrap.dataset.seededAt || "");
        if (!isNaN(seeded.getTime())) {
          at = " · board as of " + ("0" + seeded.getHours()).slice(-2) + ":" + ("0" + seeded.getMinutes()).slice(-2);
        }
      } catch (_) { /* no seed time: the offline copy still says it is buffered */ }
      el.textContent = "offline — drawing is buffered" + at;
    }
  }
  window.addEventListener("online", function () { updateNetStatus(); flushOutbox(); });
  window.addEventListener("offline", updateNetStatus);
  updateNetStatus();

  // ---- offline-first outbox (IndexedDB-persisted) ----
  // Ops drawn while offline are buffered here and replayed on reconnect.
  // The outbox is persisted to IndexedDB so it survives page close/reload.
  // The server merges each op into the shared Loro doc, so replay is
  // convergent and safe regardless of order — but the ORDER of our own ops
  // still matters (a clear after an add must not land first), so the drain is
  // sequential.
  //
  // The store is per-ORIGIN, not per-board. A record therefore carries the
  // board it belongs to: the first version stored bare ops, so two open boards
  // replayed each other's shapes — board B painted A's drawing and then posted
  // it at B. v2 adds `doc`, and the upgrade clears the un-attributable v1 rows
  // (replaying a drawing onto the wrong canvas is worse than a drawing that
  // only survives the tab that made it, which the in-memory queue still does).
  var DB_NAME = "whiteboard-outbox";
  var DB_VERSION = 2;
  var STORE_NAME = "pending_ops";

  function idbOpen() {
    return new Promise(function (resolve, reject) {
      var req = indexedDB.open(DB_NAME, DB_VERSION);
      req.onupgradeneeded = function () {
        var store = req.result.objectStoreNames.contains(STORE_NAME)
          ? req.transaction.objectStore(STORE_NAME)
          : req.result.createObjectStore(STORE_NAME, { keyPath: "id", autoIncrement: true });
        if (req.oldVersion < 2) store.clear();
      };
      req.onsuccess = function () { resolve(req.result); };
      req.onerror = function () { reject(req.error); };
    });
  }

  // idbSaveOp persists one op for THIS board and resolves its row id (null when
  // the write could not be stored). A failure is a dropped change — the op then
  // lives only in memory and dies with the tab — so it is REPORTED, never
  // swallowed: the banner says the browser could not store it.
  function idbSaveOp(op) {
    return idbOpen().then(function (db) {
      return new Promise(function (resolve, reject) {
        var tx = db.transaction(STORE_NAME, "readwrite");
        var req = tx.objectStore(STORE_NAME).add({ doc: docID, op: op });
        tx.oncomplete = function () { db.close(); resolve(req.result); };
        tx.onerror = function () { db.close(); reject(tx.error); };
      });
    }).catch(function (err) {
      offlinePolicy.reportStorageDrop("whiteboard");
      return null;
    });
  }

  function idbLoadAll() {
    return idbOpen().then(function (db) {
      return new Promise(function (resolve, reject) {
        var tx = db.transaction(STORE_NAME, "readonly");
        var req = tx.objectStore(STORE_NAME).getAll();
        req.onsuccess = function () {
          db.close();
          var rows = req.result || [];
          var mine = [];
          for (var i = 0; i < rows.length; i++) {
            // A v1 row has no `doc`: never replayable, never guessed.
            if (rows[i] && rows[i].doc === docID && rows[i].op) {
              mine.push({ id: rows[i].id, op: rows[i].op });
            }
          }
          resolve(mine);
        };
        req.onerror = function () { db.close(); reject(req.error); };
      });
    });
  }

  // idbDelete removes ONE delivered op. The old drain cleared the whole store
  // before sending: a tab that died mid-flight lost the rest of the queue, and
  // a second board's rows were wiped with it. Delete after the answer instead.
  function idbDelete(id) {
    return idbOpen().then(function (db) {
      return new Promise(function (resolve, reject) {
        var tx = db.transaction(STORE_NAME, "readwrite");
        tx.objectStore(STORE_NAME).delete(id);
        tx.oncomplete = function () { db.close(); resolve(); };
        tx.onerror = function () { db.close(); reject(tx.error); };
      });
    });
  }

  // In-memory outbox (entries {id, op}) for fast runtime access; IndexedDB is
  // the durable copy. `saving` serialises writes so a drain that starts right
  // after a drawing still reads that drawing back.
  var outbox = [];
  var saving = Promise.resolve();
  var flushing = false;
  // The retry vocabulary is shared with the worker and the notes client
  // (web/resources/static/offline-policy.js, loaded by the page template
  // before this file). One definition: an answer the worker retries must not
  // be dropped as poison here, and vice versa.
  var offlinePolicy = window.GogogoOffline;
  // retryDelay grows while a replay keeps failing. A dead edge would
  // otherwise be retried every 300ms for as long as the queue is non-empty —
  // a request storm against a gateway that is already struggling.
  var retryDelay = 300;
  var RETRY_MAX_MS = 5000;

  // enqueueOp is the ONLY way an op enters the queue: memory first (so the
  // replay sees it immediately), then the durable row whose id we keep for the
  // post-delivery delete.
  function enqueueOp(op) {
    var entry = { id: null, op: op };
    outbox.push(entry);
    saving = saving.then(function () {
      return idbSaveOp(op).then(function (id) { entry.id = id; });
    });
    return saving;
  }

  // Restore any pending ops from IndexedDB on first load (page reload
  // or cold open). Don't await — let the canvas render first, replay
  // ops asynchronously when they arrive.
  idbLoadAll().then(function (rows) {
    if (rows.length === 0) { maybeFit(); return; }
    outbox = rows;
    // Paint the unsent shapes on the seeded board before the replay: an
    // offline reload must show what the user drew, not just what the server
    // acknowledged.
    for (var i = 0; i < rows.length; i++) applyLocal(rows[i].op);
    maybeFit();
    render();
    flushOutbox();
  }).catch(function (err) {
    // IndexedDB unavailable — the in-memory queue still works for this tab.
    console.warn("whiteboard: pending ops unavailable", err);
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

  // applyLocal folds one queued op into the rendered list. The write protocol
  // has two operations (add = upsert by id, clear = wipe), so this stays a
  // mirror of the server's merge and never invents a third shape of state.
  function applyLocal(op) {
    if (!op || !op.op) return;
    if (op.op === "clear") {
      shapes = [];
      return;
    }
    if (op.op !== "add" || !op.shape || !op.shape.id) return;
    for (var i = 0; i < shapes.length; i++) {
      if (shapes[i].id === op.shape.id) {
        shapes[i] = op.shape;
        return;
      }
    }
    shapes.push(op.shape);
  }

  // flushOutbox drains the queue ONE op at a time, under a per-board lock.
  // Sequential, because the server applies ops in arrival order: `clear` then
  // `add` must not swap places. Locked, because the queue is shared by every
  // tab of the board — the todo outbox has always serialised its drain, and
  // without the same guard two tabs replay the identical rows twice.
  function flushOutbox() {
    if (flushing || !navigator.onLine) return;
    flushing = true;
    saving.then(function () {
      return offlinePolicy.withLock("gogogo:whiteboard:" + docID, function () {
        // Under the lock IndexedDB is authoritative: a peer tab may already
        // have delivered what this tab still remembers. Ops whose durable write
        // failed are still in memory and still go out — losing them twice would
        // be the silent failure this drain exists to avoid.
        return idbLoadAll().then(function (rows) {
          outbox = rows.concat(outbox.filter(function (e) { return e.id == null && e.op; }));
          return deliverFrom(outbox.slice(), 0);
        }, function (err) {
          console.warn("whiteboard: outbox storage unavailable, sending from memory", err);
          outbox = outbox.filter(function (e) { return e && e.op; });
          return deliverFrom(outbox.slice(), 0);
        });
      });
    }).then(function (ran) {
      flushing = false;
      if (ran === "retry" || outbox.length) {
        retryDelay = offlinePolicy.retryDelay(retryDelay, 300, RETRY_MAX_MS);
        setTimeout(flushOutbox, retryDelay);
        return;
      }
      retryDelay = 300;
    }).catch(function (err) {
      flushing = false;
      console.warn("whiteboard: drain failed", err);
      setTimeout(flushOutbox, retryDelay);
    });
  }

  // deliverFrom walks a SNAPSHOT so removing delivered entries cannot shift
  // the index. "retry" stops the pass: the server did not answer, so the rest
  // would fail too, and the order must hold.
  function deliverFrom(queue, i) {
    if (!navigator.onLine || i >= queue.length) return Promise.resolve("done");
    var entry = queue[i];
    if (!entry || !entry.op) return deliverFrom(queue, i + 1);
    return sendOp(entry.op).then(function (outcome) {
      if (outcome === "retry") return "retry";
      var at = outbox.indexOf(entry);
      if (at !== -1) outbox.splice(at, 1);
      var done = entry.id != null
        ? idbDelete(entry.id).catch(function (err) {
            // The shape reached the server but its row remains: the next pass
            // replays it, which the version check resolves as a conflict.
            console.warn("whiteboard: could not delete a delivered op", err);
          })
        : Promise.resolve();
      return done.then(function () { return deliverFrom(queue, i + 1); });
    });
  }

  // sendOp is the single request for one shape op, shared by the live path and
  // the replay. It resolves an OUTCOME, never a boolean:
  //   "ok"    accepted (and our local version adopted)
  //   "drop"  a definitive refusal, or a 409 the caller already resolved
  //   "retry" no answer / 5xx / auth: keep it and stop the pass
  function sendOp(op) {
    return fetch(
      "/api/whiteboard/" + encodeURIComponent(docID) + "/update?clientID=" + encodeURIComponent(clientID),
      { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(op) }
    ).then(function (resp) {
      // A stale replay must NOT be requeued: retrying it would either fail
      // forever or, if it ever won, clobber the peer's newer edit.
      if (handleConflict(resp, op)) return "drop";
      if (resp.ok) {
        adoptVersions(resp);
        return "ok";
      }
      if (offlinePolicy.isTransientStatus(resp.status) || offlinePolicy.isAuthStatus(resp.status)) {
        return "retry";
      }
      // Permanent 4xx (400 bad op, 404 board deleted): replaying cannot fix it,
      // and keeping it would wedge every later op behind a poison entry.
      console.warn("offline: dropping an un-replayable op", resp.status, docID);
      return "drop";
    }).catch(function (e) {
      console.warn("op send failed, kept for replay", e);
      return "retry";
    });
  }

  // adoptVersions learns the revision the server assigned to the shapes we just
  // sent, IN PLACE. The list is only authoritative when it arrives over the SSE
  // stream, which is a single ordered channel: concurrent POST responses are not
  // ordered relative to each other, so replacing the list here would let a slow
  // response for an older op overwrite a newer local shape (draw X, draw Z, X's
  // response lands last -> Z disappears until the next SSE event). The same
  // reason the server uses no-echo.
  //
  // A replayed op needs this as much as a live one: without the version, the
  // next edit of that shape sends baseVersion 0 and is refused as stale on
  // every attempt.
  function adoptVersions(resp) {
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

  // ---- POST helpers ----
  function postOp(op) {
    // While anything is queued, new ops queue too: sending this one now would
    // overtake the queue (a clear could land before the adds it was meant to
    // follow).
    if (!navigator.onLine || outbox.length > 0) {
      enqueueOp(op);
      flushOutbox();
      return;
    }
    sendOp(op).then(function (outcome) {
      if (outcome !== "retry") return;
      enqueueOp(op);
      flushOutbox();
    });
  }

  // POST one fused ephemeral frame. The whiteboard streams over the
  // Cloudflare tunnel, which can occasionally reset the underlying
  // HTTP/3 (QUIC) connection (ERR_QUIC_PROTOCOL_ERROR) — a transient
  // transport blip, not an app error. We retry a couple of times so a
  // single dropped POST does not lose the trailing (final) position.
  // A retry re-sends the ORIGINAL body with its ORIGINAL cts, so on the
  // receiving end it always loses to anything captured later — a late
  // retry can never drag the dot (or the draft) back in time.
  function postFrame(frame) {
    // Offline: skip. The committed op is the durable path and replays from the
    // outbox; a live frame has no value once the connection is gone.
    if (!navigator.onLine) return;
    const url =
      "/api/whiteboard/" +
      encodeURIComponent(docID) +
      "/presence?clientID=" +
      encodeURIComponent(clientID);
    const body = JSON.stringify({
      type: frame.type || (frame.shape ? "draft" : "cursor"),
      doc: docID,
      user: user,
      x: frame.x,
      y: frame.y,
      cts: frame.cts,
      shape: frame.shape,
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
  // Screen point -> WORLD point. Every shape, draft and cursor frame uses the
  // world space, so what a peer draws at (600, 400) means the same place on
  // every screen; only the view differs.
  function localPos(e) {
    const r = canvas.getBoundingClientRect();
    return {
      x: (e.clientX - r.left - view.x) / view.scale,
      y: (e.clientY - r.top - view.y) / view.scale,
    };
  }

  // ---- view controls -----------------------------------------------------
  function zoomAt(clientX, clientY, factor) {
    const r = canvas.getBoundingClientRect();
    const px = clientX - r.left;
    const py = clientY - r.top;
    const scale = Math.min(MAX_SCALE, Math.max(MIN_SCALE, view.scale * factor));
    const k = scale / view.scale;
    view = { scale: scale, x: px - (px - view.x) * k, y: py - (py - view.y) * k };
    viewTouched = true;
    render();
    renderCursors();
  }

  function zoomCenter(factor) {
    const r = canvas.getBoundingClientRect();
    zoomAt(r.left + r.width / 2, r.top + r.height / 2, factor);
  }

  // contentBounds is the world box of what is drawn. Pen strokes only carry
  // their points, so the box has to walk them.
  function contentBounds(list) {
    var minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    function add(x, y) {
      if (x < minX) minX = x;
      if (y < minY) minY = y;
      if (x > maxX) maxX = x;
      if (y > maxY) maxY = y;
    }
    for (var i = 0; i < list.length; i++) {
      var s = list[i];
      if (!s) continue;
      if (s.type === "pen" && s.points && s.points.length >= 2) {
        for (var j = 0; j < s.points.length; j += 2) add(s.points[j], s.points[j + 1]);
      } else {
        add(s.x, s.y);
        add(s.x + s.w, s.y + s.h);
      }
    }
    if (minX === Infinity) return null;
    return { minX: minX, minY: minY, maxX: maxX, maxY: maxY };
  }

  // maybeFit is the answer to "I opened a desktop board on my phone and saw
  // nothing": content wider than the viewport is scaled down once, on the first
  // authoritative list, until the user touches the view. Small boards keep
  // identity, so nothing shifts under a user who was already drawing.
  function maybeFit() {
    if (viewTouched) return;
    var b = contentBounds(shapes);
    if (!b) return;
    var r = wrap.getBoundingClientRect();
    // "Fits" is about the VIEWPORT, not about size: a 200 px drawing parked at
    // x=620 fits in no phone screen. The old check only compared sizes, so the
    // common case (a laptop board opened on a phone) still showed nothing.
    if (b.minX >= 0 && b.minY >= 0 && b.maxX <= r.width && b.maxY <= r.height) return;
    var cw = Math.max(1, b.maxX - b.minX);
    var ch = Math.max(1, b.maxY - b.minY);
    var scale = Math.min((r.width - FIT_PAD * 2) / cw, (r.height - FIT_PAD * 2) / ch, 1);
    scale = Math.max(MIN_SCALE, scale);
    view = { scale: scale, x: FIT_PAD - b.minX * scale, y: FIT_PAD - b.minY * scale };
    viewTouched = true;
    render();
    renderCursors();
  }

  function fitToContent() {
    var b = contentBounds(shapes);
    var r = wrap.getBoundingClientRect();
    viewTouched = false;
    if (!b) {
      view = { scale: 1, x: 0, y: 0 };
    } else {
      var cw = Math.max(1, b.maxX - b.minX);
      var ch = Math.max(1, b.maxY - b.minY);
      var scale = Math.min((r.width - FIT_PAD * 2) / cw, (r.height - FIT_PAD * 2) / ch, MAX_SCALE);
      scale = Math.max(MIN_SCALE, scale);
      view = { scale: scale, x: FIT_PAD - b.minX * scale, y: FIT_PAD - b.minY * scale };
    }
    viewTouched = true;
    render();
    renderCursors();
  }

  // Active pointers on the canvas. ONE pointer draws; TWO navigate (pan/zoom,
  // the touch gesture) and abandon the stroke the first finger started — a
  // pinch is not a shape. Without tracking this a two-finger gesture left
  // `drawing` set and the pen looked dead until a reload.
  const pointers = new Map();
  let pinch = null;
  let panning = null;

  function pinchState() {
    const pts = Array.from(pointers.values());
    const a = pts[0], b = pts[1];
    return {
      dist: Math.hypot(a.x - b.x, a.y - b.y) || 1,
      mx: (a.x + b.x) / 2,
      my: (a.y + b.y) / 2,
      scale: view.scale, x: view.x, y: view.y,
    };
  }

  canvas.addEventListener("pointerdown", function (e) {
    pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
    try { canvas.setPointerCapture(e.pointerId); } catch (_) { /* pointer already gone */ }
    if (pointers.size === 2) {
      if (drawing) { drawing = null; postFrame({ type: "draft-end", cts: Date.now() }); }
      pinch = pinchState();
      panning = null;
      render();
      return;
    }
    if (pointers.size > 2) return;
    if (e.button === 1 || e.altKey) {
      panning = { x: e.clientX, y: e.clientY, vx: view.x, vy: view.y };
      e.preventDefault();
      return;
    }
    // Only the primary button draws. A right-click used to start a shape and
    // then open the context menu on top of it.
    if (e.button !== 0) return;
    const p = localPos(e);
    // Drawing is a claim on the view: a peer adding a shape outside it must not
    // refit the board under the user's hand.
    viewTouched = true;
    if (tool === "pen") {
      drawing = { id: "s-" + Math.random().toString(36).slice(2, 9), type: "pen", points: [p.x, p.y], color: color };
    } else {
      drawing = { id: "s-" + Math.random().toString(36).slice(2, 9), type: tool, x: p.x, y: p.y, w: 0, h: 0, color: color };
    }
  });

  // ONE throttle for the fused frame: ~12Hz with a trailing send (shared
  // /static/throttle.js — same contract as notes carets: delayed, never
  // dropped). Raw pointermove fires ~60/s; every event used to POST.
  // A single rate for cursor and draft (not two rates picked separately)
  // is what keeps the dot and the live corner on the same instant.
  var throttleFn = function (fn) { return fn; };
  if (window.GogogoThrottle && window.GogogoThrottle.throttleTrailing) {
    throttleFn = window.GogogoThrottle.throttleTrailing;
  }
  var throttledFrame = throttleFn(function (frame) { postFrame(frame); }, 80);

  // LIVE DRAWING rides the fused frame above: while the pointer is down the
  // frame ALSO carries the whole in-progress shape (never a delta: a dropped
  // frame is healed by the next one, so the channel needs no sequence numbers
  // or acks). It stays SUPERSEDED, never authoritative: the real `add` op on
  // pointer-up commits the shape, and peers drop the draft the instant the
  // committed shape with the same id arrives (plus a 5 s TTL backstop, and an
  // explicit end for degenerate draws that commit nothing).
  //
  // Payload note: a pen stroke's `points` array grows with the stroke and is
  // re-sent whole each frame. That is the price of loss-tolerance; it is fine
  // at this throttle and shape scale. Delta-encoding would need sequencing.

  canvas.addEventListener("pointermove", function (e) {
    if (pointers.has(e.pointerId)) pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (pinch && pointers.size >= 2) {
      // Keep the world point that was under the initial midpoint under the NEW
      // midpoint, at the new distance ratio: one gesture pans and zooms.
      const now = pinchState();
      const r = canvas.getBoundingClientRect();
      const scale = Math.min(MAX_SCALE, Math.max(MIN_SCALE, pinch.scale * (now.dist / pinch.dist)));
      const wx = (pinch.mx - r.left - pinch.x) / pinch.scale;
      const wy = (pinch.my - r.top - pinch.y) / pinch.scale;
      view = { scale: scale, x: (now.mx - r.left) - wx * scale, y: (now.my - r.top) - wy * scale };
      viewTouched = true;
      render();
      renderCursors();
      return;
    }
    if (panning) {
      view = {
        scale: view.scale,
        x: panning.vx + (e.clientX - panning.x),
        y: panning.vy + (e.clientY - panning.y),
      };
      viewTouched = true;
      render();
      renderCursors();
      return;
    }
    const p = localPos(e);
    // The frame couples cursor and shape from the SAME pointer event: the dot
    // a peer renders IS the tip of the shape version it renders. cts is
    // captured here (not at send time) so a retried frame keeps its original
    // age and loses to anything captured later. Coordinates are WORLD px: the
    // same point on every screen, regardless of each client's pan/zoom.
    const frame = {
      x: Math.round(p.x * 100) / 100,
      y: Math.round(p.y * 100) / 100,
      cts: Date.now(),
    };
    if (drawing) {
      if (tool === "pen") {
        drawing.points.push(p.x, p.y);
      } else {
        drawing.w = p.x - drawing.x;
        drawing.h = p.y - drawing.y;
      }
      frame.shape = drawing;
      render();
    }
    throttledFrame(frame);
  });

  canvas.addEventListener("pointerup", function (e) {
    pointers.delete(e.pointerId);
    if (panning) { panning = null; return; }
    if (pinch) { if (pointers.size < 2) pinch = null; return; }
    if (!drawing) return;
    // ignore degenerate shapes
    const tiny =
      tool === "pen"
        ? drawing.points.length < 4
        : Math.abs(drawing.w) < 4 && Math.abs(drawing.h) < 4;
    const done = drawing;
    drawing = null;
    if (tiny) {
      // Nothing is committed for a degenerate draw, so the draft has nothing
      // to be superseded by — end it explicitly instead of leaving a ghost
      // until the TTL.
      postFrame({ type: "draft-end", cts: Date.now() });
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

  // A cancelled pointer (the OS took the gesture: scroll, call, a system
  // gesture) must drop the stroke. The old code had no handler, so `drawing`
  // survived and the next pointerdown reused... nothing, but the canvas kept
  // painting a ghost until the next event — the "pen stopped working" report.
  canvas.addEventListener("pointercancel", function (e) {
    pointers.delete(e.pointerId);
    panning = null;
    if (pinch && pointers.size < 2) pinch = null;
    if (drawing) {
      drawing = null;
      postFrame({ type: "draft-end", cts: Date.now() });
      render();
    }
  });

  // Wheel zooms around the cursor (plain wheel: the canvas has nothing to
  // scroll, and a page scroll would move the board out from under the pointer).
  canvas.addEventListener("wheel", function (e) {
    e.preventDefault();
    zoomAt(e.clientX, e.clientY, e.deltaY < 0 ? 1.12 : 1 / 1.12);
  }, { passive: false });

  // Toolbar (Datastar sets window signals; mirror here for plain JS)
  const zoomIn = document.getElementById("wb-zoom-in");
  if (zoomIn) zoomIn.addEventListener("click", function () { zoomCenter(1.25); });
  const zoomOut = document.getElementById("wb-zoom-out");
  if (zoomOut) zoomOut.addEventListener("click", function () { zoomCenter(1 / 1.25); });
  const fitBtn = document.getElementById("wb-fit");
  if (fitBtn) fitBtn.addEventListener("click", fitToContent);

  document.querySelectorAll("[data-tool]").forEach(function (btn) {
    btn.addEventListener("click", function () {
      tool = btn.getAttribute("data-tool");
      document.querySelectorAll("[data-tool]").forEach(function (b) {
        b.classList.toggle("btn-primary", b === btn);
      });
    });
  });
  // The swatch is plain <input type="color">, not a Datastar binding: the
  // drawing code (not a signal) owns the value, so it reads the element by the
  // id the toolbar renders. This used to look for `input[data-bind="color"]`,
  // an attribute the template never carries — the picker silently did nothing
  // and every shape kept the default ink whatever the user chose.
  const colorInput = document.getElementById("color-input");
  if (colorInput) {
    color = colorInput.value || color;
    // `input` while the picker moves, `change` when it closes. Some platforms
    // (notably iOS Safari and Firefox's dialog) only guarantee the latter, so
    // wiring one of them left the swatch looking dead on those browsers.
    var applyColor = function () {
      if (colorInput.value) color = colorInput.value;
    };
    colorInput.addEventListener("input", applyColor);
    colorInput.addEventListener("change", applyColor);
  }

  // ---- rendering ----
  function render() {
    if (!ctx) return;
    const dpr = window.devicePixelRatio || 1;
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.setTransform(dpr * view.scale, 0, 0, dpr * view.scale, dpr * view.x, dpr * view.y);
    const all = shapes.slice();
    // Peers' in-progress strokes, then our own — drawn after the committed
    // shapes so a live stroke is never hidden under one.
    Object.keys(drafts).forEach(function (u) { all.push(drafts[u].shape); });
    if (drawing) all.push(drawing);
    for (const s of all) drawShape(s);
  }

  // DRAFT_TTL is the backstop for a draft whose "supersede" signal never
  // arrives (the committing op was buffered offline, or the peer vanished).
  const DRAFT_TTL = 5000;
  // pruneDrafts drops expired drafts and returns how many it removed, so a
  // caller can repaint only when the screen actually changed.
  function pruneDrafts() {
    const now = Date.now();
    let n = 0;
    Object.keys(drafts).forEach(function (u) {
      if (now - drafts[u].ts > DRAFT_TTL) { delete drafts[u]; n++; }
    });
    return n;
  }
  // A draft is superseded the moment its committed shape lands: the same id
  // in the authoritative list means the real shape is now on screen, so the
  // provisional one must go (otherwise it would linger as a duplicate).
  function dropCommittedDrafts() {
    const committed = {};
    for (let i = 0; i < shapes.length; i++) committed[shapes[i].id] = true;
    Object.keys(drafts).forEach(function (u) {
      if (committed[drafts[u].shape.id]) delete drafts[u];
    });
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
  // Roster (who is connected) and cursors (where they last pointed) are TWO
  // different identities on the wire: join/leave/count carry the CONNECTION id
  // (clientID) and are the source of truth for the "X online" count, while
  // cursor events carry the SERVER-AUTHENTICATED display name (email — see
  // whiteboard.handlePresence, which re-stamps them). Keeping both in one map
  // (as this once did) meant every authoritative "count" rebuild discarded the
  // name-keyed cursor entries and re-added clientID-keyed phantoms at the
  // canvas origin — so a real cursor vanished and a ghost appeared the instant
  // anyone joined or left, with nobody moving. Split, each map has one owner.
  let roster = {}; // clientID -> true (connections on this doc)
  let cursors = {}; // tabID -> {x, y, ts, label} (one dot per TAB, not per user)
  const CURSOR_TTL = 8000; // no server-side cursor expiry: prune client-side

  // Identity: cursor/draft frames are keyed by the sender's per-tab
  // clientID (msg.from, stamped by the server), labeled with the display
  // name (msg.user). Keying by the display name merged two tabs of the
  // SAME user under one dot — and one shared cts stream, so two clocks
  // took turns failing each other's staleness guard and the dot jumped.
  // msg.from falls back to msg.user for senders that predate the field.
  function peerID(m) { return (m && (m.from || m.user)) || ""; }
  function peerLabel(m) { return (m && (m.user || m.from)) || ""; }

  // Per-user sender-captured timestamp of the last APPLIED frame. The server
  // re-stamps `ts` on receipt (destroying capture order), so staleness is
  // adjudicated with `cts`, compared only within one sender's frames — where
  // the sender clock is monotonic and cross-machine skew cannot intrude.
  // A retried POST keeps its original cts and always loses to anything
  // captured later. Missing cts (old sender) applies: fail-open, never stuck.
  var lastCTS = {};
  function freshEnough(u, cts) {
    if (typeof cts !== "number") return true;
    if (cts < (lastCTS[u] || 0)) return false;
    lastCTS[u] = cts;
    return true;
  }
  function applyCursor(id, label, x, y) {
    cursors[id] = { x: x, y: y, ts: Date.now(), label: label };
    renderCursors();
  }
  // Applies one draft frame: stores the shape (unless already committed or
  // older-captured than the last applied frame) and moves the sender's dot
  // to the frame's cursor — the live tip coupled at capture.
  function applyDraft(msg) {
    const id = peerID(msg);
    if (!msg.shape || !msg.shape.id) {
      delete drafts[id];
      return;
    }
    // Ignore a draft for a shape we already hold as committed (an
    // out-of-order frame that arrived after the real op).
    for (let i = 0; i < shapes.length; i++) {
      if (shapes[i].id === msg.shape.id) return;
    }
    // One logical stream per sender: cursor and draft share the cts guard,
    // so an older-captured frame moves neither the dot nor the shape — even
    // when two POSTs race each other through the server.
    if (!freshEnough(id, msg.cts)) return;
    drafts[id] = { shape: msg.shape, ts: Date.now() };
    // The frame's cursor IS this shape version's live tip (coupled at
    // capture), so the dot moves with the corner: same instant, zero skew.
    if (typeof msg.x === "number" && typeof msg.y === "number") {
      applyCursor(id, peerLabel(msg), msg.x, msg.y);
    }
  }

  function updatePeerCount() {
    const el = document.getElementById("peer-count");
    if (el) el.textContent = String(Object.keys(roster).length + 1); // +self
  }
  function seedRoster(list) {
    (list || []).forEach(function (p) {
      if (p !== user) roster[p] = true;
    });
  }
  function handlePresence(msg) {
    if (msg.type === "count") {
      // Authoritative full connection set (includes us): rebuild the roster,
      // dropping self. Cursors are deliberately untouched — they live in their
      // own map and expire on their own clock, so a join/leave cannot move or
      // drop a cursor nobody moved.
      roster = {};
      seedRoster(msg.peers);
      updatePeerCount();
      return;
    }
    if (msg.type === "snapshot") {
      // Existing connections for a late arrival (we missed their joins).
      seedRoster(msg.peers);
      updatePeerCount();
      return;
    }
    if (msg.type === "join") {
      if (msg.user !== user) roster[msg.user] = true;
      updatePeerCount();
      return;
    }
    if (msg.type === "leave") {
      delete roster[msg.user];
      updatePeerCount();
      return;
    }
    if (msg.type === "cursor" && typeof msg.x === "number" && typeof msg.y === "number") {
      // Keyed by the sender's per-tab id (labeled with the display name the
      // SERVER stamped, so a client cannot spoof and two demo accounts read
      // apart). A move refreshes the TTL. A frame older than the last
      // applied one (a retried POST landing late) moves nothing —
      // last-write-wins would drag the dot back in time.
      const id = peerID(msg);
      if (!freshEnough(id, msg.cts)) return;
      applyCursor(id, peerLabel(msg), msg.x, msg.y);
      return;
    }
    if (msg.type === "draft") {
      applyDraft(msg);
      render();
      return;
    }
    if (msg.type === "draft-end") {
      // Guarded like any other frame: a stale (retried) end arriving after
      // newer drafts started must not wipe the live shape.
      const id = peerID(msg);
      if (!freshEnough(id, msg.cts)) return;
      delete drafts[id];
      render();
    }
  }
  function renderCursors() {
    cursorsEl.innerHTML = "";
    const now = Date.now();
    Object.keys(cursors).forEach(function (id) {
      const p = cursors[id];
      if (now - p.ts > CURSOR_TTL) { delete cursors[id]; delete lastCTS[id]; return; }
      // One color per user (hashed from the stable display label, so it is
      // deterministic across tabs and reloads with no server state) so two
      // demo accounts read apart at a glance — same rule as notes carets.
      // The dot is per-tab (id); the color is per-user (label).
      const label = p.label || id;
      let h = 0;
      for (let i = 0; i < label.length; i++) h = (h * 31 + label.charCodeAt(i)) >>> 0;
      const color = "hsl(" + (h % 360) + ",70%,45%)";
      const el = document.createElement("div");
      const at = toScreen(p.x, p.y);
      el.style.position = "absolute";
      el.style.left = at.x + "px";
      el.style.top = at.y + "px";
      el.style.transform = "translate(-2px,-2px)";
      el.style.pointerEvents = "none";
      el.innerHTML =
        '<svg width="16" height="16" viewBox="0 0 16 16"><path d="M0 0 L0 12 L4 9 L7 14 L9 13 L6 8 L11 8 Z" fill="' + color + '"/></svg>' +
        '<span class="badge badge-sm ml-1" style="background:' + color + ';color:#fff">' +
        escapeHtml(label) +
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
  setInterval(renderCursors, 4000); // also expires idle cursors (CURSOR_TTL)
  setInterval(function () { if (pruneDrafts() > 0) render(); }, 2000); // draft TTL backstop
})();
