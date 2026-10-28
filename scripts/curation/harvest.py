"""Collect candidate places of the day from Mapillary.

One frame is taken per region: a random z14 tile within the radius, a 360°
panorama with quality_score >= 0.7 captured in 2018 or later.
MAPLY_PANO_ONLY=0 allows a flat photo where no panorama is found.
Output: <out>/candidates.json, <out>/full/NNN.jpg (2048 px), <out>/view/NNN.jpg
(for panoramas, a ~120° forward view is cropped out) and contact sheets <out>/sheet-K.jpg of 30 frames each.

    MAPILLARY_TOKEN=... python harvest.py <out> [count]
    MAPILLARY_TOKEN=... MAPLY_POOL=plan.json python harvest.py <out>

With MAPLY_POOL, every region of a pool.py plan is tried once: a "city" in
the ring between inner and km, a "town" at its centre, "nature" on the ring
around a village.
"""
import io, json, math, os, random, sys, time, urllib.request, concurrent.futures as cf
import mapbox_vector_tile
from PIL import Image, ImageDraw, ImageFont
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from regions import REGIONS

TOKEN = os.environ["MAPILLARY_TOKEN"]
OUT = sys.argv[1]
COUNT = int(sys.argv[2]) if len(sys.argv) > 2 else 120
MIN_TS = 1514764800000  # 2018-01-01, milliseconds
# Only 360° panoramas, no flat photos (owner decision, 2026-10-28).
PANO_ONLY = os.environ.get("MAPLY_PANO_ONLY", "1") != "0"
# computed_compass_angle: where the frame centre points after Mapillary alignment; the panorama compass needs it.
FIELDS = "thumb_2048_url,geometry,creator,captured_at,is_pano,width,height,computed_compass_angle,compass_angle"

def get(url, timeout=60):
    for attempt in range(3):
        try:
            return urllib.request.urlopen(url, timeout=timeout).read()
        except Exception:
            if attempt == 2:
                raise
            time.sleep(2 * (attempt + 1))

def tile_of(lat, lon, z=14):
    n = 2 ** z
    return int((lon + 180) / 360 * n), int((1 - math.asinh(math.tan(math.radians(lat))) / math.pi) / 2 * n)

def random_point(lat, lon, km, inner=0):
    # Uniform over the ring between inner and km.
    r = math.sqrt(inner ** 2 + (km ** 2 - inner ** 2) * random.random()); a = random.random() * 2 * math.pi
    return lat + r * math.cos(a) / 111.0, lon + r * math.sin(a) / (111.0 * math.cos(math.radians(lat)))

def feature_lonlat(f, x, y, z=14, extent=4096):
    """Where a point feature of tile x/y sits; mapbox_vector_tile flips y."""
    fx, fy = f["geometry"]["coordinates"]
    n = 2 ** z
    lon = (x + fx / extent) / n * 360 - 180
    lat = math.degrees(math.atan(math.sinh(math.pi * (1 - 2 * (y + 1 - fy / extent) / n))))
    return lat, lon

def front_view(im):
    w, h = im.size
    return im.crop((w // 3, int(h * 0.28), 2 * w // 3, int(h * 0.62)))

def harvest(region):
    if isinstance(region, dict):
        name, lat, lon, km = region["name"], region["lat"], region["lon"], region["km"]
        inner, kind = region.get("inner", 0), region.get("kind", "city")
    else:
        (name, lat, lon, km), inner, kind = region, 0, "city"
    # Roads far from town are often empty: more tries.
    for _ in range(12 if kind == "nature" or PANO_ONLY else 5):
        tlat, tlon = random_point(lat, lon, km, inner)
        x, y = tile_of(tlat, tlon)
        try:
            t = mapbox_vector_tile.decode(get(f"https://tiles.mapillary.com/maps/vtp/mly1_public/2/14/{x}/{y}?access_token={TOKEN}"))
        except Exception:
            continue
        raw = [f for f in t.get("image", {}).get("features", [])
               if (f["properties"].get("captured_at") or 0) >= MIN_TS]
        if kind == "town":
            # The frames nearest the town's centre, not the bypass road.
            def far(f):
                flat, flon = feature_lonlat(f, x, y)
                return (flat - lat) ** 2 + ((flon - lon) * math.cos(math.radians(lat))) ** 2
            raw = sorted(raw, key=far)[:40]
        feats = [f["properties"] for f in raw]
        panos = [f for f in feats if f.get("is_pano") and (f.get("quality_score") or 0) >= 0.7]
        flats = [f for f in feats if not f.get("is_pano") and (f.get("quality_score") or 0) >= 0.75]
        pool = panos if PANO_ONLY else (panos or flats)
        if not pool:
            continue
        for f in random.sample(pool, min(3, len(pool))):
            try:
                meta = json.loads(get(f"https://graph.mapillary.com/{f['id']}?fields={FIELDS}&access_token={TOKEN}"))
                im = Image.open(io.BytesIO(get(meta["thumb_2048_url"]))).convert("RGB")
            except Exception:
                continue
            w, h = im.size
            pano = bool(meta.get("is_pano"))
            if pano and abs(w / h - 2) > 0.05:
                continue
            lo, la = meta["geometry"]["coordinates"]
            return {"region": name, "kind": kind, "country": region.get("country") if isinstance(region, dict) else None,
                    "source": "mapillary", "source_id": str(f["id"]), "lat": la, "lon": lo,
                    "is_pano": pano, "captured_at": meta.get("captured_at"),
                    "heading": meta.get("computed_compass_angle", meta.get("compass_angle")),
                    "author": (meta.get("creator") or {}).get("username"), "license": "CC-BY-SA-4.0"}, im
    return None

def main():
    random.seed()
    pool = REGIONS
    if os.environ.get("MAPLY_POOL"):  # a plan from pool.py: every entry is harvested once
        pool = json.load(open(os.environ["MAPLY_POOL"]))
    only = os.environ.get("MAPLY_REGIONS")  # comma-separated names, to harvest a batch for a subset of regions
    if only:
        names = {x.strip() for x in only.split(",")}
        pool = [r for r in REGIONS if r[0] in names]
    regions = random.sample(pool, len(pool) if os.environ.get("MAPLY_POOL") else min(COUNT, len(pool)))
    os.makedirs(f"{OUT}/full", exist_ok=True); os.makedirs(f"{OUT}/view", exist_ok=True)
    with cf.ThreadPoolExecutor(6) as ex:
        found = [r for r in ex.map(harvest, regions) if r]
    cands = []
    for i, (meta, im) in enumerate(found, 1):
        num = f"{i:03d}"
        im.save(f"{OUT}/full/{num}.jpg", quality=88)
        view = front_view(im) if meta["is_pano"] else im.copy()
        view.thumbnail((1024, 1024)); view.save(f"{OUT}/view/{num}.jpg", quality=85)
        cands.append({"n": i, **meta})
    json.dump(cands, open(f"{OUT}/candidates.json", "w"), ensure_ascii=False, indent=1)
    font = ImageFont.truetype("/System/Library/Fonts/Supplemental/Arial Bold.ttf", 44)
    TW, TH, COLS = 576, 340, 5
    for k in range(0, len(cands), 30):
        part = cands[k:k + 30]
        rows = (len(part) + COLS - 1) // COLS
        sheet = Image.new("RGB", (COLS * TW, rows * TH), "black"); d = ImageDraw.Draw(sheet)
        for j, c in enumerate(part):
            v = Image.open(f"{OUT}/view/{c['n']:03d}.jpg"); v.thumbnail((TW - 6, TH - 6))
            x, y = (j % COLS) * TW + 3, (j // COLS) * TH + 3
            sheet.paste(v, (x, y))
            label = str(c["n"]); bx = d.textbbox((0, 0), label, font=font)
            d.rectangle((x, y, x + bx[2] + 16, y + bx[3] + 12), fill="black")
            d.text((x + 8, y + 2), label, fill="white", font=font)
        sheet.save(f"{OUT}/sheet-{k // 30 + 1}.jpg", quality=82)
    print(f"candidates: {len(cands)} of {len(regions)} regions, sheets: {(len(cands) + 29) // 30}")

if __name__ == "__main__":
    main()
