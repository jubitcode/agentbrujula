#!/usr/bin/env python3
"""Correct the fields a test fixture wrote over one task row. Nothing else.

    Show what it would do (default, read-only):
        python3 scripts/repair-contaminated-task.py --database-url <url>

    Apply (requires the hold to be in place already):
        python3 scripts/repair-contaminated-task.py --apply \
            --database-url <url> --backup ../agentbrujula-backups/fix-$(date +%F).sql

    Undo, by snapshot id:
        python3 scripts/repair-contaminated-task.py --rollback <id> \
            --database-url <url>

SEPARATE FROM THE HOLD, ON PURPOSE
----------------------------------
Setting the refund hold is `scripts/hold-escrow-refund.py`. Two scripts, two
backups, two snapshots, two sets of verifications, two independent rollbacks.
They are two decisions: the protection has to be reversible without undoing
the correction, and the correction has to be reversible without removing the
protection. One script with two steps cannot give that.

This script REFUSES TO RUN unless the hold is already set, and its UPDATE
carries `AND refund_hold = 1`. The fields it corrects are the ones that were,
accidentally, the only thing keeping the sweeper away from 0.675 USDC — four
of its six conditions were already true. Correcting them without the hold in
place arms an automated transfer.

WHAT IS WRONG WITH THE ROW
--------------------------
`marketplace` says the client was refunded. The chain says the escrow entry is
live and holds 0.675 USDC. Four fields are the fixtures' and not the
application's:

    refunded        1                   false
    status          'refunded'          false
    refund_tx_hash  'h'                 the literal from tests/test_fund_safety.py
    refund_reason   'deadline_expired'  written by the same code path

`refund_tx_hash = 'h'` is what makes it conclusive: no Stellar transaction hash
is one character long, that value exists in the test suite and nowhere else,
and `tests/test_task_store_pg.py` used to default to postgresql://…/marketplace
with a fixture that ran `DELETE FROM tasks`.

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

#: Exactly what the row must look like before anything is written. If any of
#: these has changed, something else has touched the row since this was
#: investigated, and the plan needs re-reading rather than the script retrying.
EXPECTED_CONTAMINATED = {
    "status": "refunded",
    "settled": 0,
    "refunded": 1,
    "escrow_tx": ESCROW_TX,
    "refund_tx_hash": "h",
    "refund_reason": "deadline_expired",
}

#: Not 'pending': the deadline passed and nobody is working on it.
#: Not 'escrow_entry_unreadable': the entry reads fine. This states the only
#: thing that is true — the money is in the contract and a person decides.
CORRECTED = {
    "refunded": 0,
    "refund_tx_hash": "",
    "refund_reason": "",
    "status": "awaiting_manual_reconciliation",
    "refund_last_error": (
        "row corrected: refunded=1 / status=refunded / refund_tx_hash='h' were "
        "written by a test fixture, not by a refund. Escrow entry 40f6f3da is "
        "live on chain and holds 0.675 USDC. Under refund hold."
    ),
}

#: Columns this script must not touch. Verified after the write.
MUST_NOT_CHANGE = ("task_id", "escrow_tx", "settled", "wallet_address",
                   "agent_handle", "deadline_unix", "created_at",
                   "refund_hold", "refund_hold_reason")

#: Set once in main(), so `verify` can open its own connection for the
#: independent re-read without threading the URL through every call.
_URL_FOR_VERIFY: list[str] = [""]


def fail(msg: str) -> None:
    print(f"ABORT: {msg}", file=sys.stderr)
    sys.exit(2)


def read_row(conn) -> dict | None:
    import psycopg2.extras

    with conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor) as cur:
        cur.execute("SELECT * FROM tasks WHERE task_id = %s", (TASK_ID,))
        r = cur.fetchone()
    return dict(r) if r else None


def check_chain() -> bool:
    """The correction asserts something about money, so it is checked against
    the ledger. An unreachable RPC is NOT taken as agreement."""
    sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "agent" / "src"))
    try:
        from research_agent.escrow import get_escrow_status

        status = get_escrow_status(TASK_ID) or {}
    except Exception as e:
        print(f"  FAIL cannot reach the chain ({type(e).__name__}: {e})")
        print("       Not treated as agreement. The correction says the entry")
        print("       is live; that has to be read from the ledger.")
        return False
    print(f"  ok   status={status.get('escrow_status')!r} "
          f"amount={status.get('amount')!r}")
    print(f"       client={status.get('client')!r}")
    if status.get("status") == "not_found":
        print("  FAIL the entry is NOT readable. The facts this correction")
        print("       rests on have changed — re-run the investigation.")
        return False
    if status.get("amount") not in ("0.6750000", "0.675"):
        print(f"  FAIL amount is {status.get('amount')!r}, expected 0.6750000.")
        print("       The escrow moved. Do not apply this correction.")
        return False
    return True


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--apply", action="store_true")
    ap.add_argument("--rollback", metavar="SNAPSHOT_ID")
    ap.add_argument("--list", action="store_true")
    ap.add_argument("--backup", type=Path)
    ap.add_argument("--database-url", default="")
    ap.add_argument("--expect-database", default="marketplace")
    ap.add_argument("--docker-container", default="")
    args = ap.parse_args()

    url = args.database_url or os.environ.get("DATABASE_URL", "")
    if not url.startswith("postgres"):
        fail("pass --database-url for the PostgreSQL database")
    name = dbsafe.db_name(url)
    print(f"database: {name}")

    if (args.apply or args.rollback) and name != args.expect_database:
        fail(f"database is {name!r} but --expect-database says "
             f"{args.expect_database!r}. Name the target explicitly.")
    if args.apply and not args.backup:
        fail("--apply needs --backup <file>")

    import psycopg2

    _URL_FOR_VERIFY[0] = url
    conn = psycopg2.connect(url)
    conn.autocommit = False
    try:
        if args.list:
            for s in dbsafe.list_snapshots(conn):
                print(f"  {s['rollback_id']}  {s['taken_at']}  "
                      f"{s['rows']} row(s)  {s['reason']}")
            return 0
        if args.rollback:
            return do_rollback(conn, args.rollback)
        return do_correct(conn, url, args)
    finally:
        conn.close()


def read_hold_independently(url: str) -> tuple[bool, str, str]:
    """Read the hold on a connection of its own, committed and closed.

    Not from the connection that is about to write. That connection may sit
    inside a transaction that has already read, or could read, a value nobody
    else can see yet; and a hold is only a protection if it is a committed
    fact that any other process — the sweeper, in particular — would observe.
    Reading it here, on a fresh read-only connection, is the difference between
    "this transaction believes there is a hold" and "there is a hold".

    Returns (held, reason, note).
    """
    import psycopg2
    import psycopg2.extras

    c = psycopg2.connect(url)
    try:
        c.set_session(readonly=True, autocommit=True)
        with c.cursor(cursor_factory=psycopg2.extras.RealDictCursor) as cur:
            cur.execute(
                "SELECT refund_hold, refund_hold_reason, escrow_tx "
                "FROM tasks WHERE task_id = %s",
                (TASK_ID,),
            )
            r = cur.fetchone()
    finally:
        c.close()

    if r is None:
        return False, "", "the row does not exist on an independent read"
    if r["escrow_tx"] != ESCROW_TX:
        return False, "", f"escrow_tx differs on the independent read: {r['escrow_tx']!r}"
    return bool(r["refund_hold"]), (r["refund_hold_reason"] or ""), ""


def do_correct(conn, url: str, args) -> int:
    row = read_row(conn)
    if row is None:
        fail(f"no task {TASK_ID} in {dbsafe.db_name(url)}")

    print("\n== precondition 1: the refund hold is already in place ==")
    print("   (read on a separate, read-only connection — a hold matters only")
    print("    as a committed fact that the sweeper would also see)")
    try:
        held, hold_reason, note = read_hold_independently(url)
    except Exception as e:
        print(f"  FAIL independent read failed ({type(e).__name__}: {e})")
        held, hold_reason, note = False, "", "independent read failed"
    if note:
        print(f"  FAIL {note}")
    if held and not (hold_reason or "").strip():
        print("  FAIL refund_hold = 1 but no reason recorded. A hold nobody "
              "can explain is a hold somebody will clear.")
        held = False
    if held and bool(row.get("refund_hold")) != held:
        print(f"  FAIL this connection sees refund_hold="
              f"{row.get('refund_hold')!r} but an independent read sees "
              f"{int(held)}. Something is mid-transaction; do not write.")
        held = False
    if held:
        print(f"  ok   refund_hold = 1, confirmed independently")
        print(f"       {hold_reason[:110]}")
    else:
        print(f"  FAIL refund_hold = {row.get('refund_hold')!r}")
        print("       Run scripts/hold-escrow-refund.py --apply first. The")
        print("       fields below are, accidentally, the only thing keeping")
        print("       the sweeper away from 0.675 USDC; correcting them")
        print("       without the hold arms an automated transfer.")

    print("\n== precondition 2: the row is the one that was investigated ==")
    drift = {k: (v, row.get(k)) for k, v in EXPECTED_CONTAMINATED.items()
             if row.get(k) != v}
    for k, v in EXPECTED_CONTAMINATED.items():
        print(f"  {'ok  ' if k not in drift else 'FAIL'} {k}: "
              f"expected {v!r}, found {row.get(k)!r}")
    ok_row = not drift
    if drift:
        print("       Something changed this row since the investigation.")

    print("\n== precondition 3: the chain still says the entry is live ==")
    ok_chain = check_chain()

    print("\n== precondition 4: the sweeper would currently refuse it ==")
    sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "agent" / "src"))
    ok_guard = False
    try:
        from research_agent.refund_sweeper import refund_authorisation

        blocked = refund_authorisation(row)
        ok_guard = blocked is not None and "refund hold" in blocked
        print(f"  {'ok  ' if ok_guard else 'FAIL'} {blocked}")
        if not ok_guard and blocked is not None:
            print("       It refuses, but on a condition this script is about")
            print("       to remove — not on the hold. That is the trap.")
    except Exception as e:
        print(f"  FAIL cannot evaluate the guard: {e}")

    print("\n== what would be written ==")
    print("  UPDATE tasks SET")
    for k, v in CORRECTED.items():
        shown = v if not isinstance(v, str) or len(v) <= 70 else v[:70] + "…"
        print(f"      {k} = {shown!r}")
    print(f"   WHERE task_id = '{TASK_ID}'")
    print(f"     AND refund_hold = 1          <-- the guard")
    print(f"\n  Untouched, and verified afterwards: {', '.join(MUST_NOT_CHANGE)}")

    if not args.apply:
        print("\nDRY RUN — nothing was written.")
        print("To apply:  --apply --backup ../agentbrujula-backups/fix-$(date +%F).sql")
        return 0 if (held and ok_row and ok_chain and ok_guard) else 2

    for name, ok in (("refund hold in place", held),
                     ("row matches the investigation", ok_row),
                     ("chain confirms the entry", ok_chain),
                     ("sweeper refuses on the hold", ok_guard)):
        if not ok:
            fail(f"precondition failed: {name}. Nothing was written.")

    print("\n== file backup (disaster copy, includes the schema) ==")
    try:
        size = dbsafe.file_backup(url, args.backup, args.docker_container)
        print(f"  {args.backup}  ({size} bytes, CREATE TABLE present)")
    except dbsafe.DbSafeError as e:
        fail(str(e))

    rollback_id = dbsafe.new_rollback_id(f"fix-{TASK_ID[:8]}")
    print("\n== writing (one transaction) ==")
    try:
        with conn.cursor() as cur:
            n = dbsafe.snapshot_rows(
                conn, [TASK_ID], rollback_id,
                "before correcting fields contaminated by a test fixture",
            )
            print(f"  snapshot {rollback_id}: {n} row")

            sets = ", ".join(f'"{k}" = %s' for k in CORRECTED)
            cur.execute(
                f"UPDATE tasks SET {sets} "
                f" WHERE task_id = %s AND refund_hold = 1",
                tuple(CORRECTED.values()) + (TASK_ID,),
            )
            if cur.rowcount != 1:
                raise dbsafe.DbSafeError(
                    f"touched {cur.rowcount} rows, expected 1. The "
                    f"`AND refund_hold = 1` is what refuses an unheld row."
                )
            print("  row corrected")
    except Exception as e:
        conn.rollback()
        fail(f"{type(e).__name__}: {e} — rolled back, nothing was written")
    conn.commit()

    print("\n== verification ==")
    return verify(conn, rollback_id)


def verify(conn, rollback_id: str) -> int:
    row = read_row(conn)
    checks = [
        ("refunded = 0", int(row.get("refunded") or 0) == 0),
        ("no fabricated refund hash",
         not (row.get("refund_tx_hash") or "").strip()),
        ("status says what is true",
         row.get("status") == CORRECTED["status"]),
        ("escrow_tx intact", row.get("escrow_tx") == ESCROW_TX),
        ("settled still 0", int(row.get("settled") or 0) == 0),
        ("the hold is still in place", bool(row.get("refund_hold"))),
    ]

    # Every column this script promised not to touch, compared against the
    # snapshot taken in the same transaction.
    with conn.cursor() as cur:
        conds = " OR ".join(
            f't."{c}" IS DISTINCT FROM r."{c}"' for c in MUST_NOT_CHANGE
        )
        cur.execute(
            f"SELECT count(*) FROM {dbsafe.ROLLBACK_TABLE} r "
            f"  JOIN tasks t ON t.task_id = r.task_id "
            f" WHERE r.rollback_id = %s AND ({conds})",
            (rollback_id,),
        )
        moved = cur.fetchone()[0]
    checks.append(("no protected column changed", moved == 0))

    # The hold, re-read independently after the commit: the correction must
    # leave behind a protection that another process can see, not one that
    # only this transaction believes in.
    try:
        held_after, reason_after, note = read_hold_independently(_URL_FOR_VERIFY[0])
        ok = held_after and bool(reason_after.strip()) and not note
        print(f"  {'ok  ' if ok else 'FAIL'} the hold is still there on an "
              f"independent read")
        checks.append(("hold confirmed independently after the write", ok))
    except Exception as e:
        print(f"  FAIL independent re-read failed: {e}")
        checks.append(("hold confirmed independently after the write", False))

    sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "agent" / "src"))
    try:
        from research_agent.refund_sweeper import refund_authorisation

        blocked = refund_authorisation(row)
        ok = blocked is not None and "refund hold" in blocked
        print(f"  {'ok  ' if ok else 'FAIL'} the sweeper still refuses, and now "
              f"only because of the hold:\n       {blocked}")
        checks.append(("sweeper refuses on the hold alone", ok))
    except Exception as e:
        print(f"  FAIL could not evaluate the guard: {e}")
        checks.append(("sweeper refuses on the hold alone", False))

    for name, ok in checks:
        print(f"  {'ok  ' if ok else 'FAIL'} {name}")

    if all(ok for _, ok in checks):
        print(f"\nDone. The row is honest and the escrow is still held.")
        print(f"Roll back the correction with:  --rollback {rollback_id}")
        print(f"(That restores the contaminated fields and leaves the hold on.)")
        return 0
    print(f"\nVERIFICATION FAILED. Roll back with:  --rollback {rollback_id}")
    return 1


def do_rollback(conn, rollback_id: str) -> int:
    print(f"\n== rollback {rollback_id} ==")
    try:
        n = dbsafe.restore_rows(conn, rollback_id)
        bad = dbsafe.verify_restored(conn, rollback_id)
    except dbsafe.DbSafeError as e:
        conn.rollback()
        fail(f"{e} — nothing was written")
    if bad:
        conn.rollback()
        fail(f"{len(bad)} rows differ from their snapshot ({bad[:5]}) — "
             f"rolled back, nothing was written")
    conn.commit()
    print(f"  restored {n} row(s), every column verified against the snapshot")
    row = read_row(conn)
    print(f"  refunded={row.get('refunded')!r} status={row.get('status')!r} "
          f"refund_hold={row.get('refund_hold')!r}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
