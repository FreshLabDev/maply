# Architecture

maply will be one Go service built the way voicy and branchy are: Telegram long
polling through the family's `tg` client, PostgreSQL in the shared
`core-postgres` as its only durable store, and a small HTTP surface. What sets it
apart is that the HTTP surface is real: the same process serves the Mini App,
its API and the place images.

The service is a skeleton so far. This document fixes where things go, so each
piece lands in the right place. The product rules are in
[`design.md`](design.md) and the order of work in [`mvp-plan.md`](mvp-plan.md).

## Boundaries

- Domain tables live in the isolated `maply` schema.
- Shared Telegram identity, presence and language live in `core`. maply
  connects as `maply_core`, can reference `core.person` and `core.chat`, and can
  execute only `core.touch`, `core.set_language`, `core.clear_language` and
  `core.effective_language`. The role and its grants are a migration in the
  `core` repository; maply's own migrations never touch `core`.
- `core.presence` is not just bookkeeping here: it is how the answer window
  finds every chat a person belongs to.
- Telegram goes through `github.com/FreshLabDev/tg`. There is no SDK and no
  webhook mode. Methods `tg` does not type yet go through `Client.Call` inside
  `internal/telegram`, and move into `tg` later.
- The bot talks to `api.telegram.org`. It sends photos well under the cloud
  limits and downloads nothing large, so the self-hosted Bot API server buys it
  nothing.
- The Mini App and the site are one frontend, embedded in the binary. Nothing in
  it comes from a CDN except `telegram-web-app.js` and the map tiles.
- The 20:00 result is a picture of the map. The service serves a card page in
  the Mini App's style, and a headless Chromium container next to it takes the
  screenshot over CDP. The two share a private `cards` network; the container
  is not on `core_net` or `management` and publishes nothing, but it reaches the
  internet for the map tiles. The page opens only with a link signed for the
  chat and date (version 2 of the startapp payload layout, so a group post's
  payload does not open it) and only after the chat's reveal. If the renderer
  does not answer, the result goes out with the place's photo.
- Sharing from the Mini App goes through `shareMessage`. `POST /api/share`
  renders the same card, keeps it in memory as a JPEG under a random key for
  two hours, and saves a prepared message pointing at
  `APP_URL/card/img/<key>.jpg`; the app passes the returned id to Telegram.
- Place images are files on a volume, named by a random key and stripped of
  EXIF. The database stores the key, never a path a client could guess.

## Repository Layout

```text
maply/
├── cmd/maply/            wiring: config, preflight, bot, scheduler, HTTP server, shutdown
├── internal/
│   ├── config/           validated environment; the secrets list for log redaction
│   ├── db/               pgx pool, embedded migrations, search_path=maply, core.touch
│   ├── telegram/         adapter over tg: web_app button, pin, setChatMenuButton,
│   │                     savePreparedInlineMessage until they exist in tg
│   ├── bot/              updates, /start, menus and settings, operator chat
│   ├── round/            place of the day, day number, reveal time per zone,
│   │                     the answer window across a person's chats
│   ├── scoring/          haversine, distance curve, bonus ladder
│   ├── geo/              Natural Earth admin-0 (embedded), point in polygon
│   ├── schedule/         job queue: morning post, pin edits, intrigue, result
│   ├── intrigue/         the five intrigue types and their rotation
│   ├── card/             the card page and its assets, the Chromium client, the
│   │                     shared images, the prepared message for shareMessage
│   ├── auth/             initData verification, signed startapp payload
│   ├── api/              /api/today, /api/guess, /api/result, /api/share, /img/
│   ├── images/           rehosting store, EXIF stripping, random keys
│   ├── places/           queue, import of curated batches, low-queue warning
│   ├── i18n/             translations.json: the family's sixteen languages
│   └── health/           /healthz: database, polling, queue depth
├── migrations/           NNN_name.sql, applied at start, recorded in schema_migrations
├── web/                  package web: embeds app/ and serves it with a CSP, and
│                         /texts/<lang>.json from internal/i18n
│   ├── app/              the Mini App served by the binary: index.html, app.css,
│   │                     fonts.css, tg.js, mapstyle.js (the map's palette and
│   │                     style, shared with the card), map.js, app.js, vendor/
│   └── prototype/        the single-file prototype, kept as the reference
├── scripts/
│   ├── curation/         harvest.py, regions.py: candidates from Mapillary
│   └── sources/          coverage measurements of Mapillary and Panoramax
├── deploy/
│   ├── core-init.sql     minimal core schema so local compose boots
│   └── ws04/             compose.yaml for production (service and renderer),
│                         compose.test.yaml for the test bot
├── docs/
│   ├── design.md         the product
│   ├── mvp-plan.md       stages, decisions, risks
│   ├── architecture.md   this file
│   ├── versioning.md     shared across the family
│   ├── releases.md       shared across the family, plus this stack
│   └── img/              pictures the documents link to
├── .github/              CI, release workflow, dependabot, issue form
├── AGENTS.md  CHANGELOG.md  README.md  SECURITY.md  LICENSE  NOTICE
└── .env.example          every variable the code reads, with a comment
```

Today the Go side holds `cmd/maply`, `internal/config`, `internal/db`,
`internal/bot`, `internal/health`, `internal/round`, `internal/geo`,
`internal/scoring`, `internal/auth`, `internal/api`, `internal/telegram`,
`web`, `migrations/`, `deploy/` and the CI workflows; the other packages
arrive with their stages.
`migrations/` is a Go package too: it embeds the SQL into the binary, so an
image always carries the schema its code was built for. `curation/` exists only on the operator's
machine: its batches carry source IDs and coordinates and are gitignored.

## Data Flow

A day, from the service's point of view:

1. Every 20 seconds `schedule` reads the active chats and queues the current
   date's jobs for each one in its own zone: the post at the chat's post time
   (07:00 unless a group admin moved it), an intrigue at a minute between
   16:00 and 18:00 that is the same on every run, and the result at 20:00.
   A job's key is `kind:date:chat`, so planning twice adds nothing. A chat that
   appears during the day (a new group, a first `/start`) gets the day's post
   on the next pass, until the reveal.
2. Due jobs are claimed with a two-minute lease (`FOR UPDATE SKIP LOCKED`), so
   a second instance or a restart never sends one twice. A failed send is
   retried with a growing pause up to five attempts. A chat that blocked or
   removed maply is marked inactive and its jobs end as skipped.
3. The post is the day's photo: uploaded once from `IMAGES_DIR`, then sent to
   every other chat by Telegram's file id. A DM gets a `web_app` button; a
   group gets a `t.me/<bot>/play?startapp=<payload>` link, the payload being
   the date and the chat signed with HMAC. In a group the post is pinned
   without a sound; without the right to pin, the admins are told once. Until
   the reveal the caption's who-answered line is redrawn when it changes, at
   most once a minute.
4. The Mini App calls `/api/today` with `initData`. `auth` verifies it, checks
   the payload and, for a group, membership through `getChatMember`.
5. `/api/guess` stores one answer per person per day and answers "accepted".
   The deadline is the earliest reveal among the person's chats.
6. The intrigue is one line. In a group it is whichever of these has something
   to say and was not yesterday's: the local time and weather at the place
   (Open-Meteo, which gets the place's coordinates and nothing about players),
   how far apart the group's pins are, the continent most players worldwide
   chose, or a member whose streak of five or more is at risk. When everyone
   has answered it says so. A DM gets it only without an answer.
7. At 20:00 the result job scores the due answers (`scoring` and `geo`, no
   outside calls), unpins the post and sends the place's photo with the city,
   country, fact and the group's top ten in the caption. A DM gets the whole
   result when the person plays alone, and only their own line when a group
   already shows them the place. `/api/result` starts answering for that chat.
   Where the renderer is configured the picture is the map card; otherwise, or
   when it fails, it is the photo.

## Packages And Dependencies

- `github.com/FreshLabDev/tg` for Telegram, `github.com/jackc/pgx/v5` for
  PostgreSQL. Both are what voicy and branchy use. `chromedp` drives the card
  renderer; it is the one dependency the family does not have yet.
- Everything else is the standard library unless a dependency materially reduces
  complexity. Point-in-polygon on Natural Earth does not need a GIS library.
- The Mini App vendors MapLibre GL, its RTL text plugin, Pannellum and the
  Sofia Sans fonts (Latin, Latin Extended, Cyrillic) into
  `web/app/vendor/<name>-<version>/`, unchanged from their npm packages, with
  their licenses next to them. A versioned path is served as immutable; the
  app's own files are revalidated by ETag.
