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
// A chevron pointing down: the plate folds away to the bottom edge.
const FOLD = '<svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true"><path d="M3 6l5 5 5-5" fill="none" stroke="currentColor" stroke-width="2"/></svg>';
// The group's mark by a face in the world's table: two heads.
const GROUP = '<svg viewBox="0 0 12 12" aria-hidden="true"><circle cx="4.3" cy="3.9" r="2"/><circle cx="8.6" cy="4.5" r="1.6"/>' +
  '<path d="M.6 10.8c0-2.2 1.6-3.6 3.7-3.6s3.7 1.4 3.7 3.6zM8.9 10.8c0-1.4-.4-2.5-1.2-3.2.3-.1.6-.1.9-.1 1.7 0 2.9 1.2 2.9 3.3z"/></svg>';
const pick = (a) => a[Math.floor(Math.random() * a.length)];
// Every player on every screen is drawn by faceOf and named by nameOf, from a person as the API sends one (name, photo,
// me, site, anon, hidden; internal/people): the server has already decided what this viewer may see of them. A row
// whose name is not out yet is a question mark, a player who hid themselves ("Anonymous") the plain figure, a Telegram
// player their photo, anyone else their first letter. cls adds classes to the square.
// A photo is a kept picture at /face/<key>.jpg; anything else keeps the letter.
const PHOTO = /^\/face\/[0-9a-f]{32}\.jpg$/;
// MY_FACE is the person's own Telegram photo: their own face is it wherever it is drawn, the letter until it comes.
let MY_FACE = "";
const FIGURE = '<svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true"><circle cx="8" cy="5.2" r="3" fill="currentColor"/><path d="M2 15c0-3.4 2.7-5.6 6-5.6s6 2.2 6 5.6z" fill="currentColor"/></svg>';
function faceOf(p, cls) {
  p = p || {};
  const c = "av" + (p.me ? " me" : "") + (cls ? " " + cls : "");
  if (p.hidden) return '<i class="' + c + ' anon">?</i>';
  if (p.anon && !p.me) return '<i class="' + c + ' nobody">' + FIGURE + '</i>';
  const photo = p.photo || (p.me ? MY_FACE : "");
  if (photo && PHOTO.test(photo)) return '<i class="' + c + ' ph"><img src="' + photo + '" alt=""></i>';
  return '<i class="' + c + '">' + esc(initial(nameOf(p))) + '</i>';
}
// A player's name: their own, "Anonymous" as the server wrote it, "Guest" for a site player without one; nothing while
// it is not out yet.
const nameOf = (p) => p.hidden ? "" : p.name || (p.site ? T("guest") : "?");
// The person of a row the API sent, without the rest of the row.
const personOf = (r) => ({ name: r.name || "", photo: r.photo || "", me: !!r.me, site: !!r.site, anon: !!r.anon, hidden: !!r.hidden });
// The own photo is asked for once a launch, beside everything else; the faces already drawn take it when it comes.
function loadFace() {
  const site = typeof ME !== "undefined" && ME && ME.account && ME.account.telegram;
  if (inTg ? !tg.initData : !site) return;
  getJSON("/api/face").then((d) => {
    if (!d || !d.photo || !PHOTO.test(d.photo)) return;
    MY_FACE = d.photo;
    document.querySelectorAll("i.av.me:not(.ph):not(.anon)").forEach((el) => { el.classList.add("ph"); el.innerHTML = '<img src="' + MY_FACE + '" alt="">'; });
    if (typeof meLetter === "function") meLetter();
  }).catch(() => {});
}
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
  pano.relabel();
  globe.language(LANG);
}

/* ---------- Server ---------- */
// In Telegram every request carries initData; the server checks its signature and works out the chat from
// start_param. In a plain browser it is the site: nobody signs in, a cookie holds a random player id, the one answer
// a day goes into the world ranking under the name the player gave or "Guest", and the place opens only after the
// latest reveal among maply's zones. localStorage keeps a copy of the pin and the name. A browser signed in with
// Telegram (account.js) is no such site: it plays as the bot's player, with its session cookie instead of initData.
let SITE = !inTg;
async function api(path, body, wait) {
  if (SITE) return site(path, body);
  const headers = inTg ? { Authorization: "tma " + tg.initData } : {};
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
// Signed in with Telegram, the plate offers the bot's daily post instead.
const tgLink = () => !inTg && TODAY && TODAY.bot
  ? '<a class="pl w tgl" href="https://t.me/' + encodeURIComponent(TODAY.bot) + '" target="_blank" rel="noopener">' + esc(T(SITE ? "play_telegram" : "acct.get_post")) + ' ' + fwd() + '</a>' : "";
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
// The logo opens the menu, on a phone as on a computer.
$("logo").addEventListener("click", () => Router.go("/"));

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

/* ---------- Panorama ---------- */
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
// pano.js draws the picture and the compass tape. The viewer is shown once /api/today says which picture it is; until
// then everything that turns it waits. A picture that did not come is asked for again a few times; until it comes the
// loading screen stays only for someone who still has to answer: who has answered plays on the map.
const pano = createPano({ el: $("pano"), compass: $("compass"), tape: $("tape"), label: (k) => TX["compass_" + k] || "", haptic });
pano.on("load", hideLoader);
pano.on("error", (retry) => { if (!TODAY.answer) failLoading(T("no_image"), retry); else toast(T("no_image")); });
pano.on("touch", () => $("hint").classList.add("gone"));

/* ---------- Two views: photo with a mini globe, and the big map ---------- */
let state = "guess", view = "photo";
// The page's styles and the computer's layout follow the state: the side panel takes the plates once you answered.
function setState(s) { state = s; document.body.dataset.state = s; Desk.place(); seePano(); }
document.body.dataset.state = state;
const MINI = 136, ACT = 82, TAPE_H = 38; // TAPE_H is the height of the compass tape at the bottom (--tape)
// The small globe sits under its button, which keeps clear of a notch on the side; before layout, where it would be.
// Its size is the button's, which the layout sets.
function miniAt() {
  const g = $("globeHit").getBoundingClientRect(), m = $("map").getBoundingClientRect(), W = $("map").clientWidth, H = $("map").clientHeight;
  return { x: g.width ? g.left - m.left + g.width / 2 : W - 12 - MINI / 2, y: g.height ? g.top - m.top + g.height / 2 : H - TAPE_H - 16 - MINI / 2, d: (g.width || MINI) - 4 };
}
// How much space the plates take at top and bottom: the camera keeps the globe and markers in the free part.
function reserve() {
  // On a computer the plates are in the side panel: only the answer's bar lies over the bottom of the map's panel.
  if (Desk.on) return { top: 0, bottom: state === "guess" ? $("act").offsetHeight : 0 };
  const top = $("map").getBoundingClientRect().top, on = (id) => $(id).classList.contains("on");
  // On the result there is also the day's table at the top. We measure by the header, not the table itself: at that moment it is still expanding.
  const board = document.body.classList.contains("is-reveal") ? $("prog").parentElement.getBoundingClientRect().bottom + $("prog").offsetHeight - top + 8 : 0;
  const t = Math.max(board, on("ovTop") ? $("ovTop").getBoundingClientRect().bottom - top + 8 : 0);
  const b = (on("ovBot") ? $("ovBot").offsetHeight + 20 : 0) + ACT; // the bottom strip is always present in map mode
  return { top: t, bottom: b };
}
function overlay(top, bot) {
  $("ovTop").innerHTML = top; $("ovBot").innerHTML = bot;
  $("ovTop").classList.toggle("on", !!top); $("ovBot").classList.toggle("on", !!bot);
}
// The map covers the photo: the tape stops following it. On a computer the panel leaves the panorama in sight while
// you guess. Folded, the map is out of the tab order: its canvas would take the focus and the arrows.
const panoSeen = () => view === "photo" || (Desk.on && state === "guess");
// The panorama keeps still and out of the Tab order while the map covers it.
function seePano() { const s = panoSeen(); pano.still(!s); $("pano").inert = !s; }
function toMap() {
  if (view === "map") return;
  view = "map"; document.body.classList.add("is-map"); haptic("light"); syncBack(); seePano();
  // Open, but not reachable under a sheet or the menu.
  behindSheet(); lookLabel();
  globe.toBig();
}
function toPhoto() {
  if (view === "photo") return;
  // The focus on the folded map goes to the globe that opens it, not to the page: a focus the script then moves from
  // nowhere would show its ring after a tap.
  const inMap = $("map").contains(document.activeElement);
  view = "photo"; document.body.classList.remove("is-map"); haptic("soft"); syncBack(); seePano();
  $("map").inert = true; lookLabel(); Desk.cross();
  if (inMap) $("globeHit").focus({ preventScroll: true });
  globe.toMini(650);
}
$("map").inert = true;
$("globeHit").addEventListener("click", toMap);
// On a phone the button shows only over the map; in the computer's side panel it swaps the map and the panorama.
$("look").addEventListener("click", () => view === "map" ? toPhoto() : toMap());
// Over the free game the Back button is the free game's own. In the day it goes from the map to the photo, and from
// the photo to the menu; on any other page to the page before.
const mapBack = () => view === "map" && !(Desk.on && state === "guess");
if (tgAt("6.1")) tg.BackButton.onClick(() => document.body.classList.contains("is-free") ? null : document.body.classList.contains("is-sheet") ? $("shBack").click()
  : Router.current === "today" && mapBack() ? toPhoto() : Router.back("/"));
// Splits the bottom strip into two buttons or merges it back into one.
// The left button label depends on the stage: before the answer it invites a closer look, after the answer simply to enjoy.
const LOOK = { guess: ["look_more", "look"], wait: ["admire", "admire"], reveal: ["walk", "walk"] };
// Folded, the main button is out of sight, so it is out of the tab order and the screen reader's reach too. In the
// computer's panel the answer's bar is always there: dimmed until the pin, then the answer with its key.
function split(on) {
  const bar = Desk.on && state === "guess";
  $("act").classList.toggle("on", on); lookLabel();
  if (state === "guess") $("go").textContent = T(bar ? (on ? (Desk.keyboard ? "desk.answer_key" : "final") : "desk.pin_first") : "final");
  $("go").disabled = bar && !on;
  $("go").tabIndex = on || bar ? 0 : -1; $("go").toggleAttribute("aria-hidden", !on && !bar);
}
// In the side panel the map can give way to the panorama: then the same button brings the map back.
function lookLabel() {
  const on = $("act").classList.contains("on");
  $("look").textContent = Desk.on && state !== "guess" && view === "photo" ? T("desk.to_map") : T(LOOK[state][on ? 1 : 0]);
}
// Arrows point the way the text reads: the other way round in Arabic.
function fwd() { return document.documentElement.dir === "rtl" ? "←" : "→"; }
// The caption's arrow points at the mini globe, which stays in the right corner whichever way the text reads: in Arabic it
// comes first, which is the right-hand end.
function setCall(b, sub) {
  $("callB").textContent = b;
  $("callS").innerHTML = document.documentElement.dir === "rtl" ? "<em>→</em> " + esc(sub) : esc(sub) + " <em>→</em>";
}

// globe.js draws the globe, the pin and the answer; the layout above says where the small globe stands and what the
// plates cover. The small globe turns only once the loading screen is gone, and not under a page or free play.
const globe = createGlobe({ el: $("map"), lang: LANG0, mini: miniAt, insets: reserve, haptic,
  spin: () => $("loader").classList.contains("gone") && !document.body.classList.contains("is-page") &&
    !document.body.classList.contains("is-free") });
// A tap beside the globe, in the void around it, returns to the photo. In the computer's panel the void is the
// panel's own water, and a click there is a miss, not a way out.
globe.on("beside", () => { if (!Desk.on) toPhoto(); });
globe.on("pick", ({ lngLat, point }) => { if (state === "guess") putPin(lngLat, point); });
globe.on("pinmoved", () => { haptic("select"); toast(pick(lines("moved"))); });
function putPin(lngLat, point) {
  const first = !globe.guess;
  globe.pin(lngLat, { draggable: true, drop: true });
  toast(pick(lines(globe.water(point) ? "on_water" : first ? "on_land" : "moved")));
  haptic("medium");
  // The pin is placed but the answer is not sent: Telegram will ask for confirmation before closing the app.
  if (first) { split(true); setCall(T("pin_set"), T("answer")); if (tgAt("6.2")) tg.enableClosingConfirmation(); }
}

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
    const at = globe.guess.wrap();
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
const todayPath = () => SITE ? "/api/today" : "/api/today?" + new URLSearchParams(inTg ? { tz } : { tz, lang: LANG0 });
// "sent" right after the answer, "closed" when the window shut without one, "restore" when the app reopens after answering.
let waitShown = false;
function toWaiting(how) {
  setState("wait"); waitShown = true; globe.lock();
  if (how === "sent") haptic("success");
  // A pin that never reached the server is not an answer: it leaves the map, so nobody takes it for one.
  if (how === "closed" && !TODAY.answer) globe.pin(null);
  if (tgAt("6.2")) tg.disableClosingConfirmation();
  split(false); setCall(T(how === "closed" ? "closed" : "sent"), T("result_at", { time: atZone() }));
  $("timer").classList.add("hidden");
  // A browser in a zone maply does not name gets the time alone.
  overlay('<div class="pl nums"><b id="wleft">' + hm(secondsTo(TODAY.reveal)) + '</b><span class="up">' + esc(T("until_answer")) + '<br>' +
    esc(ZONE_NAME ? T("at", { time: AT, zone: ZONE_NAME }) : T("result_at", { time: atZone() })) + '</span></div>', tgLink());
  if (globe.guess && view === "map") globe.toBig(700);
}
// The app reopened after the answer: put the pin back where it was sent and show the result, live or final. The site
// waits for its evening.
function restore() {
  if (!TODAY.answer) return;
  globe.pin([TODAY.answer.lon, TODAY.answer.lat], { draggable: false });
  // Until the result comes, a tap on the map must not move the sent pin, and the caption already says the answer is in.
  if (SITE) toWaiting("restore");
  else { setState("wait"); globe.lock(); setCall(T("sent"), T("result_at", { time: atZone() })); loadResult(); }
  // After the state: on a computer the globe's corner is now the main area's.
  if (view === "photo") globe.toMini(0);
}
// mine is your own row of the chat's answers, once the result is in.
let mine = null;
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
// The parts of a score as a bar: kilometres, the bonuses one by one, speed, and for your own row what you missed.
function barOf(r) {
  const IF = [T("what_continent"), T("what_country"), T("what_city")];
  const b0 = r.pts - bonusOf(r.lvl) - r.sp, segs = ['<i class="km" data-tip="' + esc(T("km_points", { points: fmt(b0), km: kmT(r.d) })) + '" style="width:' + pct(b0) + ';--d:' + T_KM + 's"></i>'];
  BONUS.forEach((x, k) => {
    if (k < r.lvl) segs.push('<i data-tip="' + esc(T("bonus", { points: x, what: IF[k] })) + '" style="width:' + pct(x) + ';--d:' + T_B(k) + 's"></i>');
  });
  if (r.sp) segs.push('<i class="sp" data-tip="' + esc(T("speed_bonus", { points: r.sp })) + '" style="width:' + pct(r.sp) + ';--d:' + T_B(r.lvl) + 's"></i>');
  if (r.me) BONUS.forEach((x, k) => {
    if (k >= r.lvl) segs.push('<i class="if" data-tip="' + esc(T("bonus_missed", { points: x, what: IF[k] })) + '" style="width:' + pct(x) + ';--d:' + (T_B(r.lvl) + .1) + 's"></i>');
  });
  return segs.join("");
}
function boardRow(r, i, cls, count) {
  const v = (n) => count ? "0" : fmt(n), b0 = r.pts - bonusOf(r.lvl) - r.sp;
  // Your own face shows your initial: the name beside it already says "you".
  const n = r.rank || i + 1, grp = r.grp ? '<b class="gi" title="' + esc(T("in_group")) + '">' + GROUP + '</b>' : "";
  const face = '<i class="fc">' + faceOf(r) + grp + '</i>';
  // A hidden name in this group opens at the chat's reveal; anyone else's once the day is over in every zone.
  const wait = T(r.grp && PHASE === "live" ? "anon_row" : "anon_world");
  const label = r.hidden ? ' aria-label="' + esc([wait, r.grp ? T("in_group") : "", kmT(r.d), fmt(r.pts)].filter(Boolean).join(", ")) + '"' : "";
  return '<div class="r' + (r.me ? " me" : "") + (r.hidden ? " anon" : "") + cls + '" data-key="' + esc(r.key || "") + '"' + label + '><em' + (n <= 3 ? ' class="m' + n + '"' : "") + '>' + (typeof n === "number" ? fmt(n) : n) + '</em>' + face +
    '<span' + (r.handle && !r.hidden ? ' data-who="' + esc(r.handle) + '"' + (count ? '' : ' role="button" tabindex="0"') : '') + '>' + (r.hidden ? "•••" : esc(r.who)) + '</span>' +
    '<span class="hit" data-rest="' + esc(r.hidden ? wait : T("short_of", { points: fmt(5000 - (r.pts - r.sp)) })) + '"><span class="bar">' + barOf(r) + '</span></span>' +
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
  const meAt = rows.findIndex((r) => r.me), IF = [T("what_continent"), T("what_country"), T("what_city")];
  // Only the rows that show are drawn: the best ones and yours. The rest are in the sheet. The computer's side panel
  // has room for all of them.
  const top = Desk.on ? rows.length : meAt >= ROWS ? ROWS - 1 : ROWS;
  const shown = rows.map((r, i) => [r, i]).filter(([r, i]) => i < top || r.me), list = shown.map(([r]) => r);
  $("lbH").textContent = boardTitle();
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
  const canFold = meAt >= 0 && rows.length > 1 && !Desk.on;
  $("foldBtn").classList.toggle("hidden", !canFold);
  foldBoard(canFold && foldNow, false);
  paintScore(me);
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
// Your score in the computer's side panel: the points, what you found, the bar and its parts.
function paintScore(me) {
  const sc = $("score"), at = $("scoreIn") || sc.insertBefore(el('<div id="scoreIn"></div>'), sc.firstChild);
  sc.classList.toggle("on", !!me && Desk.on);
  if (!me || !Desk.on) { at.innerHTML = ""; return; }
  const b0 = me.pts - bonusOf(me.lvl) - me.sp, got = ["desk.got_continent", "desk.got_country", "desk.got_city"]
    .map((k, i) => '<span class="' + (i < me.lvl ? "y" : "n") + '">' + esc(T(k)) + " " + (i < me.lvl ? "✓" : "✗") + '</span>');
  at.innerHTML = '<b class="pts nums1">' + fmt(me.pts) + '</b><p class="got">' + esc(kmT(me.d)) + ' · ' + got.join(" · ") + '</p>' +
    '<span class="bar">' + barOf(me) + '</span><p class="parts">' + esc(T("desk.parts", { a: fmt(b0), b: fmt(bonusOf(me.lvl)), c: fmt(me.sp) })) + '</p>';
}
// "N more" counts what does not show: everything but the rows drawn, or but your own when folded.
function paintMore() {
  const n = Desk.on ? 0 : boardTotal - ($("lb").classList.contains("min") ? 1 : $("lb").children.length);
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
// "Top N" counts the unbroken run from first place: your group's rows further down come after the dashed line.
function boardTitle() {
  const gap = boardRows.findIndex((r, i) => skips(boardRows, i)), top = gap < 0 ? boardRows.length : gap;
  return boardRows.length < boardTotal ? T("top_of", { n: fmt(top), total: fmt(boardTotal) }) : T("all_answers", { n: fmt(boardTotal) });
}
function fillBoard() {
  $("boardH").textContent = boardTitle();
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
  if (grew) globe.keepInView();
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
const rowsOf = (res) => res.table.map((r) => ({ ...personOf(r), who: r.me ? T("you") : nameOf(r), at: [r.lon, r.lat], d: r.km || 0,
  pts: r.points || 0, lvl: levelOf(r.bonus), sp: r.speed || 0, key: r.key || r.lat.toFixed(5) + "," + r.lon.toFixed(5) }))
  .sort((a, b) => b.pts - a.pts || a.d - b.d);
// The world's table, best first: everyone scored so far today, in the DM too. The server's key tells the rows apart
// between polls without saying whose they are, and says which names are not out yet. A site guest without a name reads
// "Guest" once the world's names are out.
function boardOf(res) {
  const rows = (res.board || []).map((r) => ({ ...personOf(r), who: r.me ? T("you") : nameOf(r),
    d: r.km || 0, pts: r.points || 0, lvl: levelOf(r.bonus), sp: r.speed || 0, grp: !!r.group,
    key: r.key, rank: r.rank, handle: r.who || "" }));
  // An answer the site kept only in the browser is scored on the spot and has no place in the world: it shows as your
  // row without a rank, rather than "you didn't answer".
  const own = !rows.some((r) => r.me) && (res.table || []).find((r) => r.me && r.points != null);
  if (own) rows.push({ ...personOf(own), who: T("you"), d: own.km || 0, pts: own.points, lvl: levelOf(own.bonus), sp: own.speed || 0, key: own.key || "me", rank: "–" });
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
// The others' marks on the map. An unnamed one carries a question mark and its distance instead of a name; at the reveal
// the names take the question marks' place in the same marks.
const friendsOf = (rows) => rows.filter((r) => !r.me).map((f) => ({ key: f.key, at: f.at, d: f.d,
  label: f.hidden ? kmT(f.d) : f.who, face: faceOf(f) }));
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
  globe.world(res.world && res.world.pins);
  if (res.version && g0 === g1 && p0 === p1) { setLive(res); return; }
  const rows = rowsOf(res), fresh = new Set(rows.filter((r) => !seen.has(r.key)).map((r) => r.key));
  rows.forEach((r) => seen.add(r.key));
  mine = rows.find((r) => r.me) || mine;
  globe.others(friendsOf(rows), fresh);
  setLive(res);
  if (was === "live" && PHASE === "final") { toast(T("names_open")); haptic("success"); }
  else if (fresh.size) haptic("light");
}
function toReveal(res) {
  if (state === "reveal") return;
  setState("reveal");
  // The table below is drawn before setLive: its hidden rows say when their names come by the phase.
  PHASE = res.phase || "final";
  // The answer is in: closing the app loses nothing, so Telegram must not ask.
  if (tgAt("6.2")) tg.disableClosingConfirmation();
  const P = res.place;
  credits(P);
  const rows = rowsOf(res);
  seen = new Set(rows.map((r) => r.key));
  mine = rows.find((r) => r.me) || null;
  // A pin that never reached the server is not an answer: it leaves the map rather than draw a line to the place.
  globe.pin(mine && mine.at, { draggable: false });
  const board = boardOf(res);
  seenB = new Set(board.map((r) => r.key));
  paintBoard(board, res.players);
  $("lb").offsetWidth; // the bar and avatars must catch the closed state, otherwise there will be no animation
  document.body.classList.add("is-reveal");
  ovUnderBoard();
  // The world's pins go under the answer's marks.
  globe.world(res.world && res.world.pins);
  globe.answer({ truth: [P.lon, P.lat], km: mine ? mine.d : null, others: friendsOf(rows) });
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
  view = "map"; document.body.classList.add("is-map"); syncBack(); seePano();
  behindSheet(); lookLabel();
  globe.frame(1200);
  haptic("success");
}
$("go").addEventListener("click", () => {
  if (state === "guess" && globe.guess) send();
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
/* ---------- The sheet ---------- */
// A page over the game: the days (days.js) and every answer of the day. A site player's face carries the site's mark.
const SRC_SITE = '<svg class="src" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="12" fill="#FFFFFF"/><g fill="none" stroke="#245A3C" stroke-width="1.6"><circle cx="12" cy="12" r="6.5"/><path d="M5.5 12h13M12 5.5c-2.4 2.4-2.4 10.6 0 13M12 5.5c2.4 2.4 2.4 10.6 0 13"/></g></svg>';
// A distance as the bot writes it: metres under a kilometre, one decimal under ten.
const kmIn = (tx, lang, km) => km < 1 ? textIn(tx, "unit.m", { m: numIn(lang, km * 1000) })
  : textIn(tx, "unit.km", { km: numIn(lang, km, km >= 10 ? 0 : 1) });
const kmT = (km) => kmIn(TX, LANG, km);
// Sofia Sans draws Cyrillic in its Bulgarian forms unless told the language. A name can be in any script; in an interface
// that is not Cyrillic, Russian forms are the likelier right ones for a Cyrillic name.
const nameLang = () => ["ru", "uk", "be"].includes(LANG) ? LANG : "ru";
async function getJSON(path) {
  if (!inTg) return reply(await get(path + (path.includes("?") ? "&" : "?") + "lang=" + encodeURIComponent(LANG)));
  return reply(await get(path, { headers: { Authorization: "tma " + tg.initData } }));
}
// The sheet is a dialog over the app: a screen reader announces its title and starts on its back button, or on the
// sheet itself where the way out is the header's or Telegram's.
// SHEET counts what the sheet shows: a reply that comes back after the sheet moved on or closed is dropped.
let SHEET = 0, sheetFrom = null;
function sheet(title, html, back, close) {
  const sh = $("sheet");
  // Closing gives the focus back to what opened the sheet.
  if (!sh.classList.contains("on")) sheetFrom = document.activeElement;
  SHEET++;
  sh.innerHTML = '<div class="sh-top"><button class="sh-b" id="shBack" aria-label="' + esc(T("desk.back")) + '"></button><b id="shTitle">' + title + '</b></div><div class="sh-body">' + html + '</div>';
  sh.dataset.kind = ""; sh.classList.add("on"); document.body.classList.add("is-sheet"); syncBack(); behindSheet(true);
  const b = $("shBack");
  sheetBack(b, close);
  b.onclick = back || close || closeDays;
  b.focus({ preventScroll: true });
  return SHEET;
}
// The sheet's back is one arrow, pointing the way back reads. In a browser a step inside (a day to the list, a mini
// profile or the world's table to what it came over) always shows it. The page's own way out, the close it was given,
// shows only in a phone's browser: on a computer the header leads out (desktop.css). In Telegram its Back is the
// only one and clicks this button for both (app.css). days.js
// sets the action itself as a day opens and closes, so the button tells the two apart as its action is set.
function sheetBack(b, close) {
  let act = null;
  Object.defineProperty(b, "onclick", {
    configurable: true,
    get: () => act,
    set(f) {
      act = f; b.classList.toggle("root", !!close && f === close); b.textContent = fwd() === "→" ? "←" : "→";
      // Back from a day to the list on a phone in Telegram: the arrow that had the focus is the way out now, not drawn.
      if (document.activeElement === b && !b.getClientRects().length) $("sheet").focus({ preventScroll: true });
    },
  });
  b.addEventListener("click", (e) => { if (act) act.call(b, e); });
  // The button is the sheet's first stop for the focus, here and in the sheets put back (profile.js). Where it is not
  // drawn, the sheet itself takes the focus.
  b.focus = (o) => b.getClientRects().length ? HTMLElement.prototype.focus.call(b, o) : $("sheet").focus(o);
}
// What is in reach, from the top layer down; the rest is inert, out of Tab's and a screen reader's way. While the
// sheet is open, the sheet, and on a computer, where it is a page under the header, the header's links beside it.
// Else one of menu.js's pages with the header (a mini profile over the leaderboards gives the page back). Else the
// game, its folded map left out. The free game keeps only itself. The loading screen is never out of reach: its
// retry is a tap. Every layer that opens or closes asks here, so none undoes another's.
function behindSheet(on = $("sheet").classList.contains("on")) {
  const page = document.querySelector(".pg.on"), free = document.body.classList.contains("is-free") && $("free");
  for (const e of document.body.children) {
    if (e.id === "sheet" || e.id === "toast" || e.id === "loader" || e.tagName === "SCRIPT") continue;
    e.inert = free ? e !== free : on ? !(Desk.on && e.classList.contains("top")) : !!page && e !== page && !e.classList.contains("top");
  }
  // On a computer the header stays usable beside the sheet, so the sheet is not modal there.
  $("sheet").setAttribute("aria-modal", String(!Desk.on));
  if (!free && !on && !page) $("map").inert = view === "photo";
}
function closeDays() {
  SHEET++;
  $("sheet").classList.remove("on"); document.body.classList.remove("is-sheet"); syncBack(); behindSheet(false);
  if (sheetFrom && document.contains(sheetFrom)) sheetFrom.focus({ preventScroll: true });
  sheetFrom = null;
}
// Escape is the sheet's own back button: from a day to the days, from the days out.
addEventListener("keydown", (e) => { if (e.key === "Escape" && $("sheet").classList.contains("on")) { e.preventDefault(); $("shBack").click(); } });
// The day is /today, the page under every other: it has nothing of its own to open, only Telegram's Back, which
// leads from it to the menu. Leaving it closes a sheet that has no address of its own (a mini profile, the world's
// table), or the next page would open under it. The menu is menu.js's, the days days.js's.
Router.screen("today", /^\/today$/, {
  // Whatever closed before the day (the free game, a page) leaves its reach to be set again.
  open() { syncBack(); behindSheet(); },
  close() { if ($("sheet").classList.contains("on")) closeDays(); },
});

// The window crossed into the computer's layout or out of it: the same state, laid out again. The plates have moved
// already; the map's box changed its place, so the camera starts over in it.
Desk.change(() => {
  hideTip(); showRules(false);
  if (document.body.classList.contains("is-sheet")) behindSheet(true);
  // The sheet's way out is drawn on one side of the line and not on the other: a focus it had goes to the sheet.
  if ($("sheet").classList.contains("on") && document.activeElement === document.body) $("shBack").focus({ preventScroll: true });
  syncBack();
  split($("act").classList.contains("on"));
  if (state === "reveal") { paintBoard(boardRows, boardTotal); countUp(new Set()); ovUnderBoard(); }
  seePano();
  globe.resize();
  if (view !== "map") globe.toMini(0);
  else if (state === "reveal" && globe.answered) globe.frame(0);
  else globe.toBig(0);
});

/* ---------- Start ---------- */
// Opened inside Telegram without initData (a plain link in the in-app browser) there is nothing to play as.
async function boot() {
  // Who is signed in on the site decides which API plays.
  if (typeof accountBoot === "function") await accountBoot();
  loadFace();
  if (window.Free && Free.takes()) return Free.start(boot);
  // The client's language first, so nothing on the first screen is blank; the person's own choice from core
  // arrives with /api/today and wins.
  const [today] = await Promise.all([
    !inTg || tg.initData ? api(todayPath()).catch((e) => e) : null,
    loadTexts(LANG0).catch(() => {}),
  ]);
  setCall(T("where"), T("drop_pin"));
  if (!today) { stopLoading(T("open_in_telegram")); return; }
  if (today instanceof Error) { failLoading(errorText(today), boot); return; }
  TODAY = today;
  if (TODAY.now) SKEW = Date.parse(TODAY.now) - Date.now();
  // The texts are asked for again when they did not come with the first try, even in the same language.
  if (TODAY.lang && (TODAY.lang !== LANG || !textsLoaded)) { await loadTexts(TODAY.lang).catch(() => {}); setCall(T("where"), T("drop_pin")); }
  // The words are final now: the computer's header and keys can be written, and the page the address names can open.
  Desk.start({ view: () => view, state: () => state, open: toMap, close: toPhoto, pin: (lngLat, point) => { if (state === "guess") putPin(lngLat, point); }, globe, pano });
  // A group's post opens the app with its signed payload and no address of its own: that is the day, not the menu.
  // A DM's post names /today itself; the bot's menu button and /start open the menu.
  if (!Router.current && inTg && tg.initDataUnsafe.start_param && /^\/(?:index\.html)?$/.test(location.pathname)) Router.go("/today", { replace: true });
  // The day and the time of its result, before the first page: the menu writes them.
  $("day").textContent = "#" + TODAY.day;
  AT = new Intl.DateTimeFormat(numLang(), { timeZone: TODAY.zone, hour: "2-digit", minute: "2-digit", hourCycle: "h23" }).format(new Date(TODAY.reveal));
  ZONE_NAME = ZONES[TODAY.zone] || "";
  Router.start();
  // The answer's bar says what it waits for: on a phone it is folded, in the computer's panel it is dimmed.
  if (state === "guess") split(false);
  loadShareTexts();
  if (typeof accountReady === "function") accountReady();
  pano.show({ image: TODAY.image, kind: TODAY.kind, heading: TODAY.heading || 0 });
  // Someone who has answered is told so at once, not asked to drop a pin while the map loads.
  if (TODAY.answer) setCall(T("sent"), T("result_at", { time: atZone() }));
  // The answer and the result need the map's style: markers and the line are added to it.
  await new Promise((r) => globe.ready(r));
  if (TODAY.answer) { restore(); hideLoader(); }
  else if (!secondsTo(TODAY.deadline)) toWaiting("closed");
  tick();
  if (!secondsTo(TODAY.reveal)) loadResult();
  if (TODAY.zone_set) setTimeout(() => toast(T("zone_set", { zone: ZONES[TODAY.zone_set] || TODAY.zone_set })), 1200);
}
boot();
