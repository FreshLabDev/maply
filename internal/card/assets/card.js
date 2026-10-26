/* The result card: the map in the Mini App's style with the true point, every pin and a dashed line from each pin to
   the point. The numbers and names above and below are plain HTML from the server. When the map is drawn the page
   says so on <html data-ready>: "yes", or "failed" if the tiles did not arrive. The renderer waits for it. */
(() => {
  const root = document.documentElement;
  const data = JSON.parse(document.getElementById("card-data").textContent);
  // data-took is how long the card took, for whoever is tuning the renderer.
  const finish = (state) => { if (!root.dataset.ready) { root.dataset.took = String(Math.round(performance.now())); root.dataset.ready = state; } };
  // A card with a blank map is worse than the photo, so a stuck page fails before the renderer gives up.
  setTimeout(() => finish("failed"), 14000);
  try {
    const truth = [data.truth.lon, data.truth.lat];
    const bounds = new maplibregl.LngLatBounds(truth, truth);
    data.pins.forEach((p) => bounds.extend([p.lon, p.lat]));
    let tiles = true;
    // A flat map: the renderer draws in software, where the globe the Mini App shows at world scale takes longer than the
    // card may.
    const style = mapStyle(data.lang);
    style.projection = { type: "mercator" };
    const map = new maplibregl.Map({
      container: "map", style, bounds, fitBoundsOptions: { padding: 100, maxZoom: data.pins.length ? 9 : 4 },
      interactive: false, attributionControl: false, fadeDuration: 0, minZoom: -1, maxPitch: 0,
    });
    map.on("error", (e) => { if (e && e.sourceId === "omt") tiles = false; });
    map.on("load", () => {
      root.dataset.loadAt = String(Math.round(performance.now()));
      if (data.pins.length) {
        map.addSource("lines", { type: "geojson", data: { type: "Feature", geometry: { type: "MultiLineString", coordinates: data.pins.map((p) => [[p.lon, p.lat], truth]) } } });
        map.addLayer({ id: "line-case", type: "line", source: "lines", paint: { "line-color": "#FFFFFF", "line-width": 9 } });
        map.addLayer({ id: "line", type: "line", source: "lines", paint: { "line-color": "#1A412B", "line-width": 4, "line-dasharray": [2, 1.5] } });
      }
      // Best score on top, the point above all of them.
      data.pins.forEach((p, i) => {
        const e = document.createElement("i");
        e.className = "av" + (i < 3 ? " w" : "");
        if (p.photo) {
          const img = document.createElement("img");
          img.src = p.photo;
          img.alt = "";
          e.appendChild(img);
        } else e.textContent = p.initial;
        e.style.zIndex = String(100 - i);
        new maplibregl.Marker({ element: e }).setLngLat([p.lon, p.lat]).addTo(map);
      });
      const t = document.createElement("div");
      t.innerHTML = TARGET;
      t.style.zIndex = "200";
      new maplibregl.Marker({ element: t }).setLngLat(truth).addTo(map);
      map.once("idle", async () => {
        try { await document.fonts.ready; } catch {}
        // Two frames: markers settle and labels are placed.
        await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
        finish(tiles && map.loaded() ? "yes" : "failed");
      });
    });
  } catch (e) {
    console.error(e);
    finish("failed");
  }
})();
