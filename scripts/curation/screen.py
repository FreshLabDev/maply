"""Screen a harvested batch before anyone looks at it, and draw the sheets
the reviewers look at.

A candidate is dropped when its frame
- lies in a country maply leaves out, or in the part of one pool.py leaves
  out (the front line, occupied and disputed land, troubled borders);
- is near a military area OSM knows (landuse=military or a military=* object
  within MILITARY_M, or inside one), asked of Overpass once and cached;
- is the same frame as, or within MAPLY_AVOID_KM of, a place of the other
  pool (MAPLY_AVOID, see avoid.py);
- is within MAPLY_SPACING_KM of a candidate of the batch with a lower number,
  so two frames of one town do not both go in.

The rest get panorama sheets: every candidate is the whole 360° band from 40°
above the horizon down to -45°, where maply-places paints the nadir over, so
a face or a fence behind the camera is in front of the reviewer too. The
frame's centre, where the game opens, is marked.

    python -I screen.py <batch>

Output: <batch>/screen.json ({"drop": {n: reason}, "sheets": {name: [n]},
"frames": {n: source_id}}), <batch>/pano-K.jpg, and the Overpass answer for
every point in <batch>/overpass/. An answer and a verdict belong to the frame
that was screened: if harvest.py writes a batch again, a number may name
another frame, so the cache keeps the frame's source ID and is asked again
when it differs, and choose.py refuses reviews of a batch that changed.
"""
import json, os, sys, time, urllib.parse, urllib.request
from PIL import Image, ImageDraw, ImageFont
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import avoid
os.environ.setdefault("MAPILLARY_TOKEN", "")  # pool.py reads it at import; nothing here calls Mapillary
import pool

MILITARY_M = 300
# OSM military=* values that are not a live site: an old bunker on a beach,
# a First World War trench.
PAST = {"bunker", "trench", "barrier"}
OVERPASS = "https://overpass-api.de/api/interpreter"
SPACING_KM = float(os.environ.get("MAPLY_SPACING_KM", "5"))
STRIP_W, PER_SHEET = 1568, 4


def overpass(points, cache):
    """Ask Overpass about many points in one query, one answer file a point.

    A query costs about half a minute on the public server however small it
    is, so the points go 40 at a time: each one's statements end with a
    marker element that carries its number, then its matches."""
    def cached(n, sid):
        try:
            return json.load(open(f"{cache}/{n:03d}.json")).get("source_id") == sid
        except FileNotFoundError:
            return False

    todo = [(n, sid, lat, lon) for n, sid, lat, lon in points if not cached(n, sid)]
    for k in range(0, len(todo), 40):
        part = todo[k:k + 40]
        q = ["[out:json][timeout:300];"]
        for n, _, lat, lon in part:
            q.append(f"""is_in({lat},{lon})->.a;
(area.a["landuse"="military"]; area.a["military"];)->.inside;
(nwr(around:{MILITARY_M},{lat},{lon})["landuse"="military"];
 nwr(around:{MILITARY_M},{lat},{lon})["military"];)->.near;
make point n={n}; out;
(.inside; .near;); out tags;""")
        body = urllib.parse.urlencode({"data": "\n".join(q)}).encode()
        for attempt in range(6):
            try:
                req = urllib.request.Request(OVERPASS, data=body, headers={"User-Agent": pool.UA})
                data = json.loads(urllib.request.urlopen(req, timeout=400).read())
                if data.get("remark") and "error" in data["remark"].lower():
                    raise RuntimeError("overpass remark")
                break
            except Exception as e:
                err = e
                time.sleep(20 * (attempt + 1))
        else:
            raise RuntimeError(f"overpass: {type(err).__name__}")
        per, cur = {}, None
        for el in data.get("elements", []):
            if el.get("type") == "point":
                cur = int(el["tags"]["n"])
                per[cur] = []
            elif cur is not None:
                per[cur].append(el)
        for n, sid, _, _ in part:
            if n not in per:
                raise RuntimeError(f"overpass answer has no marker for {n}")
            json.dump({"source_id": sid, "elements": per[n]}, open(f"{cache}/{n:03d}.json", "w"))
        print(f"overpass: {k + len(part)} of {len(todo)}", file=sys.stderr)
        time.sleep(2)  # one query at a time, with a pause: Overpass is shared
    return {n: json.load(open(f"{cache}/{n:03d}.json")) for n, _, _, _ in points}


def military(data):
    """The live military objects in an Overpass answer, as short labels."""
    found = []
    for el in data.get("elements", []):
        t = el.get("tags", {})
        if any(k in t for k in ("historic", "disused", "abandoned", "ruins")) or t.get("disused:military"):
            continue
        if t.get("landuse") == "military":
            found.append(f"landuse=military {t.get('name', '')}".strip())
        elif t.get("military") and t["military"] not in PAST:
            found.append(f"military={t['military']} {t.get('name', '')}".strip())
    return found


def strip(im):
    """The band from +40° to -45° of an equirectangular frame, centre marked."""
    w, h = im.size
    band = im.crop((0, int(h * 50 / 180), w, int(h * 135 / 180)))
    band = band.resize((STRIP_W, band.height * STRIP_W // w), Image.LANCZOS)
    d = ImageDraw.Draw(band)
    cx = STRIP_W // 2
    d.polygon([(cx - 9, 0), (cx + 9, 0), (cx, 14)], fill="yellow")
    return band


def main():
    batch = sys.argv[1]
    cands = json.load(open(f"{batch}/candidates.json"))
    countries = pool.load_countries()
    other = avoid.from_env()
    os.makedirs(f"{batch}/overpass", exist_ok=True)
    drop, ok = {}, []
    for c in sorted(cands, key=lambda c: c["n"]):
        n, lat, lon = c["n"], c["lat"], c["lon"]
        # A frame on a quay or a small island can lie outside every outline;
        # then the plan's country stands for it, and without one it goes.
        iso = pool.country_of(countries, lat, lon) or c.get("country")
        if not iso:
            drop[n] = "in no country of the borders"
            continue
        if iso not in pool.WEIGHTS or not pool.allowed(iso, lat, lon):
            drop[n] = f"in {iso}, a country or a part of one left out"
            continue
        if other:
            why = other.why(lat, lon, c["source_id"])
            if why:
                drop[n] = why
                continue
        near = next((k for k in ok if avoid.km(lat, lon, k["lat"], k["lon"]) < SPACING_KM), None)
        if near:
            drop[n] = f"{avoid.km(lat, lon, near['lat'], near['lon']):.1f} km from candidate {near['n']}"
            continue
        ok.append(c)
    answers = overpass([(c["n"], str(c["source_id"]), c["lat"], c["lon"]) for c in ok], f"{batch}/overpass")
    kept = []
    for c in ok:
        mil = military(answers[c["n"]])
        if mil:
            drop[c["n"]] = "military: " + "; ".join(sorted(set(mil)))[:200]
        else:
            kept.append(c)

    font = ImageFont.truetype("/System/Library/Fonts/Supplemental/Arial Bold.ttf", 40)
    sheets = {}
    for k in range(0, len(kept), PER_SHEET):
        part = kept[k:k + PER_SHEET]
        strips = [strip(Image.open(f"{batch}/full/{c['n']:03d}.jpg").convert("RGB")) for c in part]
        sheet = Image.new("RGB", (STRIP_W, sum(s.height + 8 for s in strips)), "black")
        d = ImageDraw.Draw(sheet)
        y = 0
        for c, s in zip(part, strips):
            sheet.paste(s, (0, y))
            label = str(c["n"])
            bx = d.textbbox((0, 0), label, font=font)
            d.rectangle((0, y, bx[2] + 16, y + bx[3] + 10), fill="black")
            d.text((8, y + 2), label, fill="white", font=font)
            y += s.height + 8
        name = f"pano-{k // PER_SHEET + 1}.jpg"
        sheet.save(f"{batch}/{name}", quality=85)
        sheets[name] = [c["n"] for c in part]
    frames = {str(c["n"]): str(c["source_id"]) for c in cands}
    json.dump({"drop": {str(k): v for k, v in drop.items()}, "sheets": sheets, "frames": frames},
              open(f"{batch}/screen.json", "w"), ensure_ascii=False, indent=1)
    print(f"screened {len(cands)}: {len(kept)} to review on {len(sheets)} sheets, {len(drop)} dropped")


if __name__ == "__main__":
    main()
