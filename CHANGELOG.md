# Changelog

All notable maply changes are documented here.

The `## <tag>` section of this file *is* the GitHub Release body: the release
workflow copies it verbatim and refuses a tag that has no section. Write it
for whoever has to decide whether to upgrade.

See [`docs/versioning.md`](docs/versioning.md) for what the numbers mean and
[`docs/releases.md`](docs/releases.md) for how a release is published.

## Unreleased

## v0.0.1-alpha.6 - 2026-10-23

The statistics panel turns into a picture: the Earth turns slowly under the
person's pins of every day, beside their numbers and places in the world.

Operators: migration `009_profile_card.sql` runs on start with
`AUTO_MIGRATE=true`. The bot now draws pictures in the background every 10
minutes for recent players; `ANIMATIONS=false` turns them off with the
animated posts.

### Added

- The statistics panel shows a picture above the numbers: the Earth turns
  slowly under the person's pins of every day, beside their points, days
  played, streak, typical miss, best day, place in the world and the strip
  of their last days. It is drawn once for the numbers it shows and kept by
  Telegram's `file_id`; pictures are drawn ahead in the background for
  everyone who played in the last days, so the button rarely waits. Back from
  the picture sends the panel again, since a picture cannot turn into text.
  Migration `009_profile_card.sql` keeps the `file_id`s.

## v0.0.1-alpha.5 - 2026-10-23

Sharing the result from the Mini App now forwards the animation the chat
saw, not a still card.

Operators: migration `008_result_animation.sql` runs on start with
`AUTO_MIGRATE=true`. Nothing is needed in `.env`.

### Changed

- Sharing the result from the Mini App sends the same animation the chat's
  result went out as, instead of a still card. A result that went out as a
  picture is still shared as the map card. Migration `008_result_animation.sql`
  keeps the animation's `file_id` with the chat's result.

## v0.0.1-alpha.4 - 2026-10-23

The day's posts move: the morning post opens the day's panorama as a little
planet and unrolls it into the street, the intrigue shows the time there on
a dial of the day, and the result flies to the group's pins and ends on a
podium. Players are ranked against the whole world, once a day has closed
everywhere, and a days page shows every closed day's full ranking, site
players included. An early answer earns a small speed bonus.

Operators: migrations `006_speed.sql` and `007_world.sql` run on start with
`AUTO_MIGRATE=true`, as on WS04. The image now carries ffmpeg; nothing is
needed in `.env`, and `ANIMATIONS=false` sends photos and text as before.

### Added

- A speed bonus: the earlier the answer, the more it adds. +250 when the
  morning post goes out, +100 by 16:00, and at most +50 once the afternoon
  intrigue has started. It is small next to the 5,000 for the pin, so it
  breaks near-ties rather than deciding the day. The result screen shows it
  as a gold part of the bar, and the Help panel of the menu explains it. Migration
  `006_speed.sql` adds the column; answers already scored keep their points.
- A world ranking: everyone who answered a day, from a group, a DM or the
  site, ranked one by one. Groups are not ranked against each other. A day
  is final only after the last zone's reveal (06:00 Kyiv), so a chat hears
  about it once, the day after: the morning post says how many played, who
  came first and how close the day came, with your own place in a DM and the
  members' places in a group. Chats far east, whose morning comes before the
  day closes, get the same lines with that evening's result.
- A days page in the Mini App and on the site, opened from the logo: every
  day with its world numbers and your place, and each closed day's full
  ranking. An icon on each avatar says whether the player came from
  Telegram or the site.
- Site players are ranked. The browser keeps a random id in a cookie, the
  answer is stored, and the player can give a name or stay "Guest". There
  are no streaks or personal stats on the site, and no speed bonus, since
  the browser names its own time zone. Migration `007_world.sql` adds
  `site_player`, `site_guess` and the `day_score` view.
- Animated posts. The morning post opens the day's panorama as a little
  planet around the logo's pin and unrolls it into the street; the intrigue
  that tells the time there shows it on a dial of the day, dark or light as
  it is there; the result flies to the group's pins and puts the three
  closest on a podium. Each is a seamless loop that Telegram plays as a GIF.
  They are drawn in Go and encoded by ffmpeg in a few seconds, with no
  browser. The morning post is drawn once a day and reused in every chat. A
  photo place, a failed drawing or Telegram refusing the file sends the
  photo, the map card or the text as before. Names and places in scripts
  the brand's fonts lack are written in English or left out of the picture;
  the caption keeps them.

### Changed

- The group's 20:00 result says how many of the group answered, and the
  place and its fact are set apart in a quote, as in a forwarded card.
- The image now carries ffmpeg. `ANIMATIONS=false` in
  `.env` sends photos and text as before; `FFMPEG` names another binary.

## v0.0.1-alpha.3 - 2026-10-23

Fixes the Mini App stopping on the loading screen after alpha.2.

### Fixed

- The Mini App no longer mixes scripts from two versions. Cloudflare caches
  `.js` and `.css` and tells browsers to keep them for four hours whatever
  the service says, so after alpha.2 a browser that had opened maply before
  ran the new `mapstyle.js` next to its old `map.js` and stopped on the
  loading screen. `index.html`, which Cloudflare does not cache, now names
  each of the app's own scripts and stylesheets with its hash.

## v0.0.1-alpha.2 - 2026-10-23

The result at 20:00 becomes a map with everyone's pins, and it can be
forwarded into another chat as a card. Panoramas stop giving the place away
from below, and the Mini App opens on maply's new halftone globe.

Operators: the stack gains a second container, `card-renderer`
(`chromedp/headless-shell`, pinned by digest), on a new private network
`maply_cards`. `docker compose` creates both on deploy; nothing is needed in
`.env`. Set `CARD_RENDERER_URL=` (empty) there to turn the cards off and send
the photo as before. The place images were re-encoded with the nadir masked
and moved to new keys: load the new `images/` and update `place.image_key`
before this version serves them.

### Added

- The 20:00 result is a map. A card shows the true point, every player's pin
  with their initial, a dashed line from each pin to the point, `maply #N`,
  the place and the top three, in the chat's language and the Mini App's
  colours and fonts. The service serves the card as the page
  `/card/result/<chat>/<date>`; a headless Chromium in its own container
  (`chromedp/headless-shell`, pinned by digest) opens it over the DevTools
  protocol and takes a 1280x1280 screenshot. The page opens only with a link
  signed for that chat and date, and only after the chat's reveal. When the
  renderer is down, slow or wrong, or Telegram refuses the picture, the result
  goes out with the place's photo as before. Alone in a DM the person gets
  their own pin; in a group, everyone's.
- "Forward to a chat" sends that card through `shareMessage`: the server
  prepares it with `savePreparedInlineMessage` (`POST /api/share`, five a
  minute per person), serves it as a JPEG under a random key at
  `/card/img/`, and the Mini App opens Telegram's chat picker. Telegram
  clients older than 8.0, and any failure to prepare the card, keep the
  `t.me/share` line.
- `CARD_RENDERER_URL` and `CARD_PAGE_URL`. The WS04 stack runs the renderer
  next to the bot on a private `cards` network and sets both; an empty
  `CARD_RENDERER_URL` in `.env` turns cards and card sharing off.

### Fixed

- A panorama no longer gives its place away straight down. A 360 camera sees
  its own mount there, and capture projects stick their logo over it: the
  first day's frame read "Revolutionary Government of Zanzibar".
  `maply-places` now paints everything below -45° over with the colours
  around it and gives the frame a new key, since `/img/` is served
  immutable. One Oslo frame, whose city sticker reaches higher, was taken
  out of the queue.

### Changed

- The loading screen is maply's new mark: a globe of white halftone dots
  turning on its tilted axis, the pin hopping on it and the dots rippling
  where it lands. It is drawn on a canvas from `land.js`, 700 bytes saying
  which dots are land, and starts before MapLibre and Pannellum, which are
  now deferred.
- In a Telegram window with its own title bar, which already says "maply",
  the header drops the wordmark and is lower: the day on the left, the
  timer on the right. Fullscreen keeps the full header.

### Known Limitations

- The card is drawn under software WebGL on ws04; one card takes a few
  seconds, and the scheduler draws them one at a time. Many chats revealing
  in the same minute get their results late, not lost.
- A forwarded card shows the place. Sent into a chat whose 20:00 has not
  come yet, it gives the answer away there; the old `t.me/share` line named
  no place.
- Prepared share cards live in memory: a restart between preparing and
  sending one makes Telegram show it without a picture.
- There is no operator chat yet (#50), no warm-up for a group that adds
  maply late in the day (#57), and real Telegram clients are not yet
  checked (#69).

## v0.0.1-alpha.1 - 2026-10-23

The first cut that plays a whole day on its own. @maplyin_bot posts the
day's place every morning in each chat's zone, takes one answer per person
through the Mini App or a location, teases between 16:00 and 18:00, and
sends the result with the group's table at 20:00. The same game runs on
https://maply.ws04.cc in a browser without Telegram, unranked. 118 places
are prepared: about four months of queue.

To run it, the host needs `core` migration 018 (the `maply_core` role and
the `maply` schema), `MAPLY_CORE_PASSWORD` and `TELEGRAM_BOT_TOKEN` in the
stack's `.env`, the place images in `./images` next to the compose file, and
the hostname routed through the Cloudflare Tunnel. Load the places with the
`places.sql` that `cmd/maply-places` writes. The Mini App itself is
registered in BotFather (`/newapp`, short name `play`).

### Added

- `cmd/maply-places` turns a batch's kept candidates into approved places:
  the frame re-encoded without EXIF under a random key, the city the curator
  named with its outline from Nominatim when the outline contains the point,
  the heading from Mapillary, the country and continent from the same borders
  scoring uses, and the curator's city names and facts in all sixteen
  languages. It writes the images and a `places.sql` that adds nothing on a
  rerun.

- The Mini App plays in a plain browser too, as the site: nobody signs in,
  the answer stays in the browser's `localStorage`, nothing is ranked. The
  place and the score open only after the latest reveal among the zones maply
  offers (20:00 in Los Angeles), and a browser claiming a zone ahead of all of
  them still gets no date that has not begun in one. A plate links to the bot.
- A group admin moves the morning post by an hour or a quarter (05:00–19:00)
  and turns the intrigue and the streak mentions on or off under Settings. A
  post that has not gone out yet follows the new time the same day.
- A location sent to maply in a DM is the day's answer, under the Mini App's
  rules: one a day, refused after the deadline. Help says so.
- Blocking maply stops the day in that DM; unblocking or writing to it starts
  it again. The bot's profile description and short description are set in
  every language at start, and written only when they changed.
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

### Known Limitations

- The 20:00 result is the place's photo with the table in the caption, not
  yet the rendered map card (#65).
- Sharing from the Mini App opens `t.me/share` with a line of text; the card
  through `shareMessage` comes with the renderer (#68).
- There is no operator chat yet (#50): places are approved in `curation/`
  and loaded by hand, and nothing warns when the queue runs low.
- A group that adds maply gets today's post right away when it is before
  20:00 there, instead of the warm-up on yesterday's place the plan
  describes (#57).
- Not yet checked on real Telegram clients (#69).
