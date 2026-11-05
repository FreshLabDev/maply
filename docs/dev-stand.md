# Local stand

`cmd/maply-dev` runs the real API, the Mini App and the site on a local
PostgreSQL, with Telegram players faked by signed launches and a clock that
can move. It is for development only.

```sh
scripts/dev-db.sh start
go run ./cmd/maply-dev -places ../curation/out-004,../curation/out
```

Open <http://127.0.0.1:8399/dev>. The page opens the Mini App as any of
eight players, from one of two groups (Kyiv and Tokyo) or from their DM, as
the phone or as Telegram Desktop; answers for the others through the API;
moves the server's clock to 20:00, to the day closing everywhere or to the
next morning; and wipes and seeds the stand again.

- **Database.** `-db`, or `MAPLY_DEV_DATABASE_URL`, default
  `scripts/dev-db.sh url`. The name must contain `test`: the stand truncates
  the maply schema on a fresh start. `scripts/dev-db.sh` keeps `maply_test`
  for the database tests and `maply_dev_test` for the stand apart, since the
  tests wipe their own.
- **Places.** `-places` takes curation output folders, each with
  `places.sql` and `images/` (and `sharp/`, preferred). They are outside git.
  Without it the stand makes synthetic places with drawn panoramas.
- **History.** `-days` past days (30 by default) are created by the store's
  own queue, answered by the players before their own deadlines, scored by the
  real judge and kept once they are over in every zone. One day in nine nobody
  played. Today the others have answered and Аня, the stand's owner, has not.
- **Restart.** A stand with data is kept as it was; `-fresh` or the button on
  `/dev` seeds it again at the stand's clock.

## Telegram on the stand

The stand serves a Bot API of its own at `/tgapi/`, and the real bot and the
real scheduler run against it (`-bot=false` leaves them out). Nothing goes to
Telegram. <http://127.0.0.1:8399/dev/tg> shows its chats as Telegram would:
the two groups and every player's DM with the bot, with the photos and
animations the scheduler posts, edits, pins and buttons.

- A player writes into a chat (commands as chips), sends a location near the
  day's place, taps callback buttons, and adds the bot to a group or removes
  it. These go to the bot as updates.
- A web_app button, the DM's menu button and the bot's `t.me/<bot>/play`
  links open the Mini App in a sheet as that player, signed as Telegram would
  sign the launch. The sheet speaks Telegram's iframe protocol: Back, close,
  fullscreen, `openTelegramLink` and `shareMessage`, which posts the prepared
  message into a chat the player picks.
- `t.me/<bot>?start=…` writes `/start …` into the player's DM;
  `t.me/share/url` asks where to send the text.
- The server refuses what Telegram refuses: HTML it would not parse, captions
  over 1024 characters, an edit that changes nothing ("message is not
  modified"), a message that is gone. The last Bot API calls and their errors
  are under the chat list.
- The weather is made up from the coordinates, so the places' coordinates
  never leave the machine; there are no profile photos, so the avatars are
  letters. The result card renderer is not run: results go out as the
  animation or the place's photo.

The chats live in memory: a restart or `/dev/fresh` starts them empty, and
`/dev/fresh` restarts the bot and the scheduler on the new data.

## Extensions

A feature adds its own part in its own file under `cmd/maply-dev/` with
`Register(Extension{…})`: a seed that runs after the shared one, handlers
under `/dev/<name>/`, a block on the `/dev` page, and a `Start` that runs once
the stand listens (and again after `/dev/fresh`). `Env` gives the store,
the clock, the players, the launch URLs and the whole handler, so a fake
provider or a bot player calls the real API.

The fake bot token signs everything, so nothing from the stand verifies
anywhere else, and nothing real verifies on the stand.
