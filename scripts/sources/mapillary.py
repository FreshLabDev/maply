"""Mapillary coverage measurement over the same 68 points as Panoramax.
The key is read from the MAPILLARY_TOKEN variable (client token from mapillary.com/dashboard/developers).
For each point: one z14 vector tile (limit of 50,000 tiles per day); we count all frames,
panoramas, and panoramas with quality_score >= 0.7. Then for the contact sheet we pick one
random good panorama (or a regular frame) and download thumb_1024_url via the Entity API."""
import os, math, random, io, json, urllib.request, concurrent.futures as cf
import mapbox_vector_tile  # pip install mapbox-vector-tile
from PIL import Image, ImageDraw, ImageFont
from places import PLACES
TOKEN = os.environ["MAPILLARY_TOKEN"]
random.seed(7)
def tile(lat, lon, z=14):
    n = 2 ** z
    x = int((lon + 180) / 360 * n)
    y = int((1 - math.asinh(math.tan(math.radians(lat))) / math.pi) / 2 * n)
    return x, y
def one(p):
    name, lat, lon, kind = p
    x, y = tile(lat, lon)
    feats = []
    for dx in (-1, 0, 1):          # 3×3 tiles ≈ 7×7 km, comparable to the Panoramax window
        for dy in (-1, 0, 1):
            u = f"https://tiles.mapillary.com/maps/vtp/mly1_public/2/14/{x+dx}/{y+dy}?access_token={TOKEN}"
            try:
                raw = urllib.request.urlopen(u, timeout=40).read()
            except Exception:
                continue
            t = mapbox_vector_tile.decode(raw)
            feats += [f["properties"] for f in t.get("image", {}).get("features", [])]
    pano = [f for f in feats if f.get("is_pano")]
    good = [f for f in pano if (f.get("quality_score") or 0) >= 0.7]
    pool = good or pano or feats
    im = None
    if pool:
        iid = random.choice(pool)["id"]
        try:
            meta = json.load(urllib.request.urlopen(
                f"https://graph.mapillary.com/{iid}?fields=thumb_1024_url,is_pano&access_token={TOKEN}", timeout=40))
            im = Image.open(io.BytesIO(urllib.request.urlopen(meta["thumb_1024_url"], timeout=60).read())).convert("RGB")
            w, h = im.size
            if meta.get("is_pano") and abs(w / h - 2) < 0.05:
                im = im.crop((w // 4, h // 4, 3 * w // 4, 3 * h // 4))
            im.thumbnail((480, 360))
        except Exception:
            im = None
    return name, kind, len(feats), len(pano), len(good), im
with cf.ThreadPoolExecutor(6) as ex:
    res = list(ex.map(one, PLACES))
for r in res:
    print(f"{r[0]:22} {r[1]:5} frames={r[2]:6} pano={r[3]:5} pano q≥0.7={r[4]}")
print("\nwith frames:", sum(r[2] > 0 for r in res), "| with panoramas:", sum(r[3] > 0 for r in res),
      "| with good panoramas:", sum(r[4] > 0 for r in res), "of", len(res))
imgs = [(r[0], r[5]) for r in res if r[5]]
cols = 6; rows = (len(imgs) + cols - 1) // cols
sheet = Image.new("RGB", (cols * 480, rows * 380), "white"); d = ImageDraw.Draw(sheet)
font = ImageFont.truetype("/System/Library/Fonts/Supplemental/Arial Unicode.ttf", 20)
for i, (name, im) in enumerate(imgs):
    x, y = (i % cols) * 480, (i // cols) * 380
    sheet.paste(im, (x, y + 20)); d.text((x + 4, y), f"{i+1}. {name}", fill="black", font=font)
sheet.save("mapillary_sheet.jpg", quality=80)
