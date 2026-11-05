"""The places a batch keeps clear of: the other pool's.

A place of the free game must never become a place of the day, and the free
game must not show the spot of a future day. So a free batch stays away from
every place of the day in maply-places' SQL and from every candidate of a
daily batch (a spare frame there may still go to the queue), and a daily
batch stays away from the free pool the same way.

    MAPLY_AVOID=curation/out/places.sql,curation/out-004/places.sql,curation/batch-004/candidates.json
    MAPLY_AVOID_KM=10

A frame is refused when it is the same source frame as one of them or lies
within MAPLY_AVOID_KM of one.
"""
import json, math, os, re

INSERT = re.compile(r"INSERT INTO maply\.place \(([^)]*)\)\s*VALUES \(")


def _tuple(sql, i):
    """The comma-separated values of the tuple opening at sql[i], as text."""
    vals, cur, depth, quoted = [], [], 0, False
    while i < len(sql):
        ch = sql[i]
        if quoted:
            if ch == "'" and sql[i + 1:i + 2] == "'":
                cur.append("'")
                i += 1
            elif ch == "'":
                quoted = False
            else:
                cur.append(ch)
        elif ch == "'":
            quoted = True
        elif ch == "(":
            depth += 1
            cur.append(ch)
        elif ch == ")" and depth == 0:
            vals.append("".join(cur).strip())
            return vals
        elif ch == ")":
            depth -= 1
            cur.append(ch)
        elif ch == "," and depth == 0:
            vals.append("".join(cur).strip())
            cur = []
        else:
            cur.append(ch)
        i += 1
    raise ValueError("unterminated VALUES tuple")


def read_sql(path):
    """(source_id, lat, lon) of every place maply-places' SQL inserts."""
    sql = open(path, encoding="utf-8").read()
    out = []
    for m in INSERT.finditer(sql):
        cols = [c.strip() for c in m.group(1).split(",")]
        vals = dict(zip(cols, _tuple(sql, m.end())))
        out.append((vals["source_id"], float(vals["lat"]), float(vals["lon"])))
    return out


def read_candidates(path):
    return [(str(c["source_id"]), c["lat"], c["lon"]) for c in json.load(open(path, encoding="utf-8"))]


def km(lat1, lon1, lat2, lon2):
    p1, p2 = math.radians(lat1), math.radians(lat2)
    a = math.sin((p2 - p1) / 2) ** 2 + math.cos(p1) * math.cos(p2) * math.sin(math.radians(lon2 - lon1) / 2) ** 2
    return 2 * 6371.0088 * math.asin(min(1, math.sqrt(a)))


class Avoid:
    def __init__(self, paths, radius):
        self.radius = radius
        self.points, self.ids = [], set()
        for p in paths:
            rows = read_sql(p) if p.endswith(".sql") else read_candidates(p)
            for sid, lat, lon in rows:
                self.ids.add(sid)
                self.points.append((lat, lon))

    def taken(self, source_id):
        return str(source_id) in self.ids

    def why(self, lat, lon, source_id=None):
        """None when the frame is clear, else the reason it is not."""
        if source_id is not None and self.taken(source_id):
            return "the same frame is in the other pool"
        dlat = self.radius / 111.0
        for plat, plon in self.points:
            if abs(plat - lat) > dlat:
                continue
            d = km(lat, lon, plat, plon)
            if d < self.radius:
                return f"{d:.1f} km from a place of the other pool"
        return None

    def __len__(self):
        return len(self.points)


def from_env():
    paths = [p for p in os.environ.get("MAPLY_AVOID", "").split(",") if p.strip()]
    if not paths:
        return None
    return Avoid([p.strip() for p in paths], float(os.environ.get("MAPLY_AVOID_KM", "10")))
