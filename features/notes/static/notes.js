// Notes client — minimal collaborative textarea.
//
// Transport:
//   - SSE stream (`/api/notes/<doc>/stream`) carries resolved-text events:
//       {type:"note-text", doc, from, text, rev} -> advance the base
//       always; render when backgrounded or idle (caret saved/clamped).
//   - Typing POSTs prefix/suffix-diffed char ops with the base revision;
//     stale bases get 409 + authoritative state and recompute (bounded
//     auto-retry), so positions are never evaluated on old text.
//   - Typing POSTs prefix/suffix-diffed char ops to
//     `/api/notes/<doc>/op`; the server merges them into the Loro Text,
//     persists, and broadcasts the resolved text to every OTHER client
//     (exclude-origin).
//
// No editor library, no JS CRDT: the server owns the Loro doc and ships
// plain text. Positions are UTF-16 code units (JS strings); the server
// counts Unicode scalars, so non-BMP characters may drift by one position
// under concurrency. ASCII and BMP text are exact.

(function () {
  "use strict";

  const main = document.querySelector("[data-doc-id]");
  const DOC = main ? main.dataset.docId : "";
  if (!DOC) {
    console.error("notes: missing data-doc-id");
    return;
  }
  const CID = crypto.randomUUID ? crypto.randomUUID() : String(Date.now());
  const ta = document.getElementById("note-text");
  const net = document.getElementById("net-status");
  const peers = document.getElementById("peer-pill");
  const rtt = document.getElementById("rtt-pill");
  const typing = document.getElementById("typing-pill");
  if (!ta) return;

  // The retry vocabulary is shared with the todo outbox and the whiteboard
  // (web/resources/static/offline-policy.js, loaded before this file). It is
  // bound HERE because the initial drain (`if (outbox.length) flush()`) runs
  // during this script's own evaluation — a const declared further down would
  // still be in its temporal dead zone at that point.
  const offlinePolicy = window.GogogoOffline;

  // Pending state: a textarea cannot style ranges, so per-character
  // grey ("only unsynced letters dimmed") is impossible without
  // contenteditable — and swapping the editor for a status light would
  // trade a simple honest control for a rich-text project. The truthful
  // signal here is the save pill (saving… → last round-trip), plus
  // aria-busy for assistive tech. Whole-text dimming was tried and
  // removed: it punished the typist for the transport's latency.
  let lastRTT = "";
  function setPending(on) {
    ta.setAttribute("aria-busy", on ? "true" : "false");
    if (rtt) rtt.textContent = on ? "saving…" : lastRTT;
  }

  // Convergence model: serverText/REV track the last AUTHORITATIVE state
  // (page render, then every 200/409/stream event). Diffs always compute
  // against it, so positions are never evaluated on a stale base — a batch
  // computed across a race is refused with 409 and recomputed instead of
  // misapplied (which is how characters used to go missing).
  let serverText = ta.value;
  let REV = parseInt((main.dataset.rev || "0"), 10) || 0;
  let clean = true; // no unconfirmed local edits (blur may adopt server text)
  let lastInputAt = 0; // last keystroke: adopt-render pauses while fresh
  let timer = null;
  let flushing = false;
  let attempts = 0; // bounded 409 auto-retries per flush chain
  const draftKey = "notes-draft:" + DOC;
  const outKey = "notes-outbox:" + DOC;
  // Demo-grade offline cushion: if the server rendered empty but this
  // browser typed before (reload during an outage), restore the draft.
  // Server text always wins when non-empty — last-writer-per-browser only.
  try {
    if (!ta.value && localStorage.getItem(draftKey)) ta.value = localStorage.getItem(draftKey);
  } catch (err) { /* private mode: no draft */ }
  // Op outbox (whiteboard uses IndexedDB; localStorage is enough for text
  // ops): batches typed while offline replay on reconnect AND on reload.
  // Entries are {ops, base} pairs. At-least-once: a lost response replays
  // an applied batch, duplicating the insert — text inserts are not
  // idempotent. Demo-grade, documented.
  let outbox = [];
  // loadOutbox decodes the persisted queue. An entry written before the batch
  // shape existed is a bare op (`{t:"ins",...}`) and is wrapped.
  function loadOutbox() {
    const list = [];
    try {
      const raw = JSON.parse(localStorage.getItem(outKey) || "[]");
      for (const e of raw) {
        if (e && Array.isArray(e.ops)) list.push(e);
        else if (e && typeof e.t === "string") list.push({ ops: [e], base: 0 });
      }
    } catch (err) { /* unreadable queue: start empty, the draft still holds the text */ }
    return list;
  }
  // saveOutbox is FAIL-FAST: a queue that cannot be written is only in memory,
  // and this tab is now the only copy. Reporting it is the difference between
  // "your text is queued" and "your text is gone when you close this tab".
  function saveOutbox() {
    try { localStorage.setItem(outKey, JSON.stringify(outbox)); }
    catch (err) { offlinePolicy.reportStorageDrop("notes"); }
  }
  outbox = loadOutbox();
  ta.addEventListener("input", function () {
    clearTimeout(timer);
    lastInputAt = Date.now();
    timer = setTimeout(sendDiff, 100);
    reportTyping();
    // No maybeCaret() here on purpose: every text change ships its caret
    // WITH the op batch 100ms later (atomic with its text). A standalone
    // report from this handler is anchored to the stale serverText while
    // our own keystroke is still unconfirmed, so it overwrote the exact
    // dot with an approximate one — the per-letter line jump peers saw.
    // Selectionchange kept doing exactly that a beat later, outside this
    // handler, so maybeCaret itself now refuses to report while
    // `ta.value !== serverText` — that guard is the real one.
    // Pure cursor moves still report via selectionchange, heartbeats via
    // the 5s interval (both guarded while ops are unconfirmed).
    // Deliberately vanilla (not data-on:input__debounce): the timer,
    // lastInputAt (idle adopt), typing and caret reports share one
    // handler by design — splitting the timer into markup would scatter
    // coupled state across two owners for zero protocol gain.
  });
  ta.addEventListener("scroll", function () { renderCarets(); }, { passive: true });
  window.addEventListener("resize", function () { renderCarets(); });
  document.addEventListener("selectionchange", function () { maybeCaret(); });

  // Heartbeat re-announce (OT-demo lesson): an idle peer's dot must not
  // age out while they're still here — force a resend every 5s while the
  // page is visible.
  //
  // "Present" and "here is my caret" are TWO different claims, and this
  // heartbeat may make only the first. It used to force the dot too, reading
  // ta.selectionStart unconditionally — but a tab that never touched the
  // textarea owns no caret, only the one the browser parks at the END of the
  // value it last adopted from the other editor. So every 5s of the other
  // user's typing republished that idle tab as a cursor gliding along the end
  // of the text (the reported "demo2's cursor keeps moving inside my demo1 box
  // while I type"). heartbeatCaret resends only a caret this tab placed
  // itself; a tab that never typed sends none, which is the truth.
  setInterval(function () {
    if (!document.hidden) heartbeatCaret();
  }, 5000);
  // pagehide is the honest leave signal: an EventSource reconnect survives a
  // reload, so the tab's dot must be dropped here instead of waiting out the
  // 8s expiry in every other browser.
  window.addEventListener("pagehide", function () {
    fetch("/api/notes/" + encodeURIComponent(DOC) + "/leave?clientID=" + encodeURIComponent(CID), {
      method: "POST",
      keepalive: true,
    }).catch(function () { /* ephemeral */ });
  });

  // Remote carets ("who is where"): peers report {line, pos}; each browser
  // maps the offset to ITS OWN pixels via mirror-div (computed font metrics
  // copied from the live textarea, so zoom, fonts and wrapping can never
  // desync the math — static CSS guessing is what breaks across viewports).
  //
  // A caret is stored as {label, line, pos, text, seen}: `pos` is an offset in
  // `text`, the exact document snapshot the reporter measured against (the
  // `note-text` event ships its text, so the two travel together). Measuring
  // the mirror against that snapshot — NOT against our own ta.value — is
  // what keeps a peer's dot on the right LINE while the two editors are
  // briefly diverged. Against ta.value the same offset lands on whatever
  // character our stale copy holds at that index: a peer whose caret sits at
  // the END of line 2 (offset N) showed up at the START of line 3 in a copy
  // that was one character behind — the reported "blinks at the start of the
  // line below, then snaps back". The snapshot makes the dot's line/column
  // exactly the reporter's, independent of our local render lag.
  //
  // Identity: keyed by the sender's per-tab clientID (msg.from, stamped by
  // the server), labeled with the display name (msg.user). Keying by the
  // display name merged two tabs of the SAME user under one key: each tab's
  // heartbeat overwrote the other's dot, so the peer's caret jumped lines
  // while they typed — even with the other user idle. msg.from falls back
  // to msg.user for senders that predate the field.
  const carets = new Map(); // tabID -> {label, line, pos, text, seen}
  function peerID(m) { return (m && (m.from || m.user)) || ""; }
  function peerLabel(m) { return (m && (m.user || m.from)) || ""; }
  // shiftPeerOffsets transforms peer offsets through OUR unconfirmed local
  // ops (one-direction OT-lite): we know exactly what changed under their
  // reported positions, so their carets track our typing instead of
  // freezing on stale absolute offsets — the reported "it stops in the
  // wrong place while I keep typing" bug. Runs inside sendDiff, before the
  // batch posts. `ops` are relative to serverText, which is also the
  // snapshot a fresh caret was reported against, so the same splice keeps
  // `pos` and `text` in lockstep (a delete overlapping a caret clamps both
  // to the cut point — documented approximation: ownership of the deleted
  // range is gone, the nearest surviving position is the honest answer).
  function shiftPeerOffsets(ops) {
    if (!carets.size) return;
    carets.forEach(function (c) {
      // A caret that arrived WITH its sender's own text (op-attached) is
      // already expressed in our coordinates: the batch that moved it is
      // ours, and the peer's snapshot includes it. Shifting those again
      // dragged their dot along with every keystroke I typed — the other
      // half of the reported "their cursor slides while I type".
      if (c.own) return;
      let pos = c.pos;
      let text = typeof c.text === "string" ? c.text : null;
      for (const op of ops) {
        if (op.t === "ins") {
          if (pos >= op.i) pos += op.s.length;
          if (text !== null && op.i <= text.length) {
            text = text.slice(0, op.i) + op.s + text.slice(op.i);
          }
        } else if (op.t === "del") {
          if (pos > op.i + op.n) pos -= op.n;
          else if (pos > op.i) pos = op.i;
          if (text !== null && op.i < text.length) {
            text = text.slice(0, op.i) + text.slice(op.i + op.n);
          }
        }
      }
      c.pos = pos;
      if (text !== null) c.text = text;
    });
  }
  let caretSent = -1;
  // lastOwnCaret is the last position THIS tab reported as its own (set on
  // a focused report, cleared the moment the textarea loses focus). The
  // heartbeat resends it so the dot does not expire while the user reads;
  // it can never invent one.
  let lastOwnCaret = null;
  // Shared leading + trailing throttle (/static/throttle.js): a throttled
  // caret report is delayed, never dropped. Fallback calls through
  // directly if the shared file failed to load (offline first paint).
  var throttleFn = function (fn) { return fn; };
  if (window.GogogoThrottle && window.GogogoThrottle.throttleTrailing) {
    throttleFn = window.GogogoThrottle.throttleTrailing;
  }
  var reportCaretWire = throttleFn(function (pos, line) {
    fetch("/api/notes/" + encodeURIComponent(DOC) + "/caret?clientID=" + encodeURIComponent(CID), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ line: line, pos: pos }),
    }).catch(function () { /* ephemeral */ });
  }, 200);
  function maybeCaret() {
    // While our own ops are unconfirmed the batch already carries the
    // exact caret, so a standalone report (anchored to the stale
    // serverText) must not overwrite it. Selectionchange fires on every
    // keystroke too, which is why the guard lives here and not just at
    // the input handler.
    if (outbox.length || flushing) return;
    // Only a FOCUSED textarea reports where its user is. Unfocused, the
    // selection is not the user's: the browser leaves it at the end of
    // whatever value was last assigned to it. Reading it anyway (which the
    // old forced heartbeat path did) published an idle tab's borrowed
    // selection as a live caret.
    if (document.activeElement !== ta) {
      lastOwnCaret = null;
      return;
    }
    const pos = ta.selectionStart || 0;
    // A standalone report travels WITHOUT its text, so the peer anchors it
    // to the last text it confirmed — while a keystroke is still pending (the
    // 100ms debounce, or a batch in flight) our value is AHEAD of that base,
    // and the same offset lands somewhere else in the peer's copy. The batch
    // carries the exact caret with its text instead, so stay quiet here and
    // let flush() report the final position once the base catches up.
    if (ta.value !== serverText) return;
    // Even when there is nothing to send, this IS where we are in the text
    // the server knows: the 5s heartbeat resends it, so it must not go stale
    // (a stale resend is the same wrong-place dot by another route).
    lastOwnCaret = { pos: pos, text: ta.value };
    if (pos === caretSent) return; // unchanged since the last report
    caretSent = pos;
    reportCaretWire(pos, ta.value.slice(0, pos).split("\n").length);
  }
  // heartbeatCaret resends the last OWN caret, so a focused-but-idle
  // reader's dot stays alive without inventing a position. No-op for a tab
  // that has never placed one.
  function heartbeatCaret() {
    if (outbox.length || flushing) return;
    if (document.activeElement !== ta || !lastOwnCaret) return;
    if (ta.value !== serverText) return; // pending keystroke: the batch owns it
    caretSent = lastOwnCaret.pos;
    reportCaretWire(lastOwnCaret.pos,
      lastOwnCaret.text.slice(0, lastOwnCaret.pos).split("\n").length);
  }
  function peerColor(user) {
    let h = 0;
    for (let i = 0; i < user.length; i++) h = (h * 31 + user.charCodeAt(i)) >>> 0;
    return "hsl(" + (h % 360) + ",70%,45%)";
  }
  // caretXY maps an offset to pixels. `text` is the snapshot `offset` was
  // measured against (the reporting peer's document); it defaults to our
  // own textarea when a report carries no snapshot (the standalone
  // selectionchange/heartbeat path). The mirror is the reporter's text in
  // OUR font metrics and OUR width, so the dot's line and column are the
  // ones this editor would give that offset.
  //
  // Two things the mirror must not do, both of which moved the dot:
  //
  //  1. It must not add a character. The caret used to be measured with a
  //     zero-width-space marker, and "zero width" is not "no character": a
  //     ZWSP is a break opportunity, so at a soft wrap the marker stayed on
  //     the PREVIOUS line (measured: mirror marker top=10px while the range
  //     at the same offset is at 32px, one line down). Every time the caret
  //     landed exactly on a wrap boundary — once per typed line — the peer's
  //     dot jumped up a row and came back on the next keystroke. A Range at
  //     the offset needs no insertion at all, so the mirror's wrapping is
  //     byte-identical to what the reporter's text would render.
  //  2. It must not change the text column's width. Border widths were copied
  //     from the textarea along with box-sizing:border-box, which made the
  //     mirror's content box two pixels narrower than the textarea's — a
  //     wrap divergence waiting for a line whose break lands in those pixels.
  //     The mirror is borderless and `width` is the textarea's clientWidth
  //     (content + padding, no border), so both text columns are the same.
  function caretXY(offset, text) {
    const src = typeof text === "string" ? text : ta.value;
    const at = Math.max(0, Math.min(offset, src.length));
    const cs = getComputedStyle(ta);
    const lineH = parseFloat(cs.lineHeight) || 20;
    const padTop = parseFloat(cs.paddingTop) || 0;
    const padLeft = parseFloat(cs.paddingLeft) || 0;
    const mirror = document.createElement("div");
    const props = ["fontFamily", "fontSize", "fontWeight", "lineHeight", "letterSpacing", "textTransform", "textIndent", "paddingTop", "paddingRight", "paddingBottom", "paddingLeft", "borderTopWidth", "borderRightWidth", "borderBottomWidth", "borderLeftWidth", "boxSizing", "whiteSpace", "wordWrap", "overflowWrap", "wordBreak", "tabSize"];
    for (const p of props) { try { mirror.style[p] = cs[p]; } catch (err) {} }
    if (ta.wrap === "off") mirror.style.whiteSpace = "pre";
    else { mirror.style.whiteSpace = "pre-wrap"; mirror.style.wordWrap = "break-word"; }
    mirror.style.boxSizing = "border-box";
    mirror.style.borderTopWidth = "0";
    mirror.style.borderRightWidth = "0";
    mirror.style.borderBottomWidth = "0";
    mirror.style.borderLeftWidth = "0";
    mirror.style.position = "absolute";
    mirror.style.visibility = "hidden";
    mirror.style.top = ta.offsetTop + "px";
    mirror.style.left = ta.offsetLeft + "px";
    mirror.style.width = ta.clientWidth + "px";
    const wrap = document.getElementById("note-wrap") || document.body;
    // The WHOLE document, not just the prefix: a mirror holding less text
    // would wrap at places the reporter's never does.
    mirror.textContent = src;
    wrap.appendChild(mirror);
    const origin = mirror.getBoundingClientRect();
    let relX = padLeft;
    let relY = padTop;
    let h = lineH;
    const node = mirror.firstChild;
    if (node) {
      const range = document.createRange();
      range.setStart(node, at);
      range.collapse(true);
      const rects = range.getClientRects();
      const rect = rects.length ? rects[rects.length - 1] : range.getBoundingClientRect();
      if (rect && (rect.width || rect.height)) {
        relX = rect.left - origin.left;
        relY = rect.top - origin.top;
        h = rect.height || lineH;
      }
    }
    mirror.remove();
    return { x: ta.offsetLeft + relX, y: ta.offsetTop + relY, h: h };
  }
  // impliedLine counts the line an offset falls on in `text`, by the SAME
  // rule the reporter used (slice then split on "\n"). When it disagrees
  // with the reported line, the snapshot is not the reporter's — their text
  // has run ahead of ours (an op we have not rendered yet) — so the dot
  // cannot be placed honestly.
  function impliedLine(pos, text) {
    if (typeof text !== "string") return -1;
    return text.slice(0, Math.max(0, Math.min(pos, text.length))).split("\n").length;
  }
  // setCaret is the SINGLE gate every caret report goes through. It stores
  // {line, pos, text} only when the snapshot can actually justify the dot's
  // line; a report that cannot is used for presence only (seen refreshed)
  // and the last honest position is kept. This is what stops the "dot flashes
  // on the wrong line during the other user's typing delay" — text that
  // reaches us a frame before its caret snapshot is never used as a basis.
  // own marks a report that arrived WITH the sender's text (op-attached):
  // its offset is already in our coordinates and must not be shifted again.
  function setCaret(id, label, line, pos, text, own) {
    if (typeof text === "string" && impliedLine(pos, text) !== line) {
      const prev = carets.get(id);
      if (prev) prev.seen = Date.now();
      return;
    }
    carets.set(id, { label: label, line: line, pos: pos, text: text, seen: Date.now(), own: !!own });
  }
  function renderCarets() {
    const layer = document.getElementById("caret-layer");
    if (!layer) return;
    layer.style.position = "absolute";
    layer.style.left = "0"; layer.style.top = "0";
    layer.style.right = "0"; layer.style.bottom = "0";
    layer.style.overflow = "hidden";
    layer.style.pointerEvents = "none";
    layer.innerHTML = "";
    const now = Date.now();
    carets.forEach(function (c, id) {
      if (now - c.seen > 8000) { carets.delete(id); return; }
      const p = caretXY(c.pos, c.text);
      const x = p.x - ta.scrollLeft, y = p.y - ta.scrollTop;
      const ox = ta.offsetLeft, oy = ta.offsetTop;
      if (x < ox || y < oy || x > ox + ta.clientWidth || y > oy + ta.clientHeight) return;
      const label = c.label || id;
      const dot = document.createElement("span");
      dot.title = label + " · line " + c.line;
      dot.style.position = "absolute";
      dot.style.left = Math.round(x) + "px";
      dot.style.top = Math.round(y) + "px";
      dot.style.width = "3px";
      dot.style.height = Math.max(12, Math.round(p.h)) + "px";
      dot.style.background = peerColor(label);
      dot.style.boxShadow = "0 0 0 1px rgba(255,255,255,.7)";
      layer.appendChild(dot);
      // Name flag in the caret's own color, floating above the line —
      // the balloon: who, exactly where. pointer-events none so it never
      // eats clicks meant for the textarea.
      const flag = document.createElement("span");
      flag.textContent = label;
      flag.title = label + " · line " + c.line;
      flag.style.position = "absolute";
      flag.style.left = Math.round(x) + "px";
      flag.style.top = Math.round(y) + "px";
      flag.style.transform = "translate(2px,-100%)";
      flag.style.background = peerColor(label);
      flag.style.color = "#fff";
      flag.style.fontSize = "10px";
      flag.style.lineHeight = "1.4";
      flag.style.padding = "0 5px";
      flag.style.borderRadius = "4px";
      flag.style.whiteSpace = "nowrap";
      flag.style.pointerEvents = "none";
      layer.appendChild(flag);
    });
  }
  ta.addEventListener("blur", function () {
    // Converge a paused editor without stealing the caret mid-thought:
    // adopt server text only when nothing local is unconfirmed.
    if (clean && !flushing && !outbox.length && serverText !== ta.value) ta.value = serverText;
  });

  // Typing indicator ("X is typing…"): throttled reports while
  // typing, one stopped after 3s idle. Server relays only — no state, no
  // persistence. Peers expire entries client-side (6s without refresh),
  // so a dropped "stopped" cannot stick a ghost typist. English
  // microcopy throughout; keep it that way if the pill is copied.
  const typists = new Map();
  let typingLast = 0;
  let typingSent = false;
  let idleTimer = null;
  function reportTyping() {
    const now = Date.now();
    if (!typingSent || now - typingLast > 2500) {
      typingSent = true;
      typingLast = now;
      sendTyping(true);
    }
    clearTimeout(idleTimer);
    idleTimer = setTimeout(function () { typingSent = false; sendTyping(false); }, 3000);
  }
  function sendTyping(on) {
    fetch("/api/notes/" + encodeURIComponent(DOC) + "/typing?clientID=" + encodeURIComponent(CID), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ typing: on }),
    }).catch(function () { /* ephemeral: loss just hides the pill */ });
  }
  function renderTypists() {
    if (!typing) return;
    const now = Date.now();
    const live = [];
    typists.forEach(function (entry, id) {
      const seen = (entry && typeof entry === "object") ? entry.seen : entry;
      const label = (entry && typeof entry === "object" && entry.label) ? entry.label : id;
      if (now - seen < 6000) live.push(label);
      else typists.delete(id);
    });
    if (!live.length) { typing.textContent = ""; return; }
    const names = live.slice(0, 2).join(" and ");
    const more = live.length > 2 ? " and others" : "";
    typing.textContent = names + more + (live.length === 1 ? " is typing…" : " are typing…");
  }
  setInterval(renderTypists, 2000);
  setInterval(renderCarets, 2000); // expiry sweep for stale peer carets
  if (outbox.length) flush();
  window.addEventListener("online", flush);

  // Adopt remote text without stealing the caret mid-thought. The base
  // (REV/serverText) ALWAYS advances — that is what keeps the next local
  // diff positioned correctly. Rendering happens when the tab is in
  // background, or when the user has been idle with nothing unconfirmed;
  // an active typist keeps local content (their keystrokes still send
  // against the fresh base). Caret is saved and clamped on render.
  // A peer's text we could not render yet (we were mid-keystroke): kept so
  // the deferred render is RETRIED instead of waiting for the next event.
  // Without this a tab that stopped typing right after a peer's op kept a
  // stale textarea forever — the peer's letters (and their mirrored text)
  // never appeared until someone typed again.
  let pendingAdopt = null;
  function adoptRemote(text, rev) {
    if (typeof text !== "string") return;
    // A deferred adopt (pendingAdopt) can outlive the revision it came from: a
    // 409 rebase may have advanced REV past it. Applying the older text then
    // would replace the merged text and lose our own insert on screen.
    if (typeof rev === "number" && rev < REV) { pendingAdopt = null; return; }
    if (typeof rev === "number" && rev > REV) { REV = rev; serverText = text; }
    if (text === ta.value) { clean = !outbox.length; pendingAdopt = null; return; }
    if (outbox.length || flushing) { pendingAdopt = { text: text, rev: rev }; return; }
    const focused = document.activeElement === ta;
    if (focused && Date.now() - lastInputAt < 1500) { pendingAdopt = { text: text, rev: rev }; return; }
    const s = ta.selectionStart, e = ta.selectionEnd;
    ta.value = text;
    clean = true;
    pendingAdopt = null;
    if (focused) { try { ta.setSelectionRange(Math.min(s, text.length), Math.min(e, text.length)); } catch (err) {} }
  }
  // Retry the deferred adopt once the focused-typist window has passed (or
  // the outbox drained). Cheap: it no-ops when nothing is pending.
  setInterval(function () {
    if (pendingAdopt) adoptRemote(pendingAdopt.text, pendingAdopt.rev);
  }, 250);

  function diffOps(cur, base) {
    let p = 0;
    while (p < base.length && p < cur.length && base[p] === cur[p]) p++;
    let s = 0;
    while (s < base.length - p && s < cur.length - p &&
      base[base.length - 1 - s] === cur[cur.length - 1 - s]) s++;
    const ops = [];
    const delLen = base.length - p - s;
    if (delLen > 0) ops.push({ t: "del", i: p, n: delLen });
    const ins = cur.slice(p, cur.length - s);
    if (ins) ops.push({ t: "ins", i: p, s: ins });
    return ops;
  }

  // ---- rebase (the 409 that used to erase the peer's typing) --------------
  //
  // A stale batch was "fixed" by recomputing the whole tail from the local
  // textarea: B's textarea never saw A's concurrent insert, so the recomputed
  // diff DELETED A's characters at that spot. That is the reported "one browser
  // lost typed text on the same line". The fix maps our op positions onto the
  // peer's change instead: two inserts at the same spot both survive (ours
  // after theirs), and an edit outside the change shifts by its delta.
  //
  // shiftPos maps a position in the OLD base to the new text. Obsolete
  // positions (inside text the peer deleted) return -1: the caller must fall
  // back to the old recompute, which is lossy but honest about the overlap.
  function shiftPos(remote, i, after) {
    let shift = 0;
    for (let k = 0; k < remote.length; k++) {
      const r = remote[k];
      if (r.t === "ins") {
        if (after ? r.i <= i : r.i < i) shift += r.s.length;
        continue;
      }
      const end = r.i + r.n;
      if (i >= end) { shift -= r.n; continue; }
      if (i <= r.i) continue;
      return -1; // inside the peer's deletion
    }
    return i + shift;
  }

  // transformOps rebases one batch. `after=true` at an insert puts ours after a
  // peer insert at the same index; the delete start uses it so an insertion at
  // the deleted range's first character is kept, while the end does not (an
  // insert at the end boundary is outside the range).
  function transformOps(ops, remote) {
    const out = [];
    for (let k = 0; k < ops.length; k++) {
      const op = ops[k];
      if (!op) return null;
      if (op.t === "ins") {
        const at = shiftPos(remote, op.i, true);
        if (at < 0) return null;
        out.push({ t: "ins", i: at, s: op.s });
        continue;
      }
      if (op.t !== "del") return null;
      const a = shiftPos(remote, op.i, true);
      const b = shiftPos(remote, op.i + op.n, false);
      if (a < 0 || b < 0 || b - a !== op.n) return null;
      out.push({ t: "del", i: a, n: op.n });
    }
    return out;
  }

  function sendDiff() {
    const ops = diffOps(ta.value, serverText);
    if (!ops.length) return;
    try { localStorage.setItem(draftKey, ta.value); }
    catch (err) { offlinePolicy.reportStorageDrop("notes-draft"); }
    shiftPeerOffsets(ops);
    renderCarets();
    outbox.push({ ops: ops, base: REV });
    saveOutbox();
    rebasedRemote = null; // a new batch supersedes the last 409's context
    clean = false;
    attempts = 0;
    flush();
  }

  // transientRetryMs grows while the app keeps answering with a transient
  // status: one retry per 1.5s would be a request storm against an edge that
  // is already struggling, and one per 10s is enough to notice a recovery.
  let transientRetryMs = 1500;

  // The batch on the wire, with the textarea text and the authoritative text it
  // was diffed against. A 409 needs the BASE the ops were built on; by the time
  // the answer lands, the stream may already have advanced serverText.
  let sentOps = null;
  let sentBase = "";
  let lastSentText = "";
  let rebasedRemote = null; // remote ops of the last 409 rebase (for the caret)

  // flush drains ONE batch, under a per-document lock. The queue lives in
  // localStorage, so two tabs of the same note hold the same entries; without
  // the lock both replay the head batch and the insert lands twice (text ops
  // are not idempotent). Under the lock the persisted queue is authoritative:
  // a peer tab may already have delivered what this tab still remembers.
  function flush() {
    if (!outbox.length || flushing) return;
    flushing = true;
    setPending(true);
    flushLocked().catch(function () {
      flushing = false;
      setPending(false);
    });
  }

  function flushLocked() {
    // ONE lock acquisition drains until the queue is empty: re-requesting the
    // same lock from inside the holder queues behind itself and never resolves.
    return offlinePolicy.withLock("gogogo:notes:" + DOC, function drain() {
      outbox = loadOutbox();
      if (!outbox.length) {
        flushing = false;
        setPending(false);
        clean = true;
        return;
      }
      return sendHead().then(function (again) { return again ? drain() : undefined; });
    }).catch(function () {
      flushing = false;
      setPending(false);
    });
  }

  function sendHead() {
    const t0 = (typeof performance !== "undefined") ? performance.now() : 0;
    const head = outbox[0];
    // Caret rides WITH the batch: the server echoes it on the same
    // note-text event as the resolved text, so dot and letters arrive
    // atomically — no cross-channel race where the dot lands before its
    // text exists.
    sentOps = (head.ops || []).slice();
    sentBase = serverText;
    lastSentText = ta.value;
    const atSend = ta.selectionStart || 0;
    const sendBody = { base: head.base, ops: head.ops,
      caret: { line: ta.value.slice(0, atSend).split("\n").length, pos: atSend } };
    // Remember the position that just went out WITH its text. Every keystroke
    // also fires selectionchange, and without this the same offset was
    // reported a second time as a standalone caret (no text) right after the
    // batch drained — two channels for one caret, the exact one and an
    // approximate one, alternating on the peer's screen. This makes that
    // duplicate a no-op ("unchanged since the last report").
    caretSent = atSend;
    // RETURNED, not fired and forgotten: the caller runs this under a Web Lock,
    // and the lock is only held while the returned promise is pending. Without
    // it the lock covered the setup and released before the request.
    return fetch("/api/notes/" + encodeURIComponent(DOC) + "/op?clientID=" + encodeURIComponent(CID), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(sendBody),
    }).then(function (r) {
      if (r.status === 401 || r.status === 403) {
        // Session dead (or never existed): KEEP the batch — dropping
        // would silently eat the user's typing. Banner explains + links
        // to login; the next 200 clears it. (Our endpoints are warn-only
        // today so this fires only if hardening lands — the path must
        // still be correct when it does.)
        flushing = false;
        setPending(false);
        setSession(false);
        return false;
      }
      if (r.status !== 200 && r.status !== 409) {
        // The edge or the app did not answer (5xx, 429, 408): keep the batch
        // and retry. This is the unreachable-app case the outbox exists for;
        // dropping it here lost the user's text with the server merely down.
        if (offlinePolicy.isTransientStatus(r.status)) {
          flushing = false;
          setPending(false);
          net.classList.remove("hidden");
          setTimeout(flush, transientRetryMs);
          transientRetryMs = offlinePolicy.retryDelay(transientRetryMs, 1500, 10000);
          return false;
        }
        // Poison batch (invalid ops — only producible via devtools, our
        // UI only emits ins/del): drop it instead of retrying forever.
        // Network failures reject below and keep the batch.
        outbox.shift();
        saveOutbox();
        flushing = false;
        setPending(false);
        // A poison entry must not wedge the rest: keep draining what is valid.
        return outbox.length > 0;
      }
      return r.json().then(function (d) { return { status: r.status, body: d }; },
        function () { return { status: r.status, body: null }; });
    }).then(function (res) {
      flushing = false;
      setPending(false);
      if (!res || !res.body) return false;
      const d = res.body;
      if (res.status === 409) {
        // Stale base: adopt authority, recompute everything queued
        // against it, retry bounded (a second 409 means another race;
        // the next local input recomputes anyway).
        attempts++;
        if (res.body.authed === true) setSession(true);
        else if (res.body.authed === false) setSession(false);
        // Rebase against the NEWEST text we know. The stream may have advanced
        // past the 409 body while our batch was in flight; adopting only the
        // body (and sending with a newer base) would apply positions valid for
        // an older text.
        adoptRemote(res.body.text, res.body.rev);
        const remote = diffOps(serverText, sentBase || serverText);
        const moved = transformOps(sentOps || [], remote);
        if (moved && moved.length) {
          outbox = [{ ops: moved, base: REV }];
          saveOutbox();
          clean = false;
          rebasedRemote = remote;
        } else {
          // Overlap we cannot map (a delete across the peer's edit): fall back
          // to the recompute, the documented lossy path.
          rebasedRemote = null;
          requeue();
        }
        clean = !outbox.length;
        // Bounded: a second 409 means another race landed; the next local
        // input recomputes anyway.
        const again = outbox.length > 0 && attempts < 5;
        if (!again) attempts = 0;
        return again;
      }
      if (typeof d.text === "string") {
        serverText = d.text;
        if (typeof d.rev === "number") REV = d.rev;
        if (d.authed === true) setSession(true);
        else if (d.authed === false) setSession(false);
        outbox.shift();
        // A rebased batch landed: the server text is the MERGE (the peer's
        // insert plus ours), while the textarea still holds only our side.
        // Adopt it before recomputing, or the stale textarea produces a delete
        // that erases the peer's text a second time. Only when the user has not
        // typed since the send — newer keystrokes are recomputed as before.
        const rebase = rebasedRemote;
        rebasedRemote = null; // consume once: a later success must not reuse it
        if (!outbox.length && rebase && ta.value === lastSentText) {
          const caret = shiftPos(rebase, ta.selectionStart || 0, true);
          ta.value = d.text;
          const at = caret < 0 ? d.text.length : Math.min(caret, d.text.length);
          try { ta.setSelectionRange(at, at); } catch (err) { /* non-fatal */ }
          // The deferred adopt (if any) holds the PRE-merge text; keeping it
          // would let the 250ms interval overwrite the merged text.
          pendingAdopt = null;
        }
        // Recompute the remainder against the fresh base: queued ops were
        // positioned on older text and would 409 for sure.
        const rest = diffOps(ta.value, serverText);
        outbox = rest.length ? [{ ops: rest, base: REV }] : [];
        saveOutbox();
        clean = !outbox.length;
        transientRetryMs = 1500;
        if (t0 && rtt) { lastRTT = "· " + Math.round(performance.now() - t0) + "ms"; rtt.textContent = lastRTT; }
        adoptRemote(d.text, d.rev);
        net.classList.add("hidden");
        // The base caught up with our text: a caret that moved while the
        // batch was in flight (arrow keys, a click) could not be reported
        // above, so report it now instead of waiting for the 5s heartbeat.
        maybeCaret();
        return outbox.length > 0;
      }
      return false;
    }).catch(function () {
      flushing = false;
      setPending(false);
      net.classList.remove("hidden");
      return false;
    });
  }

  // Recompute the whole queue against current serverText as one batch.
  function requeue() {
    const rest = diffOps(ta.value, serverText);
    outbox = rest.length ? [{ ops: rest, base: REV }] : [];
    saveOutbox();
    clean = !outbox.length;
  }

  let lastChime = 0;
  function chimeJoin() {
    const now = Date.now();
    if (now - lastChime < 10000) return;
    lastChime = now;
    try {
      if (window.Cuelume && window.Cuelume.play) window.Cuelume.play("chime");
    } catch (err) { /* sound off or unavailable: presence stays visual */ }
  }

  // Session banner: the shared controller (/static/session.js) owns the
  // DOM; this stays a local alias so every call site reads the same.
  // Fallback keeps it a no-op if the shared file missed the first paint.
  // Shown on authed:false (op answers, stream session frame) or HTTP
  // 401/403; hidden on the next authed:true. Never auto-redirects — that
  // would strand unsent textarea content (the user re-logs in and keeps
  // typing).
  function setSession(on) {
    if (window.GogogoSession && window.GogogoSession.set) window.GogogoSession.set(on);
  }

  const es = new EventSource(
    "/api/notes/" + encodeURIComponent(DOC) + "/stream?clientID=" + encodeURIComponent(CID)
  );
  es.onmessage = function (e) {
    if (!e.data || e.data[0] === ":") return;
    let m;
    try {
      m = JSON.parse(e.data);
    } catch (err) {
      return;
    }
    // Doc guard (defense in depth — the hub already scopes fan-out per
    // doc): an event for another note must never touch this note's text
    // or carets. Without it a tab adopted any note's text and posted the
    // mix back, ending with two notes each holding part of the other.
    if (m.doc && m.doc !== DOC) return;
    if (m.type === "session") {
      // Initial per-connection auth state (also re-sent never: SSE is one
      // long request, so mid-stream expiry surfaces via op answers).
      if (m.authed === true) setSession(true);
      else if (m.authed === false) setSession(false);
      return;
    }
    if (m.type === "note-text" && typeof m.text === "string" && typeof m.rev === "number") {
      const sender = peerID(m);
      if (sender !== CID) {
        adoptRemote(m.text, m.rev);
        // Atomic caret: rendered from the same event as the text it
        // belongs to — dot and letters can never disagree. own=true: this
        // offset is expressed in the text that just merged into ours.
        if (m.caret && typeof m.caret.line === "number" && typeof m.caret.pos === "number") {
          setCaret(sender, peerLabel(m), Math.max(1, Math.round(m.caret.line)), Math.max(0, Math.round(m.caret.pos)), m.text, true);
          renderCarets();
        }
      }
      else if (m.rev > REV) { REV = m.rev; serverText = m.text; }
      net.classList.add("hidden");
    }
    // Presence pill: rendered from the authoritative count, never by
    // incrementing locally — every tab agrees even on missed events.
    // A peer joining while you're here gets one soft chime (throttled):
    // arrival is the one presence moment worth hearing; text, typing and
    // carets stay silent or the room becomes unbearable.
    if (peers && (m.type === "count" || m.type === "join" || m.type === "leave") && m.doc === DOC) {
      if (m.type === "count" && Array.isArray(m.peers)) {
        peers.textContent = m.peers.length + " online";
      }
      if (m.type === "join" && peerID(m) && peerID(m) !== CID) chimeJoin();
    }
    // Typing pill: peers announce intent; a leave also clears a stuck
    // typist immediately instead of waiting for the 6s expiry. Keyed by
    // per-tab id (same-user tabs type independently); the pill shows the
    // display label.
    if (m.doc === DOC && peerID(m) && peerID(m) !== CID) {
      const id = peerID(m), label = peerLabel(m);
      if (m.type === "typing") typists.set(id, { label: label, seen: Date.now() });
      else if (m.type === "stopped" || m.type === "leave") {
        typists.delete(id);
        if (m.user) typists.delete(m.user); // pre-from senders keyed by name
      }
      renderTypists();
    }
    // Caret dots: positions are approximate (offsets reported against the
    // peer's text state), the tooltip line is exact. A leave clears the
    // dot at once instead of waiting for the 8s expiry.
    if (m.doc === DOC && peerID(m) && peerID(m) !== CID) {
      const id = peerID(m);
      if (m.type === "caret" && typeof m.x === "number" && typeof m.y === "number") {
        // Standalone caret (selectionchange/heartbeat): no text snapshot on
        // the wire, so anchor to the authoritative text we hold. It is the
        // reporter's view minus any op still in flight, which is the closest
        // honest basis and far better than our possibly-diverged ta.value.
        // setCaret discards it when that basis cannot justify the line.
        // own=false: this offset is anchored to the reporter's text, so it
        // must be shifted through our unconfirmed ops.
        setCaret(id, peerLabel(m), Math.max(1, Math.round(m.x)), Math.max(0, Math.round(m.y)), serverText, false);
        renderCarets();
      } else if (m.type === "leave") {
        carets.delete(id);
        if (m.user) carets.delete(m.user); // pre-from senders keyed by name
        renderCarets();
      }
    }
  };
  es.onerror = function () { net.classList.remove("hidden"); };
})();
