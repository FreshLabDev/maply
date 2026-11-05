/* ---------- Map: a paper globe from afar, a regular map up close ---------- */
// The palette and the style are in mapstyle.js, which the result card uses too.
const style = mapStyle(LANG0);
// The map is drawn before /api/today says the language; this relabels it once it does.
function setMapLanguage(l) {
  const apply = () => {
    ["streets", "oceans", "cities"].forEach((id) => map.setLayoutProperty(id, "text-field", nameIn(l)));
    map.setLayoutProperty("countries", "text-field", countryIn(l));
  };
  whenMapReady(apply);
}
function createMap(zoom) {
  const m = new maplibregl.Map({ container: "map", style, center: [25, 30], zoom, minZoom: -1, maxPitch: 0, attributionControl: false, dragRotate: false, pitchWithRotate: false, fadeDuration: 150 });
  m.touchZoomRotate.disableRotation();
  noZoomInertia(m);
  zoomPace(m);
  m.on("zoom", () => { paintBackdrop(); zoomPace(m); });
  return m;
}

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

/* Globe → map transition: while the globe unfolds, the canvas corners are filled with the water colour, otherwise bits
   of the photo flash there. */
const GLOBE_Z = [2.2, 3.6];
function paintBackdrop() {
  const t = Math.min(1, Math.max(0, (map.getZoom() - GLOBE_Z[0]) / (GLOBE_Z[1] - GLOBE_Z[0])));
  $("map").style.backgroundColor = view === "map" && t > 0 ? "rgba(36, 90, 60, " + t.toFixed(3) + ")" : "transparent";
}
