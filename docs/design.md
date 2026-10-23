# maply: design

Draft of 20 October 2026. No code yet.

maply is a daily "guess the place from a photo" game in Telegram and on the web. Every morning all players get the same place, put a pin on the map, and learn the answer in the evening. maply also gives Asterfield its first website.

## Daily cycle

There is one place per calendar date for everyone: DMs, groups and the site. Days are numbered from launch: `maply #1`, `maply #2`.

The reveal is 20:00 in the chat's own time zone. A group admin picks it in the settings, a person picks the zone of their DM. Until someone does, it follows the capital of the language (`core.effective_language`), as quoto does: Russian and any language without a zone of its own go by Kyiv (owner decision of 22 October 2026).

| Language | Time zone |
|:--|:--|
| ru, uk | `Europe/Kyiv` |
| be | `Europe/Minsk` |
| en | `Europe/London` |
| es, fr, de, it, pl, cs, sv, ca | `Europe/Berlin` (one rule for Central Europe) |
| tr | `Europe/Istanbul` |
| ar | `Asia/Riyadh` |
| zh | `Asia/Shanghai` |
| ja | `Asia/Tokyo` |

A group's zone is fixed in `chat_settings` the first time maply is set up there, so a later change of the group's language does not move its reveal. The picker offers sixteen zones from Los Angeles to Sydney; a zone outside the list is never stored.

Zones come from tzdata and offsets are not hard-coded: Ukraine's daylight saving rules have been changed by law. In winter Kyiv is an hour behind Moscow and Minsk, and the game accounts for that (see "Anti-cheat").

| Time | Group | DM |
|:--|:--|:--|
| 07:00 | Photo of the day, "Guess" button, silent pin | Photo of the day and "Guess" button |
| 07:00–20:00 | The bot edits the pin: who has already answered, no distances | none |
| Random time in 16:00–18:00 | One intrigue message, if not everyone has answered | One intrigue reminder, if the person has not answered |
| 20:00 | Unpin, publish the result | The result, if the person is not in any group with maply |

A group can move the morning post to another time. The answer reveal time is fixed: anti-cheat depends on it.

The bot writes at most three messages a day to a group: the morning post, the intrigue and the result. Edits to the pin do not count as messages.

## How players answer

The "Guess" button opens a Mini App. The photo or panorama is on top, the map is below, the player puts a pin and taps "Done".

- In a DM this is a `web_app` button.
- `web_app` buttons do not work in groups, so a link button leads to `t.me/maplyin_bot/play?startapp=<payload>`. The payload holds the round date and the chat, signed with HMAC so that a foreign chat cannot be substituted. The bot additionally checks chat membership through `getChatMember`.
- Geolocation sent straight to the group is not accepted: everyone would see the other player's pin.
- Answering with a location in the bot's DM is available as a fallback if the Mini App does not open.

An answer cannot be changed after "Done".

### Scoring

The score for the pin has two parts, 5,000 at most:

- **distance:** `3500 · e^(−d / 1492.7)`, where `d` is the distance in kilometers. The curve is the same as GeoGuessr's for the world map: 500 km gives about 2,500, 2,000 km about 900;
- **bonuses** for the pin landing on the same continent (+300), in the same country (+500) and in the same city (+700). Bonuses form a ladder: the country counts only together with the continent, the city only together with the country.

Bonuses make a correctly guessed country visible: a pin right at a border but on the wrong side gets 800 points less. The server determines country and continent from Natural Earth polygons, and the city from the OSM administrative boundary. On the result screen the strip under the header shows these parts separately, and the total builds up together with it.

On top of that comes a **speed bonus** for answering early, counted back from the person's deadline (20:00 of the earliest zone among their chats): +250 at the 07:00 post, falling evenly to +100 at 16:00, when the afternoon intrigue starts; after that from +50 down to nothing at the deadline. Rounded to tens. It is a nudge, not a race: 250 is the difference between a pin about 110 km and one about 230 km away, and nothing next to the country bonus. The result screen shows it as a gold part of the bar after the bonuses. Answers scored before the bonus existed have none.

On equal scores, whoever answered earlier ranks higher.

Personal hints in the Mini App:

| Hint | Score ceiling after it |
|:--|:--|
| Continent | 4,000 |
| A second view of the same place | 3,000 |

Whoever answered before the intrigue and without hints gets a "clean" mark and +10%.

## Anti-cheat

1. **One answer per person per day.** The answer is shared across the DM, all groups and the site (when signed in through Telegram). A person's pin appears on the maps of all their groups.
2. **The answer window closes at the earliest reveal.** For each person, accepting answers ends at the earliest reveal among their contexts: the DM and all groups where the bot has seen them (presence in `core`). Example: a person in a Ukrainian group who is also in a Russian one must answer before 20:00 Moscow time in winter, not Kyiv time.
3. **The group gives out the answer.** If a person is in at least one group with maply, the DM does not reveal the place before the group does. After the reveal, the DM gets only their own line ("312 km · 4,057") and a link to the result in the chat.
4. **The site does not spoil.** Today's place on the site is revealed only after the latest reveal across all languages.
5. **The source does not leak.** The photo is re-uploaded under a random name and without EXIF. Neither Mapillary IDs nor Panoramax links go out: given a Mapillary ID, the API immediately returns coordinates, and Panoramax HD frames contain GPS in EXIF (checked). Panoramas are shown by our own viewer (Photo Sphere Viewer), not MapillaryJS.
6. **No defense against image search.** Google Lens finds streets poorly, and the rest relies on honor, as in Wordle. A leak through friends from another chat cannot be closed either.

## Daily result

At 20:00 the group gets one message:

- a map with the true point and the pins of everyone who answered; the picture is rendered by the service from its own result card page (see docs/mvp-plan.md, decision 1);
- the place: city and country (reverse geocoding through Nominatim) and one short fact, checked during curation;
- a table: distance, score, streak, and how many of the group answered.

### World ranking

Everyone who answered a day stands in one list, whether they played in a group, in a DM or on the site. Groups are not ranked against each other: the owner decided on 23 October that groups will compare by forwarding their results, and that the day's statistics matter more.

A day is final only after the last zone's reveal, 20:00 in Los Angeles, which is 06:00 in Kyiv. A chat hears about the world once, the day after, never as "so far":

- the morning post carries yesterday's line: how many played, first place with the distance, the median distance and the share who found the country;
- in a DM it adds the person's place: "Your place: 12 of 348, ahead of 97% of players";
- in a group it adds each member's place in the world;
- a chat whose morning comes before the day has closed everywhere (Dubai and east) gets the same lines with that evening's result instead.

Ties go to the earlier answer, then the closer pin.

A line for forwarding without spoilers: `maply #142 · 312 km · 4,057 · 🔥7`.

### Animated posts

The posts are short seamless loops, sent as MP4 without sound, which Telegram plays as a GIF:

- morning: the logo's pin lands on its globe, the day's panorama opens around it as a little planet, unrolls into the street, pans, and rolls back into the globe;
- intrigue: the time there on a dial of the day, dark or light as it is there, with the weather and the dots left until the answer;
- the group's result: the camera flies to the group's pins, the place comes out with a ripple, dashed lines run to every pin along the great circle, and the three closest stand on a podium;
- the world: everyone's pins rain onto one globe and take the colour of how close they came, and the world's first three stand on a podium. It is the same picture in every chat; a person's own place goes in the caption.

They were designed in a browser canvas; `internal/anim` draws the same frames in Go on the CPU, with no browser or GPU:

- the halftone dots of the globes are anti-aliased ellipses computed per pixel, 4° dots for the logo's globe and 1° dots for the map, both from the countries of `internal/geo`;
- text is shaped by HarfBuzz rules (go-text) in Sofia Sans with the chat's language set, since the font draws Bulgarian Cyrillic unless told otherwise; the streak's 🔥 is drawn as a flame, because the brand's faces have no emoji;
- the panorama is projected per pixel with trilinear mip sampling at twice the size and averaged down;
- frames are drawn in parallel, one per core, and piped in order to ffmpeg: libx264, CRF 22, yuv420p, BT.709, `+faststart`.

The frame at the loop's length equals the first one, and a test holds it to that. Measured on an M1 Pro with 10 cores, encoding included (`go run ./cmd/maply-anim -pano pano.jpg`):

| Loop | Frames | Time | CPU per frame | File |
|:--|--:|--:|--:|--:|
| Morning, 14 s | 420 | 4.9 s | 100 ms | 2.4 MB |
| Intrigue, 12 s, 720×720 | 360 | 0.6 s | 3 ms | 0.3 MB |
| Result, 12 s, 10 pins | 360 | 1.6 s | 19 ms | 5.2 MB |
| World, 12 s, 348 pins | 360 | 1.8 s | 26 ms | 2.8 MB |

How the scheduler sends them:

- the morning post of a panorama place is drawn once a day, uploaded once, and sent to every other chat by its `file_id`; a photo place keeps the photo;
- the intrigue is drawn when it tells the time there, per chat, since the dots left until 20:00 are the chat's own; the other teases stay text;
- the result is drawn per chat, and falls back to the map card and then to the photo;
- an upload goes as a document, which Telegram turns into an animation since the MP4 has no sound, because tg has no `sendAnimation` yet;
- names and places in scripts that Sofia Sans lacks (Arabic, Chinese, Japanese, emoji) are written in English or left out of the picture; the caption keeps them;
- the world loop has no slot yet: the world's numbers come in the morning post's caption.

## Intrigue 16–18

One message at a random moment of the window. Rules:

- An intrigue never lies. Every message is a real hint or real statistics.
- The type changes every day and does not repeat two days in a row.
- Material for place-bound types is prepared during place curation and goes through the same check.
- If everyone in the group has answered, there is only one type: "everyone answered, spread 6,000 km, waiting for 20:00".

| Type | Example | Source |
|:--|:--|:--|
| It is now there | "It is 02:14 at night there and +27. Think about it." | Open-Meteo (free for non-commercial use) and the point's time zone |
| Voice of the place | A voice message: how locals say "good morning" | TTS in the place's language, prepared during curation |
| Detail | A crop from the photo: "Look closely at this sign" | A crop marked during curation |
| Chat spread | "Your answers are spread over 11,000 km. Two are on different continents" | The chat's answers |
| World against you | "64% of players bet on Asia. You are in the minority so far" | All answers of the day |
| Rival | "'Workhorses' have already scored 18,400. You have 9,100, and four are silent" | The rival chat from `/rival` |
| Streak | "@masha, 11 days in a row. You have not answered yet today" | Streaks of 5 days or more, can be turned off |

In a DM the intrigue arrives only if the person has not answered yet. The "Chat spread" and "Rival" types are not used in DMs.

## Groups

- **First day.** Right after being added, the bot runs a warm-up round on yesterday's place with the answer the same evening, so the group does not wait until tomorrow morning.
- **Permissions.** The game works without the right to pin messages. The bot tells the admins once that it is more fun with a pin.
- **`/rival`.** Links two chats into a duel: a shared season score and the "Rival" intrigue.
- **Seasons.** A calendar month. At the end the chat gets a champion of the month, and the ranking resets.
- **Settings** through buttons in `/start`, as in quoto: morning post time, intrigue on/off, streak mentions on/off.

## "Where am I?" (after launch)

A participant sends the bot their own photo in a DM and puts a point on the map. Setting the point is mandatory: Telegram strips EXIF from photos sent as photos. The bot turns this into an extra round in the chosen group, usually on weekends: "Guess where Artem was". The author sees a warning not to mark their home and can round the point to the city.

## Site

One frontend for two modes:

- **Mini App.** The user comes from `initData`, and the backend verifies the signature.
- **Website in a browser.** Nobody signs in. The browser keeps a random player id in a cookie; the day's one answer is stored and goes into the world ranking under the name the player gave, or "Guest". There are no streaks or personal stats on the site, and no speed bonus: the browser names its own zone, and the bonus is counted from the zone's deadline.

Site sections:

- **Today.** Anyone can play. After answering, instead of the place there is "answer at 20:00, get it in Telegram" and a button to the bot.
- **Days.** The same page in the Mini App and on the site, opened from the logo. Every day, newest first, with the world's numbers and the viewer's place; a closed day opens its full ranking. An icon on each avatar says where the player came from: Telegram or the site. A day still open somewhere shows only how many have answered and when it closes.
- **Archive.** Past days with the answer shown immediately.
- **Result picture** for forwarding (OG image).

The site is a single game page. An ecosystem landing page is not part of it.

Temporary domain: `maply.ws04.cc`.

### Later: accounts on the site

Planned by the owner on 23 October, not part of the MVP. A site player can sign in with Google or with Telegram, and link the two:

- signing in keeps the site player's id, so past answers and the name stay;
- a Google account linked to Telegram makes the site and the bot one player: one answer a day across both, one place in the ranking, and the streak and stats the site has none of today;
- the cookie id stays the way to play without an account.

Open: whether Telegram on the site is the Login Widget or the OAuth flow, and what happens when a Google account and a Telegram account have both answered the same day before they are linked.

## Where places come from

### Sources

| Source | Key | License | What it provides |
|:--|:--|:--|:--|
| Mapillary | client token, READ only | CC-BY-SA, author credit | Main source: frames at 64 of 68 points, good panoramas at 53 |
| Panoramax | not needed | CC-BY-SA-4.0 | Secondary: strong in Europe, sometimes fills Mapillary gaps (panoramas in Patagonia) |
| "Where am I?" | none | author's rights | Players' photos, only in their groups |

The Mapillary key is in `maply/.env` (`MAPILLARY_TOKEN`), the file is excluded from git.

Mapillary API limits (per the documentation as of 20 October 2026):

- a `bbox` search accepts an area smaller than 0.01 square degrees, `limit` up to 2000;
- rate limits: Entity API 60,000 requests per minute, Search API 10,000 per minute, vector tiles 50,000 per day;
- `mly1_public` tiles at z14 have `id`, `is_pano`, `quality_score`, `captured_at`. So it is convenient to look for candidates by tiles and to fetch the picture (`thumb_2048_url`) through the Entity API.

### Panoramax measurement, 20 October 2026

68 points: 57 cities and 11 rural places on all continents, a window of about 7×7 km around each.

- Photos exist at 51 of 68 points, panoramas at 20.
- Empty: Central Asia (Almaty, Tashkent, Ulaanbaatar), the Middle East and North Africa (Dubai, Cairo), Lagos, Accra, Lima, São Paulo, Kathmandu, Jakarta, most rural points.
- Of 47 random frames, about 31 are fit for the game: a street with architecture, signs, markings. 16 are not: menus, plaques that spell out the place in text, night, blurry frames, a view from a train window.

Random frames: [`panoramax-sample.jpg`](img/panoramax-sample.jpg). The measurement scripts are in `scripts/sources/`.

### Mapillary measurement, 22 October 2026

The same 68 points, 3×3 z14 vector tiles for each (about 7×7 km), 612 tiles in total out of the daily limit of 50,000.

| | Panoramax | Mapillary |
|:--|:--|:--|
| Points with frames | 51 | 64 |
| Points with panoramas | 20 | 54 |
| Points with panoramas of `quality_score ≥ 0.7` | none | 53 |
| Usable frames on review | ~31 of 47 | ~54 of 64 |

- In cities there are from tens of thousands to one and a half million frames per point, and from hundreds to hundreds of thousands of panoramas. Africa, Asia and Latin America are covered: Lima, Bogotá, Jakarta, Hanoi and Manila each give over 100,000 panoramas.
- No panoramas, only regular frames: Almaty, Tashkent, Delhi, Cairo, Lagos, Accra, rural Hokkaido, Kansas, the Outback.
- Nothing at all: Castile, Scotland, Lapland, Patagonia. Remote areas are weak in both sources. In Patagonia Panoramax gave 86 panoramas where Mapillary is empty, so it is worth keeping both sources.
- What spoils frames: organization captions over the image (`OSLO KOMMUNE`), a cropped strip panorama, dusk, a blank wall, an empty field. On almost every panorama the car hood or bicycle handlebar is visible at the bottom.

Random frames: [`mapillary-sample.jpg`](img/mapillary-sample.jpg). For panoramas the sheet shows the cropped center.

Conclusion: Mapillary is the main source, Panoramax the secondary one. For cities there are enough places for years. Hard rounds in remote areas should be assembled along roads where imagery exists, not from random points.

### Curation

There is one round a day, so about 365 places a year are needed. They can be curated by hand:

1. **Candidates.** A script picks random z14 tiles from a weighted list of regions, so there are not ten Frances in a row. Filters: a panorama or a good regular frame, `quality_score ≥ 0.7`, captured in 2018 or later.
2. **LLM scoring.** A model with vision rates "interesting" and "guessable" on a scale of 1–10 and rejects frames with text that names the place, captions over the image, night and blank walls. It also proposes a difficulty, a fact for the result and a detail for the intrigue.
3. **Operator.** In a service chat, each candidate has "yes", "no" and "difficulty" buttons. The queue is filled at least 30 days ahead. If the queue has fewer than 7 days, the bot warns the operator.

The hood and handlebar at the bottom of a frame are no reason to drop a place. For a regular frame the bottom is cropped, and for a panorama the viewer starts with the gaze slightly above the horizon and limits the downward tilt.

Difficulty by weekday: Monday is easy (a recognizable city), Friday is hard (remote areas), weekends are for "Where am I?" and themed rounds.

Russia, Ukraine and Belarus are in the game (decision of 22 October 2026). Excluded: the front line, Crimea and occupied territories, military sites, and frames with recognizable faces. Mapillary blurs faces and plates itself. Whether Panoramax does so has not been checked.

## Architecture

```text
maply (Go, one service on ws04)
  ├─ bot: long polling through tg, round scheduler, intrigues, results
  ├─ HTTP API, site statics and place images (Cloudflare Tunnel, maply.ws04.cc)
  └─ curation: CLI and the operator's service chat

frontend (statics from the same service on ws04): game, archive, OG images
core-postgres: maply schema, identity, language and presence from core
image storage: re-uploaded photos and panoramas without EXIF
```

Main tables of the `maply` schema:

| Table | What it stores |
|:--|:--|
| `place` | source, ID in the source, coordinates, author and license, image key, difficulty, intrigue and result materials, curation status |
| `round` | date, day number, place |
| `guess` | date, `user_id`, point, distance, score, hints, time, where the answer came from |
| `chat_settings` | post time, intrigue, mentions, rival |
| `chat_round` | post `message_id`, intrigue and result times, pin state |

Streaks, seasons and rankings are computed from `guess` and not stored separately.

## Avatar

A map pin: a round head and a tip at 45°, with a round cutout inside and a stencil gap. The mark is built from circles and straight lines at 45°, and the `brand/` rules are met.

Candidate color: forest green `#245A3C`, hue 146°, contrast with white 8.1:1. By hue it is the farthest from the other bots: from branchy's olive (77°) and makeitMD's petrol (185°). It needs to be checked next to them on a dark theme.

## Name

maply. The bot's username is `@maplyin_bot`, confirmed by the owner on 22 October 2026. `@maply_bot` and `@maplybot` are taken.

## Open questions

1. A permanent domain. For now the site temporarily lives at `maply.ws04.cc`. On a move, `/setdomain` in BotFather (Telegram Login), links in results and the app address in Mapillary need to change.
2. ~~Time zone for languages other than ru, uk and be.~~ Decided on 22 October: the chat's own zone, picked by a group admin, with the language's capital as the default.
3. Whether "Voice of the place" is needed at launch. Without TTS six intrigue types remain, which is enough for a week.
