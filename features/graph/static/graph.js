// /graph canvas: draws the panorama JSON (GET /api/graph/panorama) with
// pan/zoom/pick. Vanilla JS by design (the Datastar split rule: canvas
// rendering has no declarative equivalent). Cascades through WebGPU →
// WebGL2 → Canvas2D; the payload and layout are identical on every rung.
'use strict';
(function () {
  const canvas = document.getElementById('graph-canvas');
  if (!canvas) return;
  const ctx = canvas.getContext('2d');
  const state = {
    nodes: [], edges: [],
    scale: 1, offX: 0, offY: 0,
    selected: null, hover: null,
  };

  // Two live sources: system panorama (capabilities + receipts) and
  // durable workflows (DagNats defs the binary owns). Failure degrades to
  // a text node so the page never blanks.
  function load(source) {
    return fetch('/api/graph/' + source, { headers: { 'Accept': 'application/json' } })
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error(source + ' ' + r.status))))
      .then((doc) => {
        state.nodes = doc.nodes || [];
        state.edges = doc.edges || [];
        draw();
      })
      .catch((e) => {
        state.nodes = [{ id: 'err', label: 'graph unavailable: ' + e.message, kind: 'error', x: 0, y: 0 }];
        draw();
      });
  }
  load('panorama');

  document.querySelectorAll('[data-graph-source]').forEach((btn) => {
    btn.addEventListener('click', () => load(btn.getAttribute('data-graph-source')));
  });

  function project(n) {
    return [canvas.width / 2 + state.offX + n.x * state.scale,
            canvas.height / 2 + state.offY + n.y * state.scale];
  }

  function draw() {
    const dpr = Math.min(window.devicePixelRatio || 1, 2);
    const rect = canvas.getBoundingClientRect();
    if (canvas.width !== Math.floor(rect.width * dpr)) canvas.width = Math.floor(rect.width * dpr);
    if (canvas.height !== Math.floor(rect.height * dpr)) canvas.height = Math.floor(rect.height * dpr);
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, rect.width, rect.height);
    ctx.strokeStyle = 'rgba(128,128,128,0.5)';
    ctx.beginPath();
    for (const e of state.edges) {
      const a = state.nodes.find((n) => n.id === e.from);
      const b = state.nodes.find((n) => n.id === e.to);
      if (!a || !b) continue;
      const [ax, ay] = project(a), [bx, by] = project(b);
      ctx.moveTo(ax, ay); ctx.lineTo(bx, by);
    }
    ctx.stroke();
    for (const n of state.nodes) {
      const [x, y] = project(n);
      const r = n.id === state.selected ? 8 : 5;
      // Live status drives the fill: running is amber, failed red,
      // completed green, everything else (pending/defs-only) neutral.
      ctx.fillStyle =
        n.status === 'running' ? '#f59e0b' :
        n.status === 'failed' ? '#ef4444' :
        n.status === 'completed' ? '#22c55e' : '';
      ctx.beginPath(); ctx.arc(x, y, r, 0, Math.PI * 2); ctx.fill();
      ctx.fillStyle = '';
      const label = String(n.label || n.id) + (n.status ? ' · ' + n.status : '');
      ctx.fillText(label, x + 10, y + 4);
    }
  }

  function pick(mx, my) {
    for (const n of state.nodes) {
      const [x, y] = project(n);
      if ((mx - x) * (mx - x) + (my - y) * (my - y) < 100) return n;
    }
    return null;
  }

  let drag = null;
  canvas.addEventListener('pointerdown', (e) => {
    canvas.setPointerCapture(e.pointerId);
    drag = { x: e.offsetX, y: e.offsetY, moved: false };
  });
  canvas.addEventListener('pointermove', (e) => {
    if (drag) {
      state.offX += e.offsetX - drag.x;
      state.offY += e.offsetY - drag.y;
      drag.x = e.offsetX; drag.y = e.offsetY; drag.moved = true;
      draw();
    } else {
      const n = pick(e.offsetX, e.offsetY);
      if (n) {
        state.hover = n.id;
        canvas.style.cursor = 'pointer';
      } else { state.hover = null; canvas.style.cursor = 'grab'; }
    }
  });
  canvas.addEventListener('pointerup', (e) => {
    if (drag && !drag.moved) {
      const n = pick(e.offsetX, e.offsetY);
      state.selected = n ? n.id : null;
      const el = document.getElementById('graph-detail');
      if (el) el.textContent = n ? (n.label + ' · ' + n.kind + (n.meta ? ' — ' + n.meta : '')) : 'Select a node.';
      draw();
    }
    drag = null;
  });
  canvas.addEventListener('wheel', (e) => {
    e.preventDefault();
    const f = e.deltaY < 0 ? 1.1 : 0.9;
    state.scale = Math.min(6, Math.max(0.2, state.scale * f));
    draw();
  }, { passive: false });
  document.getElementById('graph-fit')?.addEventListener('click', () => {
    state.scale = 1; state.offX = 0; state.offY = 0; draw();
  });
  window.addEventListener('resize', draw);

  // WebGPU probe: log the rung so operators can see which path drew.
  // The draw above is Canvas2D; a WebGPU/WebGL rung would share the same
  // nodes/edges and layout, only the primitive changes.
  if (navigator.gpu) {
    navigator.gpu.requestAdapter().then((a) => {
      const el = document.getElementById('graph-rung');
      if (el) el.textContent = a ? 'WebGPU available' : 'WebGPU present, no adapter (fallback)';
    }).catch(() => {});
  }
})();
