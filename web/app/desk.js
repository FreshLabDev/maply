/* ---------- The computer: layout, the map's panel, the side panel and the keys ---------- */
// A window from 1024 px wide and 600 px high gets the computer's layout: html.desk, set here before app.js runs, so
// the globe is made in the right place. desktop.css draws it; app.js asks Desk.on at the few forks where the camera
// or the table differ. A Telegram Desktop window is a phone until it is stretched or goes full screen.
//
//   Desk.on                     whether the computer's layout is on now
//   Desk.keyboard               whether a mouse and a keyboard are the input: hints name keys only then
//   Desk.change(fn)             fn(on) after the layout switched, the page already restyled
//   Desk.place()                the game's state changed: the plates move into the side panel or back
//   Desk.start(game)            once the words are in: the panel and the keys. game is app.js's:
//                               { view(), state(), open(), close(), pin(lngLat, point), globe, pano }
//
// The header's links (Play, the days, the leaderboards) and the full screen button are written when the router opens
// the first screen, whichever it is.
//
// In the game the map is a panel in the bottom right corner. It opens when the mouse rests on the globe, on a click
// or on M, and folds 600 ms after the mouse leaves, unless it is held open, a drag is going on or the keyboard is in
// it. After the answer the map takes the main area and the result goes into the side panel on the right.
//
// The keys work in every layout, as soon as there is a keyboard: Telegram Desktop's small window has one too.
const Desk = (() => {
  const Q = matchMedia("(min-width: 1024px) and (min-height: 600px)"), BIG = matchMedia("(min-width: 1440px)");
  const FINE = matchMedia("(hover: hover) and (pointer: fine)");
  const root = document.documentElement, fns = [];
  const $ = (id) => document.getElementById(id);
  let on = Q.matches, game = null;
  root.classList.toggle("desk", on);

  // The panel's size: small, medium (the default from 1440 px) or half the screen. The choice and the hold stay for
  // the next days.
  const SIZES = ["s", "m", "l"], SIZE = "maply.map.size", HOLD = "maply.map.hold";
  const read = (k) => { try { return localStorage.getItem(k) || ""; } catch { return ""; } };
  const write = (k, v) => { try { localStorage.setItem(k, v); } catch {} };
  let size = read(SIZE), hold = !!read(HOLD);
  const sizeNow = () => SIZES.includes(size) ? size : BIG.matches ? "m" : "s";
  root.dataset.map = sizeNow();
  BIG.addEventListener("change", () => { root.dataset.map = sizeNow(); ctl(); });
  Q.addEventListener("change", () => {
    on = Q.matches; root.classList.toggle("desk", on);
    place();
    fns.forEach((fn) => fn(on));
  });

  // The plates of the waiting and the result live over the map on a phone; on a computer they stand in the side
  // panel. A comment marks where each one came from.
  const homes = new Map();
  const MOVED = ["ovBot", "ovTop", "prog", "act"];
  function home(id) {
    const e = $(id);
    if (!homes.has(id)) { const c = document.createComment(id); e.parentNode.insertBefore(c, e); homes.set(id, c); }
    return e;
  }
  // A sheet, a page or the free game puts the game out of reach with inert on body's children, a plate among them.
  // Moved into the side panel, a plate goes under the panel's state; moved back among body's children, it takes the
  // panel's, which is theirs: otherwise a plate moved while the menu was over the game would stay inert after it.
  function put(id, into) {
    const e = home(id);
    if (into) { if (e.parentNode !== into) { into.appendChild(e); e.inert = false; } return; }
    const c = homes.get(id);
    if (c.nextSibling !== e) { c.parentNode.insertBefore(e, c.nextSibling); e.inert = c.parentNode === document.body && $("side").inert; }
  }
  // Moving a node takes the focus off it: it goes back where it was, or, after the answer from the map's panel, to
  // the result.
  const shown = (e) => e && e !== document.body && e.isConnected && e.getClientRects().length > 0 && !e.closest("[inert]");
  function place() {
    const st = document.body.dataset.state || "guess", side = on && st !== "guess";
    // The answer button is disabled while the answer goes out, which drops the focus on the body on the way.
    const a = document.activeElement, answered = side && document.body.classList.contains("is-panel") && (inPanel(a) || a === document.body);
    document.body.classList.toggle("is-side", side);
    document.body.classList.toggle("is-panel", on && st === "guess");
    MOVED.forEach((id) => put(id, side ? $("side") : null));
    // How the points add up is read in the open under the table, not behind the (i).
    put("rules", side ? $("side") : null);
    if (!side) $("score").classList.remove("on");
    if (answered) $("side").focus({ preventScroll: true });
    else if (a !== document.activeElement && shown(a)) a.focus({ preventScroll: true });
    if (game) { ctl(); cross(); }
  }

  /* The map's panel */
  let inside = false, held = false, down = false, openT = 0, closeT = 0;
  const panel = () => on && game && game.state() === "guess";
  const isOpen = () => game.view() === "map";
  // A sheet, a page of menu.js's or the free game over the day: the day's keys and the panel's hover wait.
  const sheetOn = () => document.body.matches(".is-sheet, .is-page, .is-free");
  const inPanel = (e) => !!e && ["map", "act", "mapCtl"].some((id) => $(id).contains(e));
  // The keyboard in the panel keeps it open; a click that left the focus on the map does not, whatever key comes
  // after it. So the focus remembers whether a press or a key brought it.
  let pressed = false, byPress = false;
  // A press in the map takes the keyboard's aim away even when the focus was already there.
  document.addEventListener("pointerdown", (e) => { pressed = true; if (game && inPanel(e.target)) { byPress = true; cross(); } }, true);
  addEventListener("keydown", () => { pressed = false; }, true);
  // On the way down, before the map's own focus listener asks for the cross.
  document.addEventListener("focus", () => { byPress = pressed; }, true);
  const keyIn = () => inPanel(document.activeElement) && !byPress;
  function open(how) {
    clearTimeout(closeT);
    if (!isOpen()) game.open();
    if (how !== "hover") held = true;
  }
  function fold() {
    clearTimeout(openT); clearTimeout(closeT); held = false;
    if (!isOpen()) return;
    // The folded panel can no longer hold the focus, its buttons and bar included: the globe that opens it takes it.
    const k = inPanel(document.activeElement);
    game.close();
    if (k) $("globeHit").focus({ preventScroll: true });
  }
  function later() {
    clearTimeout(closeT);
    closeT = setTimeout(() => {
      if (!panel() || !isOpen() || inside || hold || held || sheetOn()) return;
      if (down || keyIn() || game.globe.map.isMoving()) { later(); return; }
      fold();
    }, 600);
  }
  // Where the mouse is, against the open panel or the folded globe: the panel is one box with its bar and buttons.
  function track(e) {
    if (e.pointerType !== "mouse" || !panel()) return;
    const r = (isOpen() ? $("map") : $("globeHit")).getBoundingClientRect();
    const now = e.clientX >= r.left && e.clientX <= r.right && e.clientY >= r.top && e.clientY <= r.bottom;
    if (now === inside) return;
    inside = now;
    clearTimeout(openT);
    if (inside && !isOpen()) openT = setTimeout(() => { if (inside && panel() && !sheetOn()) open("hover"); }, 150);
    if (inside) clearTimeout(closeT);
    else if (isOpen()) { held = false; later(); }
  }
  function ctl() {
    const i = SIZES.indexOf(sizeNow());
    // aria-disabled, not disabled: a button that loses the focus would hand Enter to the answer.
    $("mapLess").setAttribute("aria-disabled", i <= 0); $("mapMore").setAttribute("aria-disabled", i >= SIZES.length - 1);
    $("mapHold").setAttribute("aria-pressed", hold);
  }
  function resize(by) {
    const i = Math.max(0, Math.min(SIZES.length - 1, SIZES.indexOf(sizeNow()) + by));
    size = SIZES[i]; write(SIZE, size); root.dataset.map = size; ctl();
  }

  // The keyboard's aim: Space drops the pin where it points, the middle of the open map.
  function cross() {
    const c = $("cross");
    if (!game) return;
    const canvas = game.globe.map.getCanvas();
    const show = game.state() === "guess" && isOpen() && document.activeElement === canvas && !byPress;
    c.classList.toggle("on", show);
    if (!show) return;
    const p = game.globe.center().point, m = $("map").getBoundingClientRect();
    c.style.left = m.left + p.x + "px"; c.style.top = m.top + p.y + "px";
  }

  /* Keys */
  const keysOn = () => !$("keys").classList.contains("hidden");
  let keysFrom = null;
  function keys(show) {
    const k = $("keys");
    if (show === keysOn()) return;
    k.classList.toggle("hidden", !show);
    if (show) { keysFrom = document.activeElement; k.focus({ preventScroll: true }); return; }
    if (k.contains(document.activeElement) && shown(keysFrom)) keysFrom.focus({ preventScroll: true });
    keysFrom = null;
  }
  // Held keys steer the panorama; a key released hands its axis to the other key of the pair, if still held.
  const STEER = { ArrowLeft: ["yaw", -1], KeyA: ["yaw", -1], ArrowRight: ["yaw", 1], KeyD: ["yaw", 1],
    ArrowUp: ["pitch", 1], KeyW: ["pitch", 1], ArrowDown: ["pitch", -1], KeyS: ["pitch", -1] };
  const ZOOM = { "+": 1, "=": 1, "-": -1, "_": -1, "−": -1 };
  const held_ = new Map();
  function steerNow(axis) {
    let dir = 0;
    held_.forEach(([a, d]) => { if (a === axis) dir = d; });
    game.pano.steer(axis, dir);
  }
  function release() { held_.clear(); ["yaw", "pitch", "zoom"].forEach((a) => game.pano.steer(a, 0)); }
  function onKey(e) {
    // macOS sends no keyup for a key let go while Cmd is down: the panorama would go on turning.
    if (game && e.metaKey) release();
    if (!game || e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey) return;
    if (sheetOn() || !$("loader").classList.contains("gone")) return;
    const t = e.target, typing = t.closest && t.closest("input, textarea, select, [contenteditable]");
    if (typing) return;
    const inMap = !!(t.closest && t.closest("#map")), onButton = !!(t.closest && t.closest("button, a"));
    const st = game.state(), k = e.key, code = e.code;
    // A held key repeats only where holding means something: turning and zooming.
    if (e.repeat && !STEER[code] && !ZOOM[k]) {
      if (code === "KeyM" || k === "?" || (code === "Space" && inMap) || (k === "Enter" && !onButton)) e.preventDefault();
      return;
    }
    // Space never answers, not even on the answer's own button: Enter does.
    if (code === "Space" && st === "guess" && t.closest && t.closest("#go")) { e.preventDefault(); return; }
    if (k === "?") { e.preventDefault(); keys(!keysOn()); return; }
    if (k === "Escape") {
      if (keysOn()) { e.preventDefault(); keys(false); return; }
      // On a computer the result's map is the page itself, not a window over it.
      if (isOpen() && (st === "guess" || !on)) { e.preventDefault(); fold(); }
      return;
    }
    if (code === "KeyM") {
      e.preventDefault();
      if (isOpen()) fold();
      else { open("key"); if (st === "guess") game.globe.map.getCanvas().focus(); }
      return;
    }
    if (k === "Enter") {
      if (st === "guess" && isOpen() && game.globe.guess && !onButton && !$("go").disabled) { e.preventDefault(); $("go").click(); }
      return;
    }
    // Only where the cross shows: after a click the pin is already where the mouse put it.
    if (code === "Space" && inMap && st === "guess" && isOpen() && $("cross").classList.contains("on")) {
      e.preventDefault();
      const c = game.globe.center(); game.pin(c.lngLat, c.point);
      return;
    }
    // The panorama turns only while it shows; on the focused map the arrows and +/− are MapLibre's.
    const seen = !isOpen() || panel();
    if (inMap || !seen) return;
    if (STEER[code]) { e.preventDefault(); held_.set(code, STEER[code]); steerNow(STEER[code][0]); return; }
    if (ZOOM[k]) { e.preventDefault(); held_.set(code || k, ["zoom", ZOOM[k]]); steerNow("zoom"); return; }
    if (k === "0") { e.preventDefault(); game.pano.resetZoom(); return; }
    if (code === "KeyN") { e.preventDefault(); game.pano.faceNorth(); }
  }
  function offKey(e) {
    if (!game) return;
    if (e.code === "Space" && game.state() === "guess" && e.target.closest && e.target.closest("#go")) e.preventDefault();
    const c = held_.has(e.code) ? e.code : held_.has(e.key) ? e.key : "";
    if (!c) return;
    const [axis] = held_.get(c); held_.delete(c); steerNow(axis);
  }

  /* The header */
  // Play is the menu, and the day and the free game are reached from it; then the days and the leaderboards, when
  // leaders.js is there. The profile and the sign-in are at the end, in #meBtn and #acct.
  const LINKS = [["/", "days.play", ["menu", "today", "play", "party"]], ["/days", "days", ["days"]], ["/top", "desk.top", ["top"]]];
  function nav() {
    const esc = (s) => String(s).replace(/[&<>"']/g, (c) => "&#" + c.charCodeAt(0) + ";");
    const top = !!(window.Leaders && typeof Leaders.show === "function");
    $("nav").innerHTML = LINKS.filter(([href]) => href !== "/top" || top)
      .map(([href, t, names]) => '<a href="' + href + '" data-route data-names="' + names.join(" ") + '">' + esc(T(t)) + '</a>').join("");
    mark(Router.current);
  }
  const mark = (name) => $("nav").querySelectorAll("a").forEach((a) => a.setAttribute("aria-current", a.dataset.names.split(" ").includes(name) ? "page" : "false"));
  // The header is written when the first screen opens, the words in by then, whether the page began with the day or
  // with another screen.
  let head = false;
  Router.on((name) => { if (!head) { head = true; nav(); fullscreen(); } mark(name); });
  // Telegram Desktop and the web clients can go full screen, where the computer's layout has room.
  function fullscreen() {
    const tg = window.Telegram && Telegram.WebApp;
    if (!tg || !["tdesktop", "macos", "weba", "webk"].includes(tg.platform) || !tg.isVersionAtLeast("8.0") || typeof tg.requestFullscreen !== "function") return;
    const b = $("fs");
    const sync = () => { b.classList.toggle("on", !!tg.isFullscreen); b.setAttribute("aria-label", T(tg.isFullscreen ? "desk.fullscreen_exit" : "desk.fullscreen")); };
    b.classList.remove("hidden");
    b.addEventListener("click", () => { if (tg.isFullscreen) tg.exitFullscreen(); else tg.requestFullscreen(); });
    tg.onEvent("fullscreenChanged", sync);
    sync();
  }
  function keysPanel() {
    const esc = (s) => String(s).replace(/[&<>"']/g, (c) => "&#" + c.charCodeAt(0) + ";");
    const row = (cap, text) => '<dt>' + esc(cap) + '</dt><dd>' + esc(text) + '</dd>';
    $("keys").innerHTML = '<b id="keysH">' + esc(T("desk.keys")) + '</b><dl>' +
      row("← →", T("desk.key_turn")) + row("↑ ↓", T("desk.key_tilt")) + row("+ −", T("desk.key_zoom")) + row("0", T("desk.key_reset")) +
      row("N", T("desk.key_north")) + row("M", T("desk.key_map")) + row(T("desk.space"), T("desk.key_pin")) +
      row("Enter", T("desk.key_answer")) + row("Esc", T("desk.key_close")) + '</dl><p>' + esc(T("desk.keys_hint")) + '</p>';
  }

  function start(g) {
    if (game) return;
    game = g;
    keysPanel(); place();
    $("keys").addEventListener("click", () => keys(false));
    addEventListener("keydown", onKey);
    addEventListener("keyup", offKey);
    addEventListener("blur", release);
    document.addEventListener("pointermove", track);
    document.documentElement.addEventListener("pointerleave", (e) => { if (inside) track({ pointerType: e.pointerType, clientX: -1, clientY: -1 }); });
    // A press inside the panel is a drag of the map or the pin until it is let go.
    document.addEventListener("pointerdown", (e) => {
      down = panel() && inPanel(e.target);
      // A finger has no hover to leave by: a touch beside the open panel folds it.
      if (panel() && isOpen() && !down && !hold && e.pointerType !== "mouse" && !$("globeHit").contains(e.target)) fold();
    });
    const up = () => { if (down) { down = false; if (panel() && isOpen() && !inside) later(); } };
    document.addEventListener("pointerup", up);
    document.addEventListener("pointercancel", up);
    // A click on the folded globe holds the panel open until the mouse has been in it and left; from the keyboard the
    // focus goes on to the map.
    $("globeHit").addEventListener("click", (e) => {
      if (panel()) held = true;
      if (e.detail === 0 && game.state() === "guess") requestAnimationFrame(() => game.globe.map.getCanvas().focus());
    });
    $("mapLess").addEventListener("click", () => resize(-1));
    $("mapMore").addEventListener("click", () => resize(1));
    $("mapHold").addEventListener("click", () => { hold = !hold; write(HOLD, hold ? "1" : ""); ctl(); if (!hold && !inside) later(); });
    $("sideDays").textContent = T("days") + " " + (root.dir === "rtl" ? "←" : "→");
    $("sideDays").addEventListener("click", () => Router.go("/days"));
    // A row and its mark on the map light up together.
    const lb = $("lb");
    lb.addEventListener("pointerover", (e) => {
      if (!on) return;
      const r = e.target.closest(".r");
      game.globe.light(r ? (r.classList.contains("me") ? "pin" : r.dataset.key) : null);
    });
    lb.addEventListener("pointerleave", () => { if (on) game.globe.light(null); });
    game.globe.on("hover", (key) => {
      if (!on) return;
      lb.querySelectorAll(".r.hl").forEach((r) => r.classList.remove("hl"));
      const r = key === "pin" ? lb.querySelector(".r.me") : key && [...lb.children].find((x) => x.dataset.key === key);
      if (r) { r.classList.add("hl"); r.scrollIntoView({ block: "nearest" }); }
    });
    const canvas = game.globe.map.getCanvas();
    ["focus", "blur"].forEach((t) => canvas.addEventListener(t, cross));
    // The camera is still on its way to the panel when the canvas takes focus: the cross follows it there.
    game.globe.map.on("move", () => { if ($("cross").classList.contains("on")) cross(); });
    game.globe.map.on("resize", cross);
    // Held open from an earlier day: the panel opens with the game.
    game.globe.ready(() => { if (hold && panel() && !game.globe.answered && !sheetOn()) open("key"); });
  }

  return {
    get on() { return on; },
    get keyboard() { return FINE.matches; },
    change(fn) { fns.push(fn); },
    place, start, cross,
  };
})();
