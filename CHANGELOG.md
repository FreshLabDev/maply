# Changelog

All notable maply changes are documented here.

The `## <tag>` section of this file *is* the GitHub Release body: the release
workflow copies it verbatim and refuses a tag that has no section. Write it
for whoever has to decide whether to upgrade.

See [`docs/versioning.md`](docs/versioning.md) for what the numbers mean and
[`docs/releases.md`](docs/releases.md) for how a release is published.

## Unreleased

Nothing is released yet. The repository holds the design, the MVP plan, the
source measurements, the clickable prototype of the Mini App and the skeleton
of the Go service.

### Added

- The day runs by itself (`internal/schedule`, `005_schedule.sql`): every
  chat gets the morning post with the photo, an intrigue between 16:00 and
  18:00 and the result at 20:00, each in its own zone. Sends are rows in the
  job queue keyed by kind, date and chat, so a restart or a second instance
  sends nothing twice; a test runs a whole day on a fast clock with a restart
  at noon to prove it. The group post is pinned silently and its
  who-answered line is redrawn at most once a minute. A chat that blocked or
  removed maply stops getting the day. `SCHEDULE=false` turns it off.
- Every line maply shows lives in `internal/i18n`, in the family's sixteen
  languages: the same picker, codes and fallback to English as voicy and
  branchy. A test fails on a key missing in any language or a placeholder that
  differs between them. The language is read from and written to `core`, so a
  choice made in any bot of the family holds in maply.
- Each chat has its own time zone (`004_chat_zone.sql`). A group admin picks
  it from sixteen zones, a person picks their DM's; until then the reveal
  follows the capital of the language, with Russian and unknown languages on
  Kyiv as in quoto. The binary embeds tzdata. The answer window still closes at
  the earliest reveal among a person's chats. `chat_settings.lang` is dropped:
  a group's language is core's, never a member's, so every member of a group
  gets the same reveal.
- The Mini App holds no words of its own. `/texts/<lang>.json` carries its
  strings and the zone names from `internal/i18n`; the app starts in the
  client's language and switches to the person's choice from core when
  `/api/today` names it (`lang`). Map labels follow the language, and country
  labels come from the browser's names for the ISO code, as on the result.
  Arabic and Hebrew labels are shaped by MapLibre's RTL text plugin
  (`mapbox-gl-rtl-text` 0.3.0, loaded only when such a label first appears;
  the CSP allows `'wasm-unsafe-eval'` for its ICU and still no `eval`). Sofia
  Sans carries its Latin Extended subset, so Polish, Czech, Turkish and
  Catalan letters no longer fall back to the system font.
  Curated city names and facts fall back to English for a language the
  curators have not written yet.
- The bot's menus repeat voicy's and quoto's: one panel shape (bold title,
  italic hint, quoted body), `◉`/`◎` on a choice with the current one green,
  Back to home, Close only in a group. `/start` in a DM opens Play (the Mini
  App, from the new optional `APP_URL`), the shared language picker with
  Follow Telegram, stats, settings, help and about. In a group, Settings shows
  only to admins and every press under it checks the rights again; there an
  admin picks the group's zone and language. Adding maply to a group fixes its
  zone from the group's language, or the adder's while the group has none; a
  removed group is marked inactive and keeps its zone. `/start` gets a
  description in all sixteen languages.
- The Go service skeleton, built the way voicy and branchy are: validated
  environment, PostgreSQL in the `maply` schema with migrations embedded in the
  binary, Telegram long polling through `FreshLabDev/tg` that records people
  and groups in `core`, a `Preflight` check before polling starts, and
  `/healthz` reporting the database and polling freshness.
- The game schema (`002_game.sql`): `place` with its curation status,
  `round` (one place per date), `guess` (one answer per person per day, with
  the deadline it was accepted under), `chat_settings` for groups and DMs
  alike, `chat_round` for what a chat has already been sent, and `job`, the
  send queue keyed for idempotency. Constraints refuse an answer after its
  deadline, an approved place without the data a round needs, and a morning
  post moved past the 20:00 reveal.
- Scoring: haversine distance, `3500 · e^(−d / 1492.7)`, and the bonus ladder
  for continent, country and city. Point-in-polygon handles shared borders,
  enclaves, rings across the antimeridian and polar caps. The right country
  counts as the right continent too, so a pin in Paris for a place in French
  Guiana earns the country bonus.
- The Mini App in `web/app/`, split from the prototype into `index.html`,
  `app.css`, `tg.js`, `map.js` and `app.js` and served by the binary at `/`.
  MapLibre GL 5.6.0, Pannellum 2.5.6 and Sofia Sans come from the service
  instead of unpkg, jsdelivr and Google Fonts, which are unreliable from
  Russia. A Content-Security-Policy lets the page reach only telegram.org and
  the tile server.
- The Mini App plays a real day: it sends `initData` with every request,
  restores a pin already sent, counts down to the person's deadline and then to
  the chat's reveal, and draws the result, the bonuses and the chat's table
  from the server. The demo round, the made-up friends and the browser's
  Nominatim lookup are gone. The photo's author is credited only after the
  reveal, because a Mapillary profile lists where its owner takes pictures.
- `place.heading` (migration `003`): where the frame centre points, for the
  compass.
- Country borders embedded in the binary: Natural Earth 1:10m admin-0 from the
  Ukrainian point of view, so Crimea and Sevastopol are Ukraine. Borders are
  simplified to about 220 m, which moves one land point in 90,000 to another
  country. Each polygon carries its own continent: Russia is split at the
  Urals, and France's overseas regions, Hawaii, the Caribbean Netherlands and
  Istanbul's European side sit where they are on the map.
- The game calendar: the round of a date, created from the place queue by
  weekday difficulty and numbered from the first round; the reveal at 20:00 in
  the capital of the chat's language; and a person's answer window, which
  closes at the earliest reveal among every chat core has seen them in with
  maply. Zones come from tzdata.
- `initData` verification (HMAC-SHA256 under `HMAC("WebAppData", token)`,
  `auth_date` at most a day old, repeated fields refused), a signed `startapp`
  payload for group posts (date and chat under a 16-byte HMAC, 36 characters),
  and a group membership check through `getChatMember`, cached per day.
- The game API: `GET /api/today` (the picture, the day number, the frame's
  compass heading, the zone of the chat's language, the person's deadline and
  their own pin), `POST /api/guess` (one answer a day, answered
  with "accepted" and nothing else), `GET /api/result` (the place, the fact and
  the chat's table, only after that chat's reveal; a DM waits for the latest
  reveal among the person's chats), and `GET /img/<key>`. Requests are
  rate-limited by person and by address. An integration test plays a winter
  day across Kyiv, Moscow and Minsk and checks that no response and no log
  line before the reveal says where the place is.
- `internal/telegram`: the Bot API methods `tg` does not type yet, through
  `Client.Call`: photos with a `web_app` button (by `file_id`, or uploaded under
  a fixed name with the buttons added in an edit), silent pin and unpin,
  `setChatMenuButton` and `savePreparedInlineMessage`.
- CI (modules, gofmt, vet, race tests, database tests, govulncheck, image build,
  Compose validation), the release workflow that publishes the image to GHCR,
  Dependabot, a local `docker-compose.yml` and the WS04 stack.

- `docs/design.md`: the daily cycle, scoring, anti-cheat rules, the intrigue
  window, groups, the site, where places come from and how they are picked.
- `docs/mvp-plan.md`: what is in the MVP, where it departs from the design,
  stages with a check for each, risks and open questions.
- `web/prototype/`: the Mini App as one HTML file. A panorama or photo on top, a
  globe that opens into a map, a pin, the waiting screen and the day's result
  with a leaderboard. Inside Telegram it uses the Mini App API: brand colours
  for the header, background and bottom bar, vertical swipes off, the system
  Back button from the map to the photo, haptics, a closing confirmation while
  a pin is placed but not sent, safe areas, and sharing through `t.me/share`.
- `scripts/sources/`: coverage measurements of Panoramax and Mapillary at 68
  points on every continent.
- `scripts/curation/`: candidate harvest from Mapillary vector tiles with
  weighted regions.
