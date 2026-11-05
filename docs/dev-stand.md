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

A feature adds its own part in its own file under `cmd/maply-dev/` with
`Register(Extension{…})`: a seed that runs after the shared one, handlers
under `/dev/<name>/`, and a block on the `/dev` page. `Env` gives the store,
the clock, the players, the launch URLs and the whole handler, so a fake
provider or a bot player calls the real API.

The fake bot token signs everything, so nothing from the stand verifies
anywhere else, and nothing real verifies on the stand.
