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
  if (!ta) return;

  let lastSent = ta.value;
  let timer = null;
  const draftKey = "notes-draft:" + DOC;
  // Demo-grade offline cushion: if the server rendered empty but this
  // browser typed before (reload during an outage), restore the draft.
  // Server text always wins when non-empty — last-writer-per-browser only.
  try {
    if (!ta.value && localStorage.getItem(draftKey)) ta.value = localStorage.getItem(draftKey);
    lastSent = ta.value;
  } catch (err) { /* private mode: no draft */ }
  ta.addEventListener("input", function () {
    clearTimeout(timer);
    timer = setTimeout(sendDiff, 300);
  });

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
    fetch("/api/notes/" + encodeURIComponent(DOC) + "/op?clientID=" + encodeURIComponent(CID), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ ops: ops }),
    }).catch(function () { net.classList.remove("hidden"); });
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
  };
  es.onerror = function () { net.classList.remove("hidden"); };
})();
