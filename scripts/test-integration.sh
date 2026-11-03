#!/usr/bin/env bash
# Runs the integration test suite against MySQL 8.4.
#
# Usage: scripts/test-integration.sh podman|local|external
#   podman    starts a throwaway docker.io/library/mysql:8.4 container.
#   local     starts a throwaway mysqld from the mysql-server 8.4 prefix in MYSQL_PREFIX (see
#             make mysql-local-install); needs no container engine.
#   external  uses the running server at TEST_MYSQL_ADMIN_URL (mysql://user:password@host:port, no
#             database), which must already have a bardic_torch database, as in CI.
#
# Every mode then waits until the server accepts connections, applies the migrations to bardic_torch with
# cmd/migrate, and runs go test -tags integration plus any flags in GOTESTFLAGS (default none; CI sets
# -race). Each test creates and drops its own database through TEST_MYSQL_ADMIN_URL. Throwaway servers listen
# on a random free 127.0.0.1 port and keep their files in a private temporary directory, so concurrent runs do
# not collide; both are removed on exit.
set -euo pipefail

readonly database=bardic_torch
readonly mysql_image=docker.io/library/mysql:8.4
# Both throwaway servers trade crash safety for speed: their data is discarded on exit, and creating the
# catalogue schema for each test database spends most of its time waiting for disk syncs.
readonly throwaway_server_flags=(
  --skip-log-bin --innodb-flush-log-at-trx-commit=0 --innodb-doublewrite=OFF --innodb-flush-method=nosync
)
readonly ready_timeout_seconds=120
# go test's -timeout is per package binary, and go test's default of 10m is too tight for
# internal/datasources/mysql under -race on a 2-core CI runner against a server with default durability: it takes
# about 340 s on a 20-core machine, mostly creating migrated databases, which serialise on the server. 20m leaves
# a margin of several times that and still stops a hung test. GOTESTFLAGS may override it with its own -timeout.
readonly test_timeout=20m

mode=${1:-}
workdir=$(mktemp -d)
admin_url=""
mysqld_pid=""
container=""
root_password=""

cleanup() {
  if [ -n "$mysqld_pid" ]; then
    kill "$mysqld_pid" 2>/dev/null || true
    wait "$mysqld_pid" 2>/dev/null || true
  fi
  if [ -n "$container" ]; then
    podman rm -f "$container" >/dev/null || true
  fi
  rm -rf "$workdir"
}
trap cleanup EXIT

fail() {
  echo "test-integration: $*" >&2
  exit 1
}

# free_port prints a 127.0.0.1 TCP port that nothing is listening on.
free_port() {
  local port
  for _ in $(seq 1 100); do
    port=$((20000 + RANDOM % 40000))
    if ! (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
      echo "$port"
      return
    fi
  done
  fail "no free port found"
}

# wait_until runs a command once a second until it succeeds, failing after ready_timeout_seconds.
wait_until() {
  local deadline=$((SECONDS + ready_timeout_seconds))
  until "$@"; do
    if [ "$SECONDS" -ge "$deadline" ]; then
      fail "timed out waiting for: $*"
    fi
    sleep 1
  done
}

# database_ready reports whether the bardic_torch database accepts connections.
database_ready() {
  "$workdir/migrate" version >/dev/null 2>&1
}

start_podman() {
  local port
  port=$(free_port)
  root_password=$(od -An -tx1 -N16 /dev/urandom | tr -d ' \n')
  container=$(podman run -d \
    -e MYSQL_ROOT_PASSWORD="$root_password" \
    -e MYSQL_DATABASE="$database" \
    -p "127.0.0.1:$port:3306" \
    "$mysql_image" \
    --character-set-server=utf8mb4 --collation-server=utf8mb4_0900_ai_ci "${throwaway_server_flags[@]}")
  admin_url="mysql://root:$root_password@127.0.0.1:$port"
}

podman_mysql_ready() {
  # The image's entrypoint runs a temporary server without networking while it initialises, so only a TCP
  # ping means the real server is up.
  podman exec -e MYSQL_PWD="$root_password" "$container" \
    mysqladmin ping -h 127.0.0.1 --protocol=tcp -u root --silent >/dev/null 2>&1
}

wait_podman() {
  wait_until podman_mysql_ready
}

start_local() {
  local prefix port
  prefix=$(cd "${MYSQL_PREFIX:?MYSQL_PREFIX must point at a mysql-server 8.4 prefix}" && pwd)
  port=$(free_port)
  mkdir "$workdir/data"
  echo "CREATE DATABASE IF NOT EXISTS $database;" >"$workdir/init.sql"

  "$prefix/bin/mysqld" --no-defaults --initialize-insecure --user="$(id -un)" \
    --basedir="$prefix" --datadir="$workdir/data" --log-error="$workdir/initialize.log" ||
    { cat "$workdir/initialize.log" >&2; fail "initialising mysqld data directory"; }
  "$prefix/bin/mysqld" --no-defaults --user="$(id -un)" \
    --basedir="$prefix" --datadir="$workdir/data" \
    --port="$port" --bind-address=127.0.0.1 --mysqlx=OFF \
    --socket="$workdir/mysqld.sock" --pid-file="$workdir/mysqld.pid" --log-error="$workdir/mysqld.log" \
    --character-set-server=utf8mb4 --collation-server=utf8mb4_0900_ai_ci "${throwaway_server_flags[@]}" \
    --init-file="$workdir/init.sql" &
  mysqld_pid=$!
  admin_url="mysql://root@127.0.0.1:$port"
}

local_mysql_ready() {
  if ! kill -0 "$mysqld_pid" 2>/dev/null; then
    cat "$workdir/mysqld.log" >&2
    fail "mysqld exited"
  fi
  (exec 3<>"/dev/tcp/127.0.0.1/${admin_url##*:}") 2>/dev/null
}

wait_local() {
  wait_until local_mysql_ready
}

start_external() {
  admin_url=${TEST_MYSQL_ADMIN_URL:?TEST_MYSQL_ADMIN_URL must be set in external mode}
}

wait_external() {
  # The server is already running; the database_ready wait below confirms it accepts connections.
  :
}

case "$mode" in
  podman | local | external) ;;
  *) fail "usage: $0 podman|local|external" ;;
esac

go build -o "$workdir/migrate" ./cmd/migrate

"start_$mode"
"wait_$mode"

export DATABASE_URL="$admin_url/$database"
wait_until database_ready
"$workdir/migrate" up
"$workdir/migrate" version

# GOTESTFLAGS adds go test flags, split on whitespace; CI sets -race, which needs cgo. The ${...+...} form
# expands an empty array to nothing; bash before 4.4 (macOS's 3.2) treats a bare empty "${array[@]}" as unbound
# under set -u.
read -r -a extra_test_flags <<<"${GOTESTFLAGS:-}"
TEST_MYSQL_ADMIN_URL="$admin_url" go test -tags integration -count=1 -timeout "$test_timeout" ${extra_test_flags[@]+"${extra_test_flags[@]}"} ./...
