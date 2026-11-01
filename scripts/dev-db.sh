#!/usr/bin/env bash
# Starts or removes the development MySQL 9.7 server that .env.dist's DATABASE_URL points at.
#
# Usage: scripts/dev-db.sh up|down
#   up    starts docker.io/library/mysql:9.7 as the container bardic-torch-mysql, published on
#         127.0.0.1:3306 with database bardic_torch, user bardic_torch and password pass, and waits until it
#         accepts TCP connections. Does nothing more if it is already running.
#   down  removes the container and its data.
#
# CONTAINER_ENGINE selects podman (the default) or docker.
set -euo pipefail

readonly engine=${CONTAINER_ENGINE:-podman}
readonly container=bardic-torch-mysql
readonly mysql_image=docker.io/library/mysql:9.7
readonly ready_timeout_seconds=120

# These must match DATABASE_URL in .env.dist.
readonly database=bardic_torch
readonly user=bardic_torch
readonly password=pass

fail() {
  echo "dev-db: $*" >&2
  exit 1
}

mysql_ready() {
  # The image's entrypoint runs a temporary server without networking while it initialises, so only a TCP
  # ping as the application user means the real server is up and the user exists.
  "$engine" exec -e MYSQL_PWD="$password" "$container" \
    mysqladmin ping -h 127.0.0.1 --protocol=tcp -u "$user" --silent >/dev/null 2>&1
}

up() {
  if [ -n "$("$engine" ps -aq --filter "name=^${container}\$")" ]; then
    "$engine" start "$container" >/dev/null
  else
    "$engine" run -d --name "$container" \
      -e MYSQL_RANDOM_ROOT_PASSWORD=yes \
      -e MYSQL_DATABASE="$database" \
      -e MYSQL_USER="$user" \
      -e MYSQL_PASSWORD="$password" \
      -p 127.0.0.1:3306:3306 \
      "$mysql_image" \
      --character-set-server=utf8mb4 --collation-server=utf8mb4_0900_ai_ci >/dev/null
  fi

  local deadline=$((SECONDS + ready_timeout_seconds))
  until mysql_ready; do
    if [ "$SECONDS" -ge "$deadline" ]; then
      "$engine" logs "$container" >&2 || true
      fail "timed out waiting for MySQL in container $container"
    fi
    sleep 1
  done
  echo "MySQL is ready on 127.0.0.1:3306 (database $database, user $user)"
}

down() {
  # -v also removes the image's anonymous data volume, so the next up starts from an empty database.
  "$engine" rm -f -v "$container" >/dev/null 2>&1 || true
}

case "${1:-}" in
  up) up ;;
  down) down ;;
  *) fail "usage: $0 up|down" ;;
esac
