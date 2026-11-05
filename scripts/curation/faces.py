"""A faces-only look at prepared places, at close to the game's detail.

The review sheets show the whole panorama at about 4.4 px a degree; the game
shows 11.4 after -sharpen. A face too small to judge on a sheet may be clear
in the game, so after maply-places and -sharpen every place gets one more
sheet: the two halves of the panorama, 180 degrees each, from 25 degrees
above the horizon down to -45, at 1568 px a half (8.7 px a degree).

    python -I faces.py sheets <out> <batch>...      # MAPLY_FACES_IMAGES=1: see below
    python -I faces.py verdicts <out> <batch>...

sheets draws <out>/faces/<batch>-<n>.jpg for every place of <out>/sharpen.csv
that has none yet, from <out>/sharp. A place -sharpen could not fetch again
waits, unless MAPLY_FACES_IMAGES=1 draws it from <out>/images, which is what
the game then shows. A reviewer writes
<out>/faces/review-K.json: {"drop": {"<batch>/<n>": "faces: <why>"},
"ok": ["<batch>/<n>", ...]}. verdicts turns them into <batch>/review-faces.json,
which choose.py reads with the other reviews, and fails while a place of
<out> has no verdict. File names carry the batch and number only.
"""
import csv, glob, json, os, sys
from PIL import Image, ImageDraw, ImageFont

W = 1568


def refs(out, batches):
    """<batch>/<n> of every place in <out>, by its source ID."""
    by_id = {}
    for b in batches:
        for c in json.load(open(f"{b}/candidates.json")):
            by_id[str(c["source_id"])] = f"{os.path.basename(b.rstrip('/'))}/{c['n']:03d}"
    return [(by_id[r["source_id"]], r["image_key"]) for r in csv.DictReader(open(f"{out}/sharpen.csv"))]


def half(band, left):
    """180 degrees of an equirectangular band from column left, wrapping."""
    w = band.width
    a = band.crop((left, 0, min(left + w // 2, w), band.height))
    if a.width < w // 2:
        whole = Image.new("RGB", (w // 2, band.height))
        whole.paste(a, (0, 0))
        whole.paste(band.crop((0, 0, w // 2 - a.width, band.height)), (a.width, 0))
        a = whole
    return a.resize((W, a.height * W // a.width), Image.LANCZOS)


def sheets(out, batches):
    os.makedirs(f"{out}/faces", exist_ok=True)
    font = ImageFont.truetype("/System/Library/Fonts/Supplemental/Arial Bold.ttf", 34)
    made, waiting = 0, 0
    for ref, key in refs(out, batches):
        dst = f"{out}/faces/{ref.replace('/', '-')}.jpg"
        if os.path.exists(dst):
            continue
        path = f"{out}/sharp/{key}"
        if not os.path.exists(path):
            if os.environ.get("MAPLY_FACES_IMAGES") != "1":
                waiting += 1
                continue
            path = f"{out}/images/{key}"
        im = Image.open(path).convert("RGB")
        w, h = im.size
        band = im.crop((0, int(h * 65 / 180), w, int(h * 135 / 180)))
        # The front half is centred where the game opens, the frame's middle.
        parts = [(half(band, w // 4), "front"), (half(band, w * 3 // 4), "back")]
        sheet = Image.new("RGB", (W, sum(p.height + 6 for p, _ in parts)), "black")
        d = ImageDraw.Draw(sheet)
        y = 0
        for p, label in parts:
            sheet.paste(p, (0, y))
            text = f"{ref} {label}"
            bx = d.textbbox((0, 0), text, font=font)
            d.rectangle((0, y, bx[2] + 14, y + bx[3] + 8), fill="black")
            d.text((7, y + 2), text, fill="white", font=font)
            y += p.height + 6
        sheet.save(dst, quality=86)
        made += 1
    print(f"{made} new face sheets in {out}/faces, {waiting} waiting for sharp/")


def verdicts(out, batches):
    drop, ok = {}, set()
    for path in sorted(glob.glob(f"{out}/faces/review-*.json")):
        r = json.load(open(path))
        drop.update(r.get("drop", {}))
        ok |= set(r.get("ok", []))
    places = [ref for ref, _ in refs(out, batches)]
    missing = [ref for ref in places if ref not in drop and ref not in ok]
    for b in batches:
        name = os.path.basename(b.rstrip("/"))
        mine = {ref.split("/")[1]: why for ref, why in sorted(drop.items()) if ref.split("/")[0] == name}
        json.dump({"reviewer": "faces.py", "drop": {str(int(n)): why for n, why in mine.items()}},
                  open(f"{b}/review-faces.json", "w"), ensure_ascii=False, indent=1)
    print(f"{len(places)} places: {len(drop)} dropped for faces, {len(missing)} without a verdict")
    if missing:
        sys.exit(f"no faces verdict for {missing[:20]}")


if __name__ == "__main__":
    {"sheets": sheets, "verdicts": verdicts}[sys.argv[1]](sys.argv[2], sys.argv[3:])
