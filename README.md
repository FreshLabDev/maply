<h1 align="center">maply</h1>

<p align="center"><strong>One place a day. Pin it on the map. The answer comes at 20:00.</strong><br/>A daily geo-guessing game for Telegram groups, built as a Go bot with a Mini App.</p>

<p align="center">
  <a href="#status"><img src="https://img.shields.io/badge/status-in%20development-245A3C?style=for-the-badge&labelColor=0f172a" alt="status"></a>
  <a href="docs/architecture.md"><img src="https://img.shields.io/badge/go-1.26-00ADD8?style=for-the-badge&logo=go&logoColor=white&labelColor=0f172a" alt="go version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-334155?style=for-the-badge&labelColor=0f172a" alt="license"></a>
  <a href="https://t.me/maplyin_bot"><img src="https://img.shields.io/badge/telegram-%40maplyin__bot-26A5E4?style=for-the-badge&logo=telegram&logoColor=white&labelColor=0f172a" alt="telegram bot"></a>
  <a href="https://github.com/orgs/FreshLabDev/projects/2"><img src="https://img.shields.io/badge/plan-project%20board-245A3C?style=for-the-badge&logo=github&logoColor=white&labelColor=0f172a" alt="project board"></a>
</p>

<p align="center">
  <a href="#how-a-day-goes">How A Day Goes</a> ·
  <a href="#how-it-works">How It Works</a> ·
  <a href="#fair-play">Fair Play</a> ·
  <a href="#prototype">Prototype</a> ·
  <a href="#docs">Docs</a>
</p>

---

## The Problem

A group chat wants something small to do together every day that is not another
poll or another sticker war. Geo-guessing games are fun, but they are solo,
endless, and live outside the chat.

maply is deliberately narrow:

| Need | maply approach |
|:--|:--|
| One shared thing a day | The same place for every DM, every group and the site |
| No spoilers in the chat | Everyone answers in the Mini App; pins appear only at 20:00 |
| Not another chatterbot | At most three group messages a day: post, intrigue, result |
| Something to talk about | One honest hint between 16:00 and 18:00, then the reveal |
| A reason to come back | Streaks, a chat leaderboard, a share line without spoilers |
| Familiar stack | Go, the family's `tg` client, shared Asterfield `core` |

> **Result:** a five-minute ritual that the whole chat plays at its own pace and
> argues about in the evening.

---

## Status

| Channel | Version | Meaning |
|:--|:--|:--|
| Stable | — | Nothing released yet |
| Next | `v0.1.0-alpha.1` | The Go service, the daily round and the Mini App |

The design, the MVP plan and a clickable prototype of the Mini App are here.
The Go service is a skeleton: it connects to the database, polls Telegram and
answers `/healthz`, but does not play yet. Work is tracked on the
[project board](https://github.com/orgs/FreshLabDev/projects/2); what the MVP
includes and where it departs from the design is in
[`docs/mvp-plan.md`](docs/mvp-plan.md).

---

## How A Day Goes

Time follows the capital of the chat's language: Moscow for ru, Minsk for be,
Kyiv for uk.

| Time | Group | DM |
|:--|:--|:--|
| 07:00 | Photo of the day, a "Guess" button, a quiet pin | Photo of the day and the button |
| 07:00–20:00 | The pin shows who has answered, without distances | — |
| A random minute 16:00–18:00 | One intrigue, if not everyone has answered | One reminder, if you have not |
| 20:00 | The result: the place, a fact, the table, the chat's rank | The result, if you are in no group with maply |

Score is up to 5 000: up to 3 500 for distance on the same curve GeoGuessr uses
for the world map, plus +300 for the right continent, +500 for the country and
+700 for the city.

---

## How It Works

maply will be one Go service with PostgreSQL as its only durable store. Domain
tables live in a `maply` schema in the shared `core-postgres`; identity,
presence and language come from `core`. Telegram goes through
[`FreshLabDev/tg`](https://github.com/FreshLabDev/tg) with long polling.

```text
telegram poller -> bot: /start, settings, operator chat
scheduler       -> post 07:00 -> pin edits -> intrigue 16-18 -> result 20:00
http server     -> Mini App, /api/today, /api/guess, /api/result, /img/, /healthz
```

Places come from Mapillary, with Panoramax as a second source. A script picks
candidates from weighted regions, a vision model rates them, and an operator
approves each one in a private chat. The queue holds at least 30 days.

The layout of the repository and every package is in
[`docs/architecture.md`](docs/architecture.md).

---

## Fair Play

- One answer per person per day, shared across DMs, groups and the site.
- Your window closes at the earliest reveal among all your chats, so a Kyiv
  group cannot learn the answer from a Moscow group an hour earlier.
- Before the reveal the server answers a guess with "accepted" and nothing else.
- Pictures are rehosted under random names without EXIF. Source IDs never leave
  the server: a Mapillary ID resolves to coordinates through its public API.
- Reverse image search is not defended against. Like Wordle, the rest is on
  your honour.

---

## Prototype

The Mini App prototype is one HTML file with no build step:

```sh
cd web/prototype
python3 -m http.server 4173
```

Open `http://localhost:4173/` at phone width. `?state=reveal` jumps to the
result screen.

The Mini App the service ships is the same code split into files in
`web/app/`, with MapLibre, Pannellum and the fonts vendored next to it. The
binary serves it at `/`. To look at it alone, run `python3 -m http.server 4174`
in `web/` and open `http://localhost:4174/app/`; until it talks to the API it
plays the prototype's demo round. To see the Telegram integration in a normal browser, append
`#tgWebAppData=auth_date%3D1%26hash%3Dx&tgWebAppVersion=8.0&tgWebAppPlatform=ios`.

The curation scripts need a Mapillary client token in `.env`; see
[`.env.example`](.env.example).

---

## Docs

| Document | Purpose |
|:--|:--|
| [Design](docs/design.md) | The game: cycle, scoring, anti-cheat, intrigue, places |
| [MVP plan](docs/mvp-plan.md) | Scope, stages, decisions, risks |
| [Architecture](docs/architecture.md) | Repository layout, packages, data flow |
| [Versioning](docs/versioning.md) | Pre-release and stable version line |
| [Release process](docs/releases.md) | Changelog, GitHub Release, deploying |
| [Project board](https://github.com/orgs/FreshLabDev/projects/2) | Every task and its status |

---

<p align="center">
  <a href="https://github.com/orgs/FreshLabDev/projects/2">Project board</a> ·
  <a href="CHANGELOG.md">Changelog</a> ·
  <a href="LICENSE">Apache-2.0</a> ·
  <a href="NOTICE">NOTICE</a>
</p>

<p align="center">
  maply is open source software by Asterfield.<br/>
  Copyright 2026 Asterfield.
</p>
