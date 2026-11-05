/* ---------- Days ---------- */
// The days page, the same in Telegram and on the site: every day newest first as a diary, a page at a time as the list
// scrolls. A row shows the day's strip with its country's flag, the place and the viewer's result. A day still open in
// some zone shows only how many have answered and when it closes, with its puzzle where the viewer plays it. A closed
// day opens its page: the strip, the viewer's result, the world's numbers, everyone's misses on one scale, the answers
// on a map and the whole ranking. On a phone the page takes the list's place; on a wide screen they stand side by side.

// The flags maply serves, by ISO code: country-flag-icons, 3:2. A code without a file is written in its letters.
const FLAG_DIR = "/vendor/country-flag-icons-1.6.20/3x2/";
const FLAGS = new Set(("AC AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY " +
  "BZ CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ DE DJ DK DM DO DZ EC EE EG EH ER ES ET EU FI FJ FK FM FO FR GA GB " +
  "GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY HK HM HN HR HT HU IC ID IE IL IM IN IO IQ IR IS IT JE JM JO JP KE KG KH KI " +
  "KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ NA " +
  "NC NE NF NG NI NL NO NP NR NU NZ OM PA PE PF PG PH PK PL PM PN PR PS PT PW PY QA RE RO RS RU RW SA SB SC SD SE SG SH SI SJ SK " +
  "SL SM SN SO SR SS ST SV SX SY SZ TA TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ UA UG UM US UY UZ VA VC VE VG VI VN VU WF " +
  "WS XA XC XK XO YE YT ZA ZM ZW").split(" "));
const flag = (iso) => FLAGS.has(iso) ? '<img class="fl" src="' + FLAG_DIR + iso + '.svg" alt="">'
  : '<span class="fl code">' + esc(String(iso || "").slice(0, 3)) + '</span>';

const dateOf = (iso) => new Date(iso + "T00:00:00Z");
const dayPart = (iso, opts) => { try { return new Intl.DateTimeFormat(numLang(), Object.assign({ timeZone: "UTC" }, opts)).format(dateOf(iso)); } catch { return ""; } };
// A distance on a tile, whose label says it is in kilometres: one decimal under ten.
const kmNum = (km) => numIn(LANG, km, km < 10 ? 1 : 0);
const pctT = (pct) => { try { return new Intl.NumberFormat(numLang(), { style: "percent" }).format(pct / 100); } catch { return pct + "%"; } };
const bwd = () => fwd() === "→" ? "←" : "→";
// A day's or a rank's number keeps its # on its left in Arabic too: after an Arabic letter the digits are Arabic
// numbers, which the # does not join, so the number is isolated as left-to-right.
const no = (s) => String(s).replace(/#[\d.,\u00a0\u202f\u066b\u066c]*\d/g, (m) => "\u2066" + m + "\u2069");
const TN = (key, vars) => no(T(key, vars));

// DAYS is the list as it was read, page after page; DAY the page open beside or instead of it.
let DAYS = null, DAY = null, dayTicket = 0;

// The list is the page /days and a day is /days/<n>, or /days/<date> as the first addresses had it. The sheet's own
// Back goes from a day to the list and from the list to the page before it.
Router.screen("days", /^\/days(?:\/(\d{1,6}|\d{4}-\d{2}-\d{2}))?$/, {
  open(m) {
    const key = m[1] || "";
    closeView();
    if (!daysShown()) openDays(key);
    else if (key && DAY && (key === String(DAY.day) || key === DAY.date)) return;
    else if (key) openDay(key);
    else { dropDay(); if (DAY && !wide()) closeDay(); }
  },
  close() { closeView(); dropDay(); DAY = null; DAYS = null; closeDays(); },
});
// The sheet is closed by hiding it, so the list's page stays in it until the next sheet.
const daysShown = () => $("sheet").classList.contains("on") && !!$("dPage");
// A day on its way is not shown once the address has moved on.
function dropDay() {
  dayTicket++;
  const r = $("dRows");
  if (r) r.querySelectorAll(".dr.busy").forEach((b) => { b.classList.remove("busy"); b.removeAttribute("aria-busy"); });
}
const leaveDays = () => Router.back("/");
const toList = () => Router.back("/days");

async function openDays(key) {
  haptic("light");
  const ticket = sheet(esc(T("days")), '<p class="sh-wait up">' + esc(lines("loading")[0]) + '</p>', null, leaveDays);
  let data;
  try { data = await getJSON("/api/days"); } catch (e) { if (ticket === SHEET) sheet(esc(T("days")), '<p class="sh-wait">' + esc(errorText(e)) + '</p>', null, leaveDays); return; }
  if (ticket !== SHEET) return;
  DAYS = { days: data.days || [], next: data.next || "", best: data.best || "", scroll: 0, more: false };
  showDays();
  // A wide screen has room for a day beside the list: the one asked for, or the newest closed one.
  const first = DAYS.days.find((d) => d.final);
  if (key) openDay(key);
  else if (wide() && first) openDay(String(first.day), true);
}
const wide = () => matchMedia("(min-width: 1024px)").matches;

// The sheet's title and back button, changed without drawing the sheet again.
function sheetHead(title, backTo) {
  $("shTitle").innerHTML = title;
  const b = $("shBack");
  b.textContent = backTo ? bwd() : "×";
  b.onclick = backTo || leaveDays;
}

function showDays() {
  let html = '<div class="days" id="days"><div class="dl" id="dList">';
  if (SITE) html += '<label class="sh-name"><span class="up">' + esc(T("your_name")) + '</span><input id="shName" lang="' + nameLang() + '" maxlength="24" autocomplete="nickname" placeholder="' + esc(T("guest")) + '" value="' + esc(siteName()) + '"></label>';
  if (!DAYS.days.length) html += '<p class="sh-wait">' + esc(T("no_days")) + '</p>';
  html += '<div id="dRows">' + rows(DAYS.days, null) + '</div>';
  if (DAYS.next) html += '<p class="dmore up" id="dMore">' + esc(lines("loading")[0]) + '</p>';
  html += '</div><div class="dp" id="dPage"></div></div>';
  sheet(esc(T("days")), html, null, leaveDays);
  $("sheet").dataset.kind = "days";
  const body = $("sheet").querySelector(".sh-body");
  body.scrollTop = DAYS.scroll;
  $("dRows").addEventListener("click", onRow);
  watchMore(body);
  const name = $("shName");
  if (name) name.onchange = async () => {
    try {
      localStorage.setItem(NAME, name.value.trim());
      await reply(await get("/api/site/name", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name: name.value }) }));
      toast(T("name_saved"));
    } catch (e) { toast(errorText(e)); }
  };
}

// rows draws days as the list's rows, with the month's name above the first day of each month. after is the date of
// the row above the first, so a page read later does not name its month again.
function rows(days, after) {
  let html = "", month = after ? after.slice(0, 7) : "";
  const year = new Date(serverNow()).getUTCFullYear();
  for (const d of days) {
    if (d.date.slice(0, 7) !== month) {
      month = d.date.slice(0, 7);
      const y = +month.slice(0, 4);
      html += '<p class="up dmon">' + esc(dayPart(d.date, y === year ? { month: "long" } : { month: "long", year: "numeric" })) + '</p>';
    }
    html += d.final ? closedRow(d) : openRow(d);
  }
  return html;
}

const dayCell = (d) => '<span class="d"><b>' + esc(dayPart(d.date, { day: "numeric" })) + '</b><i>' + esc(dayPart(d.date, { weekday: "short" })) + '</i></span>';

function closedRow(d) {
  const best = d.date === DAYS.best, nobody = !d.players;
  const country = d.country ? regionName(d.country) : "";
  const title = d.city || country || TN("days.day_n", { n: fmt(d.day) });
  const th = d.thumb ? '<span class="th"><img class="st" src="' + esc(d.thumb) + '" alt="" loading="lazy" decoding="async">' + flag(d.country) + '</span>'
    : '<span class="th ph">' + flag(d.country) + '</span>';
  let r;
  if (d.me) {
    r = '<b>' + fmt(d.me.points) + '</b><span><i class="km">' + esc(kmT(d.me.km)) + ' · </i>' + esc(TN("days.rank_of", { rank: fmt(d.me.rank), n: fmt(d.players) })) + '</span>' +
      (best ? '<small class="star">' + esc(T("days.best")) + '</small>' : '');
  } else {
    r = '<b>–</b><span>' + esc(T("days.no_answer")) + '</span>' + (nobody ? '' : '<small>' + esc(T("days.played", { n: fmt(d.players) })) + '</small>');
  }
  const sub = nobody ? T("days.nobody") : d.median_km != null ? T("days.median", { km: kmT(d.median_km) }) : "";
  return '<button class="dr' + (best ? ' best' : '') + (d.me ? '' : ' miss') + '" data-n="' + d.day + '" data-date="' + esc(d.date) + '">' + dayCell(d) + th +
    '<span class="t"><b>' + esc(title) + '</b><span>' + esc(d.city ? country : "") + '</span><small>' + esc(sub) + '</small></span>' +
    '<span class="r">' + r + '</span></button>';
}

// An open day: the puzzle where the viewer plays it, a question mark for the flag, how many have answered, and how
// long until the day closes everywhere.
function openRow(d) {
  const pic = d.image ? ' style="background-image:url(&quot;' + esc(d.image) + '&quot;)"' : '';
  const th = '<span class="th ' + (d.image ? 'pz' + (d.kind === "panorama" ? ' pano' : '') : 'ph') + '"' + pic + '><span class="q">?</span></span>';
  let r = '';
  const today = TODAY && d.date === TODAY.date;
  if (d.played || today && TODAY.answer) r = '<small>' + esc(T("sent")) + '</small>';
  else if (today && secondsTo(TODAY.deadline) > 0) r = '<button class="play" data-play="1">' + esc(T("days.play")) + '</button>';
  else if (today) r = '<small>' + esc(T("days.closed")) + '</small>';
  return '<div class="dr open">' + dayCell(d) + th + '<span class="t"><b>' + esc(TN("days.day_n", { n: fmt(d.day) })) + '</b><span>' +
    esc(T("day_answered", { n: fmt(d.pending) })) + '</span><small class="live" data-at="' + esc(d.opens_at) + '">' + esc(T("days.result_in", { time: hm(secondsTo(d.opens_at)) })) +
    '</small></span><span class="r">' + r + '</span></div>';
}

function onRow(e) {
  if (e.target.closest("[data-play]")) { haptic("light"); Router.go("/"); return; }
  const b = e.target.closest(".dr[data-n]");
  // A wide screen keeps the list in view, so the days tapped there are one page: × leaves them, not the day before.
  if (b) Router.go("/days/" + b.dataset.n, { replace: wide() });
}

// The next page is read when the end of the list comes into view.
let moreWatch = null;
function watchMore(body) {
  if (moreWatch) moreWatch.disconnect();
  const end = $("dMore");
  if (!end || !("IntersectionObserver" in window)) return;
  moreWatch = new IntersectionObserver((es) => { if (es.some((x) => x.isIntersecting)) more(); }, { root: body, rootMargin: "400px 0px" });
  moreWatch.observe(end);
}
async function more() {
  const list = DAYS;
  if (!list || !list.next || list.more) return;
  list.more = true;
  const ticket = SHEET, last = list.days[list.days.length - 1], end = $("dMore");
  if (end) { end.textContent = lines("loading")[0]; end.onclick = null; }
  let data;
  try { data = await getJSON("/api/days?before=" + encodeURIComponent(list.next)); }
  catch (e) {
    list.more = false;
    // The end stays in view, so the observer would not ask again: a tap does.
    if (ticket === SHEET && list === DAYS && end) { end.textContent = errorText(e); end.onclick = more; }
    return;
  }
  list.more = false;
  if (list !== DAYS) return;
  const add = data.days || [];
  DAYS.days = DAYS.days.concat(add);
  DAYS.next = data.next || "";
  if (ticket !== SHEET || !$("dRows")) return;
  $("dRows").insertAdjacentHTML("beforeend", rows(add, last && last.date));
  if (!DAYS.next) { $("dMore").remove(); if (moreWatch) moreWatch.disconnect(); }
}

// The open rows count down to the day's close while the list is open.
setInterval(() => {
  if (!$("dRows")) return;
  $("dRows").querySelectorAll(".live[data-at]").forEach((s) => { s.textContent = T("days.result_in", { time: hm(secondsTo(s.dataset.at)) }); });
}, 30000);

/* ---------- A day's page ---------- */
// A later tap wins over an earlier one whose reply comes last; the tapped row says it is on its way. key is the day's
// number or its date. A day that cannot be shown leaves the list where it was, at its own address.
async function openDay(key, quiet) {
  if (!quiet) haptic("select");
  if (!daysShown()) return openDays(key);
  const ticket = ++dayTicket, at = SHEET, byDate = !/^\d+$/.test(key);
  const row = $("dRows").querySelector(byDate ? '.dr[data-date="' + esc(key) + '"]' : '.dr[data-n="' + key + '"]');
  $("dRows").querySelectorAll(".dr.busy").forEach((b) => { b.classList.remove("busy"); b.removeAttribute("aria-busy"); });
  if (row) { row.classList.add("busy"); row.setAttribute("aria-busy", "true"); }
  let d;
  try { d = await getJSON("/api/day?" + (byDate ? "date=" : "n=") + encodeURIComponent(key)); } catch (e) {
    if (row) { row.classList.remove("busy"); row.removeAttribute("aria-busy"); }
    if (ticket !== dayTicket || at !== SHEET) return;
    const opens = e.data && e.data.opens_at;
    toast(opens ? T("days.result_in", { time: hm(secondsTo(opens)) }) : e.message === "no_round" ? T("days.no_day") : errorText(e));
    // The address goes back to what is on the screen; a wide one shows the newest closed day beside the list. That
    // one is opened quietly, so its own failure ends here.
    Router.go(DAY ? "/days/" + DAY.day : "/days", { replace: true });
    const first = !DAY && !quiet && wide() && DAYS && DAYS.days.find((x) => x.final);
    if (first) openDay(String(first.day), true);
    return;
  }
  if (row) { row.classList.remove("busy"); row.removeAttribute("aria-busy"); }
  if (ticket !== dayTicket || at !== SHEET || !$("dPage")) return;
  DAY = d;
  showDay(d);
}

function showDay(d) {
  const days = $("days"), body = $("sheet").querySelector(".sh-body");
  if (!days.classList.contains("at-day") && !wide()) DAYS.scroll = body.scrollTop;
  days.classList.add("at-day");
  $("dRows").querySelectorAll(".dr.on").forEach((b) => b.classList.remove("on"));
  const row = $("dRows").querySelector('.dr[data-n="' + d.day + '"]');
  if (row) row.classList.add("on");
  $("dPage").innerHTML = dayHTML(d);
  // The neighbouring days take this one's place in the history: Back still goes to the list.
  $("dPage").querySelectorAll("[data-n]").forEach((b) => { b.onclick = () => Router.go("/days/" + b.dataset.n, { replace: true }); });
  $("dPage").querySelector(".p360").onclick = () => view360(d);
  const mapBtn = $("dMapBtn");
  if (mapBtn) mapBtn.onclick = () => { mapBtn.remove(); dayMap(d); };
  if (wide()) {
    dayMap(d);
    $("dPage").scrollTop = 0;
    if (row && row.scrollIntoView) row.scrollIntoView({ block: "nearest" });
  } else {
    body.scrollTop = 0;
    sheetHead(esc(TN("days.day_n", { n: fmt(d.day) })), toList);
  }
}

// Back from a day on a phone is the list, where it was scrolled.
function closeDay() {
  const days = $("days");
  if (!days) return;
  closeView();
  dropDay();
  days.classList.remove("at-day");
  $("dPage").innerHTML = "";
  DAY = null;
  sheetHead(esc(T("days")), null);
  $("sheet").querySelector(".sh-body").scrollTop = DAYS.scroll;
}

function dayHTML(d) {
  const P = d.place, country = P.country ? regionName(P.country) : "";
  const title = P.city || country || no("#" + d.day);
  const strip = d.thumb || "";
  let html = '<div class="hero2' + (strip ? '' : d.kind === "panorama" ? ' pano' : ' photo') + '"' +
    (strip ? '' : ' style="background-image:url(&quot;' + esc(d.image) + '&quot;)"') + '>' +
    (strip ? '<img class="st" src="' + esc(strip) + '" alt="">' : '') +
    '<button class="p360" aria-label="' + esc(d.kind === "panorama" ? T("days.view_pano") : T("days.view_photo")) + '">' + (d.kind === "panorama" ? "360°" : "⤢") + '</button>' +
    '<div class="in">' + flag(P.country) + '<b>' + esc(title) + '</b><p>' +
    esc([P.city ? country : "", no("#" + d.day), dayPart(d.date, { day: "numeric", month: "long" })].filter(Boolean).join(" · ")) + '</p></div></div>';
  html += '<div class="dpi">';
  const src = SOURCES[P.source];
  if (src) {
    const lic = (P.license || "").replace(/^CC-/, "CC ").replace(/-(\d)/, " $1");
    html += '<p class="attr">' + esc(T("days.photo", { who: [P.author, src[0], lic].filter(Boolean).join(" · ") })) + '</p>';
  }
  if (P.fact) html += '<p class="fact">' + esc(P.fact) + '</p>';
  html += '</div><div class="dps">';
  if (d.me) {
    const checks = d.me.bonus >= 1500 ? [T("days.country_ok"), T("days.city_ok")] : d.me.bonus >= 800 ? [T("days.country_ok")] : d.me.bonus >= 300 ? [T("days.continent_ok")] : [];
    html += '<div class="you"><b>' + fmt(d.me.points) + '</b><div><p class="k">' + esc(TN("days.you_rank", { rank: fmt(d.me.rank), n: fmt(d.players) })) +
      '</p><p>' + esc([kmT(d.me.km)].concat(checks).join(" · ")) + '</p></div></div>';
  } else {
    html += '<div class="you none"><p>' + esc(d.players ? T("no_answer") : T("days.nobody")) + '</p></div>';
  }
  html += '<div class="tiles"><div><b>' + fmt(d.players) + '</b><span>' + esc(T("days.t_played")) + '</span></div>' +
    '<div><b>' + (d.median_km != null ? kmNum(d.median_km) : "–") + '</b><span>' + esc(T("days.t_median")) + '</span></div>' +
    '<div><b>' + (d.best_km != null ? kmNum(d.best_km) : "–") + '</b><span>' + esc(T("days.t_best")) + '</span></div>' +
    '<div><b>' + (d.players ? pctT(d.country_pct) : "–") + '</b><span>' + esc(T("days.t_country")) + '</span></div></div>';
  if (d.players) html += axis(d.misses || [], d.me);
  html += '</div>';
  if (d.players) {
    html += '<div class="dmapbox"><button class="mapbtn" id="dMapBtn">' + esc(T("days.map")) + '</button><div class="dmap" id="dMapAt"></div></div>';
    html += '<div class="dpt"><p class="up dth">' + esc(T("days.table")) + '</p><ol class="rkl">';
    for (const r of d.ranking) {
      const name = r.me ? T("you") : nameOf(r);
      html += '<li' + (r.me ? ' class="me"' : '') + '><span class="n">' + fmt(r.rank) + '</span><span class="w"' + (r.me ? '' : ' lang="' + nameLang() + '"') + '>' +
        (r.site ? SRC_SITE : SRC_TG) + esc(name) + '</span><span class="k">' + kmT(r.km) + '</span><b>' + fmt(r.points) + '</b></li>';
    }
    html += '</ol><p class="sh-legend"><span>' + SRC_TG + esc(T("from_tg")) + '</span><span>' + SRC_SITE + esc(T("from_site")) + '</span></p></div>';
  }
  html += '<nav class="dnav">' + (d.prev ? '<button data-n="' + d.prev + '" aria-label="' + esc(T("days.prev")) + '">' + bwd() + ' ' + no("#" + d.prev) + '</button>' : '<span></span>') +
    (d.next ? '<button data-n="' + d.next + '" aria-label="' + esc(T("days.next")) + '">' + no("#" + d.next) + ' ' + fwd() + '</button>' : '<span></span>') + '</nav>';
  return html;
}

// Everyone's misses on one logarithmic scale from 1 to 10,000 km, the viewer's in gold on top.
function axis(misses, me) {
  const x = (km) => 10 + 300 * Math.log10(Math.min(10000, Math.max(1, km))) / 4;
  let dots = "";
  for (const km of misses) dots += '<circle class="o" cx="' + x(km).toFixed(1) + '" cy="16" r="3.5"/>';
  if (me) dots += '<circle class="me" cx="' + x(me.km).toFixed(1) + '" cy="16" r="6"/>';
  const ticks = [1, 10, 100, 1000, 10000].map((v, i) => {
    const anchor = i === 0 ? "start" : i === 4 ? "end" : "middle";
    return '<line class="ax" x1="' + x(v) + '" y1="11" x2="' + x(v) + '" y2="21"/><text class="tk" x="' + x(v) + '" y="38" text-anchor="' + anchor + '">' + esc(i === 4 ? kmT(v) : fmt(v)) + '</text>';
  }).join("");
  const label = T("days.axis") + (me ? " · " + T("days.you_dot", { km: kmT(me.km) }) : "");
  return '<div class="axis"><svg viewBox="0 0 320 44" role="img" aria-label="' + esc(label) + '"><line class="ax" x1="10" y1="16" x2="310" y2="16"/>' + ticks + dots + '</svg></div>';
}

// One map serves every day's page: it moves from page to page with the answers of the day on it.
// The map's style loads once; a day opened before that is drawn when it has, and only the last one.
let DMAP = null, DMAP_EL = null, DMAP_PLACE = null, DMAP_READY = false, DMAP_DRAW = null;
function dayMap(d) {
  const at = $("dMapAt");
  if (!at || typeof maplibregl === "undefined") return;
  at.classList.add("on");
  if (!DMAP_EL) { DMAP_EL = document.createElement("div"); DMAP_EL.className = "dmapgl"; }
  at.appendChild(DMAP_EL);
  const data = () => ({
    type: "FeatureCollection",
    features: (d.pins || []).map((p) => ({ type: "Feature", properties: {}, geometry: { type: "Point", coordinates: [p[1], p[0]] } }))
      .concat(d.me && d.me.pin ? [{ type: "Feature", properties: { me: 1 }, geometry: { type: "Point", coordinates: [d.me.pin.lon, d.me.pin.lat] } }] : []),
  });
  // The frame holds the place and the nearest four answers in five, so one far miss does not turn the map away from
  // the place; when even those spread over half the world, the map shows the world round the place.
  const fit = () => {
    const P = d.place, cos = Math.cos(P.lat * Math.PI / 180);
    const near = (d.pins || []).map((p) => [p[1], p[0]])
      .sort((a, b) => Math.hypot((a[0] - P.lon) * cos, a[1] - P.lat) - Math.hypot((b[0] - P.lon) * cos, b[1] - P.lat));
    const pts = [[P.lon, P.lat]].concat(near.slice(0, Math.ceil(near.length * 0.8)));
    let w = 180, e = -180, s = 90, n = -90;
    for (const [lon, lat] of pts) { w = Math.min(w, lon); e = Math.max(e, lon); s = Math.min(s, lat); n = Math.max(n, lat); }
    if (e - w > 90 || n - s > 60) DMAP.jumpTo({ center: [P.lon, Math.max(-50, Math.min(50, P.lat))], zoom: 1 });
    else DMAP.fitBounds([[w, s], [e, n]], { padding: 36, maxZoom: 7, duration: 0 });
  };
  const draw = () => {
    DMAP.resize();
    DMAP.getSource("pins").setData(data());
    if (DMAP_PLACE) DMAP_PLACE.remove();
    DMAP_PLACE = new maplibregl.Marker({ element: el(PIN), anchor: "bottom" }).setLngLat([d.place.lon, d.place.lat]).addTo(DMAP);
    fit();
  };
  DMAP_DRAW = draw;
  if (DMAP) { if (DMAP_READY) draw(); return; }
  DMAP = new maplibregl.Map({ container: DMAP_EL, style: mapStyle(LANG), center: [d.place.lon, d.place.lat], zoom: 1, attributionControl: false,
    dragRotate: false, pitchWithRotate: false, maxPitch: 0, fadeDuration: 0 });
  DMAP.touchZoomRotate.disableRotation();
  DMAP.on("load", () => {
    DMAP.addSource("pins", { type: "geojson", data: { type: "FeatureCollection", features: [] } });
    DMAP.addLayer({ id: "pins", type: "circle", source: "pins", filter: ["!", ["has", "me"]],
      paint: { "circle-radius": 4, "circle-color": "#1A412B", "circle-stroke-color": "#FFFFFF", "circle-stroke-width": 1.5 } });
    DMAP.addLayer({ id: "me", type: "circle", source: "pins", filter: ["has", "me"],
      paint: { "circle-radius": 7, "circle-color": "#F2C14E", "circle-stroke-color": "#1A412B", "circle-stroke-width": 2 } });
    DMAP_READY = true;
    DMAP_DRAW();
  });
}

// The day's panorama or photo over the page, as the game shows it, until closed.
let DVIEW = null;
function view360(d) {
  haptic("light");
  closeView();
  const box = el('<div class="dview" id="dView"><div id="dPano"></div><button class="sh-b dview-x" aria-label="' + esc(T("aria_collapse")) + '">×</button></div>');
  $("sheet").appendChild(box);
  box.querySelector(".dview-x").onclick = closeView;
  // Back and Escape go to the page again, then out of it.
  $("shBack").onclick = closeView;
  if (typeof pannellum === "undefined") return;
  DVIEW = pannellum.viewer("dPano", Object.assign({
    type: "equirectangular", panorama: d.image, autoLoad: true, showControls: false, compass: false,
    hfov: 100, minHfov: 20, maxHfov: 110, friction: 0.1, keyboardZoom: false, disableKeyboardCtrl: true,
    backgroundColor: [0.10, 0.25, 0.17], pitch: 0
  }, d.kind === "panorama" ? {} : { haov: 80, vaov: 50, maxHfov: 80 }));
  DVIEW.on("error", () => toast(T("no_image")));
}

// closeView puts the panorama away, and its WebGL with it, and gives the sheet's Back to the page under it.
function closeView() {
  if (DVIEW) { try { DVIEW.destroy(); } catch {} DVIEW = null; }
  const box = $("dView");
  if (!box) return;
  box.remove();
  if ($("days")) { if (DAY && !wide()) sheetHead(esc(TN("days.day_n", { n: fmt(DAY.day) })), toList); else sheetHead(esc(T("days")), null); }
}

// The list follows the width: a day open on a phone turned wide stays open beside the list, and the other way. The
// day a wide screen opened on its own has no address of its own, so a phone shows the list instead.
matchMedia("(min-width: 1024px)").addEventListener("change", () => {
  if (!$("days") || !daysShown()) return;
  if (!$("dView")) sheetHead(esc(T("days")), null);
  if (DAY && (wide() || /^\/days\/./.test(location.pathname))) showDay(DAY);
  else if (DAY) closeDay();
  else if (wide() && DAYS) {
    // A wide screen has the day's half empty otherwise.
    const first = DAYS.days.find((d) => d.final);
    if (first) openDay(String(first.day), true);
  }
});
