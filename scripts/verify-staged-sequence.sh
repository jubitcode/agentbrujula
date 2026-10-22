#!/usr/bin/env bash
# Prove the staged sequence, and its rollback, on throwaway databases.
#
#   scripts/verify-staged-sequence.sh
#
# The sequence under test, in the order the stages must run:
#
#   0. full backup of the database, verified to cover tasks AND nonces
#   1. add the refund_hold columns          scripts/migrate-schema.py
#   2. apply the refund hold                scripts/hold-escrow-refund.py
#   3. correct the contaminated fields      scripts/repair-contaminated-task.py
#   4. archive the 31 test rows             scripts/classify-test-rows.py
#
# And three things the stages must guarantee:
#
#   * The backup exists and is verified BEFORE the first write, stage 1
#     included. Stage 1 refuses to run without one.
#   * Starting the API is NOT how the columns get added. It deletes rows.
#     Stage A below measures that.
#   * The whole sequence can be rolled back, by snapshot for each stage and by
#     full restore for the sequence, losing neither financial fields nor nonces.
#
# Reads the real database once with pg_dump, read-only. Everything else happens
# on port 5436 in a server of its own, torn down at the end unless KEEP=1.

set -uo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LAB="${LAB:-brujula-stage-lab}"
PORT="${PORT:-5436}"
USER_="${USER_:-agent_web3}"
PASS="${PASS:-lab-only}"
SRC_CONTAINER="${SRC_CONTAINER:-agentbrujula-postgres-1}"
SRC_DB="${SRC_DB:-marketplace}"
PY="${PY:-$REPO/agent/.venv/bin/python}"
REAL_TASK=40f6f3daf4c546fda37c19afbe8bcc5f
W="$(mktemp -d)"

pass=0; fail=0
ok()   { printf '  \033[32mok\033[0m   %s\n' "$1"; pass=$((pass+1)); }
bad()  { printf '  \033[31mFAIL\033[0m %s\n' "$1"; fail=$((fail+1)); }
step() { printf '\n\033[1m== %s ==\033[0m\n' "$1"; }

cleanup() {
  [ "${KEEP:-0}" = "1" ] && { echo; echo "KEEP=1: lab on $PORT, files in $W"; return; }
  docker rm -f "$LAB" >/dev/null 2>&1; rm -rf "$W"
}
trap cleanup EXIT

q()   { docker exec -i "$LAB" psql -U "$USER_" -qtAX -d "$1" -c "$2"; }
url() { echo "postgresql://$USER_:$PASS@127.0.0.1:$PORT/$1"; }
fresh() {  # fresh() <db> — recreate it and load the snapshot
  docker exec -i "$LAB" psql -U "$USER_" -qtAX -d postgres \
    -c "DROP DATABASE IF EXISTS $1" -c "CREATE DATABASE $1" >/dev/null 2>&1
  docker exec -i "$LAB" psql -U "$USER_" -q -v ON_ERROR_STOP=1 -d "$1" \
    < "$W/full.sql" >/dev/null 2>&1
  # Real nonces are short-lived, so the live table is usually empty. Seed some,
  # otherwise "the restore did not lose nonces" would be vacuously true.
  #
  # `seen_at` is a FIXED, old timestamp, for two reasons: fixed so that two
  # databases seeded separately have identical contents and can be compared by
  # digest, and old so that `cleanup_nonces(ttl=300)` actually considers them.
  # Seeding with now() made both of those checks pass for the wrong reason.
  docker exec -i "$LAB" psql -U "$USER_" -qtAX -d "$1" -c \
    "INSERT INTO nonces (nonce, seen_at) SELECT 'seeded-'||g, 1700000000 FROM generate_series(1,7) g ON CONFLICT DO NOTHING" >/dev/null 2>&1
}

seeded_dump() {  # seeded_dump <db> <file> — a full dump that CONTAINS the seed
  docker exec "$LAB" pg_dump -U "$USER_" --no-owner --no-acl -d "$1" > "$2"
}

step "0. read the real database once, read-only, FULL dump"
docker exec "$SRC_CONTAINER" pg_dump -U "$USER_" --no-owner --no-acl -d "$SRC_DB" \
  > "$W/full.sql" 2>"$W/e" || { bad "pg_dump failed: $(head -1 "$W/e")"; exit 1; }
ntab=$(grep -c '^CREATE TABLE' "$W/full.sql")
ok "full dump: $(wc -c <"$W/full.sql" | tr -d ' ') bytes, $ntab tables"
for t in tasks nonces; do
  grep -q "CREATE TABLE public.$t" "$W/full.sql" \
    && ok "the dump covers $t" || bad "the dump does NOT cover $t"
done

step "1. start a throwaway server on port $PORT"
docker rm -f "$LAB" >/dev/null 2>&1
docker run -d --name "$LAB" -e POSTGRES_USER="$USER_" \
  -e POSTGRES_PASSWORD="$PASS" -e POSTGRES_DB=postgres \
  -p "127.0.0.1:$PORT:5432" postgres:17-alpine >/dev/null || { bad "no lab"; exit 1; }
s=0; for _ in $(seq 1 600); do
  docker exec "$LAB" pg_isready -U "$USER_" -q 2>/dev/null && s=$((s+1)) || s=0
  [ $s -ge 20 ] && break
done
[ $s -ge 20 ] && ok "lab accepting connections" || { bad "lab never came up"; exit 1; }

step "A. what starting the API does, besides adding the columns"
fresh stage_api
# Age every row past the 24h threshold: this is what the clock does on its own.
q stage_api "UPDATE tasks SET created_at = created_at - 90000" >/dev/null
# The seed is already old (fixed 1700000000), so cleanup_nonces will see it.
before=$(q stage_api "SELECT count(*) FROM tasks")
non_b=$(q stage_api "SELECT count(*) FROM nonces")
# The sweeper's own call, exactly as api/server.py:124 makes it. No key loaded.
DATABASE_URL="" "$PY" - <<PYEOF >"$W/api.txt" 2>&1
import sys; sys.path.insert(0, "$REPO/agent/src")
from research_agent.task_store import TaskStore
u = "$(url stage_api)"
assert not u.rstrip("/").endswith("/marketplace")
s = TaskStore(database_url=u)
print("cleanup:", s.cleanup(max_age_seconds=86400))
s.cleanup_nonces(ttl=300)
print("real_row_present:", s.get_task("$REAL_TASK") is not None)
s.close()
PYEOF
after=$(q stage_api "SELECT count(*) FROM tasks")
non_a=$(q stage_api "SELECT count(*) FROM nonces")
# Housekeeping still deletes: preserving everything forever is a different
# bug. What it must not delete is a row that still accounts for money.
if [ "$after" -lt "$before" ]; then
  ok "the startup cleanup still deletes old rows ($before -> $after), no key needed"
else
  bad "the startup cleanup deleted nothing ($before -> $after)"
fi
# Before `_UNPROVEN_REFUND_SQL` this printed False: the row claiming
# refunded = 1 with refund_tx_hash = 'h' was deleted along with the rest,
# 0.675 USDC and all, by nothing more than starting the API.
grep -q "real_row_present: True" "$W/api.txt" \
  && ok "the row holding 0.675 USDC SURVIVES it (refund it cannot demonstrate)" \
  || bad "the real row was deleted — _UNPROVEN_REFUND_SQL is not holding"
left_api=$(q stage_api "SELECT count(*) FROM tasks")
[ "$left_api" = "1" ] \
  && ok "exactly the one unaccounted row is kept; the 31 test rows go" \
  || bad "expected 1 row kept, found $left_api"
[ "$non_a" -lt "$non_b" ] \
  && ok "measured: it also deletes nonces ($non_b -> $non_a), replay protection" \
  || bad "nonces unchanged ($non_b -> $non_a)"
# Still true, and still the reason migrate-schema.py exists: the startup
# deletes rows and nonces, and that is never what "add two columns" should do.
ok "=> the columns must still NOT be added by starting the API"

step "B. stage 1 refuses to write without a verified backup"
fresh stage_seq
q stage_seq "ALTER TABLE tasks DROP COLUMN IF EXISTS refund_hold" >/dev/null
q stage_seq "ALTER TABLE tasks DROP COLUMN IF EXISTS refund_hold_reason" >/dev/null
"$PY" "$REPO/scripts/migrate-schema.py" --apply --database-url "$(url stage_seq)" \
  --expect-database stage_seq >"$W/m0.txt" 2>&1
grep -q "needs --require-backup" "$W/m0.txt" \
  && ok "no --require-backup: refuses" || bad "it wrote without a backup"
echo "not a dump" > "$W/bogus.sql"
"$PY" "$REPO/scripts/migrate-schema.py" --apply --database-url "$(url stage_seq)" \
  --expect-database stage_seq --require-backup "$W/bogus.sql" >"$W/m1.txt" 2>&1
grep -q "no CREATE TABLE for" "$W/m1.txt" \
  && ok "a dump missing tasks/nonces: refuses" || bad "accepted a bogus backup"
"$PY" "$REPO/scripts/migrate-schema.py" --apply --database-url "$(url stage_seq)" \
  --expect-database stage_seq --require-backup "$W/missing.sql" >"$W/m2.txt" 2>&1
grep -q "no backup at" "$W/m2.txt" \
  && ok "a backup that does not exist: refuses" || bad "accepted a missing backup"
cols=$(q stage_seq "SELECT count(*) FROM information_schema.columns WHERE table_name='tasks' AND column_name LIKE 'refund%hold%'")
[ "$cols" = "0" ] && ok "no columns were added by the refused attempts" \
  || bad "$cols columns appeared anyway"

step "C. the staged sequence, in order"
SEQ=(--database-url "$(url stage_seq)" --expect-database stage_seq
     --docker-container "$LAB")
rows0=$(q stage_seq "SELECT count(*) FROM tasks")
non0=$(q stage_seq "SELECT count(*) FROM nonces")

# stage 1
"$PY" "$REPO/scripts/migrate-schema.py" --apply "${SEQ[@]:0:4}" \
  --require-backup "$W/full.sql" >"$W/s1.txt" 2>&1
grep -q "no NULLs in refund_hold" "$W/s1.txt" && grep -q "^Done" "$W/s1.txt" \
  && ok "stage 1: columns added, $(grep -o 'tasks still [0-9]* rows' "$W/s1.txt")" \
  || bad "stage 1 failed: $(tail -3 "$W/s1.txt")"

# stage 3 before stage 2 — must refuse
"$PY" "$REPO/scripts/repair-contaminated-task.py" --apply "${SEQ[@]}" \
  --backup "$W/nope.sql" >"$W/s3a.txt" 2>&1
grep -q "refund hold in place" "$W/s3a.txt" && [ ! -f "$W/nope.sql" ] \
  && ok "stage 3 before stage 2: refuses, writes nothing" \
  || bad "stage 3 ran out of order"
grep -q "separate, read-only connection" "$W/s3a.txt" \
  && ok "stage 3 checks the hold on an independent connection" \
  || bad "the hold check is not independent"

# stage 2
"$PY" "$REPO/scripts/hold-escrow-refund.py" --apply "${SEQ[@]}" \
  --backup "$W/h.sql" >"$W/s2.txt" 2>&1
HOLD_ID=$(grep -o 'hold-[0-9a-f]\{8\}-[0-9TZ]*' "$W/s2.txt" | head -1)
[ -n "$HOLD_ID" ] && ok "stage 2: hold applied, snapshot $HOLD_ID" \
  || bad "stage 2 failed: $(tail -3 "$W/s2.txt")"
# independent read, outside either script
h=$(q stage_seq "SELECT refund_hold FROM tasks WHERE task_id='$REAL_TASK'")
r=$(q stage_seq "SELECT length(coalesce(refund_hold_reason,'')) FROM tasks WHERE task_id='$REAL_TASK'")
[ "$h" = "1" ] && [ "$r" -gt 0 ] \
  && ok "independent psql read confirms hold=1 with a reason ($r chars)" \
  || bad "independent read: hold=$h reason=$r"

# stage 3
"$PY" "$REPO/scripts/repair-contaminated-task.py" --apply "${SEQ[@]}" \
  --backup "$W/f.sql" >"$W/s3.txt" 2>&1
FIX_ID=$(grep -o 'fix-[0-9a-f]\{8\}-[0-9TZ]*' "$W/s3.txt" | head -1)
grep -q "The row is honest" "$W/s3.txt" \
  && ok "stage 3: row corrected, snapshot $FIX_ID" \
  || bad "stage 3 failed: $(tail -4 "$W/s3.txt")"
grep -q "hold confirmed independently after the write" "$W/s3.txt" \
  && ok "stage 3 re-confirms the hold independently after committing" \
  || bad "no independent re-read after the write"

# stage 4
"$PY" "$REPO/scripts/classify-test-rows.py" --archive "${SEQ[@]}" \
  --backup "$W/a.sql" >"$W/s4.txt" 2>&1
grep -q "0 with any column differing" "$W/s4.txt" \
  && ok "stage 4: $(grep -o 'verified: .*' "$W/s4.txt")" \
  || bad "stage 4 failed: $(tail -3 "$W/s4.txt")"
rows_end=$(q stage_seq "SELECT count(*) FROM tasks")
non_end=$(q stage_seq "SELECT count(*) FROM nonces")
[ "$rows_end" = "$rows0" ] && ok "tasks unchanged through the sequence: $rows0" \
  || bad "tasks went $rows0 -> $rows_end"
[ "$non_end" = "$non0" ] && ok "nonces unchanged through the sequence: $non0" \
  || bad "nonces went $non0 -> $non_end"

step "D. rollback by snapshot, stage by stage, newest first"
"$PY" "$REPO/scripts/repair-contaminated-task.py" --rollback "$FIX_ID" \
  --database-url "$(url stage_seq)" --expect-database stage_seq >"$W/r3.txt" 2>&1
h=$(q stage_seq "SELECT refund_hold FROM tasks WHERE task_id='$REAL_TASK'")
rf=$(q stage_seq "SELECT refunded FROM tasks WHERE task_id='$REAL_TASK'")
[ "$h" = "1" ] && [ "$rf" = "1" ] \
  && ok "undoing stage 3 restored the fields and KEPT the hold" \
  || bad "independence broken: hold=$h refunded=$rf"
"$PY" "$REPO/scripts/hold-escrow-refund.py" --rollback "$HOLD_ID" \
  --database-url "$(url stage_seq)" --expect-database stage_seq >"$W/r2.txt" 2>&1
h=$(q stage_seq "SELECT refund_hold FROM tasks WHERE task_id='$REAL_TASK'")
[ "$h" = "0" ] && ok "undoing stage 2 lifted the hold" || bad "hold=$h"
q stage_seq "ALTER TABLE tasks DROP COLUMN IF EXISTS refund_hold" >/dev/null
q stage_seq "ALTER TABLE tasks DROP COLUMN IF EXISTS refund_hold_reason" >/dev/null
q stage_seq "DROP TABLE IF EXISTS tasks_test_archive" >/dev/null
q stage_seq "DROP TABLE IF EXISTS tasks_rollback" >/dev/null
ok "undoing stage 1: columns and helper tables dropped"
fresh stage_ref   # a pristine copy of where we started, for comparison
"$PY" "$REPO/scripts/compare-task-rows.py" --quiet-ok \
  --source "$(url stage_ref)" --target "$(url stage_seq)" >"$W/c1.txt" 2>&1
grep -q 'No differences' "$W/c1.txt" \
  && ok "after the staged rollback: $(grep -o 'No differences.*' "$W/c1.txt" | head -1)" \
  || { bad "the staged rollback did not return to the start"
       sed -n '/verdict/,$p' "$W/c1.txt" | head -8; }

step "E. rollback of the WHOLE sequence by full restore"
# The other path: the sequence ran, something is wrong, and the decision is to
# put the database back from the stage-0 dump rather than unwind stage by stage.
fresh stage_seq
# The stage-0 dump has to be taken AFTER the seed, or "no nonces were lost"
# would be testing a dump that never had any. This is the stage-0 backup as the
# real procedure takes it: a full dump of the database about to be changed.
seeded_dump stage_seq "$W/stage0.sql"
grep -q "CREATE TABLE public.nonces" "$W/stage0.sql" \
  && ok "stage-0 dump taken from the database about to change, covers nonces" \
  || bad "the stage-0 dump does not cover nonces"
nseed0=$(q stage_seq "SELECT count(*) FROM nonces WHERE nonce LIKE 'seeded-%'")
[ "$nseed0" -gt 0 ] && ok "stage-0 database has $nseed0 seeded nonces to lose" \
  || bad "nothing seeded: the nonce check would be vacuous"
SEQ2=(--database-url "$(url stage_seq)" --expect-database stage_seq
      --docker-container "$LAB")
"$PY" "$REPO/scripts/migrate-schema.py" --apply "${SEQ2[@]:0:4}" \
  --require-backup "$W/stage0.sql" >/dev/null 2>&1
"$PY" "$REPO/scripts/hold-escrow-refund.py" --apply "${SEQ2[@]}" \
  --backup "$W/h2.sql" >/dev/null 2>&1
"$PY" "$REPO/scripts/repair-contaminated-task.py" --apply "${SEQ2[@]}" \
  --backup "$W/f2.sql" >/dev/null 2>&1
# Archive first: --delete refuses rows it cannot find in the archive, which is
# the point. Going straight to --delete was a mistake in an earlier version of
# this script, and the refusal was correct.
"$PY" "$REPO/scripts/classify-test-rows.py" --archive "${SEQ2[@]}" \
  --backup "$W/a2.sql" >"$W/e0.txt" 2>&1
"$PY" "$REPO/scripts/classify-test-rows.py" --delete "${SEQ2[@]}" \
  --backup "$W/a2d.sql" >"$W/e1.txt" 2>&1
left=$(q stage_seq "SELECT count(*) FROM tasks")
[ "$left" = "1" ] && ok "sequence applied in full, including the delete (1 row left)" \
  || bad "expected 1 row, found $left"

# Restore the stage-0 dump into a throwaway database and compare.
docker exec -i "$LAB" psql -U "$USER_" -qtAX -d postgres \
  -c "DROP DATABASE IF EXISTS stage_restored" -c "CREATE DATABASE stage_restored" >/dev/null 2>&1
docker exec -i "$LAB" psql -U "$USER_" -q -v ON_ERROR_STOP=1 -d stage_restored \
  < "$W/stage0.sql" >/dev/null 2>&1 \
  && ok "the stage-0 dump restored into an empty database" \
  || bad "the full restore failed"
rt=$(q stage_restored "SELECT count(*) FROM tasks")
ok "restored tasks: $rt"
real_ok=$(q stage_restored "SELECT count(*) FROM tasks WHERE task_id='$REAL_TASK' AND escrow_tx<>''")
[ "$real_ok" = "1" ] && ok "the 0.675 USDC row is back, escrow_tx intact" \
  || bad "the real row did not come back"
# Nonces: seeded before the dump, so this is not vacuous.
nseed=$(q stage_restored "SELECT count(*) FROM nonces WHERE nonce LIKE 'seeded-%'")
ok "restored nonces: $(q stage_restored 'SELECT count(*) FROM nonces') (seeded: $nseed)"
[ "$nseed" -gt 0 ] && ok "no nonces lost by the full restore" \
  || bad "seeded nonces did not survive the restore"
for t in $(grep '^CREATE TABLE public.' "$W/stage0.sql" | sed 's/.*public\.//;s/ .*//'); do
  n=$(q stage_restored "SELECT count(*) FROM $t" 2>/dev/null)
  [ -n "$n" ] && ok "table $t restored ($n rows)" || bad "table $t missing"
done

step "F. the real database was not written to"
rr=$(docker exec "$SRC_CONTAINER" psql -U "$USER_" -qtAX -d "$SRC_DB" -c "SELECT count(*) FROM tasks" 2>/dev/null)
rc=$(docker exec "$SRC_CONTAINER" psql -U "$USER_" -qtAX -d "$SRC_DB" -c "SELECT count(*) FROM information_schema.columns WHERE table_name='tasks' AND column_name LIKE 'refund%hold%'" 2>/dev/null)
rt2=$(docker exec "$SRC_CONTAINER" psql -U "$USER_" -qtAX -d "$SRC_DB" -c "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('tasks_rollback','tasks_test_archive')" 2>/dev/null)
ok "$SRC_DB: $rr task rows"
[ "$rc" = "0" ] && ok "$SRC_DB has no refund_hold columns" || bad "$SRC_DB gained $rc columns"
[ "$rt2" = "0" ] && ok "$SRC_DB has no helper tables" || bad "$SRC_DB gained $rt2 tables"

printf '\n\033[1m== %d passed, %d failed ==\033[0m\n' "$pass" "$fail"
[ "$fail" -eq 0 ] || exit 1
