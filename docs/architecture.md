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
- The 20:00 result is a picture of the map. The service serves an internal card
  page in the Mini App's style, and a headless Chromium container next to it
  takes the screenshot over CDP. Nothing outside the service can reach that
  container. If it does not answer, the bot sends a text fallback.
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
│   ├── bot/              updates, /start, settings, operator chat
│   ├── round/            place of the day, day number, reveal time per zone,
│   │                     the answer window across a person's chats
│   ├── scoring/          haversine, distance curve, bonus ladder
│   ├── geo/              Natural Earth admin-0 (embedded), point in polygon
│   ├── schedule/         job queue: morning post, pin edits, intrigue, result
│   ├── intrigue/         the five intrigue types and their rotation
│   ├── card/             the result card page and the Chromium screenshot client
│   ├── auth/             initData verification, signed startapp payload
│   ├── api/              /api/today, /api/guess, /api/result, /img/
│   ├── images/           rehosting store, EXIF stripping, random keys
│   ├── places/           queue, import of curated batches, low-queue warning
│   ├── i18n/             translations.json: ru, uk, be
│   └── health/           /healthz: database, polling, queue depth
├── migrations/           NNN_name.sql, applied at start, recorded in schema_migrations
├── web/                  package web: embeds app/ and serves it with a CSP
│   ├── app/              the Mini App served by the binary: index.html, app.css,
│   │                     tg.js, map.js, app.js, vendor/
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

1. Before 07:00 in each zone, `schedule` writes the day's jobs for every chat:
   the post, an intrigue at a random minute between 16:00 and 18:00, and the
   result at 20:00. Each job carries an idempotency key, so a restart re-reads
   the queue and sends nothing twice.
2. The post goes out with a `web_app` button in a DM and a
   `t.me/<bot>/play?startapp=<payload>` link in a group. The payload is the date
   and the chat, signed with HMAC.
3. The Mini App calls `/api/today` with `initData`. `auth` verifies it, checks
   the payload and, for a group, membership through `getChatMember`.
4. `/api/guess` stores one answer per person per day and answers "accepted".
   The deadline is the earliest reveal among the person's chats.
5. At 20:00 the result job computes scores (`scoring` and `geo`, no outside
   calls), renders the map card, unpins the post and sends the card with the
   table in its caption. `/api/result` starts answering
   for that chat.

## Packages And Dependencies

- `github.com/FreshLabDev/tg` for Telegram, `github.com/jackc/pgx/v5` for
  PostgreSQL. Both are what voicy and branchy use. `chromedp` drives the card
  renderer; it is the one dependency the family does not have yet.
- Everything else is the standard library unless a dependency materially reduces
  complexity. Point-in-polygon on Natural Earth does not need a GIS library.
- The Mini App vendors MapLibre GL, Pannellum and the Sofia Sans fonts into
  `web/app/vendor/<name>-<version>/`, unchanged from their npm packages, with
  their licenses next to them. A versioned path is served as immutable; the
  app's own files are revalidated by ETag.
