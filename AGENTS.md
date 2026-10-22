# AGENTS.md

This file is for coding agents working on maply. Keep it minimal, fair to every
player, and production-minded.

## Project Shape

- maply is a daily geo-guessing game: one place a day for everyone, a pin on the
  map, the answer at 20:00. The product is in [`docs/design.md`](docs/design.md),
  the order of work in [`docs/mvp-plan.md`](docs/mvp-plan.md). Read them before
  changing behavior.
- The Go service does not exist yet. What exists: the Mini App prototype
  (`web/prototype/index.html`), the curation scripts (`scripts/curation/`) and
  the source measurements (`scripts/sources/`).
- The service will be one Go binary (module `github.com/FreshLabDev/maply`): the
  bot with its scheduler, the HTTP API, the static Mini App and the place
  images, all from one process behind a Cloudflare Tunnel at `maply.ws04.cc`.
- PostgreSQL is the only durable store. Domain tables live in a `maply` schema
  on the shared `core-postgres`, reached as `maply_core`. Telegram identity,
  presence and language are delegated to `core` through `core.touch` and
  `core.effective_language`, never by writing `core.*` tables directly.
- Telegram goes through `github.com/FreshLabDev/tg`. What `tg` lacks (the
  `web_app` button, pin and unpin, `setChatMenuButton`,
  `savePreparedInlineMessage`) is called through `Client.Call` until typed
  methods land in `tg`. A `tg` release needs separate permission. Long polling
  only. maply uses `api.telegram.org`, not the self-hosted Bot API server.
- `Preflight` runs before the bot starts and names the methods maply cannot
  work without.

## Product Boundaries

- One place per calendar date, the same for DMs, every group and the site.
  Days are numbered from launch: `maply #1`, `maply #2`.
- Time follows the capital of the chat's or person's language: ru
  `Europe/Moscow`, be `Europe/Minsk`, uk `Europe/Kyiv`. Zones come from tzdata;
  never hard-code an offset.
- A group gets at most three messages a day: the 07:00 post, one intrigue
  between 16:00 and 18:00, and the 20:00 result. Edits to the pinned post do not
  count. Do not add a fourth.
- An intrigue never lies. Every one is a real hint or a real statistic.
- One answer per person per day, shared across DMs, groups and the site when
  signed in through Telegram. An answer cannot be changed after it is sent.
- Score: `3500 · e^(−d / 1492.7)` for distance plus +300 continent, +500 country,
  +700 city as a ladder. Equal scores rank by who answered first.
- Interface text lives in `internal/i18n`, never inline in Go or in the Mini App
  script. ru, uk and be ship together; a key missing in one language is a bug.
- Bot names are written in lowercase everywhere, including headings and images.

## Anti-Cheat Invariants

These are the game. A change that weakens one is a breaking change, whatever
the diff looks like.

- A person's answer window closes at the earliest reveal among all of their
  contexts: the DM and every group `core.presence` has seen them in.
- Before the reveal the API answers a guess with "accepted" and nothing else:
  no distance, no score, no hint of direction. Distance alone is enough to
  triangulate a place.
- The place's coordinates, city, country, source ID and source URL never appear
  in an API response, an image name, an image's EXIF, a log line or an error
  message before the reveal.
- Images are rehosted under a random key with EXIF stripped. A Mapillary ID
  resolves to coordinates through the public API, and Panoramax HD frames carry
  GPS in EXIF.
- The site shows today's place only after the latest reveal across every
  language.
- The `startapp` payload is signed with HMAC; group membership is checked with
  `getChatMember`. `initData` is verified on every request that writes.

## Places

- Curation batches (`curation/`) hold source IDs and coordinates. They stay out
  of git; the repository is public.
- Excluded from the game: the front line, Crimea and occupied territories,
  military sites, and frames with recognizable faces.
- The Mapillary token is read from the environment only. Never print it, log it,
  paste it into a document, or commit a file that contains it.
- Attribution follows the source license (CC-BY-SA for Mapillary and Panoramax)
  and is shown with the picture after the reveal.

## Data And Security

- Never log the bot token, the Mapillary token, a database URL with its
  password, or full Bot API URLs.
- Connect with `search_path=maply` and call `core.touch` before domain writes
  that reference `core.person`.
- Secrets live in `.env`, which is gitignored.
- `callback_data` and the `startapp` payload are a contract with posts already
  in chats. Do not change either without keeping the old form working.

## Code Style

- Prefer small packages and simple interfaces.
- Use the standard library unless a dependency materially reduces complexity.
- Keep SQL explicit and close to the store method that uses it.
- Avoid broad refactors while changing behavior.
- Add comments only when they explain non-obvious anti-cheat, time zone,
  concurrency, or protocol behavior.
- The Mini App stays plain HTML, CSS and JavaScript without a bundler. Libraries
  and fonts are served from the service itself; only `telegram-web-app.js` and
  the map tiles come from outside.

## Migrations

- Add new SQL migrations under `migrations/` with the next numeric prefix.
- Migrations must be safe to run once and tracked by `schema_migrations`.
- Do not edit a migration after it may have been applied.
- Grants for `maply_core` belong to the `core` repository, in the same migration
  that revokes `PUBLIC`.

## Versioning

- Work on `dev`. Pre-releases (`-alpha.N`, `-beta.N`, `-rc.N`) are tagged on
  `dev`; stable versions are tagged on `main`, on the merge commit from `dev`.
  The test bot runs `dev`, the production bot runs `main`.
- Follow `docs/versioning.md`. The first line starts at `v0.1.0-alpha.1`.
- Treat the surface listed in `docs/versioning.md` under "Breaking-sensitive
  surface" as breaking before `v1.0.0` too.

## Changelog And Releases

- Keep notable changes under `## Unreleased` in `CHANGELOG.md`, in the same
  work that makes them. Exactly one `## Unreleased`, always at the top.
- Follow `docs/releases.md` for changelog sections and the release workflow.
- Mark `alpha`, `beta`, and `rc` GitHub Releases as pre-releases.
- Do not tag or publish a release without explicit permission, even when the
  work is done and everything builds.

## Planning

- Tasks and their status live in GitHub issues on the
  [maply project board](https://github.com/orgs/FreshLabDev/projects/2), not in
  files. `docs/mvp-plan.md` describes the stages and the decisions; each stage
  maps to issues through the board's Stage field, and Area says which
  part of the service a task touches. Repository labels stay the GitHub
  defaults, as in every family repository. When the plan changes,
  change the issues in the same step.
- A new issue says what to do in its title, and its body ends with
  "Done when …": the check that closes it.
- Everything public is written in English: issues, the board, docs, code
  comments, commit messages. The game's interface is ru, uk and be.

## Working In This Checkout

- Other agents may work in the same checkout. Stage files by name; never
  `git add -A` or `git add .`.
- Do not commit or push unless asked.
- Commit messages and pull request descriptions carry only a
  `Co-Authored-By` line for attribution. No session links.

## Verification

Until the Go service exists, verify the prototype in a browser at phone width
(375×812 and 320×568), with and without the Telegram emulation hash
(`#tgWebAppData=…&tgWebAppVersion=8.0&tgWebAppPlatform=ios`), and check the
console has no errors.

Once it exists:

```sh
gofmt -l .
go vet ./...
go test -race ./...
docker compose -f deploy/ws04/compose.yaml config
```

The database tests need a real PostgreSQL and a disposable database whose name
contains `test`; they refuse to run anywhere else.

## Release Checklist

- `/start` opens the settings in a group and in a DM.
- The morning post, the intrigue and the result arrive exactly once each,
  including after a restart in the middle of the day.
- A person in a Kyiv group who is also in a Moscow group cannot answer after
  20:00 Moscow time in winter.
- No API response before the reveal contains the place's coordinates, name or
  source.
- The Mini App opens from the DM button and from the group link, on iOS,
  Android and Desktop; Back, haptics and the closing confirmation work.
- Images served from `/img/` have no EXIF.
- `/healthz` reports the database, Telegram polling freshness and the size of
  the place queue.

## License

Apache-2.0. Preserve `LICENSE` and `NOTICE`. Every `.go`, `.sql`, YAML file,
`go.mod` and the `Dockerfile` starts with `SPDX-License-Identifier: Apache-2.0`.

## Deploying

Do not invent a deploy. [`docs/releases.md`](docs/releases.md) has a **Deploying**
section describing this stack exactly: which host directory it lives in, which
env file names the image, which networks it needs, and how to roll back. Read it
before touching anything on the host. Deploy only with explicit permission.

Two rules that hold everywhere and are easy to get wrong:

- **Nothing is built on the host.** A production stack pulls the image the
  release workflow published. A `build:` section in a production manifest is a
  bug.
- **Pin the digest, not the tag.** A tag moves; a digest names one build that was
  tested, and a rollback becomes one line with nothing to rebuild.
