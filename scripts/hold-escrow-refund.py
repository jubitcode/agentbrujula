#!/usr/bin/env python3
"""Put a refund hold on one task, or lift it. Nothing else.

    Show what it would do (default, read-only):
        python3 scripts/hold-escrow-refund.py --database-url <url>

    Apply:
        python3 scripts/hold-escrow-refund.py --apply --database-url <url> \
            --backup ../agentbrujula-backups/hold-$(date +%F).sql

    Undo, by snapshot id (printed when applied, and by --list):
        python3 scripts/hold-escrow-refund.py --rollback <id> --database-url <url>

    List snapshots taken by either script:
        python3 scripts/hold-escrow-refund.py --list --database-url <url>

This is ONE operation: it sets refund_hold and refund_hold_reason, and touches
nothing else. Correcting the row's contaminated fields is a separate script
(repair-contaminated-task.py) with its own backup, its own verification and its
own rollback — deliberately, because they are two decisions and either one has
to be reversible without undoing the other.

The hold goes FIRST. Task 40f6f3daf4c546fda37c19afbe8bcc5f holds 0.675 USDC in
a live escrow entry, and until this column existed the only thing keeping the
sweeper away from it was a FALSE refunded = 1 written by a test fixture. Four
of the six conditions the sweeper checks were already true. Correcting the row
removes that accidental protection, so the deliberate one must already be in
place.

Backups are written OUTSIDE the repository, to
../agentbrujula-backups/: a dump of this database carries wallet
addresses, the text of hired tasks and a real payment history, and
none of that belongs in version control.
"""

from __future__ import annotations

import argparse
import os
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import dbsafe  # noqa: E402

TASK_ID = "40f6f3daf4c546fda37c19afbe8bcc5f"
ESCROW_TX = "e6d6a1a06f1fd7b57d57aa8f1c96a63dae167f256ee2ecba343f2f20b08a7966"
CONTRACT = "CAMPNA3TNE23UYM5WSTXHQWA5HGU7HL6M2T64R5VNQT5PJS6UBIXOCCO"

HOLD_REASON = (
    f"0.675 USDC live in escrow entry 40f6f3da on {CONTRACT}; row was "
    f"contaminated by a test fixture (refund_tx_hash='h'); investigation open; "
    f"no refund authorised"
)


def fail(msg: str) -> None:
    print(f"ABORT: {msg}", file=sys.stderr)
    sys.exit(2)


def check_chain() -> bool:
    """Ask the contract whether the entry is live and still holds funds.

    A hold asserts that there is money worth protecting, so it is checked
    against the ledger. An unreachable RPC is NOT taken as agreement — but it
    is also not fatal for a hold: refusing to protect funds because a node is
    down would be the wrong way to fail. It is fatal for the correction.
    """
    sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "agent" / "src"))
    try:
        from research_agent.escrow import get_escrow_status

        status = get_escrow_status(TASK_ID) or {}
    except Exception as e:
        print(f"  chain: CANNOT CHECK ({type(e).__name__}: {e})")
        print("         A hold does not require this check to pass: refusing to")
        print("         protect funds because an RPC node is down would be the")
        print("         wrong way to fail. The correction does require it.")
        return False
    print(f"  chain: status={status.get('escrow_status')!r} "
          f"amount={status.get('amount')!r} client={status.get('client')!r}")
    if status.get("status") == "not_found":
        print("         The entry is NOT readable. A hold is still correct —")
        print("         funds may be archived rather than resolved — but the")
        print("         reason text should say so.")
        return False
    return True


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--task-id", default=TASK_ID)
    ap.add_argument("--reason", default=HOLD_REASON)
    ap.add_argument("--apply", action="store_true")
    ap.add_argument("--release", action="store_true",
                    help="lift the hold instead of setting it (needs --reason)")
    ap.add_argument("--rollback", metavar="SNAPSHOT_ID")
    ap.add_argument("--list", action="store_true")
    ap.add_argument("--backup", type=Path)
    ap.add_argument("--database-url", default="")
    ap.add_argument("--expect-database", default="marketplace")
    ap.add_argument("--docker-container", default="",
                    help="run pg_dump inside this container instead of locally")
    args = ap.parse_args()

    url = args.database_url or os.environ.get("DATABASE_URL", "")
    if not url.startswith("postgres"):
        fail("pass --database-url for the PostgreSQL database")
    name = dbsafe.db_name(url)
    print(f"database: {name}")

    writing = args.apply or args.release or bool(args.rollback)
    if writing and name != args.expect_database:
        fail(f"database is {name!r} but --expect-database says "
             f"{args.expect_database!r}. Name the target explicitly.")
    if (args.apply or args.release) and not args.backup:
        fail("--apply and --release need --backup <file>")

    import psycopg2
    import psycopg2.extras

    conn = psycopg2.connect(url)
    conn.autocommit = False
    try:
        if args.list:
            snaps = dbsafe.list_snapshots(conn)
            if not snaps:
                print("  no snapshots")
            for s in snaps:
                print(f"  {s['rollback_id']}  {s['taken_at']}  "
                      f"{s['rows']} row(s)  {s['reason']}")
            return 0

        if args.rollback:
            return do_rollback(conn, args.rollback)

        return do_hold(conn, url, args)
    finally:
        conn.close()


def read_row(conn, task_id: str) -> dict | None:
    import psycopg2.extras

    with conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor) as cur:
        cur.execute("SELECT * FROM tasks WHERE task_id = %s", (task_id,))
        r = cur.fetchone()
    return dict(r) if r else None


def do_hold(conn, url: str, args) -> int:
    task_id = args.task_id
    target = 0 if args.release else 1
    verb = "release" if args.release else "hold"

    print(f"\n== precondition 1: the columns exist ==")
    with conn.cursor() as cur:
        cur.execute(
            "SELECT column_name FROM information_schema.columns "
            "WHERE table_name = 'tasks' AND column_name IN "
            "('refund_hold', 'refund_hold_reason')"
        )
        have = {r[0] for r in cur.fetchall()}
    missing = {"refund_hold", "refund_hold_reason"} - have
    ok_cols = not missing
    if ok_cols:
        print("  ok   refund_hold, refund_hold_reason")
    else:
        print(f"  FAIL missing: {sorted(missing)}")
        print("       TaskStore adds them on startup "
              "(_REQUIRED_TASK_COLUMNS_PG). Start the API against this")
        print("       database once with ORCHESTRATOR_SECRET_KEY UNSET, then")
        print("       re-run. Verified on a throwaway copy: the ALTER TABLE")
        print("       adds both with defaults, leaves all 32 rows byte-identical")
        print("       and sets refund_hold = 0 with no NULLs.")

    print(f"\n== precondition 2: the row exists and is the right one ==")
    row = read_row(conn, task_id)
    if row is None:
        fail(f"no task {task_id} in {dbsafe.db_name(url)}")
    ok_tx = row.get("escrow_tx") == ESCROW_TX if task_id == TASK_ID else True
    print(f"  {'ok  ' if ok_tx else 'FAIL'} escrow_tx = {row.get('escrow_tx')!r}")
    print(f"       status={row.get('status')!r} settled={row.get('settled')!r} "
          f"refunded={row.get('refunded')!r}")
    if not ok_tx:
        print(f"  FAIL expected {ESCROW_TX!r}: this is not the row investigated")

    print(f"\n== precondition 3: the current hold state ==")
    current = row.get("refund_hold")
    print(f"  refund_hold = {current!r}")
    if current == target:
        print(f"  Already {'not held' if target == 0 else 'held'} — "
              f"this would be a no-op on the flag, and would still rewrite "
              f"the reason.")

    print(f"\n== precondition 4: what the chain says ==")
    check_chain()

    print(f"\n== reason ==")
    reason = (args.reason or "").strip()
    if not reason:
        fail("a hold, and a release, both need a reason")
    print(f"  {reason}")

    print(f"\n== what would be written ==")
    stored = reason if target == 1 else f"released: {reason}"
    print(f"  UPDATE tasks")
    print(f"     SET refund_hold = {target}, refund_hold_reason = <reason>")
    print(f"   WHERE task_id = '{task_id}'")
    print(f"  reason stored: {stored[:120]}")
    print(f"\n  Nothing else is touched. refunded, status, escrow_tx,")
    print(f"  refund_tx_hash and settled are not in this statement.")

    if not (args.apply or args.release):
        print(f"\nDRY RUN — nothing was written.")
        print(f"To apply:  --apply --backup ../agentbrujula-backups/hold-$(date +%F).sql")
        return 0 if (ok_cols and ok_tx) else 2

    if not ok_cols:
        fail("the columns do not exist; nothing was written")
    if not ok_tx:
        fail("this is not the row that was investigated; nothing was written")

    print(f"\n== file backup (disaster copy, includes the schema) ==")
    try:
        size = dbsafe.file_backup(url, args.backup, args.docker_container)
        print(f"  {args.backup}  ({size} bytes, CREATE TABLE present)")
    except dbsafe.DbSafeError as e:
        fail(str(e))

    rollback_id = dbsafe.new_rollback_id(f"{verb}-{task_id[:8]}")
    print(f"\n== writing (one transaction) ==")
    try:
        with conn.cursor() as cur:
            n = dbsafe.snapshot_rows(
                conn, [task_id], rollback_id,
                f"before {verb}: {reason[:200]}",
            )
            print(f"  snapshot {rollback_id}: {n} row")

            cur.execute(
                "UPDATE tasks SET refund_hold = %s, refund_hold_reason = %s "
                "WHERE task_id = %s",
                (target, stored[:500], task_id),
            )
            if cur.rowcount != 1:
                raise dbsafe.DbSafeError(
                    f"touched {cur.rowcount} rows, expected 1"
                )
            print(f"  refund_hold = {target}")
    except Exception as e:
        conn.rollback()
        fail(f"{type(e).__name__}: {e} — transaction rolled back, "
             f"nothing was written")
    conn.commit()

    print(f"\n== verification ==")
    return verify_hold(conn, task_id, target, rollback_id)


def verify_hold(conn, task_id: str, target: int, rollback_id: str) -> int:
    row = read_row(conn, task_id)
    checks = [
        (f"refund_hold = {target}", int(row.get("refund_hold") or 0) == target),
        ("reason recorded", bool((row.get("refund_hold_reason") or "").strip())),
        ("escrow_tx untouched", row.get("escrow_tx") == ESCROW_TX
         if task_id == TASK_ID else True),
    ]

    # Nothing outside the two hold columns may have moved. Compared against
    # the snapshot taken in the same transaction, which is the only record of
    # what the row looked like a moment ago.
    cols = [c for c in dbsafe.task_columns(conn)
            if c not in ("refund_hold", "refund_hold_reason")]
    with conn.cursor() as cur:
        conds = " OR ".join(f't."{c}" IS DISTINCT FROM r."{c}"' for c in cols)
        cur.execute(
            f"SELECT count(*) FROM {dbsafe.ROLLBACK_TABLE} r "
            f"  JOIN tasks t ON t.task_id = r.task_id "
            f" WHERE r.rollback_id = %s AND ({conds})",
            (rollback_id,),
        )
        moved = cur.fetchone()[0]
    checks.append(("no other column changed", moved == 0))

    sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "agent" / "src"))
    try:
        from research_agent.refund_sweeper import refund_authorisation

        blocked = refund_authorisation(row)
        if target == 1:
            ok = blocked is not None and "refund hold" in blocked
            print(f"  {'ok  ' if ok else 'FAIL'} sweeper refuses it: {blocked}")
        else:
            ok = True
            print(f"  note the sweeper now says: {blocked}")
        checks.append(("sweeper guard agrees", ok))
    except Exception as e:
        print(f"  FAIL could not evaluate the sweeper guard: {e}")
        checks.append(("sweeper guard agrees", False))

    for name, ok in checks:
        print(f"  {'ok  ' if ok else 'FAIL'} {name}")

    if all(ok for _, ok in checks):
        print(f"\nDone. Roll back with:  --rollback {rollback_id}")
        return 0
    print(f"\nVERIFICATION FAILED. Roll back with:  --rollback {rollback_id}")
    return 1


def do_rollback(conn, rollback_id: str) -> int:
    """One statement, in one transaction, from the in-database snapshot."""
    print(f"\n== rollback {rollback_id} ==")
    try:
        n = dbsafe.restore_rows(conn, rollback_id)
        bad = dbsafe.verify_restored(conn, rollback_id)
    except dbsafe.DbSafeError as e:
        conn.rollback()
        fail(f"{e} — nothing was written")
    if bad:
        conn.rollback()
        fail(f"{len(bad)} rows do not match their snapshot after the restore "
             f"({bad[:5]}) — rolled back, nothing was written")
    conn.commit()
    print(f"  restored {n} row(s), every column verified against the snapshot")
    return 0


if __name__ == "__main__":
    sys.exit(main())
