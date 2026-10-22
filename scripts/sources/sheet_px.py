import json, random, urllib.request, io, concurrent.futures as cf
from PIL import Image, ImageDraw, ImageFont
from places import PLACES
random.seed(7)
D = 0.03
def pick(p):
    name, lat, lon, kind = p
    u = f"https://api.panoramax.xyz/api/search?limit=500&bbox={lon-D},{lat-D},{lon+D},{lat+D}"
    try: fs = json.load(urllib.request.urlopen(u, timeout=40))["features"]
    except Exception: return None
    if not fs: return None
    f = random.choice(fs)
    url = f["assets"]["sd"]["href"]
    try: im = Image.open(io.BytesIO(urllib.request.urlopen(url, timeout=60).read())).convert("RGB")
    except Exception: return None
    w,h = im.size; pano = abs(w/h-2) < 0.05
    if pano:  # crop the "forward view" out of the equirectangular panorama
        im = im.crop((w//4, h//4, 3*w//4, 3*h//4))
    im.thumbnail((480, 360))
    return name, pano, im
with cf.ThreadPoolExecutor(8) as ex:
    res = [r for r in ex.map(pick, PLACES) if r]
cols = 6; rows = (len(res)+cols-1)//cols
sheet = Image.new("RGB", (cols*480, rows*380), "white"); d = ImageDraw.Draw(sheet)
try: font = ImageFont.truetype("/System/Library/Fonts/Supplemental/Arial Unicode.ttf", 20)
except Exception: font = None
for i,(name,pano,im) in enumerate(res):
    x,y = (i%cols)*480, (i//cols)*380
    sheet.paste(im, (x, y+20)); d.text((x+4, y), f"{i+1}. {name}{' [360]' if pano else ''}", fill="black", font=font)
sheet.save("panoramax_sheet.jpg", quality=80)
print(len(res), sheet.size)
