# Changelog

All notable maply changes are documented here.

The `## <tag>` section of this file *is* the GitHub Release body: the release
workflow copies it verbatim and refuses a tag that has no section. Write it
for whoever has to decide whether to upgrade.

See [`docs/versioning.md`](docs/versioning.md) for what the numbers mean and
[`docs/releases.md`](docs/releases.md) for how a release is published.

## Unreleased

### Changed

- The game picker shows the first three of all time on the chosen settings,
  not of the last seven days. The leaderboards page keeps both periods.

### Fixed

- An account that holds two answers of one day, a Telegram one and a guest's
  it took in after the day was over, is "you" on one row only: the earlier
  answer, the one the days page shows. The day's page and the site's result
  take its pin and numbers from that row; the other answer stays as everyone
  sees it.

## v0.0.1-alpha.18 - 2026-11-06

For the operator: migration 027 gives each kept photo an address; it runs by
itself at start and takes a moment.

### Changed

- Players are shown the same way on every screen: the day's and the world's
  tables, the days page, the leaderboards, the profiles and the games with
  friends take the name, the photo and "Anonymous" from one place on the
  server and draw the face one way. Telegram photos now show in a party
  too: the lobby, the tables, the row of who has answered and the pins on
  the map.
- A photo is sent as a short link that the browser keeps, not inside every
  answer: the result's polls are smaller and a face loads once.

### Fixed

- Someone else's public profile showed the viewer's own photo on its card.

## v0.0.1-alpha.17 - 2026-11-06

For the operator: no migrations and no new settings; the image replaces
alpha.16 as it is.

### Added

- The site has an icon in the browser's tab and on a phone's home screen:
  the bot's globe drawn flat, with the pin large enough to read at 16 px.
  The privacy page has it too, and /favicon.ico answers.

### Changed

- A Telegram player's photo is shown wherever their name is: the day's
  table and the world's in the Mini App, the site's result, the days page,
  the leaderboards and the mini profile, to anyone, not only inside a group
  both are in. A player who hid themselves stays "Anonymous" with a plain
  face, and before the chat's reveal the other rows still have neither name
  nor photo.
- Your own face is your Telegram photo: the profile button in the header,
  your profile and your rows, in the Mini App and on the site signed in
  with Telegram. It is asked of Telegram when the app opens, at most once a
  day, so a new photo shows the next day.
- Photos stay current: a page shows the kept ones at once and asks
  Telegram in the background for the ones missing or a day old. Names were
  already read from Telegram on every visit.

### Fixed

- A double tap on the panorama zooms on a phone. The phone's browser sends
  a mousedown of its own after the tap, and the panorama stopped the zoom
  it had just started on it, at once or after a few degrees. A double click
  with a mouse always worked.

## v0.0.1-alpha.16 - 2026-11-06

For the operator:

- Apply core migration 021 (`core.touch_person`) before deploying. The
  service refuses to start without it and names the migration.
- Sign-in on the site needs keys from BotFather and Google Cloud, set as
  `TELEGRAM_LOGIN_CLIENT_ID`/`_SECRET` and `GOOGLE_CLIENT_ID`/`_SECRET`, and
  `APP_URL`; `docs/sign-in.md` lists the steps. Without them the site plays
  as before and its sign-in buttons are greyed out.
- Migration 018 adds the site's accounts (`account`, `session`,
  `link_ticket`, `site_player.account_id`). Migration 019 adds free play
  (`free_game`, `free_member`, `free_round`, `free_guess`, `free_result`) and
  `place.pool`, which is `daily` for every place already kept.
- Free play needs its own pool. Load the free places after the deploy, when
  019 has added `place.pool`, and put their pictures in the stack's image
  directory first: a batch's `places.sql` with `pool = 'free'` and its
  `upload/` folder. A rerun of `places.sql` adds nothing. Until it is loaded,
  free play draws only from closed days.
- Migration 017 adds `place.thumb_yaw` and the table `day_thumb`. After the
  deploy the service draws a strip for every day already kept, one at a
  time, in the background.
- For the places already in the queue, `maply-places -thumb-yaws` writes
  `thumb-yaws.sql`, which sets the strip's direction only where none is set,
  with each strip beside it to look at first. It is optional: without it the
  service picks the direction itself when it draws the strip.
- Migration 021 adds `profile_setting` (hide me, the public link) and an
  index on `site_guess (player, date)` for a guest's profile.
- Migration 020 adds `free_archive`, `free_challenge` and `free_attempt`, and
  replaces the checks on `free_game` and `free_result` with named ones that
  allow the new modes. It runs in one transaction on tables the service
  already writes; nothing to do before or after.
- Migration 024 lets a free game's result and a challenge's attempt belong
  to a site player as well as to a Telegram person (`free_result.player`,
  `free_attempt.player`), and adds the index the free game's leaderboards
  read. Nothing to do before or after.
- Migration 026 only replaces the function of the trigger that keeps a
  place's pool, so a place any round names stays the day's; it reads and
  writes no row.

### Added

- `Store.TouchPerson` records a person in core without a chat or presence,
  for a person met outside the bot's chats: a website sign-in with Telegram
  or a guest who joins a game with friends from a link. `Touch` would count
  them as talking to maply in a DM, which closes their answer window by the
  DM's zone and puts them on the morning DM post.
- The curation scripts prepare the free game's pool. `maply-places -pool
  free` writes `pool = 'free'`, no facts and one city name, the English one
  where OSM has it. `-avoid` and `MAPLY_AVOID` keep a batch 10 km from every
  place and candidate of the other pool, so a free place never becomes a
  place of the day and the free game never shows the spot of a future one.
- `scripts/curation/screen.py` drops frames near or inside an OSM military
  area and in the parts of a country left out, and draws review sheets of the
  whole panorama instead of the 120° the game opens on. `choose.py` turns the
  reviewers' verdicts into `reviews.json` with at most 15 places a country
  and 500 in all, counted over every batch it is given, and writes a report
  of them.
- `scripts/curation/faces.py` gives every prepared place a sheet for faces
  alone, at twice the detail of the review sheets, close to what the game
  shows after `-sharpen`, and turns the verdicts into a review `choose.py`
  reads.
- `harvest.py` passes over a soft panorama, or one under 2048 px wide, for
  another frame of the tile.
  `pool.py` plans in more countries, with rules for their troubled borders,
  and `MAPLY_CAP` limits the entries a country gets.
- `maply-places` writes `sharpen.csv`, the input of `-sharpen`, next to
  `places.sql`.
- `maply-places` refuses a batch whose `reviews.json` was chosen for the
  other pool, `-pool free` without `-avoid`, and an `-avoid` file it reads no
  place from, so a slip of a flag cannot put free places in the queue of days
  or leave the pools free to overlap. The scripts refuse such a file too and
  say how many places they keep clear of.
- The player's profile in the Mini App, behind the name's letter at the end of
  the header and at `/me`: points, the 30-day average, streaks, the typical
  miss, the share of countries and cities found, the best day and world place,
  and the average percentile over the days that are over everywhere, in tiles
  that fill their rows at every width, the month's season among them. Three
  charts drawn in SVG (points over 30 days with the median, misses by
  distance, continents), the passport: one map of the world with the
  countries found in gold and one's own pins over them, a pin opening its
  closed day, then the history by day and the free play numbers once
  `free_result` exists. A site guest gets the same from the cookie,
  closed days only, with a note to sign in.
- A tap on a name in the world's tables opens a mini profile: days, streak,
  the 30-day average, best world place, countries, and from free play the
  average per place and duels. Inside a shared group it adds the photo and
  "closer than you on 14 of 22 shared days", counted over the days that
  group has seen the result of. Names carry an opaque handle, never an id.
- "Hide me" in the profile: for everyone outside the person's own groups
  the world's tables, the site's result, the days page, the free play's
  leaderboards and the bot's world lines show "Anonymous" with a plain
  avatar instead of the name and photo, and the mini profile does not
  open. The person's groups keep the name.
- The month's season: the sum of a player's 20 best days. The 07:00 post of
  the 1st names the champion inside yesterday's look back, a group's own in
  a group and the world's in a DM with the person's place; no new message.
  The profile shows the place in the month so far, in the world and in the
  group it was opened from.
- A public link to the profile, off until the player turns it on: `/p/…`
  opens their numbers over the days over everywhere for anyone with the
  link, without pins, history or photo, and asks search engines to stay
  away. "New link" ends the old one, and "Hide me" closes the page while
  it is on. The link's preview is the profile picture without the pins.
- The profile on the site follows the account. Signed in with Telegram it
  is the Telegram player's, with every day, the season and the settings, as
  in the bot; with Google alone it holds the account's site days from every
  browser it has taken in, one answer a day. A Telegram player's profile
  also counts the days their account's guest played on the site before the
  sign-in, where there is no Telegram answer, once the day is over
  everywhere and only an answer given before the day's first reveal. The site's header keeps one
  button: the account's menu opens the profile, and a guest's sign-in
  dialog has "My profile without signing in".
- In Telegram, Back goes to the page before, as it does in a browser: a day
  opened from the profile comes back to the profile, where it was.
- Leaderboards of free play (`GET /api/top`, `web/app/leaders.js`): Country
  streak, Against the clock, Blitz and Classic, a table for each timer and
  length, over the last 7 days or all time. One row a player with their
  best, and an equal result ranks whoever reached it first. Only games with
  a timer count, and not an attempt at a challenge, whose places the friend
  who made it could have told. Names, "Hide me" and the mini profile work
  as in the world's tables. An account with Google alone has a row of its
  own under its site name; an account with Telegram is its person, the
  games its site players finished before the link included. The profile's
  free play tab shows the player's place on each board they are on and
  opens that board, and it counts the same games: a site account without
  Telegram now has the tab too. On a phone in a browser the page has its own
  Back before the title, to the page before or to the menu; in Telegram Back
  is Telegram's, and a computer has the header. Each row has a face, as in
  the day's table: the letter, or the Telegram photo when the Mini App was
  opened from a group and the player is in it, as in the mini profile;
  "Anonymous" has the plain face and never a photo. The first three of the
  last 7 days on a mode's screen have the same faces.
- The app's screens have addresses: the server answers `/days`, `/days/<day>`,
  `/me`, `/play`, `/party/<code>` and `/p/<id>` with the app, and the app opens
  the screen the address names. `/days` opens the days and `/days/<n>` (or
  the day's date) a closed day, so a day can be linked and Back in the browser closes it. An
  address without a screen shows the menu. In Telegram the address changes
  without adding history, so Back stays Telegram's.
- A menu at `/`. The day comes first and largest, with its picture: Play,
  the answer sent and the clock to the result, or the player's own score and
  world place. Beside it are the free game's modes, Classic, Blitz, Country
  streak, Against the clock, the archive and With friends, in the day's
  green with "no place in the world" over them, and under them the days,
  the leaderboards and the profile. The day itself is at `/today`. A post's
  button opens the day as before; the bot's menu button and `/start` open
  the menu, and so does the logo. A phone gets the menu in one column.
  Each card but the day's carries its sign in the logo's halftone dots, in
  the field's light green: Classic the globe itself, a bolt, a flame, an
  hourglass, an archive box, two pins, a calendar, a cup and a person. Now
  and then one light runs over all of them at once, and a card lights its
  sign up under the pointer or the focus. The cards' arrows are gone.
- Sign-in on the site with Telegram or Google, through our server by OpenID
  Connect with PKCE. Signed in with Telegram, the site is the bot's player:
  one answer a day across both, the DM's deadline with the speed bonus, the
  place right after answering and the names at the usual time. Signed in with
  Google, the site's game keeps its answers and name across browsers. The
  browser's guest answers join the account, after a question when the account
  already existed.
- An accounts page: link and unlink Telegram and Google, sign out here or on
  every device, delete the account. Two accounts merge when one's sign-in is
  linked to the other; a closed day is never rewritten. A guest's answer
  that an account with Telegram takes in after the person's deadline stays
  a site row, marked late, and the bot takes no other answer that day. In
  the Mini App,
  linking Google opens the phone's browser with a five-minute ticket that
  names the Telegram account before going to Google; a Google that has an
  account of its own is linked from the site instead, so a ticket never
  merges.
- `/privacy` says what maply keeps, what other players see and how to delete
  the account, in the sixteen languages: the day's game, free play and its
  leaderboards, "Hide me", games with friends and challenges, and the public
  link. To have the answers erased as well, it links the developer's
  Telegram.
- The site's menu picks the interface language. Signed in with Telegram it is
  the language core keeps for every bot of the family, with "Follow
  Telegram"; otherwise the browser keeps it.
- The days are a diary: every day back to the first, a month at a time, each
  with its strip of street, its flag and city, the world's numbers and the
  viewer's place, "no answer" on a day they skipped. A day still open
  somewhere shows its puzzle where the day has begun, how many have answered
  and when it closes, and nothing about its place. On a screen 1024 pixels
  wide or more the list and the day stand side by side.
- A closed day's page, `/days/<n>`: the strip, the place, the author and the
  fact, the viewer's result, the world's four numbers, everyone's misses on
  one scale, the map of the pins and the table, with arrows to the days
  before and after.
- A link to `/days/<n>` previews in a chat or a feed with a 1200×630 picture
  of the day's street, its place and numbers, drawn in the reader's language
  (in English for a script the faces lack). Only a day over in every zone
  and kept has one; the picture is served at `/og/day/<n>-<lang>.jpg`.
- The country flags come from `country-flag-icons` 1.6.20, served by the
  service.

- Free play, outside the day's ranking, at `/play`: from the menu and the
  DM's `/start` panel. Alone it is a Classic of 3, 5 or 10
  places on 60, 90 or 120 s or no timer, a Blitz on 30 s, or a Country streak
  on 30 or 60 s that goes on while the country is right. Each mode has its
  own screen, opened from its card in the menu: only its settings, on the
  last game's where they fit, the player's record and place on the
  settings' leaderboard, and the first three of the last 7 days. A game
  shows each place after the answer and ends with the record for those
  settings. Back goes one step up, and the settings, the archive and the
  lobby lead to the menu; in Telegram only Telegram's Back is shown. With friends, up to eight play the same places
  at once: the host sets the time and the number of places, the bot prepares
  an invitation the player sends to any chat, a ribbon shows who has
  answered, and the final offers a rematch on the same settings. The speed
  bonus, up to +200, counts the share of the time left, so it is the same on
  every timer. The bot writes nothing about free play in groups. A free
  place never becomes a day's place, and its picture goes out only once the
  place has started, under the round's own address. It looks as the day's
  game does: the same header, panorama and globe, with the place, its clock
  and the points where the day has its number and countdown. On the site any
  account keeps records and plays with friends, one with Telegram as the
  person and one with Google alone as its own player; a guest plays alone
  without records and is asked to sign in for them.
- The app has a computer's layout. From 1024 px wide and 600 px high the
  panorama fills the screen and the map opens beside it as a panel, on hover, a
  click or M, in three sizes; after answering the map fills the screen and the
  score, the world's table and the buttons are in a sidebar. A header links
  Play (the menu), Days and Leaderboards, with the profile or Sign in at its
  end. The menu, the days, the profile and the leaderboards are pages up to
  1440 px wide, in columns where numbers and tables do not wrap. On a tablet
  the phone's layout gets a bigger globe and centred buttons. The phone's
  layout is unchanged apart from the menu.
- Keys: the arrows or WASD turn the panorama, + and − zoom it, Space drops a pin
  on the focused map, Enter answers, Esc folds the map, ? lists the keys. Hints
  follow the pointer, so a mouse is no longer told to use a finger.
- Telegram Desktop can switch the app to full screen from its header, and the
  computer's layout follows.
- Free play gets three more ways to play. "Against the clock": three minutes
  for as many countries as one can find, with skipping and no speed bonus,
  and a record of its own. The archive: every day over in every zone, by the
  clock as well as by the final job's row, that the player has not answered
  or seen, one place each with no timer, and where the answer would have
  stood in that day's table. Each day says how many played it and the
  world's typical miss, as a target, and a day already played shows its
  points. A challenge: after a classic game on a timer,
  friends play its places in the same order in their own time, once each,
  and the challenge's table shows the sums and marks the places someone had
  seen before. A player there is the person with every site player their
  account holds: a day answered as a guest before signing in is answered,
  and a challenge taken before Telegram was linked is taken. Challenges
  need an account, as parties do; a site guest plays the archive.

### Changed

- The panorama and the globe moved out of the day's game into `pano.js` and
  `globe.js`, each with a small API that the free game and the profile can
  reuse. The game looks and behaves as before; a double click no longer zooms
  the panorama by itself.
- The days page reads a page of thirty days in one query instead of about
  sixty, and pages back by a cursor to the first day.
- A day's row no longer shows the group's numbers beside the world's.
- A DM post's button opens `/today`. A post sent before this version opens
  the app at `/`, which is now the menu, one tap from the day, and so does
  the Play button under a forwarded result. In Telegram,
  Back on the page the app opened with goes to the menu, and the menu has
  none, so Telegram closes the app from there.
- The free play card at the top of the days is gone: the menu leads there.
- The days and the profile open in place, as the menu and the leaderboards
  do, instead of sliding up from the bottom over the day's game.
- A page's back is one arrow instead of a cross. A step inside a page (a day
  to the list, a mini profile or the world's table to what it opened over)
  keeps it in a browser. The way out of the days, the profile or a public
  profile is not drawn on a computer, where the header leads out. In
  Telegram its own Back button does both, as in the free game, so a screen
  never has two ways back.
- The day's clock and table in the header show on the day's screen only, no
  longer over the days, the profile, a mini profile or the world's table.

### Fixed

- A day takes only a 360° panorama. The queue let an approved flat photo
  become a day; prod had 42 of them, now rejected by hand.
- The final world job keeps a day as over in every zone only once the
  latest reveal has passed, whenever the job runs. A job moved by hand or a
  clock that jumped would have put a day still in play into the free
  game's archive; it now goes back to the queue for its time.

- `maply-places` wrote a heading of 359.96° as `360.0`, which the schema
  refuses, and the whole `places.sql` failed to load. It is written as `0.0`.
- `pool.py` leaves out the west coast of Sinai and the Russian strip east of
  occupied Luhansk, which its rules let through, and a plan no longer dies
  or spins when every country is used up. `screen.py` checks a frame outside
  every outline by its plan's country instead of letting it pass.
- The result's pin, target and the others' marks stood off their points, up
  to 40 px from the dashed line on a computer, after the map opened straight
  on a city. They are set again once the camera stops.
- A click or a double click on the computer's map panel while it was opening
  left the globe small in the panel's corner. Once the camera stops, the
  globe comes back to the middle at its size, closer if the player zoomed in.

### Security

- The site took any zone the browser named. One east of every chat's, such
  as UTC+14, closed and scored a guest's answer before any chat's reveal. A
  zone outside the span of the zones a chat may pick now plays in the nearer
  of them: Honolulu in Los Angeles, Auckland in Sydney.
- The guest's cookie is `__Host-maply_player`, which another site under the
  same parent domain cannot set. A browser that has the old `maply_player`
  keeps its guest and gets the new cookie on its next visit; after that an
  old one is ignored.

## v0.0.1-alpha.15 - 2026-11-05

Fixes from a review of the whole project on 5 November: security, the
scheduler and the bot, the server's logic, the animations and the Mini App.

For the operator:

- Apply core migration 020 (`core.manual_language`) before deploying. The
  service now refuses to start without it and names the migration; before,
  every launch of the Mini App from a group would have failed.
- Migrations 014 to 016: `day_rank` puts site answers after Telegram ones,
  two partial indexes for unscored answers, and the tables `day_closed` and
  `day_place`, filled at once with every day already over.
- The new `deploy/ws04/compose.yaml` goes to the host: the bot's container
  is read-only with a 512 MB `/tmp`, without capabilities, limited to 2 GB
  and 512 processes, and gets `stop_grace_period: 90s`.
- `/healthz` no longer answers through the Cloudflare tunnel; Docker and the
  family's containers still reach it.

### Security

- Every site answer ranks after every Telegram answer. A Telegram player sees
  the place on answering, from the 07:00 post on, and every new browser is a
  new site player: someone could answer at random in Telegram, then post
  exact pins from fresh browsers and take the world's first place.
- A result poll with a made-up `since` showed the place without tying the
  person to the chat. They could then answer from a group whose reveal was
  hours later. The tie now happens whatever the poll sends.
- Per-address limits count an IPv6 address by its /64, the site's writes
  share a cap of 120 a minute from everyone, and a name typed before any
  answer no longer creates a site player.
- A forged button press could delete the group's afternoon intrigue or a post
  sent as text. maply acts only on a button the message really carries.
- The app may be framed only by Telegram's web clients; the site sends HSTS,
  API replies say nosniff, and two log lines no longer tie a Telegram id to a
  time zone. Both Docker base images are pinned by digest.
- Correction to alpha.14: a row's key differs per chat, but the rank and the
  distance are the same in every view, so they still match one person's rows
  for anyone who looks at a group and a DM at once.

### Changed

- Opened from a group, the Mini App speaks the language the person picked by
  hand in any bot of the family, such as Ukrainian chosen in vido, rather than
  the group's. A group's language used to win for everyone in it, so a player
  who had picked their own read the screen in the language another bot had
  detected for the group. The Telegram app's language does not count as a
  pick, so a group still reads as one for everyone who has not chosen. What a
  player shares from the app stays in the group's language, like the bot's
  posts and the card there.
- A group that does not let maply send pictures still plays: the post and the
  result go out as text with the same button.
- In a forum, `/start` answers in its topic, not in General. An admin who
  writes anonymously can open the group's settings, and any admin may press
  that panel.
- The morning animation is kept for two dates at once, since the east posts
  the next day while the west still posts today, and one chat that forbids
  animations no longer turns them off for every chat that day.

### Fixed

- In a supergroup every poll of the result screen after the first load got a
  server error since alpha.14, so the live table stopped updating. The query
  that answers "nothing changed" typed the chat id as a 32-bit number, and a
  supergroup's id does not fit. The API's integration tests now use real
  supergroup ids.
- A deploy around 20:00 could send a group its result twice: the stop cut off
  the send in flight. The job in flight now finishes, and jobs taken but not
  started go back to the queue.
- An animation with its buttons went out in two requests, and a failed second
  one sent the whole message again. They go in one.
- A group upgraded to a supergroup lost maply. The supergroup now takes the
  group's zone, post time, switches and the language picked in maply.
- A group whose admin restricted maply (no GIFs, say) was marked as left.
- A person who opened the Mini App from a shared result but never started the
  bot got a failed post every morning, retried five times. Their DM is now
  left until they write to maply.
- A group that removed maply and added it back the same morning, or a person
  who blocked and unblocked it, got nothing that day. The post goes out on
  the return.
- Moving a chat's zone a date east before its reveal sent tomorrow's post
  before today's result. Such a change waits until after the result.
- The closed day's world line could stay "for now" for good when Telegram
  failed that one edit, and was dropped when it did not fit the caption. A
  failed edit is tried again, and the line names fewer people until it fits.
- A query that broke the post's "who answered" line did it silently; it is
  logged now, once per chat and error. Pressing the setting a panel already
  shows is no longer logged as a failure.
- Four reads took any database error for "nothing there", which repeated
  yesterday's intrigue or shared the card instead of the animation.
- The time the world closes names its zone.
- Animations: pins land before the podium with many players, chips show the
  top 12, long names on the podium end in "…", an emoji with a skin tone or
  a keycap after a letter is drawn from the emoji font, and a site winner in
  Chinese, Japanese or Arabic reads "Guest", not "?".
- An encoding gives up after 3 minutes instead of holding every post behind a
  stalled ffmpeg, at most two encode at once, and ffmpeg's own message is
  logged when it dies. A panic in one statistics picture costs that picture.
- Statistics in Tokyo or Sydney show this morning's answer before UTC
  midnight. The result card joins Tokyo and California across the Pacific.
- Mini App: the loading screen no longer waits forever for the photo, a
  failed start can be retried, the map recovers from a failed tile request,
  lifting two fingers after a pinch no longer undoes the zoom, the countdown
  follows the server's clock, sideways the panels start folded, and the days
  sheet, the keyboard and screen readers behave.

### Performance

- A result poll keeps the place it has read, finds unscored answers through
  an index, and an open day's ranking and pins are read once per new answer
  for every poll that counted the same. alpha.14 said an unchanged poll was
  worked out before any table is read; it was not, and now it is cheap.
- A day over in every zone keeps its places, so the profile and the days
  page no longer rank every day there has been.
- The days page keeps a closed day's place and ranking for ten minutes, and a
  day's ranking sends at most 500 rows plus your own.
- The place's picture is served as `.jpg`, which Cloudflare caches, with a
  per-address limit.
- Statistics pictures are drawn ahead only for players who opened them in the
  last week and leave the disk after 6 hours; the shaped-text cache is capped;
  the afternoon intrigue is drawn once per picture and sent by file id.

## v0.0.1-alpha.14 - 2026-11-05

Fixes from two reviews on 4 November: one name could take the whole
service down, several paths let a player see the place and still answer,
the world's names came out too early, and the site showed an
empty table to players who had answered. The Mini App and the texts changed
too.

For the operator:

- Migration 013 adds `chat_settings.zone_auto` and `left_at`,
  `chat_round.result_text` and `result_world`, `site_guess.late`, three
  columns at the end of the `day_score` view and a new view `day_rank`.
- The deploy also needs the new `deploy/ws04/compose.yaml` on the host: logs
  move to the host's journal.
- Rolling back to alpha.13 works on the migrated database. One difference:
  alpha.13 does not know `zone_auto`, so a DM zone stored automatically
  counts there as picked by hand, and the phone's zone no longer replaces it.

### Fixed

- A name starting with a skin tone, a joiner or a variation selector crashed
  the whole service, bot, scheduler and API, every minute while profiles
  were drawn. Such names are drawn now, and a panic while drawing, in a
  request or in a job costs that one picture, request or job.
- Moving a chat's zone west in the middle of the day could send the place
  while answers in the new zone were still open. The result job checks the
  reveal in the chat's current zone and moves itself to it.
- A DM without a picked zone followed the language, so switching the
  language after a group's reveal could reopen the day. The zone is stored
  the first time it is used; the phone's zone may still replace it once.
- The site told players who had answered that they had not: its world table
  was empty. The table is filled, a guest without a name reads "Guest", and
  an answer kept only in the browser shows as your row without a rank.
- When the answer window closed on a pin that was placed but not sent, the
  app said "Answer accepted". It now says answers are closed and removes the
  pin.
- In the world table every group's names opened at your chat's reveal.
  Now only your group's do; everyone else's open when the day is over in
  every zone.
- The pin jumped to the top corner of the map for a moment when dropped.
- The mini globe grew with the pin's latitude: 1.4 times for Romania, almost
  3 times for Tromsø.
- The place and your pin could end up under the table. The camera follows
  when the table grows; on short screens the margins shrink together and the
  place card starts folded. The opening frame covers the place, your pin and
  the answers near them, not every answer in the world.
- Telegram asked whether to discard changes when closing the result screen.
- One hung request stopped the live table until the app was reopened.
  Requests give up after 10 seconds; a server that keeps failing shows one
  message and is asked less and less often, up to once a minute.
- "Everyone answered, the pins are 0 km apart" came with one answer.
- The morning edit of a group's result rebuilt the whole caption with that
  morning's members and language. It changes the world line only.
- In UTC+3 in winter the morning look back at yesterday could come before
  the last answers were scored.
- A failing edit of the pinned post was retried every 5 seconds until the
  evening. It backs off, waits as long as Telegram asks, and leaves a
  deleted post alone.
- A long fact could push the result caption over Telegram's limit.
- Two answers on the same spot made one marker.
- The bar tooltip stayed over someone else's row after a new answer moved
  the table, "N more" counted five rows when the table was folded to one,
  and the fold button showed when you had no row.
- The site wrote "at 06:30, " with nothing after the comma for a browser in
  a zone maply does not name.
- A group that removed maply after its reveal stopped counting for its
  members that day, so they could answer from a DM in a later zone after
  seeing the place. Such a group now closes their day as before; one removed
  before its reveal does not.
- Someone who opened a group's result without answering could still answer
  from a DM in a later zone. Opening a result now counts as being in that
  group.
- A site answer sent after the first reveal of the day, when the place was
  already out in Telegram, could take first place in the world. It is still
  scored, but it cannot be first.
- The world picture in a group's result named the world's top three and
  showed their faces before the day was over everywhere. Until then the
  podium shows distances only.
- Someone Telegram had restricted and who then left a group still counted
  as its member. A refused membership check is kept for a minute, so a person
  who has just joined gets in quickly.
- A DM whose own day had ended at midnight lost the result of the day one of
  the person's groups was still playing. The open app keeps its day.
- Your place on the profile and in the world could differ on equal points:
  the two broke ties in different orders.
- For a place near the 180th meridian the line from your pin and the opening
  frame went the long way round the world.
- After a tap on a group of markers, the next update flew the map back.
- A reply cut off in the middle was taken as a result; it now counts as a
  failed request and is asked again. Coming back to the app asks at once,
  even after a run of failures.
- A long tooltip over the bars ran off a narrow screen; it wraps now.
- A slow reply in the days sheet could replace a day opened after it.
- The days sheet did not take the focus. It does now, Escape closes it, and
  the focus goes back to where it was; the folded answer button is out of
  the tab order.
- A database error while reading a chat's zone was taken for "no zone", and
  one chat whose zone could not be stored stopped the day's planning for
  every chat.

### Changed

- Posts, teases, the home panel and the reply to a sent location name the
  zone: "Results at 20:00 (Kyiv)".
- A group's first post has two lines of rules in place of the look back at
  yesterday. When maply may not pin, the request goes to the admins inside
  that post instead of a fourth message.
- The world place reads "so far" until the day is over in every zone.
- Numbers in the Mini App follow the language, as in the bot ("3,500" in
  English, "3 500" in Russian and Polish); distances under a kilometre are in
  metres. Arabic keeps Western digits in the bot and the app, as its places
  and times already had.
- In Arabic the Mini App reads right to left: the table, the plates over
  the map and the arrows are mirrored.
- Long button labels take two lines instead of losing their end.
- The name column in the table grows with the screen.
- Your own face shows your initial instead of repeating "you".
- The sheet with every answer opens on your row, marks where ranks skip,
  and says "Top 200 of 1,229" when that is all it has.
- The (i) card explains the group mark and points at its button.
- Markers on the back of the globe hide; long names on the map are cut.
- New texts for a server that is down, a server error, a bad request and a
  group maply has left. Reworded: no photo today, the zone change refusal,
  the DM settings hint, the world tease (it says "worldwide" now), the empty
  waiting tease, the first toast after a pin, the rules in Chinese. The line
  about your group under a DM result ("In “Team”: place 2 of 5") says
  "place" in nine more languages, and Belarusian was read against Russian.
- The bot's log goes to the host's journal and survives deploys.
- After the reveal, the result in the Mini App asks once a minute until the
  day has closed everywhere, so the world's names appear without reopening
  the app.

### Security

- Bot posts call a site player "Guest": a name typed on the site is not
  checked and could have said anything.
- The site's endpoints that write take only JSON from maply's own pages, so
  another site cannot post an answer or a name for a visitor.
- A row's key depends on the chat it is shown in, so one person's rows cannot
  be matched between a group, a DM and the site.
- The client error log drops query strings, which could carry the Telegram
  launch data, and takes at most 5 reports a minute from one address and 30
  in all.

### Performance

- A poll sends the version it has; when nothing changed the answer is a few
  bytes, worked out from two counts before any table is read. Polls no
  longer write to the shared core database.
- The table draws only the rows that show, markers are not laid out while
  the photo is in front, and the compass tape is rebuilt only when the
  width changes.

## v0.0.1-alpha.13 - 2026-11-04

The table under the result is the day's world ranking, in a group and in a
DM, with a small mark on the group's own players. It folds to your own row,
and how the points add up sits behind a small (i). Migration 012 adds the
speed bonus to the day's ranking view; no table changes.

### Changed

- The table under the map is the world's, in a group and in a DM alike:
  every answer scored so far today, numbered by world place. The group's
  own players carry a small group mark by the face; their answers are still
  the ones on the map. Points include the speed bonus for everyone, so the
  order matches the world place.
- The table folds to your own row and remembers that. How the points add up
  is a small (i) that opens a card over the map instead of pushing the table
  down.

### Fixed

- In your own row the empty rest of the bar no longer blends into the
  highlighted band, so the bar reads as a whole.

## v0.0.1-alpha.12 - 2026-11-04

Group posts read more clearly: the pin lists every distance, the 20:00
result sets the world places apart, and the morning look back is a heading
and one fact a line. The result table shows the top five with every
player's bonuses, opens in full in its own window, and close answers group
on the map. The result animation names every pin and turns the globe to
answers on the far side. A DM time zone change can no longer reopen a day
already seen.

### Changed

- Zooming the map stops where the hand stops. The app no longer finishes a
  gesture on its own toward the globe or the flat map, and a pinch has no
  zoom inertia. Far out, a wheel click or a trackpad swipe covers more zoom
  than up close, so getting from the globe to a city takes fewer moves.
- A shared result puts the distance and the points on lines of their own, in
  bold, with labels, so neither is read as the other.
- The Mini App opened from a group speaks the group's language, the one the
  bot posts there in, instead of the language of the player's Telegram app.
  What a player shares from it is in that language too.
- The place plate on the result opens with the fact. Its button folds it
  into a small tab and back; the step that showed the name without the fact
  is gone.
- World lines say "Place in the world: 3 of 120" instead of "Worldwide so
  far", and the group's line puts the total first so it reads for everyone
  in the list. The world's first place says "worldwide".
- The group's pinned post lists every distance so far, closest first, not
  only the best three, and catches up with new answers within about 20
  seconds instead of a minute or more.
- The group's 20:00 result sets the world apart: a bold heading with the
  number of players, the members' places on the next line, and the world's
  first place on a line of its own. Points in the table are bold.
- The morning look back is a heading and one fact a line. A group no longer
  gets its members' world places there, since its result already has them.
- A DM's result shows the distance and the points on lines of their own,
  like a shared result.
- On equal points the closer pin ranks first, then the earlier answer, in
  the group's table, the live table and the world ranking alike. Before,
  the earlier answer came first in the world and the table could put a
  closer pin below a farther one.
- The result table under the map shows the top five, yours always among
  them, and "N more" opens every answer in a window of its own, which keeps
  up with new answers while it is open. Your row stands out on a lighter
  band with bigger points; everyone else's bar shows the bonuses they
  earned in muted colours, and the first three places are coloured.
- On the map, answers too close to tell apart group into one marker: the
  best of them with a count. A tap zooms in until the group comes apart.
- The others' markers stay put when an answer comes in: a new answer adds
  its own, and at the reveal the names fill the same markers. Before, every
  new answer rebuilt them all, which made them flicker and could cut off a
  pinch that started on one.
- The colour legend and the "what if" line under the table moved behind an
  (i) button: it explains the points for the distance, each bonus and the
  speed bonus, and what the same distance would have made with more
  bonuses.
- The live plate shows only the place in the world; the "names at 20:00"
  line with the blinking square is gone, since the header already counts
  down to the names.
- The result animation: every pin has a chip with the name and the distance,
  and chips move to another side of their pin rather than cover each other.
  When the group is spread over the world, the view centres on the place, a
  pin round the globe stands where its line goes over the edge, and the
  globe turns east and west to show those pins where they are.
- Russian, Ukrainian and English texts were gone over: the bot's
  description, the home message and the help said the place shows at 20:00,
  when it shows right after the answer (fixed in every language); one word
  for a miss, the world place said as such, Ukrainian compass letters that
  no longer read as Russian, and plurals that agree with any number.

### Fixed

- The map could stop working after "Walk there" on some phones: a frame
  stamped slightly before the animation's start turned the camera's padding
  negative, and MapLibre threw on every frame after that.
- Errors inside Telegram's own script, which the browser reports only as
  "Script error.", are no longer sent to the server log.
- Changing a DM's time zone, from the settings or from the phone, could
  move the day back before its reveal for somebody who had already seen the
  place, and let them answer again. A zone that would reopen the day now
  waits until tomorrow.
- The others' points in the live table no longer include the speed bonus,
  which gave away when they answered.
- A pinch on the map no longer resets when Telegram resizes the view: the
  camera waits until the finger is up and the map stands still.
- An app reopened after answering no longer lets a tap move the sent pin
  while the result loads, retries the result if it failed, and polls only
  while Telegram shows the app.
- A group result sent after the day closed everywhere carries the final
  world line itself.
- Long names no longer push the pinned post past Telegram's caption limit,
  and yesterday's lines give way first.
- The world's sample pins come in a fixed order rather than by answer time.
- /api/log takes only JSON, has a global limit as well as one per address,
  and drops empty reports.
- Sharing before the reveal scores a location answer first, so the line has
  its points.
- Toasts are announced by screen readers; unnamed rows and the collapse
  button say what they are.

## v0.0.1-alpha.11 - 2026-11-02

Players in Telegram see the place and their score as soon as they answer,
and watch the group's other answers come in without names until 20:00.

Operators: no migration and nothing to do on deploy.

### Breaking

- The anti-cheat rule changed: in Telegram an answer is scored the moment it
  comes in, and `POST /api/guess` returns the result instead of
  `{"status":"accepted"}`. Before the chat's reveal, `/api/result` gives the
  others' answers only as distance, score and a point, with no name, photo,
  ID, bonus, speed or answer time, and answers `403 not_answered` to anyone
  who has not answered. The site still waits for the latest reveal and shows
  no one else's answers before it.

### Added

- The live screen after answering: the place, the person's distance and
  score, the group's answers as unnamed rows and map points, their place in
  the world so far and everyone's answers as grey dots. It refreshes every
  five seconds while open, a new answer slides in, and the names appear at
  the chat's reveal without reopening the app.
- Sharing before the reveal sends a text line with the day, distance and
  score, without the picture of the place.
- The group's 20:00 result carries the members' places in the world so far.
  When the day closes everywhere, the bot edits that line into the final
  places and who came first in the world. The edit is not a new message.
- A DM without a chosen zone takes the phone's zone, matched to the closest
  zone in the picker with the same clock all year (Paris to Berlin, Riga to
  Kyiv). A zone the person picked is never overwritten.
- Errors in the Mini App are reported to the server log through
  `POST /api/log`, at most five per session, without the launch data.

### Changed

- The pinned post is laid out in blocks: who answered, in alphabetical order
  so the names cannot be matched to the live rows, the three best distances
  so far without names, and yesterday's summary in a quote.
- The result table gives medals to the first three.

### Fixed

- Changing the time zone in the middle of the day could open the result
  before the answers were scored and show 0 points.
- Emoji in names are drawn in the animations instead of a question mark, and
  an avatar without a photo shows the whole emoji, flags included.
- A long name no longer wraps onto a second line in the Mini App's result
  table.

## v0.0.1-alpha.10 - 2026-10-28

New places join the queue mixed with the old ones, and the place pool grows
with 148 panoramas away from big-city centres: outskirts, small towns and
country roads in 69 countries.

Operators: no migration. The new places and their images were loaded on
ws04 before this release; nothing is needed on deploy.

### Changed

- Places of the same difficulty come up in a fixed shuffled order instead of
  the order they were added, so a new batch mixes with the places left from
  the old ones instead of waiting behind them.
- The curation scripts collect 360° panoramas only, and plan a batch as big
  cities away from the centre, small towns and villages, and roads and
  nature (`scripts/curation/pool.py`).

### Fixed

- Dates in the JSON logs are written as the day, not as `{}`.

## v0.0.1-alpha.9 - 2026-10-27

Ukrainian and Belarusian posts are drawn in the usual Cyrillic letters.

Operators: nothing to do. Pictures already drawn and kept by file_id, such
as today's world animation, stay as they are until they are drawn again.

### Fixed

- Ukrainian and Belarusian text on the posts and the stats card is drawn in
  the usual Cyrillic forms. It came out in the font's Bulgarian ones, a в
  like a ϐ and a т like an m, since Sofia Sans has local forms for Russian
  only.

## v0.0.1-alpha.8 - 2026-10-26

The DM result shows the whole world's day, and sharing it sends everyone,
not one pin.

Operators: migration `011_world_card.sql` runs on start with
`AUTO_MIGRATE=true`: a `world_card` table, a `world_version` column on
`chat_round` and the job kind `world`. The world animation is drawn with
ffmpeg; without it DM results go out as before.

### Changed

- The DM result is the world animation of the day: every answer scored so
  far on one globe and the first three on the podium, with their photos.
  The caption keeps the person's own day and adds their place in the world
  so far. Sharing the result from the Mini App sends the same animation,
  so the shared post shows everyone, not one pin.
- Until the day closes in every time zone the result carries a Refresh
  button and an italic note on when the ranking is final. Refresh redraws
  the picture with the answers scored since, at most once every two
  minutes per language, and says when there is nothing new.
- Two minutes after the last zone's 20:00 the final picture replaces the
  one in every DM result, with the final place and without the button.
- The world animation writes distances under a kilometre in metres.

## v0.0.1-alpha.7 - 2026-10-26

Results show the players' faces, the DM result is about the person's own
day, and the texts are shorter.

Operators: migration `010_avatar.sql` runs on start with
`AUTO_MIGRATE=true`. The bot now calls `getUserProfilePhotos` and
downloads small profile photos, at most once a day per player. The
sharper place images are files in the images directory under the same
keys; no database change goes with them.

### Added

- Players' Telegram photos stand in for their letters: in the pins and on
  the podium of the result animation, on the result card and on the Mini
  App's map and table. A photo is asked of Telegram at most once a day per
  person and kept as a 128 px square; someone with no photo the bot may see
  keeps the letter. Migration `010_avatar.sql` keeps the photos.
- The DM result tells the person about their own day: where the points came
  from (distance, city, country and continent, speed), a new record or their
  usual miss, the days in a row, and their place in each of their groups.
- `maply-places -sharpen` fetches the original frames again: panoramas at
  4096×2048 and photos at 3072 px instead of Mapillary's 2048 px thumbnails.

### Changed

- The look back at yesterday is shorter: the first place, then the person's
  place or the group's places. The typical miss shows from 5 players and the
  share of players beaten from 10, since for two players both say nothing.
- Distances under a kilometre read in metres: "2 m", not "0.0 km".
- No em dash anywhere a person reads maply, and tests keep it so.

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
