# AGENTS.md

This file is for coding agents working on maply. Keep it minimal, fair to every
player, and production-minded.

## Project Shape

- maply is a daily geo-guessing game: one place a day for everyone, a pin on the
  map, the place right after answering, the names and the group's result at
  20:00. The product is in [`docs/design.md`](docs/design.md),
  the order of work in [`docs/mvp-plan.md`](docs/mvp-plan.md). Read them before
  changing behavior.
- The Go service has config, the database with embedded migrations, Telegram
  long polling that records people and chats in `core`, the game calendar,
  scoring over embedded Natural Earth borders, the HTTP API, sign-in on the
  site with Telegram and Google (`docs/sign-in.md`) and `/healthz`. It
  serves the Mini App from `web/app/` (package `web`). Next to it are the
  original single-file prototype (`web/prototype/index.html`), the curation
  scripts (`scripts/curation/`) and the source measurements
  (`scripts/sources/`).
- The app is one page on many addresses: the server answers every path in
  `web.Pages` with `index.html`, and `web/app/router.js` maps the path to a
  screen. A screen registers itself there from its own file; a new address
  goes into `Pages`. `index.html` names its files from the root.
- `desktop.css` lays the app out on a tablet and a computer over `app.css`.
  `html.desk` (from 1024×600, set by `desk.js`) marks the computer's layout;
  the file's header documents the base a screen builds its own on. A screen
  in the computer's header passes `nav: { href, t }` to `Router.screen`. The
  phone's layout must not change.
- The result is a map card when `CARD_RENDERER_URL` names a headless Chromium:
  `internal/card` serves the page `/card/result/<chat>/<date>`, the renderer
  photographs it, and `schedule` sends the PNG. Any renderer failure falls back
  to the place's photo; never let the card block the result. The Mini App
  forwards the same card with `shareMessage` through `POST /api/share`.
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
- The reveal is 20:00 in the chat's own zone. A group admin picks it from
  `round.ZoneChoices`, a person picks their DM's; until then it follows the
  capital of the language, with ru and anything unknown on `Europe/Kyiv`
  (`internal/round/zones.go`). Zones come from tzdata; never hard-code an
  offset.
- A group gets at most three messages a day: the 07:00 post, one intrigue
  between 16:00 and 18:00, and the 20:00 result. Edits to the pinned post do not
  count. Do not add a fourth: the rules in a group's first post and the
  admins' note when maply may not pin go into the post itself, and so does
  the month's season champion, in the look back of the month's last day.
- Posts write the reveal with the chat's zone, "20:00 (Kyiv)". A chat's zone is
  stored the first time maply needs it (`FixZone`), so a later change of
  language cannot move a reveal. A DM's stored this way is marked automatic
  and the phone's zone may replace it; a zone someone picked never moves.
- Before the chat's reveal the API sends nobody else's name, photo, id or
  answer time. In the world's table the chat's own players are named at its
  reveal, everyone else once the day has closed in every zone, so a group in
  Tokyo does not learn who is who in a Kyiv group still guessing.
- An intrigue never lies. Every one is a real hint or a real statistic.
- One answer per person per day, shared across DMs, groups and the site when
  signed in through Telegram. An answer cannot be changed after it is sent.
- Score: `3500 · e^(−d / 1492.7)` for distance plus +300 continent, +500 country,
  +700 city as a ladder. Equal scores rank by who answered first.
- Interface text lives in `internal/i18n`, never inline in Go or in the Mini App
  script. maply speaks the family's sixteen languages (en, ru, uk, es, fr, de,
  it, pl, cs, tr, sv, be, ca, zh, ja, ar), and they ship together; a key
  missing in one language is a bug.
- The interface language is core's, shared with every bot in the family:
  `core.set_language` with `manual`, `core.clear_language` for "Follow
  Telegram". maply keeps no copy of it. Opened from a group, the Mini App
  speaks the language the person picked by hand in any bot of the family
  (`core.manual_language`, core migration 020), else the group's, else the
  person's. The language of their Telegram app is not a pick. What a player
  shares from a group is in the group's language, as the bot's posts there
  and the card are (`share_lang` in `/api/today`).
- Panels look like voicy's and quoto's: `m:<owner>:<action>` callbacks, a
  bold title, an italic hint, the substance in a quote, `◉`/`◎` on choices,
  Success on the current one, Back, and Close only in a group.
- Bot names are written in lowercase everywhere, including headings and images.
- Free play (`internal/free`, `/play`, `/party/<code>`; the archive and a
  challenge are screens inside `/play`) is outside the day:
  its own `free_*` tables, no place in the world, no stats shared with the
  day, and no bot message in groups. Its rules are in `docs/design.md` →
  "Free play". Records and friends need a Telegram person: the Mini App,
  or the site signed in with an account that has Telegram, which plays as
  the person and writes nothing to `core`.
  Its leaderboards (`/api/top`) count only games on a timer and never an
  attempt at a challenge, one row a person or an account with their best:
  an account's site players count for the account, or for its Telegram
  person once it has one.

## Anti-Cheat Invariants

These are the game. A change that weakens one is a breaking change, whatever
the diff looks like.

- A person's answer window closes at the earliest reveal among all of their
  contexts: the DM and every group `core.presence` has seen them in. A group
  that removed maply after its reveal still counts that day
  (`chat_settings.left_at`): its members saw the place there. Opening a
  group's result puts the person in that group's presence, answered or not.
- In Telegram (DM and group) a guess is scored the moment it comes in, and the
  person sees the place, their distance and their score right away. Before the
  chat's reveal they also see the world's table and the chat's pins, but
  without names: distance, points with their parts, the world place, a mark
  for the chat's own players, and a point on the map for the chat. No name,
  photo, user ID or answer time. The server strips these; the client never
  receives them. This is light hiding for the evening's intrigue, not a
  privacy promise: the points include the speed bonus, which roughly dates an
  answer. A row's key depends on the viewing chat, so the key does not link
  one person's rows between a group, a DM and the site. The rank and the
  distance are the same in every view, though, and match the rows for anyone
  who looks at two views at once. The world picture's
  podium has no names or faces until the day has closed in every zone.
- Nobody sees the place without having answered. Before the reveal
  `/api/result` and `/api/share` answer a person with no answer `403
  not_answered`, and nothing else. This is the invariant that keeps the live
  screen safe; `TestADayAcrossZones` covers it.
- Before the reveal, sharing sends a text line with the day, distance and score
  only, never the picture with the place.
- The place's coordinates, city, country, source ID and source URL never appear
  in an API response to someone who has not answered, an image name, an image's
  EXIF, a log line or an error message before the reveal.
- The card page opens only with a card token (`auth.Signer.SignCard`, never the
  startapp payload) and only after the chat's reveal; shared card images are
  served under random keys. Keep both when touching `internal/card`.
- A day's strip (`day_thumb`, `/thumb/<key>.jpg`) and its link preview
  (Open Graph tags on `/days/<n>`, `/og/day/<n>-<lang>.jpg`) exist only for a
  day in `day_closed`, after the latest reveal: the strip's key goes out with
  the day's city, and a strip of a place still in play would be a key to it.
  `place.thumb_yaw` is never sent to a client.
- Images are rehosted under a random key with EXIF stripped. A Mapillary ID
  resolves to coordinates through the public API, and Panoramax HD frames carry
  GPS in EXIF.
- The site's own game, played by a guest or by an account with Google alone,
  shows today's place only after the latest reveal across every language. A
  Telegram player sees the place on answering and a new browser is a new
  player, so `day_rank` puts every site answer after every Telegram one: a
  site row never takes a Telegram player's place. Google is not a person the
  bot knows, so a Google account ranks as the site does.
- A browser signed in with an account that has Telegram is that Telegram
  player, not a site player. It answers through `/api/guess` with the session
  cookie: one answer a day with the bot, closed at the earliest reveal among
  the person's chats, with the speed bonus, the place on answering and the
  names at the DM's reveal, as in the DM. It writes nothing to `core`: no
  presence, so it neither ties the person to a chat nor puts them on the
  morning post, and without an answer it sees the place only after the latest
  reveal anywhere. `TestATelegramAccountPlaysOnTheSite` covers it.
- Merging two accounts never rewrites a closed day. In an open day the
  earliest site answer stays, and becomes the Telegram answer only when both
  the answer and the merge come before the person's Telegram deadline: after
  it they may have seen the place, and would pick the best of several
  guests' answers. A Telegram answer is never replaced. The Mini App's
  ticket links Google but never merges two accounts.
- A name typed on the site is not checked: bot posts call that player "Guest".
  The site's write endpoints take only JSON from maply's own pages.
- A profile shows its owner every scored day, today's too, signed in on
  the site with Telegram as well; anyone else, and a site guest or an
  account without Telegram looking at their own, gets only days over in
  every zone (`profile.LatestClosed`), with no pins, history or answer
  times. A world place and the percentile count only days over everywhere.
  A mini profile
  opens by an opaque handle (`profile.Handles`) that the server puts only
  next to a name it already shows, never on an anonymous row, so a handle
  cannot name a row before its reveal. The photo goes out only inside a
  group both are in, and the head to head counts only the days whose result
  that group has seen.
- "Hide me" (`profile_setting.hide_me`) replaces the name with "Telegram
  player" in the world's tables, the site's result, the days page and the
  free play's leaderboards for
  everyone outside the person's own groups, from a DM too, and the mini
  profile answers `404 hidden`. A group's own table and card, which a
  player may forward, and the bot's posts, the world's podium in a DM
  included, keep the name. A handle is scoped to its table
  (`profile.Handles.Of`), so one player's rows in two tables cannot be
  matched by it.
- A public page `/p/<slug>` exists only while its owner keeps it on. The
  slug is random (`profile.NewSlug`), a new one ends the old, and it never
  reaches a log line. The page and its preview (`/api/p/<slug>/card.png`)
  hold a stranger's view: the name and the numbers of days over in every
  zone, with no id, username, photo, handle, pins, history or answer
  times, and both ask search engines to stay away. "Hide me" closes it
  until the player shows again; the preview may stay at the edge for the
  five minutes it is cached.
- `/api/log` strips query strings, which can carry `initData`, and is rate
  limited per address and in total.
- A free place never becomes a day's place: the day's queue reads
  `pool = 'daily'` only, and free play takes the free pool and days in
  `day_closed` that none of the game's players answered.
- A free place's picture is served only under its round's address
  (`/api/free/img/<token>`: the game, the place's number and an HMAC) and only
  once the place has started, never under the place's image key, which the
  days page shows for closed days. Starting a place fixes its start and end;
  starting it again gives the same time left. The coordinates, name, fact
  and source come only in the answer, and in a party when the place closes;
  before that a party's poll says who has answered and nothing else.
- A Telegram person who makes or joins a free game is recorded with
  `TouchPerson`, never `Touch`: free play must not add a DM context that
  closes their day's answer window or puts them on the morning DM post.
  The day's game opened from a party's invitation is the DM's, and touches
  as the DM does: the invitation is not a way around the DM's reveal.
- Free play takes the free pool before closed days: a closed day's picture
  is public on the days page. A late joiner in a party skips a place drawn
  before they came that they have had (`FreeKnown`).
- The free game's archive lists and serves only days in `day_closed`, so it
  never holds a place still in play, whatever number is asked. A player
  takes a day once (`free_archive`), and never a day they answered (`guess`,
  or `site_guess` for the browser's player) or whose place they had in free
  play; taking it starts the place. Its result is `free_result` with mode
  `archive`: no record, and never a row of the day's table.
- A challenge (`free_challenge`) shows names and sums only, never a place:
  an attempt gets each place as a solo game does, when it starts it, on its
  own clock. A Telegram person takes it once (`free_attempt`); a place they
  have had is played and counted as seen in the table. The day's game opened
  from `startapp=c<code>` is the DM's, as from a party's invitation.
- Against the clock gives no speed bonus, and every place ends when the
  game's three minutes do, so no place stays open past them.
- The `startapp` payload is signed with HMAC; group membership is checked with
  `getChatMember`. `initData` is verified on every request that writes.

## Places

- Curation batches (`curation/`) hold source IDs and coordinates. They stay out
  of git; the repository is public.
- Excluded from the game: the front line, Crimea and occupied territories,
  military sites, and frames with recognizable faces.
- A place of the free pool (`pool = 'free'`) never becomes a place of the
  day, and the day reads only `pool = 'daily'`. The pools share no frame and
  keep 10 km apart: a batch for either pool is harvested and prepared with
  the other pool's `places.sql` and `candidates.json` in `MAPLY_AVOID` and
  `maply-places -avoid`.
- The Mapillary token is read from the environment only. Never print it, log it,
  paste it into a document, or commit a file that contains it.
- Attribution follows the source license (CC-BY-SA for Mapillary and Panoramax)
  and is shown with the picture after the reveal.

## Data And Security

- Never log the bot token, the Mapillary token, a database URL with its
  password, or full Bot API URLs.
- Sign-in on the site (`internal/account` over `internal/oidc`) is the code
  flow with PKCE, a fresh `state` and `nonce` each time, and RS256 id_tokens
  only. The state lives in a signed ten-minute cookie bound to the browser's
  session, so a callback from someone else's sign-in, a swapped state or a
  replayed code signs nobody in. The session cookie is
  `__Host-maply_session`; the database keeps its SHA-256 only, it lasts 90
  days from the sign-in and is never extended, and every sign-in and link
  starts a new one. Writes authenticated by the cookie take only JSON from
  maply's own pages. The sign-in log carries the result code only: no
  Telegram id, no Google sub, no address.
- A Telegram sign-in records the person with `core.touch_person`, never
  `core.touch`. There are no `/start link_…` links: the Mini App links Google
  with a five-minute ticket that the phone's browser confirms by name first.
  The ticket's link never merges, keeps another account's browser signed in
  there, and ends with a sign-out everywhere or an unlink.
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
- A day over in every zone is kept in `day_place` by the final world job, and
  the profile and the days page read it from there. An answer removed by hand
  from such a day needs `DELETE FROM day_closed WHERE date = …` too, or the
  old places stay. The day's strip goes with it and is drawn again under a
  new key.

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
  comments, commit messages. The game's interface is in the family's sixteen
  languages.

## Working In This Checkout

- Other agents may work in the same checkout. Stage files by name; never
  `git add -A` or `git add .`.
- Do not commit or push unless asked.
- Commit messages and pull request descriptions carry only a
  `Co-Authored-By` line for attribution. No session links.

## Verification

```sh
gofmt -l .
go vet ./...
go test -race ./...
docker compose -f deploy/ws04/compose.yaml config
```

The database tests need a real PostgreSQL and a disposable database whose name
contains `test`; they refuse to run anywhere else:

```sh
MAPLY_TEST_DATABASE_URL=postgres://maply:maply@localhost:5432/maply_test?sslmode=disable \
  go test -tags=integration -p 1 ./...
```

Verify the Mini App in a browser at phone width (375×812 and 320×568), with and
without the Telegram emulation hash
(`#tgWebAppData=…&tgWebAppVersion=8.0&tgWebAppPlatform=ios`), and check the
console has no errors.

## Release Checklist

- `/start` opens the settings in a group and in a DM.
- The morning post, the intrigue and the result arrive exactly once each,
  including after a restart in the middle of the day.
- A person in a Kyiv group who is also in a Moscow group cannot answer after
  20:00 Moscow time in winter.
- Before the reveal, no API response to someone who has not answered gives
  the place's coordinates, name or source, and no response names another
  player.
- Right after answering in Telegram, the Mini App shows the place and the
  others' answers without names; a new answer appears within a few seconds;
  names appear at the chat's reveal without reopening the app.
- The Mini App opens from the DM button and from the group link, on iOS,
  Android and Desktop; Back, haptics and the closing confirmation work.
- Images served from `/img/` have no EXIF.
- `/healthz` reports the database, Telegram polling freshness and the size of
  the place queue.
- On the site, signing in with Telegram and with Google, linking, unlinking,
  signing out and deleting the account work; signed in with Telegram, an
  answer given in the bot shows on the site, and the site refuses a second.

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
