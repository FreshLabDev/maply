/* ---------- Map: a paper globe from afar, a regular map up close ---------- */
// Palette: land is light "paper", water is the maply field colour, no background around the globe.
// Flat fills without shadows or glow, as in the design system; only the graticule gives the globe volume.
const C = { water: "#245A3C", land: "#DDE5E0", park: "#C9D7CE", build: "#C3D0C8",
            road: "#FFFFFF", major: "#59826B", line: "#59826B", deep: "#1A412B", muted: "#B5C7BD" };
// Labels in the reader's language where OpenStreetMap has one. Arabic needs MapLibre's RTL plugin to be shaped,
// and the service does not carry it yet, so an Arabic reader gets the English names on the map.
const mapLang = (lang) => (lang === "ar" ? "en" : lang);
const nameIn = (lang) => ["coalesce", ["get", "name:" + lang], ["get", "name"]];
// Country labels are the browser's short name for the ISO code: the same source as the country on the result.
function countryIn(lang) {
  let dn;
  try { dn = new Intl.DisplayNames([lang], { type: "region", style: "short", fallback: "none" }); } catch { return nameIn(lang); }
  const pairs = [];
  for (let a = 65; a <= 90; a++) for (let b = 65; b <= 90; b++) {
    const iso = String.fromCharCode(a, b);
    let n; try { n = dn.of(iso); } catch { continue; }
    if (n && n !== iso) pairs.push(iso, n);
  }
  return pairs.length ? ["match", ["get", "iso_a2"], ...pairs, nameIn(lang)] : nameIn(lang);
}
const name = nameIn(mapLang(LANG0)), country = countryIn(mapLang(LANG0));
const z = (...stops) => ["interpolate", ["linear"], ["zoom"], ...stops];
// Parallels and meridians every 15°: on the globe this is the main beauty; they dissolve by zoom 4.5.
const grid = { type: "FeatureCollection", features: [] };
for (let lon = -180; lon < 180; lon += 15) grid.features.push({ type: "Feature", properties: { eq: lon === 0 }, geometry: { type: "LineString", coordinates: Array.from({ length: 37 }, (_, i) => [lon, -90 + i * 5]) } });
for (let lat = -75; lat <= 75; lat += 15) grid.features.push({ type: "Feature", properties: { eq: lat === 0 }, geometry: { type: "LineString", coordinates: Array.from({ length: 73 }, (_, i) => [-180 + i * 5, lat]) } });
const style = {
  version: 8, glyphs: "https://tiles.openfreemap.org/fonts/{fontstack}/{range}.pbf",
  // Globe up to zoom 2.2, unfolding smoothly into a regular flat map by zoom 3.6 (see GLOBE_Z below).
  // The day's result (usually 3.5–9) thus almost never lands in the middle of the unfolding, where the map looks cropped.
  projection: { type: ["interpolate", ["linear"], ["zoom"], 2.2, "vertical-perspective", 3.6, "mercator"] },
  sky: { "atmosphere-blend": 0 },
  sources: { omt: { type: "vector", url: "https://tiles.openfreemap.org/planet" }, grid: { type: "geojson", data: grid } },
  layers: [
    { id: "land", type: "background", paint: { "background-color": C.land } },
    { id: "park", type: "fill", source: "omt", "source-layer": "landcover", minzoom: 6, filter: ["in", ["get", "class"], ["literal", ["wood", "grass", "farmland"]]], paint: { "fill-color": C.park } },
    { id: "water", type: "fill", source: "omt", "source-layer": "water", paint: { "fill-color": C.water } },
    { id: "grid", type: "line", source: "grid", maxzoom: 3.6,
      paint: { "line-color": C.line, "line-width": ["case", ["get", "eq"], 1.4, 0.7], "line-opacity": z(2.2, 1, 3.4, 0) } },
    { id: "rivers", type: "line", source: "omt", "source-layer": "waterway", minzoom: 6, paint: { "line-color": C.water, "line-width": z(6, 0.6, 14, 2.5) } },
    { id: "buildings", type: "fill", source: "omt", "source-layer": "building", minzoom: 14, paint: { "fill-color": C.build } },
    { id: "roads", type: "line", source: "omt", "source-layer": "transportation", minzoom: 8,
      filter: ["all", ["in", ["get", "class"], ["literal", ["secondary", "tertiary", "minor", "service"]]], ["==", ["geometry-type"], "LineString"]],
      layout: { "line-cap": "square" }, paint: { "line-color": C.road, "line-width": z(8, 0.5, 16, 7) } },
    { id: "highways", type: "line", source: "omt", "source-layer": "transportation", minzoom: 5,
      filter: ["all", ["in", ["get", "class"], ["literal", ["motorway", "trunk", "primary"]]], ["==", ["geometry-type"], "LineString"]],
      layout: { "line-cap": "square" },
      // From afar, highways are green; up close they become ordinary, wider white streets.
      paint: { "line-color": ["interpolate", ["linear"], ["zoom"], 11, C.major, 13, C.road], "line-width": z(5, 0.5, 16, 10) } },
    { id: "borders", type: "line", source: "omt", "source-layer": "boundary", filter: ["all", ["==", ["get", "admin_level"], 2], ["!=", ["get", "maritime"], 1]],
      paint: { "line-color": C.deep, "line-width": z(1, 0.6, 8, 1.4), "line-dasharray": [3, 2] } },
    { id: "streets", type: "symbol", source: "omt", "source-layer": "transportation_name", minzoom: 14,
      layout: { "symbol-placement": "line", "text-field": name, "text-font": ["Noto Sans Regular"], "text-size": 11 },
      paint: { "text-color": C.major, "text-halo-color": C.road, "text-halo-width": 1.5 } },
    { id: "oceans", type: "symbol", source: "omt", "source-layer": "water_name", minzoom: 0.8, maxzoom: 6, filter: ["in", ["get", "class"], ["literal", ["ocean", "sea"]]],
      layout: { "text-field": name, "text-font": ["Noto Sans Italic"], "text-size": z(1, 10, 5, 13), "text-letter-spacing": 0.25, "text-max-width": 6 },
      paint: { "text-color": C.muted } },
    { id: "cities", type: "symbol", source: "omt", "source-layer": "place", minzoom: 5, filter: ["in", ["get", "class"], ["literal", ["city", "town"]]],
      layout: { "text-field": name, "text-font": ["Noto Sans Regular"], "text-size": z(5, 11, 12, 16) },
      paint: { "text-color": C.deep, "text-halo-color": C.land, "text-halo-width": 1.5 } },
    // From afar only large countries are labelled, and the mini globe in the corner has no labels at all.
    { id: "countries", type: "symbol", source: "omt", "source-layer": "place", minzoom: 0.8, maxzoom: 7,
      filter: ["all", ["==", ["get", "class"], "country"], ["<=", ["get", "rank"], ["step", ["zoom"], 1, 2, 2, 3, 3, 4, 5]]],
      layout: { "text-field": country, "text-font": ["Noto Sans Bold"], "text-size": z(1, 9, 5, 14), "text-transform": "uppercase", "text-letter-spacing": 0.14, "text-max-width": 7, "text-padding": 6 },
      paint: { "text-color": C.water, "text-halo-color": C.land, "text-halo-width": 1.2 } }
  ]
};
// The map is drawn before /api/today says the language; this relabels it once it does.
function setMapLanguage(lang) {
  const l = mapLang(lang);
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
