/* ---------- Map style: a paper globe from afar, a regular map up close ---------- */
// Shared by the Mini App (globe.js) and the result card (card/card.js).
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
// The lines OpenStreetMap draws for Russia's claim on Crimea (and Ukraine's answer on the Arabat Spit) are
// disputed country borders in the tiles. maply scores Crimea as Ukraine, so it draws them as a region's border.
// Below zoom 5 the tiles take Natural Earth's line instead, which names no claimant: it is the one disputed
// line lying wholly around Crimea.
const crimeaBox = { type: "Polygon", coordinates: [[[33.2, 44.2], [37, 44.2], [37, 46.5], [33.2, 46.5], [33.2, 44.2]]] };
const crimea = ["all", ["==", ["get", "admin_level"], 2], ["==", ["get", "disputed"], 1],
                ["any", ["in", ["coalesce", ["get", "claimed_by"], ""], ["literal", ["RU", "UA"]]], ["within", crimeaBox]]];
// Cities by zoom: capitals and the biggest from 3, the large ones from 4, every city and town from 5.
const cities = ["all", ["in", ["get", "class"], ["literal", ["city", "town"]]],
                ["step", ["zoom"], ["any", ["==", ["get", "capital"], 2], ["<=", ["get", "rank"], 1]],
                 4, ["any", ["==", ["get", "capital"], 2], ["<=", ["get", "rank"], 3]], 5, true]];
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
      // Big rivers from afar (the tiles hold only those until zoom 6), every one up close.
      { id: "rivers", type: "line", source: "omt", "source-layer": "waterway", minzoom: 3.5,
        paint: { "line-color": C.water, "line-width": z(3.5, 0.4, 6, 0.7, 14, 2.5), "line-opacity": z(3.5, 0, 4, 1) } },
      { id: "buildings", type: "fill", source: "omt", "source-layer": "building", minzoom: 14, paint: { "fill-color": C.build } },
      { id: "roads", type: "line", source: "omt", "source-layer": "transportation", minzoom: 8,
        filter: ["all", ["in", ["get", "class"], ["literal", ["secondary", "tertiary", "minor", "service"]]], ["==", ["geometry-type"], "LineString"]],
        layout: { "line-cap": "square" }, paint: { "line-color": C.road, "line-width": z(8, 0.5, 16, 7) } },
      { id: "highways", type: "line", source: "omt", "source-layer": "transportation", minzoom: 5,
        filter: ["all", ["in", ["get", "class"], ["literal", ["motorway", "trunk", "primary"]]], ["==", ["geometry-type"], "LineString"]],
        layout: { "line-cap": "square" },
        // From afar, highways are green; up close they become ordinary, wider white streets.
        paint: { "line-color": ["interpolate", ["linear"], ["zoom"], 11, C.major, 13, C.road], "line-width": z(5, 0.5, 16, 10) } },
      // States, provinces and oblasts: a light dotted line under the countries' dashes, from the flat map on.
      // The lines of Russia's claim on Crimea count among them: maply draws Crimea as Ukraine, as its scoring does.
      { id: "states", type: "line", source: "omt", "source-layer": "boundary", minzoom: 2.8,
        filter: ["all", ["!=", ["get", "maritime"], 1], ["any", ["==", ["get", "admin_level"], 4], crimea]],
        layout: { "line-cap": "round" },
        paint: { "line-color": C.line, "line-width": z(3, 0.6, 8, 1.2), "line-dasharray": [0.1, 2.2], "line-opacity": z(2.8, 0, 3.4, 0.8) } },
      { id: "borders", type: "line", source: "omt", "source-layer": "boundary",
        filter: ["all", ["==", ["get", "admin_level"], 2], ["!=", ["get", "maritime"], 1], ["!", crimea]],
        paint: { "line-color": C.deep, "line-width": z(1, 0.6, 8, 1.4), "line-dasharray": [3, 2] } },
      { id: "streets", type: "symbol", source: "omt", "source-layer": "transportation_name", minzoom: 14,
        layout: { "symbol-placement": "line", "text-field": name, "text-font": ["Noto Sans Regular"], "text-size": 11 },
        paint: { "text-color": C.major, "text-halo-color": C.road, "text-halo-width": 1.5 } },
      { id: "oceans", type: "symbol", source: "omt", "source-layer": "water_name", minzoom: 0.8, maxzoom: 6, filter: ["in", ["get", "class"], ["literal", ["ocean", "sea"]]],
        layout: { "text-field": name, "text-font": ["Noto Sans Italic"], "text-size": z(1, 10, 5, 13), "text-letter-spacing": 0.25, "text-max-width": 6 },
        paint: { "text-color": C.muted } },
      // A region's name from afar, the larger ones first. Russia's "Republic of Crimea" is left out: the Autonomous
      // Republic of Crimea, Ukraine's, names the same land.
      { id: "regions", type: "symbol", source: "omt", "source-layer": "place", minzoom: 3, maxzoom: 8,
        filter: ["all", ["==", ["get", "class"], "state"], ["!=", ["coalesce", ["get", "name:en"], ["get", "name_en"], ""], "Republic of Crimea"],
                 ["<=", ["get", "rank"], ["step", ["zoom"], 2, 4.5, 4, 5.5, 99]]],
        layout: { "text-field": name, "text-font": ["Noto Sans Italic"], "text-size": z(3.5, 9.5, 7, 12), "text-transform": "uppercase",
                  "text-letter-spacing": 0.1, "text-max-width": 8, "text-padding": 4 },
        paint: { "text-color": C.line, "text-halo-color": C.land, "text-halo-width": 1.2 } },
      // Capitals and the biggest cities from the flat map on, with a dot to aim at; every city and town from zoom 5.
      { id: "city-dots", type: "circle", source: "omt", "source-layer": "place", minzoom: 3, maxzoom: 9, filter: cities,
        paint: { "circle-radius": z(3, 2, 8, 3), "circle-color": C.deep, "circle-stroke-color": C.land, "circle-stroke-width": 1 } },
      { id: "cities", type: "symbol", source: "omt", "source-layer": "place", minzoom: 3, filter: cities,
        layout: { "text-field": name, "text-font": ["Noto Sans Regular"], "text-size": z(3, 10, 5, 11, 12, 16),
                  "text-variable-anchor": ["left", "right", "top", "bottom"], "text-radial-offset": 0.5, "text-justify": "auto" },
        paint: { "text-color": C.deep, "text-halo-color": C.land, "text-halo-width": 1.5 } },
      // From afar only large countries are labelled, and the mini globe in the corner has no labels at all.
      { id: "countries", type: "symbol", source: "omt", "source-layer": "place", minzoom: 0.8, maxzoom: 7,
        filter: ["all", ["==", ["get", "class"], "country"], ["<=", ["get", "rank"], ["step", ["zoom"], 1, 2, 2, 3, 3, 4, 5]]],
        layout: { "text-field": country, "text-font": ["Noto Sans Bold"], "text-size": z(1, 9, 5, 14), "text-transform": "uppercase", "text-letter-spacing": 0.14, "text-max-width": 7, "text-padding": 6 },
        paint: { "text-color": C.water, "text-halo-color": C.land, "text-halo-width": 1.2 } }
    ]
  };
}
