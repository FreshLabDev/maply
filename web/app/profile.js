/* ---------- Profile ---------- */
// The player's own profile opens from the letter at the end of the header; the logo still opens the days. Any element
// with data-who="<handle>" opens that player's mini profile: the server puts a handle next to a name only, so a row
// without a name has nothing to tap. Charts are drawn here in SVG, without a library.
// PROF is the player's own profile, VIEW the one on show: the own one or a public page.
let PROF = null, VIEW = null, PROF_TAB = "ranked";
try { PROF_TAB = localStorage.getItem("maply.prof.tab") === "free" ? "free" : "ranked"; } catch {}

// The letter in the header: the Telegram name, the name typed on the site, or a figure for a guest without one.
const FIGURE = '<svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true"><circle cx="8" cy="5.2" r="3" fill="currentColor"/><path d="M2 15c0-3.4 2.7-5.6 6-5.6s6 2.2 6 5.6z" fill="currentColor"/></svg>';
// A player who hid themselves ("Hide me") is "Anonymous" in shared tables, with this plain avatar instead of a photo or
// a letter, and with it instead of the Telegram mark on the days page.
const anonAv = () => '<i class="av nobody">' + FIGURE + '</i>';
const SRC_ANON = '<svg class="src" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="12" fill="#59826B"/><circle cx="12" cy="9.3" r="3.6" fill="#D8E1DC"/><path d="M5.6 19.4c.6-3.4 3.2-5.6 6.4-5.6s5.8 2.2 6.4 5.6" fill="#D8E1DC"/></svg>';
function meLetter(name) {
  const n = String(name || (inTg && tg.initDataUnsafe.user && tg.initDataUnsafe.user.first_name) || (SITE ? siteName() : "")).trim();
  $("meBtn").innerHTML = n ? esc(initial(n)) : FIGURE;
}
meLetter();
// The profile is the page /me and a public profile /p/<slug>: the letter goes to /me, and × goes back to the page before.
$("meBtn").addEventListener("click", () => Router.go("/me"));
const leavePage = () => Router.back("/");
// Back from a day comes to the profile where it was; any other way in starts at the top.
Router.screen("me", /^\/me$/, { open(m, from) { if (from !== "days") PROF_AT = null; openProfile(); }, close() { closeDays(); } });
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

// From the router, or from a mini profile that is the viewer's own, over the sheet it came from. Anything else, the
// avatar's menu say, goes through the address.
async function openProfile(saved) {
  if (!saved && Router.current !== "me") { Router.go("/me"); return; }
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
  profileAt();
}

function showProfile() { drawProfile(PROF, backTo(PROF.saved), "profile"); }
function drawProfile(d, back, kind) {
  VIEW = d;
  sheet(esc(T("prof.title")), '<div class="prof">' + cardHTML(d) + tabsHTML() + '<div id="profTab"></div></div>', back, leavePage);
  $("sheet").dataset.kind = kind;
  $("sheet").querySelectorAll(".ptabs button").forEach((b) => { b.onclick = () => { haptic("select"); setTab(b.dataset.tab); }; });
  $("sheet").querySelector(".ptabs").onkeydown = tabKey;
  const signIn = $("profSignIn");
  if (signIn) signIn.onclick = () => acctDialog();
  setTab(PROF_TAB, true);
}

function cardHTML(d) {
  const name = d.name || (d.guest ? T("guest") : d.site ? T("acct.google_account") : "");
  let h = '<div class="pcard">' + av(name || "?", true) + '<div><b lang="' + nameLang() + '" dir="auto">' + esc(name) + '</b>' +
    '<span class="up">' + esc(T(d.guest || d.site ? "from_site" : "from_tg")) + '</span></div></div>';
  if (d.guest) {
    // The sign-in comes from the accounts' dialog; a site without accounts has the note alone.
    const btn = typeof acctDialog === "function" && typeof ME !== "undefined" && ME ? '<button class="pbtn" id="profSignIn">' + esc(T("prof.sign_in")) + '</button>' : "";
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
  box.innerHTML = tab === "free" ? freeHTML(VIEW.free, VIEW.top) : rankedHTML(VIEW);
  if (tab === "ranked") bindRanked(box);
  // A tab chosen from far down the other one brings the tabs back into view, not the panel's top under the header.
  const tabs = $("sheet").querySelector(".ptabs"), body = $("sheet").querySelector(".sh-body");
  if (!first && tabs && body && tabs.getBoundingClientRect().top < body.getBoundingClientRect().top) tabs.scrollIntoView({ block: "start" });
}

/* ---------- Numbers ---------- */
const tile = (label, value, sub, cls) => ({ cls, html: '<span class="up">' + esc(label) + '</span><b>' + value + '</b>' + (sub ? '<small>' + sub + '</small>' : "") });
// Tiles in rows as even as they come, so no row ends with a lone tile: two to a row on a phone, an odd one first and
// across; up to four from 560 px; up to eight on a computer's own profile. Each tile carries how many share its row
// at each width (--a, --b, --c), and profile.css makes it that share of the row. Missing tiles are false and left out.
function tilesHTML(list, cls) {
  list = list.filter(Boolean);
  const n = list.length, row = (max) => {
    // The rows with fewer tiles come first: a lone one opens the group, across the width.
    const rows = Math.ceil(n / max), less = Math.floor(n / rows), more = n % rows, k = [];
    for (let r = 0; r < rows; r++) { const m = less + (r >= rows - more ? 1 : 0); for (let i = 0; i < m; i++) k.push(m); }
    return k;
  };
  const a = row(2), b = row(4), c = row(8);
  return '<div class="ptiles' + (cls ? " " + cls : "") + '">' + list.map((t, i) => '<div class="pt' + (t.cls ? " " + t.cls : "") +
    '" style="--a:' + a[i] + ';--b:' + b[i] + ';--c:' + c[i] + '">' + t.html + '</div>').join("") + '</div>';
}
const none = "–";
// The same percent the translations write, "47%", "47 %" or "%47", and not Intl's, which differs from them in ru, uk, es.
const profPct = (n) => T("prof.pct", { pct: fmt(n) });
const profDate = (iso) => { try { return new Intl.DateTimeFormat(numLang(), { day: "numeric", month: "short", timeZone: "UTC" }).format(new Date(iso + "T00:00:00Z")); } catch { return iso; } };
const dayLink = (day, date) => '#' + fmt(day) + ' · ' + esc(profDate(date));
const placeOf = (p) => esc(T("prof.place_of", { rank: fmt(p.rank), of: fmt(p.of) }));
// A flag from the files the days page uses (days.js), the same on every system; a code without a file is its letters.
const profFlag = (iso) => !iso ? "" : typeof FLAGS !== "undefined" && FLAGS.has(iso)
  ? '<img class="pflag" src="' + FLAG_DIR + iso + '.svg" alt="">'
  : '<i class="pflag code">' + esc(iso) + '</i>';

function rankedHTML(d) {
  const r = d.ranked;
  if (!r.days) return '<p class="sh-wait">' + esc(T(d.guest || d.site || d.pub ? "prof.no_days_guest" : "prof.no_days")) + '</p>' + settingsHTML(d);
  let h = '';
  if (r.percentile != null) h += '<div class="pbig"><b>' + esc(T("prof.better", { pct: fmt(r.percentile) })) + '</b><span>' + esc(T("prof.better_note")) + '</span></div>';
  // The month's season so far stands with the numbers, its note under them all.
  const season = (d.season || []).map((p) => tile(T(p.kind === "group" ? "prof.season_group" : "prof.season_world"), placeOf(p),
    esc(T("prof.season_points", { points: fmt(p.points) })), p.rank === 1 ? "gold" : ""));
  h += tilesHTML([
    tile(T("prof.points"), fmt(r.points)),
    tile(T("prof.avg30"), r.avg30 != null ? fmt(r.avg30) : none),
    tile(T("prof.days"), fmt(r.days)),
    tile(T("prof.streak"), fmt(r.streak), esc(T("prof.best_streak", { n: fmt(r.best_streak) }))),
    tile(T("prof.median"), r.median_km != null ? esc(kmT(r.median_km)) : none),
    tile(T("prof.found"), esc(profPct(r.country_pct)), esc(T("prof.found_city", { pct: fmt(r.city_pct) }))),
    tile(T("prof.best_day"), r.best_day ? fmt(r.best_day.points) : none, r.best_day ? dayLink(r.best_day.day, r.best_day.date) : "", "gold"),
    tile(T("prof.best_place"), r.best_place ? placeOf(r.best_place) : none, r.best_place ? dayLink(r.best_place.day, r.best_place.date) : "", r.best_place && r.best_place.rank === 1 ? "gold" : ""),
    ...season,
  ], season.length ? "pseason" : "");
  if (season.length) h += '<p class="pnote-s pseason-n">' + esc(T("prof.season_hint")) + '</p>';
  h += '<div class="pcharts">';
  h += section(T("prof.chart_points"), seriesSVG(r.series, r.median30));
  h += section(T("prof.chart_misses"), missesSVG(r.misses));
  h += section(T("prof.chart_continents"), continentsHTML(r));
  // One map of the world: the found countries in gold and, for the owner, their pins over them. A public page has
  // no pins and no history.
  h += section(T("prof.passport"), passportHTML(r.passport, r.history), "wide");
  h += '</div>';
  // The settings come before the history, which grows by a row a day.
  return h + settingsHTML(d) + (r.history ? section(T("prof.history"), historyHTML(r.history)) : "");
}
const section = (title, body, cls) => '<section class="psec' + (cls ? " " + cls : "") + '"><h3 class="up">' + esc(title) + '</h3>' + body + '</section>';

function bindRanked(box) {
  box.querySelectorAll("[data-date]").forEach((e) => {
    const go = () => openDayFrom(e.dataset.date, e.tagName.toLowerCase());
    e.addEventListener("click", go);
    if (e.tagName !== "BUTTON") e.addEventListener("keydown", (k) => { if (k.key === "Enter" || k.key === " ") { k.preventDefault(); go(); } });
  });
  box.querySelectorAll(".pmap.world").forEach((svg) => { fitPins(svg); if (PIN_FIT) PIN_FIT.observe(svg); });
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

// A closed day's page, by its address; its Back comes back to the profile, where it was, with the focus on what was
// tapped: the row or the pin. Only a day over everywhere has one.
let PROF_AT = null;
function openDayFrom(date, tag) {
  const body = $("sheet").querySelector(".sh-body");
  PROF_AT = { date, tag, scroll: body ? body.scrollTop : 0 };
  Router.go("/days/" + date);
}
function profileAt() {
  const at = PROF_AT, body = $("sheet").querySelector(".sh-body");
  PROF_AT = null;
  if (!at || !body || PROF_TAB !== "ranked") return;
  body.scrollTop = at.scroll;
  const el = body.querySelector(at.tag + '[data-date="' + at.date + '"]');
  if (el) el.focus({ preventScroll: true });
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

const PROFILE_LAND_STEP = () => typeof PROFILE_LAND === "undefined" ? 2 : PROFILE_LAND.step;

// The passport: the world's land dots, a found country's in gold, the owner's pins over them, then the flags and the
// two stamps. A public page and a stranger have no pins.
function passportHTML(p, history) {
  // A found country's dots are drawn larger: a small country has only a few.
  const found = new Set(p.countries), dot = (PROFILE_LAND_STEP() * .62).toFixed(2), big = (PROFILE_LAND_STEP() * 1.05).toFixed(2);
  let a = "", b = "";
  for (const [x, y, c] of landDots()) {
    if (y < 4 || y > 154) continue;
    const s = "M" + x.toFixed(1) + " " + y.toFixed(1) + "h0";
    if (found.has(c)) b += s; else a += s;
  }
  const pins = history && history.length ? history : null, n = T("prof.passport_n", { n: fmt(p.countries.length) });
  let h = '<p class="pbig-s">' + esc(n) + '</p>';
  h += '<svg class="pmap world" viewBox="0 4 360 150" role="' + (pins ? "group" : "img") + '" aria-label="' + esc(pins ? T("prof.chart_map") + " · " + n : n) + '">' +
    '<path class="land" stroke-width="' + dot + '" d="' + a + '"/><path class="land gold" stroke-width="' + big + '" d="' + b + '"/>' + (pins ? pinsSVG(pins) : "") + '</svg>';
  if (pins) {
    const legend = PIN_BINS.map(([top, c], i) => '<span><i class="' + c + '"></i>' + esc(i < PIN_BINS.length - 1 ? T("prof.upto", { km: kmT(top) }) : T("prof.further")) + '</span>').join("");
    h += '<p class="plegend">' + legend + '</p><p class="pnote-s">' + esc(T("prof.map_hint")) + '</p>';
  }
  if (p.countries.length) h += '<p class="pflags">' + p.countries.map((c) => '<span title="' + esc(regionName(c)) + '">' + profFlag(c) + '<small>' + esc(regionName(c)) + '</small></span>').join("") + '</p>';
  else h += '<p class="pnote-s">' + esc(T("prof.passport_hint")) + '</p>';
  const stamp = (on, key) => '<span class="pstamp' + (on ? " on" : "") + '">' + esc(T(key)) + '</span>';
  return h + '<p class="pstamps">' + stamp(p.streak30, "prof.stamp_streak30") + stamp(p.city1km, "prof.stamp_city1km") + '</p>';
}
// The pins on the passport's world, closer ones on top so a good day is not buried under a far one. A pin of a closed
// day opens it. A pin near a pole stays inside the frame, ring and all, on a phone 320 px wide too.
function pinsSVG(history) {
  return history.slice().sort((p, q) => q.km - p.km).map((d) => {
    const label = '#' + d.day + ' · ' + kmT(d.km) + ' · ' + fmt(d.points);
    const at = 'cx="' + (d.lon + 180).toFixed(2) + '" cy="' + Math.min(Math.max(90 - d.lat, 12), 146).toFixed(2) + '"';
    const part = (k, cls) => '<circle class="pin ' + k + (cls ? " " + cls : "") + '" ' + at + ' r="' + (PIN_R[k] / PIN_PHONE).toFixed(2) + '"/>';
    const pin = part("edge", pinClass(d.km)) + part("pp", pinClass(d.km));
    return d.final
      ? '<g class="go" data-date="' + esc(d.date) + '" tabindex="0" role="button" aria-label="' + esc(label) + '"><title>' + esc(label) + '</title>' +
        part("hit") + part("ring") + part("gap") + pin + '</g>'
      : '<g><title>' + esc(label) + '</title>' + pin + '</g>';
  }).join("");
}
// A pin keeps its size in pixels however wide the map is drawn, on a phone and across a computer's page: the radii of
// its parts are in pixels, the target for the finger, the ring of the keyboard's focus and its gap, the edge that
// parts it from the land and its colour, and follow the map's scale whenever its width changes. Drawn, they are a
// phone's: a map 343 px wide.
const PIN_R = { hit: 12, ring: 8.5, gap: 6.5, edge: 5.5, pp: 4 }, PIN_PHONE = 343 / 360;
const PIN_FIT = typeof ResizeObserver === "function" ? new ResizeObserver((all) => all.forEach((e) => fitPins(e.target))) : null;
function fitPins(svg) {
  const m = svg.getScreenCTM(), a = m && m.a;
  if (!a) return;
  svg.querySelectorAll("circle.pin").forEach((c) => c.setAttribute("r", (PIN_R[c.classList[1]] / a).toFixed(2)));
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
function freeHTML(f, top) {
  if (!f || !f.games) return '<p class="sh-wait">' + esc(T("prof.free_none")) + '</p>';
  let h = tilesHTML([
    tile(T("prof.free_games"), fmt(f.games)),
    tile(T("prof.free_places"), fmt(f.places)),
    tile(T("prof.free_avg"), f.avg != null ? fmt(f.avg) : none, esc(T("prof.free_avg_note"))),
    tile(T("prof.median"), f.median_km != null ? esc(kmT(f.median_km)) : none),
    f.duels && tile(T("prof.duels"), fmt(f.duels.won) + ' : ' + fmt(f.duels.lost), esc(T("prof.duels_note"))),
    f.beaten != null && tile(T("prof.beaten"), esc(profPct(f.beaten))),
  ]) + topHTML(top);
  if (f.records.length) h += section(T("prof.free_records"), '<ul class="precs">' + f.records.map((r) =>
    '<li><span>' + esc(timerName(r.seconds)) + ' · ' + esc(T("prof.rounds", { n: fmt(r.rounds) })) + '</span><b>' + fmt(r.points) + '</b></li>').join("") + '</ul>');
  if (f.streaks.length) h += section(T("prof.free_streaks"), '<ul class="precs">' + f.streaks.map((s) =>
    '<li><span>' + esc(timerName(s.seconds)) + '</span><b>' + fmt(s.streak) + '</b></li>').join("") + '</ul>');
  return h;
}

// The player's place on each board of free play they are on, all time; each opens its board (leaders.js).
function topHTML(top) {
  if (!top || !top.length || !window.Leaders) return "";
  return section(T("lead.title"), '<ul class="precs lead-places">' + top.map((p) =>
    '<li><button class="lead-at" data-b="' + esc(p.board) + '" data-s="' + p.seconds + '" data-r="' + (p.rounds || 0) + '"><span>' + esc(Leaders.label(p)) +
    '<small>' + esc(T(p.board === "streak" ? "lead.unit_streak" : p.board === "timed" ? "lead.unit_timed" : "lead.unit_points")) + ': ' + fmt(p.score) + '</small></span>' +
    '<b' + (p.rank === 1 ? ' class="gold"' : '') + '>' + placeOf(p) + '</b></button></li>').join("") + '</ul>');
}
document.addEventListener("click", (e) => {
  const a = e.target.closest && e.target.closest(".lead-at");
  if (!a || !window.Leaders) return;
  haptic("light");
  Leaders.open({ board: a.dataset.b, seconds: Number(a.dataset.s), rounds: Number(a.dataset.r) });
});

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
// The accounts page opens over the profile, and its Back brings the profile back as it was: the address stays /me.
document.addEventListener("click", (e) => {
  if (!e.target.closest("#profAccounts") || typeof openAccounts !== "function") return;
  const saved = keepSheet();
  openAccounts(() => restoreSheet(saved));
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
  h += tilesHTML([
    tile(T("prof.days"), fmt(d.days)),
    tile(T("prof.streak"), fmt(d.streak)),
    tile(T("prof.avg30"), d.avg30 != null ? fmt(d.avg30) : none),
    tile(T("prof.best_place"), d.best_place ? placeOf(d.best_place) : none, d.best_place ? dayLink(d.best_place.day, d.best_place.date) : ""),
    tile(T("prof.countries"), fmt(d.countries)),
  ]);
  if (d.free) {
    h += section(T("prof.tab_free"), tilesHTML([tile(T("prof.free_avg"), d.free.avg != null ? fmt(d.free.avg) : none, esc(T("prof.free_avg_note"))),
      d.free.duels && tile(T("prof.duels"), fmt(d.free.duels.won) + ' : ' + fmt(d.free.duels.lost), esc(T("prof.duels_note")))]));
  }
  sheet(esc(T("prof.title")), h + '</div>', back);
  $("sheet").dataset.kind = "mini";
}
