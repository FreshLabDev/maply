"""Turn the reviewers' verdicts on a screened batch into the reviews.json
maply-places reads, and report what the batch holds.

    python -I choose.py <batch> [target]

Reads <batch>/candidates.json, screen.json and every review-*.json, each
{"keep": {n: difficulty}, "scene": {n: "city"|"town"|"road"},
 "drop": {n: "<code>: <why>"}} with code one of faces, military, sign,
quality, view, other.

A frame goes in when a reviewer kept it and the screen did not drop it, with
at most MAPLY_KEEP_CAP frames a country (default 15) and at most target in
all (default 500). Over either limit, the frame that goes is from the scene
furthest over its share (SHARE: city outskirts, towns, roads, as batch-004)
and, for the total, from the country with the most frames.

Output: <batch>/reviews.json and <batch>/report.md.
"""
import collections, datetime, glob, gzip, json, os, sys
from shapely.geometry import Point, shape

HERE = os.path.dirname(os.path.abspath(__file__))
BORDERS = os.path.join(HERE, "..", "..", "internal", "geo", "countries.geojson.gz")
SHARE = {"city": 0.2, "town": 0.4, "road": 0.4}
CAP = int(os.environ.get("MAPLY_KEEP_CAP", "15"))


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


def main():
    batch = sys.argv[1]
    target = int(sys.argv[2]) if len(sys.argv) > 2 else 500
    cands = {c["n"]: c for c in json.load(open(f"{batch}/candidates.json"))}
    screen = json.load(open(f"{batch}/screen.json"))
    keep, scene, drop = {}, {}, {}
    for path in sorted(glob.glob(f"{batch}/review-*.json")):
        r = json.load(open(path))
        keep.update({int(k): int(v) for k, v in r.get("keep", {}).items()})
        scene.update({int(k): v for k, v in r.get("scene", {}).items()})
        drop.update({int(k): v for k, v in r.get("drop", {}).items()})
    sdrop = {int(k): "screen: " + v for k, v in screen["drop"].items()}
    reviewed = {n for ns in screen["sheets"].values() for n in ns}
    missing = sorted(reviewed - set(keep) - set(drop))
    if missing:
        sys.exit(f"no verdict for {len(missing)} reviewed candidates: {missing[:20]}")
    for n in keep:
        if scene.get(n) not in SHARE:
            sys.exit(f"candidate {n} is kept without a scene")

    at = locator()
    where = {n: at(c["lat"], c["lon"]) for n, c in cands.items()}
    chosen = sorted(n for n in keep if n not in sdrop and n not in drop)
    cut = {}

    def over(ns):
        count = collections.Counter(scene[n] for n in chosen)
        return max(sorted({scene[n] for n in ns}), key=lambda s: count[s] / (SHARE[s] * len(chosen)))

    by_country = collections.defaultdict(list)
    for n in chosen:
        by_country[where[n][0] or cands[n].get("country")].append(n)
    for iso, ns in sorted(by_country.items(), key=lambda x: str(x[0])):
        while len(ns) > CAP:
            s = over(ns)
            n = max(x for x in ns if scene[x] == s)
            ns.remove(n)
            chosen.remove(n)
            cut[n] = f"cap: more than {CAP} in {iso}"
    while len(chosen) > target:
        s = over(chosen)
        count = collections.Counter(where[n][0] for n in chosen if scene[n] == s)
        iso = count.most_common(1)[0][0]
        n = max(x for x in chosen if scene[x] == s and where[x][0] == iso)
        chosen.remove(n)
        cut[n] = f"spare: over the target of {target}"

    out = {
        "reviewer": "claude-sonnet-5-5, with screen.py and choose.py",
        "at": datetime.date.today().isoformat(),
        "pool": "free",
        "keep": {str(n): keep[n] for n in chosen},
        "scene": {str(n): scene[n] for n in chosen},
        "drop": {str(n): v for n, v in sorted({**sdrop, **drop, **cut}.items())},
        "note": f"panoramas only; at most {CAP} a country; scene shares {SHARE}",
    }
    json.dump(out, open(f"{batch}/reviews.json", "w"), ensure_ascii=False, indent=1)

    def table(counter, head):
        rows = [f"| {head} | places |", "|:--|--:|"]
        rows += [f"| {k} | {v} |" for k, v in counter.most_common()]
        return "\n".join(rows)

    def code(reason):
        head = reason.split(":", 1)[0]
        if head == "screen":
            r = reason[len("screen: "):]
            if r.startswith("military"):
                return "screen: military area in OSM"
            if "other pool" in r:
                return "screen: near a place of the day"
            if "from candidate" in r:
                return "screen: near another candidate"
            return "screen: country or zone left out"
        return head

    reasons = collections.Counter(code(v) for v in out["drop"].values())
    lines = [
        f"# {os.path.basename(batch)}",
        "",
        f"{len(cands)} candidates, {len(reviewed)} reviewed, {len(chosen)} kept.",
        "",
        table(collections.Counter(where[n][1] for n in chosen), "continent"),
        "",
        table(collections.Counter(scene[n] for n in chosen), "scene"),
        "",
        table(collections.Counter(keep[n] for n in chosen), "difficulty"),
        "",
        f"{len({where[n][0] for n in chosen})} countries:",
        "",
        table(collections.Counter(where[n][0] for n in chosen), "country"),
        "",
        "Not kept:",
        "",
        table(reasons, "reason"),
        "",
    ]
    open(f"{batch}/report.md", "w").write("\n".join(lines))
    print("\n".join(lines[:12]))


if __name__ == "__main__":
    main()
