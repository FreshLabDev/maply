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
