/* ---------- Globe: MapLibre, a paper globe from afar and a map up close ---------- */
// The globe of a game, shared by the day's game and the free game: the small globe that turns while there is no pin,
// the big globe or the map where the pin goes, and the answer: the place, the line from your pin and the others' marks.
// A screen makes one; its layout says where the small globe stands and what its plates cover. The palette and the
// style are in mapstyle.js, which the result card uses too.
//
//   const globe = createGlobe({
//     el: $("map"),                      // the box: the canvas fills it and does not change size for a camera move
//     lang: "en",                        // the labels' language; globe.language(l) relabels
//     mini: () => ({ x, y, d }),         // the small globe's centre in the box and its diameter, in px
//     insets: () => ({ top, bottom, left, right }), // what the screen's plates cover of the box with the map open
//     spin: () => true,                  // optional: whether the small globe may turn now
//     backdrop: true,                    // optional: fill the box with water while the globe unfolds into a map
//   });
//   globe.ready(fn)                      fn once the style is in: pins, marks and layers wait for it
//   globe.mode                           "mini" or "big"
//   globe.toBig(ms)                      the globe grows into the free part of the box, on your pin if there is one
//   globe.toMini(ms)                     back to the small globe, turned to your pin; 0 jumps there
//   globe.refit()                        the layout changed: the camera follows once no finger is down
//   globe.resize()                       the box changed size: the canvas follows at once, then the camera
//   globe.center()                       { lngLat, point } at the middle of the free part of the box
//   globe.pin(lngLat, { draggable, drop }) your pin, placed or moved (drop plays its fall again); null takes it away
//   globe.guess                          your pin's LngLat, or null
//   globe.lock()                         your pin can no longer be dragged
//   globe.water(point)                   whether a point in the box is water
//   globe.answer({ truth, km, others })  the place, the line from your pin and the others' marks; km is your distance
//   globe.others(list, fresh)            the others' marks again, after a poll. An item is { key, at: [lng, lat], d,
//                                        label, face }: d in km, label a text, face the HTML of an avatar; fresh is a
//                                        Set of the keys that pop in
//   globe.world(pins)                    everyone's pins, small and grey: [{ lat, lon }]
//   globe.frame(ms)                      the place, your pin and the answers near them, in the free part of the box
//   globe.keepInView()                   frame again if the plates have grown over the place or your pin
//   globe.light(key)                     light a mark from outside ("pin" is yours), null for none
//   globe.clear()                        the answer goes: the place, the line, the marks; your pin goes too
//   globe.on(name, fn)                   "pick" ({ lngLat, point }): a click on the globe's surface
//                                        "beside": a click in the box off the globe
//                                        "pinmoved" (lngLat): your pin was dragged
//                                        "hover" (key or null): the mark under the cursor, "pin" for yours
//   globe.language(lang)                 relabel the map
//   globe.map                            the MapLibre map, for anything else
//   globe.destroy()
// The pin and the longitude helper are the page's: the days' map and the free game draw the same pin.
// On the map, markers are dark with a white outline: visible on both light land and green water.
// MapLibre places a marker by its element's transform, so the drop plays on the svg inside, not on the element itself.
const PIN_PATH = "M320 501 L214 395 A150 150 0 1 1 426 395 Z M320 229 A60 60 0 1 0 320 349 A60 60 0 1 0 320 229 Z";
const PIN = '<div class="pinm"><svg class="pin" viewBox="150 119 340 402"><path fill="#1A412B" stroke="#FFFFFF" stroke-width="24" stroke-linejoin="miter" paint-order="stroke" fill-rule="evenodd" d="' + PIN_PATH + '"/></svg></div>';
// The longitude of lng nearest to from, so a line or a frame crosses the 180th meridian the short way.
const lngNear = (lng, from) => from + ((((lng - from) % 360) + 540) % 360) - 180;
const createGlobe = (() => {
  const el = (html) => { const d = document.createElement("div"); d.innerHTML = html; return d.firstElementChild; };
  const easeOut = (t) => 1 - (1 - t) ** 3;
  // Globe radius on screen: the equator is 512·2^zoom px long, which gives the zoom for a target diameter.
  const zoomFor = (d) => Math.log2(d * Math.PI / 512);
  const globeR = (zm) => 512 * 2 ** zm / (2 * Math.PI);
  // MapLibre's globe grows as 1/cos of the centre's latitude at the same zoom, so the zoom takes that back. The mini
  // globe's centre stays within ±40°: a pin further north or south still shows, nearer the edge, and the zoom stays
  // above the map's minimum.
  const MINI_LAT = 40, miniLat = (lat) => Math.max(-MINI_LAT, Math.min(MINI_LAT, lat || 0));
  const atLat = (lat) => Math.log2(Math.cos(lat * Math.PI / 180));
  // Globe → map transition: while the globe unfolds, the canvas corners are filled with the water colour, otherwise bits
  // of the photo flash there.
  const GLOBE_Z = [2.2, 3.6];
  // A longitude moved by whole turns to within half a turn of from's: two points either side of the 180th meridian frame
  // and join the short way round, not across the rest of the world.
  const near180 = (at, from) => [lngNear(at[0], from), at[1]];
  // The angle between two points on the globe, in degrees.
  function arc(a, b) {
    const r = Math.PI / 180, la1 = a.lat * r, la2 = b[1] * r, dl = (b[0] - a.lng) * r;
    return Math.acos(Math.max(-1, Math.min(1, Math.sin(la1) * Math.sin(la2) + Math.cos(la1) * Math.cos(la2) * Math.cos(dl)))) / r;
  }
  // Result markers: a friend's point that falls under the target moves out onto a ring around it; nearby points are
  // pushed apart; a label takes the first free spot, and if it ends up far away, a leader line is drawn to the point.
  const RING = 27, GAP = 23, CLUSTER = 30;
  // A mark round the back of the globe would show through the land: past this angle from the centre, when MapLibre's
  // own test is gone.
  const HORIZON = 80;
  const stillMotion = matchMedia("(prefers-reduced-motion: reduce)");

  /* Zoom goes exactly as far as the hand does. No snapping after a gesture and no zoom inertia after a pinch: both kept
     the map moving after the fingers stopped, and people fought it. Instead the pace depends on how far out the camera
     is: from the globe a wheel click or a trackpad swipe covers more zoom than up close, where you aim. A pinch follows
     the fingers exactly, as it should. */
  function zoomPace(m) {
    const z = m.getZoom(), far = Math.min(1, Math.max(0, (9 - z) / 8)); // 1 on the globe, 0 from zoom 9 in
    m.scrollZoom.setWheelZoomRate((1 + 1.4 * far) / 450); // MapLibre's defaults are 1/450 for a wheel
    m.scrollZoom.setZoomRate((1 + 1.4 * far) / 100); // and 1/100 for a trackpad
  }
  // MapLibre has no option for it: a pinch's zoom is recorded for inertia and continued after release. The recorder is
  // wrapped so a frame that zooms leaves no zoom (and no pan, which a pinch also makes) to continue. A one-finger drag
  // keeps its pan inertia.
  function noZoomInertia(m) {
    const inertia = m.handlers && m.handlers._inertia;
    if (!inertia || typeof inertia.record !== "function") return;
    const record = inertia.record.bind(inertia);
    inertia.record = (s) => record(s && s.zoomDelta ? { ...s, zoomDelta: 0, panDelta: undefined } : s);
  }

  return function createGlobe(o) {
    const box = o.el, handlers = {};
    const emit = (name, ...a) => (handlers[name] || []).forEach((fn) => fn(...a));
    const size = () => ({ W: box.clientWidth, H: box.clientHeight });
    const insets = () => Object.assign({ top: 0, bottom: 0, left: 0, right: 0 }, o.insets ? o.insets() : {});
    let mode = "mini", ready = false, guess = null, marker = null, marks = null, alive = true;
    const waiters = [];

    function miniCam(lat) {
      // The small globe's centre is wherever the screen puts it: the focus point moves by the padding; the canvas
      // stays as it is.
      const { W, H } = size(), m = o.mini(), at = miniLat(lat !== undefined ? lat : map ? map.getCenter().lat : 30);
      // MapLibre rejects negative padding and fails entirely; this happens while the canvas is still zero-sized.
      return { padding: { left: Math.max(0, 2 * m.x - W), top: Math.max(0, 2 * m.y - H), right: 0, bottom: 0 }, zoom: zoomFor(m.d) + atLat(at) };
    }
    function bigCam() {
      const { W, H } = size(), i = insets(), d = o.mini().d + 4;
      return { padding: { left: i.left, top: i.top, right: i.right, bottom: i.bottom },
        zoom: zoomFor(Math.max(d, 0.86 * Math.min(W - i.left - i.right, H - i.top - i.bottom))) + atLat(miniLat(map.getCenter().lat)) };
    }
    function onGlobe(pt) {
      const T = map.transform;
      if (typeof T.isPointOnMapSurface === "function") return T.isPointOnMapSurface(pt);
      const c = map.project(map.getCenter()); // fallback in case MapLibre's internal method disappears
      return map.getZoom() >= GLOBE_Z[1] || Math.hypot(pt.x - c.x, pt.y - c.y) <= globeR(map.getZoom()) * 0.88;
    }
    function behind(at) {
      const T = map.transform;
      if (typeof T.isLocationOccluded === "function") { try { return T.isLocationOccluded(maplibregl.LngLat.convert(at)); } catch {} }
      return arc(map.getCenter(), at) >= HORIZON;
    }
    function paintBackdrop() {
      if (o.backdrop === false) return;
      const t = Math.min(1, Math.max(0, (map.getZoom() - GLOBE_Z[0]) / (GLOBE_Z[1] - GLOBE_Z[0])));
      box.style.backgroundColor = mode === "big" && t > 0 ? "rgba(36, 90, 60, " + t.toFixed(3) + ")" : "transparent";
    }
    function setMode(m) {
      mode = m;
      box.classList.toggle("small", m === "mini");
      paintBackdrop();
    }

    box.classList.add("globe", "small");
    const map = new maplibregl.Map({ container: box, style: mapStyle(o.lang || "en"), center: [25, 30], zoom: zoomFor(o.mini().d) + atLat(30),
      minZoom: -1, maxPitch: 0, attributionControl: false, dragRotate: false, pitchWithRotate: false, fadeDuration: 150 });
    map.touchZoomRotate.disableRotation();
    noZoomInertia(map);
    zoomPace(map);
    map.on("zoom", () => { paintBackdrop(); zoomPace(map); });
    map.setPadding(miniCam().padding);

    // Markers, the line and the layers of the answer need the style, not the tiles. "load" waits for the tiles'
    // description too, and when that one request fails MapLibre never fires it: a returning player would get no pin.
    function onReady() {
      if (ready) return;
      ready = true; map.jumpTo(miniCam());
      waiters.splice(0).forEach((fn) => fn());
    }
    map.on("style.load", onReady);
    map.on("load", onReady);
    const whenReady = (fn) => { ready ? fn() : waiters.push(fn); };
    // MapLibre does not ask for the tiles' description again: without it the globe has no water or coasts all session.
    let omtTries = 0;
    map.on("error", (e) => {
      const src = e && e.sourceId === "omt" && map.getSource("omt");
      if (!src || src.loaded() || omtTries >= 6) return;
      const url = src.url;
      setTimeout(() => { const s = map.getSource("omt"); if (s && !s.loaded()) s.setUrl(url); }, 1500 * 2 ** omtTries++);
    });

    // Not easeTo: if zooming out starts from a flat map (zoom > 3.6), MapLibre clamps the final zoom right at the start
    // to "the world is no narrower than the screen", log2(H/512), and the globe gets stuck large. A frame-by-frame jumpTo
    // does not know this: by the time zoom drops below 3.6 the projection is already a globe, and there is no constraint.
    let tween = 0;
    // The box can take another place in the page between two camera moves (the computer's panel becomes the main
    // area after the answer): the canvas has to match it before a camera is worked out for it.
    function fitCanvas() {
      const c = map.getCanvas();
      if (c.clientWidth !== box.clientWidth || c.clientHeight !== box.clientHeight) map.resize();
    }
    function toMini(ms) {
      cancelAnimationFrame(tween); map.stop(); fitCanvas();
      setMode("mini"); opening = false;
      const z0 = map.getZoom(), c0 = map.getCenter(), p0 = map.getPadding(), t0 = performance.now();
      const c1 = { lng: (guess || c0).lng, lat: miniLat((guess || c0).lat) }, dl = ((c1.lng - c0.lng + 540) % 360) - 180;
      if (!ms) { map.jumpTo({ ...miniCam(c1.lat), center: [c1.lng, c1.lat] }); return; }
      const mix = (a, b, e) => a + (b - a) * e, pad = (a, b, e) => Math.max(0, mix(a, b, e));
      const step = (now) => {
        if (mode !== "mini") return;
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
    function toBig(ms) {
      cancelAnimationFrame(tween); fitCanvas();
      const was = mode;
      setMode("big");
      const c = bigCam();
      // Already big (the answer just went out): only the padding and the centre move, the zoom stays where it is.
      const zoom = was === "big" ? map.getZoom() : guess ? Math.max(map.getZoom(), 2.2) : c.zoom;
      opening = true; openZoom = zoom;
      map.easeTo({ ...c, zoom, center: guess || map.getCenter(), duration: ms === undefined ? 750 : ms, easing: easeOut });
    }
    // A press on the map stops MapLibre's camera, the opening's padding and zoom with it: a click while the panel was
    // opening left the globe small in the panel's corner. Once the camera stands still the big view takes its padding
    // back, and its zoom unless the player has come closer.
    let opening = false, openZoom = 0;
    function settleOpen() {
      if (!opening || mode !== "big" || touches || map.isMoving()) return;
      const p = map.getPadding(), q = bigCam().padding, zoom = Math.max(map.getZoom(), openZoom);
      if (["top", "right", "bottom", "left"].every((k) => Math.abs(p[k] - q[k]) < 1) && zoom - map.getZoom() < .01) { opening = false; return; }
      map.easeTo({ padding: q, zoom, duration: 300, easing: easeOut });
    }

    // A camera call during a pinch resets the gesture, so in the big view the new padding waits until the map stands
    // still and no finger is down.
    let touches = 0, refitDue = false, viewDue = false, userMoved = false;
    box.addEventListener("touchstart", (e) => { touches = e.touches.length; }, { passive: true });
    ["touchend", "touchcancel"].forEach((t) => box.addEventListener(t, (e) => {
      touches = e.touches.length;
      if (!touches) { refitNow(); settleOpen(); if (viewDue) keepInView(); }
    }, { passive: true }));
    function refitNow() {
      if (!refitDue || !ready) return;
      if (mode !== "big") { refitDue = false; toMini(200); return; }
      if (touches || map.isMoving()) return;
      refitDue = false; map.jumpTo({ padding: bigCam().padding });
    }
    function refit() { refitDue = true; refitNow(); }
    // MapLibre 5.6 skips the first call of its own ResizeObserver: a window resized right after loading left the
    // canvas at its old size and the globe off its place.
    const onResize = () => { fitCanvas(); refit(); };
    addEventListener("resize", onResize);
    map.on("moveend", refitNow);
    map.on("moveend", settleOpen);
    // The box can change size with the window still: a screen moving the map into another part of the page.
    let boxSize = "";
    new ResizeObserver(() => {
      const s = box.clientWidth + "x" + box.clientHeight;
      if (s !== boxSize) { const first = !boxSize; boxSize = s; if (!first) refit(); }
    }).observe(box);

    // While there is no pin, the mini globe rotates slowly: it shows it is alive and can be touched. It turns by the time
    // passed, about twelve times a second: every turn redraws a full-screen canvas, and at the display's rate it heated
    // the phone and spun faster on 120 Hz. Not at all for someone who asked the system for less motion.
    let spunAt = 0;
    (function spinGlobe(now) {
      if (!alive) return;
      const due = now - spunAt;
      if (due > 80) {
        if (ready && mode === "mini" && !guess && !map.isMoving() && !stillMotion.matches && (!o.spin || o.spin())) {
          const c = map.getCenter(); map.setCenter([c.lng + Math.min(due, 250) * 0.0072, c.lat]);
        }
        spunAt = now;
      }
      requestAnimationFrame(spinGlobe);
    })(0);

    map.on("click", (e) => {
      // A click beside the globe, in the void around it, is the screen's to handle. We check the surface itself via
      // MapLibre: for a point off the globe it still returns a coordinate on its edge, and previously a pin was placed there.
      if (!onGlobe(e.point)) { emit("beside"); return; }
      emit("pick", { lngLat: e.lngLat, point: e.point });
    });

    function pin(lngLat, opt) {
      opt = opt || {};
      if (!lngLat) { if (marker) marker.remove(); marker = null; guess = null; return; }
      guess = maplibregl.LngLat.convert(lngLat);
      if (!marker) {
        marker = new maplibregl.Marker({ element: el(PIN), anchor: "bottom", draggable: !!opt.draggable }).setLngLat(guess).addTo(map);
        marker.on("dragend", () => { guess = marker.getLngLat(); emit("pinmoved", guess); });
        const e = marker.getElement();
        e.addEventListener("mouseenter", () => emit("hover", "pin"));
        e.addEventListener("mouseleave", () => emit("hover", null));
        if (marks) e.style.zIndex = 3;
      } else {
        marker.setLngLat(guess);
        if (opt.draggable !== undefined) marker.setDraggable(!!opt.draggable);
        if (opt.drop) { const p = marker.getElement().firstChild; p.style.animation = "none"; void p.offsetWidth; p.style.animation = ""; }
      }
    }

    function layoutMarks() {
      // In the small view the markers are hidden: nothing to lay out until the map opens.
      if (!marks || mode !== "big") return;
      const T = map.project(marks.truth), G = guess ? map.project(guess) : { x: -1e4, y: -1e4 }, W = box.clientWidth;
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

    // Everyone's pins worldwide, small and grey under the markers: where the world thinks it is, without anyone's name.
    function world(pins) {
      const data = { type: "FeatureCollection", features: (pins || []).map((p) => ({ type: "Feature", properties: {}, geometry: { type: "Point", coordinates: [p.lon, p.lat] } })) };
      const src = map.getSource("world");
      if (src) { src.setData(data); return; }
      map.addSource("world", { type: "geojson", data });
      map.addLayer({ id: "world", type: "circle", source: "world", paint: { "circle-radius": 3, "circle-color": "#B5C7BD", "circle-stroke-color": "#1A412B", "circle-stroke-width": 1 } });
    }
    // The others' marks. A mark lives as long as its answer: a new answer adds one, and a changed name or face (the
    // names opening at the reveal) is drawn into the same mark. Nothing under a finger is ever replaced, so a gesture
    // that starts on a mark keeps going.
    const friendAt = new Map();
    let lit = null;
    function others(list, fresh) {
      const keep = new Set(list.map((f) => f.key));
      friendAt.forEach((it, k) => { if (!keep.has(k)) { it.m.remove(); friendAt.delete(k); } });
      marks.friends = list.map((f) => {
        let it = friendAt.get(f.key);
        if (!it) {
          it = newFriend(f, fresh && fresh.has(f.key));
          friendAt.set(f.key, it);
        }
        it.d = f.d;
        // A changed face or name is drawn by groupMarks on the next layout, as a group or alone.
        if (it.name !== f.label || it.face0 !== f.face) { it.name = f.label; it.face0 = f.face; it.n = NaN; }
        return it;
      });
      layoutMarks();
      if (lit) light(lit);
    }
    function newFriend(f, fresh) {
      const e = el('<div class="friend' + (fresh ? " new" : "") + '"><span class="g"><s></s><i class="av"></i><b></b></span></div>');
      const m = new maplibregl.Marker({ element: e }).setLngLat(f.at).addTo(map);
      e.style.zIndex = 1;
      // The pop plays once: a marker that hides in a group and comes back does not pop again.
      e.addEventListener("animationend", () => e.classList.remove("new"), { once: true });
      const it = { key: f.key, m, at: f.at, e, g: e.firstChild, s: e.querySelector("s"), b: e.querySelector("b"), face: e.querySelector(".av"), n: NaN, group: null };
      it.g.addEventListener("mouseenter", () => emit("hover", f.key));
      it.g.addEventListener("mouseleave", () => emit("hover", null));
      // A tap on a group zooms in until it comes apart.
      it.g.addEventListener("click", (ev) => {
        if (!it.group || !it.group.length) return;
        ev.stopPropagation(); if (o.haptic) o.haptic("light");
        const b = new maplibregl.LngLatBounds(it.at, it.at);
        it.group.forEach((x) => b.extend(near180(x.at, it.at[0])));
        const cam = map.cameraForBounds(b, { padding: 70, maxZoom: Math.min(map.getZoom() + 4, 14) });
        // The player chose this view: a growing table does not take it back to the opening frame.
        userMoved = true;
        if (cam) map.easeTo({ center: cam.center, zoom: Math.max(cam.zoom, map.getZoom() + 1), duration: 650, easing: easeOut });
      });
      return it;
    }
    // Several answers within a finger's width of each other become one marker: the best of them, with how many more
    // stand behind it. Which ones group changes with the zoom.
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
    // A mark lit from outside: a hovered row. A mark inside a group lights its group's.
    function light(key) {
      lit = key;
      friendAt.forEach((it) => it.e.classList.remove("lit"));
      if (marker) marker.getElement().classList.toggle("lit", key === "pin");
      const it = key && friendAt.get(key);
      if (!it) return;
      const lead = it.group === null ? [...friendAt.values()].find((x) => x.group && x.group.includes(it)) : it;
      (lead || it).e.classList.add("lit");
    }
    // MapLibre works out how flat the globe is while drawing, after the camera's last move: a jump from the whole globe
    // to a city left the HTML markers where the globe would have put them, the pin a dozen pixels off its line. The
    // first frame after the camera stops puts them back.
    function settleMarks() {
      [marker, marks && marks.targetMarker, ...[...friendAt.values()].map((f) => f.m)].forEach((m) => { if (m) m.setLngLat(m.getLngLat()); });
      layoutMarks();
    }
    map.on("moveend", () => { map.off("render", settleMarks); map.once("render", settleMarks); });

    // The answer: the place's target, the others' marks, the line from your pin. Layer order: the world's pins, the
    // others, the target above them, your own pin on top.
    function answer(a) {
      const truth = maplibregl.LngLat.convert(a.truth);
      marks = { truth, km: a.km, friends: [], target: el(TARGET) };
      others(a.others || [], null);
      marks.targetMarker = new maplibregl.Marker({ element: marks.target }).setLngLat(truth).addTo(map);
      marks.targetMarker.getElement().style.zIndex = 2;
      if (marker) marker.getElement().style.zIndex = 3;
      layoutMarks();
      if (guess) {
        const line = { type: "Feature", geometry: { type: "LineString", coordinates: [near180([guess.lng, guess.lat], truth.lng), [truth.lng, truth.lat]] } };
        map.addSource("line", { type: "geojson", data: line });
        map.addLayer({ id: "line-case", type: "line", source: "line", paint: { "line-color": "#FFFFFF", "line-width": 5 } });
        map.addLayer({ id: "line", type: "line", source: "line", paint: { "line-color": "#1A412B", "line-width": 2, "line-dasharray": [2, 1.5] } });
      }
    }
    function clear() {
      if (marks && marks.targetMarker) marks.targetMarker.remove();
      friendAt.forEach((it) => it.m.remove()); friendAt.clear();
      ["line", "line-case", "world"].forEach((id) => { if (map.getLayer(id)) map.removeLayer(id); });
      ["line", "world"].forEach((id) => { if (map.getSource(id)) map.removeSource(id); });
      marks = null; lit = null; userMoved = false;
      pin(null);
    }

    // The opening frame: the place, your pin and the answers near them. An answer from the other side of the world would
    // zoom out to the whole globe and squeeze the two that matter into one dot, or turn them to its back.
    function frame(duration) {
      setMode("big"); opening = false;
      cancelAnimationFrame(tween); fitCanvas();
      const { W, H } = size(), i = insets(), c = marks.truth.lng, pts = [[c, marks.truth.lat]];
      if (guess) pts.push(near180([guess.lng, guess.lat], c));
      const ds = marks.friends.map((f) => f.d).sort((x, y) => x - y);
      const near = Math.min(3000, Math.max(800, marks.km != null ? marks.km * 1.5 : ds[Math.min(2, ds.length - 1)] || 0));
      marks.friends.forEach((f) => { if (f.d <= near) pts.push(near180(f.at, c)); });
      const bounds = new maplibregl.LngLatBounds(pts[0], pts[0]);
      pts.forEach((p) => bounds.extend(p));
      // Your pin stands above its point. On a short screen the margins shrink together, so the map keeps some room: with
      // margins larger than the screen MapLibre does not move at all. A map as wide as a computer's keeps its points
      // further from the edges than a phone's.
      const wide = W - i.left - i.right >= 700, air = wide ? 56 : 24;
      let top = i.top + air + (guess ? 46 : 0), bottom = i.bottom + air;
      const side = Math.min(wide ? 96 : 40, (W - i.left - i.right) / 8), room = H - 120;
      if (top + bottom > room) { const k = room / (top + bottom); top *= k; bottom *= k; }
      map.setPadding({ left: 0, top: 0, right: 0, bottom: 0 });
      userMoved = false;
      map.fitBounds(bounds, { padding: { top, left: i.left + side, right: i.right + side, bottom }, maxZoom: 9, duration });
    }
    // When the plates grow over the place or your pin, the camera moves to show them again. Not under a finger or in
    // flight: then it waits until the map stands still. Once the player has moved the map, the view is theirs: a point
    // they took off the screen is not "under the table".
    map.on("movestart", (e) => { if (e.originalEvent) userMoved = true; });
    function keepInView() {
      if (mode !== "big" || !marks || userMoved) return;
      if (touches || map.isMoving()) { viewDue = true; return; }
      viewDue = false;
      const i = insets(), { W, H } = size();
      const under = (ll, above) => {
        const p = map.project(ll);
        return p.x >= 0 && p.x <= W && ((p.y >= 0 && p.y - above < i.top) || (p.y > H - i.bottom && p.y <= H));
      };
      if (under(marks.truth, 15) || (guess && under(guess, 46))) frame(600);
    }
    map.on("moveend", () => { if (viewDue) keepInView(); });

    return {
      ready: whenReady, toBig, toMini, refit, pin, answer, others, world, frame, keepInView, light, clear,
      resize: onResize,
      center() {
        const p = map.getPadding(), { W, H } = size(), x = (W + p.left - p.right) / 2, y = (H + p.top - p.bottom) / 2;
        return { lngLat: map.unproject([x, y]), point: new maplibregl.Point(x, y) };
      },
      lock() { if (marker) marker.setDraggable(false); },
      water: (point) => map.queryRenderedFeatures(point, { layers: ["water"] }).length > 0,
      language(l) { whenReady(() => { ["streets", "oceans", "regions", "cities"].forEach((id) => map.setLayoutProperty(id, "text-field", nameIn(l))); map.setLayoutProperty("countries", "text-field", countryIn(l)); }); },
      on(name, fn) { (handlers[name] = handlers[name] || []).push(fn); },
      get mode() { return mode; },
      get guess() { return guess; },
      get answered() { return !!marks; },
      map,
      destroy() { alive = false; cancelAnimationFrame(tween); removeEventListener("resize", onResize); map.remove(); },
    };
  };
})();
