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
    filters: { core: true, plugin: true, feature: true },
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
        // Radial deterministic layout: angle by index, radius by kind rank
        // so core in center, feature outer — prevents overlap.
        const kinds = ['core','plugin','feature'];
        const byKind = { core: [], plugin: [], feature: [] };
        state.nodes.forEach((n) => {
          const k = kinds.includes(n.kind) ? n.kind : 'feature';
          (byKind[k] = byKind[k] || []).push(n);
        });
        const cx = 0, cy = 0;
        const ringR = { core: 80, plugin: 140, feature: 200 };
        Object.entries(byKind).forEach(([kind, list]) => {
          const r = ringR[kind] || 140;
          list.forEach((n, i) => {
            const a = (i / Math.max(1, list.length)) * Math.PI * 2;
            n._nx = cx + Math.cos(a) * r;
            n._ny = cy + Math.sin(a) * r;
          });
        });
        draw();
      })
      .catch((e) => {
        state.nodes = [{ id: 'err', label: 'graph unavailable: ' + e.message, kind: 'error', _nx: 0, _ny: 0 }];
        draw();
      });
  }
  load('panorama');

  document.querySelectorAll('[data-graph-source]').forEach((btn) => {
    btn.addEventListener('click', () => load(btn.getAttribute('data-graph-source')));
  });

  document.querySelectorAll('[data-graph-filter]').forEach((chk) => {
    chk.addEventListener('change', () => {
      const k = chk.getAttribute('data-graph-filter');
      state.filters[k] = chk.checked;
      draw();
    });
  });

  function project(n) {
    const rect = canvas.getBoundingClientRect();
    return [rect.width / 2 + state.offX + (n._nx || n.x || 0) * state.scale,
            rect.height / 2 + state.offY + (n._ny || n.y || 0) * state.scale];
  }

  function themeColors() {
    const dark = document.documentElement.classList.contains('dark') || document.documentElement.dataset.theme === 'dark';
    return {
      bg: dark ? '#1f2937' : '#ffffff',
      label: dark ? '#e5e7eb' : '#111827',
      edge: dark ? 'rgba(156,163,175,0.6)' : 'rgba(107,114,128,0.5)',
      kind: { core: dark ? '#60a5fa' : '#2563eb', plugin: dark ? '#34d399' : '#059669', feature: dark ? '#f472b6' : '#db2777' }
    };
  }

  function draw() {
    const dpr = Math.min(window.devicePixelRatio || 1, 2);
    const rect = canvas.getBoundingClientRect();
    if (canvas.width !== Math.floor(rect.width * dpr)) canvas.width = Math.floor(rect.width * dpr);
    if (canvas.height !== Math.floor(rect.height * dpr)) canvas.height = Math.floor(rect.height * dpr);
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    const t = themeColors();
    ctx.fillStyle = t.bg;
    ctx.fillRect(0, 0, rect.width, rect.height);
    ctx.strokeStyle = t.edge;
    ctx.beginPath();
    const visible = state.nodes.filter((n) => state.filters[n.kind] !== false);
    for (const e of state.edges) {
      const a = visible.find((n) => n.id === e.from);
      const b = visible.find((n) => n.id === e.to);
      if (!a || !b) continue;
      const [ax, ay] = project(a), [bx, by] = project(b);
      ctx.moveTo(ax, ay); ctx.lineTo(bx, by);
    }
    ctx.stroke();
    // draw nodes with kind colors, labels offset to avoid overlap: put labels to right, clipped
    for (const n of visible) {
      const [x, y] = project(n);
      const r = n.id === state.selected ? 8 : 5;
      ctx.fillStyle = n.status === 'running' ? '#f59e0b' :
        n.status === 'failed' ? '#ef4444' :
        n.status === 'completed' ? '#22c55e' : (t.kind[n.kind] || '#9ca3af');
      ctx.beginPath(); ctx.arc(x, y, r, 0, Math.PI * 2); ctx.fill();
      ctx.fillStyle = t.label;
      ctx.font = '11px system-ui, sans-serif';
      const label = String(n.label || n.id);
      // truncate to 18 chars to avoid overlap
      const short = label.length > 18 ? label.slice(0, 17) + '…' : label;
      ctx.fillText(short, x + 10, y + 4);
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

  if (navigator.gpu) {
    navigator.gpu.requestAdapter().then((a) => {
      const el = document.getElementById('graph-rung');
      if (el) el.textContent = a ? 'WebGPU available' : 'WebGPU present, no adapter (fallback)';
    }).catch(() => {});
  }
})();
