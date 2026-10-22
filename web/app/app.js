const ROUND = window.MAPLY_ROUND || { n: 142, img: "../prototype/sample.jpg", pano: true, lat: 38.7139, lon: -9.1389, cc: "pt", city: "Лиссабон", place: "Лиссабон, Португалия · площадь Россиу",
  fact: "Волнистая мостовая этой площади стала образцом для набережной Копакабаны в Рио.", author: "southglos", revealHour: 20,
  heading: 164.1, tz: "Europe/Moscow", tzName: "по москве" }; // heading: where the frame centre points (computed_compass_angle from Mapillary), 0 = north
const $ = (id) => document.getElementById(id);
const PIN_PATH = "M320 501 L214 395 A150 150 0 1 1 426 395 Z M320 229 A60 60 0 1 0 320 349 A60 60 0 1 0 320 229 Z";
// On the map, markers are dark with a white outline: visible on both light land and green water.
const PIN = '<svg class="pin" viewBox="150 119 340 402"><path fill="#1A412B" stroke="#FFFFFF" stroke-width="24" stroke-linejoin="miter" paint-order="stroke" fill-rule="evenodd" d="' + PIN_PATH + '"/></svg>';
const TARGET = '<svg class="target" viewBox="0 0 30 30"><path fill="none" stroke="#FFFFFF" stroke-width="6" d="M15 3.5a11.5 11.5 0 1 1 0 23a11.5 11.5 0 1 1 0-23"/><circle cx="15" cy="15" r="7.5" fill="#FFFFFF"/><circle cx="15" cy="15" r="5" fill="#1A412B"/><path fill="none" stroke="#1A412B" stroke-width="3" d="M15 3.5a11.5 11.5 0 1 1 0 23a11.5 11.5 0 1 1 0-23"/></svg>';
const pick = (a) => a[Math.floor(Math.random() * a.length)];
const av = (who, me) => '<i class="av' + (me ? ' me' : '') + '">' + (me ? "ты" : who[0]) + '</i>';
const el = (html) => { const d = document.createElement("div"); d.innerHTML = html; return d.firstElementChild; };
// Thousands separated by a narrow space, as in the Asterfield system.
const fmt = (n) => String(Math.round(n)).replace(/\B(?=(\d{3})+(?!\d))/g, " ");

/* ---------- Touches of character ---------- */
const LOADING = ["Завязываем глаза…", "Крутим глобус…", "Телепортируем…", "Прячем табличку с названием…", "Отключаем GPS…"];
const ON_WATER = ["Это вода. Рыбы оценят.", "Смело. Прямо в море.", "Капитан, мы точно не на суше?"];
const ON_LAND = ["Записали. Отступать некуда.", "Чуйка говорит — тут.", "Смело.", "Хм. Интересный выбор.", "Окей, Колумб."];
const MOVED = ["Передумал? Бывает.", "Ещё разок прицелимся.", "Сомнения — признак ума."];
const LOGO = ["Логотип не подсказывает. Мы проверяли.", "Нет, жать сюда бесполезно.", "Ладно, вот подсказка: это на Земле."];
let toastT = 0;
function toast(text) { const t = $("toast"); t.textContent = text; t.classList.add("on"); clearTimeout(toastT); toastT = setTimeout(() => t.classList.remove("on"), 2300); }

let li = 0;
const loadTimer = setInterval(() => { $("loadtext").textContent = LOADING[++li % LOADING.length]; }, 900);
let logoTaps = 0;
$("logo").addEventListener("click", () => { if (++logoTaps % 3 === 0) toast(pick(LOGO)); });

/* ---------- Countdown to the result, by the language capital's time ---------- */
const AT = ROUND.revealHour + ":00";
function secondsLeft() {
  const p = Object.fromEntries(new Intl.DateTimeFormat("en-GB", { timeZone: ROUND.tz, hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false })
    .formatToParts(new Date()).map((x) => [x.type, x.value]));
  const s = ROUND.revealHour * 3600 - (+p.hour * 3600 + +p.minute * 60 + +p.second);
  return s < 0 ? s + 86400 : s;
}
const hm = (s) => Math.floor(s / 3600) + ":" + String(Math.floor(s % 3600 / 60)).padStart(2, "0");
function tick() { $("left").textContent = hm(secondsLeft()); const w = $("wleft"); if (w) w.textContent = hm(secondsLeft()); }
tick(); setInterval(tick, 15000);

/* ---------- Panorama: Pannellum, inertia, pinch, double tap ---------- */
$("credits").innerHTML = '<span>© ' + ROUND.author + ', <a href="https://www.mapillary.com" target="_blank" rel="noopener">Mapillary</a> CC BY-SA ·</span>' +
  '<span>© <a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noopener">OpenStreetMap</a>, OpenMapTiles, <a href="https://openfreemap.org" target="_blank" rel="noopener">OpenFreeMap</a></span>';
// Pannellum sets the horizontal field of view. On a portrait phone this gave ~140° vertically,
// and tilting up/down hit the limits. We pick hfov so that the vertical span is ~95°.
function baseHfov() {
  const w = $("pano").clientWidth || innerWidth, h = $("pano").clientHeight || innerHeight, v = 95 * Math.PI / 180;
  const hf = 2 * Math.atan(Math.tan(v / 2) * w / h) * 180 / Math.PI;
  return Math.max(50, Math.min(105, hf));
}
const viewer = pannellum.viewer("pano", Object.assign({
  type: "equirectangular", panorama: ROUND.img, autoLoad: true, showControls: false, compass: false,
  hfov: baseHfov(), minHfov: 20, maxHfov: 110, friction: 0.1, keyboardZoom: false, disableKeyboardCtrl: true,
  backgroundColor: [0.10, 0.25, 0.17], pitch: 0
}, ROUND.pano ? {} : { haov: 80, vaov: 50, maxHfov: 80 }));
addEventListener("resize", () => viewer.setHfov(baseHfov(), 0));
viewer.on("load", () => { clearInterval(loadTimer); $("loader").classList.add("gone"); });
viewer.on("error", () => { clearInterval(loadTimer); $("loadtext").textContent = "Кадр не загрузился. Даже мы не знаем, где это."; });
let lastTap = 0;
$("pano").addEventListener("pointerdown", () => $("hint").classList.add("gone"));
$("pano").addEventListener("pointerup", () => {
  const now = Date.now();
  if (now - lastTap < 280) { const b = baseHfov(), h = viewer.getHfov(); viewer.setHfov(h > b * 0.8 ? b * 0.5 : b, 350); haptic("light"); }
  lastTap = now;
});
// Direction by compass = frame centre direction + camera rotation. The compass card rotates the opposite way.
const HEADING = ROUND.heading || 0;
// Full-width tape: about 150° is visible in the window, so the cardinal directions are readable rather than flickering.
// Drawn over three turns (−360…720°) and shifted: crossing north does not jump.
let TAPE_K = 1.5, TAPE_W = 0;
function buildTape() {
  TAPE_W = $("compass").clientWidth || innerWidth; TAPE_K = TAPE_W / 150;
  const NAMES = { 0: "С", 45: "СВ", 90: "В", 135: "ЮВ", 180: "Ю", 225: "ЮЗ", 270: "З", 315: "СЗ" };
  let svg = "";
  for (let d = -360; d <= 720; d += 5) {
    const x = ((d + 360) * TAPE_K).toFixed(1), n = ((d % 360) + 360) % 360;
    if (n === 0) svg += '<rect x="' + (x - 12) + '" y="6" width="24" height="26" fill="#FFFFFF"/><text x="' + x + '" y="26" text-anchor="middle" fill="#245A3C" style="font: 700 19px/1 var(--display)">С</text>';
    else if (n % 90 === 0) svg += '<text x="' + x + '" y="26" text-anchor="middle" fill="#FFFFFF" style="font: 700 19px/1 var(--display)">' + NAMES[n] + '</text>';
    else if (n % 45 === 0) svg += '<text x="' + x + '" y="24" text-anchor="middle" fill="#B5C7BD" style="font: 700 11px/1 var(--label); letter-spacing: .06em">' + NAMES[n] + '</text>';
    else if (n % 15 === 0) svg += '<path d="M' + x + ' 13v12" stroke="#59826B" stroke-width="2"/>';
    else svg += '<path d="M' + x + ' 17v4" stroke="#59826B" stroke-width="1.5"/>';
  }
  const t = $("tape"), w = (1080 * TAPE_K).toFixed(0);
  t.setAttribute("width", w); t.setAttribute("viewBox", "0 0 " + w + " 38"); t.innerHTML = svg;
}
buildTape();
addEventListener("resize", buildTape);
(function spin() {
  const h = (((viewer.getYaw() + HEADING) % 360) + 360) % 360;
  $("tape").style.transform = "translateX(" + (TAPE_W / 2 - (h + 360) * TAPE_K) + "px)";
  requestAnimationFrame(spin);
})();
// The tape can be dragged with a finger or mouse; the panorama rotates along, with inertia after release.
// A short tap without movement turns the view to north. The wheel over the tape also rotates.
const tapeEl = $("compass");
let tdrag = null, spinV = 0, spinRaf = 0;
function faceNorth() {
  const north = ((-HEADING + 540) % 360) - 180; // yaw at which we face north, within −180…180
  viewer.lookAt(viewer.getPitch(), north, viewer.getHfov(), 700); haptic("select");
}
function coast() {
  // Inertia: velocity in degrees per frame decays, like the panorama's own.
  if (Math.abs(spinV) < 0.05) return;
  viewer.setYaw(viewer.getYaw() + spinV, false); spinV *= 0.92;
  spinRaf = requestAnimationFrame(coast);
}
tapeEl.addEventListener("pointerdown", (e) => {
  cancelAnimationFrame(spinRaf); spinV = 0;
  tdrag = { x: e.clientX, last: e.clientX, t: performance.now(), moved: false };
  tapeEl.setPointerCapture(e.pointerId); $("hint").classList.add("gone");
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
function tapeUp() {
  if (!tdrag) return;
  const moved = tdrag.moved, idle = performance.now() - tdrag.t; tdrag = null;
  if (!moved) { spinV = 0; faceNorth(); return; }
  if (idle < 80) coast(); // finger stopped before release: no inertia
}
tapeEl.addEventListener("pointerup", tapeUp);
tapeEl.addEventListener("pointercancel", tapeUp);
tapeEl.addEventListener("wheel", (e) => {
  e.preventDefault(); cancelAnimationFrame(spinRaf);
  viewer.setYaw(viewer.getYaw() + (Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY) / TAPE_K * 0.6, false);
}, { passive: false });

/* ---------- Two views: photo with a mini globe, and the big map ---------- */
let state = "guess", view = "photo";
let map = null, mapReady = false; // declared up front: otherwise window.map is the div#map
const MINI = 136, ACT = 82, TAPE_H = 38; // TAPE_H is the height of the compass tape at the bottom (--tape)
// Globe radius on screen: the equator is 512·2^zoom px long, which gives the zoom for a target diameter.
const zoomFor = (d) => Math.log2(d * Math.PI / 512);
const globeR = (zm) => 512 * 2 ** zm / (2 * Math.PI);
function onGlobe(pt) {
  const T = map.transform;
  if (typeof T.isPointOnMapSurface === "function") return T.isPointOnMapSurface(pt);
  const c = map.project(map.getCenter()); // fallback in case MapLibre's internal method disappears
  return map.getZoom() >= GLOBE_Z[1] || Math.hypot(pt.x - c.x, pt.y - c.y) <= globeR(map.getZoom()) * 0.88;
}
function box() { const m = $("map"); return { W: m.clientWidth, H: m.clientHeight }; }
function miniCam() {
  // Globe centre in the bottom-right corner: shift the focus point with padding; the canvas stays fullscreen.
  const { W, H } = box(), cx = W - 12 - MINI / 2, cy = H - TAPE_H - 16 - MINI / 2;
  // MapLibre rejects negative padding and fails entirely; this happens while the canvas is still zero-sized.
  return { padding: { left: Math.max(0, 2 * cx - W), top: Math.max(0, 2 * cy - H), right: 0, bottom: 0 }, zoom: zoomFor(MINI - 4) };
}
// How much space the plates take at top and bottom: the camera keeps the globe and markers in the free part.
function reserve() {
  const top = $("map").getBoundingClientRect().top, on = (id) => $(id).classList.contains("on");
  // On the result there is also the day's table at the top. We measure by the header, not the table itself: at that moment it is still expanding.
  const board = document.body.classList.contains("is-reveal") ? $("prog").parentElement.getBoundingClientRect().bottom + $("prog").offsetHeight - top + 8 : 0;
  const t = Math.max(board, on("ovTop") ? $("ovTop").getBoundingClientRect().bottom - top + 8 : 0);
  const b = (on("ovBot") ? $("ovBot").offsetHeight + 20 : 0) + ACT; // the bottom strip is always present in map mode
  return { t, b };
}
function bigCam() {
  const { W, H } = box(), { t, b } = reserve();
  return { padding: { left: 0, top: t, right: 0, bottom: b }, zoom: zoomFor(Math.max(MINI, 0.86 * Math.min(W, H - t - b))) };
}
function overlay(top, bot) {
  $("ovTop").innerHTML = top; $("ovBot").innerHTML = bot;
  $("ovTop").classList.toggle("on", !!top); $("ovBot").classList.toggle("on", !!bot);
}
function toMap() {
  if (view === "map") return;
  view = "map"; document.body.classList.add("is-map"); haptic("light"); paintBackdrop(); cancelAnimationFrame(tween); syncBack();
  const c = bigCam();
  map.easeTo({ ...c, zoom: guess ? Math.max(map.getZoom(), 2.2) : c.zoom, center: guess || map.getCenter(), duration: 750, easing: easeOut });
}
function toPhoto() {
  if (view === "photo") return;
  view = "photo"; document.body.classList.remove("is-map"); haptic("soft"); paintBackdrop(); syncBack();
  glideToMini(650);
}
// Not easeTo: if zooming out starts from a flat map (zoom > 3.6), MapLibre clamps the final zoom right at the start
// to "the world is no narrower than the screen", log2(H/512), and the globe gets stuck large. A frame-by-frame jumpTo does not know this:
// by the time zoom drops below 3.6 the projection is already a globe, and there is no constraint.
let tween = 0;
function glideToMini(ms) {
  cancelAnimationFrame(tween); map.stop();
  const z0 = map.getZoom(), c0 = map.getCenter(), p0 = map.getPadding(), t0 = performance.now();
  const c1 = guess || c0, dl = ((c1.lng - c0.lng + 540) % 360) - 180, mix = (a, b, e) => a + (b - a) * e;
  const step = (now) => {
    if (view !== "photo") return;
    const k = Math.min(1, (now - t0) / ms), e = easeOut(k), cam = miniCam(), p = cam.padding;
    map.jumpTo({ zoom: mix(z0, cam.zoom, e), center: [c0.lng + dl * e, mix(c0.lat, c1.lat, e)],
      padding: { left: mix(p0.left, p.left, e), top: mix(p0.top, p.top, e), right: mix(p0.right, p.right, e), bottom: mix(p0.bottom, p.bottom, e) } });
    // The last frame may have landed on the flat projection, so we finish with a couple more frames.
    if (k < 1 || (Math.abs(map.getZoom() - cam.zoom) > .01 && now - t0 < ms + 500)) tween = requestAnimationFrame(step);
  };
  tween = requestAnimationFrame(step);
}
const easeOut = (t) => 1 - (1 - t) ** 3;
$("globeHit").addEventListener("click", toMap);
$("look").addEventListener("click", toPhoto);
if (tgAt("6.1")) tg.BackButton.onClick(toPhoto);
// Splits the bottom strip into two buttons or merges it back into one.
// The left button label depends on the stage: before the answer it invites a closer look, after the answer simply to enjoy.
const LOOK = { guess: ["Ещё разглядеть", "Разглядеть"], wait: ["Полюбоваться", "Полюбоваться"], reveal: ["Погулять там", "Погулять там"] };
function split(on) { $("act").classList.toggle("on", on); $("look").textContent = LOOK[state][on ? 1 : 0]; }
addEventListener("resize", () => { if (mapReady) map.jumpTo(view === "map" ? { padding: bigCam().padding } : miniCam()); });
function setCall(b, sub) { $("callB").textContent = b; $("callS").innerHTML = sub + " <em>→</em>"; }

map = createMap(miniCam().zoom);
map.setPadding(miniCam().padding);
map.on("load", () => { mapReady = true; map.jumpTo(miniCam()); });
// While there is no pin, the mini globe rotates slowly: it shows it is alive and can be touched.
(function spinGlobe() {
  if (mapReady && view === "photo" && !guess && !map.isMoving()) { const c = map.getCenter(); map.setCenter([c.lng + 0.12, c.lat]); }
  requestAnimationFrame(spinGlobe);
})();

let guess = null, marker = null;
function putPin(lngLat, point) {
  const first = !guess; guess = lngLat;
  if (!marker) {
    marker = new maplibregl.Marker({ element: el(PIN), anchor: "bottom", draggable: true }).setLngLat(guess).addTo(map);
    marker.on("dragend", () => { guess = marker.getLngLat(); haptic("select"); toast(pick(MOVED)); });
  } else {
    marker.setLngLat(guess);
    const p = marker.getElement(); p.style.animation = "none"; void p.offsetWidth; p.style.animation = "";
  }
  const wet = map.queryRenderedFeatures(point, { layers: ["water"] }).length > 0;
  toast(pick(wet ? ON_WATER : first ? ON_LAND : MOVED));
  haptic("medium");
  // The pin is placed but the answer is not sent: Telegram will ask for confirmation before closing the app.
  if (first) { split(true); setCall("Булавка стоит", "ответить"); if (tgAt("6.2")) tg.enableClosingConfirmation(); }
}
map.on("click", (e) => {
  // A tap beside the globe, in the void around it, returns to the photo. We check the surface itself via MapLibre:
  // for a point off the globe it still returns a coordinate on its edge, and previously a pin was placed there.
  if (!onGlobe(e.point)) { toPhoto(); return; }
  if (state === "guess") putPin(e.lngLat, e.point);
});

/* ---------- Waiting and result ---------- */
function km(a, b) {
  const R = 6371, r = Math.PI / 180, dLat = (b.lat - a.lat) * r, dLon = (b.lng - a.lng) * r;
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(a.lat * r) * Math.cos(b.lat * r) * Math.sin(dLon / 2) ** 2;
  return 2 * R * Math.asin(Math.sqrt(h));
}
// Score: up to 3,500 for distance plus bonuses for guessing the continent, country and city. The maximum is the same: 5,000.
const BONUS = [300, 500, 700];
const base = (d) => 3500 * Math.exp(-d / 1492.7);
const score = (d, lvl) => base(d) + BONUS.slice(0, lvl).reduce((a, b) => a + b, 0);
function toWaiting() {
  state = "wait"; marker.setDraggable(false); haptic("success");
  if (tgAt("6.2")) tg.disableClosingConfirmation();
  split(false); setCall("Ответ принят", "итог в " + AT);
  $("timer").classList.add("hidden");
  overlay(
    '<div class="pl nums"><b id="wleft">' + hm(secondsLeft()) + '</b><span class="up">до ответа<br>в ' + AT + ' ' + ROUND.tzName + '</span></div>',
    '<div class="chips up"><span class="me">ты ✓</span><span>маша ✓</span><span>лёша ✓</span><span class="no">артём думает…</span></div>' +
    '<button class="demo" id="demo">демо: показать итог ' + AT + '</button>');
  $("demo").onclick = toReveal;
  // Zoom past the edge of the globe unfolding: at 3.5 the map is still slightly curved, and a pinch immediately snapped it back to a globe.
  map.easeTo({ padding: bigCam().padding, center: guess, zoom: GLOBE_Z[1] + 0.4, duration: 700, easing: easeOut });
}
// Result markers are laid out in pixels on every map-motion frame. A friend's point that falls under the target
// moves out onto a ring around it; nearby points are pushed apart; a label takes the first free spot, and if it
// ends up far away, a leader line is drawn to the point.
let marks = null, lvlMe = 0;
const RING = 27, GAP = 23;
function layoutMarks() {
  if (!marks) return;
  const T = map.project(marks.truth), G = map.project(guess), c = map.getContainer(), W = c.clientWidth;
  const rect = (x, y, w, h) => ({ x, y, w, h });
  const hit = (a, b) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;
  const pts = marks.friends.map((f) => { const p = map.project(f.at); return { f, p, d: { x: p.x, y: p.y }, w: f.b.offsetWidth, h: f.b.offsetHeight }; });
  for (let k = 0; k < 6; k++) {
    for (let i = 0; i < pts.length; i++) for (let j = i + 1; j < pts.length; j++) {
      const a = pts[i].d, b = pts[j].d; let dx = b.x - a.x, dy = b.y - a.y, r = Math.hypot(dx, dy);
      if (r >= GAP) continue;
      if (r < .5) { dx = 1; dy = 0; r = 1; }
      const m = (GAP - r) / 2; a.x -= dx / r * m; a.y -= dy / r * m; b.x += dx / r * m; b.y += dy / r * m;
    }
    pts.forEach((a, i) => {
      let dx = a.d.x - T.x, dy = a.d.y - T.y, r = Math.hypot(dx, dy);
      if (r >= RING) return;
      if (r < .5) { const ang = -Math.PI / 4 + i * 1.3; dx = Math.cos(ang); dy = Math.sin(ang); r = 1; }
      a.d.x = T.x + dx / r * RING; a.d.y = T.y + dy / r * RING;
    });
  }
  const taken = [rect(T.x - 17, T.y - 17, 34, 34), rect(G.x - 20, G.y - 46, 40, 46), ...pts.map((a) => rect(a.d.x - 11, a.d.y - 11, 22, 22))];
  pts.forEach((a) => {
    const { w, h } = a;
    // First flush on the right, then left, below, above; then the diagonals with a leader line.
    const slots = [[14, -h / 2], [-14 - w, -h / 2], [-w / 2, 14], [-w / 2, -14 - h],
      [22, -h - 16], [-22 - w, -h - 16], [22, 16], [-22 - w, 16], [34, -h - 30], [-34 - w, -h - 30], [34, 30], [-34 - w, 30]];
    let n = slots.findIndex(([x, y]) => { const r = rect(a.d.x + x, a.d.y + y, w, h); return r.x >= 4 && r.x + w <= W - 4 && !taken.some((t) => hit(r, t)); });
    if (n < 0) n = 0;
    const [x, y] = slots[n];
    taken.push(rect(a.d.x + x, a.d.y + y, w, h));
    a.f.g.style.transform = "translate(" + (a.d.x - a.p.x) + "px," + (a.d.y - a.p.y) + "px)";
    a.f.b.style.transform = "translate(" + x + "px," + y + "px)";
    const lx = Math.max(x, Math.min(0, x + w)), ly = Math.max(y, Math.min(0, y + h)), len = n < 4 ? 0 : Math.hypot(lx, ly);
    a.f.s.style.width = len + "px";
    a.f.s.style.transform = "rotate(" + Math.atan2(ly, lx) + "rad)";
  });
}
map.on("move", layoutMarks);
map.on("resize", layoutMarks);
// Result table under the header. Delays are shared by bars and counters: first the kilometres, then the bonuses one by one,
// then the dashed missed part.
const T_KM = .25, T_B = (k) => .75 + k * .3, ROWS = 4;
const pct = (x) => (x / 50).toFixed(2) + "%";
function paintBoard(rows, d, lvl) {
  // Collapsed, the first rows and yours are visible; if you are lower, there is one fewer row so the height does not change.
  const meAt = rows.findIndex((r) => r.me), top = meAt >= ROWS ? ROWS - 1 : ROWS, IF = ["континент", "страну", "город"];
  $("lb").innerHTML = rows.map((r, i) => {
    const b0 = Math.round(base(r.d)), segs = ['<i class="km" data-t="' + fmt(b0) + ' за ' + fmt(r.d) + ' км" style="width:' + pct(b0) + ';--d:' + T_KM + 's"></i>'];
    BONUS.forEach((x, k) => {
      if (k < r.lvl) segs.push('<i data-t="+' + x + ' за ' + IF[k] + '" style="width:' + pct(x) + ';--d:' + T_B(k) + 's"></i>');
      else if (r.me) segs.push('<i class="if" data-t="+' + x + ' за ' + IF[k] + ' — мимо" style="width:' + pct(x) + ';--d:' + (T_B(r.lvl) + .1) + 's"></i>');
    });
    const x = i >= top && !r.me ? " x" : "";
    return '<div class="r' + (r.me ? " me" : "") + x + '"><em>' + (i + 1) + '</em>' + av(r.who, r.me) + '<span>' + r.who + '</span>' +
      '<span class="hit" data-rest="до 5 000 не хватило ' + fmt(5000 - score(r.d, r.lvl)) + '"><span class="bar">' + segs.join("") + '</span></span>' +
      '<b class="s" data-b="' + b0 + '" data-l="' + r.lvl + '">0</b><span class="k">' + fmt(r.d) + ' км</span></div>';
  }).join("");
  const hidden = rows.length - Math.min(rows.length, top + (meAt >= top ? 1 : 0));
  $("lb").classList.remove("open");
  $("more").classList.toggle("hidden", hidden <= 0);
  $("more").dataset.n = hidden;
  $("more").textContent = "ещё " + hidden + " ↓";
  // Legend and "what if" at the same distance: one line, wraps in whole chunks.
  const t = (h) => '<span>' + h + '</span>';
  $("ifs").innerHTML = t('<i class="km"></i>за км') + ' ' + t('<i></i>бонусы') + ' · ' + (lvl >= 3 ? t("все бонусы твои") :
    t('<i class="if"></i>угадал бы ' + IF[lvl] + ' — <b>' + fmt(score(d, lvl + 1)) + '</b>') +
    IF.slice(lvl + 1).map((n, j) => ', ' + t(n + ' — <b>' + fmt(score(d, lvl + j + 2)) + '</b>')).join(""));
}
// Full leaderboard: a tap on the table or on "ещё N" expands and collapses it.
function toggleBoard() {
  if ($("more").classList.contains("hidden")) return;
  const open = $("lb").classList.toggle("open");
  $("more").textContent = open ? "свернуть ↑" : "ещё " + $("more").dataset.n + " ↓";
}
$("more").addEventListener("click", toggleBoard);
// The plates over the map stay under the table and move down as it expands.
const ovUnderBoard = () => { $("ovTop").style.top = $("prog").getBoundingClientRect().top + $("prog").offsetHeight + 10 + "px"; };
new ResizeObserver(() => { if (state === "reveal") ovUnderBoard(); }).observe($("prog"));
$("lb").addEventListener("click", (e) => { if (!e.target.closest(".hit")) toggleBoard(); });
// Tooltip above the bar: which segment is under the finger or cursor. We look the segment up by x so it is easy to hit.
function showTip(e) {
  const hit = e.target.closest(".hit");
  if (!hit) return hideTip();
  const segs = [...hit.querySelectorAll("i")], x = e.clientX;
  const seg = segs.find((g) => { const r = g.getBoundingClientRect(); return x >= r.left - 1 && x <= r.right + 1; });
  segs.forEach((g) => g.classList.toggle("lit", g === seg));
  const tip = $("tip"), pr = $("prog").getBoundingClientRect(), rr = (seg || hit).getBoundingClientRect();
  tip.textContent = seg ? seg.dataset.t : hit.dataset.rest;
  const cx = seg ? (rr.left + rr.right) / 2 : x, w = tip.offsetWidth;
  tip.style.left = Math.max(4, Math.min(pr.width - w - 4, cx - pr.left - w / 2)) + "px";
  tip.style.top = hit.getBoundingClientRect().top - pr.top - tip.offsetHeight + 2 + "px";
  tip.classList.add("on");
}
function hideTip() { $("tip").classList.remove("on"); document.querySelectorAll(".lb .lit").forEach((g) => g.classList.remove("lit")); }
$("lb").addEventListener("pointermove", showTip);
$("lb").addEventListener("pointerdown", showTip);
$("lb").addEventListener("pointerleave", (e) => { if (e.pointerType === "mouse") hideTip(); });
document.addEventListener("pointerdown", (e) => { if (!e.target.closest(".lb")) hideTip(); });
// Points in the rows count up in time with the bars.
function countUp() {
  const t0 = performance.now(), ease = (x) => 1 - (1 - Math.min(1, Math.max(0, x))) ** 3, els = [...document.querySelectorAll(".lb .s")];
  const tick = (now) => {
    const t = (now - t0) / 1000;
    els.forEach((e) => {
      let v = +e.dataset.b * ease((t - T_KM) / .45);
      BONUS.slice(0, +e.dataset.l).forEach((x, k) => { v += x * ease((t - T_B(k)) / .45); });
      e.textContent = fmt(v);
    });
    if (t < 2.4) requestAnimationFrame(tick);
  };
  matchMedia("(prefers-reduced-motion: reduce)").matches ? tick(t0 + 9e3) : requestAnimationFrame(tick);
}
async function toReveal() {
  if (state === "reveal") return;
  state = "reveal";
  if (!guess) { guess = new maplibregl.LngLat(16.37, 48.21); marker = new maplibregl.Marker({ element: el(PIN), anchor: "bottom" }).setLngLat(guess).addTo(map); }
  const truth = new maplibregl.LngLat(ROUND.lon, ROUND.lat), d = km(guess, truth);
  // The bonus level comes from the server, which locates the pin in Natural Earth; until the API is wired in,
  // the demo scores the distance only.
  const lvl = 0;
  lvlMe = lvl;
  const friends = [
    { who: "маша", lng: ROUND.lon + 0.35, lat: ROUND.lat + 0.12, lvl: 2 }, { who: "лёша", lng: ROUND.lon + 13.5, lat: ROUND.lat + 6.2, lvl: 1 },
    { who: "костя", lng: ROUND.lon + 0.02, lat: ROUND.lat + 0.01, lvl: 3 }, { who: "ваня", lng: -8.61, lat: 41.15, lvl: 2 },
    { who: "ира", lng: -3.70, lat: 40.42, lvl: 1 }, { who: "артём", lng: 31.24, lat: 30.04, lvl: 0 },
  ];
  const rows = [...friends.map((f) => ({ who: f.who, d: km(f, truth), lvl: f.lvl })), { who: "ты", d, lvl, me: true }]
    .map((r) => ({ ...r, pts: score(r.d, r.lvl) })).sort((a, b) => b.pts - a.pts);
  paintBoard(rows, d, lvl);
  $("lb").offsetWidth; // the bar and avatars must catch the closed state, otherwise there will be no animation
  document.body.classList.add("is-reveal");
  ovUnderBoard();
  // Layer order: friends, the target above them, your own pin on top.
  marks = { truth, friends: friends.map((f) => {
    const e = el('<div class="friend"><span class="g"><s></s>' + av(f.who) + '<b>' + f.who + '</b></span></div>');
    new maplibregl.Marker({ element: e }).setLngLat([f.lng, f.lat]).addTo(map).getElement().style.zIndex = 1;
    return { at: [f.lng, f.lat], g: e.firstChild, s: e.querySelector("s"), b: e.querySelector("b") };
  }) };
  new maplibregl.Marker({ element: el(TARGET) }).setLngLat(truth).addTo(map).getElement().style.zIndex = 2;
  marker.getElement().style.zIndex = 3;
  layoutMarks();
  const line = { type: "Feature", geometry: { type: "LineString", coordinates: [[guess.lng, guess.lat], [truth.lng, truth.lat]] } };
  map.addSource("line", { type: "geojson", data: line });
  map.addLayer({ id: "line-case", type: "line", source: "line", paint: { "line-color": "#FFFFFF", "line-width": 5 } });
  map.addLayer({ id: "line", type: "line", source: "line", paint: { "line-color": "#1A412B", "line-width": 2, "line-dasharray": [2, 1.5] } });
  $("timer").classList.add("hidden"); split(true); $("go").textContent = "Переслать в чат";
  setCall("Итог дня", "смотреть");
  overlay("",
    '<div class="place up" id="place"><button class="t" id="placeT">' + ROUND.place + ' ↓<p></p></button>' +
    '<button class="x" id="placeX" aria-label="свернуть">×</button><button class="tab" id="placeTab">о месте ↑</button></div>');
  countUp();
  // The fact expands on tap; the cross collapses the plate into a tab so it does not cover the map on a phone.
  $("placeT").onclick = () => {
    const open = !$("placeT").querySelector("p").textContent;
    $("placeT").innerHTML = ROUND.place + (open ? " ↑" : " ↓") + "<p>" + (open ? ROUND.fact : "") + "</p>";
  };
  $("placeX").onclick = () => $("place").classList.add("min");
  $("placeTab").onclick = () => $("place").classList.remove("min");
  view = "map"; document.body.classList.add("is-map"); paintBackdrop(); syncBack();
  const b = new maplibregl.LngLatBounds(guess, guess).extend(truth); friends.forEach((f) => b.extend([f.lng, f.lat]));
  map.setPadding({ left: 0, top: 0, right: 0, bottom: 0 });
  const { t, b: bt } = reserve();
  map.fitBounds(b, { padding: { top: t + 24, left: 40, right: 40, bottom: bt + 24 }, maxZoom: 9, duration: 1200 });
  haptic("success");
}
$("go").addEventListener("click", () => {
  if (state === "guess" && guess) toWaiting();
  else if (state === "reveal") share();
});
// A line to forward without spoilers: day number, distance and score; the place is not in it.
// In Telegram we open the chat picker; in a browser, the system "Share" menu or the clipboard.
const SHARE_URL = window.MAPLY_SHARE_URL || location.origin;
function share() {
  const d = km(guess, { lat: ROUND.lat, lng: ROUND.lon });
  const line = "maply #" + ROUND.n + " · " + fmt(d) + " км · " + fmt(score(d, lvlMe));
  if (inTg) { tg.openTelegramLink("https://t.me/share/url?url=" + encodeURIComponent(SHARE_URL) + "&text=" + encodeURIComponent(line)); return; }
  if (navigator.share) { navigator.share({ text: line, url: SHARE_URL }).catch(() => {}); return; }
  navigator.clipboard.writeText(line + " " + SHARE_URL).then(() => toast("Скопировано: " + line), () => toast(line));
}
// Demo result via link: wait for both the panorama and the map style, otherwise addSource fails on an unloaded style.
if (new URLSearchParams(location.search).get("state") === "reveal")
  Promise.all([new Promise((r) => viewer.on("load", r)), new Promise((r) => mapReady ? r() : map.once("load", r))]).then(() => setTimeout(toReveal, 300));
