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

  const main = document.querySelector("main[data-doc-id]");
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
  try {
    const raw = JSON.parse(localStorage.getItem(outKey) || "[]");
    for (const e of raw) {
      if (e && Array.isArray(e.ops)) outbox.push(e);
      else if (e && typeof e.t === "string") outbox.push({ ops: [e], base: 0 });
    }
  } catch (err) { outbox = []; }
  function saveOutbox() { try { localStorage.setItem(outKey, JSON.stringify(outbox)); } catch (err) {} }
  ta.addEventListener("input", function () {
    clearTimeout(timer);
    lastInputAt = Date.now();
    timer = setTimeout(sendDiff, 100);
    reportTyping();
    maybeCaret();
    // Deliberately vanilla (not data-on:input__debounce): the timer,
    // lastInputAt (idle adopt), typing and caret reports share one
    // handler by design — splitting the timer into markup would scatter
    // coupled state across two owners for zero protocol gain.
  });
  ta.addEventListener("scroll", function () { renderCarets(); }, { passive: true });
  window.addEventListener("resize", function () { renderCarets(); });
  document.addEventListener("selectionchange", function () { maybeCaret(); });

  // Heartbeat re-announce (OT-demo lesson): an idle peer's dot must not
  // age out while they're still here — force a resend every 5s when the
  // page is visible, even unfocused (readers count as present; only the
  // position goes stale, and the tooltip line stays truthful).
  setInterval(function () {
    if (!document.hidden) { caretSent = -1; maybeCaret(true); }
  }, 5000);

  // Remote carets ("who is where"): peers report {line, pos}; each browser
  // maps the offset to ITS OWN pixels via mirror-div (computed font metrics
  // copied from the live textarea, so zoom, fonts and wrapping can never
  // desync the math — static CSS guessing is what breaks across viewports).
  // Dots are approximate when text moved since the report (offsets go
  // stale); the tooltip line number is the exact, dimension-independent
  // truth. No library, no build step, ~60 lines.
  const carets = new Map(); // user -> {line, pos, seen}
  // shiftPeerOffsets transforms peer offsets through OUR unconfirmed local
  // ops (one-direction OT-lite): we know exactly what changed under their
  // reported positions, so their carets track our typing instead of
  // freezing on stale absolute offsets — the reported "it stops in the
  // wrong place while I keep typing" bug. Runs inside sendDiff, before the
  // batch posts. Deletes overlapping a caret clamp it to the cut point
  // (documented approximation: ownership of the deleted range is gone, the
  // nearest surviving position is the honest answer).
  function shiftPeerOffsets(ops) {
    if (!carets.size) return;
    carets.forEach(function (c) {
      let pos = c.pos;
      for (const op of ops) {
        if (op.t === "ins") {
          if (pos >= op.i) pos += op.s.length;
        } else if (op.t === "del") {
          if (pos > op.i + op.n) pos -= op.n;
          else if (pos > op.i) pos = op.i;
        }
      }
      c.pos = pos;
    });
  }
  let caretSent = -1;
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
  }, 500);
  function maybeCaret(force) {
    const pos = force ? (ta.selectionStart || 0) : (document.activeElement !== ta ? -1 : (ta.selectionStart || 0));
    if (pos < 0) return;
    // Forced (heartbeat) resends even unmoved: expiry is time-based, so
    // only a fresh report proves presence. Normal path skips duplicates.
    if (!force && pos === caretSent) return;
    caretSent = pos;
    reportCaretWire(pos, ta.value.slice(0, pos).split("\n").length);
  }
  function peerColor(user) {
    let h = 0;
    for (let i = 0; i < user.length; i++) h = (h * 31 + user.charCodeAt(i)) >>> 0;
    return "hsl(" + (h % 360) + ",70%,45%)";
  }
  function caretXY(offset) {
    const cs = getComputedStyle(ta);
    const mirror = document.createElement("div");
    const props = ["fontFamily", "fontSize", "fontWeight", "lineHeight", "letterSpacing", "textTransform", "textIndent", "paddingTop", "paddingRight", "paddingBottom", "paddingLeft", "borderTopWidth", "borderRightWidth", "borderBottomWidth", "borderLeftWidth", "boxSizing", "whiteSpace", "wordWrap", "overflowWrap", "wordBreak", "tabSize"];
    for (const p of props) { try { mirror.style[p] = cs[p]; } catch (err) {} }
    if (ta.wrap === "off") mirror.style.whiteSpace = "pre";
    else { mirror.style.whiteSpace = "pre-wrap"; mirror.style.wordWrap = "break-word"; }
    mirror.style.position = "absolute";
    mirror.style.visibility = "hidden";
    mirror.style.top = ta.offsetTop + "px";
    mirror.style.left = ta.offsetLeft + "px";
    mirror.style.width = ta.clientWidth + "px";
    const wrap = document.getElementById("note-wrap") || document.body;
    mirror.textContent = ta.value.substring(0, offset);
    const marker = document.createElement("span");
    marker.textContent = "​";
    mirror.appendChild(marker);
    // Trailing remainder (when any): without it, an offset exactly at a
    // "\n" (caret at a line start) collapses — a newline ending the mirror
    // creates no line box, so the marker lands at the end of the PREVIOUS
    // line and the dot drops one line down until more typing moves it
    // mid-line (the reported bug). The remainder forces every line box to
    // exist, so line-start carets measure on their own line.
    mirror.appendChild(document.createTextNode(ta.value.substring(offset)));
    wrap.appendChild(mirror);
    // Marker is a zero-size inline at the caret: its offset box top is the
    // line top, its height the line height — a text caret, not a dot.
    const x = ta.offsetLeft + marker.offsetLeft;
    const y = ta.offsetTop + marker.offsetTop;
    const h = marker.offsetHeight || parseInt(getComputedStyle(ta).lineHeight, 10) || 20;
    mirror.remove();
    return { x: x, y: y, h: h };
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
    carets.forEach(function (c, user) {
      if (now - c.seen > 8000) { carets.delete(user); return; }
      const p = caretXY(Math.min(c.pos, ta.value.length));
      const x = p.x - ta.scrollLeft, y = p.y - ta.scrollTop;
      const ox = ta.offsetLeft, oy = ta.offsetTop;
      if (x < ox || y < oy || x > ox + ta.clientWidth || y > oy + ta.clientHeight) return;
      const dot = document.createElement("span");
      dot.title = user + " · line " + c.line;
      dot.style.position = "absolute";
      dot.style.left = Math.round(x) + "px";
      dot.style.top = Math.round(y) + "px";
      dot.style.width = "3px";
      dot.style.height = Math.max(12, Math.round(p.h)) + "px";
      dot.style.background = peerColor(user);
      dot.style.boxShadow = "0 0 0 1px rgba(255,255,255,.7)";
      layer.appendChild(dot);
      // Name flag in the caret's own color, floating above the line —
      // the balloon: who, exactly where. pointer-events none so it never
      // eats clicks meant for the textarea.
      const flag = document.createElement("span");
      flag.textContent = user;
      flag.title = user + " · line " + c.line;
      flag.style.position = "absolute";
      flag.style.left = Math.round(x) + "px";
      flag.style.top = Math.round(y) + "px";
      flag.style.transform = "translate(2px,-100%)";
      flag.style.background = peerColor(user);
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
    typists.forEach(function (seen, user) {
      if (now - seen < 6000) live.push(user);
      else typists.delete(user);
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
  function adoptRemote(text, rev) {
    if (typeof text !== "string") return;
    if (typeof rev === "number" && rev > REV) { REV = rev; serverText = text; }
    if (text === ta.value) { clean = !outbox.length; return; }
    if (outbox.length || flushing) return;
    const focused = document.activeElement === ta;
    if (focused && Date.now() - lastInputAt < 1500) return;
    const s = ta.selectionStart, e = ta.selectionEnd;
    ta.value = text;
    clean = true;
    if (focused) { try { ta.setSelectionRange(Math.min(s, text.length), Math.min(e, text.length)); } catch (err) {} }
  }

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

  function sendDiff() {
    const ops = diffOps(ta.value, serverText);
    if (!ops.length) return;
    try { localStorage.setItem(draftKey, ta.value); } catch (err) { /* private mode */ }
    shiftPeerOffsets(ops);
    renderCarets();
    outbox.push({ ops: ops, base: REV });
    saveOutbox();
    clean = false;
    attempts = 0;
    flush();
  }

  function flush() {
    if (!outbox.length || flushing) return;
    flushing = true;
    setPending(true);
    const t0 = (typeof performance !== "undefined") ? performance.now() : 0;
    const head = outbox[0];
    fetch("/api/notes/" + encodeURIComponent(DOC) + "/op?clientID=" + encodeURIComponent(CID), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ base: head.base, ops: head.ops }),
    }).then(function (r) {
      if (r.status !== 200 && r.status !== 409) {
        // Poison batch (invalid ops — only producible via devtools, our
        // UI only emits ins/del): drop it instead of retrying forever.
        // Network failures reject below and keep the batch.
        outbox.shift();
        saveOutbox();
        flushing = false;
        setPending(false);
        if (outbox.length) setTimeout(flush, 1000);
        return null;
      }
      return r.json().then(function (d) { return { status: r.status, body: d }; },
        function () { return { status: r.status, body: null }; });
    }).then(function (res) {
      flushing = false;
      setPending(false);
      if (!res || !res.body) return;
      const d = res.body;
      if (res.status === 409) {
        // Stale base: adopt authority, recompute everything queued
        // against it, retry bounded (a second 409 means another race;
        // the next local input recomputes anyway).
        attempts++;
        adoptRemote(res.body.text, res.body.rev);
        requeue();
        clean = !outbox.length;
        if (outbox.length && attempts < 5) flush();
        else attempts = 0;
        return;
      }
      if (typeof d.text === "string") {
        serverText = d.text;
        if (typeof d.rev === "number") REV = d.rev;
        outbox.shift();
        // Recompute the remainder against the fresh base: queued ops were
        // positioned on older text and would 409 for sure.
        const rest = diffOps(ta.value, serverText);
        outbox = rest.length ? [{ ops: rest, base: REV }] : [];
        saveOutbox();
        clean = !outbox.length;
        if (t0 && rtt) { lastRTT = "· " + Math.round(performance.now() - t0) + "ms"; rtt.textContent = lastRTT; }
        adoptRemote(d.text, d.rev);
        net.classList.add("hidden");
        if (outbox.length) flush();
      }
    }).catch(function () {
      flushing = false;
      setPending(false);
      net.classList.remove("hidden");
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
    if (m.type === "note-text" && typeof m.text === "string" && typeof m.rev === "number") {
      if (m.from !== CID) adoptRemote(m.text, m.rev);
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
      if (m.type === "join" && m.user && m.user !== CID) chimeJoin();
    }
    // Typing pill: peers announce intent; a leave also clears a stuck
    // typist immediately instead of waiting for the 6s expiry.
    if (m.doc === DOC && m.user && m.user !== CID) {
      if (m.type === "typing") typists.set(m.user, Date.now());
      else if (m.type === "stopped" || m.type === "leave") typists.delete(m.user);
      renderTypists();
    }
    // Caret dots: positions are approximate (offsets reported against the
    // peer's text state), the tooltip line is exact. A leave clears the
    // dot at once instead of waiting for the 8s expiry.
    if (m.doc === DOC && m.user && m.user !== CID) {
      if (m.type === "caret" && typeof m.x === "number" && typeof m.y === "number") {
        carets.set(m.user, { line: Math.max(1, Math.round(m.x)), pos: Math.max(0, Math.round(m.y)), seen: Date.now() });
        renderCarets();
      } else if (m.type === "leave") {
        carets.delete(m.user);
        renderCarets();
      }
    }
  };
  es.onerror = function () { net.classList.remove("hidden"); };
})();
