/* ---------- Loading screen: the halftone globe ---------- */
// The globe of maply's avatar, turning on its tilted axis while the picture loads. Dots lie in rows along the
// parallels (land.js says which are land); each is a small disc on the sphere, so towards the limb it flattens into
// an ellipse, and its size is the light it gets from the upper left. The pin hops; where it lands, the dots ripple.
(() => {
  const cv = document.getElementById("loadglobe"), box = document.getElementById("loader");
  if (!cv || typeof LAND === "undefined") return;
  const ctx = cv.getContext("2d");
  const FIELD = "#245A3C", SEA = "#59826B", WHITE = "#FFFFFF";
  const TILT = 23.4 * Math.PI / 180, VIEW_LAT = 24 * Math.PI / 180;
  const LIGHT = (() => { const v = [-0.62, -0.58, 0.53], l = Math.hypot(...v); return v.map((x) => x / l); })();
  const PIN = new Path2D("M320 501 L214 395 A150 150 0 1 1 426 395 Z M320 229 A60 60 0 1 0 320 349 A60 60 0 1 0 320 229 Z");
  const still = matchMedia("(prefers-reduced-motion: reduce)").matches;

  // Unit vectors of every dot, and whether it is land.
  const raw = atob(LAND.bits), dots = [];
  let n = 0;
  LAND.rows.forEach((count, i) => {
    const lat = (-90 + LAND.step / 2 + i * LAND.step) * Math.PI / 180;
    for (let j = 0; j < count; j++, n++) {
      const lon = (-180 + (j + 0.5 + (i % 2) * 0.5) * 360 / count) * Math.PI / 180;
      dots.push({ lat, lon, land: (raw.charCodeAt(n >> 3) >> (n & 7)) & 1 });
    }
  });
  const spacing = LAND.step * Math.PI / 180; // between dots, in radians of the sphere

  let size = 0, dpr = 1;
  function fit() {
    dpr = Math.min(3, devicePixelRatio || 1);
    size = cv.clientWidth;
    cv.width = cv.height = Math.round(size * dpr);
  }
  fit();
  addEventListener("resize", () => { fit(); if (still) requestAnimationFrame(frame); });

  const smooth = (a, b, x) => { const t = Math.min(1, Math.max(0, (x - a) / (b - a))); return t * t * (3 - 2 * t); };
  const HOP = 1100; // ms per hop of the pin
  const t0 = performance.now();

  function frame(now) {
    if (box.classList.contains("gone") && getComputedStyle(box).visibility === "hidden") return;
    const t = still ? 0 : now - t0;
    const W = size * dpr, R = W * 0.36, cx = W / 2, cy = W * 0.56;
    ctx.clearRect(0, 0, W, W);

    // The pin stands on the globe a little above the centre; it rises and falls back every HOP.
    const phase = (t % HOP) / HOP, rise = still ? 0 : Math.sin(Math.PI * Math.min(1, phase / 0.62)) ** 1.6 * (phase < 0.62 ? 1 : 0);
    const since = still ? 1e9 : (phase >= 0.62 ? (phase - 0.62) * HOP : (phase + 0.38) * HOP); // ms since it landed
    const tipX = cx, tipY = cy - R * 0.32;

    // The Earth turns west to east, a full turn in 24 s.
    const spin = -0.6 + t / 24000 * 2 * Math.PI;
    const ct = Math.cos(TILT), st = Math.sin(TILT), cv0 = Math.cos(VIEW_LAT), sv0 = Math.sin(VIEW_LAT);
    const land = new Path2D(), sea = new Path2D();
    for (const d of dots) {
      const cl = Math.cos(d.lat), lon = d.lon + spin;
      let x = cl * Math.cos(lon), y = cl * Math.sin(lon), z = Math.sin(d.lat);
      // Tip the north pole towards the viewer by the view latitude: x is depth.
      const x1 = x * cv0 + z * sv0, z1 = -x * sv0 + z * cv0;
      if (x1 < 0.02) continue;
      // Lean the axis to the right by the Earth's tilt.
      const sx = y * ct + z1 * st, sy = -(-y * st + z1 * ct);
      const nz = x1;
      const lit = smooth(-0.15, 0.95, sx * LIGHT[0] + sy * LIGHT[1] + nz * LIGHT[2]);
      const px = cx + sx * R, py = cy + sy * R;
      // The ripple: a ring that runs out from where the pin landed and fades.
      const dist = Math.hypot(px - tipX, py - tipY) / R, ring = since / 650;
      const wave = still ? 0 : Math.max(0, 1 - Math.abs(dist - ring) / 0.09) * Math.max(0, 1 - ring);
      const k = d.land ? 0.26 + 0.68 * lit + 0.55 * wave : 0.12 + 0.3 * lit + 0.35 * wave;
      const rad = k * spacing * R / 2;
      if (rad < 0.4) continue;
      const p = d.land || wave > 0.5 ? land : sea;
      p.moveTo(px + rad, py);
      p.ellipse(px, py, Math.max(0.4, rad * nz), rad, Math.atan2(sy, sx), 0, 2 * Math.PI);
    }
    ctx.fillStyle = SEA; ctx.fill(sea);
    ctx.fillStyle = WHITE; ctx.fill(land);

    // The pin, its hole in the field colour so the dots do not show through it.
    const h = R * 0.62, k = h / 362, y = tipY - rise * R * 0.22;
    ctx.save();
    ctx.translate(tipX, y); ctx.scale(k, k); ctx.translate(-320, -501);
    ctx.fillStyle = FIELD;
    ctx.beginPath(); ctx.arc(320, 289, 64, 0, 2 * Math.PI); ctx.fill();
    ctx.lineWidth = 34; ctx.lineJoin = "round"; ctx.strokeStyle = FIELD; ctx.stroke(PIN);
    ctx.fillStyle = WHITE; ctx.fill(PIN, "evenodd");
    ctx.restore();

    if (!still) requestAnimationFrame(frame);
  }
  requestAnimationFrame(frame);
})();
