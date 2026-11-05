"""Turn the reviewers' verdicts on screened batches into the reviews.json
maply-places reads, and report what the batches hold together.

    MAPLY_TARGET=500 python -I choose.py <batch>...

Reads each batch's candidates.json, screen.json and every review-*.json, each
{"keep": {n: difficulty}, "scene": {n: "city"|"town"|"road"},
 "drop": {n: "<code>: <why>"}} with code one of faces, military, sign,
quality, view, other. A drop in any review wins over a keep in another.

A frame goes in when a reviewer kept it and the screen did not drop it, with
at most MAPLY_KEEP_CAP frames a country (default 15) and at most MAPLY_TARGET
in all (default 500), counted over all the batches given. Over either limit,
the frame that goes is from the scene furthest over its share (SHARE: city
outskirts, towns, roads, as batch-004) and, for the total, from the country
with the most frames.

Output: every batch's reviews.json, and the same report.md in each.
"""
import collections, datetime, glob, gzip, json, os, sys
from shapely.geometry import Point, shape

HERE = os.path.dirname(os.path.abspath(__file__))
BORDERS = os.path.join(HERE, "..", "..", "internal", "geo", "countries.geojson.gz")
SHARE = {"city": 0.2, "town": 0.4, "road": 0.4}
CAP = int(os.environ.get("MAPLY_KEEP_CAP", "15"))
TARGET = int(os.environ.get("MAPLY_TARGET", "500"))


def locator():
    with gzip.open(BORDERS, "rt") as f:
        feats = [(shape(x["geometry"]), x["properties"]) for x in json.load(f)["features"]]

    def at(lat, lon):
        p = Point(lon, lat)
        for g, props in feats:
            if g.contains(p):
                return props["iso"], props["continent"]
        return None, None
    return at


def code(reason):
    head = reason.split(":", 1)[0]
    if head != "screen":
        return head
    r = reason[len("screen: "):]
    if r.startswith("military"):
        return "screen: military area in OSM"
    if "other pool" in r:
        return "screen: near a place of the day"
    if "from candidate" in r:
        return "screen: near another candidate"
    return "screen: country or zone left out"


def table(counter, head):
    rows = [f"| {head} | places |", "|:--|--:|"]
    return "\n".join(rows + [f"| {k} | {v} |" for k, v in counter.most_common()])


def main():
    batches = sys.argv[1:]
    at = locator()
    cands, keep, scene, drop, reviewed = {}, {}, {}, {}, set()
    for b in batches:
        for c in json.load(open(f"{b}/candidates.json")):
            cands[(b, c["n"])] = c
        screen = json.load(open(f"{b}/screen.json"))
        drop.update({(b, int(k)): "screen: " + v for k, v in screen["drop"].items()})
        reviewed |= {(b, n) for ns in screen["sheets"].values() for n in ns}
        rdrop = {}
        for path in sorted(glob.glob(f"{b}/review-*.json")):
            r = json.load(open(path))
            keep.update({(b, int(k)): int(v) for k, v in r.get("keep", {}).items()})
            scene.update({(b, int(k)): v for k, v in r.get("scene", {}).items()})
            rdrop.update({(b, int(k)): v for k, v in r.get("drop", {}).items()})
        drop.update(rdrop)
    missing = sorted(reviewed - set(keep) - set(drop))
    if missing:
        sys.exit(f"no verdict for {len(missing)} reviewed candidates: {missing[:20]}")
    for k in keep:
        if scene.get(k) not in SHARE:
            sys.exit(f"candidate {k} is kept without a scene")

    where = {k: at(c["lat"], c["lon"]) for k, c in cands.items()}
    iso = {k: where[k][0] or cands[k].get("country") for k in cands}
    chosen = sorted(k for k in keep if k not in drop)

    def over(ks):
        count = collections.Counter(scene[k] for k in chosen)
        return max(sorted({scene[k] for k in ks}), key=lambda s: count[s] / (SHARE[s] * len(chosen)))

    by_country = collections.defaultdict(list)
    for k in chosen:
        by_country[iso[k]].append(k)
    for country, ks in sorted(by_country.items(), key=lambda x: str(x[0])):
        while len(ks) > CAP:
            s = over(ks)
            k = max(x for x in ks if scene[x] == s)
            ks.remove(k)
            chosen.remove(k)
            drop[k] = f"cap: more than {CAP} in {country}"
    while len(chosen) > TARGET:
        s = over(chosen)
        count = collections.Counter(iso[k] for k in chosen if scene[k] == s)
        country = count.most_common(1)[0][0]
        k = max(x for x in chosen if scene[x] == s and iso[x] == country)
        chosen.remove(k)
        drop[k] = f"spare: over the target of {TARGET}"

    today = datetime.date.today().isoformat()
    for b in batches:
        out = {
            "reviewer": "claude-sonnet-5-5, with screen.py and choose.py",
            "at": today,
            "pool": "free",
            "keep": {str(n): keep[(bb, n)] for bb, n in chosen if bb == b},
            "scene": {str(n): scene[(bb, n)] for bb, n in chosen if bb == b},
            "drop": {str(n): v for (bb, n), v in sorted(drop.items()) if bb == b},
            "note": f"panoramas only; at most {CAP} a country and {TARGET} in all over "
                    + ", ".join(os.path.basename(x) for x in batches),
        }
        json.dump(out, open(f"{b}/reviews.json", "w"), ensure_ascii=False, indent=1)

    names = ", ".join(os.path.basename(b) for b in batches)
    reasons = collections.Counter(code(v) for k, v in drop.items() if k in cands)
    lines = [
        f"# Free pool: {names}",
        "",
        f"{len(cands)} candidates, {len(reviewed)} reviewed after the screen, {len(chosen)} kept,"
        f" {len({iso[k] for k in chosen})} countries.",
        "",
        table(collections.Counter(where[k][1] for k in chosen), "continent"),
        "",
        table(collections.Counter(scene[k] for k in chosen), "scene"),
        "",
        table(collections.Counter(keep[k] for k in chosen), "difficulty"),
        "",
        "Not kept:",
        "",
        table(reasons, "reason"),
        "",
        table(collections.Counter(iso[k] for k in chosen), "country"),
        "",
    ]
    for b in batches:
        open(f"{b}/report.md", "w").write("\n".join(lines))
    print("\n".join(lines))


if __name__ == "__main__":
    main()
