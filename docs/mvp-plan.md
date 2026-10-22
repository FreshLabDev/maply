# maply: MVP plan

Draft of 22 October 2026. Based on [design.md](design.md) and the prototype [web/prototype/index.html](../web/prototype/index.html).

The MVP is done when, in one Russian, one Ukrainian and one Belarusian group, a full day runs for a week in a row without manual intervention: the post arrives at 07:00, players answer through the Mini App, the intrigue arrives between 16:00 and 18:00, the result arrives at 20:00, and no place leaks ahead of time.

## In scope and deferred

| In the MVP | After the MVP |
|:--|:--|
| Daily round in the family's sixteen languages, in each chat's own time zone | "Where am I?" |
| DMs and groups, pinned post with marks for who has answered | `/rival` and the "Rival" intrigue |
| Mini App: photo or panorama, pin, waiting, result | Seasons and champion of the month |
| Scoring: distance plus bonuses for continent, country and city | "Continent" and "second angle" hints |
| Anti-cheat, items 1–5 from design.md | "Voice of the place" (TTS) |
| Intrigue: "Right now there", "Detail", "Chat spread", "World vs you", "Streak" | Archive on the site, OG image |
| Result in the group and in the DM, cross-chat ranking | Telegram Login on the site |
| Site in the browser: anonymous play without a ranking | Curated city names and facts beyond ru, uk, be and en |
| Place curation: harvest, LLM scoring, operator service chat | |

Hints are deferred (owner decision of 22 Oct): without them scoring and anti-cheat are simpler, and at launch the "clean" mark can simply be given for answering before the intrigue. The archive and the OG image should be built right after launch: they are the only way to bring in people from outside Telegram.

## Technical decisions

1. **Result in the group: a map image drawn by our own frontend.** Owner decision of 22 Oct: the result is sent as a nicely drawn map image, as in design.md. There is no need to draw it with a separate server-side MapLibre renderer. The service serves an internal card page `/card/result/<chat>/<date>` styled like the result screen: a map with the real point, pins and player avatars, a dashed line from each pin to the point, a `maply #142` header, the place and the top three. A 1280×1280 snapshot is taken by headless Chromium in a separate container next to the service (over CDP via `chromedp`), reachable only from the service; it loads nothing but the card page and the map tiles. This way the card looks the same as the Mini App, and the same renderer later draws the OG images. If the renderer does not respond within 20 seconds, a fallback result is sent: the photo of the place and a text table, so the result is not lost. Under the image there is an "Open result" button that opens the result screen in the Mini App.
2. **The city for the bonus is determined without requests during play.** In the prototype the bonus is computed by Nominatim from the browser. That does not work for the server: the Nominatim limit is 1 request per second, and answers arrive in bursts. We need the boundary of only one city, the one where the place of the day is. So at curation time we store its polygon (`polygon_geojson` from Nominatim) in `place`. The country and continent come from Natural Earth admin-0, the file lives inside the service. Scores are then computed locally with point-in-polygon, with no external calls.
3. **Panorama viewer: Pannellum, not Photo Sphere Viewer.** The prototype, the compass and the tilt limit are already set up on Pannellum, and the library is noticeably lighter than PSV with three.js. The downside of Pannellum is that it is rarely updated. If it breaks on some device, we will switch to PSV. Anti-cheat item 5 does not depend on the choice of viewer.
4. **The result in the Mini App shows the table of the chat the player came from.** The chat is taken from the signed payload. If the Mini App is opened from a DM, we show the player's own row and the overall stats of the day.

## Stages

Each stage has a "Done when" check after which the stage counts as closed. Stages 1 and 2 are independent of each other and can run in parallel.

### 0. Repository and registration

- The repository [`FreshLabDev/maply`](https://github.com/FreshLabDev/maply) was created on 22 Oct, license Apache-2.0, tasks are on the [project board](https://github.com/orgs/FreshLabDev/projects/2). The layout matches voicy and branchy and is described in [architecture.md](architecture.md): `cmd/maply`, `internal/…`, `migrations`, `deploy/ws04`, CI that publishes the image to GHCR.
- `.gitignore` already has `.env` and `curation/`. Add the harvest dumps and the image cache.
- BotFather:
  - create the bot `@maplyin_bot` (name approved on 22 Oct);
  - create the Mini App with `/newapp` and the short name `play`, so that the link `t.me/maplyin_bot/play?startapp=…` works;
  - set the menu button and the description.
  The token goes into `.env` on ws04 and does not enter the repository.

Done when: the link `t.me/maplyin_bot/play` opens a static placeholder on `maply.ws04.cc`.

### 1. Data schema and the migration in core

- Add the migration `0NN_maply.sql` to `core`:
  - the role `maply_core`;
  - the bot `maply` in `core.bot`;
  - grants to read identity, presence and `effective_language`.
  As in architecture.md, `PUBLIC` is revoked in the same migration, and the `core/tests` tests check both sides.
- The `maply` schema is managed by the service itself, as in voicy and branchy. I refine the tables from design.md as follows:

| Table | Fields beyond design.md |
|:--|:--|
| `place` | `city_poly` (geojson), `country_iso`, `continent`, `fact`, `detail_crop`, `difficulty`, `status`, `reviewed_by` |
| `round` | `day_no`, `place_id`, `date` (unique) |
| `guess` | `context` (dm, group, site), `chat_id`, `answered_at`, `deadline_at`, `km`, `pts_km`, `pts_bonus`, `clean`. Unique on `(date, user_id)` |
| `chat_settings` | `kind` (dm, group), `lang`, `post_time`, `intrigue`, `mentions`, `active`. DMs live here too, so the scheduler treats every chat alike |
| `chat_round` | `post_msg_id`, `pinned`, `intrigue_at`, `intrigue_sent`, `result_msg_id`. The scheduler uses this table to know what has already been done |
| `job` | queue of scheduled sends with an idempotency key, so that nothing goes out twice after a restart |

Done when: the migrations apply on a test Postgres. The role test proves that `maply_core` does not write to `core.*` directly.

### 2. Place curation

The scripts already exist: `scripts/curation/harvest.py`, `regions.py`, and the measurements in `scripts/sources/`.

- Finish harvest:
  - first find out why the previous run had no candidates from Ukraine;
  - add a filter for excluded zones: the front line, Crimea, occupied territories. The polygons are kept in the repository and updated by hand.
- LLM scoring per design.md. A cheap model, run in batches.
- When a candidate is approved:
  - download the frame (panoramas as full equirectangular);
  - strip EXIF;
  - save under a random name;
  - get the city polygon from Nominatim;
  - prepare the fact and the crop for "Detail".
- Operator service chat in the bot:
  - under a candidate, the buttons "yes", "no" and "difficulty";
  - the bot warns if the queue has fewer than 7 days.
  At the start a CLI import of the already selected `batch-001` and `batch-002` is enough, and the buttons in the chat can be a second step.

Done when: the queue has 30 places, and each has an image without EXIF, a city polygon, a country, a continent and a fact. No file and no API response contains a Mapillary ID.

### 3. Game server (Go)

Packages (layout in [architecture.md](architecture.md)):

- `round`: which place is today's, the day number, the reveal time for a time zone.
- `deadline`: a person's answer window closes at the earliest reveal among their contexts (the DM and the groups from `core.presence`). Time zones come from tzdata, offsets are not hard-coded. Tests cover winter Kyiv and a change of daylight saving rules.
- `scoring`: haversine, `3500·e^(−d/1492.7)`, the bonus ladder, point-in-polygon over Natural Earth and `city_poly`. Tests: a point right at the border, the antimeridian, the poles.
- `auth`:
  - `initData` check: HMAC-SHA256 with the key `HMAC("WebAppData", token)`, `auth_date` no older than a day;
  - `startapp` payload: date, `chat_id` and an HMAC truncated to 16 bytes, in base64url, up to 64 characters;
  - chat membership is checked with `getChatMember` and cached for a day.
- HTTP API (same process, static files from the same address, no CORS needed):
  - `GET /api/today`: the picture of the day, the day number, the reveal time, my answer if there is one already;
  - `POST /api/guess`: accepts a point and is called once per day. Before the reveal it answers only "accepted". We do not return the distance before the reveal, otherwise the place can be triangulated from it;
  - `GET /api/result?chat=…`: works only after the reveal; returns the place, the fact and the chat table;
  - `GET /img/<key>`: images without EXIF with a long cache.
- Rate limit on `/api/guess` by `user_id` and IP.

Done when: an integration test runs a day on a test database with an accelerated clock. Three players from different time zones answer, and no API response reveals the place's coordinates before the deadline.

### 4. Bot and scheduler

For the bot we use the `tg` library. What it lacks (checked on 22 Oct):

- the `web_app` field of `InlineKeyboardButton`;
- `pinChatMessage` and `unpinChatMessage`;
- `setChatMenuButton`;
- `savePreparedInlineMessage`.

maply calls them through `Client.Call` right away. Typed methods will be added to `tg` in a separate PR, and a `tg` release is made only with separate permission.

- `/start` in a group:
  - settings with buttons, as in voicy and branchy;
  - a warm-up on yesterday's place with the answer the same evening;
  - one message to the admins if the bot has no right to pin.
- 07:00 in the chat's time zone, or at the time the group chose:
  - a post with the photo and a button;
  - in a DM a `web_app` button, in a group the link `t.me/maplyin_bot/play?startapp=…`;
  - a silent pin.
- Editing the pin "who has answered": at most once a minute per chat, so as not to hit Telegram limits.
- Intrigue:
  - the time is chosen at random in the 16:00–18:00 window and written to `chat_round` in advance;
  - the type is chosen from five, with the rule "not two days in a row";
  - if everyone has answered, a separate text is sent.
  "Right now there" takes the weather from Open-Meteo at the moment of sending.
- 20:00:
  - unpin;
  - send the result: the map image (stage 5, "Result card"), with the table and the chat rank in the caption, or "the chat rank will be at 21:00";
  - the "Open result" button;
  - if the renderer did not respond, the fallback result: the photo of the place and a text table.
- In a DM the result arrives only if the person is not in any group with maply. Otherwise they get their own row and a link to the group result.
- Fallback way to answer: send a location to the bot's DM.

Done when: on a test group with an accelerated schedule (a day passes in 15 minutes) the post, pin, intrigue and result each arrive exactly once, including after a container restart in the middle of the day.

### 5. Frontend from the prototype

The prototype already works as a Mini App:

- `ready` and `expand`;
- header, background and bottom bar colors;
- vertical swipes disabled;
- the system "Back" button from the map to the photo;
- haptic feedback;
- closing confirmation while the pin is placed and the answer is not sent;
- safe area and content safe area;
- external links through `openLink`;
- "Forward to chat" through `t.me/share`.

All of this has been checked in a browser emulating Telegram, on 375×812 and 320×568 screens. It has not been checked on a live client yet.

What remains:

- Split the file into `index.html`, `app.css`, `app.js`, `tg.js` and `map.js` without a bundler. The service serves them through `embed`.
- Download MapLibre, Pannellum and the Sofia Sans fonts locally. Right now they come from unpkg, jsdelivr and Google Fonts, and those addresses are sometimes reachable from Russia and sometimes not. Only `telegram-web-app.js` and the tiles stay external.
- Replace the demo data (`ROUND`, friends, `?state=reveal`) with API requests. Send `initData` in a header.
- Take the bonuses on the result screen from the server and remove the Nominatim request from the browser.
- Texts in every language of the family, the jokes on the loading screen included.
- In a browser without Telegram:
  - the answer is stored in `localStorage`;
  - the result is shown after the latest reveal;
  - there is a "get it in Telegram" button.
- Result card: the page `/card/result/<chat>/<date>` behind a signed link, internal network only, and a container with headless Chromium on ws04. The fonts and the map style are the same as in the Mini App; the card text is in the chat's language.
- "Forward to chat" through `shareMessage` (Bot API 8.0). The server prepares the message through `savePreparedInlineMessage`, and the chat gets a card with a "Play" button instead of a bare link. Currently `t.me/share` is used, and it stays as the fallback.

Done when: a full day runs in Telegram on iOS, Android and Desktop. On an iPhone with a notch the header does not slide under the Telegram buttons.

### 6. Deploy to ws04

Everything in this stage is done only after separate permission.

- Image in GHCR, compose in `deploy/ws04` modeled on voicy (image by digest, `maply_core` on the shared `core-postgres`).
- Cloudflare Tunnel to `maply.ws04.cc`, a volume for images, volume backup together with the database.
- Telegram endpoint: `api.telegram.org`. The bot sends only photos up to 10 MB, so by the rule in architecture.md it does not need its own Bot API server.

Done when: `/healthz` responds and a trial day in a closed group has passed.

### 7. Closed launch

A week in our three own groups (ru, uk, be), then open the bot. What we watch every day:

- how many of the people who opened the Mini App answered;
- the share of answers after the intrigue;
- complaints of "recognized the place by a sign";
- the stock of places in the queue.

## Risks

| Risk | What we do |
|:--|:--|
| OpenFreeMap tiles are slow or unavailable from Russia | Check from a Russian IP before launch. Plan B: PMTiles (Protomaps) on ws04, extract up to z14 |
| The place is found by image search | Accepted, as written in design.md. Curation filters out frames with signs |
| Coordinates leak through the API or a file name | The test from stage 3 and random image keys. The service logs do not contain the place's coordinates |
| Mapillary changes limits or terms | Panoramax as a second source, a queue 30 days ahead |
| The group has no right to pin | The game works without the pin, the marks for who has answered go into the result |

## Owner decisions

- 22 Oct: the repository is public, Apache-2.0; tasks are kept on the public GitHub project board. Everything public (board, issues, documents, code comments) is written in English, while the game interface stays in ru, uk and be.
- 22 Oct: maply speaks all sixteen languages of the family from the start. The result opens at 20:00 in the chat's own zone: a group admin picks it, and until then it follows the capital of the language. Russian goes by Kyiv, as in quoto.
- 22 Oct: hints and Telegram login on the site are deferred until after the MVP.
- 22 Oct: the 20:00 result arrives as a nicely drawn map image (decision 1).
- 22 Oct: the bot name is `@maplyin_bot`.
