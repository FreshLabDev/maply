/* ---------- Free game: alone, and what the game with friends (party.js) shares ---------- */
// The free game's screens over the day's. Its pages (the settings, the archive, a challenge, the end) are drawn as
// the days' list is; its game as the day's game is: the same header, the panorama of pano.js, the globe of globe.js
// in the same corner, the caption, the bottom strip, and after an answer the plates on a phone or the side panel on
// a computer. Only what is counted differs: the place of the game, its clock and its points.
// Opened on the free game (/play, /party/<code>, or a party's or a challenge's invitation), the app does not load the
// day under it at all; leaving it loads the day. Every word comes from internal/i18n through T(), and the server
// decides everything: the place comes only with the answer, the time from the server's clock.
(() => {
  "use strict";
  const CODE = /^[0-9A-HJKMNP-TV-Z]{8}$/;
  // The app sends a pin set but not sent itself once the time is up; the server takes answers 2 s after that.
  const GRACE = 2000;
  const MINI = 136, ACT = 82, TAPE_H = 38;

  // dayBoot is the day's start when the app opened on the free game; dayBooted says it has run since.
  let root = null, onBack = null, dayBooted = false, dayBoot = null, opened = false;
  let skew = 0;
  const now = () => Date.now() + skew;
  const msTo = (iso) => Date.parse(iso) - now();

  /* ---------- Requests ---------- */
  // In Telegram the signed launch; on the site the cookie player, JSON only, as the server asks.
  const signed = () => typeof inTg !== "undefined" && inTg && !!tg.initData;
  // A Telegram person: in the Mini App, or on the site signed in with an account that has Telegram (SITE is false then).
  const person = () => signed() || (typeof inTg !== "undefined" && !inTg && typeof SITE !== "undefined" && !SITE);
  // Who keeps records and plays with friends: a Telegram person or anyone signed in on the site, an account with
  // Google alone too. The server says so with the free screen; until it has, a Telegram person does.
  const keeps = () => HOME && typeof HOME.keeps === "boolean" ? HOME.keeps : person();
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
    root = el('<div class="free" id="free">' +
      '<section class="fr-pg"><div class="sh-top"><button class="sh-b" id="frBack"></button><b id="frTitle"></b></div>' +
      '<div class="sh-body fr-body" id="frPage" tabindex="-1"></div></section>' +
      '<div class="fr-game"><main class="fr-stage"><div id="frPano" role="region"></div><button class="fr-compass" id="frCompass"><svg id="frTape"></svg></button></main>' +
      '<div class="fr-bar off" id="frBar"><i></i></div><div class="fr-ribbon" id="frRibbon"></div><button class="fr-skip up hidden" id="frSkip"></button>' +
      '<div id="frMap"></div><button class="fr-hit" id="frHit"></button>' +
      '<div class="fr-call" id="frCall"><b id="frCallB"></b><span class="up" id="frCallS"></span></div>' +
      '<div class="fr-act" id="frAct"><button id="frLook"></button><button id="frGo"></button></div>' +
      '<div class="fr-ovt" id="frOvT"></div><div class="fr-ovb" id="frOvB"></div>' +
      '<aside class="side fr-side" id="frSide" tabindex="-1"></aside><footer class="fr-cred" id="frCred"></footer></div></div>');
    document.body.appendChild(root);
    // The header is the day's: in the game the place stands where the day's number does, its clock where the day's
    // countdown does, and Back before the logo.
    $("logo").insertAdjacentHTML("beforebegin", '<button class="sh-b fr-hb" id="frHB"></button>');
    $("logo").insertAdjacentHTML("beforeend", '<span class="up fr-no" id="frDay"></span>');
    $("timer").insertAdjacentHTML("afterend", '<div class="timer fr-clock" id="frClock"><span class="up" id="frClockL"></span><b id="frClockV"></b></div>');
    $("frBack").onclick = $("frHB").onclick = () => onBack && onBack();
    const acts = (e) => {
      const b = e.target.closest("[data-act]");
      if (!b || b.disabled) return;
      const f = ACTS[b.dataset.act];
      if (f) { e.preventDefault(); f(b); }
    };
    root.addEventListener("click", acts);
    addEventListener("keydown", onKey);
    addEventListener("keyup", offKey);
    addEventListener("blur", release);
    // The day's page reacts to the window crossing into the computer's layout and back as if its days were open: what
    // it put out of reach for them is this layer's to set.
    Desk.change(() => { if (opened) behind(true); });
    return root;
  }
  const backButton = () => onBack && onBack();
  // Everything but this layer and the toast is out of reach while it is open, and the header too where a page covers
  // it (a phone outside the game). body.is-sheet tells the day's page and the computer's keys and map panel that a page
  // is over the game, as the days' list does.
  function behind(on) {
    const head = Desk.on || root.classList.contains("fr-g");
    for (const e of document.body.children) {
      if (e === root || e.id === "toast" || e.tagName === "SCRIPT") continue;
      e.inert = on && !(head && e.classList.contains("top"));
    }
    root.inert = false;
  }
  // Where the focus was before the layer opened, to put it back when it closes.
  let before = null;
  function open() {
    layer();
    if (opened) return;
    opened = true;
    before = document.activeElement;
    root.classList.add("on"); document.body.classList.add("is-free", "is-sheet");
    behind(true);
    if (tgBack()) { tg.BackButton.onClick(backButton); tg.BackButton.show(); }
    focusing = true;
  }
  function close() {
    opened = false;
    stopClock(); Kit.stopped(); guard(false); hideGame(); release();
    root.classList.remove("on"); document.body.classList.remove("is-free", "is-sheet", "fr-up", "fr-tg");
    behind(false);
    if (tgBack()) { tg.BackButton.offClick(backButton); syncBack(); }
    // Opened on the free game, the app has not loaded the day yet: it does now.
    if (dayBoot && !dayBooted) { dayBooted = true; $("loader").classList.remove("gone"); dayBoot(); }
    if (before && before.isConnected) before.focus({ preventScroll: true });
    before = null;
  }
  // The title of a page, or of the game in the header; the line under it; and what Back does here: one step up. A page
  // without back is one of the top: the mode's settings, the archive, the lobby. Its way up is the menu (or the list of
  // days it was opened from), and on a computer the header is: the page has no button. In Telegram, Telegram's Back is
  // the only one, on every screen. Escape does what the button does.
  let SUB = "", focusing = false;
  const tgBack = () => signed() && tgAt("6.1");
  function top(title, sub, back) {
    $("frTitle").textContent = title;
    $("frDay").textContent = title;
    $("frClockL").textContent = sub || "";
    SUB = sub || "";
    const arrow = document.documentElement.dir === "rtl" ? "→" : "←";
    for (const b of [$("frBack"), $("frHB")]) { b.textContent = arrow; b.setAttribute("aria-label", T("free.back")); }
    document.body.classList.toggle("fr-up", !back);
    document.body.classList.toggle("fr-tg", tgBack());
    onBack = back || leave;
    // Just opened: the focus goes to Back where there is one, else to the page.
    if (focusing) { focusing = false; ($("frBack").offsetParent ? $("frBack") : $("frPage")).focus({ preventScroll: true }); }
  }
  // A page: settings, the lobby, a result. The game screen goes while one is up. Drawn again with keep (a setting
  // changed, someone joined), it stays where it was scrolled to, and the focus on the same button.
  const same = (a, b) => a.dataset.act === b.dataset.act && a.dataset.k === b.dataset.k && a.dataset.v === b.dataset.v;
  function page(html, cls, keep) {
    stopClock(); ribbon(null); hideGame();
    const p = $("frPage"), y = p.scrollTop, had = keep && p.contains(document.activeElement) ? document.activeElement : null;
    p.className = "sh-body fr-body " + (cls || "");
    p.parentNode.classList.toggle("wide", WIDE.test(cls || ""));
    p.innerHTML = (SUB ? '<p class="fr-sub up">' + esc(SUB) + '</p>' : '') + html;
    p.scrollTop = keep ? y : 0;
    const again = had && had.dataset && [...p.querySelectorAll("[data-act]")].find((b) => same(b, had));
    if (again) again.focus({ preventScroll: true });
    return p;
  }
  // The pages laid out in columns on a computer, as wide as the shell's pages.
  const WIDE = /\bfr-(home|lobby|arch)\b/;
  const wait = () => page('<p class="fr-wait up">' + esc(lines("loading")[0]) + '</p>');
  function failed(e, again) {
    ACTS.retry = again;
    page('<p class="fr-wait">' + esc(ferr(e)) + '</p><div class="fr-go"><button class="fr-sec" data-act="retry">' + esc(T("free.retry")) + '</button></div>');
  }

  /* ---------- Settings ---------- */
  const ROUNDS = { 3: "free.r3", 5: "free.r5", 10: "free.r10" };
  // A streak and a game against the clock have no number of places; the clock's game has three minutes in all.
  const endless = (s) => s.mode === "streak" || s.mode === "timed";
  const key = (s) => endless(s) ? s.mode + "/" + s.seconds : "classic/" + s.seconds + "/" + s.rounds;
  const bestKey = (s) => s.mode === "streak" ? "free.best_streak" : s.mode === "timed" ? "free.best_timed" : "free.best";
  const secLabel = (n) => n ? T("free.sec", { n: fmt(n) }) : T("free.no_timer");
  // The modes of the menu's cards. Blitz is the classic on 30 s a place, with nothing but the places to choose.
  const MODES = { classic: "free.classic", blitz: "free.blitz", streak: "free.streak", timed: "free.timed" };
  const HINTS = { classic: "free.classic_hint", blitz: "free.blitz_hint", streak: "free.streak_hint", timed: "free.timed_hint" };
  const modeOf = (s) => s.mode === "classic" ? (s.seconds === 30 ? "blitz" : "classic") : s.mode;
  const modeName = (s) => T(s && MODES[modeOf(s)] || "free.title");
  // "Blitz · 5 places", "60 s · 10 places", "Country streak · 30 s": the name of a set of settings and its records.
  function setLabel(s) {
    if (s.mode === "timed") return T("free.timed_set");
    if (s.mode === "streak") return T("free.streak") + " · " + T("free.sec", { n: fmt(s.seconds) });
    return (s.seconds === 30 ? T("free.blitz") : s.seconds ? T("free.sec", { n: fmt(s.seconds) }) : T("free.no_timer")) + " · " + T(ROUNDS[s.rounds]);
  }
  // One choice of a set: ◉ on the current one, as the bot's panels do.
  function seg(k, items, cur, locked) {
    return '<div class="fr-seg" role="radiogroup" aria-labelledby="frL-' + k + '">' + items.map(([v, label]) => '<button role="radio" aria-checked="' + (v === cur) + '" data-act="set" data-k="' + k +
      '" data-v="' + v + '"' + (locked ? " disabled" : "") + '>' + esc(label) + '</button>').join("") + '</div>';
  }
  // A settings block: a mode's own, or the lobby's, which has every timer and no "no timer". The clock's game has
  // nothing to choose; a blitz only its places.
  function settings(s, party, locked) {
    const m = party ? "party" : modeOf(s);
    if (m === "timed") return "";
    let html = "";
    const times = m === "streak" ? [30, 60] : m === "party" ? [30, 60, 90, 120] : m === "classic" ? [60, 90, 120, 0] : null;
    if (times) html += '<p class="fr-l up" id="frL-seconds">' + esc(T("free.time")) + '</p>' + seg("seconds", times.map((n) => [n, secLabel(n)]), s.seconds, locked);
    if (m !== "streak") html += '<p class="fr-l up" id="frL-rounds">' + esc(T("free.places")) + '</p>' + seg("rounds", [3, 5, 10].map((n) => [n, fmt(n)]), s.rounds, locked);
    return html;
  }
  // A choice made: a streak has a 30 or 60 s timer and no number of places, the clock's game three minutes and nothing
  // to choose.
  function choose(s, k, v) {
    const n = Object.assign({}, s);
    n[k] = k === "mode" ? v : +v;
    if (n.mode === "timed") { n.seconds = 180; n.rounds = 0; }
    else if (n.mode === "streak") { n.rounds = 0; if (n.seconds !== 30) n.seconds = 60; }
    else { if (!n.rounds) n.rounds = 5; if (n.seconds === 180) n.seconds = 60; }
    return n;
  }
  // The menu's cards open the settings on a mode: /play?mode=classic|blitz|streak|clock. Blitz is the classic on 30 s;
  // a mode chosen keeps what it can of the last game's settings.
  function modeSet(mode, last) {
    const l = last || {}, rounds = l.rounds || 5;
    if (mode === "blitz") return { mode: "classic", seconds: 30, rounds };
    if (mode === "classic") return { mode: "classic", seconds: l.mode === "classic" && l.seconds !== 30 ? l.seconds : 60, rounds };
    if (mode === "streak") return { mode: "streak", seconds: l.mode === "streak" ? l.seconds : 60, rounds: 0 };
    if (mode === "clock") return { mode: "timed", seconds: 180, rounds: 0 };
    return null;
  }

  /* ---------- The game screen: the day's, with a panorama and a globe of its own ---------- */
  // phase: "guess" while a pin may go down, "sent" once a party's answer is in, "reveal" with the place on the map.
  let pano = null, globe = null, view = "photo", phase = "guess", pinned = false, onPin = null, nopic = false;
  // The bottom strip's main button: what it does and, after the guess, what it says.
  let GO = { act: "", label: "", off: false };
  function stage() {
    if (pano) return;
    pano = createPano({ el: $("frPano"), compass: $("frCompass"), tape: $("frTape"), label: (k) => TX["compass_" + k] || "", haptic });
    pano.on("error", () => toast(T("no_image")));
    globe = createGlobe({ el: $("frMap"), lang: LANG, mini: miniAt, insets: reserve, haptic,
      spin: () => opened && root.classList.contains("fr-g") && $("loader").classList.contains("gone") });
    // A tap beside the globe returns to the photo; in the computer's panel the void is the panel's water, a miss.
    globe.on("beside", () => { if (!Desk.on) toPhoto(); });
    globe.on("pick", ({ lngLat, point }) => { if (phase === "guess" && onPin) putPin(lngLat, point); });
    globe.on("pinmoved", () => { haptic("select"); toast(pick(lines("moved"))); if (onPin) onPin(here()); });
    $("frMap").inert = true;
    $("frHit").addEventListener("click", (e) => {
      toMap();
      if (panel()) { held = true; if (e.detail === 0) requestAnimationFrame(() => globe.map.getCanvas().focus()); }
    });
    $("frLook").addEventListener("click", look);
    // The layout switched: the same state, laid out again, the camera starting over in the map's new box.
    Desk.change(() => {
      if (!root.classList.contains("fr-g")) return;
      place(); split(); globe.resize();
      if (view !== "map") globe.toMini(0);
      else if (phase === "reveal" && globe.answered) globe.frame(0);
      else globe.toBig(0);
    });
    panelEvents();
  }
  const here = () => { const at = globe.guess.wrap(); return { lat: at.lat, lon: at.lng }; };
  function showGame() {
    stage();
    if (root.classList.contains("fr-g")) return;
    root.classList.add("fr-g"); document.body.classList.add("fr-play");
    if (opened) behind(true);
    place(); globe.resize();
  }
  function hideGame() {
    if (!root || !root.classList.contains("fr-g")) return;
    root.classList.remove("fr-g", "fr-m"); document.body.classList.remove("fr-play");
    if (opened) behind(true);
    view = "photo"; guard(false); hideSkip();
    if (pano) { pano.still(true); $("frMap").inert = true; }
  }
  // The small globe sits under its button; before layout, where it would be.
  function miniAt() {
    const g = $("frHit").getBoundingClientRect(), m = $("frMap").getBoundingClientRect(), W = $("frMap").clientWidth, H = $("frMap").clientHeight;
    return { x: g.width ? g.left - m.left + g.width / 2 : W - 12 - MINI / 2, y: g.height ? g.top - m.top + g.height / 2 : H - TAPE_H - 16 - MINI / 2, d: (g.width || MINI) - 4 };
  }
  // What the plates cover of the open map: the camera keeps the globe and the marks in the rest. On a computer the
  // plates are in the side panel, and only the answer's bar lies over the panel's bottom.
  function reserve() {
    if (Desk.on) return { top: 0, bottom: phase !== "reveal" ? $("frAct").offsetHeight : 0 };
    const top = $("frMap").getBoundingClientRect().top, on = (id) => $(id).classList.contains("on");
    const t = on("frOvT") ? $("frOvT").getBoundingClientRect().bottom - top + 8 : 0;
    return { top: t, bottom: (on("frOvB") ? $("frOvB").offsetHeight + 20 : 0) + ACT };
  }
  function overlay(t, b) {
    $("frOvT").innerHTML = t; $("frOvB").innerHTML = b;
    $("frOvT").classList.toggle("on", !!t); $("frOvB").classList.toggle("on", !!b);
  }
  // On a computer the map is a panel by the panorama until the answer, then the main area with the result beside it.
  function place() {
    root.classList.toggle("fr-dp", Desk.on && phase !== "reveal");
    root.classList.toggle("fr-ds", Desk.on && phase === "reveal");
    if (Desk.on && phase === "reveal" && root.contains(document.activeElement) && !shown(document.activeElement)) $("frSide").focus({ preventScroll: true });
    seePano();
  }
  const shown = (e) => e && e.isConnected && e.getClientRects().length > 0;
  // The panorama keeps still and out of the Tab order while the map covers it.
  const panoSeen = () => view === "photo" || (Desk.on && phase !== "reveal");
  function seePano() { if (!pano) return; const s = panoSeen() && !nopic; pano.still(!s); $("frPano").inert = !s; }
  function toMap() {
    if (view === "map") return;
    view = "map"; root.classList.add("fr-m"); haptic("light"); seePano();
    $("frMap").inert = false; lookLabel();
    globe.toBig();
  }
  function toPhoto() {
    if (view === "photo" || nopic) return;
    const inMap = $("frMap").contains(document.activeElement);
    view = "photo"; root.classList.remove("fr-m"); haptic("soft"); seePano();
    $("frMap").inert = true; lookLabel();
    if (inMap) $("frHit").focus({ preventScroll: true });
    globe.toMini(650);
  }
  const look = () => view === "map" ? toPhoto() : toMap();
  // The bottom strip, as the day's: one button until there is a pin, then two halves; in the computer's panel the
  // answer's bar, dimmed until the pin. After the answer its second half is the way on.
  function split() {
    const bar = Desk.on && phase !== "reveal", go = $("frGo");
    // Once a party's answer is in, the second half says who the party still waits for.
    const on = phase === "guess" ? pinned : phase === "reveal" ? !!GO.act : !!GO.label;
    $("frAct").classList.toggle("on", on);
    $("frAct").classList.toggle("one", nopic);
    if (phase === "guess") go.textContent = T(bar ? (pinned ? (Desk.keyboard ? "desk.answer_key" : "final") : "desk.pin_first") : "final");
    else go.textContent = GO.label;
    go.dataset.act = GO.act;
    go.disabled = phase === "guess" ? bar && !pinned : !GO.act || GO.off;
    go.tabIndex = on || bar ? 0 : -1; go.toggleAttribute("aria-hidden", !on && !bar);
    lookLabel();
  }
  function lookLabel() {
    const k = phase === "reveal" ? "walk" : phase === "sent" ? "admire" : pinned ? "look" : "look_more";
    const t = Desk.on && phase === "reveal" && view === "photo" ? T("desk.to_map") : T(k);
    $("frLook").textContent = t;
    const s = $("frSLook");
    if (s) s.textContent = view === "photo" ? T("desk.to_map") : T("walk");
  }
  // The caption by the small globe; its arrow points at the globe, which stays in the right corner.
  function setCall(b, sub) {
    $("frCallB").textContent = b;
    $("frCallS").innerHTML = document.documentElement.dir === "rtl" ? "<em>→</em> " + esc(sub) : esc(sub) + " <em>→</em>";
  }
  function putPin(lngLat, point) {
    const first = !globe.guess;
    globe.pin(lngLat, { draggable: true, drop: true });
    toast(pick(lines(globe.water(point) ? "on_water" : first ? "on_land" : "moved")));
    haptic("medium");
    if (first) { pinned = true; setCall(T("pin_set"), T("answer")); guard(true); split(); }
    onPin(here());
  }
  // A place, or a game that opens on an answer without one: the picture, the small globe without a pin, the caption
  // that asks where it is.
  function newRound(r) {
    showGame();
    phase = "guess"; pinned = false; onPin = null; nopic = !r || !r.image; inside = held = false;
    GO = { act: "", label: "", off: false };
    globe.ready(() => { if (globe.answered || globe.guess) globe.clear(); });
    overlay("", ""); $("frSide").innerHTML = ""; credits(null); hideSkip();
    view = "photo"; root.classList.remove("fr-m"); $("frMap").inert = true;
    place();
    globe.toMini(0);
    if (!nopic) pano.show({ image: r.image, kind: r.kind, heading: r.heading || 0 });
    setCall(T("where"), T("drop_pin"));
    split();
  }
  // The button of the strip for this place: send, or nothing yet.
  function goTo(act) { GO = { act, label: "", off: false }; split(); }
  // A party's answer is in: the pin stays, the strip and the caption say who is still guessing.
  function sent(left) {
    phase = "sent"; onPin = null; guard(false); hideSkip();
    if (globe.guess) globe.lock();
    GO = { act: "", label: left ? T("party.waiting", { n: fmt(left) }) : T("sent"), off: true };
    setCall(T("sent"), left ? T("party.waiting_n", { n: fmt(left) }) : "");
    split(); place();
  }
  function lock() { onPin = null; guard(false); if (globe && globe.guess) globe.lock(); }
  // Over the panorama: Skip against the clock, Give up without a timer.
  function skip(act, label) { $("frSkip").dataset.act = act; $("frSkip").textContent = label; $("frSkip").classList.remove("hidden"); }
  function hideSkip() { if (root) $("frSkip").classList.add("hidden"); }
  // As in the day's game, a pin set and not yet sent asks before the app closes. Only what the free game turned on
  // is turned off: the day keeps its own.
  let guarding = false;
  function guard(on) {
    if (on === guarding || !signed() || !tgAt("6.2")) return;
    guarding = on;
    if (on) tg.enableClosingConfirmation(); else tg.disableClosingConfirmation();
  }
  // The photo's author is named only with the place; the map's always.
  function credits(p) {
    let photo = "";
    if (p && SOURCES[p.source]) {
      const [name, url] = SOURCES[p.source], lic = (p.license || "").replace(/^CC-/, "CC ").replace(/-(\d)/, " $1");
      photo = '<span>© ' + (p.author ? esc(p.author) + ", " : "") + '<a href="' + url + '" target="_blank" rel="noopener">' + name + '</a> ' + esc(lic) + ' ·</span>';
    }
    $("frCred").innerHTML = photo + MAP_CREDITS;
  }

  /* The computer's map panel: opens when the mouse rests on the globe, on a click or on M; folds 600 ms after the
     mouse leaves, unless a click holds it, a drag goes on or the keyboard is in it. As desk.js does for the day. */
  let inside = false, held = false, down = false, openT = 0, closeT = 0;
  const panel = () => Desk.on && opened && root.classList.contains("fr-g") && phase !== "reveal";
  const inPanel = (e) => !!e && ["frMap", "frAct"].some((id) => $(id).contains(e));
  function later() {
    clearTimeout(closeT);
    closeT = setTimeout(() => {
      if (!panel() || view !== "map" || inside || held) return;
      if (down || inPanel(document.activeElement) || globe.map.isMoving()) { later(); return; }
      toPhoto();
    }, 600);
  }
  function track(e) {
    if (e.pointerType !== "mouse") return;
    // Out of the panel's time (an answer on screen, a page) the mouse is nowhere: the next place starts outside.
    if (!panel()) { inside = false; return; }
    const r = (view === "map" ? $("frMap") : $("frHit")).getBoundingClientRect();
    const now = e.clientX >= r.left && e.clientX <= r.right && e.clientY >= r.top && e.clientY <= r.bottom;
    if (now === inside) return;
    inside = now;
    clearTimeout(openT);
    if (inside && view !== "map") openT = setTimeout(() => { if (inside && panel()) toMap(); }, 150);
    if (inside) clearTimeout(closeT);
    else if (view === "map") { held = false; later(); }
  }
  function panelEvents() {
    document.addEventListener("pointermove", track);
    document.addEventListener("pointerdown", (e) => {
      down = panel() && inPanel(e.target);
      // A finger has no hover to leave by: a touch beside the open panel folds it.
      if (panel() && view === "map" && !down && e.pointerType !== "mouse" && !$("frHit").contains(e.target)) toPhoto();
    });
    const up = () => { if (down) { down = false; if (panel() && view === "map" && !inside) later(); } };
    document.addEventListener("pointerup", up);
    document.addEventListener("pointercancel", up);
  }

  /* Keys: the panorama turns with the arrows, M opens the map, Enter answers or goes on, Esc folds the map or goes back. */
  const STEER = { ArrowLeft: ["yaw", -1], KeyA: ["yaw", -1], ArrowRight: ["yaw", 1], KeyD: ["yaw", 1],
    ArrowUp: ["pitch", 1], KeyW: ["pitch", 1], ArrowDown: ["pitch", -1], KeyS: ["pitch", -1] };
  const ZOOM = { "+": 1, "=": 1, "-": -1, "_": -1, "−": -1 };
  const keysHeld = new Map();
  function steerNow(axis) {
    let dir = 0;
    keysHeld.forEach(([a, d]) => { if (a === axis) dir = d; });
    pano.steer(axis, dir);
  }
  function release() { keysHeld.clear(); if (pano) ["yaw", "pitch", "zoom"].forEach((a) => pano.steer(a, 0)); }
  function onKey(e) {
    if (pano && e.metaKey) release();
    if (!opened || e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey) return;
    if (document.querySelector(".acct-dlg, .acct-pop")) return;
    const t = e.target, k = e.key, code = e.code;
    if (t.closest && t.closest("input, textarea, select, [contenteditable]")) return;
    const game = root.classList.contains("fr-g"), onButton = !!(t.closest && t.closest("button, a"));
    if (k === "Escape") {
      e.preventDefault();
      if (game && view === "map" && (phase !== "reveal" || !Desk.on) && !nopic) toPhoto(); else onBack && onBack();
      return;
    }
    if (!game) return;
    if (e.repeat && !STEER[code] && !ZOOM[k]) { if (code === "KeyM" || (k === "Enter" && !onButton)) e.preventDefault(); return; }
    if (code === "KeyM") {
      e.preventDefault();
      if (view === "map") toPhoto();
      else { toMap(); if (phase === "guess") globe.map.getCanvas().focus(); }
      return;
    }
    if (k === "Enter") {
      // Not the next day of the archive: a day is taken once, and Enter has just sent the answer to this one.
      const go = $("frGo");
      if (!onButton && go.dataset.act && go.dataset.act !== "nextDay" && !go.disabled) { e.preventDefault(); go.click(); }
      return;
    }
    const inMap = !!(t.closest && t.closest("#frMap"));
    if (inMap || !panoSeen() || nopic) return;
    if (STEER[code]) { e.preventDefault(); keysHeld.set(code, STEER[code]); steerNow(STEER[code][0]); return; }
    if (ZOOM[k]) { e.preventDefault(); keysHeld.set(code || k, ["zoom", ZOOM[k]]); steerNow("zoom"); return; }
    if (k === "0") { e.preventDefault(); pano.resetZoom(); return; }
    if (code === "KeyN") { e.preventDefault(); pano.faceNorth(); }
  }
  function offKey(e) {
    const c = keysHeld.has(e.code) ? e.code : keysHeld.has(e.key) ? e.key : "";
    if (!c) return;
    const [axis] = keysHeld.get(c); keysHeld.delete(c); steerNow(axis);
  }

  /* ---------- The clock in the header ---------- */
  let tickT = 0;
  // limit is the place's time in seconds; done runs once when it is up.
  function clock(endsAt, limit, done) {
    stopClock();
    const bar = $("frBar"), v = $("frClockV");
    bar.classList.toggle("off", !endsAt);
    if (!endsAt) return;
    let fired = false;
    const step = () => {
      const ms = msTo(endsAt), s = Math.max(0, Math.ceil(ms / 1000)), low = s <= 10;
      v.textContent = s >= 60 ? Math.floor(s / 60) + ":" + String(s % 60).padStart(2, "0") : T("free.sec", { n: fmt(s) });
      $("frClock").classList.toggle("low", low); bar.classList.toggle("low", low);
      bar.firstChild.style.transform = "scaleX(" + Math.max(0, Math.min(1, ms / (limit * 1000))).toFixed(4) + ")";
      if (ms <= 0 && !fired) { fired = true; done(); }
    };
    step();
    tickT = setInterval(step, 200);
  }
  function stopClock() {
    clearInterval(tickT); tickT = 0;
    if (root) { $("frBar").classList.add("off"); $("frClockV").textContent = ""; $("frClock").classList.remove("low"); }
  }

  /* ---------- The answer ---------- */
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
  // The phone's plate over the map: the points large, the distance and what happened beside them.
  function scorePlate(g, note, extra) {
    const hit = g && g.lat != null;
    return '<div class="pl fr-sc"><div class="nums"><b>' + fmt(hit ? g.points : 0) + '</b><span class="up">' +
      [hit ? esc(kmT(g.km)) : "", note ? esc(note) : ""].filter(Boolean).join("<br>") + '</span></div>' +
      (hit && parts(g) ? '<p class="fr-parts">' + esc(parts(g)) + '</p>' : '') + (extra || "") + '</div>';
  }
  // The side panel's score, as the day's: the points, what was found, the bar in parts.
  function sideScore(g, note) {
    if (!g || g.lat == null) return '<div class="score on"><b class="pts">0</b><p class="got">' + esc(note || "") + '</p></div>';
    const r = { pts: g.points, lvl: levelOf(g.bonus), sp: g.speed || 0, d: g.km, me: true };
    const got = ["desk.got_continent", "desk.got_country", "desk.got_city"].map((k, i) => '<span class="' + (i < r.lvl ? "y" : "n") + '">' + esc(T(k)) + " " + (i < r.lvl ? "✓" : "✗") + '</span>');
    return '<div class="score on"><b class="pts">' + fmt(g.points) + '</b><p class="got">' + esc(kmT(g.km)) + ' · ' + got.join(" · ") + '</p>' +
      '<span class="bar">' + barOf(r) + '</span><p class="parts">' + esc(T("desk.parts", { a: fmt(g.distance), b: fmt(g.bonus || 0), c: fmt(g.speed || 0) })) + '</p>' +
      (note ? '<p class="fr-snote">' + esc(note) + '</p>' : '') + '</div>';
  }
  // The place: its name and its fact. On a phone a plate that folds into a tab; in the side panel the head of it.
  const placeText = (p) => esc(whereOf(p)) + (p.fact ? '<p>' + esc(p.fact) + '</p>' : '');
  const placePlate = (p) => '<div class="place up' + (innerHeight < 640 ? " min" : "") + '"><div class="t">' + placeText(p) + '</div>' +
    '<button class="x" data-act="fold" aria-label="' + esc(T("aria_collapse")) + '">' + FOLD + '</button>' +
    '<button class="tab" data-act="unfold">' + esc(T("about_place")) + ' ↑</button></div>';
  const btn = (b) => '<button class="' + (b.cls || "fr-sec") + '" data-act="' + b.act + '"' + (b.attrs || "") + '>' + esc(b.label) + '</button>';
  // The answer on screen. a: { place, me (your answer or null), note, others ([{ key, lat, lon, km, name }]), call (the
  // caption's word), top and side (more for the phone's plate and the side panel), bot (plates over the place's),
  // go ({ act, label }: the way on), more (further buttons: [{ act, label }]) }.
  function answer(a) {
    phase = "reveal"; onPin = null; guard(false); hideSkip();
    const P = a.place, g = a.me && a.me.lat != null ? a.me : null, more = a.more || [];
    credits(P);
    overlay(scorePlate(a.me, a.note, a.top), (a.bot || "") +
      (more.length ? '<div class="fr-pbs">' + more.map((b) => '<button class="pl fr-pb up" data-act="' + b.act + '"' + (b.attrs || "") + '>' + esc(b.label) + ' ' + fwd() + '</button>').join("") + '</div>' : '') + placePlate(P));
    $("frSide").innerHTML = '<div class="place"><div class="t">' + placeText(P) + '</div></div>' + sideScore(a.me, a.note) + (a.side || "") +
      '<div class="fr-sbtns">' + (a.go ? btn({ act: a.go.act, label: a.go.label, cls: "fr-pri" }) : "") + more.map((b) => btn(b)).join("") +
      (nopic ? "" : '<button class="fr-sec" data-act="look" id="frSLook"></button>') + '</div>';
    GO = a.go ? { act: a.go.act, label: a.go.label, off: false } : { act: "", label: "", off: false };
    setCall(a.call, T("see"));
    place(); split();
    globe.ready(() => {
      if (globe.answered) globe.clear();
      globe.pin(g ? [g.lon, g.lat] : null, { draggable: false });
      globe.answer({ truth: [P.lon, P.lat], km: g ? g.km : null,
        others: (a.others || []).filter((o) => o.lat != null).map((o) => ({ key: o.key, at: [o.lon, o.lat], d: o.km, label: o.name, face: av(o.name) })) });
      view = "map"; root.classList.add("fr-m"); seePano();
      $("frMap").inert = false; lookLabel();
      globe.frame(1200);
    });
  }
  // A list of answers or of a standing, as the days' rankings: place, face, name, distance, points; yours is white.
  function table(rows, faces) {
    return '<ol class="rk fr-rk' + (faces ? "" : " min") + '">' + rows.map((r) => '<li' + (r.me ? ' class="me"' : '') + '><span class="n">' + (r.rank ? fmt(r.rank) : "") + '</span>' +
      (faces ? '<span class="a">' + (r.face || av(r.name, r.me)) + '</span>' : '') + '<span class="w" lang="' + nameLang() + '">' + esc(r.name) + (r.mark ? ' <em>' + esc(r.mark) + '</em>' : '') + '</span>' +
      '<span class="k">' + (r.km != null ? esc(kmT(r.km)) : "") + '</span><b>' + (r.points != null ? fmt(r.points) : "") + '</b></li>').join("") + '</ol>';
  }

  /* ---------- Alone ---------- */
  let HOME = null, SET = null, GAME = null, ROUND = null, mine = null, sending = false;
  // gen moves on with every screen of the solo game, so an answer that comes back after Back draws nothing.
  let gen = 0, missT = 0;
  // The screen of a mode: its settings, the way to play it, its records. mode is a menu card's, or none for the mode
  // of the settings on screen.
  async function home(h, mode) {
    const my = ++gen; ROUND = null; clearTimeout(missT);
    if (mode && SET) SET = modeSet(mode, SET) || SET;
    top(modeName(SET), "");
    if (!h) {
      wait();
      // Records read again with the screen: a game since may have moved them.
      TOP.clear();
      try { h = await req("/api/free"); } catch (e) { if (my === gen) failed(e, () => home(null, mode)); return; }
      if (my !== gen) return;
    }
    HOME = h;
    if (h.lang && h.lang !== LANG) {
      await loadTexts(h.lang).catch(() => {});
      if (my !== gen) return;
    }
    if (!SET) SET = Object.assign({}, h.last);
    if (mode) SET = modeSet(mode, SET) || SET;
    drawHome();
  }
  // The way to sign in on the site, for a guest: records and friends come with any account.
  const signIn = (note) => keeps() || signed() ? "" : '<div class="fr-in"><p>' + esc(T(note || "free.sign_in_note")) + '</p>' +
    (typeof acctDialog === "function" && typeof ME !== "undefined" && ME ? '<button class="fr-pri" data-act="signIn">' + esc(T("acct.sign_in")) + '</button>' : '') + '</div>';
  // On a phone one column: the games to go back to, what the mode is, its settings and the way in, its records. On a
  // computer the settings on the left, what the mode is and its records on the right.
  function drawHome(keep) {
    const h = HOME, s = SET, m = modeOf(s);
    top(modeName(s), "");
    let html = "";
    if (h.party || h.open) {
      html += '<div class="fr-c">';
      if (h.party) html += '<button class="fr-wide" data-act="toParty" data-code="' + esc(h.party) + '">' + esc(T("free.to_party")) + ' <em>' + fwd() + '</em></button>';
      if (h.open) html += '<button class="fr-wide" data-act="resume">' + esc(T("free.continue")) + ' <em>' + fwd() + '</em></button>';
      html += '</div>';
    }
    html += '<div class="fr-d"><p class="fr-hint">' + esc(T(HINTS[m] || "free.classic_hint")) + '</p></div>';
    html += '<div class="fr-a">' + settings(s, false, false) + '<div class="fr-go"><button class="fr-pri" data-act="go">' + esc(T("free.go")) + '</button>';
    if (keeps()) html += '<button class="fr-sec" data-act="friends">' + esc(T("free.friends")) + '</button>';
    html += '</div></div><div class="fr-r">' + records(s);
    if (h.challenges && h.challenges.length) {
      html += '<p class="fr-l up">' + esc(T("free.challenges")) + '</p><div class="fr-chs">' + h.challenges.map((c) =>
        '<button class="fr-ch" data-act="toChallenge" data-code="' + esc(c.code) + '"><b lang="' + nameLang() + '">' +
        esc(c.maker ? T("free.challenge_mine") : T("free.challenge_by", { name: c.by })) + '</b><span>' + esc(setLabel(c.settings)) + ' · ' +
        esc(T("free.players", { n: fmt(c.players) })) + '</span><em>' + fwd() + '</em></button>').join("") + '</div>';
    }
    page(html + '</div>', "fr-home", keep);
    readRecords(s);
  }

  /* ---------- Records: your best on the settings, your place on their board, the week's first three ---------- */
  // The boards are /api/top's: a game without a timer is on none, a blitz is the classic on 30 s.
  const TOP = new Map();
  const boardOf = (s) => !s.seconds ? "" : s.mode === "timed" ? "board=timed&seconds=180" : s.mode === "streak" ? "board=streak&seconds=" + s.seconds :
    "board=" + (s.seconds === 30 ? "blitz" : "classic") + "&seconds=" + s.seconds + "&rounds=" + s.rounds;
  // A board read, false when it could not be, undefined while it is on the way.
  const topOf = (s, period) => boardOf(s) ? TOP.get(boardOf(s) + "&period=" + period) : false;
  const UNIT = { streak: "free.streak_len", timed: "free.timed_len" };
  function records(s) {
    let html = '<section class="fr-recs" id="frRecs">';
    if (keeps()) {
      const best = HOME.best && HOME.best[key(s)], all = topOf(s, "all"), me = all && all.rows.find((r) => r.me);
      html += '<p class="fr-l up">' + esc(T("free.rec_mine")) + '</p>';
      // Points are a number alone, as at the end of a game; countries say what they count.
      html += best ? '<div class="fr-mine"><b>' + fmt(best) + '</b>' + (UNIT[s.mode] ? '<span class="up">' + esc(T(UNIT[s.mode])) + '</span>' : '') + '</div>' +
        (me ? '<p class="fr-best">' + esc(T("free.rec_rank", { rank: fmt(me.rank), of: fmt(all.total) })) + '</p>' : '')
        : '<p class="fr-hint">' + esc(T("free.rec_none")) + '</p>';
    } else html += signIn();
    if (!boardOf(s)) return html + '<p class="fr-hint">' + esc(T("free.no_board")) + '</p></section>';
    const week = topOf(s, "week");
    if (week === false) return html + '</section>';
    html += '<p class="fr-l up">' + esc(T("free.rec_week")) + '</p>';
    if (!week) html += '<p class="fr-hint fr-busy">' + esc(lines("loading")[0]) + '</p>';
    else if (!week.rows.length) html += '<p class="fr-hint">' + esc(T("free.rec_week_none")) + '</p>';
    // The faces of the leaderboards' rows: "Anonymous" with the plain one, a photo when /api/top sent it.
    else html += table(week.rows.slice(0, 3).map((r) => ({ rank: r.rank, name: r.name, me: r.me, points: r.score,
      face: r.anon && !r.me ? anonAv() : av(r.name, r.me, r.photo) })), true);
    return html + '</section>';
  }
  // The boards of the settings on screen, each read once; the block is drawn again when they come, if the settings
  // are still the same.
  async function readRecords(s) {
    const b = boardOf(s);
    if (!b) return;
    const qs = (keeps() ? ["all", "week"] : ["week"]).map((p) => b + "&period=" + p).filter((q) => !TOP.has(q));
    if (!qs.length) return;
    qs.forEach((q) => TOP.set(q, undefined));
    await Promise.all(qs.map((q) => req("/api/top?" + q).then((d) => TOP.set(q, d), () => TOP.set(q, false))));
    const e = $("frRecs");
    if (e && SET && boardOf(SET) === b && $("frPage").classList.contains("fr-home")) e.outerHTML = records(SET);
  }
  // The settings of a party made from the solo ones: a classic on a timer.
  const partySet = (s) => s.mode === "timed" ? { mode: "classic", seconds: 60, rounds: 5 } : s.mode === "streak" ? { mode: "classic", seconds: s.seconds, rounds: 5 } :
    { mode: "classic", seconds: s.seconds || 60, rounds: s.rounds };
  const ACTS = {
    set(b) {
      if (Kit.lobby && Kit.lobby(b)) return;
      SET = choose(SET, b.dataset.k, b.dataset.v); haptic("select"); drawHome(true);
    },
    async go(b) {
      const g = await fetchGame(b, "/api/free/solo", SET);
      if (g) { haptic("medium"); next(); }
    },
    async resume(b) { if (await fetchGame(b, "/api/free/solo/" + HOME.open)) continueGame(); },
    friends() { Kit.party.create(partySet(SET)); },
    signIn() {
      acctDialog();
      // The dialog gives the page back to everything when it closes: this layer takes it again.
      const d = $("acctDlg");
      if (!d) return;
      const o = new MutationObserver(() => { if (!d.isConnected) { o.disconnect(); if (opened) behind(true); } });
      o.observe(document.body, { childList: true });
    },
    home() { home(); },
    toParty(b) { Router.go("/party/" + b.dataset.code, { replace: true }); },
    look() { look(); },
    fold(b) { const p = b.closest(".place"); p.classList.add("min"); p.querySelector(".tab").focus({ preventScroll: true }); globe.keepInView(); },
    unfold(b) { const p = b.closest(".place"); p.classList.remove("min"); p.querySelector(".x").focus({ preventScroll: true }); globe.keepInView(); },
    send() { sendSolo(false); },
    giveUp() { sendSolo(false, true); },
    next(b) { b.disabled = true; haptic("light"); GAME.next ? next() : final(); },
    again(b) { ACTS.go(b); },
    settings() { home(HOME); },
    archive() { archive(); },
    day(b) { takeDay(b, +b.dataset.n); },
    async played(b) { if (await fetchGame(b, "/api/free/solo/" + b.dataset.game)) continueGame(); },
    nextDay(b) { const d = nextOpen(); d ? takeDay(b, d.n) : archive(); },
    toDay() { Router.go("/days/" + GAME.day.date); },
    toChallenge(b) { challenge(b.dataset.code); },
    async dare(b) {
      const my = gen, g = GAME;
      b.disabled = true;
      let d;
      try { d = await req("/api/free/solo/" + g.id + "/challenge", {}); } catch (e) { b.disabled = false; if (my === gen) toast(ferr(e)); return; }
      g.challenge = d.code;
      if (my !== gen) return;
      haptic("success"); challenge(d.code);
    },
    async take(b) { if (await fetchGame(b, "/api/free/challenge/" + CH.code + "/play", {})) { haptic("medium"); continueGame(); } },
    async mine(b) { if (await fetchGame(b, "/api/free/solo/" + CH.mine)) continueGame(); },
    async invite(b) {
      b.disabled = true;
      let d = null;
      try { d = await req("/api/free/challenge/" + CH.code + "/invite", {}); } catch (e) { toast(ferr(e)); }
      b.disabled = false;
      if (!d) return;
      // shareMessage throws while its sheet is still open; the link is the fallback.
      if (d.id && signed() && tgAt("8.0") && tg.shareMessage) { try { tg.shareMessage(d.id); return; } catch {} }
      share(d.link, T("free.challenge_text", { set: setLabel(CH.settings) }));
    },
  };
  // A game asked for from a button: it becomes the game on screen only if the screen is still the one asked from.
  async function fetchGame(b, path, body) {
    const my = gen;
    b.disabled = true;
    let g;
    try { g = await req(path, body); } catch (e) { b.disabled = false; if (my === gen) toast(ferr(e)); return null; }
    if (my !== gen) return null;
    GAME = g;
    return g;
  }
  // A link to pass on: Telegram's share sheet in Telegram, the system's or the clipboard on the site.
  function share(url, text) {
    if (signed() && tgAt("6.1")) { tg.openTelegramLink("https://t.me/share/url?" + new URLSearchParams({ url, text })); return; }
    if (navigator.share) { navigator.share({ text, url }).catch(() => {}); return; }
    navigator.clipboard.writeText(text + " " + url).then(() => toast(T("copied", { line: url })), () => toast(url));
  }
  // A day taken is the player's from then on: the list here says so without asking the server again.
  async function takeDay(b, n) {
    const my = gen;
    b.disabled = true;
    let g;
    try { g = await req("/api/free/archive", { n }); }
    catch (e) {
      b.disabled = false;
      if (my !== gen) return;
      toast(ferr(e));
      // Answered or seen since the list was drawn: the list read again says so.
      if (e.message === "day_seen" || e.message === "day_answered") archive();
      return;
    }
    const d = DAYS && DAYS.find((x) => x.n === n);
    if (d) { d.state = "played"; d.game = g.id; }
    if (my !== gen) return;
    GAME = g;
    haptic("medium"); continueGame();
  }
  const nextOpen = () => DAYS && DAYS.find((x) => x.state === "open" && (!GAME || !GAME.day || x.n !== GAME.day.n));
  // A day from the archive ends on its one place, so its place is its end; so is an over game's last.
  function continueGame() {
    // A day left without its answer before days from the archive waited for one: back to the list.
    if (GAME.day && GAME.status !== "playing") return GAME.done.length ? revealSolo() : archive();
    if (GAME.status !== "playing") return final();
    if (GAME.current) return play(GAME.current);
    if (GAME.next) return next();
    final();
  }
  async function next() {
    const my = ++gen;
    wait();
    let r;
    try { r = await req("/api/free/solo/" + GAME.id + "/start", { n: GAME.next }); }
    catch (e) {
      if (my !== gen) return;
      // The clock's game ran out between the answer and the next place: the game read again is over.
      if (e.message === "game_over") {
        let g = null;
        try { g = await req("/api/free/solo/" + GAME.id); } catch {}
        if (my === gen) { if (g) GAME = g; final(); }
        return;
      }
      failed(e, () => next()); return;
    }
    if (my === gen) play(r);
  }
  // Where Back goes from a game: a day to the archive, a game of a challenge to its standings, anything else to the screen.
  const gameBack = () => GAME.day ? archive() : GAME.challenge ? challenge(GAME.challenge) : home();
  const placeTitle = (g, n) => g.day ? no("#" + fmt(g.day.n)) : endless(g.settings) ? T("free.place", { n: fmt(n) }) : T("free.place_n", { n: fmt(n), total: fmt(g.settings.rounds) });
  function nowLine(g) {
    if (g.day) return dayDate(g.day.date);
    if (g.settings.mode === "streak") return T("free.streak_now", { n: fmt(g.streak) });
    if (g.settings.mode === "timed") return T("free.timed_now", { n: fmt(g.countries) });
    return T("free.points_now", { n: fmt(g.points) });
  }
  function play(r) {
    gen++; clearTimeout(missT);
    ROUND = r; mine = null; sending = false;
    top(placeTitle(GAME, r.n), nowLine(GAME), gameBack);
    newRound(r);
    onPin = (p) => { mine = p; };
    if (r.repeat) toast(T("free.repeat"));
    goTo("send");
    // Without a timer giving up is the way out; against the clock a place may be passed, which costs the time it took.
    if (!r.ends_at) skip("giveUp", T("free.give_up"));
    else if (GAME.settings.mode === "timed") skip("giveUp", T("free.pass"));
    clock(r.ends_at, GAME.settings.seconds, () => {
      // At zero a pin set is sent by itself; without one the server counts the place as missed once the grace is over.
      if (mine) sendSolo(true);
      else missT = setTimeout(missed, GRACE + 600);
    });
  }
  async function sendSolo(auto, skipped) {
    if (sending || !ROUND) return;
    const my = gen;
    sending = true; $("frGo").disabled = true; $("frSkip").disabled = true;
    const body = skipped || !mine ? { n: ROUND.n, skip: true } : { n: ROUND.n, lat: mine.lat, lon: mine.lon, auto };
    let g;
    try { g = await req("/api/free/solo/" + GAME.id + "/guess", body); }
    catch (e) {
      if (my !== gen) return;
      sending = false; split(); $("frSkip").disabled = false;
      if (e.message === "already_answered") return missed();
      toast(ferr(e)); return;
    }
    $("frSkip").disabled = false;
    if (my !== gen) return;
    GAME = g;
    haptic(skipped ? "light" : "success");
    revealSolo(skipped ? "passed" : "");
  }
  // The time ran out without a pin: the server has written the miss, and the game read again shows the place.
  async function missed() {
    if (!ROUND || !opened) return;
    const my = gen, n = ROUND.n;
    let g;
    try { g = await req("/api/free/solo/" + GAME.id); } catch (e) { if (my === gen) toast(ferr(e)); return; }
    if (my !== gen) return;
    GAME = g;
    if (GAME.done.some((d) => d.n === n)) revealSolo("missed");
  }
  // how: "passed" for a place given up or passed, "missed" for one whose time ran out.
  function revealSolo(how) {
    stopClock();
    const d = GAME.done[GAME.done.length - 1], g = d.guess, s = GAME.settings;
    // A game opened on its answer (a day played before) has no picture: the map alone.
    if (!ROUND) newRound(null);
    ROUND = null;
    top(placeTitle(GAME, d.n), nowLine(GAME), gameBack);
    const note = d.late ? T("free.late") : g.lat == null ? (how === "missed" ? T("free.missed") : s.mode === "timed" ? T("free.passed") : s.seconds ? T("free.missed") : T("free.gave_up"))
      : s.mode === "timed" ? T(g.country ? "free.timed_ok" : "free.timed_miss") : s.mode === "streak" ? T(g.country ? "free.country_ok" : "free.country_miss") : "";
    const a = { place: d.place, me: g, note, call: fmt(g.lat != null ? g.points : 0) };
    if (GAME.day) {
      // A day from the archive ends here: where it would have stood among the day's answers, and the way on.
      const f = GAME.final || {}, e = DAYS && DAYS.find((x) => x.n === GAME.day.n);
      if (e) { e.state = "played"; e.game = GAME.id; e.points = f.points; }
      const rank = f.day_of > 1 ? T("free.day_rank", { rank: fmt(f.day_rank), of: fmt(f.day_of) }) : f.day_of === 1 ? T("free.day_nobody") : "";
      if (rank) { a.top = '<p class="fr-rank">' + esc(rank) + '</p>'; a.side = '<p class="pl fr-rank">' + esc(rank) + '</p>'; }
      const more = !!nextOpen();
      a.go = more ? { act: "nextDay", label: T("free.next_day") } : { act: "archive", label: T("free.to_archive") };
      a.more = (more ? [{ act: "archive", label: T("free.to_archive") }] : []).concat([{ act: "toDay", label: T("free.day_page") }]);
    } else a.go = { act: "next", label: T(GAME.next ? "free.next" : "free.to_final") };
    answer(a);
    // Against the clock the time runs on through the reveal; at zero the game is over.
    if (s.mode === "timed" && GAME.next && GAME.ends_at) {
      const my = gen;
      clock(GAME.ends_at, s.seconds, async () => {
        let g;
        try { g = await req("/api/free/solo/" + GAME.id); } catch { return; }
        if (my !== gen) return;
        GAME = g;
        if (!GAME.next && GO.act === "next") { GO.label = T("free.to_final"); split(); root.querySelectorAll('.fr-sbtns [data-act="next"]').forEach((b) => { b.textContent = GO.label; }); }
      });
    }
  }
  function final() {
    gen++;
    const f = GAME.final || {}, s = GAME.settings, countries = s.mode === "streak" || s.mode === "timed";
    const score = s.mode === "streak" ? f.streak || 0 : s.mode === "timed" ? f.countries || 0 : f.points;
    top(T("free.final"), setLabel(s), () => home());
    let html = '<div class="fr-big">';
    if (s.mode === "streak") html += '<b>' + fmt(f.streak || 0) + '</b><span class="up">' + esc(T("free.streak_len")) + '</span>';
    else if (s.mode === "timed") html += '<b>' + fmt(f.countries || 0) + '</b><span class="up">' + esc(T("free.timed_len")) + '</span>';
    else html += '<b>' + fmt(f.points || 0) + '</b>';
    html += '</div>';
    if (f.record) html += '<p class="fr-rec">' + esc(T("free.record", { n: fmt(f.best) })) + '</p>';
    else if (f.best == null) html += keeps() && score ? '<p class="fr-best">' + esc(T("free.first_set")) + '</p>' : "";
    else if (f.best) html += '<p class="fr-best">' + esc(T(bestKey(s), { set: setLabel(s), n: fmt(f.best) })) + '</p>';
    html += '<ol class="fr-list">';
    // A streak and the clock's game count countries: a place is the country found or not, not its points.
    for (const d of GAME.done) {
      html += '<li><span class="n">' + fmt(d.n) + '</span><span class="w">' + esc(whereOf(d.place)) + '</span><span class="k">' +
        (d.guess.lat != null ? kmT(d.guess.km) : "") + '</span><b>' + (countries ? (d.guess.country ? "✓" : "✗") : fmt(d.guess.points)) + '</b></li>';
    }
    // A classic on a timer becomes a challenge: friends play its places in their own time. An attempt, and a game made
    // into one, lead to its standings.
    const dare = keeps() && GAME.status === "done" && (GAME.challenge ? '<button class="fr-pri" data-act="toChallenge" data-code="' + esc(GAME.challenge) + '">' +
      esc(T("free.to_challenge")) + '</button>' : s.mode === "classic" && s.seconds ? '<button class="fr-sec" data-act="dare">' + esc(T("free.dare")) + '</button>' : "");
    html += '</ol><div class="fr-go">' + (GAME.challenge && dare ? dare : '') + '<button class="' + (GAME.challenge && dare ? "fr-sec" : "fr-pri") + '" data-act="again">' + esc(T("free.again")) + '</button>' +
      (!GAME.challenge && dare ? dare : '') +
      (keeps() ? '<button class="fr-sec" data-act="friends">' + esc(T("free.friends")) + '</button>' : '') +
      '<button class="fr-link" data-act="settings">' + esc(T("free.settings")) + '</button></div>' + signIn();
    page(html, "fr-final");
    // The game over is no longer the one to continue; another left open still is.
    HOME = Object.assign({}, HOME || {});
    if (HOME.open === GAME.id) HOME.open = null;
    if (keeps() && score != null && HOME.best) HOME.best[key(s)] = Math.max(HOME.best[key(s)] || 0, score);
    TOP.clear();
    // "Again" plays the settings of the game just played, and Settings shows them.
    if (s.mode !== "archive") SET = Object.assign({}, s);
  }

  /* ---------- The archive: days closed in every zone, one place each, without a timer ---------- */
  let DAYS = null;
  // "#41" reads left to right in a right-to-left line.
  const no = (s) => "⁦" + s + "⁩";
  function dayDate(iso) {
    try { return new Intl.DateTimeFormat(numLang(), { timeZone: "UTC", day: "numeric", month: "long", year: iso.slice(0, 4) === String(new Date().getUTCFullYear()) ? undefined : "numeric" }).format(new Date(iso + "T00:00:00Z")); }
    catch { return iso; }
  }
  async function archive() {
    const my = ++gen; ROUND = null; clearTimeout(missT);
    top(T("free.archive"), "");
    // The list drawn before shows at once and is drawn again when the server answers.
    if (DAYS) drawArchive(); else wait();
    try { const d = (await req("/api/free/archive")).days; if (my !== gen) return; DAYS = d; }
    catch (e) { if (my === gen && !DAYS) failed(e, () => archive()); return; }
    drawArchive(true);
  }
  // Each day with how the world did: how many played and the typical miss, a target that gives the place away no more
  // than the days page does. A day played, on the day or from here, shows its points.
  const worldOf = (d) => d.players ? T("free.day_world", { n: fmt(d.players), km: kmT(d.median_km) }) : T("free.day_nobody");
  function drawArchive(keep) {
    let html = '<p class="fr-hint">' + esc(T("free.archive_hint")) + ' ' + esc(T("free.archive_goal")) + '</p>';
    if (!DAYS.length) html += '<p class="fr-wait">' + esc(T("free.archive_empty")) + '</p>';
    html += '<div class="fr-days">' + DAYS.map((d) => {
      const name = '<b>' + no("#" + fmt(d.n)) + '</b><span class="d">' + esc(dayDate(d.date)) + '</span><span class="s">' + esc(worldOf(d)) + '</span>';
      if (d.state === "open") return '<button class="fr-day" data-act="day" data-n="' + d.n + '">' + name + '<em>' + esc(T("free.play_day")) + ' ' + fwd() + '</em></button>';
      if (d.state === "played") return '<button class="fr-day done" data-act="played" data-game="' + d.game + '">' + name + '<em>' +
        (d.points != null ? '<i>' + fmt(d.points) + '</i>' : esc(T("free.day_going"))) + ' ' + fwd() + '</em></button>';
      if (d.state === "answered" && d.day_points != null) return '<div class="fr-day off mine">' + name + '<em><i>' + fmt(d.day_points) + '</i><small>' + esc(T("free.day_mine")) + '</small></em></div>';
      return '<div class="fr-day off">' + name + '<em>' + esc(T(d.state === "answered" ? "free.day_answered" : "free.day_seen")) + '</em></div>';
    }).join("") + '</div>';
    page(html, "fr-arch", keep);
  }

  /* ---------- A challenge: the places of one game, for friends to play in their own time ---------- */
  let CH = null;
  async function challenge(code) {
    const my = ++gen; ROUND = null; clearTimeout(missT);
    top(T("free.challenge"), "", () => home());
    // A guest sees what a challenge is and how to take one: signed in.
    if (!keeps()) {
      page('<p class="fr-hint">' + esc(T("free.challenge_hint")) + '</p>' + signIn("free.challenge_sign_in") +
        '<div class="fr-go"><button class="fr-link" data-act="home">' + esc(T("free.title")) + '</button></div>', "fr-ch-page");
      return;
    }
    if (!CH || CH.code !== code) wait();
    let c;
    try { c = await req("/api/free/challenge/" + code); } catch (e) { if (my === gen) failed(e, () => challenge(code)); return; }
    if (my !== gen) return;
    CH = c;
    top(T("free.challenge"), setLabel(CH.settings), () => home());
    const made = CH.lines.some((l) => l.me && l.maker);
    let html = '<p class="fr-chby" lang="' + nameLang() + '">' + esc(made ? T("free.challenge_mine") : T("free.challenge_by", { name: CH.by })) + '</p>' +
      '<p class="fr-hint">' + esc(T("free.challenge_hint")) + '</p><ol class="fr-list fr-table">';
    for (const l of CH.lines) {
      const how = l.status === "done" ? (l.seen ? '<em class="fr-under">' + esc(T("free.seen_n", { n: fmt(l.seen) })) + '</em>' : '')
        : '<em class="fr-under">' + esc(l.status === "playing" ? T("free.ch_playing", { n: fmt(l.played), total: fmt(CH.settings.rounds) }) : T("free.ch_left")) + '</em>';
      html += '<li' + (l.me ? ' class="me"' : '') + '><span class="n">' + (l.rank ? fmt(l.rank) : "") + '</span><span class="w"><span lang="' + nameLang() + '">' +
        esc(l.name) + '</span>' + how + '</span><b>' + (l.status === "done" ? fmt(l.points) : "") + '</b></li>';
    }
    html += '</ol><div class="fr-go">';
    // A line of theirs without a game to open is an attempt made before Telegram was linked: taken once all the same.
    const took = CH.mine != null || CH.lines.some((l) => l.me);
    if (!took) html += '<button class="fr-pri" data-act="take">' + esc(T("free.take")) + '</button>';
    else if (CH.mine != null && CH.status === "playing") html += '<button class="fr-pri" data-act="mine">' + esc(T("free.continue")) + '</button>';
    if (took) html += '<button class="' + (CH.mine != null && CH.status === "playing" ? "fr-sec" : "fr-pri") + '" data-act="invite">' + esc(T("free.invite_friends")) + '</button>';
    page(html + '<button class="fr-link" data-act="home">' + esc(T("free.title")) + '</button></div>', "fr-ch-page");
  }

  /* ---------- What party.js uses ---------- */
  const Kit = {
    req, ferr, now, msTo, top, page, wait, failed, open, close, settings, choose, setLabel, secLabel, key, keeps, signIn,
    round: newRound, pinned: (fn) => { onPin = fn; }, goTo, sent, lock, clock, stopClock, answer, table, whereOf, share, ACTS, ribbon,
    sending: (on) => { $("frGo").disabled = on || $("frGo").disabled; if (!on) split(); },
    home: () => home(),
    bot: () => HOME && HOME.bot,
    // party.js fills these in.
    party: null, lobby: null, stopped: () => {},
  };
  // Who is in the place, and who has answered: white is an answer, a dashed square a player the party no longer waits for.
  function ribbon(list) {
    const r = root && $("frRibbon");
    if (!r) return;
    if (!list) { r.innerHTML = ""; r.classList.remove("on"); return; }
    r.innerHTML = list.map((m) => '<i class="av' + (m.me ? " me" : "") + (m.done ? " done" : "") + (m.away ? " away" : "") + '" title="' + esc(m.name) + '">' + esc(initial(m.name)) + '</i>').join("");
    r.classList.add("on");
  }

  /* ---------- Into and out of the free game ---------- */
  // The free game is the page /play and a party /party/<code>; an invitation opens the app with "p" and the code as its
  // start_param, and the app moves to the party's address. Leaving goes to the day, the page under every other.
  const PARTY = /^\/party\/([0-9A-HJKMNP-TV-Z]{8})$/;
  const startParam = () => signed() && tg.initDataUnsafe && tg.initDataUnsafe.start_param || "";
  function invite() {
    const p = startParam();
    return p[0] === "p" && CODE.test(p.slice(1)) ? p.slice(1) : "";
  }
  // A challenge opens the free screen on it: from the invitation's "c" and code, or on the site from /play?c=<code>.
  // Each is taken once: Back from the challenge is the free screen, not the challenge again.
  let dared = false;
  function dareCode() {
    if (dared) return "";
    const p = startParam(), q = new URLSearchParams(location.search).get("c") || "";
    const c = p[0] === "c" ? p.slice(1) : q;
    return CODE.test(c) ? c : "";
  }
  // What the address asks of /play: a mode from the menu's cards, the archive, a game with friends. Read once and
  // taken off the address, so Back and a reload do not ask again; in Telegram the router would carry it on.
  function asked() {
    const q = new URLSearchParams(location.search), a = { mode: q.get("mode") || "", friends: q.get("with") === "friends" };
    if (!q.has("mode") && !q.has("with") && !q.has("c")) return a;
    ["mode", "with", "c"].forEach((k) => q.delete(k));
    history.replaceState(history.state, "", location.pathname + (q.toString() ? "?" + q : "") + location.hash);
    return a;
  }
  // Telegram has no history, so Back goes to the parent: the list of days when the game was opened from it.
  let parent = "/";
  const came = (from) => { if (from !== "play" && from !== "party") parent = from === "days" ? "/days" : "/"; };
  const leave = () => Router.back(parent);
  // The home fetched before the router opened the page, used once.
  let first = null;
  Router.screen("play", /^\/play$/, {
    open(m, from) {
      came(from); open();
      const h = first, c = dareCode(), a = asked(); first = null; dared = true;
      if (h) { HOME = h; if (!SET) SET = Object.assign({}, h.last); }
      if (c) return challenge(c);
      if (a.mode === "archive") return archive();
      if (a.friends) return withFriends(h);
      home(h, a.mode);
    },
    close(to) { if (to !== "party") close(); },
    nav: { href: "/play", t: "free.title", at: 3 },
  });
  // /play?with=friends: the party already open, or a new one on the last settings. A guest gets the way to sign in,
  // and to play alone.
  async function withFriends(h) {
    const my = ++gen;
    top(T("free.friends"), "");
    if (!h) {
      wait();
      try { h = HOME = await req("/api/free"); if (!SET) SET = Object.assign({}, h.last); } catch (e) { if (my === gen) failed(e, () => withFriends()); return; }
      if (my !== gen) return;
    }
    if (!keeps()) {
      page(signIn("free.friends_sign_in") + '<div class="fr-go"><button class="fr-link" data-act="home">' + esc(T("free.solo")) + '</button></div>', "fr-friends");
      return;
    }
    if (h.party) return Router.go("/party/" + h.party, { replace: true });
    Kit.party.create(partySet(SET));
  }
  Router.screen("party", PARTY, {
    open(m, from) { came(from); open(); Kit.party.open(m[1]); },
    close(to) { Kit.stopped(); if (to !== "play") close(); },
  });
  window.Free = {
    // Whether the app opens on the free game rather than the day: once, before the day has started.
    takes() { return !dayBoot && !opened && (!!invite() || !!dareCode() || /^\/play$/.test(location.pathname) || PARTY.test(location.pathname)); },
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
      else if (dareCode() && !/^\/play$/.test(location.pathname)) Router.go("/play", { replace: true });
      Router.start();
      if (typeof accountReady === "function") accountReady();
    },
    // The way in from the day's list of days.
    entry() { return '<a class="dd fr-entry" href="/play" data-route><b>' + esc(T("free.title")) + '</b><em>' + fwd() + '</em></a>'; },
  };
  window.FreeKit = Kit;
})();
