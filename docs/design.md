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

The score has two parts, 5,000 at most:

- **distance:** `3500 · e^(−d / 1492.7)`, where `d` is the distance in kilometers. The curve is the same as GeoGuessr's for the world map: 500 km gives about 2,500, 2,000 km about 900;
- **bonuses** for the pin landing on the same continent (+300), in the same country (+500) and in the same city (+700). Bonuses form a ladder: the country counts only together with the continent, the city only together with the country.

Bonuses make a correctly guessed country visible: a pin right at a border but on the wrong side gets 800 points less. The server determines country and continent from Natural Earth polygons, and the city from the OSM administrative boundary. On the result screen the strip under the header shows these parts separately, and the total builds up together with it.

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
- a table: distance, score, streak;
- the chat's rank among all chats of the day.

The cross-chat ranking uses the median of the chat's players, if at least three answered, so large chats get no advantage. It is published only after the last reveal across all time zones. Until then the result says "chat rank will be at 21:00".

A line for forwarding without spoilers: `maply #142 · 312 km · 4,057 · 🔥7`.

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
- **Website in a browser.** One can play anonymously or sign in through Telegram Login to keep a streak. Anonymous answers do not go into rankings.

Site sections:

- **Today.** Anyone can play. After answering, instead of the place there is "answer at 20:00, get it in Telegram" and a button to the bot.
- **Archive.** Past days with the answer shown immediately.
- **Result picture** for forwarding (OG image).

The site is a single game page. An ecosystem landing page is not part of it.

Temporary domain: `maply.ws04.cc`.

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
