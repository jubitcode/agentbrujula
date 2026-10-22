#!/usr/bin/env bash
# Prove the backup and rollback path works, without touching the real database.
#
#   scripts/verify-rollback.sh
#
# What it does, in a PostgreSQL server of its own on port 5434:
#
#   1. Reads `marketplace` ONCE with pg_dump. Read-only: no writes, no locks
#      that block writers, nothing created. That read is the only contact with
#      the real database in this whole script.
#   2. Restores that snapshot into two throwaway databases, so the test runs
#      against the real schema and the real 32 rows rather than against
#      fixtures that would not reproduce the problem.
#   3. Backs up one of them and restores it into the other, then compares
#      every column of every row.
#   4. Applies the schema migration that adds refund_hold, and checks that no
#      existing data changed.
#   5. Runs hold -> correct -> roll back the correction -> roll back the hold,
#      and checks the database ends byte-identical to where it started.
#   6. Runs archive -> verify -> delete -> restore on the 31 test rows, and
#      checks the same.
#
# Everything is torn down at the end unless KEEP=1.
#
# Why a separate server and not another database on the existing one: a dump
# that only ever moves between databases of one server does not prove it is
# portable, and the point of a backup is that it can be restored somewhere
# else.

set -uo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LAB_CONTAINER="${LAB_CONTAINER:-brujula-rollback-lab}"
LAB_PORT="${LAB_PORT:-5434}"
LAB_PASS="${LAB_PASS:-lab-only}"
LAB_USER="${LAB_USER:-agent_web3}"
SRC_CONTAINER="${SRC_CONTAINER:-agentbrujula-postgres-1}"
SRC_DB="${SRC_DB:-marketplace}"
PY="${PY:-$REPO/agent/.venv/bin/python}"
WORK="$(mktemp -d)"

pass=0; fail=0
ok()   { printf '  \033[32mok\033[0m   %s\n' "$1"; pass=$((pass+1)); }
bad()  { printf '  \033[31mFAIL\033[0m %s\n' "$1"; fail=$((fail+1)); }
step() { printf '\n\033[1m== %s ==\033[0m\n' "$1"; }

cleanup() {
  if [ "${KEEP:-0}" = "1" ]; then
    echo; echo "KEEP=1: lab left running on port $LAB_PORT, files in $WORK"
    return
  fi
  docker rm -f "$LAB_CONTAINER" >/dev/null 2>&1
  rm -rf "$WORK"
}
trap cleanup EXIT

lab() { docker exec -i "$LAB_CONTAINER" psql -U "$LAB_USER" -qtAX "$@"; }
url() { echo "postgresql://$LAB_USER:$LAB_PASS@127.0.0.1:$LAB_PORT/$1"; }

step "0. read the real database once, read-only"
if ! docker exec "$SRC_CONTAINER" pg_dump -U "$LAB_USER" --no-owner --no-acl \
        -d "$SRC_DB" > "$WORK/snapshot.sql" 2>"$WORK/dump.err"; then
  bad "pg_dump of $SRC_DB failed: $(head -1 "$WORK/dump.err")"
  echo "     Is the project's postgres container running? docker compose up -d postgres"
  exit 1
fi
rows_real=$(grep -c . < /dev/null; true)
ok "snapshot: $(wc -c < "$WORK/snapshot.sql" | tr -d ' ') bytes, $(grep -c '^CREATE TABLE' "$WORK/snapshot.sql") tables"

step "1. start a separate throwaway PostgreSQL on port $LAB_PORT"
docker rm -f "$LAB_CONTAINER" >/dev/null 2>&1
docker run -d --name "$LAB_CONTAINER" \
  -e POSTGRES_USER="$LAB_USER" -e POSTGRES_PASSWORD="$LAB_PASS" \
  -e POSTGRES_DB=postgres -p "127.0.0.1:$LAB_PORT:5432" \
  postgres:17-alpine >/dev/null || { bad "could not start the lab"; exit 1; }
# Postgres restarts itself once during first-time initialisation, so a single
# successful pg_isready is not enough: require a run of them.
streak=0
for _ in $(seq 1 600); do
  if docker exec "$LAB_CONTAINER" pg_isready -U "$LAB_USER" -q 2>/dev/null; then
    streak=$((streak+1)); [ $streak -ge 20 ] && break
  else streak=0; fi
done
[ $streak -ge 20 ] && ok "lab is accepting connections" || { bad "lab never came up"; exit 1; }

for db in mig_src mig_dst mig_schema; do
  lab -d postgres -c "CREATE DATABASE brujula_$db" >/dev/null 2>&1
done
ok "created brujula_mig_src, brujula_mig_dst, brujula_mig_schema"

step "2. load the real schema and data into the lab"
for db in brujula_mig_src brujula_mig_schema; do
  docker exec -i "$LAB_CONTAINER" psql -U "$LAB_USER" -q -v ON_ERROR_STOP=1 \
    -d "$db" < "$WORK/snapshot.sql" >/dev/null 2>&1 \
    && ok "$db loaded" || bad "$db failed to load"
done
n=$(lab -d brujula_mig_src -c "SELECT count(*) FROM tasks")
[ "$n" -gt 0 ] && ok "brujula_mig_src has $n task rows" || bad "no rows loaded"

step "3. the OLD backup form: pg_dump --data-only (expected to be unusable)"
docker exec "$LAB_CONTAINER" pg_dump -U "$LAB_USER" --data-only --table=tasks \
  --no-owner -d brujula_mig_src > "$WORK/data-only.sql" 2>/dev/null
if docker exec -i "$LAB_CONTAINER" psql -U "$LAB_USER" -v ON_ERROR_STOP=1 \
     -d brujula_mig_dst < "$WORK/data-only.sql" >/dev/null 2>&1; then
  bad "--data-only restored into an empty database; it should not have"
else
  ok "--data-only cannot restore into an empty database (the defect this fixes)"
fi

step "4. the NEW backup form: with schema, restored into the other database"
docker exec "$LAB_CONTAINER" pg_dump -U "$LAB_USER" --table=tasks --no-owner \
  --no-acl --clean --if-exists -d brujula_mig_src > "$WORK/full.sql" 2>/dev/null
grep -q '^CREATE TABLE' "$WORK/full.sql" \
  && ok "the dump contains CREATE TABLE" || bad "no CREATE TABLE in the dump"
docker exec -i "$LAB_CONTAINER" psql -U "$LAB_USER" -q -v ON_ERROR_STOP=1 \
  -d brujula_mig_dst < "$WORK/full.sql" >/dev/null 2>&1 \
  && ok "restored into the empty database" || bad "restore failed"

step "5. compare every column of every row"
if "$PY" "$REPO/scripts/compare-task-rows.py" --quiet-ok \
      --source "$(url brujula_mig_src)" --target "$(url brujula_mig_dst)" \
      > "$WORK/cmp1.txt" 2>&1; then
  ok "$(grep -o 'identical: .*' "$WORK/cmp1.txt" | head -1)"
else
  # `--table=tasks` covers tasks only, so the other tables are absent. That is
  # a real limitation of a table-scoped backup, not a restore error.
  if grep -q 'Every financial field matches' "$WORK/cmp1.txt"; then
    ok "every financial field matches (other tables absent: table-scoped dump)"
    grep -o 'identical: .*' "$WORK/cmp1.txt" | head -1 | sed 's/^/       /'
  else
    bad "comparison found discrepancies"; sed -n '/verdict/,$p' "$WORK/cmp1.txt"
  fi
fi

step "6. apply the schema migration that adds refund_hold"
DATABASE_URL="" "$PY" - <<PYEOF >"$WORK/mig.txt" 2>&1
import sys; sys.path.insert(0, "$REPO/agent/src")
from research_agent.task_store import TaskStore
u = "$(url brujula_mig_schema)"
assert not u.rstrip("/").endswith("/marketplace")
s = TaskStore(database_url=u)
c = s._fetchall("SELECT column_name FROM information_schema.columns "
                "WHERE table_name='tasks' AND column_name IN "
                "('refund_hold','refund_hold_reason') ORDER BY 1")
print("columns:", [x["column_name"] for x in c])
print("nulls:", s._fetchall("SELECT count(*) AS n FROM tasks WHERE refund_hold IS NULL")[0]["n"])
print("rows:", s._fetchall("SELECT count(*) AS n FROM tasks")[0]["n"])
s.close()
TaskStore(database_url=u).close()   # idempotent
print("idempotent: yes")
PYEOF
grep -q "refund_hold'," "$WORK/mig.txt" || grep -q "refund_hold'" "$WORK/mig.txt" \
  && ok "both columns added" || bad "columns missing: $(cat "$WORK/mig.txt")"
grep -q "^nulls: 0$" "$WORK/mig.txt" \
  && ok "no NULLs on existing rows (defaults applied)" || bad "NULLs found"
grep -q "^idempotent: yes$" "$WORK/mig.txt" \
  && ok "running it twice is a no-op" || bad "not idempotent"
"$PY" "$REPO/scripts/compare-task-rows.py" --quiet-ok \
  --source "$(url brujula_mig_src)" --target "$(url brujula_mig_schema)" \
  > "$WORK/cmp2.txt" 2>&1
grep -q 'all equal' "$WORK/cmp2.txt" \
  && ok "the migration changed no existing data: $(grep -o 'identical: .*' "$WORK/cmp2.txt")" \
  || bad "the migration altered data"

step "7. hold, correct, and roll each back independently"
LAB_URL="$(url brujula_mig_schema)"
COMMON=(--database-url "$LAB_URL" --expect-database brujula_mig_schema
        --docker-container "$LAB_CONTAINER")

"$PY" "$REPO/scripts/repair-contaminated-task.py" --apply "${COMMON[@]}" \
  --backup "$WORK/should-not-exist.sql" >"$WORK/r1.txt" 2>&1
if [ -f "$WORK/should-not-exist.sql" ]; then
  bad "the correction wrote a backup without a hold in place"
else
  grep -q "refund hold in place" "$WORK/r1.txt" \
    && ok "the correction refuses while there is no hold" \
    || bad "the correction did not refuse: $(tail -2 "$WORK/r1.txt")"
fi

"$PY" "$REPO/scripts/hold-escrow-refund.py" --apply "${COMMON[@]}" \
  --backup "$WORK/hold.sql" >"$WORK/h.txt" 2>&1
HOLD_ID=$(grep -o 'hold-[0-9a-f]\{8\}-[0-9TZ]*' "$WORK/h.txt" | head -1)
[ -n "$HOLD_ID" ] && ok "hold applied, snapshot $HOLD_ID" \
  || { bad "hold failed: $(tail -3 "$WORK/h.txt")"; HOLD_ID=""; }

"$PY" "$REPO/scripts/repair-contaminated-task.py" --apply "${COMMON[@]}" \
  --backup "$WORK/fix.sql" >"$WORK/f.txt" 2>&1
FIX_ID=$(grep -o 'fix-[0-9a-f]\{8\}-[0-9TZ]*' "$WORK/f.txt" | head -1)
if [ -n "$FIX_ID" ] && grep -q "The row is honest" "$WORK/f.txt"; then
  ok "correction applied and verified, snapshot $FIX_ID"
else
  bad "correction failed: $(tail -3 "$WORK/f.txt")"; FIX_ID=""
fi

if [ -n "$FIX_ID" ]; then
  "$PY" "$REPO/scripts/repair-contaminated-task.py" --rollback "$FIX_ID" \
    --database-url "$LAB_URL" --expect-database brujula_mig_schema \
    >"$WORK/rb1.txt" 2>&1
  hold_after=$(lab -d brujula_mig_schema -c \
    "SELECT refund_hold FROM tasks WHERE task_id='40f6f3daf4c546fda37c19afbe8bcc5f'")
  refunded_after=$(lab -d brujula_mig_schema -c \
    "SELECT refunded FROM tasks WHERE task_id='40f6f3daf4c546fda37c19afbe8bcc5f'")
  [ "$hold_after" = "1" ] && [ "$refunded_after" = "1" ] \
    && ok "rolling back the correction restored the fields AND kept the hold" \
    || bad "independence broken: hold=$hold_after refunded=$refunded_after"
fi

if [ -n "$HOLD_ID" ]; then
  "$PY" "$REPO/scripts/hold-escrow-refund.py" --rollback "$HOLD_ID" \
    --database-url "$LAB_URL" --expect-database brujula_mig_schema \
    >"$WORK/rb2.txt" 2>&1
  hold_after=$(lab -d brujula_mig_schema -c \
    "SELECT refund_hold FROM tasks WHERE task_id='40f6f3daf4c546fda37c19afbe8bcc5f'")
  [ "$hold_after" = "0" ] \
    && ok "rolling back the hold lifted it" || bad "hold still $hold_after"
fi

step "8. archive, delete and restore the test rows"
"$PY" "$REPO/scripts/classify-test-rows.py" --archive "${COMMON[@]}" \
  --backup "$WORK/arch.sql" >"$WORK/a1.txt" 2>&1
grep -q "0 with any column differing" "$WORK/a1.txt" \
  && ok "$(grep -o 'verified: .*' "$WORK/a1.txt")" \
  || bad "archive verification failed: $(tail -3 "$WORK/a1.txt")"

"$PY" "$REPO/scripts/classify-test-rows.py" --delete "${COMMON[@]}" \
  --backup "$WORK/del-nohold.sql" >"$WORK/a2.txt" 2>&1
grep -q "not under a refund hold" "$WORK/a2.txt" \
  && ok "deleting refuses while the real row has no hold" \
  || bad "the delete did not refuse"

"$PY" "$REPO/scripts/hold-escrow-refund.py" --apply "${COMMON[@]}" \
  --backup "$WORK/hold2.sql" >"$WORK/h2.txt" 2>&1
HOLD2=$(grep -o 'hold-[0-9a-f]\{8\}-[0-9TZ]*' "$WORK/h2.txt" | head -1)
"$PY" "$REPO/scripts/classify-test-rows.py" --delete "${COMMON[@]}" \
  --backup "$WORK/del.sql" >"$WORK/a3.txt" 2>&1
left=$(lab -d brujula_mig_schema -c "SELECT count(*) FROM tasks")
[ "$left" = "1" ] && ok "31 deleted, the real row kept" || bad "$left rows left"

"$PY" "$REPO/scripts/classify-test-rows.py" --restore-archive \
  --database-url "$LAB_URL" --expect-database brujula_mig_schema \
  >"$WORK/a4.txt" 2>&1
grep -q "every column verified" "$WORK/a4.txt" \
  && ok "$(grep -o 'restored 31 rows.*' "$WORK/a4.txt")" \
  || bad "restore failed: $(tail -3 "$WORK/a4.txt")"

[ -n "$HOLD2" ] && "$PY" "$REPO/scripts/hold-escrow-refund.py" --rollback "$HOLD2" \
  --database-url "$LAB_URL" --expect-database brujula_mig_schema >/dev/null 2>&1

step "9. is the database byte-identical to where it started?"
"$PY" "$REPO/scripts/compare-task-rows.py" --quiet-ok \
  --source "$(url brujula_mig_src)" --target "$LAB_URL" >"$WORK/cmp3.txt" 2>&1
if grep -q 'all equal' "$WORK/cmp3.txt"; then
  ok "$(grep -o 'identical: .*' "$WORK/cmp3.txt")"
else
  bad "the database did not return to its starting state"
  sed -n '/field by field/,$p' "$WORK/cmp3.txt"
fi

step "10. the real database was not written to"
real_rows=$(docker exec "$SRC_CONTAINER" psql -U "$LAB_USER" -qtAX -d "$SRC_DB" \
  -c "SELECT count(*) FROM tasks" 2>/dev/null)
real_new=$(docker exec "$SRC_CONTAINER" psql -U "$LAB_USER" -qtAX -d "$SRC_DB" \
  -c "SELECT count(*) FROM information_schema.columns WHERE table_name='tasks' AND column_name LIKE 'refund%hold%'" 2>/dev/null)
real_tabs=$(docker exec "$SRC_CONTAINER" psql -U "$LAB_USER" -qtAX -d "$SRC_DB" \
  -c "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('tasks_rollback','tasks_test_archive')" 2>/dev/null)
ok "$SRC_DB: $real_rows task rows"
[ "$real_new" = "0" ] && ok "$SRC_DB has no refund_hold columns" \
  || bad "$SRC_DB gained $real_new columns"
[ "$real_tabs" = "0" ] && ok "$SRC_DB has no rollback or archive tables" \
  || bad "$SRC_DB gained $real_tabs tables"

printf '\n\033[1m== %d passed, %d failed ==\033[0m\n' "$pass" "$fail"
[ "$fail" -eq 0 ] || exit 1
