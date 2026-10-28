"""Plan a mixed batch of regions for harvest.py: big cities away from the
centre, small towns, and roads and nature between villages.

A town or a road starts at a random point in a country's Natural Earth
outline. A random point of Mapillary coverage within ~40 km of it is the
road; for a town, Nominatim names the town or village there and gives its
centre. So every region has frames, and none is empty taiga.

    MAPILLARY_TOKEN=... python pool.py <out.json> [cities] [towns] [nature]

Each entry: {"name", "lat", "lon", "km", "inner", "kind", "country"}.
"""
import gzip, json, math, os, random, socket, sys, threading, time, urllib.parse, urllib.request
import concurrent.futures as cf
import mapbox_vector_tile
from shapely.geometry import shape, Point
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from regions import REGIONS

HERE = os.path.dirname(os.path.abspath(__file__))
BORDERS = os.path.join(HERE, "..", "..", "internal", "geo", "countries.geojson.gz")
UA = "maply-curation/0 (+https://github.com/FreshLabDev/maply)"

# Where a random frame is likely to exist on Mapillary and be safe to show,
# with a weight for how often the country comes up.
WEIGHTS = {
    # Europe
    "FR": 4, "DE": 4, "ES": 4, "IT": 4, "GB": 3, "PL": 3, "NO": 2, "SE": 2, "FI": 2, "PT": 2,
    "NL": 1, "BE": 1, "CH": 1, "AT": 1, "CZ": 2, "SK": 1, "HU": 1, "RO": 2, "BG": 1, "GR": 2,
    "HR": 1, "SI": 1, "RS": 1, "BA": 1, "ME": 0.5, "AL": 0.5, "MK": 0.5, "IE": 1, "IS": 1,
    "DK": 1, "EE": 1, "LV": 1, "LT": 1, "MD": 0.5,
    # Russia, Ukraine, Belarus
    "RU": 4, "UA": 3, "BY": 2,
    # Caucasus, Turkey, Middle East, Central Asia
    "GE": 1, "AM": 0.5, "AZ": 0.5, "TR": 3, "KZ": 1, "KG": 0.5, "UZ": 0.5, "MN": 0.5,
    "JO": 0.5, "AE": 0.5, "OM": 0.5,
    # Asia
    "JP": 4, "KR": 2, "TW": 1, "TH": 2, "VN": 1, "MY": 1, "ID": 2, "PH": 1, "IN": 2,
    "LK": 0.5, "NP": 0.5, "KH": 0.5,
    # Africa
    "ZA": 2, "KE": 1, "MA": 1, "TN": 0.5, "NA": 0.5, "BW": 0.5, "TZ": 0.5, "UG": 0.5,
    "GH": 0.5, "SN": 0.5, "RW": 0.3,
    # Americas
    "US": 6, "CA": 3, "MX": 2, "BR": 3, "AR": 2, "CL": 2, "CO": 1, "PE": 1, "EC": 0.5,
    "UY": 0.5, "CR": 0.5, "GT": 0.5, "BO": 0.5,
    # Oceania
    "AU": 3, "NZ": 2,
}


def allowed(iso, lat, lon):
    """False for the parts of a country left out: war zones and the lines
    near them, disputed land, and far islands of another continent."""
    if iso == "UA":
        # West and centre only: no front line, no occupied land, no Crimea.
        return (lon < 32.5 and lat > 47.0) or (lon < 31.0 and lat > 45.6)
    if iso == "RU":
        if lon < 40 and lat < 54:  # the regions along the Ukrainian border
            return False
        if lat < 44.6 and 42 < lon < 48.6:  # the North Caucasus republics
            return False
        return True
    if iso == "GE":
        return not (lon < 41.6 and lat > 42.4) and not (42.0 < lat < 42.75 and 43.3 < lon < 44.7)
    if iso == "MD":
        return lon < 28.95  # not Transnistria
    if iso == "TR":
        return not (lat < 37.6 and lon > 36.5)  # not along the Syrian border
    if iso == "IN":
        return not (lat > 32.3 and lon < 80.5)  # not Kashmir
    if iso == "PH":
        return not (lat < 8 and lon < 123)
    if iso == "US":
        return 24 < lat < 50 and -125 < lon < -66
    if iso == "FR":
        return 41 < lat < 51.5 and -5.5 < lon < 9.8
    if iso == "NL":
        return lat > 50
    if iso == "NO":
        return lat < 71.5
    if iso == "CL":
        return lon > -76
    if iso == "EC":
        return lon > -82
    return True


def load_countries():
    with gzip.open(BORDERS, "rt") as f:
        data = json.load(f)
    out = {}
    for feat in data["features"]:
        iso = feat["properties"]["iso"]
        if iso in WEIGHTS:
            out.setdefault(iso, []).append(shape(feat["geometry"]))
    return out


def random_in(shapes, iso):
    """A random allowed point in a country, by area."""
    areas = [s.area for s in shapes]
    for _ in range(400):
        s = random.choices(shapes, areas)[0]
        x0, y0, x1, y1 = s.bounds
        lon, lat = random.uniform(x0, x1), random.uniform(y0, y1)
        if allowed(iso, lat, lon) and s.contains(Point(lon, lat)):
            return lat, lon
    return None


# OSM's servers are slow over IPv6 from some networks; IPv4 answers at once.
_getaddrinfo = socket.getaddrinfo
socket.getaddrinfo = lambda host, *a, **k: [r for r in _getaddrinfo(host, *a, **k) if r[0] == socket.AF_INET] or _getaddrinfo(host, *a, **k)

TOKEN = os.environ["MAPILLARY_TOKEN"]
MIN_TS = 1514764800000  # 2018-01-01, as harvest.py
# The game shows 360° panoramas: only drives shot with a 360° camera count.
PANO_ONLY = os.environ.get("MAPLY_PANO_ONLY", "1") != "0"


def get(url, timeout=60):
    req = urllib.request.Request(url, headers={"User-Agent": UA, "Accept-Language": "en"})
    for attempt in range(3):
        try:
            return urllib.request.urlopen(req, timeout=timeout).read()
        except Exception:
            if attempt == 2:
                raise
            time.sleep(2 * (attempt + 1))


def tile_of(lat, lon, z):
    n = 2 ** z
    return int((lon + 180) / 360 * n), int((1 - math.asinh(math.tan(math.radians(lat))) / math.pi) / 2 * n)


def tile_point(x, y, z, fx, fy, extent=4096):
    n = 2 ** z
    lon = (x + fx / extent) / n * 360 - 180
    lat = math.degrees(math.atan(math.sinh(math.pi * (1 - 2 * (y + 1 - fy / extent) / n))))
    return lat, lon


def covered(lat, lon, dense=False):
    """A random point on a Mapillary sequence in the z10 tile (about 40 km)
    around lat/lon, or None. A long drive has more points than a walk round
    the square, so roads come up more often than town centres."""
    x, y = tile_of(lat, lon, 10)
    try:
        t = mapbox_vector_tile.decode(get(f"https://tiles.mapillary.com/maps/vtp/mly1_public/2/10/{x}/{y}?access_token={TOKEN}"))
    except Exception:
        return None
    points = []
    for f in t.get("sequence", {}).get("features", []):
        if (f["properties"].get("captured_at") or 0) < MIN_TS:
            continue
        if PANO_ONLY and not f["properties"].get("is_pano"):
            continue
        g = f["geometry"]
        if g["type"] == "MultiLineString":
            lines = g["coordinates"]
        elif g["type"] == "LineString":
            lines = [g["coordinates"]]
        else:  # a sequence of one frame is a point
            lines = [[g["coordinates"]]]
        for line in lines:
            points.extend(tuple(p) for p in line)
    if not points:
        return None
    if dense:
        # Towns are where the drives cross: one of the five busiest ~2.5 km
        # cells of the tile, then a point in it.
        cells = {}
        for p in points:
            cells.setdefault((p[0] // 256, p[1] // 256), []).append(p)
        busiest = sorted(cells.values(), key=len, reverse=True)[:5]
        return tile_point(x, y, 10, *random.choice(random.choice(busiest)))
    return tile_point(x, y, 10, *random.choice(points))


last_nm = 0.0
nm_lock = threading.Lock()


def place_at(lat, lon):
    """The town or village Nominatim finds around the point."""
    global last_nm
    q = urllib.parse.urlencode({"format": "jsonv2", "lat": f"{lat:.6f}", "lon": f"{lon:.6f}", "zoom": 13, "namedetails": 1})
    with nm_lock:  # Nominatim's policy: one request a second, from all threads
        wait = last_nm + 1.1 - time.time()
        if wait > 0:
            time.sleep(wait)
        last_nm = time.time()
    try:
        return json.loads(get("https://nominatim.openstreetmap.org/reverse?" + q))
    except Exception:
        return None


def label(r, iso):
    names = r.get("namedetails") or {}
    name = names.get("name:ru") or names.get("name:en") or r.get("name") or "?"
    return f"{name}, {iso}"


def anchor(countries, iso, kind):
    """A covered point in iso: the centre of a town or village for "town",
    any covered road for "nature"."""
    for _ in range(30):
        p = random_in(countries[iso], iso)
        if not p:
            return None
        c = covered(*p, dense=kind == "town")
        if not c or not allowed(iso, *c):
            continue
        r = place_at(*c)
        if not r or r.get("error") or (r.get("address", {}).get("country_code") or "").upper() not in (iso, ""):
            continue
        if kind == "nature":
            return {"name": label(r, iso), "lat": c[0], "lon": c[1]}
        if r.get("addresstype") in ("town", "village"):
            # The covered point itself: the town's centre may have no frame.
            return {"name": label(r, iso), "lat": c[0], "lon": c[1]}
    return None


def main():
    out = sys.argv[1]
    n_cities, n_towns, n_nature = (int(x) for x in (sys.argv[2:5] + ["45", "100", "110"][len(sys.argv[2:5]):]))
    random.seed()
    countries = load_countries()
    isos = sorted(countries)
    weights = [WEIGHTS[i] for i in isos]
    plan = []
    # Big cities, away from the centre the first batches used.
    for name, lat, lon, r in random.sample(REGIONS, min(n_cities, len(REGIONS))):
        plan.append({"name": name, "lat": lat, "lon": lon, "km": max(r * 2.5, r + 8), "inner": r, "kind": "city"})
    jobs = [k for k, n in (("town", n_towns), ("nature", n_nature)) for _ in range(n)]
    done = {"town": 0, "nature": 0}
    lock = threading.Lock()

    def one(kind):
        while True:
            iso = random.choices(isos, weights)[0]
            e = anchor(countries, iso, kind)
            if e:
                break
        entry = {**e, "kind": kind, "country": iso}
        entry.update({"km": 0.3, "inner": 0})
        with lock:
            plan.append(entry)
            done[kind] += 1
            print(f"{kind} {done[kind]}: {entry['name']}", file=sys.stderr)

    # Mapillary tiles load in parallel; Nominatim stays at one request a second.
    with cf.ThreadPoolExecutor(4) as ex:
        list(ex.map(one, jobs))
    json.dump(plan, open(out, "w"), ensure_ascii=False, indent=1)
    print(f"{len(plan)} regions in {out}")


if __name__ == "__main__":
    main()
