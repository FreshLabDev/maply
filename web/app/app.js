// TODAY is /api/today: the day number, the picture, the deadline and the reveal for the chat the app was opened from.
let TODAY = null;
const $ = (id) => document.getElementById(id);
const PIN_PATH = "M320 501 L214 395 A150 150 0 1 1 426 395 Z M320 229 A60 60 0 1 0 320 349 A60 60 0 1 0 320 229 Z";
// On the map, markers are dark with a white outline: visible on both light land and green water.
const PIN = '<svg class="pin" viewBox="150 119 340 402"><path fill="#1A412B" stroke="#FFFFFF" stroke-width="24" stroke-linejoin="miter" paint-order="stroke" fill-rule="evenodd" d="' + PIN_PATH + '"/></svg>';
const pick = (a) => a[Math.floor(Math.random() * a.length)];
const av = (who, me) => '<i class="av' + (me ? ' me' : '') + '">' + (me ? esc(T("you")) : esc(who[0] || "?")) + '</i>';
// Names come from Telegram profiles and go into innerHTML.
const esc = (s) => String(s).replace(/[&<>"']/g, (c) => "&#" + c.charCodeAt(0) + ";");
const el = (html) => { const d = document.createElement("div"); d.innerHTML = html; return d.firstElementChild; };
// Thousands separated by a narrow space, as in the Asterfield system.
const fmt = (n) => String(Math.round(n)).replace(/\B(?=(\d{3})+(?!\d))/g, " ");

/* ---------- Words ---------- */
// Every line comes from /texts/<lang>.json, built from internal/i18n: the app holds no text of its own.
let LANG = "en", TX = {}, ZONES = {};
const T = (key, vars) => {
  let s = TX[key] === undefined ? key : TX[key];
  for (const k in vars || {}) s = s.split("{" + k + "}").join(vars[k]);
  return s;
};
const lines = (key) => T(key).split("\n");
const errorText = (e) => TX["error." + e.message] ? T("error." + e.message) : T("error.network");
async function loadTexts(lang) {
  let r = await fetch("texts/" + lang + ".json").catch(() => null);
  if (!r || !r.ok) r = await fetch("texts/en.json");
  const d = await r.json();
  LANG = d.lang; TX = d.t; ZONES = d.zone;
  document.documentElement.lang = LANG;
  document.querySelectorAll("[data-t]").forEach((e) => { e.textContent = T(e.dataset.t); });
  document.querySelectorAll("[data-aria]").forEach((e) => e.setAttribute("aria-label", T(e.dataset.aria)));
  buildTape();
  if (typeof setMapLanguage === "function" && map) setMapLanguage(LANG);
}

/* ---------- Server ---------- */
// In Telegram every request carries initData; the server checks its signature and works out the chat from
// start_param. In a plain browser it is the site: nobody signs in, a cookie holds a random player id, the one answer
// a day goes into the world ranking under the name the player gave or "Guest", and the place opens only after the
// latest reveal among maply's zones. localStorage keeps a copy of the pin and the name.
const SITE = !inTg;
async function api(path, body) {
  if (SITE) return site(path, body);
  const headers = { Authorization: "tma " + tg.initData };
  if (body) headers["Content-Type"] = "application/json";
  const r = await fetch(path, { method: body ? "POST" : "GET", headers, body: body && JSON.stringify(body) });
  return reply(r);
}
async function reply(r) {
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw Object.assign(new Error(data.error || "network"), { data });
  return data;
}
const KEPT = "maply.answers";
function kept() { try { return JSON.parse(localStorage.getItem(KEPT)) || {}; } catch { return {}; } }
function keep(date, pin) {
  const all = kept(); all[date] = pin;
  // Two weeks are enough to see any result; older answers go.
  Object.keys(all).sort().slice(0, -14).forEach((d) => delete all[d]);
  try { localStorage.setItem(KEPT, JSON.stringify(all)); } catch { throw new Error("network"); }
}
const NAME = "maply.name";
const siteName = () => { try { return localStorage.getItem(NAME) || ""; } catch { return ""; } };
let tz = ""; try { tz = Intl.DateTimeFormat().resolvedOptions().timeZone || ""; } catch {}
async function site(path, body) {
  if (path === "/api/guess") {
    if (!secondsTo(TODAY.deadline)) throw new Error("closed");
    const res = await reply(await fetch("/api/site/guess?" + new URLSearchParams({ tz }), {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ lat: body.lat, lon: body.lon, name: siteName() }),
    }));
    keep(TODAY.date, body);
    return res;
  }
  if (path === "/api/today") {
    const today = await reply(await fetch("/api/site/today?" + new URLSearchParams({ tz, lang: LANG0 })));
    today.answer = today.answer || kept()[today.date] || null;
    if (today.name) try { localStorage.setItem(NAME, today.name); } catch {}
    return today;
  }
  const q = { date: TODAY.date, lang: LANG }, mine = kept()[TODAY.date];
  if (mine) { q.lat = mine.lat; q.lon = mine.lon; }
  return reply(await fetch("/api/site/result?" + new URLSearchParams(q)));
}
// On the site a plate under the map says where the real game is: the ranking and the group live in Telegram.
const tgLink = () => SITE && TODAY && TODAY.bot
  ? '<a class="pl w tgl" href="https://t.me/' + encodeURIComponent(TODAY.bot) + '" target="_blank" rel="noopener">' + esc(T("play_telegram")) + ' →</a>' : "";
let toastT = 0;
function toast(text) { const t = $("toast"); t.textContent = text; t.classList.add("on"); clearTimeout(toastT); toastT = setTimeout(() => t.classList.remove("on"), 2300); }

let li = 0;
const loadTimer = setInterval(() => { if (TX.loading) { const l = lines("loading"); $("loadtext").textContent = l[++li % l.length]; } }, 900);
function stopLoading(text) { clearInterval(loadTimer); $("loadtext").textContent = text; }
$("logo").addEventListener("click", () => openDays());

/* ---------- Countdown ---------- */
// Before the answer the timer runs to the deadline: the earliest reveal among the person's chats. After it, to the reveal
// of this chat. Both are instants from the server; AT says the reveal in the chat's own zone.
let AT = "", ZONE_NAME = "";
const secondsTo = (iso) => Math.max(0, Math.round((Date.parse(iso) - Date.now()) / 1000));
const hm = (s) => Math.floor(s / 3600) + ":" + String(Math.floor(s % 3600 / 60)).padStart(2, "0");
function tick() {
  if (!TODAY) return;
  $("left").textContent = hm(secondsTo(state === "guess" ? TODAY.deadline : TODAY.reveal));
  const w = $("wleft"); if (w) w.textContent = hm(secondsTo(TODAY.reveal));
  if (state === "guess" && !secondsTo(TODAY.deadline)) toWaiting(true);
  if (state === "wait" && !secondsTo(TODAY.reveal)) loadResult();
}
setInterval(tick, 15000);

/* ---------- Panorama: Pannellum, inertia, pinch, double tap ---------- */
// The photo's author is credited only after the reveal: a Mapillary profile lists where its owner takes pictures.
const MAP_CREDITS = '<span>© <a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noopener">OpenStreetMap</a>, OpenMapTiles, <a href="https://openfreemap.org" target="_blank" rel="noopener">OpenFreeMap</a></span>';
const SOURCES = { mapillary: ["Mapillary", "https://www.mapillary.com"], panoramax: ["Panoramax", "https://panoramax.fr"] };
function credits(place) {
  let photo = "";
  if (place && SOURCES[place.source]) {
    const [name, url] = SOURCES[place.source], lic = place.license.replace(/^CC-/, "CC ").replace(/-(\d)/, " $1");
    photo = '<span>© ' + (place.author ? esc(place.author) + ", " : "") + '<a href="' + url + '" target="_blank" rel="noopener">' + name + '</a> ' + lic + ' ·</span>';
  }
  $("credits").innerHTML = photo + MAP_CREDITS;
}
credits(null);
// Pannellum sets the horizontal field of view. On a portrait phone this gave ~140° vertically,
// and tilting up/down hit the limits. We pick hfov so that the vertical span is ~95°.
function baseHfov() {
  const w = $("pano").clientWidth || innerWidth, h = $("pano").clientHeight || innerHeight, v = 95 * Math.PI / 180;
  const hf = 2 * Math.atan(Math.tan(v / 2) * w / h) * 180 / Math.PI;
  return Math.max(50, Math.min(105, hf));
}
// The viewer is created once /api/today says which picture it is; until then everything that turns it waits.
let viewer = null;
function startPano() {
  viewer = pannellum.viewer("pano", Object.assign({
    type: "equirectangular", panorama: TODAY.image, autoLoad: true, showControls: false, compass: false,
    hfov: baseHfov(), minHfov: 20, maxHfov: 110, friction: 0.1, keyboardZoom: false, disableKeyboardCtrl: true,
    backgroundColor: [0.10, 0.25, 0.17], pitch: 0
  }, TODAY.kind === "panorama" ? {} : { haov: 80, vaov: 50, maxHfov: 80 }));
  viewer.on("load", () => { clearInterval(loadTimer); $("loader").classList.add("gone"); });
  viewer.on("error", () => stopLoading(T("no_image")));
}
addEventListener("resize", () => viewer && viewer.setHfov(baseHfov(), 0));
let lastTap = 0;
$("pano").addEventListener("pointerdown", () => $("hint").classList.add("gone"));
$("pano").addEventListener("pointerup", () => {
  const now = Date.now();
  if (viewer && now - lastTap < 280) { const b = baseHfov(), h = viewer.getHfov(); viewer.setHfov(h > b * 0.8 ? b * 0.5 : b, 350); haptic("light"); }
  lastTap = now;
});
// Direction by compass = frame centre direction + camera rotation. The compass card rotates the opposite way.
let HEADING = 0;
// Full-width tape: about 150° is visible in the window, so the cardinal directions are readable rather than flickering.
// Drawn over three turns (−360…720°) and shifted: crossing north does not jump.
let TAPE_K = 1.5, TAPE_W = 0;
function buildTape() {
  TAPE_W = $("compass").clientWidth || innerWidth; TAPE_K = TAPE_W / 150;
  const NAMES = {};
  ["n", "ne", "e", "se", "s", "sw", "w", "nw"].forEach((k, i) => { NAMES[i * 45] = esc(TX["compass_" + k] || ""); });
  let svg = "";
  for (let d = -360; d <= 720; d += 5) {
    const x = ((d + 360) * TAPE_K).toFixed(1), n = ((d % 360) + 360) % 360;
    if (n === 0) svg += '<rect x="' + (x - 12) + '" y="6" width="24" height="26" fill="#FFFFFF"/><text x="' + x + '" y="26" text-anchor="middle" fill="#245A3C" style="font: 700 19px/1 var(--display)">' + NAMES[0] + '</text>';
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
  const h = ((((viewer ? viewer.getYaw() : 0) + HEADING) % 360) + 360) % 360;
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
  if (!viewer) return;
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
  if (viewer) viewer.setYaw(viewer.getYaw() + (Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY) / TAPE_K * 0.6, false);
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
if (tgAt("6.1")) tg.BackButton.onClick(() => document.body.classList.contains("is-sheet") ? $("shBack").click() : toPhoto());
// Splits the bottom strip into two buttons or merges it back into one.
// The left button label depends on the stage: before the answer it invites a closer look, after the answer simply to enjoy.
const LOOK = { guess: ["look_more", "look"], wait: ["admire", "admire"], reveal: ["walk", "walk"] };
function split(on) { $("act").classList.toggle("on", on); $("look").textContent = T(LOOK[state][on ? 1 : 0]); }
addEventListener("resize", () => { if (mapReady) map.jumpTo(view === "map" ? { padding: bigCam().padding } : miniCam()); });
function setCall(b, sub) { $("callB").textContent = b; $("callS").innerHTML = esc(sub) + " <em>→</em>"; }

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
    marker.on("dragend", () => { guess = marker.getLngLat(); haptic("select"); toast(pick(lines("moved"))); });
  } else {
    marker.setLngLat(guess);
    const p = marker.getElement(); p.style.animation = "none"; void p.offsetWidth; p.style.animation = "";
  }
  const wet = map.queryRenderedFeatures(point, { layers: ["water"] }).length > 0;
  toast(pick(lines(wet ? "on_water" : first ? "on_land" : "moved")));
  haptic("medium");
  // The pin is placed but the answer is not sent: Telegram will ask for confirmation before closing the app.
  if (first) { split(true); setCall(T("pin_set"), T("answer")); if (tgAt("6.2")) tg.enableClosingConfirmation(); }
}
map.on("click", (e) => {
  // A tap beside the globe, in the void around it, returns to the photo. We check the surface itself via MapLibre:
  // for a point off the globe it still returns a coordinate on its edge, and previously a pin was placed there.
  if (!onGlobe(e.point)) { toPhoto(); return; }
  if (state === "guess") putPin(e.lngLat, e.point);
});

/* ---------- Waiting and result ---------- */
// Score: up to 3,500 for distance plus bonuses for guessing the continent, country and city, 5,000 at most for the pin;
// on top, up to 250 for answering early (scoring.SpeedBonus). The bars are drawn on that whole scale.
const BONUS = [300, 500, 700], SPEED_MAX = 250;
const base = (d) => 3500 * Math.exp(-d / 1492.7);
const bonusOf = (lvl) => BONUS.slice(0, lvl).reduce((a, b) => a + b, 0);
const score = (d, lvl) => base(d) + bonusOf(lvl);
// The server sends the bonus in points; the ladder makes the level unambiguous.
const levelOf = (bonus) => [300, 800, 1500].filter((x) => (bonus || 0) >= x).length;

// Send the pin. Before the reveal the server answers "accepted" and nothing else: no distance, no score.
async function send() {
  $("go").disabled = true;
  try {
    const at = guess.wrap();
    await api("/api/guess", { lat: at.lat, lon: at.lng });
    toWaiting("sent");
  } catch (e) {
    if (e.message === "already_answered") { TODAY = await api("/api/today").catch(() => TODAY); restore(); return; }
    toast(errorText(e));
    if (e.message === "closed") toWaiting("closed");
  } finally { $("go").disabled = false; }
}
// "sent" right after the answer, "closed" when the window shut without one, "restore" when the app reopens after answering.
function toWaiting(how) {
  state = "wait"; if (marker) marker.setDraggable(false);
  if (how === "sent") haptic("success");
  if (tgAt("6.2")) tg.disableClosingConfirmation();
  split(false); setCall(T(how === "closed" ? "closed" : "sent"), T("result_at", { time: AT }));
  $("timer").classList.add("hidden");
  overlay('<div class="pl nums"><b id="wleft">' + hm(secondsTo(TODAY.reveal)) + '</b><span class="up">' + esc(T("until_answer")) + '<br>' +
    esc(T("at", { time: AT, zone: ZONE_NAME })) + '</span></div>', tgLink());
  // Zoom past the edge of the globe unfolding: at 3.5 the map is still slightly curved, and a pinch immediately snapped it back to a globe.
  if (guess && view === "map") map.easeTo({ padding: bigCam().padding, center: guess, zoom: GLOBE_Z[1] + 0.4, duration: 700, easing: easeOut });
}
// The app reopened after the answer: put the pin back where it was sent.
function restore() {
  if (!TODAY.answer) return;
  guess = new maplibregl.LngLat(TODAY.answer.lon, TODAY.answer.lat);
  if (!marker) marker = new maplibregl.Marker({ element: el(PIN), anchor: "bottom" }).setLngLat(guess).addTo(map);
  else marker.setLngLat(guess);
  if (view === "photo") map.jumpTo({ ...miniCam(), center: guess });
  toWaiting("restore");
}
// Result markers are laid out in pixels on every map-motion frame. A friend's point that falls under the target
// moves out onto a ring around it; nearby points are pushed apart; a label takes the first free spot, and if it
// ends up far away, a leader line is drawn to the point.
let marks = null, mine = null;
const RING = 27, GAP = 23;
function layoutMarks() {
  if (!marks) return;
  const T = map.project(marks.truth), G = guess ? map.project(guess) : { x: -1e4, y: -1e4 }, c = map.getContainer(), W = c.clientWidth;
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
const pct = (x) => (x * 100 / (5000 + SPEED_MAX)).toFixed(2) + "%";
// rows: { who, d (km), pts, lvl, sp, me } from /api/result. The grey part of a bar is the points for the distance, the
// gold one after the bonuses is for speed.
function paintBoard(rows) {
  // Collapsed, the first rows and yours are visible; if you are lower, there is one fewer row so the height does not change.
  const meAt = rows.findIndex((r) => r.me), top = meAt >= ROWS ? ROWS - 1 : ROWS, IF = [T("what_continent"), T("what_country"), T("what_city")];
  // Segment tooltips sit in data-tip, not data-t: data-t marks the static lines loadTexts fills.
  $("lb").innerHTML = rows.map((r, i) => {
    const b0 = r.pts - bonusOf(r.lvl) - r.sp, segs = ['<i class="km" data-tip="' + esc(T("km_points", { points: fmt(b0), km: fmt(r.d) })) + '" style="width:' + pct(b0) + ';--d:' + T_KM + 's"></i>'];
    BONUS.forEach((x, k) => {
      if (k < r.lvl) segs.push('<i data-tip="' + esc(T("bonus", { points: x, what: IF[k] })) + '" style="width:' + pct(x) + ';--d:' + T_B(k) + 's"></i>');
    });
    if (r.sp) segs.push('<i class="sp" data-tip="' + esc(T("speed_bonus", { points: r.sp })) + '" style="width:' + pct(r.sp) + ';--d:' + T_B(r.lvl) + 's"></i>');
    if (r.me) BONUS.forEach((x, k) => {
      if (k >= r.lvl) segs.push('<i class="if" data-tip="' + esc(T("bonus_missed", { points: x, what: IF[k] })) + '" style="width:' + pct(x) + ';--d:' + (T_B(r.lvl) + .1) + 's"></i>');
    });
    const x = i >= top && !r.me ? " x" : "";
    return '<div class="r' + (r.me ? " me" : "") + x + '"><em>' + (i + 1) + '</em>' + av(r.who, r.me) + '<span>' + esc(r.who) + '</span>' +
      '<span class="hit" data-rest="' + esc(T("short_of", { points: fmt(5000 - (r.pts - r.sp)) })) + '"><span class="bar">' + segs.join("") + '</span></span>' +
      '<b class="s" data-b="' + b0 + '" data-l="' + r.lvl + '" data-sp="' + r.sp + '">0</b><span class="k">' + fmt(r.d) + ' ' + esc(T("km")) + '</span></div>';
  }).join("");
  const hidden = rows.length - Math.min(rows.length, top + (meAt >= top ? 1 : 0));
  $("lb").classList.remove("open");
  $("more").classList.toggle("hidden", hidden <= 0);
  $("more").dataset.n = hidden;
  $("more").textContent = T("more", { n: hidden }) + " ↓";
  // Legend and "what if" at the same distance: one line, wraps in whole chunks.
  const t = (h) => '<span>' + h + '</span>', me = rows[meAt], legend = t('<i class="km"></i>' + esc(T("legend_km"))) + ' ' + t('<i></i>' + esc(T("legend_bonus"))) +
    (rows.some((r) => r.sp) ? ' ' + t('<i class="sp"></i>' + esc(T("legend_speed"))) : "");
  if (!me) { $("ifs").innerHTML = rows.length ? legend : t(esc(T("no_answer"))); return; }
  const d = me.d, lvl = me.lvl, b = (n) => '<b>' + fmt(n) + '</b>';
  $("ifs").innerHTML = legend + ' · ' + (lvl >= 3 ? t(esc(T("all_bonuses"))) :
    t('<i class="if"></i>' + T("if_first", { what: esc(IF[lvl]), score: b(score(d, lvl + 1) + me.sp) })) +
    IF.slice(lvl + 1).map((n, j) => ', ' + t(T("if_next", { what: esc(n), score: b(score(d, lvl + j + 2) + me.sp) }))).join(""));
}
// Full leaderboard: a tap on the table or on "N more" expands and collapses it.
function toggleBoard() {
  if ($("more").classList.contains("hidden")) return;
  const open = $("lb").classList.toggle("open");
  $("more").textContent = open ? T("less") + " ↑" : T("more", { n: $("more").dataset.n }) + " ↓";
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
  tip.textContent = seg ? seg.dataset.tip : hit.dataset.rest;
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
      v += +e.dataset.sp * ease((t - T_B(+e.dataset.l)) / .45);
      e.textContent = fmt(v);
    });
    if (t < 2.4) requestAnimationFrame(tick);
  };
  matchMedia("(prefers-reduced-motion: reduce)").matches ? tick(t0 + 9e3) : requestAnimationFrame(tick);
}
// The country's name in the interface language, from the browser: the server sends only the ISO code.
const regionName = (iso) => { try { return new Intl.DisplayNames([LANG], { type: "region" }).of(iso); } catch { return iso; } };
let resultLoading = false;
async function loadResult() {
  if (resultLoading || state === "reveal") return;
  resultLoading = true;
  try { toReveal(await api("/api/result")); }
  catch (e) {
    // The server's clock decides; it says when to ask again.
    if (e.message === "not_revealed" && e.data.opens_at) TODAY.reveal = e.data.opens_at;
    else toast(errorText(e));
  } finally { resultLoading = false; }
}
function toReveal(res) {
  if (state === "reveal") return;
  state = "reveal";
  const P = res.place, truth = new maplibregl.LngLat(P.lon, P.lat);
  credits(P);
  const rows = res.table.map((r) => ({ who: r.me ? T("you") : r.name, at: [r.lon, r.lat], d: r.km || 0, pts: r.points || 0, lvl: levelOf(r.bonus), sp: r.speed || 0, me: !!r.me }))
    .sort((a, b) => b.pts - a.pts);
  mine = rows.find((r) => r.me) || null;
  // A pin that never reached the server is not an answer: it leaves the map rather than draw a line to the place.
  if (!mine && marker) { marker.remove(); marker = null; guess = null; }
  if (mine && !guess) guess = new maplibregl.LngLat(mine.at[0], mine.at[1]);
  if (guess && !marker) marker = new maplibregl.Marker({ element: el(PIN), anchor: "bottom" }).setLngLat(guess).addTo(map);
  const friends = rows.filter((r) => !r.me);
  paintBoard(rows);
  $("lb").offsetWidth; // the bar and avatars must catch the closed state, otherwise there will be no animation
  document.body.classList.add("is-reveal");
  ovUnderBoard();
  // Layer order: friends, the target above them, your own pin on top.
  marks = { truth, friends: friends.map((f) => {
    const e = el('<div class="friend"><span class="g"><s></s>' + av(f.who) + '<b>' + esc(f.who) + '</b></span></div>');
    new maplibregl.Marker({ element: e }).setLngLat(f.at).addTo(map).getElement().style.zIndex = 1;
    return { at: f.at, g: e.firstChild, s: e.querySelector("s"), b: e.querySelector("b") };
  }) };
  new maplibregl.Marker({ element: el(TARGET) }).setLngLat(truth).addTo(map).getElement().style.zIndex = 2;
  if (marker) marker.getElement().style.zIndex = 3;
  layoutMarks();
  if (guess) {
    const line = { type: "Feature", geometry: { type: "LineString", coordinates: [[guess.lng, guess.lat], [truth.lng, truth.lat]] } };
    map.addSource("line", { type: "geojson", data: line });
    map.addLayer({ id: "line-case", type: "line", source: "line", paint: { "line-color": "#FFFFFF", "line-width": 5 } });
    map.addLayer({ id: "line", type: "line", source: "line", paint: { "line-color": "#1A412B", "line-width": 2, "line-dasharray": [2, 1.5] } });
  }
  $("timer").classList.add("hidden"); split(!!mine); $("go").textContent = T("share");
  setCall(T("day_result"), T("see"));
  const where = esc([P.city, regionName(P.country)].filter(Boolean).join(", ")), fact = esc(P.fact || "");
  overlay("", tgLink() +
    '<div class="place up" id="place"><button class="t" id="placeT">' + where + (fact ? ' ↓' : '') + '<p></p></button>' +
    '<button class="x" id="placeX" aria-label="' + esc(T("aria_collapse")) + '">×</button><button class="tab" id="placeTab">' + esc(T("about_place")) + ' ↑</button></div>');
  countUp();
  // The fact expands on tap; the cross collapses the plate into a tab so it does not cover the map on a phone.
  $("placeT").onclick = () => {
    if (!fact) return;
    const open = !$("placeT").querySelector("p").textContent;
    $("placeT").innerHTML = where + (open ? " ↑" : " ↓") + "<p>" + (open ? fact : "") + "</p>";
  };
  $("placeX").onclick = () => $("place").classList.add("min");
  $("placeTab").onclick = () => $("place").classList.remove("min");
  view = "map"; document.body.classList.add("is-map"); paintBackdrop(); syncBack();
  const b = new maplibregl.LngLatBounds(truth, truth); if (guess) b.extend(guess); friends.forEach((f) => b.extend(f.at));
  map.setPadding({ left: 0, top: 0, right: 0, bottom: 0 });
  const { t, b: bt } = reserve();
  map.fitBounds(b, { padding: { top: t + 24, left: 40, right: 40, bottom: bt + 24 }, maxZoom: 9, duration: 1200 });
  haptic("success");
}
$("go").addEventListener("click", () => {
  if (state === "guess" && guess) send();
  else if (state === "reveal") share();
});
// Forwarding the result. In Telegram 8.0 and later the server prepares the result card, a map picture with a Play button,
// and shareMessage opens Telegram's chat picker with it. If the card cannot be made (no renderer, a slow one, an old client,
// the picker refusing it), a line with the day number, distance and score goes through t.me/share. In a browser it is
// the system "Share" menu or the clipboard.
const SHARE_URL = window.MAPLY_SHARE_URL || location.origin;
const shareLine = () => "maply #" + TODAY.day + (mine ? " · " + fmt(mine.d) + " " + T("km") + " · " + fmt(mine.pts) : "");
function shareLink() {
  const line = shareLine();
  if (inTg) { tg.openTelegramLink("https://t.me/share/url?url=" + encodeURIComponent(SHARE_URL) + "&text=" + encodeURIComponent(line)); return; }
  if (navigator.share) { navigator.share({ text: line, url: SHARE_URL }).catch(() => {}); return; }
  navigator.clipboard.writeText(line + " " + SHARE_URL).then(() => toast(T("copied", { line })), () => toast(line));
}
// Telegram reports a prepared message it could not send (expired, unsupported); declining the picker is not a failure.
if (inTg) tg.onEvent("shareMessageFailed", (e) => { if (!e || e.error !== "USER_DECLINED") shareLink(); });
let sharing = false;
async function share() {
  if (!(inTg && tgAt("8.0") && typeof tg.shareMessage === "function")) return shareLink();
  if (sharing) return;
  sharing = true; $("go").classList.add("busy");
  try {
    const { id } = await api("/api/share", {});
    tg.shareMessage(id);
    return;
  } catch (e) {
    // Too many cards in a minute is worth telling; any other reason, the person still gets to forward.
    if (e.message === "rate_limited") { toast(errorText(e)); return; }
  } finally { sharing = false; $("go").classList.remove("busy"); }
  shareLink();
}
/* ---------- Days ---------- */
// The home page, the same in Telegram and on the site: every day newest first with the world's numbers, and for a
// closed day its whole ranking. A day closes after the last zone's reveal; until then it shows only how many answered.
// Each avatar carries where the player came from: Telegram or the site.
const SRC_TG = '<svg class="src" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="12" fill="#2AABEE"/><path fill="#fff" d="M5.4 11.7l10.9-4.2c.5-.2 1 .1.8.9l-1.9 8.8c-.1.6-.5.8-1 .5l-2.8-2.1-1.4 1.3c-.2.2-.3.3-.6.3l.2-2.9 5.3-4.8c.2-.2 0-.3-.3-.1l-6.5 4.1-2.8-.9c-.6-.2-.6-.6.1-.9z"/></svg>';
const SRC_SITE = '<svg class="src" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="12" fill="#FFFFFF"/><g fill="none" stroke="#245A3C" stroke-width="1.6"><circle cx="12" cy="12" r="6.5"/><path d="M5.5 12h13M12 5.5c-2.4 2.4-2.4 10.6 0 13M12 5.5c2.4 2.4 2.4 10.6 0 13"/></g></svg>';
const kmT = (km) => fmt(km) + " " + T("km");
const dayDate = (iso) => { try { return new Intl.DateTimeFormat(LANG, { day: "numeric", month: "short", timeZone: "UTC" }).format(new Date(iso + "T00:00:00Z")); } catch { return iso; } };
const clock = (iso) => { try { return new Intl.DateTimeFormat(LANG, { hour: "2-digit", minute: "2-digit", hourCycle: "h23" }).format(new Date(iso)); } catch { return ""; } };
const nameOf = (r) => r.name || (r.site ? T("guest") : "?");
// Sofia Sans draws Cyrillic in its Bulgarian forms unless told the language. A name can be in any script; in an interface
// that is not Cyrillic, Russian forms are the likelier right ones for a Cyrillic name.
const nameLang = () => ["ru", "uk", "be"].includes(LANG) ? LANG : "ru";
async function getJSON(path) {
  if (SITE) return reply(await fetch(path + (path.includes("?") ? "&" : "?") + "lang=" + encodeURIComponent(LANG)));
  return reply(await fetch(path, { headers: { Authorization: "tma " + tg.initData } }));
}
function sheet(title, html, back) {
  const sh = $("sheet");
  sh.innerHTML = '<div class="sh-top"><button class="sh-b" id="shBack" aria-label="' + esc(T("aria_collapse")) + '">' + (back ? "←" : "×") + '</button><b>' + title + '</b></div><div class="sh-body">' + html + '</div>';
  sh.classList.add("on"); document.body.classList.add("is-sheet"); syncBack();
  $("shBack").onclick = back || closeDays;
}
function closeDays() { $("sheet").classList.remove("on"); document.body.classList.remove("is-sheet"); syncBack(); }
async function openDays() {
  haptic("light");
  sheet(esc(T("days")), '<p class="sh-wait up">' + esc(lines("loading")[0]) + '</p>');
  let data;
  try { data = await getJSON("/api/days"); } catch (e) { sheet(esc(T("days")), '<p class="sh-wait">' + esc(errorText(e)) + '</p>'); return; }
  const days = data.days || [];
  let html = "";
  if (SITE) html += '<label class="sh-name"><span class="up">' + esc(T("your_name")) + '</span><input id="shName" lang="' + nameLang() + '" maxlength="24" autocomplete="nickname" placeholder="' + esc(T("guest")) + '" value="' + esc(siteName()) + '"></label>';
  if (!days.length) html += '<p class="sh-wait">' + esc(T("no_days")) + '</p>';
  for (const d of days) {
    const head = '<span class="up">#' + d.day + ' · ' + esc(dayDate(d.date)) + '</span>';
    if (!d.final) {
      html += '<div class="dd open">' + head + '<p>' + esc(T("day_open", { time: clock(d.opens_at) })) + ' · ' + esc(T("day_answered", { n: fmt(d.pending) })) + '</p></div>';
      continue;
    }
    const where = [d.city, d.country && regionName(d.country)].filter(Boolean).join(", ");
    let rows = '<p>' + esc(T("day_world", { n: fmt(d.players), km: d.median_km != null ? kmT(d.median_km) : "—", pct: d.country_pct })) + '</p>';
    if (d.group) rows += '<p>' + esc(T("day_group", { n: d.group.answered, total: d.group.members })) + (d.group.best_name ? ' · <span lang="' + nameLang() + '">' + esc(d.group.best_name) + "</span>, " + kmT(d.group.best_km) : "") + '</p>';
    if (d.me) rows += '<p class="me">' + esc(T("day_me", { rank: d.me.rank, points: fmt(d.me.points) })) + '</p>';
    html += '<button class="dd" data-date="' + esc(d.date) + '">' + head + '<b>' + esc(where) + '</b>' + rows + '<em>→</em></button>';
  }
  sheet(esc(T("days")), html);
  $("sheet").querySelectorAll(".dd[data-date]").forEach((b) => { b.onclick = () => openDay(b.dataset.date); });
  const name = $("shName");
  if (name) name.onchange = async () => {
    try {
      localStorage.setItem(NAME, name.value.trim());
      await reply(await fetch("/api/site/name", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name: name.value }) }));
      toast(T("name_saved"));
    } catch (e) { toast(errorText(e)); }
  };
}
async function openDay(date) {
  haptic("select");
  let d;
  try { d = await getJSON("/api/day?date=" + encodeURIComponent(date)); } catch (e) { toast(errorText(e)); return; }
  const P = d.place, where = [P.city, regionName(P.country)].filter(Boolean).join(", ");
  let html = '<p class="sh-sub">' + esc(T("day_world", { n: fmt(d.players), km: d.median_km != null ? kmT(d.median_km) : "—", pct: d.country_pct })) + '</p><ol class="rk">';
  for (const r of d.ranking) {
    const name = nameOf(r);
    html += '<li class="' + (r.me ? "me" : "") + '"><span class="n">' + r.rank + '</span><span class="a">' + av(name) + (r.site ? SRC_SITE : SRC_TG) +
      '</span><span class="w" lang="' + nameLang() + '">' + esc(name) + (r.me ? ' <em>· ' + esc(T("you")) + '</em>' : '') + '</span><span class="k">' + kmT(r.km) + '</span><b>' + fmt(r.points) + '</b></li>';
  }
  html += '</ol><p class="sh-legend">' + SRC_TG + esc(T("from_tg")) + SRC_SITE + esc(T("from_site")) + '</p>';
  sheet("#" + d.day + " · " + esc(where), html, openDays);
}

/* ---------- Start ---------- */
// Opened inside Telegram without initData (a plain link in the in-app browser) there is nothing to play as.
async function boot() {
  // The client's language first, so nothing on the first screen is blank; the person's own choice from core
  // arrives with /api/today and wins.
  const [today] = await Promise.all([
    SITE || tg.initData ? api("/api/today").catch((e) => e) : null,
    loadTexts(LANG0).catch(() => {}),
  ]);
  setCall(T("where"), T("drop_pin"));
  if (!today) { stopLoading(T("open_in_telegram")); return; }
  if (today instanceof Error) { stopLoading(errorText(today)); return; }
  TODAY = today;
  if (TODAY.lang && TODAY.lang !== LANG) { await loadTexts(TODAY.lang).catch(() => {}); setCall(T("where"), T("drop_pin")); }
  $("day").textContent = "#" + TODAY.day;
  HEADING = TODAY.heading || 0;
  AT = new Intl.DateTimeFormat(LANG, { timeZone: TODAY.zone, hour: "2-digit", minute: "2-digit", hourCycle: "h23" }).format(new Date(TODAY.reveal));
  ZONE_NAME = ZONES[TODAY.zone] || "";
  startPano();
  // The answer and the result need the map's style: markers and the line are added to it.
  if (!mapReady) await new Promise((r) => map.once("load", r));
  if (TODAY.answer) restore();
  else if (!secondsTo(TODAY.deadline)) toWaiting("closed");
  tick();
  if (!secondsTo(TODAY.reveal)) loadResult();
}
boot();
