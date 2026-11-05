"""Plan a mixed batch of regions for harvest.py: big cities away from the
centre, small towns, and roads and nature between villages.

A town or a road starts at a random point in a country's Natural Earth
outline. A random point of Mapillary coverage within ~40 km of it is the
road; for a town, Nominatim names the town or village there and gives its
centre. So every region has frames, and none is empty taiga.

    MAPILLARY_TOKEN=... python pool.py <out.json> [cities] [towns] [nature]

Each entry: {"name", "lat", "lon", "km", "inner", "kind", "country"}.

MAPLY_CAP=N plans at most N entries in one country, big cities included, so a
pool of a few hundred places spreads over many countries. MAPLY_AVOID keeps the
towns and roads clear of the other pool's places (see avoid.py).
"""
import gzip, json, math, os, random, socket, sys, threading, time, urllib.parse, urllib.request
import concurrent.futures as cf
import mapbox_vector_tile
from shapely.geometry import shape, Point
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from regions import REGIONS
import avoid

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
    "JO": 0.5, "AE": 0.5, "OM": 0.5, "SA": 0.5, "QA": 0.3, "BH": 0.2, "CY": 0.5,
    # Asia
    "JP": 4, "KR": 2, "TW": 1, "TH": 2, "VN": 1, "MY": 1, "ID": 2, "PH": 1, "IN": 2,
    "LK": 0.5, "NP": 0.5, "KH": 0.5, "CN": 1, "HK": 0.5, "SG": 0.5, "LA": 0.3, "BD": 0.5,
    "BT": 0.2, "BN": 0.2,
    # Africa
    "ZA": 2, "KE": 1, "MA": 1, "TN": 0.5, "NA": 0.5, "BW": 0.5, "TZ": 0.5, "UG": 0.5,
    "GH": 0.5, "SN": 0.5, "RW": 0.3, "EG": 1, "DZ": 0.3, "ZM": 0.3, "ZW": 0.3, "MW": 0.3,
    "MG": 0.5, "MZ": 0.3, "CI": 0.3, "BJ": 0.2, "TG": 0.2, "NG": 0.5, "MU": 0.2,
    # Americas
    "US": 6, "CA": 3, "MX": 2, "BR": 3, "AR": 2, "CL": 2, "CO": 1, "PE": 1, "EC": 0.5,
    "UY": 0.5, "CR": 0.5, "GT": 0.5, "BO": 0.5, "PA": 0.5, "DO": 0.5, "PR": 0.5, "CU": 0.3,
    "JM": 0.3, "HN": 0.3, "SV": 0.3, "NI": 0.3, "BZ": 0.2, "PY": 0.5,
    # Oceania
    "AU": 3, "NZ": 2, "FJ": 0.3, "NC": 0.2, "PF": 0.2,
    # Small European countries
    "LU": 0.5, "MT": 0.5,
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
        # Not along the Syrian border, not the southeast by Iraq and Iran.
        return not (lat < 37.6 and lon > 36.5) and not (lon > 41.5 and lat < 38.6)
    if iso == "IN":
        # Not Kashmir, not the Pakistani border, not the northeast (Manipur).
        return not (lat > 32.3 and lon < 80.5) and not (lon < 75.2 and lat > 23.5) and lon < 92
    if iso == "PH":
        # Not Sulu and Zamboanga, not the rest of Mindanao.
        return not (lat < 8 and lon < 123) and not (lat < 9.6 and lon > 121.8)
    if iso == "AZ":
        # Not Karabakh and the Armenian border, not Nakhchivan.
        return not (39.0 < lat < 41.3 and 44.8 < lon < 47.6) and not (lon < 46.2 and lat < 39.8)
    if iso == "AM":
        return lon < 45.0 and lat > 39.7  # not the Azerbaijani border
    if iso == "BY":
        return lat > 51.9  # not along the Ukrainian border
    if iso == "PL":
        return not (lon > 23.4 and 52.0 < lat < 54.0)  # not the zone along Belarus
    if iso == "FI":
        return lon < 30.0  # not the zone along Russia
    if iso == "EE":
        return lon < 27.8
    if iso == "LV":
        return lon < 27.5
    if iso == "RS":
        return not (41.85 < lat < 43.27 and 20.0 < lon < 21.8)  # not Kosovo, which these borders file under RS
    if iso == "GR":
        return not (lon > 25.9 and lat > 40.7)  # not the Evros border
    if iso == "CY":
        # The Republic's side, below the buffer zone; not the British bases.
        return lat < 35.1 and not (lat < 34.66 and 32.85 < lon < 33.1) and not (34.95 < lat and 33.6 < lon < 33.95)
    if iso == "KR":
        return lat < 37.8  # not near the DMZ
    if iso == "TH":
        # Not the insurgent far south, not the Cambodian border.
        return not (lat < 7.0 and lon > 100.8) and not (13.8 < lat < 15.0 and 102.3 < lon < 105.7)
    if iso == "KH":
        return lat < 13.6 and not (lon < 103.0 and lat > 12.0)  # not the Thai border
    if iso == "ID":
        return lon < 130.5  # not Papua
    if iso == "CN":
        return lon > 100  # the east; not Xinjiang and Tibet
    if iso == "JO":
        # Not the Syrian and Iraqi borders, not the Jordan valley.
        return not (lat > 32.15 and lon > 35.95) and lon < 37.5 and not (lat > 29.6 and lon < 35.65)
    if iso == "SA":
        return 18.5 < lat < 29.0  # not the Yemeni and northern borders
    if iso == "OM":
        return not (lat < 19 and lon < 53.2)  # not by Yemen
    if iso == "UZ":
        return lat > 37.6  # not by Afghanistan
    if iso == "EG":
        # The Nile and the Red Sea coast: not Sinai, the Libyan or the Sudanese border.
        if lat >= 29.95:
            return 27 < lon < 32.35
        return 27 < lon < 32.55 + (29.95 - lat) * 0.57 and lat > 23.5
    if iso == "DZ":
        return lat > 34.5  # the north only
    if iso == "TN":
        return not (lon > 10.8 and lat < 33.6) and not (lon < 8.9 and lat < 35.6)
    if iso == "MA":
        return lat > 28.0  # well north of Western Sahara
    if iso == "KE":
        return lat < 2.5 and not (lon > 39.5 and lat > -2.0)  # not the Somali border
    if iso == "TZ":
        return not (lat < -10.0 and lon > 38.5)  # not by Cabo Delgado
    if iso == "UG":
        return lat < 2.5 and lon > 30.5  # not the north, not the Congolese border
    if iso == "RW":
        return lon > 29.6  # not the Congolese border
    if iso == "MZ":
        return lat < -16.0  # not the insurgent north
    if iso in ("GH", "TG"):
        return lat < 10.0
    if iso == "BJ":
        return lat < 10.5
    if iso == "CI":
        return lat < 9.5
    if iso == "NG":
        return lat < 8.5  # the south only
    if iso == "SN":
        return lat > 13.1  # not Casamance
    if iso == "CO":
        return not (lon > -73.0 and lat > 6.5) and lon > -77.0  # not Catatumbo, not the Pacific coast
    if iso == "PE":
        return not (-13.6 < lat < -11.4 and -74.6 < lon < -73.0)  # not the VRAEM
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
        out.setdefault(iso, []).append(shape(feat["geometry"]))
    return out


def country_of(countries, lat, lon):
    pt = Point(lon, lat)
    for iso, shapes in countries.items():
        if any(s.contains(pt) for s in shapes):
            return iso
    return None


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
AVOID = avoid.from_env()
CAP = int(os.environ.get("MAPLY_CAP", "0"))
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
        if not c or not allowed(iso, *c) or (AVOID and AVOID.why(*c)):
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
    isos = sorted(i for i in countries if i in WEIGHTS)
    weights = [WEIGHTS[i] for i in isos]
    plan = []
    per = {}
    # Big cities, away from the centre the first batches used. A city whose
    # centre is in a part of its country left out is left out too.
    cities = []
    for name, lat, lon, r in REGIONS:
        iso = country_of(countries, lat, lon)
        if iso and not allowed(iso, lat, lon):
            continue
        cities.append((name, lat, lon, r, iso))
    for name, lat, lon, r, iso in random.sample(cities, len(cities)):
        if len(plan) >= n_cities:
            break
        if CAP and iso and per.get(iso, 0) >= CAP:
            continue
        per[iso] = per.get(iso, 0) + 1
        plan.append({"name": name, "lat": lat, "lon": lon, "km": max(r * 2.5, r + 8), "inner": r, "kind": "city", "country": iso})
    jobs = [k for k, n in (("town", n_towns), ("nature", n_nature)) for _ in range(n)]
    done = {"town": 0, "nature": 0}
    lock = threading.Lock()

    def pick():
        # A country is drawn by weight, and not again once it has CAP entries.
        with lock:
            free = [(i, w) for i, w in zip(isos, weights) if not CAP or per.get(i, 0) < CAP]
            iso = random.choices([i for i, _ in free], [w for _, w in free])[0]
            per[iso] = per.get(iso, 0) + 1
            return iso

    def one(kind):
        while True:
            iso = pick()
            e = anchor(countries, iso, kind)
            if e:
                break
            with lock:
                per[iso] -= 1
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
