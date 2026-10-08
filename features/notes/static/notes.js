// Notes client — minimal collaborative textarea.
//
// Transport:
//   - SSE stream (`/api/notes/<doc>/stream`) carries resolved-text events:
//       {type:"note-text", doc, from, text} -> replace textarea content
//       (unless focused — never steal the caret).
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

  // Grey-unconfirmed: text the server has not confirmed yet renders dimmed.
  // Confirmation is the POST 200 below — nothing claims a state the server
  // has not accepted (same honesty rule as the OT demo's grey text, but
  // driven by our own round-trip instead of an OT verdict). aria-busy
  // carries the same state programmatically (opacity alone is not enough
  // for forced-colors / screen-reader users).
  function setPending(on) { ta.style.opacity = on ? "0.55" : ""; ta.setAttribute("aria-busy", on ? "true" : "false"); }

  let lastSent = ta.value;
  let timer = null;
  let flushing = false;
  const draftKey = "notes-draft:" + DOC;
  const outKey = "notes-outbox:" + DOC;
  // Demo-grade offline cushion: if the server rendered empty but this
  // browser typed before (reload during an outage), restore the draft.
  // Server text always wins when non-empty — last-writer-per-browser only.
  try {
    if (!ta.value && localStorage.getItem(draftKey)) ta.value = localStorage.getItem(draftKey);
    lastSent = ta.value;
  } catch (err) { /* private mode: no draft */ }
  // Op outbox (whiteboard uses IndexedDB; localStorage is enough for text
  // ops): batches typed while offline replay on reconnect AND on reload.
  // At-least-once: a lost response replays an applied batch, duplicating
  // the insert — text inserts are not idempotent. Demo-grade, documented.
  let outbox = [];
  try { outbox = JSON.parse(localStorage.getItem(outKey) || "[]"); } catch (err) { outbox = []; }
  function saveOutbox() { try { localStorage.setItem(outKey, JSON.stringify(outbox)); } catch (err) {} }
  ta.addEventListener("input", function () {
    clearTimeout(timer);
    timer = setTimeout(sendDiff, 300);
    reportTyping();
  });

  // Typing indicator ("X está digitando…"): throttled reports while
  // typing, one stopped after 3s idle. Server relays only — no state, no
  // persistence. Peers expire entries client-side (6s without refresh),
  // so a dropped "stopped" cannot stick a ghost typist. Portuguese
  // microcopy matches the product voice; keep it if the pill is copied.
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
    const names = live.slice(0, 2).join(" e ");
    const more = live.length > 2 ? " e outros" : "";
    typing.textContent = names + more + (live.length === 1 ? " está digitando…" : " estão digitando…");
  }
  setInterval(renderTypists, 2000);
  if (outbox.length) flush();
  window.addEventListener("online", flush);

  function sendDiff() {
    const cur = ta.value;
    if (cur === lastSent) return;
    let p = 0;
    while (p < lastSent.length && p < cur.length && lastSent[p] === cur[p]) p++;
    let s = 0;
    while (s < lastSent.length - p && s < cur.length - p &&
      lastSent[lastSent.length - 1 - s] === cur[cur.length - 1 - s]) s++;
    const ops = [];
    const delLen = lastSent.length - p - s;
    if (delLen > 0) ops.push({ t: "del", i: p, n: delLen });
    const ins = cur.slice(p, cur.length - s);
    if (ins) ops.push({ t: "ins", i: p, s: ins });
    lastSent = cur;
    if (!ops.length) return;
    try { localStorage.setItem(draftKey, cur); } catch (err) { /* private mode */ }
    for (const op of ops) outbox.push(op);
    saveOutbox();
    flush();
  }

  function flush() {
    if (!outbox.length || flushing) return;
    flushing = true;
    setPending(true);
    const t0 = (typeof performance !== "undefined") ? performance.now() : 0;
    const batch = outbox.slice();
    fetch("/api/notes/" + encodeURIComponent(DOC) + "/op?clientID=" + encodeURIComponent(CID), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ ops: batch }),
    }).then(function (r) {
      if (!r.ok) {
        // Poison batch (invalid ops — only producible via devtools, our
        // UI only emits ins/del): drop it instead of retrying forever.
        // Network failures reject below and keep the batch.
        outbox = outbox.slice(batch.length);
        saveOutbox();
        flushing = false;
        return null;
      }
      return r.json();
    }).then(function (d) {
      flushing = false;
      setPending(false);
      if (!d) return;
      if (typeof d.text === "string") {
        outbox = outbox.slice(batch.length);
        saveOutbox();
        if (t0 && rtt) rtt.textContent = "· " + Math.round(performance.now() - t0) + "ms";
        // Adopt the authoritative text when unfocused (peers may have
        // typed while we were offline); the focused case converges on
        // the next local edit via the stream handler below.
        if (document.activeElement !== ta && d.text !== ta.value) ta.value = d.text;
        lastSent = ta.value;
        net.classList.add("hidden");
      }
      if (outbox.length) setTimeout(flush, 1000);
    }).catch(function () {
      flushing = false;
      setPending(false);
      net.classList.remove("hidden");
    });
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
    if (m.type === "note-text" && m.from !== CID && typeof m.text === "string") {
      // Never steal the caret: a focused textarea keeps local content;
      // it converges on the next local edit.
      if (document.activeElement !== ta) {
        ta.value = m.text;
        lastSent = m.text;
      }
      net.classList.add("hidden");
    }
    // Presence pill: rendered from the authoritative count, never by
    // incrementing locally — every tab agrees even on missed events.
    if (peers && (m.type === "count" || m.type === "join" || m.type === "leave") && m.doc === DOC) {
      if (m.type === "count" && Array.isArray(m.peers)) {
        peers.textContent = m.peers.length + " online";
      }
    }
    // Typing pill: peers announce intent; a leave also clears a stuck
    // typist immediately instead of waiting for the 6s expiry.
    if (m.doc === DOC && m.user && m.user !== CID) {
      if (m.type === "typing") typists.set(m.user, Date.now());
      else if (m.type === "stopped" || m.type === "leave") typists.delete(m.user);
      renderTypists();
    }
  };
  es.onerror = function () { net.classList.remove("hidden"); };
})();
