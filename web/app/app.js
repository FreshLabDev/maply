// TODAY is /api/today: the day number, the picture, the deadline and the reveal for the chat the app was opened from.
let TODAY = null;
// A script error on someone's phone would otherwise stay there: the first few of a session go to the server's log.
let reported = 0;
function report(message, source, line, column, stack) {
  // "Script error." with no source is an error inside a cross-origin script (Telegram's own): the browser hides
  // everything about it, so it tells us nothing and only uses up the session's five reports.
  if (!message || (/^Script error\.?$/.test(String(message)) && !source)) return;
  if (reported++ >= 5) return;
  const body = JSON.stringify({ message: String(message || ""), source: String(source || ""), line: line || 0, column: column || 0,
    stack: String(stack || ""), version: (window.Telegram && Telegram.WebApp && Telegram.WebApp.version) || "", page: window.Telegram && Telegram.WebApp && Telegram.WebApp.initData ? "tg" : "site" });
  try { fetch("/api/log", { method: "POST", headers: { "Content-Type": "application/json" }, body, keepalive: true }).catch(() => {}); } catch {}
}
addEventListener("error", (e) => report(e.message, e.filename, e.lineno, e.colno, e.error && e.error.stack));
addEventListener("unhandledrejection", (e) => report(e.reason && e.reason.message || e.reason, "promise", 0, 0, e.reason && e.reason.stack));
const $ = (id) => document.getElementById(id);
const PIN_PATH = "M320 501 L214 395 A150 150 0 1 1 426 395 Z M320 229 A60 60 0 1 0 320 349 A60 60 0 1 0 320 229 Z";
// On the map, markers are dark with a white outline: visible on both light land and green water.
// MapLibre places a marker by its element's transform, so the drop plays on the svg inside, not on the element itself.
const PIN = '<div class="pinm"><svg class="pin" viewBox="150 119 340 402"><path fill="#1A412B" stroke="#FFFFFF" stroke-width="24" stroke-linejoin="miter" paint-order="stroke" fill-rule="evenodd" d="' + PIN_PATH + '"/></svg></div>';
// A chevron pointing down: the plate folds away to the bottom edge.
const FOLD = '<svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true"><path d="M3 6l5 5 5-5" fill="none" stroke="currentColor" stroke-width="2"/></svg>';
// The group's mark by a face in the world's table: two heads.
const GROUP = '<svg viewBox="0 0 12 12" aria-hidden="true"><circle cx="4.3" cy="3.9" r="2"/><circle cx="8.6" cy="4.5" r="1.6"/>' +
  '<path d="M.6 10.8c0-2.2 1.6-3.6 3.7-3.6s3.7 1.4 3.7 3.6zM8.9 10.8c0-1.4-.4-2.5-1.2-3.2.3-.1.6-.1.9-.1 1.7 0 2.9 1.2 2.9 3.3z"/></svg>';
const pick = (a) => a[Math.floor(Math.random() * a.length)];
// A Telegram photo comes as a data: URL of a JPEG maply encoded; anything else keeps the letter.
const PHOTO = /^data:image\/jpeg;base64,[A-Za-z0-9+\/=]+$/;
const av = (who, me, photo) => photo && PHOTO.test(photo)
  ? '<i class="av ph' + (me ? ' me' : '') + '"><img src="' + photo + '" alt=""></i>'
  : '<i class="av' + (me ? ' me' : '') + '">' + esc(initial(who)) + '</i>';
// The first character as a reader sees it: a flag, an emoji with its skin tone or a letter with its accent is one.
function initial(s) {
  s = String(s || "").trim();
  if (!s) return "?";
  try {
    if (!SEG || SEG.lang !== LANG) SEG = { lang: LANG, s: new Intl.Segmenter(LANG, { granularity: "grapheme" }) };
    for (const g of SEG.s.segment(s)) return g.segment;
  } catch {}
  return [...s][0];
}
let SEG = null;
// Names come from Telegram profiles and go into innerHTML.
const esc = (s) => String(s).replace(/[&<>"']/g, (c) => "&#" + c.charCodeAt(0) + ";");
const el = (html) => { const d = document.createElement("div"); d.innerHTML = html; return d.firstElementChild; };
// Numbers as the language writes them, like the bot's messages: "3 500" in Russian, "3,500" in English.
// Numbers, dates and times as the bot writes them: digits grouped from four up ("3 500" in Polish too), and Western
// digits in Arabic, as its texts use for places and times.
const numLang = () => LANG === "ar" ? "ar-u-nu-latn" : LANG;
// A number in a language, whole or with given decimals. Each format is built once: building one is slow, and the table
// writes hundreds of numbers.
const NF = new Map();
const numIn = (lang, n, digits = 0) => {
  const key = lang + ":" + digits;
  if (!NF.has(key)) {
    try { NF.set(key, new Intl.NumberFormat(lang === "ar" ? "ar-u-nu-latn" : lang, { minimumFractionDigits: digits, maximumFractionDigits: digits, useGrouping: "always" })); }
    catch { NF.set(key, null); }
  }
  const f = NF.get(key), v = digits ? n : Math.round(n);
  return f ? f.format(v) : v.toFixed(digits);
};
const fmt = (n) => numIn(LANG, n);

/* ---------- Words ---------- */
// Every line comes from /texts/<lang>.json, built from internal/i18n: the app holds no text of its own.
let LANG = "en", TX = {}, ZONES = {}, textsLoaded = false;
// A line from a set of words, with {name} filled in; T is the screen's.
const textIn = (tx, key, vars) => {
  let s = tx[key] === undefined ? key : tx[key];
  for (const k in vars || {}) s = s.split("{" + k + "}").join(vars[k]);
  return s;
};
const T = (key, vars) => textIn(TX, key, vars);
const lines = (key) => T(key).split("\n");
const errorText = (e) => TX["error." + e.message] ? T("error." + e.message) : T("error.network");
async function loadTexts(lang) {
  // Through get(), so a file that hangs gives up like any request rather than keep the loading screen up.
  const d = await get("/texts/" + lang + ".json").then(reply).catch(() => null) || await get("/texts/en.json").then(reply);
  LANG = d.lang; TX = d.t; ZONES = d.zone; textsLoaded = true;
  document.documentElement.lang = LANG;
  // Arabic reads right to left: rows, labels and numbers next to words follow it.
  document.documentElement.dir = LANG === "ar" ? "rtl" : "ltr";
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
async function api(path, body, wait) {
  if (SITE) return site(path, body);
  const headers = { Authorization: "tma " + tg.initData };
  if (body) headers["Content-Type"] = "application/json";
  return reply(await get(path, { method: body ? "POST" : "GET", headers, body: body && JSON.stringify(body) }, wait));
}
// fetch that gives up after wait ms, 10 s by default, the body included: one request that hangs, before its headers or
// in the middle of its body, would otherwise hold the live table still until the app is opened again. A timeout reads as
// a lost connection.
async function get(url, opts, wait) {
  const c = new AbortController(), t = setTimeout(() => c.abort(), wait || 10000);
  try {
    const r = await fetch(url, Object.assign({}, opts, { signal: c.signal }));
    return { ok: r.ok, status: r.status, text: await r.text() };
  }
  catch { throw new Error("network"); }
  finally { clearTimeout(t); }
}
// An error from maply names itself; one from the proxy in front (a 502 page while the bot restarts) does not, and
// reads as the server being away rather than the phone's connection. A success whose body did not arrive whole is a
// lost connection too, not an empty answer.
async function reply(r) {
  let data = null;
  try { data = JSON.parse(r.text); } catch {}
  if (!r.ok) throw Object.assign(new Error((data && data.error) || (r.status >= 500 ? "unavailable" : "bad_request")), { data: data || {} });
  if (!data || typeof data !== "object") throw new Error("network");
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
    const res = await reply(await get("/api/site/guess?" + new URLSearchParams({ tz }), {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ lat: body.lat, lon: body.lon, name: siteName() }),
    }));
    keep(TODAY.date, body);
    return res;
  }
  if (path === "/api/today") {
    const today = await reply(await get("/api/site/today?" + new URLSearchParams({ tz, lang: LANG0 })));
    today.answer = today.answer || kept()[today.date] || null;
    if (today.name) try { localStorage.setItem(NAME, today.name); } catch {}
    return today;
  }
  const q = { date: TODAY.date, lang: LANG }, mine = kept()[TODAY.date];
  if (mine) { q.lat = mine.lat; q.lon = mine.lon; }
  return reply(await get("/api/site/result?" + new URLSearchParams(q)));
}
// On the site a plate under the map says where the real game is: the ranking and the group live in Telegram.
const tgLink = () => SITE && TODAY && TODAY.bot
  ? '<a class="pl w tgl" href="https://t.me/' + encodeURIComponent(TODAY.bot) + '" target="_blank" rel="noopener">' + esc(T("play_telegram")) + ' ' + fwd() + '</a>' : "";
let toastT = 0;
function toast(text) { const t = $("toast"); t.textContent = text; t.classList.add("on"); clearTimeout(toastT); toastT = setTimeout(() => t.classList.remove("on"), 2300); }

let li = 0;
const loadTimer = setInterval(() => { if (TX.loading) { const l = lines("loading"); $("loadtext").textContent = l[++li % l.length]; } }, 900);
function stopLoading(text) { clearInterval(loadTimer); $("loadtext").textContent = text; }
function hideLoader() { clearInterval(loadTimer); $("loader").classList.add("gone"); }
// A start that failed is not final: a tap on the loading screen, the connection coming back or Telegram showing the app
// again tries once more. Without any words loaded the screen shows only the arrow.
let retryLoad = null;
function failLoading(text, again) {
  stopLoading(text.startsWith("error.") ? "↻" : text + " ↻");
  retryLoad = again;
}
function retryLoading() {
  const again = retryLoad; if (!again) return;
  retryLoad = null; $("loadtext").textContent = TX.loading ? lines("loading")[0] : "…";
  again();
}
$("loader").addEventListener("click", retryLoading);
addEventListener("online", retryLoading);
$("logo").addEventListener("click", () => Router.go("/days"));

/* ---------- Countdown ---------- */
// Before the answer the timer runs to the deadline: the earliest reveal among the person's chats. After it, to the reveal
// of this chat. Both are instants from the server; AT says the reveal in the chat's own zone.
let AT = "", ZONE_NAME = "";
// "20:00 (Kyiv)", as the bot writes it, so nobody takes it for their own 20:00. A zone maply does not name gets the time alone.
const withZone = (t) => !ZONE_NAME ? t : LANG === "ja" || LANG === "zh" ? t + "（" + ZONE_NAME + "）" : t + " (" + ZONE_NAME + ")";
const atZone = () => withZone(AT);
// The server's clock: a phone running ten minutes fast must not close the answers early. SKEW is the server's time minus
// the phone's, measured when /api/today came.
let SKEW = 0;
const serverNow = () => Date.now() + SKEW;
const secondsTo = (iso) => Math.max(0, Math.round((Date.parse(iso) - serverNow()) / 1000));
const hm = (s) => Math.floor(s / 3600) + ":" + String(Math.floor(s % 3600 / 60)).padStart(2, "0");
function tick() {
  if (!TODAY) return;
  $("left").textContent = hm(secondsTo(state === "guess" ? TODAY.deadline : TODAY.reveal));
  const w = $("wleft"); if (w) w.textContent = hm(secondsTo(TODAY.reveal));
  if (state === "guess" && !secondsTo(TODAY.deadline)) toWaiting("closed");
  if (state === "wait" && !secondsTo(TODAY.reveal)) loadResult();
  if (state === "reveal" && PHASE === "live" && !secondsTo(TODAY.reveal)) loadResult();
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
// A picture that did not come is asked for again a few times; until it comes the loading screen stays only for someone
// who still has to answer: who has answered plays on the map.
let panoTries = 0;
function startPano() {
  if (viewer) { try { viewer.destroy(); } catch {} viewer = null; }
  viewer = pannellum.viewer("pano", Object.assign({
    type: "equirectangular", panorama: TODAY.image, autoLoad: true, showControls: false, compass: false,
    hfov: baseHfov(), minHfov: 20, maxHfov: 110, friction: 0.1, keyboardZoom: false, disableKeyboardCtrl: true,
    backgroundColor: [0.10, 0.25, 0.17], pitch: 0
  }, TODAY.kind === "panorama" ? {} : { haov: 80, vaov: 50, maxHfov: 80 }));
  viewer.on("load", () => { panoTries = 0; hideLoader(); });
  viewer.on("error", () => {
    if (panoTries < 3) { setTimeout(startPano, 2000 * 2 ** panoTries++); return; }
    if (!TODAY.answer) failLoading(T("no_image"), () => { panoTries = 0; startPano(); });
    else toast(T("no_image"));
  });
}
addEventListener("resize", () => viewer && viewer.setHfov(baseHfov(), 0));
// A double tap zooms in or back out. Only a tap counts: one finger, short and still, the second near the first. Lifting
// two fingers after a pinch fires two pointerups too, and used to undo the pinch.
let lastTap = null, downs = new Map(), multi = false;
$("pano").addEventListener("pointerdown", (e) => {
  $("hint").classList.add("gone");
  downs.set(e.pointerId, { x: e.clientX, y: e.clientY, t: performance.now() });
  if (downs.size > 1) { multi = true; lastTap = null; }
});
const panoUp = (e) => {
  const d = downs.get(e.pointerId); downs.delete(e.pointerId);
  const wasMulti = multi; if (!downs.size) multi = false;
  if (e.type !== "pointerup" || !d || wasMulti) return;
  const now = performance.now();
  if (now - d.t > 250 || Math.hypot(e.clientX - d.x, e.clientY - d.y) > 10) { lastTap = null; return; }
  if (viewer && lastTap && now - lastTap.t < 300 && Math.hypot(e.clientX - lastTap.x, e.clientY - lastTap.y) < 30) {
    const b = baseHfov(), h = viewer.getHfov(); viewer.setHfov(h > b * 0.8 ? b * 0.5 : b, 350); haptic("light"); lastTap = null; return;
  }
  lastTap = { x: e.clientX, y: e.clientY, t: now };
};
$("pano").addEventListener("pointerup", panoUp);
$("pano").addEventListener("pointercancel", panoUp);
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
// Telegram changes the window's height often (keyboard, fullscreen, safe area); the tape depends on the width alone.
addEventListener("resize", () => { if (($("compass").clientWidth || innerWidth) !== TAPE_W) buildTape(); });
// The tape follows the panorama; on the map it is hidden and stands still.
let tapeAt = "";
(function spin() {
  if (!document.body.classList.contains("is-map")) {
    const h = ((((viewer ? viewer.getYaw() : 0) + HEADING) % 360) + 360) % 360;
    const at = "translateX(" + (TAPE_W / 2 - (h + 360) * TAPE_K) + "px)";
    if (at !== tapeAt) { $("tape").style.transform = at; tapeAt = at; }
  }
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
// Enter or Space on the compass, which has no pointer: face north, as a tap does.
tapeEl.addEventListener("click", (e) => { if (e.detail === 0 && viewer) faceNorth(); });
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
// MapLibre's globe grows as 1/cos of the centre's latitude at the same zoom, so the zoom takes that back. The mini
// globe's centre stays within ±40°: a pin further north or south still shows, nearer the edge, and the zoom stays
// above the map's minimum.
const MINI_LAT = 40, miniLat = (lat) => Math.max(-MINI_LAT, Math.min(MINI_LAT, lat || 0));
const atLat = (lat) => Math.log2(Math.cos(lat * Math.PI / 180));
function miniCam(lat) {
  // Globe centre in the bottom-right corner: shift the focus point with padding; the canvas stays fullscreen.
  // The globe sits under its button, which keeps clear of a notch on the side; before layout, where it would be.
  const { W, H } = box(), g = $("globeHit").getBoundingClientRect(), m = $("map").getBoundingClientRect();
  const cx = g.width ? g.left - m.left + g.width / 2 : W - 12 - MINI / 2, cy = g.height ? g.top - m.top + g.height / 2 : H - TAPE_H - 16 - MINI / 2;
  const at = miniLat(lat !== undefined ? lat : map ? map.getCenter().lat : 30);
  // MapLibre rejects negative padding and fails entirely; this happens while the canvas is still zero-sized.
  return { padding: { left: Math.max(0, 2 * cx - W), top: Math.max(0, 2 * cy - H), right: 0, bottom: 0 }, zoom: zoomFor(MINI - 4) + atLat(at) };
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
  return { padding: { left: 0, top: t, right: 0, bottom: b }, zoom: zoomFor(Math.max(MINI, 0.86 * Math.min(W, H - t - b))) + atLat(miniLat(map.getCenter().lat)) };
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
  const c1 = { lng: (guess || c0).lng, lat: miniLat((guess || c0).lat) }, dl = ((c1.lng - c0.lng + 540) % 360) - 180;
  const mix = (a, b, e) => a + (b - a) * e, pad = (a, b, e) => Math.max(0, mix(a, b, e));
  const step = (now) => {
    if (view !== "photo") return;
    // A frame's timestamp can be a little earlier than t0. Unclamped, k goes negative, the padding below zero,
    // and MapLibre throws on every frame after that.
    const k = Math.min(1, Math.max(0, (now - t0) / ms)), e = easeOut(k), cam = miniCam(c1.lat), p = cam.padding;
    map.jumpTo({ zoom: mix(z0, cam.zoom, e), center: [c0.lng + dl * e, mix(c0.lat, c1.lat, e)],
      padding: { left: pad(p0.left, p.left, e), top: pad(p0.top, p.top, e), right: pad(p0.right, p.right, e), bottom: pad(p0.bottom, p.bottom, e) } });
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
// Folded, the main button is out of sight, so it is out of the tab order and the screen reader's reach too.
function split(on) {
  $("act").classList.toggle("on", on); $("look").textContent = T(LOOK[state][on ? 1 : 0]);
  $("go").tabIndex = on ? 0 : -1; $("go").toggleAttribute("aria-hidden", !on);
}
// Telegram resizes the view on its own events (viewport, safe area, fullscreen). A camera call during a pinch resets
// the gesture, so in the map view the new padding waits until the map stands still and no finger is down.
let touches = 0;
$("map").addEventListener("touchstart", (e) => { touches = e.touches.length; }, { passive: true });
["touchend", "touchcancel"].forEach((t) => $("map").addEventListener(t, (e) => {
  touches = e.touches.length;
  if (!touches) { refit(); if (viewDue) keepInView(); }
}, { passive: true }));
let refitDue = false;
function refit() {
  if (!refitDue || !mapReady) return;
  if (view !== "map") { refitDue = false; glideToMini(200); return; }
  if (touches || map.isMoving()) return;
  refitDue = false; map.jumpTo({ padding: bigCam().padding });
}
addEventListener("resize", () => { refitDue = true; refit(); });
// Arrows point the way the text reads: the other way round in Arabic.
function fwd() { return document.documentElement.dir === "rtl" ? "←" : "→"; }
// The caption's arrow points at the mini globe, which stays in the right corner whichever way the text reads: in Arabic it
// comes first, which is the right-hand end.
function setCall(b, sub) {
  $("callB").textContent = b;
  $("callS").innerHTML = document.documentElement.dir === "rtl" ? "<em>→</em> " + esc(sub) : esc(sub) + " <em>→</em>";
}

map = createMap(miniCam().zoom);
map.setPadding(miniCam().padding);
// Markers, the line and the layers of the answer need the style, not the tiles. "load" waits for the tiles' description
// too, and when that one request fails MapLibre never fires it: a returning player would get no pin and no result.
const mapWaiters = [];
function whenMapReady(fn) { mapReady ? fn() : mapWaiters.push(fn); }
function onMapReady() {
  if (mapReady) return;
  mapReady = true; map.jumpTo(miniCam());
  mapWaiters.splice(0).forEach((fn) => fn());
}
map.on("style.load", onMapReady);
map.on("load", onMapReady);
// MapLibre does not ask for the tiles' description again: without it the globe has no water or coasts all session.
let omtTries = 0;
map.on("error", (e) => {
  const src = e && e.sourceId === "omt" && map.getSource("omt");
  if (!src || src.loaded() || omtTries >= 6) return;
  const url = src.url;
  setTimeout(() => { const s = map.getSource("omt"); if (s && !s.loaded()) s.setUrl(url); }, 1500 * 2 ** omtTries++);
});
map.on("moveend", refit);
// While there is no pin, the mini globe rotates slowly: it shows it is alive and can be touched. It turns by the time
// passed, about twelve times a second: every turn redraws a full-screen canvas, and at the display's rate it heated the
// phone and spun faster on 120 Hz. Not at all for someone who asked the system for less motion.
const stillMotion = matchMedia("(prefers-reduced-motion: reduce)");
let spunAt = 0;
(function spinGlobe(now) {
  const due = now - spunAt;
  if (due > 80) {
    if (mapReady && view === "photo" && !guess && !map.isMoving() && !stillMotion.matches && $("loader").classList.contains("gone")) {
      const c = map.getCenter(); map.setCenter([c.lng + Math.min(due, 250) * 0.0072, c.lat]);
    }
    spunAt = now;
  }
  requestAnimationFrame(spinGlobe);
})(0);

let guess = null, marker = null;
function putPin(lngLat, point) {
  const first = !guess; guess = lngLat;
  if (!marker) {
    marker = new maplibregl.Marker({ element: el(PIN), anchor: "bottom", draggable: true }).setLngLat(guess).addTo(map);
    marker.on("dragend", () => { guess = marker.getLngLat(); haptic("select"); toast(pick(lines("moved"))); });
  } else {
    marker.setLngLat(guess);
    const p = marker.getElement().firstChild; p.style.animation = "none"; void p.offsetWidth; p.style.animation = "";
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

// Send the pin. In Telegram the answer comes back with the result at once: the place, your score, and the others'
// answers without names until this chat's reveal. The site answers "accepted" and keeps the place for the evening.
async function send() {
  $("go").disabled = true;
  try {
    const at = guess.wrap();
    const res = await api("/api/guess", { lat: at.lat, lon: at.lng });
    if (res.place && Array.isArray(res.table)) toReveal(res);
    else {
      // Accepted without the result (the site, or a reply cut short): the answer is in all the same, and the live
      // result follows on the next poll.
      TODAY.answer = { lat: at.lat, lon: at.lng };
      toWaiting("sent");
    }
  } catch (e) {
    if (e.message === "already_answered") { TODAY = await api(todayPath()).catch(() => TODAY); restore(); return; }
    toast(errorText(e));
    if (e.message === "closed") toWaiting("closed");
  } finally { $("go").disabled = false; }
}
// The DM's zone follows the phone until the person picks one: the server takes it from here.
const todayPath = () => SITE ? "/api/today" : "/api/today?" + new URLSearchParams({ tz });
// "sent" right after the answer, "closed" when the window shut without one, "restore" when the app reopens after answering.
let waitShown = false;
function toWaiting(how) {
  state = "wait"; waitShown = true; if (marker) marker.setDraggable(false);
  if (how === "sent") haptic("success");
  // A pin that never reached the server is not an answer: it leaves the map, so nobody takes it for one.
  if (how === "closed" && !TODAY.answer && marker) { marker.remove(); marker = null; guess = null; }
  if (tgAt("6.2")) tg.disableClosingConfirmation();
  split(false); setCall(T(how === "closed" ? "closed" : "sent"), T("result_at", { time: atZone() }));
  $("timer").classList.add("hidden");
  // A browser in a zone maply does not name gets the time alone.
  overlay('<div class="pl nums"><b id="wleft">' + hm(secondsTo(TODAY.reveal)) + '</b><span class="up">' + esc(T("until_answer")) + '<br>' +
    esc(ZONE_NAME ? T("at", { time: AT, zone: ZONE_NAME }) : T("result_at", { time: atZone() })) + '</span></div>', tgLink());
  if (guess && view === "map") map.easeTo({ padding: bigCam().padding, center: guess, duration: 700, easing: easeOut });
}
// The app reopened after the answer: put the pin back where it was sent and show the result, live or final. The site
// waits for its evening.
function restore() {
  if (!TODAY.answer) return;
  guess = new maplibregl.LngLat(TODAY.answer.lon, TODAY.answer.lat);
  if (!marker) marker = new maplibregl.Marker({ element: el(PIN), anchor: "bottom" }).setLngLat(guess).addTo(map);
  else marker.setLngLat(guess);
  if (view === "photo") map.jumpTo({ ...miniCam(guess.lat), center: [guess.lng, miniLat(guess.lat)] });
  // Until the result comes, a tap on the map must not move the sent pin, and the caption already says the answer is in.
  if (SITE) toWaiting("restore");
  else { state = "wait"; marker.setDraggable(false); setCall(T("sent"), T("result_at", { time: atZone() })); loadResult(); }
}
// Result markers are laid out in pixels on every map-motion frame. A friend's point that falls under the target
// moves out onto a ring around it; nearby points are pushed apart; a label takes the first free spot, and if it
// ends up far away, a leader line is drawn to the point.
let marks = null, mine = null;
const RING = 27, GAP = 23, CLUSTER = 30;
// The angle between two points on the globe, in degrees.
function arc(a, b) {
  const r = Math.PI / 180, la1 = a.lat * r, la2 = b[1] * r, dl = (b[0] - a.lng) * r;
  return Math.acos(Math.max(-1, Math.min(1, Math.sin(la1) * Math.sin(la2) + Math.cos(la1) * Math.cos(la2) * Math.cos(dl)))) / r;
}
// A mark round the back of the globe would show through the land. MapLibre knows exactly where the horizon is at this
// zoom; when its internal method is gone, the angle from the centre is the guess: a little under 90°.
const HORIZON = 80;
function behind(at) {
  const T = map.transform;
  if (typeof T.isLocationOccluded === "function") { try { return T.isLocationOccluded(maplibregl.LngLat.convert(at)); } catch {} }
  return arc(map.getCenter(), at) >= HORIZON;
}
function layoutMarks() {
  // In the photo view the markers are hidden: nothing to lay out until the map opens.
  if (!marks || view !== "map") return;
  const T = map.project(marks.truth), G = guess ? map.project(guess) : { x: -1e4, y: -1e4 }, c = map.getContainer(), W = c.clientWidth;
  const rect = (x, y, w, h) => ({ x, y, w, h });
  const hit = (a, b) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;
  const front = (at) => !behind(at);
  marks.target.classList.toggle("far", !front([marks.truth.lng, marks.truth.lat]));
  marks.friends.forEach((f) => f.e.classList.toggle("far", !front(f.at)));
  const pts = groupMarks(marks.friends.filter((f) => front(f.at)).map((f) => ({ f, p: map.project(f.at) })))
    .map(({ f, p }) => ({ f, p, d: { x: p.x, y: p.y }, w: f.b.offsetWidth, h: f.b.offsetHeight }));
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
    // Your pin stands above its point: a mark under it could not be tapped, so it moves out sideways or below.
    if (guess) pts.forEach((a) => {
      const dx = a.d.x - G.x, dy = a.d.y - G.y, half = 30;
      if (dy <= -56 || dy >= 11 || Math.abs(dx) >= half) return;
      if (11 - dy < half - Math.abs(dx)) a.d.y = G.y + 11; else a.d.x = G.x + (dx < 0 ? -half : half);
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
const T_KM = .25, T_B = (k) => .75 + k * .3, ROWS = 5;
const pct = (x) => (x * 100 / (5000 + SPEED_MAX)).toFixed(2) + "%";
// rows: { who, d (km), pts, lvl, sp, me, grp, anon, rank } from the world's table in /api/result. The grey part of a bar
// is the points for the distance, the gold one after the bonuses is for speed. Before the reveal the others are a
// question mark without a name; the players of this group carry its mark by the face. fresh holds the rows that came in
// since the last paint; they slide in, the rest stay as they are.
// One row: place, face, name, the bar and the points. Your own row has every part in full colour and the bonuses you
// missed as dashed outlines; the others show only what they earned, in muted colours, so the eye finds yours first.
function boardRow(r, i, cls, count) {
  const IF = [T("what_continent"), T("what_country"), T("what_city")], v = (n) => count ? "0" : fmt(n);
  const b0 = r.pts - bonusOf(r.lvl) - r.sp, segs = ['<i class="km" data-tip="' + esc(T("km_points", { points: fmt(b0), km: kmT(r.d) })) + '" style="width:' + pct(b0) + ';--d:' + T_KM + 's"></i>'];
  BONUS.forEach((x, k) => {
    if (k < r.lvl) segs.push('<i data-tip="' + esc(T("bonus", { points: x, what: IF[k] })) + '" style="width:' + pct(x) + ';--d:' + T_B(k) + 's"></i>');
  });
  if (r.sp) segs.push('<i class="sp" data-tip="' + esc(T("speed_bonus", { points: r.sp })) + '" style="width:' + pct(r.sp) + ';--d:' + T_B(r.lvl) + 's"></i>');
  if (r.me) BONUS.forEach((x, k) => {
    if (k >= r.lvl) segs.push('<i class="if" data-tip="' + esc(T("bonus_missed", { points: x, what: IF[k] })) + '" style="width:' + pct(x) + ';--d:' + (T_B(r.lvl) + .1) + 's"></i>');
  });
  // Your own face shows your initial: the name beside it already says "you".
  const n = r.rank || i + 1, grp = r.grp ? '<b class="gi" title="' + esc(T("in_group")) + '">' + GROUP + '</b>' : "";
  const face = '<i class="fc">' + (r.anon ? '<i class="av anon">?</i>' : av(r.me ? r.name || "" : r.who, r.me, r.photo)) + grp + '</i>';
  // A hidden name in this group opens at the chat's reveal; anyone else's once the day is over in every zone.
  const wait = T(r.grp && PHASE === "live" ? "anon_row" : "anon_world");
  const label = r.anon ? ' aria-label="' + esc([wait, r.grp ? T("in_group") : "", kmT(r.d), fmt(r.pts)].filter(Boolean).join(", ")) + '"' : "";
  return '<div class="r' + (r.me ? " me" : "") + (r.anon ? " anon" : "") + cls + '" data-key="' + esc(r.key || "") + '"' + label + '><em' + (n <= 3 ? ' class="m' + n + '"' : "") + '>' + (typeof n === "number" ? fmt(n) : n) + '</em>' + face +
    '<span' + (r.handle && !r.anon ? ' data-who="' + esc(r.handle) + '"' + (count ? '' : ' role="button" tabindex="0"') : '') + '>' + (r.anon ? "•••" : esc(r.who)) + '</span>' +
    '<span class="hit" data-rest="' + esc(r.anon ? wait : T("short_of", { points: fmt(5000 - (r.pts - r.sp)) })) + '"><span class="bar">' + segs.join("") + '</span></span>' +
    '<b class="s" data-b="' + b0 + '" data-l="' + r.lvl + '" data-sp="' + r.sp + '">' + v(r.pts) + '</b><span class="k">' + kmT(r.d) + '</span></div>';
}
// Under the map the world's top five show, yours among them (the fifth gives way if you are lower), so the height does not
// change. "N more" opens every answer in a window of its own. Folded, the table keeps your row alone.
let boardRows = [], boardTotal = 0;
// A rank that skips ahead (4, then 60) gets a line above it: there are rows between them that do not show. A tie
// (3, 3, 5) is not a skip.
const skips = (rows, k) => k > 0 && rows[k].rank > rows[k - 1].rank + 1 && rows[k].rank > k + 1;
function paintBoard(rows, total, fresh) {
  boardRows = rows; boardTotal = Math.max(total || 0, rows.length);
  const meAt = rows.findIndex((r) => r.me), top = meAt >= ROWS ? ROWS - 1 : ROWS, IF = [T("what_continent"), T("what_country"), T("what_city")];
  // Only the rows that show are drawn: the best ones and yours. The rest are in the sheet.
  const shown = rows.map((r, i) => [r, i]).filter(([r, i]) => i < top || r.me), list = shown.map(([r]) => r);
  // Segment tooltips sit in data-tip, not data-t: data-t marks the static lines loadTexts fills.
  $("lb").innerHTML = shown.map(([r, i], k) => boardRow(r, i, (r.me || (meAt < 0 && !i) ? " keep" : "") + (skips(list, k) ? " skip" : "") +
    (fresh && fresh.has(r.key) ? " new" : ""), true)).join("");
  if ($("sheet").dataset.kind === "board" && $("sheet").classList.contains("on")) fillBoard();
  // How the points add up sits behind the (i) button, with the colours of the bars, and what the same distance would
  // have made with more bonuses.
  const me = rows[meAt], line = (c, h) => '<p><i class="' + c + '"></i><span>' + h + '</span></p>';
  $("none").classList.toggle("hidden", rows.length > 0);
  $("rulesBtn").classList.toggle("hidden", !rows.length);
  // "Only my place" needs a place of yours; without one the table stays open whatever was chosen before.
  const canFold = meAt >= 0 && rows.length > 1;
  $("foldBtn").classList.toggle("hidden", !canFold);
  foldBoard(canFold && foldNow, false);
  let h = line("km", esc(T("rules_km", { n: fmt(3500) }))) + line("", esc(T("rules_bonus", { a: BONUS[0], b: BONUS[1], c: BONUS[2] }))) +
    line("sp", esc(T("rules_speed", { n: SPEED_MAX })));
  // The two heads by a face are explained here: a title shows nowhere on a phone.
  if (rows.some((r) => r.grp)) h += '<p><b class="gi">' + GROUP + '</b><span>' + esc(T("in_group")) + '</span></p>';
  if (me) {
    const d = me.d, lvl = me.lvl, b = (n) => '<b>' + fmt(n) + '</b>';
    h += lvl >= 3 ? line("if", esc(T("all_bonuses"))) : line("if", esc(T("rules_if")) + ' ' + T("if_first", { what: esc(IF[lvl]), score: b(score(d, lvl + 1) + me.sp) }) +
      IF.slice(lvl + 1).map((n, j) => ', ' + T("if_next", { what: esc(n), score: b(score(d, lvl + j + 2) + me.sp) })).join(""));
  }
  $("rules").innerHTML = h;
  retip();
}
// "N more" counts what does not show: everything but the rows drawn, or but your own when folded.
function paintMore() {
  const n = boardTotal - ($("lb").classList.contains("min") ? 1 : $("lb").children.length);
  $("more").classList.toggle("hidden", n <= 0);
  $("more").textContent = T("more", { n: fmt(n) }) + " " + fwd();
}
// The (i) under the table opens how the points add up; a tap anywhere else closes it. The card's corner points at the
// button wherever it stands: next to the fold, or at the edge when there is none.
function showRules(on) {
  $("rules").classList.toggle("hidden", !on);
  $("rulesBtn").setAttribute("aria-expanded", on);
  if (!on) return;
  const b = $("rulesBtn").getBoundingClientRect(), r = $("rules").getBoundingClientRect();
  // The notch is placed from the card's end edge: the right one, or the left one in Arabic.
  const mid = b.left + b.width / 2, fromEnd = document.documentElement.dir === "rtl" ? mid - r.left : r.right - mid;
  $("rules").style.setProperty("--notch", Math.max(6, fromEnd - 5) + "px");
}
$("rulesBtn").addEventListener("click", () => { hideTip(); showRules($("rules").classList.contains("hidden")); });
// The fold keeps your own row only, and the choice stays for the next days.
const FOLDED = "maply.board.folded";
let FOLD_PREF = (() => { try { return !!localStorage.getItem(FOLDED); } catch { return false; } })();
// A phone turned sideways has 300–400 px of height: the open table and the place's plate would cover the whole map. There
// both start folded, without making it the choice for the next days; turned back upright, the choice is back.
const short = () => innerHeight < 500;
let foldNow = FOLD_PREF || short(), wasShort = short();
addEventListener("resize", () => {
  if (short() === wasShort) return;
  wasShort = short(); foldNow = FOLD_PREF || wasShort;
  if (!$("foldBtn").classList.contains("hidden")) foldBoard(foldNow, false);
  if (wasShort && $("place")) $("place").classList.add("min");
});
function foldBoard(on, save) {
  $("lb").classList.toggle("min", on);
  $("foldBtn").setAttribute("aria-expanded", !on);
  $("foldBtn").setAttribute("aria-label", T(on ? "board_unfold" : "board_fold"));
  if (save) { FOLD_PREF = foldNow = on; try { localStorage.setItem(FOLDED, on ? "1" : ""); } catch {} }
  paintMore();
}
$("foldBtn").addEventListener("click", () => { hideTip(); showRules(false); haptic("light"); foldBoard(!$("lb").classList.contains("min"), true); });
document.addEventListener("pointerdown", (e) => { if (!e.target.closest("#rules, #rulesBtn")) showRules(false); });
// Every answer, in a window over the app; a new answer while it is open joins the list where it stands.
function openBoard() {
  if (!boardRows.length) return;
  haptic("light"); hideTip(); showRules(false);
  sheet('<span id="boardH"></span>', '<div class="lb all" id="lbAll"></div>');
  $("sheet").dataset.kind = "board";
  fillBoard();
  // The sheet opens on your own row.
  const me = $("lbAll").querySelector(".r.me");
  if (me) me.scrollIntoView({ block: "center" });
}
// The app gets the best 200 and your group's rows, out of everyone who answered: the header says which it is.
function fillBoard() {
  // "Top N" counts the unbroken run from first place: your group's rows further down come after the dashed line.
  const gap = boardRows.findIndex((r, i) => skips(boardRows, i)), top = gap < 0 ? boardRows.length : gap;
  $("boardH").textContent = boardRows.length < boardTotal ? T("top_of", { n: fmt(top), total: fmt(boardTotal) }) : T("all_answers", { n: fmt(boardTotal) });
  $("lbAll").innerHTML = boardRows.map((r, i) => boardRow(r, i, skips(boardRows, i) ? " skip" : "", false)).join("");
}
$("more").addEventListener("click", openBoard);
// The plates over the map stay under the table and move down as it expands.
const ovUnderBoard = () => { $("ovTop").style.top = $("prog").getBoundingClientRect().top + $("prog").offsetHeight + 10 + "px"; };
// The header grows with Telegram's safe area in fullscreen: the plates follow it.
addEventListener("resize", () => { if (state === "reveal") requestAnimationFrame(ovUnderBoard); });
let progH = 0;
new ResizeObserver(() => {
  if (state !== "reveal") return;
  ovUnderBoard();
  const h = $("prog").offsetHeight, grew = h > progH + 4;
  progH = h;
  if (grew) keepInView();
}).observe($("prog"));
$("lb").addEventListener("click", (e) => {
  if (e.target.closest(".hit")) return;
  if ($("lb").classList.contains("min")) { haptic("light"); foldBoard(false, true); } else if (!$("more").classList.contains("hidden")) openBoard();
});
// Tooltip above the bar: which segment is under the finger or cursor. We look the segment up by x so it is easy to hit.
function showTip(e) {
  const hit = e.target.closest(".hit");
  if (!hit) return hideTip();
  const x = e.clientX, seg = [...hit.querySelectorAll("i")].find((g) => { const r = g.getBoundingClientRect(); return x >= r.left - 1 && x <= r.right + 1; });
  placeTip(hit, seg || null, x);
}
function placeTip(hit, seg, x) {
  const segs = [...hit.querySelectorAll("i")];
  document.querySelectorAll(".lb .lit").forEach((g) => g.classList.toggle("lit", g === seg));
  if (seg) seg.classList.add("lit");
  const tip = $("tip"), pr = $("prog").getBoundingClientRect(), rr = (seg || hit).getBoundingClientRect();
  tip.textContent = seg ? seg.dataset.tip : hit.dataset.rest;
  // Measured from the left edge: a long tip wraps to the width of the table, wherever it stood before.
  tip.style.left = "0px";
  const cx = seg ? (rr.left + rr.right) / 2 : x, w = tip.offsetWidth;
  tip.style.left = Math.max(4, Math.min(pr.width - w - 4, cx - pr.left - w / 2)) + "px";
  tip.style.top = hit.getBoundingClientRect().top - pr.top - tip.offsetHeight + 2 + "px";
  // The tip remembers its row, not its place: when a new answer moves the rows, it moves with its own.
  Object.assign(tip.dataset, { key: hit.closest(".r").dataset.key, seg: segs.indexOf(seg), x });
  tip.classList.add("on");
}
// After a repaint the tip goes back over the same row and segment, or away if the row is gone or folded.
function retip() {
  const tip = $("tip");
  if (!tip.classList.contains("on")) return;
  const row = [...$("lb").children].find((r) => r.dataset.key === tip.dataset.key), hit = row && row.offsetHeight && row.querySelector(".hit");
  if (!hit) return hideTip();
  const k = +tip.dataset.seg;
  placeTip(hit, k >= 0 ? hit.querySelectorAll("i")[k] || null : null, +tip.dataset.x);
}
function hideTip() { $("tip").classList.remove("on"); document.querySelectorAll(".lb .lit").forEach((g) => g.classList.remove("lit")); }
$("lb").addEventListener("pointermove", showTip);
$("lb").addEventListener("pointerdown", showTip);
$("lb").addEventListener("pointerleave", (e) => { if (e.pointerType === "mouse") hideTip(); });
document.addEventListener("pointerdown", (e) => { if (!e.target.closest(".lb")) hideTip(); });
// Points in the rows count up in time with the bars: every row on the first paint, only the new ones after it.
function countUp(fresh) {
  if (fresh) document.querySelectorAll("#lb .r:not(.new) .s").forEach((e) => {
    e.textContent = fmt(+e.dataset.b + bonusOf(+e.dataset.l) + +e.dataset.sp);
  });
  const t0 = performance.now(), ease = (x) => 1 - (1 - Math.min(1, Math.max(0, x))) ** 3, els = [...document.querySelectorAll(fresh ? "#lb .r.new .s" : "#lb .s")];
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
let resultLoading = false, failed = 0, retryAt = 0, lastPoll = 0;
// PHASE is "live" from the answer until this chat's reveal, with the others unnamed, then "final". VERSION changes when
// someone answers, so a poll that brings nothing new redraws nothing. WORLD_OPEN says the day has closed in every zone:
// until then a final result still waits for the rest of the world's names.
let PHASE = "", VERSION = "", WORLD_OPEN = false, seen = new Set(), seenB = new Set();
// The server stopped showing this day to this screen (a DM left open past the next day): the result stays as it is.
let GONE = false;
// A result the screen can be drawn from: a poll's "nothing new", or the place and the table.
const whole = (res) => res.same || (res.place && Array.isArray(res.table));
async function loadResult() {
  if (resultLoading || GONE || (state === "reveal" && PHASE !== "live" && (WORLD_OPEN || SITE)) || Date.now() < retryAt) return;
  resultLoading = true; lastPoll = Date.now();
  try {
    // A poll says which version it has: when nothing changed, the server answers in a few bytes. The date keeps a DM
    // on its day after midnight in its own zone, while one of the person's groups is still guessing it.
    const q = {};
    if (state === "reveal" && VERSION) q.since = VERSION;
    if (TODAY && TODAY.date) q.date = TODAY.date;
    const res = await api("/api/result?" + new URLSearchParams(q));
    if (!whole(res)) throw new Error("network");
    failed = 0; retryAt = 0;
    state === "reveal" ? repaint(res) : toReveal(res);
  } catch (e) {
    // The server's clock decides; it says when to ask again.
    if (state === "reveal" && e.message === "not_answered") GONE = true;
    else if ((e.message === "not_revealed" || e.message === "not_answered") && e.data && e.data.opens_at) TODAY.reveal = e.data.opens_at;
    else {
      // One toast, then the polls space out, up to a minute: a server that keeps failing must not flash the screen
      // every five seconds.
      failed++; retryAt = Date.now() + Math.min(60000, 5000 * 2 ** failed);
      if (failed === 1 && state !== "reveal") toast(errorText(e));
    }
    if (state !== "reveal" && TODAY.answer && !waitShown) toWaiting("restore");
  } finally { resultLoading = false; }
}
// While the names are hidden the result asks again every few seconds, so new answers appear as they come. It stops while
// the app is in the background and picks up again when it comes back.
const shown = () => !document.hidden && !(inTg && tg.isActive === false);
// Once the names are out, the world's still come later in the evening: a final result asks once a minute until then.
const slow = () => state === "reveal" && PHASE === "final";
const polling = () => !GONE && ((state === "reveal" && (PHASE === "live" || (PHASE === "final" && !WORLD_OPEN && !SITE))) ||
  (state === "wait" && !SITE && TODAY && TODAY.answer));
setInterval(() => { if (shown() && polling() && (!slow() || Date.now() - lastPoll > 60000)) loadResult(); }, 5000);
// Coming back to the app asks at once, even while a run of failures has the polls spaced out.
const back = () => {
  if (!shown()) return;
  tick(); retryLoading();
  if (polling()) { retryAt = 0; loadResult(); }
};
document.addEventListener("visibilitychange", back);
if (inTg && tg.onEvent) tg.onEvent("activated", back);
// The chat's answers, for the markers on the map. A marker's key is its pin: it stays the same when the names open, so only
// answers that are really new pop in.
const rowsOf = (res) => res.table.map((r) => {
  const anon = res.phase === "live" && !r.me;
  return { who: r.me ? T("you") : r.name, at: [r.lon, r.lat], d: r.km || 0, pts: r.points || 0, lvl: levelOf(r.bonus), sp: r.speed || 0,
    me: !!r.me, photo: r.photo, anon, key: r.key || r.lat.toFixed(5) + "," + r.lon.toFixed(5) };
}).sort((a, b) => b.pts - a.pts || a.d - b.d);
// The world's table, best first: everyone scored so far today, in the DM too. The server's key tells the rows apart
// between polls without saying whose they are, and says which names are not out yet. A site guest without a name reads
// "Guest" once the world's names are out.
function boardOf(res) {
  const rows = (res.board || []).map((r) => ({ who: r.me ? T("you") : r.name || (r.site ? T("guest") : ""), name: r.name || "",
    d: r.km || 0, pts: r.points || 0, lvl: levelOf(r.bonus), sp: r.speed || 0, me: !!r.me, grp: !!r.group, photo: r.photo,
    anon: !!r.hidden, key: r.key, rank: r.rank, handle: r.who || "" }));
  // An answer the site kept only in the browser is scored on the spot and has no place in the world: it shows as your
  // row without a rank, rather than "you didn't answer".
  const own = !rows.some((r) => r.me) && (res.table || []).find((r) => r.me && r.points != null);
  if (own) rows.push({ who: T("you"), name: "", d: own.km || 0, pts: own.points, lvl: levelOf(own.bonus), sp: own.speed || 0, me: true, key: own.key || "me", rank: "–" });
  return rows;
}
// The plate under the table: where you stand in the world. Until the day is over in every zone the place is "so far".
function livePlate(res) {
  const w = res.world, parts = [], open = (res.version || "").split(".")[3] === "true";
  if (w && w.rank) parts.push('<span>' + esc(T(open ? "world_final" : "world_now", { rank: fmt(w.rank), n: fmt(w.players) })) + '</span>');
  return parts.length ? '<div class="pl lv">' + parts.join("") + '</div>' : "";
}
function setLive(res) {
  PHASE = res.phase || "final"; VERSION = res.version || ""; WORLD_OPEN = VERSION.split(".")[3] === "true";
  if (res.opens_at) TODAY.reveal = res.opens_at;
  const top = livePlate(res);
  $("ovTop").innerHTML = top; $("ovTop").classList.toggle("on", !!top);
  // In the live phase the header counts down to the names.
  $("timer").classList.toggle("hidden", PHASE !== "live");
  $("timer").querySelector("span").textContent = T("until_names");
  tick();
}
// Everyone's pins worldwide, small and grey under the markers: where the world thinks it is, without anyone's name.
function drawWorld(w) {
  const data = { type: "FeatureCollection", features: ((w && w.pins) || []).map((p) => ({ type: "Feature", properties: {}, geometry: { type: "Point", coordinates: [p.lon, p.lat] } })) };
  const src = map.getSource("world");
  if (src) { src.setData(data); return; }
  map.addSource("world", { type: "geojson", data });
  map.addLayer({ id: "world", type: "circle", source: "world", paint: { "circle-radius": 3, "circle-color": "#B5C7BD", "circle-stroke-color": "#1A412B", "circle-stroke-width": 1 } });
}
// The others' markers. An unnamed one carries a question mark and its distance instead of a name. A marker lives as long
// as its answer: a new answer adds one, and at the reveal the names take the question marks' place in the same markers.
// Nothing under a finger is ever replaced, so a gesture that starts on a marker keeps going.
const friendAt = new Map();
function drawFriends(friends, fresh) {
  const keep = new Set(friends.map((f) => f.key));
  friendAt.forEach((it, k) => { if (!keep.has(k)) { it.m.remove(); friendAt.delete(k); } });
  marks.friends = friends.map((f) => {
    const name = f.anon ? kmT(f.d) : f.who, face0 = f.anon ? '<i class="av anon">?</i>' : av(f.who, false, f.photo);
    let it = friendAt.get(f.key);
    if (!it) {
      it = newFriend(f, fresh && fresh.has(f.key));
      friendAt.set(f.key, it);
    }
    // A changed face or name is drawn by groupMarks on the next layout, as a group or alone.
    if (it.name !== name || it.face0 !== face0) { it.name = name; it.face0 = face0; it.n = NaN; }
    return it;
  });
  layoutMarks();
}
function newFriend(f, fresh) {
  const e = el('<div class="friend' + (fresh ? " new" : "") + '"><span class="g"><s></s><i class="av"></i><b></b></span></div>');
  const m = new maplibregl.Marker({ element: e }).setLngLat(f.at).addTo(map);
  e.style.zIndex = 1;
  // The pop plays once: a marker that hides in a group and comes back does not pop again.
  e.addEventListener("animationend", () => e.classList.remove("new"), { once: true });
  const it = { m, at: f.at, e, g: e.firstChild, s: e.querySelector("s"), b: e.querySelector("b"), face: e.querySelector(".av"), n: NaN, group: null };
  // A tap on a group zooms in until it comes apart.
  it.g.addEventListener("click", (ev) => {
    if (!it.group || !it.group.length) return;
    ev.stopPropagation(); haptic("light");
    const b = new maplibregl.LngLatBounds(it.at, it.at);
    it.group.forEach((o) => b.extend(near180(o.at, it.at[0])));
    const cam = map.cameraForBounds(b, { padding: 70, maxZoom: Math.min(map.getZoom() + 4, 14) });
    // The player chose this view: a growing table does not take it back to the opening frame.
    userMoved = true;
    if (cam) map.easeTo({ center: cam.center, zoom: Math.max(cam.zoom, map.getZoom() + 1), duration: 650, easing: easeOut });
  });
  return it;
}
// Several answers within a finger's width of each other become one marker: the best of them, with how many more stand
// behind it. Which ones group changes with the zoom.
function groupMarks(all) {
  const leads = [];
  all.forEach((a) => {
    const host = leads.find((l) => Math.hypot(l.p.x - a.p.x, l.p.y - a.p.y) < CLUSTER);
    if (host) host.f.group.push(a.f); else { a.f.group = []; leads.push(a); }
  });
  all.forEach((a) => {
    const f = a.f, lead = leads.includes(a), n = lead ? f.group.length : -1;
    if (!lead) f.group = null;
    if (n === f.n) return;
    f.n = n;
    f.e.classList.toggle("hid", !lead);
    f.e.classList.toggle("grp", n > 0);
    if (n < 0) return;
    f.face.outerHTML = n > 0 ? '<i class="av grp">' + (n + 1) + '</i>' : f.face0;
    f.face = f.g.querySelector(".av");
    // The name is isolated, so in Arabic "+3" stays after it and does not mix with its digits.
    f.b.textContent = n > 0 ? "⁨" + f.name + "⁩ +" + n : f.name;
  });
  return leads;
}
// A later look at the live result: new answers slide in, and at the reveal the names take the question marks' place.
function repaint(res) {
  const was = PHASE;
  if (res.opens_at) TODAY.reveal = res.opens_at;
  if (res.same || (res.version && res.version === VERSION)) return;
  // The version is "group answers.world players.phase": the table follows the world, the markers only this chat.
  const [g0, , p0] = VERSION.split("."), [g1, , p1] = (res.version || "").split(".");
  const board = boardOf(res), freshB = new Set(board.filter((r) => !seenB.has(r.key)).map((r) => r.key));
  board.forEach((r) => seenB.add(r.key));
  paintBoard(board, res.players, freshB); countUp(freshB);
  drawWorld(res.world);
  if (res.version && g0 === g1 && p0 === p1) { setLive(res); return; }
  const rows = rowsOf(res), fresh = new Set(rows.filter((r) => !seen.has(r.key)).map((r) => r.key));
  rows.forEach((r) => seen.add(r.key));
  mine = rows.find((r) => r.me) || mine;
  marks.rows = rows.filter((r) => !r.me);
  drawFriends(marks.rows, fresh);
  setLive(res);
  if (was === "live" && PHASE === "final") { toast(T("names_open")); haptic("success"); }
  else if (fresh.size) haptic("light");
}
function toReveal(res) {
  if (state === "reveal") return;
  state = "reveal";
  // The table below is drawn before setLive: its hidden rows say when their names come by the phase.
  PHASE = res.phase || "final";
  // The answer is in: closing the app loses nothing, so Telegram must not ask.
  if (tgAt("6.2")) tg.disableClosingConfirmation();
  const P = res.place, truth = new maplibregl.LngLat(P.lon, P.lat);
  credits(P);
  const rows = rowsOf(res);
  seen = new Set(rows.map((r) => r.key));
  mine = rows.find((r) => r.me) || null;
  // A pin that never reached the server is not an answer: it leaves the map rather than draw a line to the place.
  if (!mine && marker) { marker.remove(); marker = null; guess = null; }
  if (mine) guess = new maplibregl.LngLat(mine.at[0], mine.at[1]);
  if (marker && guess) marker.setLngLat(guess);
  if (guess && !marker) marker = new maplibregl.Marker({ element: el(PIN), anchor: "bottom" }).setLngLat(guess).addTo(map);
  if (marker) marker.setDraggable(false);
  const friends = rows.filter((r) => !r.me), board = boardOf(res);
  seenB = new Set(board.map((r) => r.key));
  paintBoard(board, res.players);
  $("lb").offsetWidth; // the bar and avatars must catch the closed state, otherwise there will be no animation
  document.body.classList.add("is-reveal");
  ovUnderBoard();
  drawWorld(res.world);
  // Layer order: the world's pins, friends, the target above them, your own pin on top.
  marks = { truth, friends: [], rows: friends, target: el(TARGET) };
  drawFriends(friends, null);
  new maplibregl.Marker({ element: marks.target }).setLngLat(truth).addTo(map).getElement().style.zIndex = 2;
  if (marker) marker.getElement().style.zIndex = 3;
  layoutMarks();
  if (guess) {
    const line = { type: "Feature", geometry: { type: "LineString", coordinates: [near180([guess.lng, guess.lat], truth.lng), [truth.lng, truth.lat]] } };
    map.addSource("line", { type: "geojson", data: line });
    map.addLayer({ id: "line-case", type: "line", source: "line", paint: { "line-color": "#FFFFFF", "line-width": 5 } });
    map.addLayer({ id: "line", type: "line", source: "line", paint: { "line-color": "#1A412B", "line-width": 2, "line-dasharray": [2, 1.5] } });
  }
  split(!!mine); $("go").textContent = T("share");
  setCall(T("day_result"), T("see"));
  const where = esc([P.city, regionName(P.country)].filter(Boolean).join(", ")), fact = esc(P.fact || "");
  overlay("", tgLink() +
    '<div class="place up" id="place"><div class="t">' + where + (fact ? '<p>' + fact + '</p>' : '') + '</div>' +
    '<button class="x" id="placeX" aria-label="' + esc(T("aria_collapse")) + '" aria-expanded="true">' + FOLD + '</button>' +
    '<button class="tab" id="placeTab" aria-expanded="false">' + esc(T("about_place")) + ' ↑</button></div>');
  countUp();
  // The plate opens with the place and its fact. Folding it leaves a small tab, so it does not cover the map on a phone.
  const fold = (on) => { $("place").classList.toggle("min", on); (on ? $("placeTab") : $("placeX")).focus({ preventScroll: true }); };
  $("placeX").onclick = () => fold(true);
  $("placeTab").onclick = () => fold(false);
  // On a short screen the plate starts folded: open, it would leave the map a strip between the table and the buttons.
  if (innerHeight < 640) $("place").classList.add("min");
  setLive(res);
  view = "map"; document.body.classList.add("is-map"); paintBackdrop(); syncBack();
  frameResult(1200);
  haptic("success");
}
// A longitude moved by whole turns to within half a turn of from's: two points either side of the 180th meridian frame
// and join the short way round, not across the rest of the world.
const lngNear = (lng, from) => from + ((((lng - from) % 360) + 540) % 360) - 180;
const near180 = (at, from) => [lngNear(at[0], from), at[1]];
// The opening frame: the place, your pin and the answers near them. An answer from the other side of the world would
// zoom out to the whole globe and squeeze the two that matter into one dot, or turn them to its back.
function frameResult(duration) {
  const { W, H } = box(), { t, b } = reserve(), c = marks.truth.lng, pts = [[c, marks.truth.lat]];
  if (guess) pts.push(near180([guess.lng, guess.lat], c));
  const ds = marks.rows.map((f) => f.d).sort((x, y) => x - y);
  const near = Math.min(3000, Math.max(800, mine ? mine.d * 1.5 : ds[Math.min(2, ds.length - 1)] || 0));
  marks.rows.forEach((f) => { if (f.d <= near) pts.push(near180(f.at, c)); });
  const bounds = new maplibregl.LngLatBounds(pts[0], pts[0]);
  pts.forEach((p) => bounds.extend(p));
  // Your pin stands above its point. On a short screen the margins shrink together, so the map keeps some room: with
  // margins larger than the screen MapLibre does not move at all.
  let top = t + 24 + (guess ? 46 : 0), bottom = b + 24;
  const side = Math.min(40, W / 8), room = H - 120;
  if (top + bottom > room) { const k = room / (top + bottom); top *= k; bottom *= k; }
  map.setPadding({ left: 0, top: 0, right: 0, bottom: 0 });
  userMoved = false;
  map.fitBounds(bounds, { padding: { top, left: side, right: side, bottom }, maxZoom: 9, duration });
}
// When the table grows over the place or your pin, the camera moves to show them again. Not under a finger or in flight:
// then it waits until the map stands still. Once the player has moved the map, the view is theirs: a point they took
// off the screen is not "under the table".
let viewDue = false, userMoved = false;
map.on("movestart", (e) => { if (e.originalEvent) userMoved = true; });
function keepInView() {
  if (state !== "reveal" || view !== "map" || !marks || userMoved) return;
  if (touches || map.isMoving()) { viewDue = true; return; }
  viewDue = false;
  const { t, b } = reserve(), { W, H } = box();
  const under = (ll, above) => {
    const p = map.project(ll);
    return p.x >= 0 && p.x <= W && ((p.y >= 0 && p.y - above < t) || (p.y > H - b && p.y <= H));
  };
  if (under(marks.truth, 15) || (guess && under(guess, 46))) frameResult(600);
}
map.on("moveend", () => { if (viewDue) keepInView(); });
$("go").addEventListener("click", () => {
  if (state === "guess" && guess) send();
  else if (state === "reveal") share();
});
// Forwarding the result. In Telegram 8.0 and later the server prepares the result card, a map picture with a Play button,
// and shareMessage opens Telegram's chat picker with it. If the card cannot be made (no renderer, a slow one, an old client,
// the picker refusing it), a line with the day number, distance and score goes through t.me/share. In a browser it is
// the system "Share" menu or the clipboard.
const SHARE_URL = window.MAPLY_SHARE_URL || location.origin;
// What the player shares lands next to the group's posts, so it is in the group's language even when the screen speaks
// the player's own pick. The server names that language when the two differ; its words are loaded ahead, since the
// share has to go out within the tap.
let SHARE_TX = null;
function loadShareTexts() {
  const lang = TODAY && TODAY.share_lang;
  if (!lang || lang === LANG || (SHARE_TX && SHARE_TX.lang === lang)) return;
  get("/texts/" + lang + ".json").then(reply).then((d) => { if (d && d.lang === lang) SHARE_TX = d; }).catch(() => {});
}
// The same lines as the message the bot prepares: distance and points apart, so neither is read as the other.
const shareLine = () => {
  const g = TODAY.share_lang && SHARE_TX && SHARE_TX.lang === TODAY.share_lang ? SHARE_TX : null;
  const tx = g ? g.t : TX, lang = g ? g.lang : LANG;
  return "maply #" + TODAY.day + (mine ? "\n📍 " + textIn(tx, "share_distance", { km: kmIn(tx, lang, mine.d) }) +
    "\n⭐ " + textIn(tx, "share_points", { points: numIn(lang, mine.pts) }) + "\n\n" + textIn(tx, "share_ask") : "");
};
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
    // The card can take half a minute to draw.
    const { id } = await api("/api/share", {}, 45000);
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
// A distance as the bot writes it: metres under a kilometre, one decimal under ten.
const kmIn = (tx, lang, km) => km < 1 ? textIn(tx, "unit.m", { m: numIn(lang, km * 1000) })
  : textIn(tx, "unit.km", { km: numIn(lang, km, km >= 10 ? 0 : 1) });
const kmT = (km) => kmIn(TX, LANG, km);
const dayDate = (iso) => { try { return new Intl.DateTimeFormat(numLang(), { day: "numeric", month: "short", timeZone: "UTC" }).format(new Date(iso + "T00:00:00Z")); } catch { return iso; } };
// clock is a time in the zone the app shows today's reveal in, with that
// zone's name, so it reads the same as the reveal line.
const clock = (iso) => {
  let t;
  try { t = new Intl.DateTimeFormat(numLang(), { timeZone: TODAY && TODAY.zone || undefined, hour: "2-digit", minute: "2-digit", hourCycle: "h23" }).format(new Date(iso)); } catch { return ""; }
  return withZone(t);
};
const nameOf = (r) => r.name || (r.site ? T("guest") : "?");
// Sofia Sans draws Cyrillic in its Bulgarian forms unless told the language. A name can be in any script; in an interface
// that is not Cyrillic, Russian forms are the likelier right ones for a Cyrillic name.
const nameLang = () => ["ru", "uk", "be"].includes(LANG) ? LANG : "ru";
async function getJSON(path) {
  if (SITE) return reply(await get(path + (path.includes("?") ? "&" : "?") + "lang=" + encodeURIComponent(LANG)));
  return reply(await get(path, { headers: { Authorization: "tma " + tg.initData } }));
}
// The sheet is a dialog over the app: a screen reader announces its title and starts on its close button.
// SHEET counts what the sheet shows: a reply that comes back after the sheet moved on or closed is dropped.
let SHEET = 0, sheetFrom = null;
function sheet(title, html, back, close) {
  const sh = $("sheet");
  // Closing gives the focus back to what opened the sheet.
  if (!sh.classList.contains("on")) sheetFrom = document.activeElement;
  SHEET++;
  sh.innerHTML = '<div class="sh-top"><button class="sh-b" id="shBack" aria-label="' + esc(T("aria_collapse")) + '">' + (back ? (fwd() === "→" ? "←" : "→") : "×") + '</button><b id="shTitle">' + title + '</b></div><div class="sh-body">' + html + '</div>';
  sh.dataset.kind = ""; sh.classList.add("on"); document.body.classList.add("is-sheet"); syncBack(); behindSheet(true);
  $("shBack").onclick = back || close || closeDays;
  $("shBack").focus({ preventScroll: true });
  return SHEET;
}
// While the sheet is open the page behind it is out of reach: Tab and a screen reader stay in the dialog.
function behindSheet(on) {
  for (const e of document.body.children) if (e.id !== "sheet" && e.id !== "toast" && e.tagName !== "SCRIPT") e.inert = on;
}
function closeDays() {
  SHEET++;
  $("sheet").classList.remove("on"); document.body.classList.remove("is-sheet"); syncBack(); behindSheet(false);
  if (sheetFrom && document.contains(sheetFrom)) sheetFrom.focus({ preventScroll: true });
  sheetFrom = null;
}
// Escape is the sheet's own back button: from a day to the days, from the days out.
addEventListener("keydown", (e) => { if (e.key === "Escape" && $("sheet").classList.contains("on")) { e.preventDefault(); $("shBack").click(); } });
// The days are the page /days and a day is /days/<date>: closing the sheet goes back to the page before it.
const leaveDays = () => Router.back("/");
async function openDays() {
  haptic("light");
  const ticket = sheet(esc(T("days")), '<p class="sh-wait up">' + esc(lines("loading")[0]) + '</p>', null, leaveDays);
  let data;
  try { data = await getJSON("/api/days"); } catch (e) { if (ticket === SHEET) sheet(esc(T("days")), '<p class="sh-wait">' + esc(errorText(e)) + '</p>', null, leaveDays); return; }
  if (ticket !== SHEET) return;
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
    let rows = '<p>' + esc(T("day_world", { n: fmt(d.players), km: d.median_km != null ? kmT(d.median_km) : "…", pct: d.country_pct })) + '</p>';
    if (d.group) rows += '<p>' + esc(T("day_group", { n: fmt(d.group.answered), total: fmt(d.group.members) })) + (d.group.best_name ? ' · <span lang="' + nameLang() + '">' + esc(d.group.best_name) + "</span>, " + kmT(d.group.best_km) : "") + '</p>';
    if (d.me) rows += '<p class="me">' + esc(T("day_me", { rank: fmt(d.me.rank), points: fmt(d.me.points) })) + '</p>';
    html += '<button class="dd" data-date="' + esc(d.date) + '">' + head + '<b>' + esc(where) + '</b>' + rows + '<em>' + fwd() + '</em></button>';
  }
  DAYS = { html, scroll: 0 };
  showDays();
}
// The days as they were drawn last: Back from a day comes back to them where they were scrolled, without asking again.
let DAYS = null;
function showDays() {
  sheet(esc(T("days")), DAYS.html, null, leaveDays);
  const body = $("sheet").querySelector(".sh-body");
  body.scrollTop = DAYS.scroll;
  $("sheet").querySelectorAll(".dd[data-date]").forEach((b) => { b.onclick = () => { DAYS.scroll = body.scrollTop; Router.go("/days/" + b.dataset.date); }; });
  const name = $("shName");
  if (name) name.value = siteName();
  if (name) name.onchange = async () => {
    try {
      localStorage.setItem(NAME, name.value.trim());
      await reply(await get("/api/site/name", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name: name.value }) }));
      toast(T("name_saved"));
    } catch (e) { toast(errorText(e)); }
  };
}
// A later tap wins over an earlier one whose reply comes last; the tapped day says it is on its way. A day opened by its
// address has no list under it: the sheet waits for it on its own, and Back goes to the list.
async function openDay(date) {
  haptic("select");
  const btn = $("sheet").classList.contains("on") && [...$("sheet").querySelectorAll(".dd[data-date]")].find((b) => b.dataset.date === date);
  const toDays = () => Router.back("/days");
  const ticket = btn ? ++SHEET : sheet(esc(T("days")), '<p class="sh-wait up">' + esc(lines("loading")[0]) + '</p>', toDays);
  $("sheet").querySelectorAll(".dd.busy").forEach((b) => { b.classList.remove("busy"); b.removeAttribute("aria-busy"); });
  if (btn) { btn.classList.add("busy"); btn.setAttribute("aria-busy", "true"); }
  let d;
  try { d = await getJSON("/api/day?date=" + encodeURIComponent(date)); } catch (e) {
    if (ticket !== SHEET) return;
    // The list stays, and so does its address.
    if (btn) { toast(errorText(e)); btn.classList.remove("busy"); btn.removeAttribute("aria-busy"); Router.go("/days", { replace: true }); }
    else sheet(esc(T("days")), '<p class="sh-wait">' + esc(errorText(e)) + '</p>', toDays);
    return;
  }
  if (ticket !== SHEET) return;
  const P = d.place, where = [P.city, regionName(P.country)].filter(Boolean).join(", ");
  let html = '<p class="sh-sub">' + esc(T("day_world", { n: fmt(d.players), km: d.median_km != null ? kmT(d.median_km) : "…", pct: d.country_pct })) + '</p><ol class="rk">';
  for (const r of d.ranking) {
    const name = nameOf(r);
    html += '<li class="' + (r.me ? "me" : "") + '"><span class="n">' + fmt(r.rank) + '</span><span class="a">' + av(name) + (r.site ? SRC_SITE : SRC_TG) +
      '</span><span class="w" lang="' + nameLang() + '"' + (r.who ? ' data-who="' + esc(r.who) + '" role="button" tabindex="0"' : '') + '>' + esc(name) + (r.me ? ' <em>· ' + esc(T("you")) + '</em>' : '') + '</span><span class="k">' + kmT(r.km) + '</span><b>' + fmt(r.points) + '</b></li>';
  }
  html += '</ol><p class="sh-legend"><span>' + SRC_TG + esc(T("from_tg")) + '</span><span>' + SRC_SITE + esc(T("from_site")) + '</span></p>';
  sheet("#" + d.day + " · " + esc(where), html, toDays);
}
// The game is the page under every other: today has nothing of its own to open or close. Until days.js takes /days
// over, the days are the sheet above; Back from a day comes back to the list as it was scrolled, without asking again.
Router.screen("today", /^\/(?:index\.html)?$/, { open() {} });
let dayShown = false;
Router.screen("days", /^\/days(?:\/(\d{4}-\d{2}-\d{2}))?$/, {
  open(m) {
    if (m[1]) openDay(m[1]);
    else if (dayShown && DAYS) showDays();
    else openDays();
    dayShown = !!m[1];
  },
  close() { dayShown = false; closeDays(); },
});

/* ---------- Start ---------- */
// Opened inside Telegram without initData (a plain link in the in-app browser) there is nothing to play as.
async function boot() {
  // The client's language first, so nothing on the first screen is blank; the person's own choice from core
  // arrives with /api/today and wins.
  const [today] = await Promise.all([
    SITE || tg.initData ? api(todayPath()).catch((e) => e) : null,
    loadTexts(LANG0).catch(() => {}),
  ]);
  setCall(T("where"), T("drop_pin"));
  if (!today) { stopLoading(T("open_in_telegram")); return; }
  if (today instanceof Error) { failLoading(errorText(today), boot); return; }
  TODAY = today;
  if (TODAY.now) SKEW = Date.parse(TODAY.now) - Date.now();
  // The texts are asked for again when they did not come with the first try, even in the same language.
  if (TODAY.lang && (TODAY.lang !== LANG || !textsLoaded)) { await loadTexts(TODAY.lang).catch(() => {}); setCall(T("where"), T("drop_pin")); }
  // The words are final now: the page the address names can open.
  Router.start();
  loadShareTexts();
  $("day").textContent = "#" + TODAY.day;
  HEADING = TODAY.heading || 0;
  AT = new Intl.DateTimeFormat(numLang(), { timeZone: TODAY.zone, hour: "2-digit", minute: "2-digit", hourCycle: "h23" }).format(new Date(TODAY.reveal));
  ZONE_NAME = ZONES[TODAY.zone] || "";
  startPano();
  // Someone who has answered is told so at once, not asked to drop a pin while the map loads.
  if (TODAY.answer) setCall(T("sent"), T("result_at", { time: atZone() }));
  // The answer and the result need the map's style: markers and the line are added to it.
  await new Promise((r) => whenMapReady(r));
  if (TODAY.answer) { restore(); hideLoader(); }
  else if (!secondsTo(TODAY.deadline)) toWaiting("closed");
  tick();
  if (!secondsTo(TODAY.reveal)) loadResult();
  if (TODAY.zone_set) setTimeout(() => toast(T("zone_set", { zone: ZONES[TODAY.zone_set] || TODAY.zone_set })), 1200);
}
boot();
