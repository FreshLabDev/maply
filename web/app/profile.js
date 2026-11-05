/* ---------- Profile ---------- */
// The player's own profile opens from the letter at the end of the header; the logo still opens the days. Any element
// with data-who="<handle>" opens that player's mini profile: the server puts a handle next to a name only, so a row
// without a name has nothing to tap. Charts are drawn here in SVG, without a library.
// PROF is the player's own profile, VIEW the one on show: the own one or a public page.
let PROF = null, VIEW = null, PROF_TAB = "ranked";
try { PROF_TAB = localStorage.getItem("maply.prof.tab") === "free" ? "free" : "ranked"; } catch {}

// The letter in the header: the Telegram name, the name typed on the site, or a figure for a guest without one.
const FIGURE = '<svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true"><circle cx="8" cy="5.2" r="3" fill="currentColor"/><path d="M2 15c0-3.4 2.7-5.6 6-5.6s6 2.2 6 5.6z" fill="currentColor"/></svg>';
function meLetter(name) {
  const n = String(name || (inTg && tg.initDataUnsafe.user && tg.initDataUnsafe.user.first_name) || (SITE ? siteName() : "")).trim();
  $("meBtn").innerHTML = n ? esc(initial(n)) : FIGURE;
}
meLetter();
// The profile is the page /me and a public profile /p/<slug>: the letter goes to /me, and × goes back to the page before.
$("meBtn").addEventListener("click", () => Router.go("/me"));
const leavePage = () => Router.back("/");
Router.screen("me", /^\/me$/, { open() { openProfile(); }, close() { closeDays(); } });
Router.screen("p", /^\/p\/([\w-]+)$/, { open(m) { openPublic(m[1]); }, close() { closeDays(); } });

// A sheet put aside to come back to: its own nodes, so whatever it bound keeps working, and where it was scrolled.
// Focus comes back to what was tapped: a row of the history, a pin, a name.
function keepSheet() {
  const sh = $("sheet");
  if (!sh.classList.contains("on")) return null;
  const body = sh.querySelector(".sh-body"), at = document.activeElement;
  return { top: sh.querySelector(".sh-top"), body, kind: sh.dataset.kind, scroll: body ? body.scrollTop : 0, focus: body && body.contains(at) ? at : null };
}
function restoreSheet(s) {
  SHEET++;
  const sh = $("sheet");
  sh.replaceChildren(s.top, s.body);
  sh.dataset.kind = s.kind || "";
  if (s.body) s.body.scrollTop = s.scroll;
  // The world's table may have grown while it was aside, and the settings may have changed in another copy of the profile.
  if (s.kind === "board" && typeof fillBoard === "function") fillBoard();
  const was = s.focus && s.focus.id;
  if (s.kind === "profile") redrawSettings();
  (s.focus && s.focus.isConnected ? s.focus : (was && $(was)) || $("shBack")).focus({ preventScroll: true });
}
// A sheet opened from another comes back to it; opened from the app, it closes.
const backTo = (saved) => saved ? () => restoreSheet(saved) : null;

const wait = () => '<p class="sh-wait up">' + esc(lines("loading")[0]) + '</p>';

async function openProfile(saved) {
  haptic("light");
  saved = saved || null;
  const ticket = sheet(esc(T("prof.title")), wait(), backTo(saved), leavePage);
  $("sheet").dataset.kind = "profile";
  let d;
  try { d = await getJSON("/api/profile"); } catch (e) {
    if (ticket === SHEET) sheet(esc(T("prof.title")), '<p class="sh-wait">' + esc(errorText(e)) + '</p>', backTo(saved), leavePage);
    return;
  }
  if (ticket !== SHEET) return;
  PROF = Object.assign(d, { saved });
  if (d.name) meLetter(d.name);
  showProfile();
}

function showProfile() { drawProfile(PROF, backTo(PROF.saved), "profile"); }
function drawProfile(d, back, kind) {
  VIEW = d;
  sheet(esc(T("prof.title")), '<div class="prof">' + cardHTML(d) + tabsHTML() + '<div id="profTab"></div></div>', back, leavePage);
  $("sheet").dataset.kind = kind;
  $("sheet").querySelectorAll(".ptabs button").forEach((b) => { b.onclick = () => { haptic("select"); setTab(b.dataset.tab); }; });
  $("sheet").querySelector(".ptabs").onkeydown = tabKey;
  const signIn = $("profSignIn");
  if (signIn) signIn.onclick = () => openSignIn();
  setTab(PROF_TAB, true);
}

function cardHTML(d) {
  const name = d.name || (d.guest ? T("guest") : "");
  let h = '<div class="pcard">' + av(name || "?", true) + '<div><b lang="' + nameLang() + '" dir="auto">' + esc(name) + '</b>' +
    '<span class="up">' + esc(T(d.guest ? "from_site" : "from_tg")) + '</span></div></div>';
  if (d.guest) {
    // The account menu comes from the accounts screen; until it is there the note stands alone.
    const btn = typeof openSignIn === "function" ? '<button class="pbtn" id="profSignIn">' + esc(T("prof.sign_in")) + '</button>' : "";
    h += '<div class="pnote"><p>' + esc(T("prof.guest_note")) + '</p>' + btn + '</div>';
  }
  return h;
}

function tabsHTML() {
  const tab = (id, key) => '<button role="tab" id="pt-' + id + '" data-tab="' + id + '" aria-controls="profTab">' + esc(T(key)) + '</button>';
  return '<div class="ptabs" role="tablist">' + tab("ranked", "prof.tab_ranked") + tab("free", "prof.tab_free") + '</div>';
}
// The arrows move between the tabs, the other way in a right-to-left language; Home and End go to the ends.
const TABS = ["ranked", "free"];
function tabKey(e) {
  const rtl = document.documentElement.dir === "rtl", i = TABS.indexOf(PROF_TAB);
  const to = { ArrowRight: rtl ? i - 1 : i + 1, ArrowLeft: rtl ? i + 1 : i - 1, Home: 0, End: TABS.length - 1 }[e.key];
  if (to == null) return;
  e.preventDefault();
  const tab = TABS[(to + TABS.length) % TABS.length];
  if (tab !== PROF_TAB) { haptic("select"); setTab(tab); }
  $("pt-" + tab).focus();
}

function setTab(tab, first) {
  PROF_TAB = tab;
  try { localStorage.setItem("maply.prof.tab", tab); } catch {}
  $("sheet").querySelectorAll(".ptabs button").forEach((b) => {
    const on = b.dataset.tab === tab;
    b.setAttribute("aria-selected", on); b.tabIndex = on ? 0 : -1;
  });
  const box = $("profTab");
  box.setAttribute("aria-labelledby", "pt-" + tab);
  box.setAttribute("role", "tabpanel");
  box.innerHTML = tab === "free" ? freeHTML(VIEW.free) : rankedHTML(VIEW);
  if (tab === "ranked") bindRanked(box);
  // A tab chosen from far down the other one brings the tabs back into view, not the panel's top under the header.
  const tabs = $("sheet").querySelector(".ptabs"), body = $("sheet").querySelector(".sh-body");
  if (!first && tabs && body && tabs.getBoundingClientRect().top < body.getBoundingClientRect().top) tabs.scrollIntoView({ block: "start" });
}

/* ---------- Numbers ---------- */
const tile = (label, value, sub, cls) => '<div class="pt' + (cls ? " " + cls : "") + '"><span class="up">' + esc(label) + '</span><b>' + value + '</b>' +
  (sub ? '<small>' + sub + '</small>' : "") + '</div>';
const none = "–";
// The same percent the translations write, "47%", "47 %" or "%47", and not Intl's, which differs from them in ru, uk, es.
const profPct = (n) => T("prof.pct", { pct: fmt(n) });
const dayLink = (day, date) => '#' + fmt(day) + ' · ' + esc(dayDate(date));
const placeOf = (p) => esc(T("prof.place_of", { rank: fmt(p.rank), of: fmt(p.of) }));
// A flag from the country code. Windows draws no flag emoji, so there the code stands in a box.
const NO_FLAGS = /Windows/.test(navigator.userAgent);
const profFlag = (iso) => !iso ? "" : NO_FLAGS || !/^[A-Z]{2}$/.test(iso)
  ? '<i class="pflag code">' + esc(iso) + '</i>'
  : '<i class="pflag" aria-hidden="true">' + String.fromCodePoint(...[...iso].map((c) => 0x1F1A5 + c.charCodeAt(0))) + '</i>';

function rankedHTML(d) {
  const r = d.ranked;
  if (!r.days) return '<p class="sh-wait">' + esc(T(d.guest || d.pub ? "prof.no_days_guest" : "prof.no_days")) + '</p>' + settingsHTML(d);
  let h = '';
  if (r.percentile != null) h += '<div class="pbig"><b>' + esc(T("prof.better", { pct: fmt(r.percentile) })) + '</b><span>' + esc(T("prof.better_note")) + '</span></div>';
  h += '<div class="ptiles">' +
    tile(T("prof.points"), fmt(r.points)) +
    tile(T("prof.avg30"), r.avg30 != null ? fmt(r.avg30) : none) +
    tile(T("prof.days"), fmt(r.days)) +
    tile(T("prof.streak"), fmt(r.streak), esc(T("prof.best_streak", { n: fmt(r.best_streak) }))) +
    tile(T("prof.median"), r.median_km != null ? esc(kmT(r.median_km)) : none) +
    tile(T("prof.found"), esc(profPct(r.country_pct)), esc(T("prof.found_city", { pct: fmt(r.city_pct) }))) +
    tile(T("prof.best_day"), r.best_day ? fmt(r.best_day.points) : none, r.best_day ? dayLink(r.best_day.day, r.best_day.date) : "", "gold") +
    tile(T("prof.best_place"), r.best_place ? placeOf(r.best_place) : none, r.best_place ? dayLink(r.best_place.day, r.best_place.date) : "", r.best_place && r.best_place.rank === 1 ? "gold" : "") +
    '</div>' + seasonHTML(d.season);
  h += '<div class="pcharts">';
  h += section(T("prof.chart_points"), seriesSVG(r.series, r.median30));
  h += section(T("prof.chart_misses"), missesSVG(r.misses));
  h += section(T("prof.chart_continents"), continentsHTML(r));
  // The pins and the history are the owner's: a public page has neither.
  if (r.history) h += section(T("prof.chart_map"), mapSVG(r.history) + '<p class="pnote-s">' + esc(T("prof.map_hint")) + '</p>');
  h += section(T("prof.passport"), passportHTML(r.passport), "wide");
  h += '</div>';
  // The settings come before the history, which grows by a row a day.
  return h + settingsHTML(d) + (r.history ? section(T("prof.history"), historyHTML(r.history)) : "");
}
// The month's season so far, the sum of its 20 best days: in the world, and in the group the profile was opened from.
function seasonHTML(list) {
  if (!list || !list.length) return "";
  return '<div class="ptiles pseason">' + list.map((p) => tile(T(p.kind === "group" ? "prof.season_group" : "prof.season_world"), placeOf(p),
    esc(T("prof.season_points", { points: fmt(p.points) })), p.rank === 1 ? "gold" : "")).join("") + '</div><p class="pnote-s pseason-n">' + esc(T("prof.season_hint")) + '</p>';
}
const section = (title, body, cls) => '<section class="psec' + (cls ? " " + cls : "") + '"><h3 class="up">' + esc(title) + '</h3>' + body + '</section>';

function bindRanked(box) {
  box.querySelectorAll("[data-date]").forEach((e) => {
    const go = () => openDayFrom(e.dataset.date);
    e.addEventListener("click", go);
    if (e.tagName !== "BUTTON") e.addEventListener("keydown", (k) => { if (k.key === "Enter" || k.key === " ") { k.preventDefault(); go(); } });
  });
  bindSettings();
}
function bindSettings() {
  const hide = $("hideMe");
  if (hide) hide.onchange = () => saveHide(hide);
  const pub = $("pubOn");
  if (pub) pub.onchange = () => savePublic(pub.checked, false, "pubOn");
  if ($("pubNew")) $("pubNew").onclick = () => savePublic(true, true, "pubNew");
  if ($("pubCopy")) $("pubCopy").onclick = copyPublic;
}

// A closed day's page, by its address: on the site Back comes back to the profile. Only a day over everywhere has one.
function openDayFrom(date) {
  Router.go("/days/" + date);
}

/* ---------- Charts ---------- */
// Every chart is a viewBox the width of a phone, scaled to the column; text inside stays left to right, as the axes do.
const SVG = (w, h, inner, label) => '<svg class="pchart" viewBox="0 0 ' + w + ' ' + h + '" role="img" aria-label="' + esc(label) + '">' + inner + '</svg>';
const MAXPTS = 5000 + SPEED_MAX;

// The last 30 rounds: a bar for a day played, a dot for one missed, and the median of the days played as a dashed line.
function seriesSVG(series, median) {
  // Thirty slots however many rounds there have been, the newest at the right: two days are two thin bars, not two walls.
  const W = 330, H = 132, top = 16, base = H - 20, slots = Math.max(series.length, 30), slot = W / slots, x0 = W - series.length * slot, bw = Math.max(2, slot * .62);
  const y = (p) => base - p / MAXPTS * (base - top);
  let g = '<path class="grid" d="M0 ' + y(2500) + 'H' + W + 'M0 ' + y(5000) + 'H' + W + '"/>';
  g += '<text class="ax" x="0" y="' + (y(5000) - 4) + '">' + fmt(5000) + '</text>';
  series.forEach((p, i) => {
    const x = x0 + i * slot + (slot - bw) / 2;
    if (p.points == null) { g += '<circle class="miss" cx="' + (x + bw / 2) + '" cy="' + (base - 1.5) + '" r="1.5"/>'; return; }
    const h = Math.max(1.5, base - y(p.points));
    g += '<rect class="bar' + (p.points >= 4500 ? ' hi' : '') + '" x="' + x.toFixed(1) + '" y="' + (base - h).toFixed(1) + '" width="' + bw.toFixed(1) + '" height="' + h.toFixed(1) + '"><title>' +
      esc('#' + p.day + ' · ' + fmt(p.points)) + '</title></rect>';
  });
  if (median != null) {
    const my = y(median).toFixed(1), label = T("prof.median_line", { n: fmt(median) });
    // The label sits in the top row, by the scale, so it never covers a bar.
    g += '<path class="med" d="M0 ' + my + 'H' + W + '"/><text class="mt" x="' + W + '" y="' + (y(5000) - 4) + '" text-anchor="end">' + esc(label) + '</text>';
  }
  if (series.length) {
    g += '<text class="ax" x="' + x0.toFixed(1) + '" y="' + (H - 4) + '">#' + series[0].day + '</text>' +
      (series.length > 1 ? '<text class="ax" x="' + W + '" y="' + (H - 4) + '" text-anchor="end">#' + series[series.length - 1].day + '</text>' : "");
  }
  const played = series.filter((p) => p.points != null).length;
  return SVG(W, H, g, T("prof.chart_points") + ": " + T("prof.played_of", { n: fmt(played), of: fmt(series.length) }) + (median != null ? ", " + T("prof.median_line", { n: fmt(median) }) : ""));
}

// Misses by distance, nearest bin first: up to 1 km, 10, 50, 150, 500, 1,000, 2,500 and further.
const MISS_EDGES = [1, 10, 50, 150, 500, 1000, 2500];
function missesSVG(misses) {
  const W = 330, H = 128, top = 16, base = H - 22, n = misses.length, slot = W / n, bw = slot * .7, most = Math.max(1, ...misses);
  let g = '', alt = [];
  misses.forEach((c, i) => {
    const x = i * slot + (slot - bw) / 2, h = c ? Math.max(2, c / most * (base - top)) : 0;
    const edge = i < MISS_EDGES.length ? fmt(MISS_EDGES[i]) : fmt(MISS_EDGES[MISS_EDGES.length - 1]) + "+";
    if (c) g += '<rect class="bar' + (i < 3 ? ' hi' : '') + '" x="' + x.toFixed(1) + '" y="' + (base - h).toFixed(1) + '" width="' + bw.toFixed(1) + '" height="' + h.toFixed(1) + '"/>';
    g += '<path class="grid" d="M' + x.toFixed(1) + ' ' + base + 'h' + bw.toFixed(1) + '"/>';
    if (c) g += '<text class="ct" x="' + (x + bw / 2).toFixed(1) + '" y="' + (base - h - 4).toFixed(1) + '" text-anchor="middle">' + fmt(c) + '</text>';
    g += '<text class="ax" x="' + (x + bw / 2).toFixed(1) + '" y="' + (H - 6) + '" text-anchor="middle">' + edge + '</text>';
    alt.push(edge + ": " + fmt(c));
  });
  return SVG(W, H, g, T("prof.chart_misses") + ": " + alt.join(", "));
}

// Continents by the mean points of their days, with the best and the worst named when there are two to tell apart.
function continentsHTML(r) {
  if (!r.continents.length) return "";
  let h = '<div class="pconts">';
  for (const c of r.continents) {
    const w = (c.avg / MAXPTS * 100).toFixed(1);
    h += '<div class="pc' + (c.continent === r.best_continent ? " best" : c.continent === r.worst_continent ? " worst" : "") + '"><span><b>' + esc(c.name) + '</b><small>' + esc(T("prof.days_n", { n: fmt(c.days) })) + '</small></span><i><svg viewBox="0 0 100 8" preserveAspectRatio="none" aria-hidden="true"><rect width="100" height="8" class="bg"/><rect width="' + w + '" height="8" class="fg"/></svg></i><b>' + fmt(c.avg) + '</b></div>';
  }
  h += '</div>';
  const name = (k) => (r.continents.find((c) => c.continent === k) || {}).name || k;
  if (r.best_continent && r.worst_continent) h += '<p class="pnote-s">' + esc(T("prof.best_worst", { best: name(r.best_continent), worst: name(r.worst_continent) })) + '</p>';
  return h;
}

// The land dots of profland.js, decoded once: x and y in degrees from the top left, and the country of each.
let DOTS = null;
function landDots() {
  if (DOTS || typeof PROFILE_LAND === "undefined") return DOTS || [];
  const L = PROFILE_LAND, bits = atob(L.bits), idx = atob(L.land);
  DOTS = [];
  let n = 0, k = 0;
  L.rows.forEach((count, i) => {
    const lat = -90 + L.step / 2 + i * L.step;
    for (let j = 0; j < count; j++, n++) {
      if (!(bits.charCodeAt(n >> 3) & (1 << (n & 7)))) continue;
      let lon = -180 + (j + .5 + (i % 2) * .5) * 360 / count;
      if (lon >= 180) lon -= 360;
      const c = idx.charCodeAt(k++);
      DOTS.push([lon + 180, 90 - lat, c ? L.codes[c - 1] : ""]);
    }
  });
  // A country too small for the grid has a dot of its own.
  for (const [lon, lat, c] of L.extra || []) DOTS.push([lon + 180, 90 - lat, L.codes[c - 1]]);
  return DOTS;
}
// A pin's colour by its miss: gold close, white near, grey far, dim very far. The same bins as the legend.
const PIN_BINS = [[100, "p1"], [500, "p2"], [1500, "p3"], [Infinity, "p4"]];
const pinClass = (km) => PIN_BINS.find(([top]) => km <= top)[1];

// The map of the player's pins on the land dots, framed on where they are; a tap on a pin of a closed day opens it.
function mapSVG(history) {
  if (!history.length) return "";
  const W = 330;
  let x0 = 180, x1 = 180, y0 = 90, y1 = 90;
  const pts = history.map((h) => ({ h, x: h.lon + 180, y: 90 - h.lat }));
  pts.forEach((p, i) => { if (!i) { x0 = x1 = p.x; y0 = y1 = p.y; } x0 = Math.min(x0, p.x); x1 = Math.max(x1, p.x); y0 = Math.min(y0, p.y); y1 = Math.max(y1, p.y); });
  // At least a region the size of Europe, two to one, inside the world without the poles.
  let w = Math.max(56, (x1 - x0) * 1.25 + 12), hgt = w / 2;
  if (hgt < (y1 - y0) * 1.25 + 8) { hgt = (y1 - y0) * 1.25 + 8; w = hgt * 2; }
  w = Math.min(w, 360); hgt = Math.min(hgt, 150);
  let vx = Math.min(Math.max((x0 + x1) / 2 - w / 2, 0), 360 - w), vy = Math.min(Math.max((y0 + y1) / 2 - hgt / 2, 4), 154 - hgt);
  // The land's dots stay thinner than a pin however close the frame is.
  const unit = w / W, dot = Math.min(PROFILE_LAND_STEP() * .62, 2.5 * unit).toFixed(2);
  let d = "";
  for (const [x, y] of landDots()) if (x >= vx - 2 && x <= vx + w + 2 && y >= vy - 2 && y <= vy + hgt + 2) d += "M" + x.toFixed(1) + " " + y.toFixed(1) + "h0";
  let g = '<path class="land" stroke-width="' + dot + '" d="' + d + '"/>';
  // Closer pins on top, so a good day is not buried under a far one.
  // A pin near a pole stays inside the frame. A pin that opens its day has a wider ring around it for the finger.
  pts.sort((a, b) => b.h.km - a.h.km).forEach(({ h, x, y }) => {
    const label = '#' + h.day + ' · ' + kmT(h.km) + ' · ' + fmt(h.points), cx = x.toFixed(2), cy = Math.min(Math.max(y, vy + 5 * unit), vy + hgt - 5 * unit).toFixed(2);
    const pin = '<circle class="pp ' + pinClass(h.km) + '" cx="' + cx + '" cy="' + cy + '" r="' + (4.5 * unit).toFixed(2) + '" stroke-width="' + (1.2 * unit).toFixed(2) + '"/>';
    g += h.final
      ? '<g class="go" data-date="' + esc(h.date) + '" tabindex="0" role="button" aria-label="' + esc(label) + '"><title>' + esc(label) + '</title>' +
        '<circle class="hit" cx="' + cx + '" cy="' + cy + '" r="' + (11 * unit).toFixed(2) + '"/><circle class="ring" cx="' + cx + '" cy="' + cy + '" r="' + (7 * unit).toFixed(2) + '" stroke-width="' + (2 * unit).toFixed(2) + '"/>' + pin + '</g>'
      : '<g><title>' + esc(label) + '</title>' + pin + '</g>';
  });
  const legend = PIN_BINS.map(([top, c], i) => '<span><i class="' + c + '"></i>' + esc(i < PIN_BINS.length - 1 ? T("prof.upto", { km: kmT(top) }) : T("prof.further")) + '</span>').join("");
  return '<svg class="pmap" viewBox="' + vx.toFixed(1) + ' ' + vy.toFixed(1) + ' ' + w.toFixed(1) + ' ' + hgt.toFixed(1) + '" role="group" aria-label="' + esc(T("prof.chart_map")) + '">' + g + '</svg>' +
    '<p class="plegend">' + legend + '</p>';
}
const PROFILE_LAND_STEP = () => typeof PROFILE_LAND === "undefined" ? 2 : PROFILE_LAND.step;

// The passport: the world's land dots, a found country's in gold, then its flags and the two stamps.
function passportHTML(p) {
  // A found country's dots are drawn larger: a small country has only a few.
  const found = new Set(p.countries), dot = (PROFILE_LAND_STEP() * .62).toFixed(2), big = (PROFILE_LAND_STEP() * 1.05).toFixed(2);
  let a = "", b = "";
  for (const [x, y, c] of landDots()) {
    if (y < 4 || y > 154) continue;
    const s = "M" + x.toFixed(1) + " " + y.toFixed(1) + "h0";
    if (found.has(c)) b += s; else a += s;
  }
  let h = '<p class="pbig-s">' + esc(T("prof.passport_n", { n: fmt(p.countries.length) })) + '</p>';
  h += '<svg class="pmap world" viewBox="0 4 360 150" role="img" aria-label="' + esc(T("prof.passport_n", { n: fmt(p.countries.length) })) + '"><path class="land" stroke-width="' + dot + '" d="' + a + '"/><path class="land gold" stroke-width="' + big + '" d="' + b + '"/></svg>';
  if (p.countries.length) h += '<p class="pflags">' + p.countries.map((c) => '<span title="' + esc(regionName(c)) + '">' + profFlag(c) + '<small>' + esc(regionName(c)) + '</small></span>').join("") + '</p>';
  else h += '<p class="pnote-s">' + esc(T("prof.passport_hint")) + '</p>';
  const stamp = (on, key) => '<span class="pstamp' + (on ? " on" : "") + '">' + esc(T(key)) + '</span>';
  return h + '<p class="pstamps">' + stamp(p.streak30, "prof.stamp_streak30") + stamp(p.city1km, "prof.stamp_city1km") + '</p>';
}

// Every played day, newest first. A day over everywhere opens its page; today's waits for its world place.
function historyHTML(history) {
  let h = '<ol class="phist">';
  for (const d of history) {
    const where = [d.city, d.country && regionName(d.country)].filter(Boolean).join(", ");
    const world = d.rank ? placeOf(d) + (d.pct > 0 ? ' · ' + esc(T("prof.better_day", { pct: fmt(d.pct) })) : "") : esc(T("prof.final_wait"));
    const inner = '<span class="up">' + dayLink(d.day, d.date) + '</span><span class="w">' + profFlag(d.country) + ' <bdi>' + esc(where) + '</bdi></span>' +
      '<span class="n"><b>' + fmt(d.points) + '</b> · ' + esc(kmT(d.km)) + '</span><span class="r">' + world + '</span>';
    h += '<li>' + (d.final ? '<button data-date="' + esc(d.date) + '">' + inner + '<em>' + fwd() + '</em></button>' : '<div>' + inner + '</div>') + '</li>';
  }
  return h + '</ol>';
}

/* ---------- Free play ---------- */
const timerName = (s) => s ? T("prof.timer_s", { n: fmt(s) }) : T("prof.no_timer");
function freeHTML(f) {
  if (!f || !f.games) return '<p class="sh-wait">' + esc(T("prof.free_none")) + '</p>';
  let h = '<div class="ptiles">' +
    tile(T("prof.free_games"), fmt(f.games)) +
    tile(T("prof.free_places"), fmt(f.places)) +
    tile(T("prof.free_avg"), f.avg != null ? fmt(f.avg) : none, esc(T("prof.free_avg_note"))) +
    tile(T("prof.median"), f.median_km != null ? esc(kmT(f.median_km)) : none) +
    (f.duels ? tile(T("prof.duels"), fmt(f.duels.won) + ' : ' + fmt(f.duels.lost), esc(T("prof.duels_note"))) : "") +
    (f.beaten != null ? tile(T("prof.beaten"), esc(profPct(f.beaten))) : "") +
    '</div>';
  if (f.records.length) h += section(T("prof.free_records"), '<ul class="precs">' + f.records.map((r) =>
    '<li><span>' + esc(timerName(r.seconds)) + ' · ' + esc(T("prof.rounds", { n: fmt(r.rounds) })) + '</span><b>' + fmt(r.points) + '</b></li>').join("") + '</ul>');
  if (f.streaks.length) h += section(T("prof.free_streaks"), '<ul class="precs">' + f.streaks.map((s) =>
    '<li><span>' + esc(timerName(s.seconds)) + '</span><b>' + fmt(s.streak) + '</b></li>').join("") + '</ul>');
  return h;
}

/* ---------- Settings ---------- */
function settingsHTML(d) {
  if (!d.settings) return "";
  let h = '<label class="pswitch"><input type="checkbox" id="hideMe"' + (d.settings.hide_me ? " checked" : "") + '><span><b>' + esc(T("prof.hide_me")) + '</b>' +
    '<small>' + esc(T("prof.hide_me_note")) + '</small></span></label>' + publicHTML(d.settings);
  // Linked accounts live on the accounts screen; the profile only points there.
  if (typeof openAccounts === "function") h += '<button class="pbtn wide" id="profAccounts">' + esc(T("prof.accounts")) + ' ' + fwd() + '</button>';
  return section(T("prof.settings"), h);
}
async function saveHide(box) {
  haptic("select");
  box.disabled = true;
  try { PROF.settings = await api("/api/profile/settings", { hide_me: box.checked }); } catch (e) { toast(errorText(e)); }
  redrawSettings("hideMe");
}
// The settings drawn again from PROF, after a change and when a copy of the profile put aside comes back; the focus
// goes back to the control named.
function redrawSettings(focus) {
  const set = $("hideMe") && $("hideMe").closest(".psec");
  if (!set || !PROF || !PROF.settings) return;
  set.outerHTML = settingsHTML(PROF);
  bindSettings();
  const to = focus && $(focus);
  if (to) to.focus({ preventScroll: true });
}
document.addEventListener("click", (e) => {
  if (e.target.closest("#profAccounts") && typeof openAccounts === "function") openAccounts();
});

/* ---------- Public page ---------- */
// The link to a page anyone may open: on, off, or a new one, which ends the old.
const publicURL = (path) => location.origin + path;
// While the player is hidden the page does not open: the switch waits, and the note on hiding above says so.
function publicHTML(set) {
  let h = '<label class="pswitch' + (set.hide_me ? ' held' : '') + '"><input type="checkbox" id="pubOn"' + (set.public ? " checked" : "") + (set.hide_me ? " disabled" : "") + '><span><b>' + esc(T("prof.public")) + '</b>' +
    '<small>' + esc(T("prof.public_note")) + '</small></span></label>';
  if (set.public && !set.hide_me) h += '<div class="plink"><input id="pubUrl" readonly dir="ltr" value="' + esc(publicURL(set.public)) + '" aria-label="' + esc(T("prof.public")) + '">' +
    '<button class="pbtn" id="pubCopy">' + esc(T("prof.copy")) + '</button><button class="pbtn" id="pubNew">' + esc(T("prof.public_new")) + '</button></div>';
  return h;
}
// A second tap while the first is on its way would end the link just made.
async function savePublic(on, renew, from) {
  haptic("select");
  ["pubOn", "pubNew", "pubCopy"].forEach((id) => { if ($(id)) $(id).disabled = true; });
  try {
    PROF.settings = await api("/api/profile/public", { on });
    if (renew) toast(T("prof.public_renewed"));
  } catch (e) { toast(errorText(e)); }
  redrawSettings($(from) ? from : "pubOn");
}
function copyPublic() {
  const url = $("pubUrl");
  if (!url) return;
  const pick = () => { url.focus(); url.select(); };
  if (navigator.clipboard) navigator.clipboard.writeText(url.value).then(() => toast(T("prof.copied")), pick);
  else pick();
}

// A public page, as anyone with its link sees it: the closed days, without pins, history or settings.
async function openPublic(slug) {
  const ticket = sheet(esc(T("prof.title")), wait(), null, leavePage);
  let d;
  try { d = await reply(await get("/api/p/" + encodeURIComponent(slug) + "?lang=" + encodeURIComponent(LANG))); } catch (e) {
    if (ticket === SHEET) sheet(esc(T("prof.title")), '<p class="sh-wait">' + esc(e.message === "not_found" ? T("prof.public_gone") : errorText(e)) + '</p>', null, leavePage);
    return;
  }
  if (ticket === SHEET) drawProfile(Object.assign(d, { pub: true }), null, "public");
}

/* ---------- Mini profile ---------- */
// A tap on a name: what anyone who sees the name may know. Under the map the table's own taps stay as they were.
// In the sheets a name is a button for the keyboard too: Enter or Space opens it.
const whoOf = (e) => { const w = e.target.closest && e.target.closest("[data-who]"); return w && !w.closest("#lb") ? w : null; };
document.addEventListener("click", (e) => {
  const w = whoOf(e);
  if (!w) return;
  e.preventDefault(); e.stopPropagation();
  openMini(w.dataset.who);
}, true);
document.addEventListener("keydown", (e) => {
  const w = (e.key === "Enter" || e.key === " ") && whoOf(e);
  if (!w) return;
  e.preventDefault(); e.stopPropagation();
  openMini(w.dataset.who);
}, true);

async function openMini(handle) {
  if (!handle) return;
  haptic("light");
  const saved = keepSheet(), back = backTo(saved);
  const ticket = sheet(esc(T("prof.title")), wait(), back);
  $("sheet").dataset.kind = "mini";
  let d;
  try { d = await getJSON("/api/profile/who?id=" + encodeURIComponent(handle)); } catch (e) {
    if (ticket === SHEET) sheet(esc(T("prof.title")), '<p class="sh-wait">' + esc(errorText(e)) + '</p>', back);
    return;
  }
  if (ticket !== SHEET) return;
  // Your own name opens your whole profile.
  if (d.me) { SHEET++; return openProfile(saved); }
  let h = '<div class="prof mini"><div class="pcard">' + av(d.name || "?", false, d.photo) + '<div><b lang="' + nameLang() + '" dir="auto">' + esc(d.name) + '</b><span class="up">' + esc(T("from_tg")) + '</span></div></div>';
  if (d.duel) h += '<div class="pbig"><b>' + esc(T("prof.duel", { closer: fmt(d.duel.closer), of: fmt(d.duel.of) })) + '</b></div>';
  h += '<div class="ptiles">' +
    tile(T("prof.days"), fmt(d.days)) +
    tile(T("prof.streak"), fmt(d.streak)) +
    tile(T("prof.avg30"), d.avg30 != null ? fmt(d.avg30) : none) +
    tile(T("prof.best_place"), d.best_place ? placeOf(d.best_place) : none, d.best_place ? dayLink(d.best_place.day, d.best_place.date) : "") +
    tile(T("prof.countries"), fmt(d.countries)) +
    '</div>';
  if (d.free) {
    h += section(T("prof.tab_free"), '<div class="ptiles">' + tile(T("prof.free_avg"), d.free.avg != null ? fmt(d.free.avg) : none, esc(T("prof.free_avg_note"))) +
      (d.free.duels ? tile(T("prof.duels"), fmt(d.free.duels.won) + ' : ' + fmt(d.free.duels.lost), esc(T("prof.duels_note"))) : "") + '</div>');
  }
  sheet(esc(T("prof.title")), h + '</div>', back);
  $("sheet").dataset.kind = "mini";
}
