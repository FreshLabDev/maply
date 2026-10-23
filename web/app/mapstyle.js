/* ---------- Map style: a paper globe from afar, a regular map up close ---------- */
// Shared by the Mini App (map.js) and the result card (card/card.js).
// Palette: land is light "paper", water is the maply field colour, no background around the globe.
// Flat fills without shadows or glow, as in the design system; only the graticule gives the globe volume.
const C = { water: "#245A3C", land: "#DDE5E0", park: "#C9D7CE", build: "#C3D0C8",
            road: "#FFFFFF", major: "#59826B", line: "#59826B", deep: "#1A412B", muted: "#B5C7BD" };
// Labels in the reader's language where OpenStreetMap has one. Arabic and Hebrew are shaped by the RTL plugin,
// which MapLibre fetches only when a label in those scripts first appears; local names show them in every language.
// Its ICU is WebAssembly, hence 'wasm-unsafe-eval' in the CSP.
maplibregl.setRTLTextPlugin(new URL("vendor/mapbox-gl-rtl-text-0.3.0/mapbox-gl-rtl-text.js", document.currentScript.src).href, true)
  .catch(() => {});
// The true point on every map: a ring with a dark centre.
const TARGET = '<svg class="target" viewBox="0 0 30 30"><path fill="none" stroke="#FFFFFF" stroke-width="6" d="M15 3.5a11.5 11.5 0 1 1 0 23a11.5 11.5 0 1 1 0-23"/><circle cx="15" cy="15" r="7.5" fill="#FFFFFF"/><circle cx="15" cy="15" r="5" fill="#1A412B"/><path fill="none" stroke="#1A412B" stroke-width="3" d="M15 3.5a11.5 11.5 0 1 1 0 23a11.5 11.5 0 1 1 0-23"/></svg>';
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
const z = (...stops) => ["interpolate", ["linear"], ["zoom"], ...stops];
// Parallels and meridians every 15°: on the globe this is the main beauty; they dissolve by zoom 4.5.
const grid = { type: "FeatureCollection", features: [] };
for (let lon = -180; lon < 180; lon += 15) grid.features.push({ type: "Feature", properties: { eq: lon === 0 }, geometry: { type: "LineString", coordinates: Array.from({ length: 37 }, (_, i) => [lon, -90 + i * 5]) } });
for (let lat = -75; lat <= 75; lat += 15) grid.features.push({ type: "Feature", properties: { eq: lat === 0 }, geometry: { type: "LineString", coordinates: Array.from({ length: 73 }, (_, i) => [-180 + i * 5, lat]) } });
// The style in the reader's language: the Mini App and the result card draw the same map.
function mapStyle(lang) {
  const name = nameIn(lang), country = countryIn(lang);
  return {
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
}
