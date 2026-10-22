#!/usr/bin/env python3
"""Put one deleted task row back, from a full pg_dump, and verify it.

    Show what it would do (default, read-only):
        python3 scripts/restore-deleted-row.py --backup <dump.sql> \
            --database-url <url> --expect-database marketplace

    Apply:
        ... --apply

Task `40f6f3daf4c546fda37c19afbe8bcc5f` disappeared from `marketplace`
between two reads on 2026-10-21, while its escrow entry stayed live on chain
holding 0.675 USDC. The funds are safe; what was lost is the only local record
naming them — the same way the 0.070 USDC became unattributable.

Restores a single row rather than the whole table, because 31 other rows have
been there the whole time and a table-wide reload would also undo anything
legitimate since the dump. The row is read out of the dump's own COPY block,
inserted, and then compared field by field against what the dump holds.

It will not overwrite a row that is already there: if the task exists, this
script is not what you need.

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

DEFAULT_TASK = "40f6f3daf4c546fda37c19afbe8bcc5f"


def fail(msg: str) -> None:
    print(f"ABORT: {msg}", file=sys.stderr)
    sys.exit(2)


def parse_copy_block(dump_text: str, task_id: str) -> tuple[list[str], list[str]]:
    """Pull the column list and one row out of `COPY public.tasks ... FROM stdin`.

    pg_dump writes tab-separated values with \\N for NULL and backslash
    escapes for tabs, newlines and backslashes themselves. Decoding those is
    the whole job: a row restored with a literal "\\n" in its text would look
    restored and be wrong.
    """
    start = None
    columns: list[str] = []
    for line in dump_text.splitlines():
        if line.startswith("COPY public.tasks ("):
            inner = line[len("COPY public.tasks ("):].split(")")[0]
            columns = [c.strip().strip('"') for c in inner.split(",")]
            start = True
            continue
        if start:
            if line == "\\.":
                break
            fields = line.split("\t")
            if fields and fields[0] == task_id:
                return columns, fields
    if not columns:
        fail("the dump has no COPY block for public.tasks")
    fail(f"the dump has no row for task {task_id}")
    raise AssertionError  # unreachable, keeps type checkers calm


def decode(value: str):
    if value == "\\N":
        return None
    out = []
    i = 0
    while i < len(value):
        ch = value[i]
        if ch == "\\" and i + 1 < len(value):
            nxt = value[i + 1]
            out.append({"n": "\n", "t": "\t", "r": "\r", "\\": "\\"}.get(nxt, nxt))
            i += 2
            continue
        out.append(ch)
        i += 1
    return "".join(out)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--backup", type=Path, required=True)
    ap.add_argument("--task-id", default=DEFAULT_TASK)
    ap.add_argument("--database-url", default="")
    ap.add_argument("--expect-database", default="marketplace")
    ap.add_argument("--apply", action="store_true")
    args = ap.parse_args()

    url = args.database_url or os.environ.get("DATABASE_URL", "")
    if not url.startswith("postgres"):
        fail("pass --database-url")
    name = dbsafe.db_name(url)
    print(f"database: {name}")
    if args.apply and name != args.expect_database:
        fail(f"database is {name!r}, --expect-database says "
             f"{args.expect_database!r}")

    if not args.backup.exists():
        fail(f"no backup at {args.backup}")
    text = args.backup.read_text(encoding="utf8", errors="replace")
    columns, raw = parse_copy_block(text, args.task_id)
    values = [decode(v) for v in raw]
    row = dict(zip(columns, values))

    print(f"\n== the row, as the backup holds it ==")
    for k in ("task_id", "wallet_address", "agent_handle", "escrow_tx",
              "status", "settled", "refunded", "refund_tx_hash",
              "deadline_unix", "created_at"):
        if k in row:
            v = row[k]
            shown = v if v is None or len(str(v)) <= 70 else str(v)[:70] + "…"
            print(f"  {k:16} {shown!r}")

    import psycopg2
    import psycopg2.extras

    conn = psycopg2.connect(url)
    conn.autocommit = False
    try:
        print(f"\n== precondition: the row is actually missing ==")
        with conn.cursor() as cur:
            cur.execute("SELECT count(*) FROM tasks WHERE task_id = %s",
                        (args.task_id,))
            present = cur.fetchone()[0]
        if present:
            fail(f"task {args.task_id} is already in {name}. This script "
                 f"restores a deleted row and will not overwrite a live one.")
        print(f"  ok   not present in {name}")

        print(f"\n== precondition: the live schema can hold it ==")
        live = set(dbsafe.task_columns(conn))
        missing = [c for c in columns if c not in live]
        extra = sorted(live - set(columns))
        if missing:
            fail(f"the dump has columns the table does not: {missing}")
        print(f"  ok   all {len(columns)} dumped columns exist")
        if extra:
            print(f"  note columns added since the dump, left at their "
                  f"defaults: {extra}")

        print(f"\n== what would be written ==")
        cols_sql = ", ".join(f'"{c}"' for c in columns)
        print(f"  INSERT INTO tasks ({cols_sql}) VALUES (…{len(columns)} values…)")
        print(f"  One row. Nothing else is touched.")

        if not args.apply:
            print(f"\nDRY RUN — nothing was written.")
            print(f"To apply:  --apply")
            return 0

        with conn.cursor() as cur:
            ph = ", ".join(["%s"] * len(columns))
            cur.execute(
                f"INSERT INTO tasks ({cols_sql}) VALUES ({ph})",
                tuple(values),
            )
            if cur.rowcount != 1:
                conn.rollback()
                fail(f"inserted {cur.rowcount} rows, expected 1 — rolled back")
        conn.commit()

        print(f"\n== verification, field by field against the backup ==")
        with conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor) as cur:
            cur.execute("SELECT * FROM tasks WHERE task_id = %s",
                        (args.task_id,))
            restored = dict(cur.fetchone())

        bad = []
        for c in columns:
            want = row[c]
            got = restored.get(c)
            # The dump is text; the table is typed. Compare as text, which is
            # what the dump round-trips through anyway.
            if (want is None) != (got is None):
                bad.append((c, want, got))
            elif want is not None and str(got) != str(want):
                # Numeric formatting differs (1792510302.438025 vs Decimal).
                try:
                    if abs(float(got) - float(want)) < 1e-9:
                        continue
                except (TypeError, ValueError):
                    pass
                bad.append((c, want, got))
        for c, want, got in bad:
            print(f"  FAIL {c}: backup {want!r}, restored {got!r}")
        if bad:
            print(f"\n{len(bad)} field(s) differ. The row is back but not "
                  f"identical — read the list before relying on it.")
            return 1
        print(f"  ok   all {len(columns)} fields match the backup")

        print(f"\n== and what the chain says about it ==")
        sys.path.insert(
            0, str(Path(__file__).resolve().parent.parent / "agent" / "src")
        )
        try:
            from research_agent.escrow import get_escrow_status

            chain = get_escrow_status(args.task_id) or {}
            print(f"  escrow entry: {chain.get('escrow_status')} "
                  f"{chain.get('amount')} USDC")
            if chain.get("escrow_status") == "Created":
                print(f"  ok   the funds are still there, and now a row names them")
        except Exception as e:
            print(f"  could not read the chain ({type(e).__name__}: {e})")

        print(f"\nDone. The row is back.")
        print(f"\n  It is NOT protected: `refund_hold` does not exist on this")
        print(f"  database yet, and the row still carries the false")
        print(f"  refunded = 1. Stage 1 and 2 of PLAN-DE-EJECUCION-POR-ETAPAS")
        print(f"  are what close that.")
        return 0
    finally:
        conn.close()


if __name__ == "__main__":
    sys.exit(main())
