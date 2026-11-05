/* ---------- Free game: alone, and what the game with friends (party.js) shares ---------- */
// A layer over the day's screen in a colour of its own, so a free answer is never taken for the day's. Opened on the
// free game (/play, /party/<code>, or a party's invitation), the app does not load the day under it at all; leaving it
// loads the day. Every word comes from internal/i18n through T(), and the server decides everything: the place comes only
// with the answer, the time from the server's clock.
// The game screen here is a plain split, the picture above and the map below (side by side on a computer), until the
// shared panorama and globe module takes its place.
(() => {
  "use strict";
  const FIELD = { field: "#26496E", deep: "#1A3452", tint: "#5D7C9E" };
  const DAY = { field: "#245A3C", deep: "#1A412B", tint: "#59826B" };
  const CODE = /^[0-9A-HJKMNP-TV-Z]{8}$/;
  // The app sends a pin set but not sent itself once the time is up; the server takes answers 2 s after that.
  const GRACE = 2000;

  // dayBoot is the day's start when the app opened on the free game; dayBooted says it has run since.
  let root = null, onBack = null, dayBooted = false, dayBoot = null, opened = false;
  let skew = 0;
  const now = () => Date.now() + skew;
  const msTo = (iso) => Date.parse(iso) - now();

  /* ---------- Requests ---------- */
  // In Telegram the signed launch; on the site the cookie player, JSON only, as the server asks.
  const signed = () => typeof inTg !== "undefined" && inTg && !!tg.initData;
  // A Telegram person: in the Mini App, or on the site signed in with an account that has Telegram (SITE is false
  // then). They keep records and play with friends; a site guest and an account with Google alone play alone.
  const person = () => signed() || (typeof inTg !== "undefined" && !inTg && typeof SITE !== "undefined" && !SITE);
  async function req(path, body) {
    const headers = {};
    if (signed()) headers.Authorization = "tma " + tg.initData;
    if (body !== undefined) headers["Content-Type"] = "application/json";
    if (!signed()) path += (path.includes("?") ? "&" : "?") + "lang=" + encodeURIComponent(LANG);
    const d = await reply(await get(path, {
      method: body !== undefined ? "POST" : "GET", headers, credentials: "same-origin",
      body: body !== undefined ? JSON.stringify(body) : undefined,
    }));
    if (d.now) skew = Date.parse(d.now) - Date.now();
    return d;
  }
  // A refusal of the free game names itself; anything else reads as the day's errors do.
  const ferr = (e) => TX["free.error." + e.message] ? T("free.error." + e.message) : errorText(e);

  /* ---------- The layer ---------- */
  function layer() {
    if (root) return root;
    root = el('<div class="free" id="free"><header class="fr-top"><button class="fr-back" id="frBack"></button>' +
      '<div class="fr-head"><b id="frTitle"></b><span class="up" id="frSub"></span></div><div class="fr-clock" id="frClock"></div></header>' +
      '<div class="fr-time" id="frTime"><i></i></div><div class="fr-ribbon" id="frRibbon"></div>' +
      '<main class="fr-page" id="frPage"></main>' +
      '<section class="fr-play hidden" id="frPlay"><div class="fr-pano" id="frPano"></div>' +
      '<div class="fr-map" id="frMap"><button class="fr-grow" id="frGrow" data-act="grow"><svg viewBox="0 0 16 16" aria-hidden="true"><path d="M2 6V2h4M10 14h4v-4M2 2l5 5M14 14l-5-5" fill="none" stroke="currentColor" stroke-width="2"/></svg></button></div>' +
      '<div class="fr-act" id="frAct"></div><div class="fr-res hidden" id="frRes"></div></section></div>');
    document.body.appendChild(root);
    $("frBack").onclick = () => onBack && onBack();
    root.addEventListener("click", (e) => {
      const b = e.target.closest("[data-act]");
      if (!b || b.disabled || !root.contains(b)) return;
      const f = ACTS[b.dataset.act];
      if (f) { e.preventDefault(); f(b); }
    });
    addEventListener("keydown", (e) => {
      if (!opened) return;
      if (e.key === "Escape") { e.preventDefault(); onBack && onBack(); }
      // Enter answers, as in the day's game on a computer; the space bar is too easy to hit.
      if (e.key === "Enter" && !e.target.closest("button,a,input,textarea") && !$("frPlay").classList.contains("hidden")) {
        const go = $("frAct").querySelector("[data-act=send]:not([disabled]),[data-act=pSend]:not([disabled])") || $("frRes").querySelector(".fr-pri");
        if (go) { e.preventDefault(); go.click(); }
      }
    });
    return root;
  }
  const backButton = () => onBack && onBack();
  // Where the focus was before the layer opened, to put it back when it closes.
  let before = null;
  function open() {
    layer();
    if (opened) return;
    opened = true;
    before = document.activeElement;
    root.classList.add("on"); document.body.classList.add("is-free");
    for (const e of document.body.children) if (e !== root && e.id !== "toast" && e.tagName !== "SCRIPT") e.inert = true;
    colours(FIELD);
    if (signed() && tgAt("6.1")) { tg.BackButton.onClick(backButton); tg.BackButton.show(); }
    $("frBack").focus({ preventScroll: true });
  }
  function close() {
    opened = false;
    stopClock(); Kit.stopped(); guard(false);
    root.classList.remove("on"); document.body.classList.remove("is-free");
    for (const e of document.body.children) if (e !== root && e.tagName !== "SCRIPT") e.inert = false;
    colours(DAY);
    if (signed() && tgAt("6.1")) { tg.BackButton.offClick(backButton); syncBack(); }
    // Opened on the free game, the app has not loaded the day yet: it does now.
    if (dayBoot && !dayBooted) { dayBooted = true; $("loader").classList.remove("gone"); dayBoot(); }
    if (before && before.isConnected) before.focus({ preventScroll: true });
    before = null;
  }
  function colours(c) {
    const meta = document.querySelector('meta[name="theme-color"]');
    if (meta) meta.content = c.field;
    if (!signed()) return;
    if (tgAt("6.9")) { tg.setHeaderColor(c.field); tg.setBackgroundColor(c.field); }
    if (tgAt("7.10")) tg.setBottomBarColor(c.deep);
  }
  // The header: a title, a line under it, and what Back does here.
  function top(title, sub, back) {
    $("frTitle").textContent = title;
    $("frSub").textContent = sub || T("free.unranked");
    $("frBack").textContent = fwd() === "→" ? "←" : "→";
    $("frBack").setAttribute("aria-label", T("free.back"));
    onBack = back || close;
  }
  // A page: settings, the lobby, a result. The game screen hides while one is up. Drawn again with keep (a setting
  // changed, someone joined), it stays where it was scrolled to, and the focus on the same button.
  const same = (a, b) => a.dataset.act === b.dataset.act && a.dataset.k === b.dataset.k && a.dataset.v === b.dataset.v;
  function page(html, cls, keep) {
    stopClock(); ribbon(null);
    $("frPlay").classList.add("hidden");
    const p = $("frPage"), y = p.scrollTop, had = keep && p.contains(document.activeElement) ? document.activeElement : null;
    p.className = "fr-page " + (cls || ""); p.innerHTML = html; p.classList.remove("hidden"); p.scrollTop = keep ? y : 0;
    const again = had && had.dataset && [...p.querySelectorAll("[data-act]")].find((b) => same(b, had));
    if (again) again.focus({ preventScroll: true });
    return p;
  }
  const wait = () => page('<p class="fr-wait up">' + esc(lines("loading")[0]) + '</p>');
  function failed(e, again) {
    ACTS.retry = again;
    page('<p class="fr-wait">' + esc(ferr(e)) + '</p><div class="fr-go"><button class="fr-sec" data-act="retry">' + esc(T("free.retry")) + '</button></div>');
  }

  /* ---------- Settings ---------- */
  const ROUNDS = { 3: "free.r3", 5: "free.r5", 10: "free.r10" };
  const key = (s) => s.mode === "streak" ? "streak/" + s.seconds : "classic/" + s.seconds + "/" + s.rounds;
  const secLabel = (n) => n === 30 ? T("free.blitz") + " " + fmt(30) : n ? T("free.sec", { n: fmt(n) }) : T("free.no_timer");
  // "Blitz · 5 places", "60 s · 10 places", "Country streak · 30 s": the name of a set of settings and its records.
  function setLabel(s) {
    if (s.mode === "streak") return T("free.streak") + " · " + T("free.sec", { n: fmt(s.seconds) });
    return (s.seconds === 30 ? T("free.blitz") : s.seconds ? T("free.sec", { n: fmt(s.seconds) }) : T("free.no_timer")) + " · " + T(ROUNDS[s.rounds]);
  }
  // One choice of a set: ◉ on the current one, as the bot's panels do.
  function seg(k, items, cur, locked) {
    return '<div class="fr-seg" role="radiogroup" aria-labelledby="frL-' + k + '">' + items.map(([v, label]) => '<button role="radio" aria-checked="' + (v === cur) + '" data-act="set" data-k="' + k +
      '" data-v="' + v + '"' + (locked ? " disabled" : "") + '>' + esc(label) + '</button>').join("") + '</div>';
  }
  // A settings block, the solo one or the lobby's: the party has no "no timer" and no streak.
  function settings(s, party, locked) {
    let html = "";
    if (!party) {
      html += '<p class="fr-l up" id="frL-mode">' + esc(T("free.mode")) + '</p>' + seg("mode", [["classic", T("free.classic")], ["streak", T("free.streak")]], s.mode, locked);
      if (s.mode === "streak") html += '<p class="fr-hint">' + esc(T("free.streak_hint")) + '</p>';
    }
    const times = s.mode === "streak" ? [30, 60] : party ? [30, 60, 90, 120] : [30, 60, 90, 120, 0];
    html += '<p class="fr-l up" id="frL-seconds">' + esc(T("free.time")) + '</p>' + seg("seconds", times.map((n) => [n, secLabel(n)]), s.seconds, locked);
    if (s.mode !== "streak") html += '<p class="fr-l up" id="frL-rounds">' + esc(T("free.places")) + '</p>' + seg("rounds", [3, 5, 10].map((n) => [n, fmt(n)]), s.rounds, locked);
    return html;
  }
  // A choice made: a streak has a 30 or 60 s timer and no number of places.
  function choose(s, k, v) {
    const n = Object.assign({}, s);
    n[k] = k === "mode" ? v : +v;
    if (n.mode === "streak") { n.rounds = 0; if (n.seconds !== 30) n.seconds = 60; }
    else if (!n.rounds) n.rounds = 5;
    return n;
  }

  /* ---------- The game screen: the picture and the map ---------- */
  let viewer = null, fmap = null, fmapReady = false, pin = null, marks = [], onPin = null, retryT = 0, pics = 0;
  function showPlay() {
    $("frPage").classList.add("hidden");
    $("frPlay").classList.remove("hidden");
    $("frRes").classList.add("hidden");
    $("frPlay").classList.remove("grown", "rev");
    $("frGrow").setAttribute("aria-label", T("free.map_bigger"));
    if (!fmap) makeMap(); else requestAnimationFrame(() => fmap.resize());
  }
  // The picture, under the round's own address; it is fetched only once the round has begun.
  function picture(img, kind) {
    clearTimeout(retryT);
    const my = ++pics;
    // A panorama shows 95° from top to bottom; a plain photo covers 80° by 50°, so it shows a little less than its
    // height and never the empty sphere around it.
    const w = $("frPano").clientWidth || innerWidth, h = $("frPano").clientHeight || innerHeight / 2, pano = kind === "panorama";
    const fov = (v) => 2 * Math.atan(Math.tan(v * Math.PI / 360) * w / h) * 180 / Math.PI;
    const hfov = pano ? Math.max(50, Math.min(105, fov(95))) : Math.max(20, Math.min(78, fov(46)));
    let tries = 0;
    const start = () => {
      if (viewer) { try { viewer.destroy(); } catch {} viewer = null; }
      $("frPano").innerHTML = "";
      viewer = pannellum.viewer("frPano", Object.assign({
        type: "equirectangular", panorama: img, autoLoad: true, showControls: false, compass: false, hfov, minHfov: 20, maxHfov: 110,
        friction: 0.1, keyboardZoom: false, disableKeyboardCtrl: true, backgroundColor: [0.10, 0.20, 0.32], pitch: 0,
      }, pano ? {} : { haov: 80, vaov: 50, maxHfov: hfov, avoidShowingBackground: true }));
      // A failed picture is tried again, unless another place's picture has come since.
      viewer.on("error", () => {
        if (my !== pics) return;
        if (tries++ < 3) retryT = setTimeout(start, 1500 * tries); else toast(T("no_image"));
      });
    };
    start();
  }
  // The day's marks and map style in the free game's colours.
  const ours = (svg) => svg.split(DAY.deep).join(FIELD.deep);
  function freeStyle() {
    let s = JSON.stringify(mapStyle(LANG));
    for (const k of ["field", "deep", "tint"]) s = s.split(DAY[k]).join(FIELD[k]);
    return JSON.parse(s);
  }
  function makeMap() {
    fmap = new maplibregl.Map({ container: "frMap", style: freeStyle(), center: [15, 25], zoom: earth(), minZoom: -1, maxPitch: 0,
      attributionControl: false, dragRotate: false, pitchWithRotate: false, fadeDuration: 150 });
    fmap.touchZoomRotate.disableRotation();
    fmap.on("load", () => {
      fmapReady = true;
      fmap.addSource("fr-lines", { type: "geojson", data: { type: "FeatureCollection", features: [] } });
      // As the day's line: a white case keeps the dashes seen on light land and on the sea alike.
      fmap.addLayer({ id: "fr-lines-case", type: "line", source: "fr-lines", paint: { "line-color": "#FFFFFF", "line-width": 5 } });
      fmap.addLayer({ id: "fr-lines", type: "line", source: "fr-lines", paint: { "line-color": FIELD.deep, "line-width": 2, "line-dasharray": [2, 1.5] } });
    });
    fmap.on("click", (e) => onPin && onPin(e.lngLat));
    fmap.on("moveend", () => friends.length && declutter());
    fmap.on("movestart", (e) => { if (e.originalEvent) untouched = false; });
    // The play screen and the reveal give the map another height, and a phone turns: once the size settles the canvas
    // follows it, and the camera fits again: the reveal's frame, or the whole Earth while nobody has moved the map.
    let settle = 0;
    new ResizeObserver(() => {
      clearTimeout(settle);
      settle = setTimeout(() => {
        fmap.resize();
        if (fitTo) fit(500);
        else if (untouched) fmap.jumpTo({ center: [15, 25], zoom: earth() });
      }, 120);
    }).observe($("frMap"));
    // Pannellum follows the window's size only; its box changes with the layout too.
    new ResizeObserver(() => { if (viewer && $("frPano").clientHeight) viewer.resize(); }).observe($("frPano"));
  }
  // As in the day's game, a pin set and not yet sent asks before the app closes. Only what the free game turned on
  // is turned off: the day keeps its own.
  let guarding = false;
  function guard(on) {
    if (on === guarding || !signed() || !tgAt("6.2")) return;
    guarding = on;
    if (on) tg.enableClosingConfirmation(); else tg.disableClosingConfirmation();
  }
  function clearMarks() {
    guard(false);
    marks.forEach((m) => m.remove()); marks = []; friends = [];
    if (pin) { pin.remove(); pin = null; }
    if (fmapReady) fmap.getSource("fr-lines").setData({ type: "FeatureCollection", features: [] });
  }
  // A new place: the map back to the whole Earth, empty, and a tap that sets the pin.
  // The whole Earth fills most of the map: MapLibre's globe is about 195 px across at zoom 0.4 over latitude 25 (a
  // little more in a low map), and at one zoom it grows as 1 / cos of the latitude at the centre.
  const rad = Math.PI / 180;
  const earth = (lat = 25) => 0.4 + Math.log2(Math.max(120, 0.8 * Math.min($("frMap").clientWidth, $("frMap").clientHeight)) / 195) +
    Math.log2(Math.cos(Math.min(80, Math.abs(lat)) * rad) / Math.cos(25 * rad));
  let untouched = true;
  function freshMap(set) {
    clearMarks(); fitTo = null; untouched = true;
    if (fmap) { fmap.resize(); fmap.jumpTo({ center: [15, 25], zoom: earth() }); }
    onPin = (ll) => {
      const p = { lat: ll.lat, lon: ((ll.lng + 540) % 360) - 180 };
      if (!pin) pin = new maplibregl.Marker({ element: el(ours(PIN)), anchor: "bottom" }).setLngLat(ll).addTo(fmap);
      else pin.setLngLat(ll);
      guard(true);
      haptic("light");
      set(p);
    };
  }
  // The camera on the place and the pins, once the map has its size after the layout changed. MapLibre frames bounds
  // as on a flat map, so on the globe a wide frame comes out too close and puts the points at its edges. The frame is
  // tried on the globe and widened before the camera moves; jumps within one task draw nothing in between.
  let fitTo = null;
  function fit(duration) {
    if (!fitTo || !fmap) return;
    // The margin holds a whole mark, not only its point; a pin stands 46 px above its point.
    const w = $("frMap").clientWidth, h = $("frMap").clientHeight, pad = Math.min(60, w / 6, h / 6), top = pad + Math.min(46, h / 4);
    // The place alone (no answer) is shown with its country around it, not its streets.
    const cam = fmap.cameraForBounds(fitTo.b, { padding: { top, bottom: pad, left: pad, right: pad }, maxZoom: fitTo.pts.length > 1 ? 9 : 4 }) ||
      { center: fitTo.b.getCenter(), zoom: earth(fitTo.b.getCenter().lat) };
    const was = { center: fmap.getCenter(), zoom: fmap.getZoom() };
    let zoom = cam.zoom;
    for (let i = 0; i < 3; i++) {
      fmap.jumpTo({ center: cam.center, zoom });
      let k = 0;
      for (const p of fitTo.pts) {
        const s = fmap.project(p), dx = s.x - w / 2, dy = s.y - h / 2;
        k = Math.max(k, Math.abs(dx) / Math.max(20, w / 2 - pad), dy < 0 ? -dy / Math.max(20, h / 2 - top) : dy / Math.max(20, h / 2 - pad));
      }
      if (k <= 1.02) break;
      zoom -= Math.log2(k);
    }
    fmap.jumpTo(was);
    fmap.easeTo({ center: cam.center, zoom: Math.max(zoom, earth(maplibregl.LngLat.convert(cam.center).lat)), duration });
  }
  // An answer sent: the pin stays where it is.
  function lock() { onPin = null; guard(false); }
  // After the answer: the true point, every pin with a line to it, and the camera on all of them.
  function revealMap(place, pins) {
    onPin = null;
    const show = () => {
      clearMarks();
      const at = [place.lon, place.lat], lines = [], pts = [at];
      // The place stays above the friends' marks that crowd round it.
      const target = el(ours(TARGET));
      target.style.zIndex = 2;
      marks.push(new maplibregl.Marker({ element: target }).setLngLat(at).addTo(fmap));
      for (const p of pins) {
        if (p.lat == null) continue;
        const ll = [p.lon, p.lat];
        const m = p.me ? el(ours(PIN)) : el('<div class="fr-f"><i class="av">' + esc(initial(p.name || "?")) + '</i><b lang="' + nameLang() + '">' + esc(p.name || "") + '</b></div>');
        marks.push(new maplibregl.Marker({ element: m, anchor: p.me ? "bottom" : "center" }).setLngLat(ll).addTo(fmap));
        lines.push({ type: "Feature", geometry: { type: "LineString", coordinates: [ll, [lngNear(at[0], ll[0]), at[1]]] } });
        pts.push(ll);
      }
      fmap.getSource("fr-lines").setData({ type: "FeatureCollection", features: lines });
      friends = marks.filter((m) => m.getElement().classList.contains("fr-f"));
      const near = pts.map((p) => [lngNear(p[0], at[0]), p[1]]), b = new maplibregl.LngLatBounds(at, at);
      near.forEach((p) => b.extend(p));
      fitTo = { b, pts: near };
      fit(900);
    };
    fmapReady ? show() : fmap.once("load", show);
  }
  // A friend's name that would lie on another's is left out; the letter stays. Read again when the camera stops.
  let friends = [];
  function declutter() {
    const kept = [];
    for (const m of friends) {
      const p = fmap.project(m.getLngLat()), tight = kept.some((q) => Math.abs(q.x - p.x) < 110 && Math.abs(q.y - p.y) < 22);
      m.getElement().classList.toggle("tight", tight);
      if (!tight) kept.push(p);
    }
  }
  // The buttons under the map.
  function act(html) { $("frAct").innerHTML = html; }
  function result(html) {
    guard(false);
    const r = $("frRes"); r.innerHTML = html; r.classList.remove("hidden"); r.scrollTop = 0;
    $("frPlay").classList.add("rev");
  }

  /* ---------- The clock ---------- */
  let tickT = 0;
  // limit is the place's time in seconds; done runs once when it is up.
  function clock(startsAt, endsAt, limit, done) {
    stopClock();
    const bar = $("frTime").firstChild;
    $("frTime").classList.toggle("off", !endsAt);
    if (!endsAt) { $("frClock").textContent = ""; return; }
    let fired = false;
    const step = () => {
      const ms = msTo(endsAt), s = Math.max(0, Math.ceil(ms / 1000));
      $("frClock").textContent = s >= 60 ? Math.floor(s / 60) + ":" + String(s % 60).padStart(2, "0") : String(s);
      $("frClock").classList.toggle("low", s <= 10);
      bar.style.transform = "scaleX(" + Math.max(0, Math.min(1, ms / (limit * 1000))).toFixed(4) + ")";
      bar.classList.toggle("low", s <= 10);
      if (ms <= 0 && !fired) { fired = true; done(); }
    };
    step();
    tickT = setInterval(step, 200);
  }
  function stopClock() { clearInterval(tickT); tickT = 0; if (root) { $("frTime").classList.add("off"); $("frClock").textContent = ""; } }

  /* ---------- What an answer was worth ---------- */
  const WHAT = { 300: "what_continent", 800: "what_country", 1500: "what_city" };
  // Only when there is more than the distance: otherwise the line repeats the points and the kilometres above it.
  function parts(g) {
    if (g.lat == null || (!g.bonus && !g.speed)) return "";
    const out = [T("km_points", { points: fmt(g.distance), km: kmT(g.km) })];
    if (g.bonus) out.push(T("bonus", { points: fmt(g.bonus), what: T(WHAT[g.bonus]) }));
    if (g.speed) out.push(T("speed_bonus", { points: fmt(g.speed) }));
    return out.join(" · ");
  }
  const whereOf = (p) => [p.city, p.country && regionName(p.country)].filter(Boolean).join(", ");
  function credit(p) {
    if (!p || !SOURCES[p.source]) return "";
    const [name, url] = SOURCES[p.source], lic = (p.license || "").replace(/^CC-/, "CC ").replace(/-(\d)/, " $1");
    return '<p class="fr-cred">© ' + (p.author ? esc(p.author) + ", " : "") + '<a href="' + url + '" target="_blank" rel="noopener">' + name + '</a> ' + esc(lic) + '</p>';
  }
  // The place after an answer: where it was, how far, the points in parts, the fact.
  function placeBlock(p, g, note) {
    let html = '<b class="fr-where">' + esc(whereOf(p)) + '</b>';
    if (note) html += '<p class="fr-note">' + esc(note) + '</p>';
    if (g && g.lat != null) html += '<p class="fr-score"><span>' + kmT(g.km) + '</span><b>' + fmt(g.points) + '</b></p>' +
      (parts(g) ? '<p class="fr-parts">' + esc(parts(g)) + '</p>' : '');
    else html += '<p class="fr-score"><b>0</b></p>';
    if (p.fact) html += '<p class="fr-fact">' + esc(p.fact) + '</p>';
    return html + credit(p);
  }

  /* ---------- Alone ---------- */
  let HOME = null, SET = null, GAME = null, ROUND = null, mine = null, sending = false;
  // gen moves on with every screen of the solo game, so an answer that comes back after Back draws nothing.
  let gen = 0, missT = 0;
  async function home(h) {
    gen++; ROUND = null; clearTimeout(missT);
    top(T("free.title"), "", leave);
    if (!h) {
      wait();
      try { h = await req("/api/free"); } catch (e) { failed(e, () => home()); return; }
    }
    HOME = h;
    if (h.lang && h.lang !== LANG) { await loadTexts(h.lang).catch(() => {}); top(T("free.title"), "", leave); }
    if (!SET) SET = Object.assign({}, h.last);
    drawHome();
  }
  function drawHome(keep) {
    const h = HOME, s = SET;
    let html = "";
    if (h.party) html += '<button class="fr-wide" data-act="toParty" data-code="' + esc(h.party) + '">' + esc(T("free.to_party")) + ' <em>' + fwd() + '</em></button>';
    if (h.open) html += '<button class="fr-wide" data-act="resume">' + esc(T("free.continue")) + ' <em>' + fwd() + '</em></button>';
    html += settings(s, false, false);
    const best = h.best && h.best[key(s)];
    // A record of nothing is not shown; the line keeps its height so the buttons do not jump between settings.
    if (person()) html += '<p class="fr-best">' + (best ? esc(T(s.mode === "streak" ? "free.best_streak" : "free.best", { set: setLabel(s), n: fmt(best) })) : "") + '</p>';
    else html += '<p class="fr-hint">' + esc(T("free.guest_note")) + '</p>';
    html += '<div class="fr-go"><button class="fr-pri" data-act="go">' + esc(T("free.go")) + '</button>';
    if (person()) html += '<button class="fr-sec" data-act="friends">' + esc(T("free.friends")) + '</button>';
    page(html + '</div>', "fr-home", keep);
  }
  const ACTS = {
    set(b) {
      if (Kit.lobby && Kit.lobby(b)) return;
      SET = choose(SET, b.dataset.k, b.dataset.v); haptic("select"); drawHome(true);
    },
    async go(b) {
      b.disabled = true;
      try { GAME = await req("/api/free/solo", SET); } catch (e) { b.disabled = false; toast(ferr(e)); return; }
      haptic("medium"); next();
    },
    async resume(b) {
      b.disabled = true;
      try { GAME = await req("/api/free/solo/" + HOME.open); } catch (e) { b.disabled = false; toast(ferr(e)); return; }
      continueGame();
    },
    friends() { Kit.party.create(SET.mode === "streak" ? { mode: "classic", seconds: SET.seconds, rounds: 5 } : { mode: "classic", seconds: SET.seconds || 60, rounds: SET.rounds }); },
    toParty(b) { Router.go("/party/" + b.dataset.code); },
    grow() {
      const on = $("frPlay").classList.toggle("grown");
      $("frGrow").setAttribute("aria-label", T(on ? "free.map_smaller" : "free.map_bigger"));
      setTimeout(() => fmap && fmap.resize(), 320);
    },
    send() { sendSolo(false); },
    giveUp() { sendSolo(false, true); },
    next(b) { b.disabled = true; haptic("light"); GAME.next ? next() : final(); },
    again(b) { ACTS.go(b); },
    settings() { home(HOME); },
  };
  function continueGame() {
    if (GAME.status !== "playing") return final();
    if (GAME.current) return play(GAME.current);
    if (GAME.next) return next();
    final();
  }
  async function next() {
    const my = ++gen;
    wait();
    let r;
    try { r = await req("/api/free/solo/" + GAME.id + "/start", { n: GAME.next }); } catch (e) { if (my === gen) failed(e, () => next()); return; }
    if (my === gen) play(r);
  }
  const placeTitle = (g, n) => g.settings.mode === "streak" ? T("free.place", { n: fmt(n) }) : T("free.place_n", { n: fmt(n), total: fmt(g.settings.rounds) });
  function play(r) {
    gen++; clearTimeout(missT);
    ROUND = r; mine = null; sending = false;
    top(placeTitle(GAME, r.n), GAME.settings.mode === "streak" ? T("free.streak_now", { n: fmt(GAME.streak) }) : T("free.points_now", { n: fmt(GAME.points) }), () => home());
    showPlay();
    picture(r.image, r.kind);
    if (r.repeat) toast(T("free.repeat"));
    freshMap((p) => { mine = p; act(buttons()); });
    act(buttons());
    const limit = GAME.settings.seconds;
    clock(r.starts_at, r.ends_at, limit, () => {
      // At zero a pin set is sent by itself; without one the server counts the place as missed once the grace is over.
      if (mine) sendSolo(true);
      else missT = setTimeout(missed, GRACE + 600);
    });
  }
  function buttons() {
    const give = !ROUND.ends_at ? '<button class="fr-sec" data-act="giveUp">' + esc(T("free.give_up")) + '</button>' : "";
    return give + '<button class="fr-pri" data-act="send"' + (mine && !sending ? "" : " disabled") + '>' + esc(mine ? T("free.answer") : T("free.tap_map")) + '</button>';
  }
  async function sendSolo(auto, skip) {
    if (sending || !ROUND) return;
    const my = gen;
    sending = true; act(buttons());
    const body = skip || !mine ? { n: ROUND.n, skip: true } : { n: ROUND.n, lat: mine.lat, lon: mine.lon, auto };
    let g;
    try { g = await req("/api/free/solo/" + GAME.id + "/guess", body); }
    catch (e) {
      if (my !== gen) return;
      sending = false; act(buttons());
      if (e.message === "already_answered") return missed();
      toast(ferr(e)); return;
    }
    GAME = g;
    if (my !== gen) return;
    haptic(skip ? "light" : "success");
    revealSolo();
  }
  // The time ran out without a pin: the server has written the miss, and the game read again shows the place.
  async function missed() {
    if (!ROUND || !opened) return;
    const my = gen, n = ROUND.n;
    let g;
    try { g = await req("/api/free/solo/" + GAME.id); } catch (e) { if (my === gen) toast(ferr(e)); return; }
    GAME = g;
    if (my === gen && GAME.done.some((d) => d.n === n)) revealSolo();
  }
  function revealSolo() {
    stopClock();
    const d = GAME.done[GAME.done.length - 1], g = d.guess;
    ROUND = null;
    top(placeTitle(GAME, d.n), GAME.settings.mode === "streak" ? T("free.streak_now", { n: fmt(GAME.streak) }) : T("free.points_now", { n: fmt(GAME.points) }), () => home());
    act("");
    const note = d.late ? T("free.late") : g.lat == null ? (GAME.settings.seconds ? T("free.missed") : T("free.gave_up"))
      : GAME.settings.mode === "streak" ? T(g.country ? "free.country_ok" : "free.country_miss") : "";
    // The panel first: the map takes its reveal size, and the camera frames that.
    result(placeBlock(d.place, g, note) + '<button class="fr-pri" data-act="next">' + esc(T(GAME.next ? "free.next" : "free.to_final")) + '</button>');
    revealMap(d.place, [{ me: true, lat: g.lat, lon: g.lon }]);
  }
  function final() {
    gen++;
    const f = GAME.final || {}, s = GAME.settings;
    top(T("free.final"), setLabel(s), () => home());
    let html = '<div class="fr-big">';
    if (s.mode === "streak") html += '<b>' + fmt(f.streak || 0) + '</b><span class="up">' + esc(T("free.streak_len")) + '</span>';
    else html += '<b>' + fmt(f.points || 0) + '</b>';
    html += '</div>';
    if (f.record) html += '<p class="fr-rec">' + esc(T("free.record", { n: fmt(f.best) })) + '</p>';
    else if (f.best == null) html += person() ? '<p class="fr-best">' + esc(T("free.first_set")) + '</p>' : "";
    else if (f.best) html += '<p class="fr-best">' + esc(T(s.mode === "streak" ? "free.best_streak" : "free.best", { set: setLabel(s), n: fmt(f.best) })) + '</p>';
    html += '<ol class="fr-list">';
    for (const d of GAME.done) {
      html += '<li><span class="n">' + fmt(d.n) + '</span><span class="w">' + esc(whereOf(d.place)) + '</span><span class="k">' +
        (d.guess.lat != null ? kmT(d.guess.km) : "") + '</span><b>' + fmt(d.guess.points) + '</b></li>';
    }
    html += '</ol><div class="fr-go"><button class="fr-pri" data-act="again">' + esc(T("free.again")) + '</button>' +
      (person() ? '<button class="fr-sec" data-act="friends">' + esc(T("free.friends")) + '</button>' : '') +
      '<button class="fr-link" data-act="settings">' + esc(T("free.settings")) + '</button></div>';
    page(html, "fr-final");
    HOME = Object.assign({}, HOME || {}, { open: null });
    if (f.points != null && HOME.best) HOME.best[key(s)] = Math.max(HOME.best[key(s)] || 0, s.mode === "streak" ? f.streak || 0 : f.points);
  }

  /* ---------- What party.js uses ---------- */
  const Kit = {
    req, ferr, now, msTo, top, page, wait, failed, open, close, settings, choose, setLabel, secLabel, key, showPlay, picture, freshMap,
    revealMap, lock, act, result, clock, stopClock, parts, whereOf, placeBlock, ACTS, ribbon, home: () => home(),
    bot: () => HOME && HOME.bot,
    // party.js fills these in.
    party: null, lobby: null, stopped: () => {},
  };
  // The ribbon under the header: who is in, and who has answered.
  function ribbon(list) {
    const r = $("frRibbon");
    if (!list) { r.innerHTML = ""; r.classList.remove("on"); return; }
    r.innerHTML = list.map((m) => '<i class="av' + (m.me ? " me" : "") + (m.done ? " done" : "") + (m.away ? " away" : "") + '" title="' + esc(m.name) + '">' + esc(initial(m.name)) + '</i>').join("");
    r.classList.add("on");
  }

  /* ---------- Into and out of the free game ---------- */
  // The free game is the page /play and a party /party/<code>; an invitation opens the app with "p" and the code as its
  // start_param, and the app moves to the party's address. Leaving goes to the day, the page under every other.
  const PARTY = /^\/party\/([0-9A-HJKMNP-TV-Z]{8})$/;
  function invite() {
    const p = signed() && tg.initDataUnsafe && tg.initDataUnsafe.start_param || "";
    return p[0] === "p" && CODE.test(p.slice(1)) ? p.slice(1) : "";
  }
  // Telegram has no history, so Back goes to the parent: the list of days when the game was opened from it.
  let parent = "/";
  const came = (from) => { if (from !== "play" && from !== "party") parent = from === "days" ? "/days" : "/"; };
  const leave = () => Router.back(parent);
  // The home fetched before the router opened the page, used once.
  let first = null;
  Router.screen("play", /^\/play$/, {
    open(m, from) { came(from); open(); const h = first; first = null; home(h); },
    close(to) { if (to !== "party") close(); },
  });
  Router.screen("party", PARTY, {
    open(m, from) { came(from); open(); Kit.party.open(m[1]); },
    close(to) { Kit.stopped(); if (to !== "play") close(); },
  });
  window.Free = {
    // Whether the app opens on the free game rather than the day: once, before the day has started.
    takes() { return !dayBoot && !opened && (!!invite() || /^\/play$/.test(location.pathname) || PARTY.test(location.pathname)); },
    // start opens it in place of the day; boot is the day's start, run when the free game is left.
    async start(boot) {
      dayBoot = boot;
      await loadTexts(LANG0).catch(() => {});
      try {
        first = await req("/api/free");
        if (first.lang && first.lang !== LANG) await loadTexts(first.lang).catch(() => {});
        HOME = first; SET = Object.assign({}, first.last);
      } catch {}
      hideLoader();
      const code = invite();
      if (code && !PARTY.test(location.pathname)) Router.go("/party/" + code, { replace: true });
      Router.start();
    },
    // The way in from the day's list of days.
    entry() { return '<a class="dd fr-entry" href="/play" data-route><span class="up">' + esc(T("free.unranked")) + '</span><b>' + esc(T("free.title")) + '</b><em>' + fwd() + '</em></a>'; },
  };
  window.FreeKit = Kit;
})();
