# maply: design

Draft of 20 October 2026. No code yet.

maply is a daily "guess the place from a photo" game in Telegram and on the web. Every morning all players get the same place, put a pin on the map, and learn the answer in the evening. maply also gives Asterfield its first website.

## Daily cycle

There is one place per calendar date for everyone: DMs, groups and the site. Days are numbered from launch: `maply #1`, `maply #2`.

The reveal is 20:00 in the chat's own time zone. A group admin picks it in the settings, a person picks the zone of their DM. Until someone does, it follows the capital of the language (`core.effective_language`), as quoto does: Russian and any language without a zone of its own go by Kyiv (owner decision of 22 October 2026).

| Language | Time zone |
|:--|:--|
| ru, uk | `Europe/Kyiv` |
| be | `Europe/Minsk` |
| en | `Europe/London` |
| es, fr, de, it, pl, cs, sv, ca | `Europe/Berlin` (one rule for Central Europe) |
| tr | `Europe/Istanbul` |
| ar | `Asia/Riyadh` |
| zh | `Asia/Shanghai` |
| ja | `Asia/Tokyo` |

A group's zone is fixed in `chat_settings` the first time maply is set up there, so a later change of the group's language does not move its reveal. A DM's is fixed the same way the first time the person opens maply or the day is planned, marked as automatic: the phone's zone may replace it once, and a zone the person picked never moves. The picker offers sixteen zones from Los Angeles to Sydney; a zone outside the list is never stored.

Posts and teases write the reveal with the chat's zone, "Results at 20:00 (Kyiv)", so a reader in Moscow does not take it for their own 20:00.

Zones come from tzdata and offsets are not hard-coded: Ukraine's daylight saving rules have been changed by law. In winter Kyiv is an hour behind Moscow and Minsk, and the game accounts for that (see "Anti-cheat").

| Time | Group | DM |
|:--|:--|:--|
| 07:00 | Photo of the day, "Guess" button, silent pin | Photo of the day and "Guess" button |
| 07:00–20:00 | The bot edits the pin: who has already answered, alphabetically, and every distance so far, closest first, without names; it looks every few seconds and edits at most every 15 s | none |
| Right after answering | The Mini App shows the place, the person's score and the group's answers without names, updating live | The place and the person's score |
| Random time in 16:00–18:00 | One intrigue message, if not everyone has answered | One intrigue reminder, if the person has not answered |
| 20:00 | Unpin, publish the result | The result, if the person is not in any group with maply |

A group can move the morning post to another time. The answer reveal time is fixed: anti-cheat depends on it.

The bot writes at most three messages a day to a group: the morning post, the intrigue and the result. Edits to the pin and to the result do not count as messages. A group's first post carries two lines of rules in place of the look back at yesterday. When maply may not pin, the admins' note goes into that day's post as an edit, once.

## How players answer

The "Guess" button opens a Mini App. The photo or panorama is on top, the map is below, the player puts a pin and taps "Done".

- In a DM this is a `web_app` button.
- `web_app` buttons do not work in groups, so a link button leads to `t.me/maplyin_bot/play?startapp=<payload>`. The payload holds the round date and the chat, signed with HMAC so that a foreign chat cannot be substituted. The bot additionally checks chat membership through `getChatMember`.
- Geolocation sent straight to the group is not accepted: everyone would see the other player's pin.
- Answering with a location in the bot's DM is available as a fallback if the Mini App does not open.

An answer cannot be changed after "Done".

### Scoring

The score for the pin has two parts, 5,000 at most:

- **distance:** `3500 · e^(−d / 1492.7)`, where `d` is the distance in kilometers. The curve is the same as GeoGuessr's for the world map: 500 km gives about 2,500, 2,000 km about 900;
- **bonuses** for the pin landing on the same continent (+300), in the same country (+500) and in the same city (+700). Bonuses form a ladder: the country counts only together with the continent, the city only together with the country.

Bonuses make a correctly guessed country visible: a pin right at a border but on the wrong side gets 800 points less. The server determines country and continent from Natural Earth polygons, and the city from the OSM administrative boundary. On the result screen the strip under the header shows these parts separately, and the total builds up together with it.

On top of that comes a **speed bonus** for answering early, counted back from the person's deadline (20:00 of the earliest zone among their chats): +250 at the 07:00 post, falling evenly to +100 at 16:00, when the afternoon intrigue starts; after that from +50 down to nothing at the deadline. Rounded to tens. It is a nudge, not a race: 250 is the difference between a pin about 110 km and one about 230 km away, and nothing next to the country bonus. The result screen shows it as a gold part of the bar after the bonuses. Answers scored before the bonus existed have none.

On equal scores, whoever answered earlier ranks higher.

Personal hints in the Mini App:

| Hint | Score ceiling after it |
|:--|:--|
| Continent | 4,000 |
| A second view of the same place | 3,000 |

Whoever answered before the intrigue and without hints gets a "clean" mark and +10%.

## Anti-cheat

1. **One answer per person per day.** The answer is shared across the DM, all groups and the site (when signed in through Telegram). A person's pin appears on the maps of all their groups.
2. **The answer window closes at the earliest reveal.** For each person, accepting answers ends at the earliest reveal among their contexts: the DM and all groups where the bot has seen them (presence in `core`). Example: a person in a Ukrainian group who is also in a Russian one must answer before 20:00 Moscow time in winter, not Kyiv time. A group that removed maply still counts that day if it did so after its reveal: its members saw the place there. Opening a group's result, answered or not, adds the person to that group's presence, so someone who looked at the place in a group cannot then answer from a DM in a later zone.
3. **Your answer opens the place, the reveal opens the names.** In Telegram an answer is scored the moment it comes in. The person sees the place, their distance and their score right away, and the world's table without names, updating live: every answer scored so far today, ranked as the world ranks them, with distance, points and the bonuses in them (speed included, so the order matches the world place), and a small group mark by the face of the chat's own players. The map shows the chat's answers as points. The server strips names, photos, IDs and answer times; the client never gets them. This is light hiding to keep the evening's intrigue, not a privacy promise. Folded, the table keeps only your row; the choice is remembered. Names appear at the chat's reveal (20:00 in its zone). In a DM that is the latest reveal among the person's contexts. In the world's table only the chat's own players are named then; everyone else stays unnamed until the day has closed in every zone, so a group in Tokyo does not learn who is who in a Kyiv group still guessing. Until then the world place reads "so far". Before the reveal, sharing sends the line "maply #142 · 312 km · 4,057" with no picture. A row's key depends on the chat it is shown in, so the key does not link one person's rows between a group, a DM and the site; the rank and the distance are the same in every view and still match them for anyone who looks at two at once.
4. **Nobody sees the place without answering.** Before the reveal, `/api/result` and `/api/share` answer someone who has not answered with 403 `not_answered` and nothing else.
5. **The site does not spoil.** On the site's own game, played by a guest or by an account with Google alone, today's place is revealed only after the latest reveal across all languages, and the site shows no one else's answers before then: a guest could answer at random, look where the dots cluster, then answer precisely in Telegram. The reverse is closed too: a Telegram player sees the place on answering, from the morning post on, so every site answer ranks after every Telegram one in the world. A name typed on the site is not checked, so bot posts call such a player "Guest". A browser signed in with Telegram is not the site's game: it is the person's Telegram player, under rules 1 to 4 with the DM's zone, and it writes nothing to `core`, so it neither ties the person to a chat nor puts them on the morning post. Without an answer it waits for the latest reveal anywhere, as a guest does.
6. **The source does not leak.** The photo is re-uploaded under a random name and without EXIF. Neither Mapillary IDs nor Panoramax links go out: given a Mapillary ID, the API immediately returns coordinates, and Panoramax HD frames contain GPS in EXIF (checked). Panoramas are shown by our own viewer (Photo Sphere Viewer), not MapillaryJS.
7. **No defense against image search.** Google Lens finds streets poorly, and the rest relies on honor, as in Wordle. A leak through friends from another chat cannot be closed either.

## Daily result

At 20:00 the group gets one message:

- a map with the true point and the pins of everyone who answered; the picture is rendered by the service from its own result card page (see docs/mvp-plan.md, decision 1);
- the place: city and country (reverse geocoding through Nominatim) and one short fact, checked during curation;
- a table: medals for the first three, distance, score, streak, and how many of the group answered;
- a line with the group's places in the world so far. When the day has closed everywhere, the bot edits that line into the day's final world places and, if no one in the group came first, who did. The edit is not a new message.

### World ranking

Everyone who answered a day stands in one list, whether they played in a group, in a DM or on the site. Groups are not ranked against each other: the owner decided on 23 October that groups will compare by forwarding their results, and that the day's statistics matter more.

A day is final only after the last zone's reveal, 20:00 in Los Angeles, which is 06:00 in Kyiv. Before that the world is shown only as "so far": on the live screen after answering ("Place in the world so far: 12 of 120", with everyone's points as grey dots) and in the group's 20:00 result, whose world line is edited when the day closes. The day after:

- the morning post carries yesterday's lines under a heading: first place with the distance, then the median distance and the share who found the country;
- in a DM it adds the person's place: "Your place in the world: 12 of 348, ahead of 97% of players";
- a group gets no member places there: its 20:00 result already has them, made final by the world job;
- a chat whose morning comes before the day has closed everywhere (Dubai and east) gets the same lines with that evening's result instead.

Ties go to the closer pin, then the earlier answer, in the world and in a group's table alike.

A line for forwarding: `maply #142`, then the distance and the points on lines of their own, each with its label.

### Animated posts

The posts are short seamless loops, sent as MP4 without sound, which Telegram plays as a GIF:

- morning: the logo's pin lands on its globe, the day's panorama opens around it as a little planet, unrolls into the street, pans, and rolls back into the globe;
- intrigue: the time there on a dial of the day, dark or light as it is there, with the weather and the dots left until the answer;
- the group's result: the camera flies to the group's pins, the place comes out with a ripple, dashed lines run to every pin along the great circle, each pin gets a chip with the name and distance, and the three closest stand on a podium. When the pins are spread over the world, the view centres on the place, a pin round the globe stands where its line goes over the edge, and the globe turns east and west to show them;
- the world: everyone's pins rain onto one globe and take the colour of how close they came, and the world's first three stand on a podium. It is the same picture in every chat; a person's own place goes in the caption. Before the day has closed in every zone the podium shows distances only, with no names or faces.

They were designed in a browser canvas; `internal/anim` draws the same frames in Go on the CPU, with no browser or GPU:

- the halftone dots of the globes are anti-aliased ellipses computed per pixel, 4° dots for the logo's globe and 1° dots for the map, both from the countries of `internal/geo`;
- text is shaped by HarfBuzz rules (go-text) in Sofia Sans with the chat's language set, since the font draws Bulgarian Cyrillic unless told otherwise; it has local forms for Russian only, so Ukrainian and Belarusian are shaped as Russian; the streak's 🔥 is drawn as a flame, because the brand's faces have no emoji;
- the panorama is projected per pixel with trilinear mip sampling at twice the size and averaged down;
- frames are drawn in parallel, one per core, and piped in order to ffmpeg: libx264, CRF 22, yuv420p, BT.709, `+faststart`.

The frame at the loop's length equals the first one, and a test holds it to that. Measured on an M1 Pro with 10 cores, encoding included (`go run ./cmd/maply-anim -pano pano.jpg`):

| Loop | Frames | Time | CPU per frame | File |
|:--|--:|--:|--:|--:|
| Morning, 14 s | 420 | 4.9 s | 100 ms | 2.4 MB |
| Intrigue, 12 s, 720×720 | 360 | 0.6 s | 3 ms | 0.3 MB |
| Result, 12 s, 10 pins | 360 | 1.6 s | 19 ms | 5.2 MB |
| World, 12 s, 348 pins | 360 | 1.8 s | 26 ms | 2.8 MB |

How the scheduler sends them:

- the morning post of a panorama place is drawn once a day, uploaded once, and sent to every other chat by its `file_id`; a photo place keeps the photo;
- the intrigue is drawn when it tells the time there, per chat, since the dots left until 20:00 are the chat's own; the other teases stay text;
- the result is drawn per chat, and falls back to the map card and then to the photo;
- sharing the result from the Mini App sends the animation the chat's result went out as, by the `file_id` kept with it (`mpeg4_gif` in `savePreparedInlineMessage`); a result that went out as a picture is shared as the map card;
- an upload goes as a document, which Telegram turns into an animation since the MP4 has no sound, because tg has no `sendAnimation` yet;
- names and places in scripts that Sofia Sans lacks (Arabic, Chinese, Japanese, emoji) are written in English or left out of the picture; the caption keeps them;
- the world loop has no slot yet: the world's numbers come in the morning post's caption.

### Statistics picture

The statistics panel shows a 24-second loop above its text: the Earth turns once under the person's pins of every day (the best day as a gold pin), beside their points, days played, streak, typical miss (the median), best day, place in the latest final world ranking, best world place, and a strip of the last 14 days. Only the Earth moves. Labels come in the person's language and shrink to fit; a language whose script Sofia Sans lacks is drawn in English, as on the posts.

- The picture is named by a fingerprint of everything on it. Telegram's `file_id` for it is kept in `profile_card` per person and language, so a press with unchanged numbers draws nothing.
- Every 10 minutes the bot draws, one at a time and with two workers, the missing pictures of everyone who answered in the last two days, in their own language. Numbers change when someone answers (the streak), at the reveal and when a day's world ranking becomes final.
- A press with no picture ready shows the text at once and puts the picture in when it is drawn, if the panel still shows the statistics.
- `editMessageMedia` turns the text panel into the animation. A picture cannot turn back into text, so Back sends the panel again and deletes the picture. Buttons under an animation act only on a statistics picture this process put there; a forged Close against the morning post or the result is ignored.

## Intrigue 16–18

One message at a random moment of the window. Rules:

- An intrigue never lies. Every message is a real hint or real statistics.
- The type changes every day and does not repeat two days in a row.
- Material for place-bound types is prepared during place curation and goes through the same check.
- If everyone in the group has answered, and there are at least two answers, there is only one type: "everyone answered, spread 6,000 km, waiting for 20:00".

| Type | Example | Source |
|:--|:--|:--|
| It is now there | "At the photo's spot it is 02:14 now and 27 °C." | Open-Meteo (free for non-commercial use) and the point's time zone |
| Voice of the place | A voice message: how locals say "good morning" | TTS in the place's language, prepared during curation |
| Detail | A crop from the photo: "Look closely at this sign" | A crop marked during curation |
| Chat spread | "Your answers are spread over 11,000 km. Two are on different continents" | The chat's answers |
| World | "Most pins worldwide so far: Asia (64%)" | All answers of the day |
| Rival | "'Workhorses' have already scored 18,400. You have 9,100, and four are silent" | The rival chat from `/rival` |
| Streak | "@masha, 11 days in a row. You have not answered yet today" | Streaks of 5 days or more, can be turned off |

In a DM the intrigue arrives only if the person has not answered yet. The "Chat spread" and "Rival" types are not used in DMs.

## Groups

- **First day.** Right after being added, the bot runs a warm-up round on yesterday's place with the answer the same evening, so the group does not wait until tomorrow morning.
- **Permissions.** The game works without the right to pin messages. The bot tells the admins once that it is more fun with a pin.
- **`/rival`.** Links two chats into a duel: a shared season score and the "Rival" intrigue.
- **Seasons.** A calendar month. At the end the chat gets a champion of the month, and the ranking resets.
- **Settings** through buttons in `/start`, as in quoto: morning post time, intrigue on/off, streak mentions on/off.

## Free play

Next to the day's game, which keeps every rule above, there is free play: alone or with up to eight friends, as many places as one likes, with no place in the world and no stats shared with the day. It is called "Free play" and opens at `/play`: from the menu, from the DM's `/start` panel and from a friend's invitation. It looks as the day's game does, the same header, panorama, globe, caption and bottom strip, and after an answer the same plates on a phone and the same side panel on a computer; only what is counted differs: the place of the game ("Place 2/5") where the day has its number, the place's clock where the day has its countdown, and the points. The menu's cards open the settings on a mode, `/play?mode=classic|blitz|streak|clock|archive`, and `/play?with=friends` opens a lobby; Blitz is the classic on 30 s. The bot writes nothing about it in groups, so the three messages a day stay three.

### Alone

The settings screen starts on the last game's settings, and a game begins in one tap.

| Mode | Places | Time per place | Ends |
|:--|:--|:--|:--|
| Classic | 3, 5 or 10 | Blitz 30, 60, 90, 120 s, or no timer | After the last place |
| Country streak | Until a miss | 30 or 60 s | Wrong country, a pin in the sea, or the time running out |
| Against the clock | As many as fit | 3 minutes for the whole game | The three minutes running out |

Without a timer a place ends with the answer or with "Give up", which scores 0 and shows the place. A streak has no "no timer": without one it turns into a search for the answer on the internet. After each place the player sees it with the distance, the points and their parts and the fact, and goes on when they like. The final shows the sum, or the streak's length, and the record for those settings.

Against the clock the score is the number of countries found in three minutes. A wrong country does not end the game, a place can be skipped, and there is no speed bonus: the clock is the game. Every place ends when the game does, so the time runs on through a place's reveal, and a place cut by the end counts as missed.

A place is three requests: start, answer, picture. Starting writes when the place began and when it ends, and only then gives the picture; asking again gives the same picture and the same time left, so a reload does not reset the clock, and the place counts as seen from then on. The answer is taken until the end plus 2 s for the network. A game nobody touches closes after a day.

### With friends

Everyone plays the same places at the same time. The host picks the time per place (Blitz 30, 60, 90 or 120 s, no "no timer": one player who walked away would hold everyone) and 3, 5 or 10 places; guests see the settings and cannot change them, and the lobby shows how long the game takes if everyone answers at the end of the time. Starting sends the settings' version, and the server refuses a start on settings someone has changed since.

The bot prepares an invitation, "maply · Blitz · 5 places · 30 s" with a button, and the player sends it to any chat. The button opens the Mini App in the lobby, also for someone who never started the bot. A place shows a ribbon of who has answered, without points; at zero a pin already set goes by itself. Then the place, everyone's pins with names and the points stay for 8 s on a blitz and 10 s on the longer timers, and a place everyone has answered closes early. A player who stops polling for 10 s is not waited for, and one who missed two places in a row is marked away until they answer again. A lobby whose host is silent for 30 s passes to the next player by joining time; a lobby lives 30 minutes.

The final has the standings, "Ahead of 67% of the others" in place of an average place (2nd of 3 and 2nd of 8 do not compare), the closest pin of the game, "Rematch" into a new lobby on the same settings, and "Share". With friends needs an account: a Telegram person, in the Mini App or on the site, or an account with Google alone. The invitation from the site is the link to `/party/<code>`, since the site has no `shareMessage`.

### The archive

Past days of the daily game, one place each and no timer, from the free screen's "Archive". Only days in `day_closed` are there, over in every zone, and the clock is checked as well: a day comes in only once its last reveal has passed, so a row written early never opens a place still in play. A player takes a day once; a day they answered in the bot or on the site, and a day whose place came up in their free play, are shown closed. A player here is the person with everything their account holds: a day answered as a guest before signing in, or taken as the account's site player before Telegram was linked, is theirs too. Taking a day starts its place, which counts as seen from then on, and taking it again gives the same game. After the answer the player sees where they would have stood in that day's table, "4 of 12", counted on the points without the speed bonus, a tie counting as behind; the day's real table does not change. A site guest plays the archive as the browser's player.

### Challenges

A challenge is a party for friends who are not online at once. After a classic game on a timer the player taps "Challenge friends": the game's places, in the same order and on the same settings, become a challenge with a code, and the game is their attempt. The invitation goes out as a party's does, `startapp=c<code>`. Everyone who takes it plays an attempt of their own, once, on their own clock: a place starts when they start it, as in a solo game. Once means once a person: an attempt made as the account's site player before Telegram was linked is the person's, and they cannot take it again as the Telegram person. Unlinking Telegram makes two players of one, as a second account would. The challenge's table shows names and sums only, never a place, and lists the attempts still going and the ones given up after a day. A place the player has had before is played all the same and marked in the table: the places were drawn for the maker, and someone who plays a lot will know some of them. Challenges need an account, as parties do; the free screen lists the ones from the last seven days, and the site's link is `/play?c=<code>`. "Hide me" does not hide a name in a party or a challenge: the people there chose to play together, as in a group, which keeps the name too. Taking one records a Telegram person with `TouchPerson`, never as a DM. A day from the archive waits for its answer however long it takes: it is taken once, and a game closed unanswered would lose the day.

### Points and records

The judge is the day's: up to 3,500 for the distance and the bonuses for continent, country and city. The speed bonus is up to +200 a place and counts the share of the time left, not the seconds, so the same share earns the same bonus on every timer; it scales with the distance points, so a quick random click earns nothing:

```
left  = clamp(ends_at − answered_at, 0, T)
share = min(1, left / (T − 2 s))      -- the first 2 s load the picture
speed = round10(200 × share × points_for_distance / 3500)
```

No timer, an answer in the 2 s of grace and a pin the app sent at zero earn none. With friends, equal points rank by fewer kilometres in all, then by less time in all; a missed place counts as half the Earth's circumference and the whole time, or silence would look like a quick answer. Records are kept for each set of settings, "60 s · 5 places", "Country streak · 30 s" or the countries found against the clock, from finished games, alone and with friends together, since the counting is the same. On the site an account with Telegram is the person, the same games and records as in the Mini App, and writes nothing to core: the sign-in has recorded them. A day from the archive keeps no record: it has its place in the day's table instead. Any account keeps records and plays with friends. An account with Google alone plays as its own site player, so its games and records follow it from browser to browser. A site guest plays alone and keeps no records; the free screen says "Sign in to keep your records and play with friends" with the sign-in button. A result belongs to a Telegram person or to a site player (`free_result.user_id` or `.player`), never both.

### Leaderboards

The page `/top` (`leaders.js`, `GET /api/top`) ranks each player's best on a board, over the last 7 days or all time:

| Board | Score | A table for each |
| --- | --- | --- |
| Country streak | the longest streak | timer, 30 or 60 s |
| Against the clock | countries in 3 minutes | (one) |
| Blitz | the game's points, 30 s a place | length, 3, 5 or 10 places |
| Classic | the game's points | timer, 60, 90 or 120 s, and length |

Only games with a timer count: without one a place can be looked up at leisure. An attempt at a challenge does not count either, as the friend who made it played its places first and could have told them; a day from the archive has no timer. A game with friends counts as a game alone does: the places and the clock are the same for everyone. One row a player, their best, and an equal result ranks whoever reached it first, as the day ranks equal points by who answered first. The page shows the first hundred and the viewer's own row. Names, "Hide me" and the mini profile are the world table's: a hidden player is "Anonymous" with a plain avatar outside their own groups, and a handle is scoped to its board, period and chat. A result is a Telegram person's or an account's site player's. An account with Google alone has a row of its own, every site player it holds in it, under its site name and without a mini profile; an account with Telegram is its person, and the games its site players finished before the link count for the person. A site guest keeps no free results and is on no board. The profile's free play tab lists the player's place on every board they are on, all time, and each opens its board.

### Places

A free place never becomes a day's place, and free play never sees a future day's. It takes places from the free pool, which the day's queue does not read (`pool = 'daily'` there), and from days closed in every zone, by the clock as well as by `day_closed`, that none of the game's players answered, as any player their account holds, or took from the archive: a day taken there waits for its answer, and its place never comes back as a repeat with the answer. A place none of the players has had comes first, in a country the game has not had yet if there is one, and the free pool before a closed day; only when there are none left does a repeat come, the one had longest ago, marked as a repeat.

A closed day is public: the days page shows its place, its answer and its picture, and the free game serves the same picture under another address, so a player who has seen the days page may know it. That is why the free pool goes first, and why free play has no world and no stats: it is the price of more places, not a hole in the day.

With friends, the next place is drawn when the one before closes, for the players who are in the party then. Someone who joins in the reveal or the countdown after it would get a place drawn without their history, so one they have had (answered its day, or had it in another free game) waits out as a place already begun, and they play from the place after it.

The picture goes out under the round's own address, the game, the place's number and an HMAC, and only once the place has started, never under the place's image key: the keys of closed days are on the days page. The place's coordinates, name and fact come in the answer alone, and with friends when the place closes.

## "Where am I?" (after launch)

A participant sends the bot their own photo in a DM and puts a point on the map. Setting the point is mandatory: Telegram strips EXIF from photos sent as photos. The bot turns this into an extra round in the chosen group, usually on weekends: "Guess where Artem was". The author sees a warning not to mark their home and can round the point to the city.

## Site

One frontend for two modes:

- **Mini App.** The user comes from `initData`, and the backend verifies the signature.
- **Website in a browser.** Without signing in, the browser keeps a random player id in a cookie; the day's one answer is stored and goes into the world ranking under the name the player gave, or "Guest". There are no streaks or personal stats on the site, and no speed bonus: the browser names its own zone, and the bonus is counted from the zone's deadline. A zone west or east of every zone a chat may pick plays in the nearer of those two, so a site answer never closes before the first chat's reveal. Signed in with Telegram, the browser plays as the bot's player instead (below).

Site sections:

- **Menu**, at `/`. The day comes first and largest, "maply #31" with its picture: Play with the clock to the end of answering; "Answer sent" or "Answers closed", "result at 20:00", with the clock to it; the player's own score and world place, with the clock to the names while they are hidden; or the day's result for someone who did not answer. After the reveal the picture carries its author and licence. Beside it are the free game's modes, with "no place in the world" over them and the same green: Classic, Blitz (Classic on 30 s), Country streak, Against the clock, the archive and With friends, each opening `/play` on its settings (`/play?mode=…`, `/play?with=friends`). Under them are the days, the leaderboards and the profile. Each of these cards carries its sign as a halftone, 18 dots by 18 like the logo's globe, in the field's light green and lit from the upper left: Classic the globe itself, a bolt, a flame, an hourglass, an archive box, two pins, a calendar, a cup and a person. A sign stands whole in the card's corner, never cut by its edge. The signs stand still; every 14 seconds one light runs over all of them at once, and the card under the pointer or the focus lights its own up. Without motion only that remains. A phone has one column. The bot's menu button and the `/start` panel open the menu; a post's button opens the day, as it always did: a DM's carries `/today`, a group's its signed payload, and a start with a payload goes to the day. The Play button under a forwarded result has no payload and opens the menu, the day first on it. The logo opens the menu.
- **Today**, at `/today`. Anyone can play. After answering, instead of the place there is "answer at 20:00, get it in Telegram" and a button to the bot.
- **Days.** The same page in the Mini App and on the site, opened from the menu and the header, at `/days`. A diary: every day back to the first, newest first, under month headings. A closed day's row has a strip of its street (the band from 8° below the horizon to 22° above, facing the curated `thumb_yaw`), the flag and the city, the world's numbers and the viewer's place, or "no answer"; a day nobody played says so. A day still open somewhere shows its puzzle's picture where the day has begun, how many have answered, when it closes and a button to play, and nothing about its place. On a screen 1024 pixels wide or more the list and the open day stand side by side.
- **A day's page**, `/days/<n>`, for a closed day only: the wide strip and the panorama to look around, the place, the author and the fact, the viewer's result, the world's four numbers (players, median, best, share in the right country), everyone's misses on one logarithmic scale, the map of the pins without names, the ranking with an icon for Telegram or the site, and arrows to the days before and after.
- **Archive.** Past days with the answer shown immediately.
- **Result picture** for forwarding (OG image). A link to a closed day's page previews with a 1200×630 picture drawn by `internal/anim`: the day's street, `maply #N`, the city and country and the world's four numbers, in the reader's language (`?lang=`, else `Accept-Language`) or in English for a script Sofia Sans lacks.

The site is a single game page. An ecosystem landing page is not part of it.

### Phone, tablet and computer

The same page lays itself out by the window:

- **Phone**, under 768 px wide or under 500 px high (a phone on its side): as described above.
- **Tablet**, from 768 px wide and 500 px high, short of the computer's: the phone's layout with a 168 px globe, the buttons at most 560 px wide and centred, the days sheet at most 640 px.
- **Computer**, from 1024 px wide and 600 px high: a header with Play (the menu), Days and Leaderboards in the middle and the profile or Sign in at the end; the logo opens the menu too. The menu, the days, the profile and the leaderboards are pages up to 1440 px wide, laid out in columns so that numbers and tables do not wrap. While guessing, the panorama fills the screen and the map is a panel in the bottom corner. It folds into the globe and opens on hover, a click or M, and folds 600 ms after the mouse leaves unless it is pinned or the keyboard is in it. It comes in three sizes, 480×360, 640×480 (the default from 1440 px) and half the screen but at least 768 px wide, kept in the browser. The answer bar is under the map and says "Drop a pin first" until there is one, then names Enter when there is a keyboard. A touch screen this wide gets the same layout: a tap on the globe opens the map and a tap on the panorama folds it. After answering, the map fills the screen and the result is in a sidebar: the place and its fact, the score with its parts, the world's table, the legend and the buttons. A row lights its pin on the map, and a pin its row.

Hints follow the pointer: a mouse is told to drag with the mouse and to use the wheel, a finger to drag and to pinch. On a computer:

| Key | Does |
|---|---|
| Arrows, WASD | turn the panorama while held |
| + and −, 0 | zoom the panorama, reset the zoom |
| N | face north |
| M | open or fold the map |
| Space | drop a pin in the middle of the focused map |
| Enter | answer, once a pin is down |
| Esc | fold the map, close the list of keys |
| ? | the list of keys |

Letters work in any keyboard layout. Space never answers: it is the key a player presses by accident.

Telegram Desktop opens the Mini App in a 384×694 window, which gets the phone's layout with the mouse's hints. Stretched past 1024 px, or full screen from the header's button (Telegram Desktop and the web clients, Bot API 8.0), it gets the computer's. The game keeps its state across the switch: the pin, the open map, the result.

Temporary domain: `maply.ws04.cc`.

### Accounts on the site

Planned by the owner on 23 October, built after v0.0.1-alpha.15. A site player can sign in with Telegram or with Google, and link the two. Accounts live in maply, not in `core`: an account has at most one Telegram and one Google sign-in, and never none.

- **Sign-in goes through our server.** Both are OpenID Connect: the code flow with PKCE, a fresh state and nonce each time, the state in a signed ten-minute cookie bound to the browser's session. Telegram's is `oauth.telegram.org` with `openid profile`, the account being the `id` claim; Google's asks for `openid` alone, so maply never sees an address, a name or a photo. The session cookie lasts 90 days from the sign-in and is not extended; signing out works here or on every device. The owner's setup is in [`sign-in.md`](sign-in.md).
- **Telegram makes the site and the bot one player.** One answer a day across both, the DM's zone and deadline with the speed bonus, the place right after answering and the names at the usual time. The site writes no presence, so signing in never puts a person on the morning post; a button leads to the bot for that.
- **Google alone plays the site's game**, as a guest does, but keeps its answers and its name across browsers.
- **The guest's history comes along.** Signing in for the first time takes in the browser's guest answers; signing in to an account that already exists asks "Add N answers from this browser?" first.
- **Linking.** On the site, from the accounts page. In the Mini App Google refuses to sign in inside Telegram, so maply issues a five-minute ticket, the phone's browser opens with it and asks "Link Google to the Telegram account Name (@username)?" before going to Google. There are no `/start link_…` links.
- **Merging.** A sign-in that belongs to another account merges the two, unless both have a sign-in of the same kind. A closed day is never rewritten. In an open day the earliest site answer stays, and becomes the Telegram answer when both the answer and the merge come before the person's Telegram deadline; merged later, it stays a site row, since by then the person may have seen the place. That row is marked late and is the person's answer of the day: the bot takes no other, from any chat. Linking from the Mini App's ticket never merges: a Google that already has an account is linked from the site instead.
- **Unlinking** never removes the last sign-in, and ends the sessions opened by the one removed and the account's tickets. An account that unlinks Telegram plays the site's game again: the same day it may answer there once, ranked after every Telegram answer, as a new browser could.
- **A sign-out everywhere** ends every session and every ticket of the account, and a link under way with them.
- **Deleting** erases the account and its sign-ins at once. Answers given on the site without Telegram stay as "Guest", so nobody's place in a past ranking changes; the Telegram player's answers, from the bot and from the site, stay with that player. What is kept and what other players see, free play, its leaderboards and the public link included, is on `/privacy`, in the sixteen languages; erasing the answers as well is asked of the developer, whose Telegram the page links.

## Profile

Planned by the owner on 5 November (plan v2, "Профиль игрока"). In the Mini App the profile opens from the name's letter at the end of the header, and its address is `/me`; the logo opens the menu. A site guest gets a profile from the cookie with a note to sign in so the history is not lost. Signed in on the site with Telegram, the profile is the Telegram player's, settings included; with Google alone it is the account's site days from every browser it took in, one a day, and its free play. A Telegram player's profile also counts the site days their account took in on the days without a Telegram answer, so the guest's history stays with the sign-in. Such a day counts once it is over everywhere: before that the guest's pin would show the place of a day the person can still answer in a chat whose reveal is ahead. A guest's answer given after the day's first reveal never counts, as the person may have seen the place in a chat by then. On the site the account's button is the header's only one: its menu opens the profile, and a guest's sign-in dialog has a link to it.

- **The player.** Two tabs. The daily game: total points, the 30-day average, days, the current and best streak, the typical miss (median km), the share of countries and cities found, the best day, the best world place, and the average percentile, "better than 78% of players". Players are compared by percentile, not place: each day has a different number of players, and site players have no speed bonus, so the percentile counts points without it. Charts drawn in SVG: points over the last 30 rounds with gaps and the median, misses in bins from 1 to 2,500 km, continents with the best and the worst, and a map of one's own pins where a tap opens that day. Under them the passport (countries found turn gold; stamps for 30 days in a row and a city pin within 1 km), the history by day and the settings. Free play shows only means, medians and records, never sums: games are unlimited, and a sum would reward quantity. Its place on each leaderboard is there too.
- **Others, on a tap on a name.** Name and letter, days, streak, the 30-day average, best world place, countries; from free play the average per place and duels. Only days over in every zone count. Inside a group both are in, the photo and "closer than you on 14 of 22 shared days".
- **Hide me.** In shared tables and lists the player is "Anonymous" with a plain avatar instead of the name and photo, and the mini profile does not open: the world's tables in the Mini App and on the site, the days page, the free play's leaderboards, and the bot's world lines in a DM and in other groups (yesterday's first, the world's first in a group's result, the month's world champion). The person's own groups and the people in them still see the name, as the group's table, card and posts show it. The world's podium picture is one for every DM of a language, so it names nobody who hid. The owner widened it on 6 November from "Telegram player" in the site's tables.
- **The month's season.** The sum of a player's 20 best days in the month, so missed days cost nothing while twenty remain; Telegram players only, as site days have no speed bonus and unchecked names. The world's season and each group's, over the people maply has seen there. The champion goes into the look back of the month's last day: the 07:00 post of the 1st, or the result when that post went out before the day closed everywhere. A group hears its own champion, a DM the world's and the person's place. The profile shows the month so far over the days over everywhere: the world's place and, opened from a group, the group's.
- **Public link.** Off by default. Turned on, it is `/p/<random>`; "New link" ends the old one, and "Hide me" closes the page until the player shows again. The page is a stranger's view with the charts: the closed days' numbers, the passport and free play, no pins, history, percentile, photo or id, and `noindex`. Its link preview is the profile picture of the bot's `/profile` without the pins, one frame drawn in Go.

## Where places come from

### Sources

| Source | Key | License | What it provides |
|:--|:--|:--|:--|
| Mapillary | client token, READ only | CC-BY-SA, author credit | Main source: frames at 64 of 68 points, good panoramas at 53 |
| Panoramax | not needed | CC-BY-SA-4.0 | Secondary: strong in Europe, sometimes fills Mapillary gaps (panoramas in Patagonia) |
| "Where am I?" | none | author's rights | Players' photos, only in their groups |

The Mapillary key is in `maply/.env` (`MAPILLARY_TOKEN`), the file is excluded from git.

Mapillary API limits (per the documentation as of 20 October 2026):

- a `bbox` search accepts an area smaller than 0.01 square degrees, `limit` up to 2000;
- rate limits: Entity API 60,000 requests per minute, Search API 10,000 per minute, vector tiles 50,000 per day;
- `mly1_public` tiles at z14 have `id`, `is_pano`, `quality_score`, `captured_at`. So it is convenient to look for candidates by tiles and to fetch the picture (`thumb_2048_url`) through the Entity API.

### Panoramax measurement, 20 October 2026

68 points: 57 cities and 11 rural places on all continents, a window of about 7×7 km around each.

- Photos exist at 51 of 68 points, panoramas at 20.
- Empty: Central Asia (Almaty, Tashkent, Ulaanbaatar), the Middle East and North Africa (Dubai, Cairo), Lagos, Accra, Lima, São Paulo, Kathmandu, Jakarta, most rural points.
- Of 47 random frames, about 31 are fit for the game: a street with architecture, signs, markings. 16 are not: menus, plaques that spell out the place in text, night, blurry frames, a view from a train window.

Random frames: [`panoramax-sample.jpg`](img/panoramax-sample.jpg). The measurement scripts are in `scripts/sources/`.

### Mapillary measurement, 22 October 2026

The same 68 points, 3×3 z14 vector tiles for each (about 7×7 km), 612 tiles in total out of the daily limit of 50,000.

| | Panoramax | Mapillary |
|:--|:--|:--|
| Points with frames | 51 | 64 |
| Points with panoramas | 20 | 54 |
| Points with panoramas of `quality_score ≥ 0.7` | none | 53 |
| Usable frames on review | ~31 of 47 | ~54 of 64 |

- In cities there are from tens of thousands to one and a half million frames per point, and from hundreds to hundreds of thousands of panoramas. Africa, Asia and Latin America are covered: Lima, Bogotá, Jakarta, Hanoi and Manila each give over 100,000 panoramas.
- No panoramas, only regular frames: Almaty, Tashkent, Delhi, Cairo, Lagos, Accra, rural Hokkaido, Kansas, the Outback.
- Nothing at all: Castile, Scotland, Lapland, Patagonia. Remote areas are weak in both sources. In Patagonia Panoramax gave 86 panoramas where Mapillary is empty, so it is worth keeping both sources.
- What spoils frames: organization captions over the image (`OSLO KOMMUNE`), a cropped strip panorama, dusk, a blank wall, an empty field. On almost every panorama the car hood or bicycle handlebar is visible at the bottom.

Random frames: [`mapillary-sample.jpg`](img/mapillary-sample.jpg). For panoramas the sheet shows the cropped center.

Conclusion: Mapillary is the main source, Panoramax the secondary one. For cities there are enough places for years. Hard rounds in remote areas should be assembled along roads where imagery exists, not from random points.

### Curation

There is one round a day, so about 365 places a year are needed. They can be curated by hand:

1. **Candidates.** A script picks random z14 tiles from a weighted list of regions, so there are not ten Frances in a row. Filters: a panorama or a good regular frame, `quality_score ≥ 0.7`, captured in 2018 or later.
2. **LLM scoring.** A model with vision rates "interesting" and "guessable" on a scale of 1–10 and rejects frames with text that names the place, captions over the image, night and blank walls. It also proposes a difficulty, a fact for the result and a detail for the intrigue.
3. **Operator.** In a service chat, each candidate has "yes", "no" and "difficulty" buttons. The queue is filled at least 30 days ahead. If the queue has fewer than 7 days, the bot warns the operator.

The hood and handlebar at the bottom of a frame are no reason to drop a place. For a regular frame the bottom is cropped, and for a panorama the viewer starts with the gaze slightly above the horizon and limits the downward tilt.

Difficulty by weekday: Monday is easy (a recognizable city), Friday is hard (remote areas), weekends are for "Where am I?" and themed rounds.

Russia, Ukraine and Belarus are in the game (decision of 22 October 2026). Excluded: the front line, Crimea and occupied territories, military sites, and frames with recognizable faces. Mapillary blurs faces and plates itself. Whether Panoramax does so has not been checked.

### The free pool

The free game (solo and party) draws from a pool of its own: `place.pool = 'free'`. The day picks only `pool = 'daily'`, so a free place never becomes a place of the day, and the free game never sees a future day.

The pool is curated by the same scripts and the same rules as the places of the day, with these differences:

- **No facts, one city name.** There is no curator to write names and facts in sixteen languages. The city is stored as `{"en": …}`: the English name of the area whose outline gives the city bonus, or the local name where OSM has no English one. The client names the country itself with `Intl.DisplayNames`.
- **The two pools keep apart.** No free frame is a frame of the day, and none is within 10 km of a place of the day or of any candidate of a daily batch, where a spare frame may still go to the queue. A daily batch keeps 10 km from the free pool the same way. `MAPLY_AVOID` in `pool.py`, `harvest.py` and `screen.py`, and `-avoid` in `maply-places`, take the other pool's `places.sql` and `candidates.json` files.
- **A wide spread.** At most 15 places a country, more countries than the daily batches (`pool.py` weights), and the scene mix of batch-004: a fifth city outskirts, two fifths towns and villages, two fifths roads and nature. No two frames of a batch closer than 5 km.
- **More checks before the eyes.** `harvest.py` passes over a soft panorama, or one under 2048 px wide, for another frame of the tile. `screen.py` drops frames within 300 m of an OSM military area or inside one, frames in a part of a country left out, and draws the review sheets as the whole 360° band, so a face or a fence behind the camera is in front of the reviewer too. The daily batches showed only the 120° the game opens on.

City outskirts are the hard part of the mix: the daily batches took the big cities, so the 10 km around their places leave little, and outskirts have few panoramas. The first pool (`batch-free-001` and `-002`) has 500 places in 99 countries, but only 28 city outskirts against the hundred the mix asks for; towns (216) and roads (256) make up the rest.

The pipeline, for `curation/batch-free-001`, with a virtualenv's Python (`-I` leaves out the user's site-packages) that has shapely, Pillow and mapbox-vector-tile:

```sh
pool.py pool-free-001.json 160 270 270          # MAPLY_CAP=20, MAPLY_AVOID=…
harvest.py batch-free-001                       # MAPLY_POOL=pool-free-001.json, MAPLY_AVOID=…
screen.py batch-free-001                        # Overpass, then pano-K.jpg
# reviewers write review-K.json from the pano sheets
choose.py batch-free-001 batch-free-002         # MAPLY_TARGET=500: reviews.json, report.md
maply-places -pool free -avoid … -out out-free batch-free-001 batch-free-002
maply-places -out out-free -sharpen out-free/sharpen.csv
faces.py sheets out-free batch-free-001 batch-free-002
# reviewers write out-free/faces/review-K.json from the face sheets
faces.py verdicts out-free batch-free-001 batch-free-002   # then choose.py and maply-places again
```

The review sheets show the panorama at about 4.4 px a degree and the game at 11.4, so a face too small to judge on a sheet may be clear in the game. `faces.py` gives every prepared place one more sheet, the two halves at 8.7 px a degree, for faces alone; a place dropped there is replaced by a spare and the spare gets its sheet in turn.

## Architecture

```text
maply (Go, one service on ws04)
  ├─ bot: long polling through tg, round scheduler, intrigues, results
  ├─ HTTP API, site statics and place images (Cloudflare Tunnel, maply.ws04.cc)
  └─ curation: CLI and the operator's service chat

frontend (statics from the same service on ws04): game, archive, OG images
core-postgres: maply schema, identity, language and presence from core
image storage: re-uploaded photos and panoramas without EXIF
```

Main tables of the `maply` schema:

| Table | What it stores |
|:--|:--|
| `place` | source, ID in the source, coordinates, author and license, image key, difficulty, intrigue and result materials, curation status |
| `round` | date, day number, place |
| `guess` | date, `user_id`, point, distance, score, hints, time, where the answer came from |
| `chat_settings` | post time, intrigue, mentions, rival |
| `chat_round` | post `message_id`, intrigue and result times, pin state |

Streaks, seasons and rankings are computed from `guess` and not stored separately.

## Avatar

A map pin: a round head and a tip at 45°, with a round cutout inside and a stencil gap. The mark is built from circles and straight lines at 45°, and the `brand/` rules are met.

Candidate color: forest green `#245A3C`, hue 146°, contrast with white 8.1:1. By hue it is the farthest from the other bots: from branchy's olive (77°) and makeitMD's petrol (185°). It needs to be checked next to them on a dark theme.

## Name

maply. The bot's username is `@maplyin_bot`, confirmed by the owner on 22 October 2026. `@maply_bot` and `@maplybot` are taken.

## Open questions

1. A permanent domain. For now the site temporarily lives at `maply.ws04.cc`. On a move, `/setdomain` in BotFather (Telegram Login), links in results and the app address in Mapillary need to change.
2. ~~Time zone for languages other than ru, uk and be.~~ Decided on 22 October: the chat's own zone, picked by a group admin, with the language's capital as the default.
3. Whether "Voice of the place" is needed at launch. Without TTS six intrigue types remain, which is enough for a week.
