import json, sys, urllib.request, concurrent.futures as cf
from places import PLACES
D = 0.03  # window half-width, ~3 km
def one(p):
    name, lat, lon, kind = p
    url = f"https://api.panoramax.xyz/api/search?limit=500&bbox={lon-D},{lat-D},{lon+D},{lat+D}"
    try:
        d = json.load(urllib.request.urlopen(url, timeout=40))
    except Exception as e:
        return name, kind, None, None, str(e)[:60]
    fs = d.get("features", [])
    pano = 0
    for f in fs:
        io = f["properties"].get("pers:interior_orientation", {}) or {}
        dims = io.get("sensor_array_dimensions") or [0, 1]
        if io.get("field_of_view") == 360 or (dims[1] and dims[0] / dims[1] == 2):
            pano += 1
    return name, kind, len(fs), pano, ""
with cf.ThreadPoolExecutor(8) as ex:
    res = list(ex.map(one, PLACES))
ok = [r for r in res if r[2] is not None]
for r in res: print(f"{r[0]:22} {r[1]:5} photos={r[2]} pano={r[3]} {r[4]}")
print("\nwith photos:", sum(1 for r in ok if r[2] > 0), "of", len(ok), "| with panoramas:", sum(1 for r in ok if r[3] > 0))
