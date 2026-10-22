#!/usr/bin/env python3
"""Separate rows left by test runs from rows that record real money.

    Classify and print (default, reads nothing but `tasks`):
        python3 scripts/classify-test-rows.py --database-url ...

    Archive the discardable rows into `tasks_test_archive`, keeping them:
        ... --archive --backup <file>

    Delete them, only after they are archived:
        ... --delete --backup <file>

HOW A ROW IS PROVED DISCARDABLE
-------------------------------
Not by looking wrong. A Stellar transaction hash is 64 hexadecimal characters,
always — that is the format, not a convention. So a row's class follows from
what it can possibly refer to:

  A  escrow_tx is 64 hex chars AND the chain confirms it
     The only class that can be evidence of a payment. Never touched here.

  C  escrow_tx is empty
     Nothing was ever submitted to the network under this task, so no
     transaction exists that this row could be the record of. There is no
     financial evidence to preserve because there was never a transaction.

  D  escrow_tx is present but cannot be a transaction hash
     Measured values: 'abc' (3 chars), 'tx' (2), 'mock_escrow_tx' (14). These
     three literals appear in agent/tests/ and nowhere else in the system.

  B  escrow_tx is 64 hex chars but the chain does not know it
     Would need a decision. On 2026-10-20 this class was EMPTY: all 32 hashes
     were checked against Horizon and exactly one resolved.

WHAT THIS DOES NOT PROVE
------------------------
That no money is unaccounted for. `usdc-escrow` holds 0.745 USDC and only
0.675 is attributable to class A; 0.070 belongs to escrows whose rows were
deleted by the old `cleanup` before this was noticed. Those rows are already
gone — the 31 here do not explain the 0.070, and deleting them does not
destroy that evidence, because the evidence was lost in 2026 when housekeeping
ran. Recovering it means reading `create_escrow` calls out of the contract's
transaction history, which is a separate job.

Backups are written OUTSIDE the repository, to
../agentbrujula-backups/: a dump of this database carries wallet
addresses, the text of hired tasks and a real payment history, and
none of that belongs in version control.
"""

from __future__ import annotations

import argparse
import os
import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import dbsafe  # noqa: E402

HEX64 = re.compile(r"^[0-9a-f]{64}$")

#: Verified live on 2026-10-20: entry present, status Created, 0.675 USDC.
KNOWN_REAL = {"40f6f3daf4c546fda37c19afbe8bcc5f"}

ARCHIVE_TABLE = "tasks_test_archive"


def fail(msg: str) -> None:
    print(f"ABORT: {msg}", file=sys.stderr)
    sys.exit(1)


def classify(row: dict) -> str:
    tx = (row.get("escrow_tx") or "").strip()
    if row["task_id"] in KNOWN_REAL:
        return "A"
    if not tx:
        return "C"
    if HEX64.match(tx.lower()):
        return "B"
    return "D"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--database-url", default="")
    ap.add_argument("--expect-database", default="marketplace")
    ap.add_argument("--archive", action="store_true",
                    help=f"copy class C and D rows into {ARCHIVE_TABLE}")
    ap.add_argument("--delete", action="store_true",
                    help="delete class C and D rows (requires them archived)")
    ap.add_argument("--restore-archive", action="store_true",
                    help=f"put every row in {ARCHIVE_TABLE} back into tasks")
    ap.add_argument("--verify-archive", action="store_true",
                    help="compare the archive against tasks, field by field")
    ap.add_argument("--docker-container", default="",
                    help="run pg_dump inside this container instead of locally")
    ap.add_argument("--backup", type=Path)
    args = ap.parse_args()

    writing = args.archive or args.delete or args.restore_archive
    if (args.archive or args.delete) and not args.backup:
        fail("--archive and --delete need --backup <file>")

    url = args.database_url or os.environ.get("DATABASE_URL", "")
    if not url.startswith("postgres"):
        fail("pass --database-url for the PostgreSQL application database")
    name = url.rstrip("/").rsplit("/", 1)[-1].split("?")[0]
    print(f"database: {name}")
    if writing and name != args.expect_database:
        fail(f"database is {name!r}, --expect-database says {args.expect_database!r}")

    import psycopg2
    import psycopg2.extras

    conn = psycopg2.connect(url)
    conn.autocommit = False
    try:
        with conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor) as cur:
            cur.execute("SELECT * FROM tasks")
            rows = [dict(r) for r in cur.fetchall()]

        buckets: dict[str, list[dict]] = {"A": [], "B": [], "C": [], "D": []}
        for r in rows:
            buckets[classify(r)].append(r)

        print(f"\n{len(rows)} rows\n")
        labels = {
            "A": "real — escrow confirmed on chain. NEVER touched by this script",
            "B": "64-hex hash the chain does not know. NEEDS A DECISION",
            "C": "no escrow_tx: nothing was ever submitted",
            "D": "escrow_tx cannot be a transaction hash",
        }
        for k in "ABCD":
            print(f"  {k}  {len(buckets[k]):>3}  {labels[k]}")

        if buckets["B"]:
            print("\n  Class B is not empty. Each of these has to be checked")
            print("  against the chain before anything is archived or deleted:")
            for r in buckets["B"]:
                print(f"    {r['task_id']}  {r['escrow_tx']}")
            fail("class B is non-empty; nothing was written")

        print("\n  class A (kept, and the reason this matters):")
        for r in buckets["A"]:
            print(f"    {r['task_id']}  {r['escrow_tx']}")
            print(f"      refunded={r.get('refunded')!r} status={r.get('status')!r} "
                  f"refund_hold={r.get('refund_hold')!r}")

        fixture_hashes = sorted(
            {(r.get("refund_tx_hash") or "").strip()
             for r in buckets["C"] + buckets["D"]} - {""}
        )
        print(f"\n  refund_tx_hash values on C and D: {fixture_hashes}")
        print("    None of these is 64 hex characters, so none of them names a")
        print("    refund that happened. They are the literals in the fixtures.")

        discardable = buckets["C"] + buckets["D"]
        print(f"\n  discardable: {len(discardable)} rows")

        if args.verify_archive or args.restore_archive:
            return archive_ops(conn, args, buckets)

        if not writing:
            print("\nDRY RUN — nothing was written.")
            print(f"  --archive --backup <file>   copy them into {ARCHIVE_TABLE}")
            print( "  --delete  --backup <file>   remove them (archive first)")
            print( "  --verify-archive            compare the archive to tasks")
            print( "  --restore-archive           put the archived rows back")
            return 0

        # The class A row must be protected before anything is deleted. A
        # DELETE with a wrong predicate is how the only record of a live escrow
        # disappears, and that has already happened once on this table.
        held = all(r.get("refund_hold") for r in buckets["A"])
        if args.delete and not held:
            fail(
                "the class A row is not under a refund hold. Run "
                "scripts/hold-escrow-refund.py --apply first: a DELETE near "
                "the only live escrow in the system needs that row protected "
                "deliberately, not just excluded by a WHERE clause."
            )

        # With --data-only this dump restored into nothing: no CREATE TABLE,
        # so an empty database rejects it, a populated one rejects it on the
        # primary key, and a different schema on a missing column. All three
        # measured. `dbsafe.file_backup` includes the schema.
        try:
            size = dbsafe.file_backup(url, args.backup, args.docker_container)
        except dbsafe.DbSafeError as e:
            fail(str(e))
        print(f"\n  backup: {args.backup} ({size} bytes, CREATE TABLE present)")

        ids = [r["task_id"] for r in discardable]
        with conn.cursor() as cur:
            if args.archive:
                cur.execute(
                    f"CREATE TABLE IF NOT EXISTS {ARCHIVE_TABLE} "
                    f"(LIKE tasks INCLUDING DEFAULTS)"
                )
                cur.execute(
                    f"INSERT INTO {ARCHIVE_TABLE} SELECT * FROM tasks "
                    f"WHERE task_id = ANY(%s) "
                    f"ON CONFLICT DO NOTHING",
                    (ids,),
                )
                print(f"  archived: {cur.rowcount} rows into {ARCHIVE_TABLE}")

                # An archive is only reversible if it is complete. Compared
                # column by column against the live rows, in the database, so
                # type round-tripping is part of what is checked.
                cols = dbsafe.task_columns(conn)
                conds = " OR ".join(
                    f'a."{c}" IS DISTINCT FROM t."{c}"' for c in cols
                )
                cur.execute(
                    f"SELECT count(*) FROM {ARCHIVE_TABLE} a "
                    f"  JOIN tasks t ON t.task_id = a.task_id "
                    f" WHERE a.task_id = ANY(%s) AND ({conds})",
                    (ids,),
                )
                differing = cur.fetchone()[0]
                cur.execute(
                    f"SELECT count(*) FROM {ARCHIVE_TABLE} WHERE task_id = ANY(%s)",
                    (ids,),
                )
                archived_n = cur.fetchone()[0]
                print(f"  verified: {archived_n} of {len(ids)} present, "
                      f"{differing} with any column differing "
                      f"({len(cols)} columns compared)")
                if differing or archived_n != len(ids):
                    conn.rollback()
                    fail("the archive does not match the live rows; "
                         "rolled back, nothing was archived")

            if args.delete:
                cur.execute(
                    f"SELECT count(*) FROM {ARCHIVE_TABLE} WHERE task_id = ANY(%s)",
                    (ids,),
                )
                archived = cur.fetchone()[0]
                if archived != len(ids):
                    conn.rollback()
                    fail(
                        f"{archived} of {len(ids)} are in {ARCHIVE_TABLE}. "
                        f"Run --archive first; nothing was deleted."
                    )
                cur.execute("DELETE FROM tasks WHERE task_id = ANY(%s)", (ids,))
                if cur.rowcount != len(ids):
                    conn.rollback()
                    fail(f"DELETE touched {cur.rowcount}, expected {len(ids)} "
                         f"— rolled back")
                print(f"  deleted: {cur.rowcount} rows")
        conn.commit()

        with conn.cursor() as cur:
            cur.execute("SELECT count(*) FROM tasks")
            left = cur.fetchone()[0]
            cur.execute("SELECT count(*) FROM tasks WHERE task_id = ANY(%s)",
                        (sorted(KNOWN_REAL),))
            real_still_there = cur.fetchone()[0]
        print(f"\n  tasks now: {left}")
        print(f"  the real row is still present: {real_still_there == len(KNOWN_REAL)}")
        return 0 if real_still_there == len(KNOWN_REAL) else 1
    finally:
        conn.close()


def archive_ops(conn, args, buckets) -> int:
    """Verify, or reverse, the archive. This is what makes archiving a
    decision that can be taken back."""
    with conn.cursor() as cur:
        cur.execute(
            "SELECT 1 FROM information_schema.tables "
            "WHERE table_schema = 'public' AND table_name = %s",
            (ARCHIVE_TABLE,),
        )
        if cur.fetchone() is None:
            fail(f"{ARCHIVE_TABLE} does not exist: nothing was ever archived")

        cols = dbsafe.task_columns(conn)
        cur.execute(f"SELECT count(*) FROM {ARCHIVE_TABLE}")
        n_arch = cur.fetchone()[0]
        cur.execute(
            f"SELECT a.task_id FROM {ARCHIVE_TABLE} a "
            f"LEFT JOIN tasks t ON t.task_id = a.task_id "
            f"WHERE t.task_id IS NULL ORDER BY 1"
        )
        absent = [r[0] for r in cur.fetchall()]

        conds = " OR ".join(f'a."{c}" IS DISTINCT FROM t."{c}"' for c in cols)
        cur.execute(
            f"SELECT a.task_id FROM {ARCHIVE_TABLE} a "
            f"  JOIN tasks t ON t.task_id = a.task_id "
            f" WHERE ({conds}) ORDER BY 1"
        )
        differing = [r[0] for r in cur.fetchall()]

    print(f"\n== archive ==")
    print(f"  {ARCHIVE_TABLE}: {n_arch} rows, {len(cols)} columns")
    print(f"  still present in tasks:  {n_arch - len(absent)}")
    print(f"  deleted from tasks:      {len(absent)}")
    print(f"  present but differing:   {len(differing)}")
    for t in differing[:5]:
        print(f"    DIFFERS: {t}")

    # An archived row must never be the class A row.
    real_in_archive = sorted(set(KNOWN_REAL) & set(
        _archive_ids(conn)))
    if real_in_archive:
        fail(f"the real row is in the archive: {real_in_archive}. "
             f"That is a bug in the classification, not something to reverse.")
    print(f"  the real row is NOT in the archive: ok")

    if args.verify_archive and not args.restore_archive:
        if differing:
            print("\n  The archive does not match the live rows. Read the list.")
            return 1
        print("\n  Every archived row that is still in `tasks` matches it "
              "column for column.")
        return 0

    if not absent:
        print("\n  Nothing to restore: every archived row is still in `tasks`.")
        return 0

    print(f"\n== restoring {len(absent)} rows into `tasks` ==")
    quoted = ", ".join(f'"{c}"' for c in cols)
    with conn.cursor() as cur:
        cur.execute(
            f"INSERT INTO tasks ({quoted}) "
            f"SELECT {quoted} FROM {ARCHIVE_TABLE} WHERE task_id = ANY(%s)",
            (absent,),
        )
        inserted = cur.rowcount
        if inserted != len(absent):
            conn.rollback()
            fail(f"inserted {inserted} of {len(absent)} — rolled back")
        conds = " OR ".join(f'a."{c}" IS DISTINCT FROM t."{c}"' for c in cols)
        cur.execute(
            f"SELECT count(*) FROM {ARCHIVE_TABLE} a "
            f"  JOIN tasks t ON t.task_id = a.task_id "
            f" WHERE a.task_id = ANY(%s) AND ({conds})",
            (absent,),
        )
        bad = cur.fetchone()[0]
        if bad:
            conn.rollback()
            fail(f"{bad} restored rows differ from the archive — rolled back")
    conn.commit()
    print(f"  restored {inserted} rows, every column verified against "
          f"{ARCHIVE_TABLE}")
    return 0


def _archive_ids(conn) -> list[str]:
    with conn.cursor() as cur:
        cur.execute(f"SELECT task_id FROM {ARCHIVE_TABLE}")
        return [r[0] for r in cur.fetchall()]


if __name__ == "__main__":
    sys.exit(main())
