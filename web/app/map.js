/* ---------- Map: a paper globe from afar, a regular map up close ---------- */
// The palette and the style are in mapstyle.js, which the result card uses too.
const style = mapStyle(LANG0);
// The map is drawn before /api/today says the language; this relabels it once it does.
function setMapLanguage(l) {
  const apply = () => {
    ["streets", "oceans", "cities"].forEach((id) => map.setLayoutProperty(id, "text-field", nameIn(l)));
    map.setLayoutProperty("countries", "text-field", countryIn(l));
  };
  mapReady ? apply() : map.once("load", apply);
}
function createMap(zoom) {
  const m = new maplibregl.Map({ container: "map", style, center: [25, 30], zoom, minZoom: -1, maxPitch: 0, attributionControl: false, dragRotate: false, pitchWithRotate: false, fadeDuration: 150 });
  m.touchZoomRotate.disableRotation();
  m.on("zoom", paintBackdrop);
  m.on("zoomstart", onZoomStart);
  m.on("zoomend", onZoomEnd);
  return m;
}

/* Globe → map transition.
   1. While the globe unfolds, the canvas corners are filled with the water colour: otherwise bits of the photo flash there.
   2. The middle of the unfolding looks broken, so we do not linger there: release your fingers and
      the camera continues to the flat map (if you were zooming in) or back to the globe (if zooming out). */
const GLOBE_Z = [2.2, 3.6];
function paintBackdrop() {
  const t = Math.min(1, Math.max(0, (map.getZoom() - GLOBE_Z[0]) / (GLOBE_Z[1] - GLOBE_Z[0])));
  $("map").style.backgroundColor = view === "map" && t > 0 ? "rgba(36, 90, 60, " + t.toFixed(3) + ")" : "transparent";
}
let zoomFrom = null, snapT = 0;
function onZoomStart(e) { clearTimeout(snapT); if (e.originalEvent && zoomFrom === null) zoomFrom = map.getZoom(); }
function onZoomEnd(e) {
  if (!e.originalEvent || zoomFrom === null) return;
  clearTimeout(snapT);
  // The mouse wheel fires zoomend on every click: wait until the gesture really ends.
  snapT = setTimeout(() => {
    const zm = map.getZoom(), dir = zm - zoomFrom; zoomFrom = null;
    if (zm <= GLOBE_Z[0] || zm >= GLOBE_Z[1]) return;
    map.easeTo({ zoom: dir >= 0 ? GLOBE_Z[1] + 0.4 : GLOBE_Z[0] - 0.3, duration: 520, easing: easeOut });
  }, 160);
}
