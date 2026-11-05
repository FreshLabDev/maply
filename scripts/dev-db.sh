#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# A local PostgreSQL for the stand (cmd/maply-dev) and the database tests.
#
#   scripts/dev-db.sh start   # create on first use, then start
#   scripts/dev-db.sh stop
#   scripts/dev-db.sh url     # the stand's database URL
#
# MAPLY_DEV_PG (default ~/.cache/maply-dev/pg) is the cluster's folder and
# MAPLY_DEV_PG_PORT (default 55491) its port. It listens on 127.0.0.1 only,
# trusts local connections, and holds maply_test for the database tests and
# maply_dev_test for the stand: the tests wipe their database.
set -eu

DIR=${MAPLY_DEV_PG:-"$HOME/.cache/maply-dev/pg"}
PORT=${MAPLY_DEV_PG_PORT:-55491}
export LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8

case "${1:-start}" in
start)
  if [ ! -f "$DIR/PG_VERSION" ]; then
    mkdir -p "$DIR"
    initdb -D "$DIR" -U maply --auth=trust >/dev/null
    echo "dev-db: created $DIR"
  fi
  if ! pg_ctl -D "$DIR" status >/dev/null 2>&1; then
    pg_ctl -D "$DIR" -o "-p $PORT -k '' -h 127.0.0.1" -l "$DIR/server.log" -w start >/dev/null
  fi
  for db in maply_test maply_dev_test; do
    psql -h 127.0.0.1 -p "$PORT" -U maply -d postgres -Atc "SELECT 1 FROM pg_database WHERE datname = '$db'" | grep -q 1 ||
      createdb -h 127.0.0.1 -p "$PORT" -U maply "$db"
  done
  echo "dev-db: running on 127.0.0.1:$PORT"
  ;;
stop)
  pg_ctl -D "$DIR" -w stop >/dev/null && echo "dev-db: stopped"
  ;;
url)
  echo "postgres://maply@127.0.0.1:$PORT/maply_dev_test?sslmode=disable"
  ;;
*)
  echo "usage: $0 start|stop|url" >&2
  exit 2
  ;;
esac
