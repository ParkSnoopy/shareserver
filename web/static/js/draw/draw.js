// Freehand shape primitives. Every outline gets a slow, seeded wobble, so a
// pill or an oval never has two matching corners.
(function (root) {
  'use strict';
  const BM = root.BM || (root.BM = {});
  const TAU = Math.PI * 2;
  const f1 = (n) => Math.round(n * 10) / 10;

  // Subdivide a polyline so the wobble has points to move.
  function dense(pts, step, closed) {
    const src = closed ? pts.concat([pts[0]]) : pts;
    const out = [];
    for (let i = 0; i < src.length - 1; i++) {
      const [ax, ay] = src[i];
      const [bx, by] = src[i + 1];
      const n = Math.max(1, Math.round(Math.hypot(bx - ax, by - ay) / step));
      for (let j = 0; j < n; j++) out.push([ax + ((bx - ax) * j) / n, ay + ((by - ay) * j) / n]);
    }
    if (!closed) out.push(src[src.length - 1]);
    return out;
  }

  function wobble(pts, seed, amp, closed) {
    const r = BM.rng(seed);
    const n = pts.length;
    const waves = [];
    for (let k = 1; k <= 4; k++) waves.push([amp / k, r() * TAU, closed ? k + 1 : k * (0.6 + r())]);
    let walk = 0;
    return pts.map((p, i) => {
      const a = closed ? pts[(i - 1 + n) % n] : pts[Math.max(i - 1, 0)];
      const b = closed ? pts[(i + 1) % n] : pts[Math.min(i + 1, n - 1)];
      const dx = b[0] - a[0];
      const dy = b[1] - a[1];
      const len = Math.hypot(dx, dy) || 1;
      walk = walk * 0.7 + (r() - 0.5) * amp * 0.4;
      let off = walk;
      for (const [am, ph, f] of waves) off += am * Math.sin(TAU * f * (i / n) + ph);
      return [p[0] - (dy / len) * off, p[1] + (dx / len) * off];
    });
  }

  const d = (pts, closed) => 'M' + pts.map((p) => f1(p[0]) + ' ' + f1(p[1])).join('L') + (closed ? 'Z' : '');

  function ell(cx, cy, rx, ry) {
    const n = Math.max(20, Math.round((Math.PI * (rx + ry)) / 5));
    const p = [];
    for (let i = 0; i < n; i++) p.push([cx + rx * Math.cos((TAU * i) / n), cy + ry * Math.sin((TAU * i) / n)]);
    return p;
  }

  function arc(cx, cy, rx, ry, a0, a1, n) {
    const p = [];
    for (let i = 0; i <= n; i++) {
      const a = a0 + ((a1 - a0) * i) / n;
      p.push([cx + rx * Math.cos(a), cy + ry * Math.sin(a)]);
    }
    return p;
  }

  // Rounded box. r can be larger than half the height: then it is a pill.
  function rrect(x, y, w, h, r) {
    const rx = Math.min(r, w / 2);
    const ry = Math.min(r, h / 2);
    const H = Math.PI / 2;
    return dense([
      ...arc(x + w - rx, y + ry, rx, ry, -H, 0, 6),
      ...arc(x + w - rx, y + h - ry, rx, ry, 0, H, 6),
      ...arc(x + rx, y + h - ry, rx, ry, H, 2 * H, 6),
      ...arc(x + rx, y + ry, rx, ry, 2 * H, 3 * H, 6),
    ], 6, true);
  }
  const pill = (x, y, w, h) => rrect(x, y, w, h, h / 2);

  // Tab shape: flat bottom, round top.
  function tomb(x, y, w, h) {
    const rx = w / 2;
    const ry = Math.min(w / 2, h * 0.55);
    return dense([[x, y + h], ...arc(x + rx, y + ry, rx, ry, Math.PI, TAU, 14), [x + w, y + h]], 6, true);
  }

  // Wavy band, used for the submenu ribbons.
  function band(x, y, w, h, seed) {
    const r = BM.rng(seed);
    const top = [];
    const bot = [];
    const ph = r() * TAU;
    for (let i = 0; i <= 24; i++) {
      const t = i / 24;
      const lift = Math.sin(t * Math.PI * 1.6 + ph) * h * 0.18;
      top.push([x + w * t, y + h * 0.12 + lift]);
      bot.unshift([x + w * t, y + h * 0.88 + lift - Math.sin(t * Math.PI) * h * 0.1]);
    }
    return dense(top.concat(bot), 6, true);
  }

  // o: fill, stroke, sw (stroke width), seed, amp, closed (default true).
  function shape(pts, o) {
    const op = Object.assign({ fill: 'none', stroke: '#000', sw: 4, amp: 2.2, closed: true, seed: 1, extra: '' }, o);
    const p = op.amp ? wobble(op.closed ? pts : dense(pts, 6, false), op.seed, op.amp, op.closed) : pts;
    const st = op.stroke ? ` stroke="${op.stroke}" stroke-width="${op.sw}"` : '';
    return `<path d="${d(p, op.closed)}" fill="${op.fill}"${st} stroke-linejoin="round" stroke-linecap="round"${op.extra}/>`;
  }
  const line = (pts, o) => shape(pts, Object.assign({ closed: false, amp: 1.2 }, o));
  const poly = (pts, o) => shape(dense(pts, 6, true), o);
  const dot = (x, y, r, fill) => shape(ell(x, y, r, r), { fill: fill || '#000', stroke: null, amp: 0 });

  // A crude face: eyes, and a mouth that is a line or an open "D".
  function face(cx, cy, s, seed, mood) {
    let g = dot(cx - s * 0.3, cy - s * 0.1, s * 0.09) + dot(cx + s * 0.25, cy - s * 0.12, s * 0.09);
    if (mood === 'open') g += shape(arc(cx, cy + s * 0.18, s * 0.28, s * 0.22, 0, Math.PI, 8), { fill: '#C00', sw: 2.5, seed });
    else if (mood === 'sad') g += line(arc(cx, cy + s * 0.42, s * 0.22, s * 0.15, Math.PI * 1.1, Math.PI * 1.9, 6), { sw: 2.5, seed });
    else g += line(arc(cx, cy + s * 0.1, s * 0.25, s * 0.18, 0.2, Math.PI - 0.2, 6), { sw: 2.5, seed });
    return g;
  }

  function svg(w, h, body, cls) {
    return `<svg class="d ${cls || ''}" viewBox="0 0 ${w} ${h}" width="${w}" height="${h}" aria-hidden="true" focusable="false">${body}</svg>`;
  }

  // The paper sheet at a given size: a hill-shaped top that is low at the
  // sides, and two leaning sides. The bottom has no outline, because the
  // footer ground covers it. Returns the inner markup of an <svg>.
  function paper(W, H) {
    const pts = [[4, H]];
    for (let i = 0; i <= 40; i++) {
      const t = i / 40;
      pts.push([12 + t * (W - 24), 74 - 32 * Math.pow(Math.sin(Math.PI * t), 0.6)]);
    }
    pts.push([W - 4, H]);
    const p = d(wobble(dense(pts, 8, false), 'paper', 2.5, false), false);
    return `<path d="${p}Z" fill="#FFF"/><path d="${p}" fill="none" stroke="#000" stroke-width="6" stroke-linejoin="round" stroke-linecap="round"/>`;
  }

  Object.assign(BM, { dense, wobble, ell, arc, rrect, pill, tomb, band, shape, line, poly, dot, face, svg, paper });
})(typeof window !== 'undefined' ? window : globalThis);
