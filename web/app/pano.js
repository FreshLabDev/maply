/* ---------- Panorama: Pannellum, the compass tape, double tap and keys ---------- */
// The picture of a place, shared by the day's game and the free game. A screen makes one and puts pictures in it:
//
//   const pano = createPano({
//     el: $("pano"),                      // Pannellum's box
//     compass: $("compass"), tape: $("tape"), // the heading tape under it, optional
//     label: (k) => words["compass_" + k], // the tape's letters for n, ne, e, se, s, sw, w, nw
//     haptic: (kind) => {},               // optional: "light", "select"
//   });
//   pano.show({ image, kind, heading })   a picture: "panorama" turns all round, a photo pans a little; heading is
//                                         the compass direction of the picture's centre
//   pano.on("load", fn)                   the picture is on screen
//   pano.on("error", (retry) => {})       it did not come after three more tries; retry() starts again
//   pano.on("touch", fn)                  a finger, the mouse or the tape moved it: a hint can go
//   pano.steer(axis, dir)                 "yaw", "pitch" or "zoom" at -1, 0 or 1 while a key is held
//   pano.zoomBy(k) / pano.resetZoom()     closer by k (0.5 is twice as close), or back to the whole view
//   pano.faceNorth()                      turn to the north, as a tap on the tape does
//   pano.relabel()                        the words changed: the tape is drawn again
//   pano.still(on)                        the tape stops following while it is out of sight
//   pano.viewer                           the Pannellum viewer, null before the first show()
//   pano.destroy()
function createPano(o) {
  const box = o.el, tapeEl = o.compass, tapeSvg = o.tape, haptic = o.haptic || (() => {});
  const handlers = {};
  const emit = (name, ...a) => (handlers[name] || []).forEach((fn) => fn(...a));
  const esc = (s) => String(s).replace(/[&<>"']/g, (c) => "&#" + c.charCodeAt(0) + ";");
  let viewer = null, pic = null, tries = 0, again = 0, heading = 0, still = false;

  // Pannellum sets the horizontal field of view. On a portrait phone this gave ~140° vertically,
  // and tilting up/down hit the limits. We pick hfov so that the vertical span is ~95°.
  function base() {
    const w = box.clientWidth || innerWidth, h = box.clientHeight || innerHeight, v = 95 * Math.PI / 180;
    const hf = 2 * Math.atan(Math.tan(v / 2) * w / h) * 180 / Math.PI;
    return Math.max(50, Math.min(105, hf));
  }
  // A picture that did not come is asked for again a few times before the screen is told.
  function start() {
    clearTimeout(again);
    if (viewer) { try { viewer.destroy(); } catch {} viewer = null; }
    // Pannellum's own double click zooms all the way in, to minHfov, and fought the double tap below: one double click
    // went from 105° to 20°. Its keys listen only while its box has the focus; the screen's keys work everywhere.
    viewer = pannellum.viewer(box, Object.assign({
      type: "equirectangular", panorama: pic.image, autoLoad: true, showControls: false, compass: false,
      hfov: base(), minHfov: 20, maxHfov: 110, friction: 0.1, keyboardZoom: false, disableKeyboardCtrl: true, doubleClickZoom: false,
      backgroundColor: [0.10, 0.25, 0.17], pitch: 0
    }, pic.kind === "panorama" ? {} : { haov: 80, vaov: 50, maxHfov: 80 }));
    viewer.on("load", () => { tries = 0; emit("load"); });
    viewer.on("error", () => {
      if (tries < 3) { again = setTimeout(start, 2000 * 2 ** tries++); return; }
      emit("error", () => { tries = 0; start(); });
    });
  }
  function show(p) { pic = p; heading = p.heading || 0; tries = 0; start(); }
  const fit = () => viewer && viewer.setHfov(base(), 0);
  addEventListener("resize", fit);
  // A box that changes size without the window (a side panel coming in) needs Pannellum told.
  let size = "";
  new ResizeObserver(() => {
    const s = box.clientWidth + "x" + box.clientHeight;
    if (s === size) return;
    size = s;
    if (viewer) { viewer.resize(); fit(); }
  }).observe(box);

  // A double tap zooms in or back out. Only a tap counts: one finger, short and still, the second near the first. Lifting
  // two fingers after a pinch fires two pointerups too, and used to undo the pinch. A double click is two such taps.
  let lastTap = null, downs = new Map(), multi = false;
  box.addEventListener("pointerdown", (e) => {
    emit("touch");
    downs.set(e.pointerId, { x: e.clientX, y: e.clientY, t: performance.now() });
    if (downs.size > 1) { multi = true; lastTap = null; }
  });
  const up = (e) => {
    const d = downs.get(e.pointerId); downs.delete(e.pointerId);
    const wasMulti = multi; if (!downs.size) multi = false;
    if (e.type !== "pointerup" || !d || wasMulti) return;
    const now = performance.now();
    if (now - d.t > 250 || Math.hypot(e.clientX - d.x, e.clientY - d.y) > 10) { lastTap = null; return; }
    if (viewer && lastTap && now - lastTap.t < 300 && Math.hypot(e.clientX - lastTap.x, e.clientY - lastTap.y) < 30) {
      const b = base(), h = viewer.getHfov(); viewer.setHfov(h > b * 0.8 ? b * 0.5 : b, 350); haptic("light"); lastTap = null; return;
    }
    lastTap = { x: e.clientX, y: e.clientY, t: now };
  };
  box.addEventListener("pointerup", up);
  box.addEventListener("pointercancel", up);
  box.addEventListener("wheel", () => emit("touch"), { passive: true });

  // Keys move the view while they are held, slower when zoomed in, as a drag does.
  const steering = { yaw: 0, pitch: 0, zoom: 0 };
  let steerRaf = 0, steerAt = 0;
  function steerStep(now) {
    const dt = Math.min(0.05, (now - steerAt) / 1000); steerAt = now;
    if (viewer) {
      const k = viewer.getHfov() / base();
      if (steering.yaw) viewer.setYaw(viewer.getYaw() + steering.yaw * 80 * k * dt, false);
      if (steering.pitch) viewer.setPitch(viewer.getPitch() + steering.pitch * 55 * k * dt, false);
      if (steering.zoom) viewer.setHfov(viewer.getHfov() * Math.exp(-steering.zoom * 1.3 * dt), false);
    }
    steerRaf = steering.yaw || steering.pitch || steering.zoom ? requestAnimationFrame(steerStep) : 0;
  }
  function steer(axis, dir) {
    if (steering[axis] === dir) return;
    steering[axis] = dir;
    if (dir) emit("touch");
    if (dir && !steerRaf) { cancelAnimationFrame(spinRaf); steerAt = performance.now(); steerRaf = requestAnimationFrame(steerStep); }
  }
  const zoomBy = (k) => viewer && viewer.setHfov(Math.max(20, Math.min(110, viewer.getHfov() * k)), 250);
  const resetZoom = () => viewer && viewer.setHfov(base(), 350);

  // Direction by compass = frame centre direction + camera rotation. The compass card rotates the opposite way.
  // Full-width tape: about 150° is visible in the window, so the cardinal directions are readable rather than flickering.
  // Drawn over three turns (−360…720°) and shifted: crossing north does not jump.
  let TAPE_K = 1.5, TAPE_W = 0;
  function buildTape() {
    if (!tapeEl) return;
    TAPE_W = tapeEl.clientWidth || innerWidth; TAPE_K = TAPE_W / 150;
    const NAMES = {};
    ["n", "ne", "e", "se", "s", "sw", "w", "nw"].forEach((k, i) => { NAMES[i * 45] = esc(o.label ? o.label(k) : ""); });
    let svg = "";
    for (let d = -360; d <= 720; d += 5) {
      const x = ((d + 360) * TAPE_K).toFixed(1), n = ((d % 360) + 360) % 360;
      if (n === 0) svg += '<rect x="' + (x - 12) + '" y="6" width="24" height="26" fill="#FFFFFF"/><text x="' + x + '" y="26" text-anchor="middle" fill="#245A3C" style="font: 700 19px/1 var(--display)">' + NAMES[0] + '</text>';
      else if (n % 90 === 0) svg += '<text x="' + x + '" y="26" text-anchor="middle" fill="#FFFFFF" style="font: 700 19px/1 var(--display)">' + NAMES[n] + '</text>';
      else if (n % 45 === 0) svg += '<text x="' + x + '" y="24" text-anchor="middle" fill="#B5C7BD" style="font: 700 11px/1 var(--label); letter-spacing: .06em">' + NAMES[n] + '</text>';
      else if (n % 15 === 0) svg += '<path d="M' + x + ' 13v12" stroke="#59826B" stroke-width="2"/>';
      else svg += '<path d="M' + x + ' 17v4" stroke="#59826B" stroke-width="1.5"/>';
    }
    const w = (1080 * TAPE_K).toFixed(0);
    tapeSvg.setAttribute("width", w); tapeSvg.setAttribute("viewBox", "0 0 " + w + " 38"); tapeSvg.innerHTML = svg;
  }
  buildTape();
  // Telegram changes the window's height often (keyboard, fullscreen, safe area); the tape depends on the width alone.
  if (tapeEl) {
    addEventListener("resize", () => { if ((tapeEl.clientWidth || innerWidth) !== TAPE_W) buildTape(); });
    new ResizeObserver(() => { if (tapeEl.clientWidth && tapeEl.clientWidth !== TAPE_W) buildTape(); }).observe(tapeEl);
  }
  // The tape follows the panorama; out of sight it stands still.
  let tapeAt = "", alive = true;
  (function spin() {
    if (!alive) return;
    if (tapeEl && !still) {
      const h = ((((viewer ? viewer.getYaw() : 0) + heading) % 360) + 360) % 360;
      const at = "translateX(" + (TAPE_W / 2 - (h + 360) * TAPE_K) + "px)";
      if (at !== tapeAt) { tapeSvg.style.transform = at; tapeAt = at; }
    }
    requestAnimationFrame(spin);
  })();
  // The tape can be dragged with a finger or mouse; the panorama rotates along, with inertia after release.
  // A short tap without movement turns the view to north. The wheel over the tape also rotates.
  let tdrag = null, spinV = 0, spinRaf = 0;
  function faceNorth() {
    if (!viewer) return;
    const north = ((-heading + 540) % 360) - 180; // yaw at which we face north, within −180…180
    viewer.lookAt(viewer.getPitch(), north, viewer.getHfov(), 700); haptic("select");
  }
  function coast() {
    // Inertia: velocity in degrees per frame decays, like the panorama's own.
    if (Math.abs(spinV) < 0.05) return;
    viewer.setYaw(viewer.getYaw() + spinV, false); spinV *= 0.92;
    spinRaf = requestAnimationFrame(coast);
  }
  if (tapeEl) {
    tapeEl.addEventListener("pointerdown", (e) => {
      if (!viewer) return;
      cancelAnimationFrame(spinRaf); spinV = 0;
      tdrag = { x: e.clientX, last: e.clientX, t: performance.now(), moved: false };
      tapeEl.setPointerCapture(e.pointerId); emit("touch");
    });
    tapeEl.addEventListener("pointermove", (e) => {
      if (!tdrag) return;
      const dx = e.clientX - tdrag.last, now = performance.now(), dt = Math.max(1, now - tdrag.t);
      if (Math.abs(e.clientX - tdrag.x) > 4) tdrag.moved = true;
      // Dragging the tape right moves it right, so the heading decreases.
      const dYaw = -dx / TAPE_K;
      viewer.setYaw(viewer.getYaw() + dYaw, false);
      spinV = 0.7 * (dYaw / dt * 16) + 0.3 * spinV;
      tdrag.last = e.clientX; tdrag.t = now;
    });
    const tapeUp = () => {
      if (!tdrag) return;
      const moved = tdrag.moved, idle = performance.now() - tdrag.t; tdrag = null;
      if (!moved) { spinV = 0; faceNorth(); return; }
      if (idle < 80) coast(); // finger stopped before release: no inertia
    };
    tapeEl.addEventListener("pointerup", tapeUp);
    // Enter or Space on the compass, which has no pointer: face north, as a tap does.
    tapeEl.addEventListener("click", (e) => { if (e.detail === 0 && viewer) faceNorth(); });
    tapeEl.addEventListener("pointercancel", tapeUp);
    tapeEl.addEventListener("wheel", (e) => {
      e.preventDefault(); cancelAnimationFrame(spinRaf);
      if (viewer) viewer.setYaw(viewer.getYaw() + (Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY) / TAPE_K * 0.6, false);
    }, { passive: false });
  }

  return {
    show, steer, zoomBy, resetZoom, faceNorth,
    relabel: buildTape,
    still(on) { still = !!on; },
    on(name, fn) { (handlers[name] = handlers[name] || []).push(fn); },
    get viewer() { return viewer; },
    destroy() {
      alive = false; clearTimeout(again); cancelAnimationFrame(steerRaf); cancelAnimationFrame(spinRaf);
      removeEventListener("resize", fit);
      if (viewer) { try { viewer.destroy(); } catch {} viewer = null; }
    },
  };
}
