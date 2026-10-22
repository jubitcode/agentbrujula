#!/usr/bin/env python3
"""Add the refund_hold columns. Only that. Does not start the API.

    Show what it would do (default, read-only):
        python3 scripts/migrate-schema.py --database-url <url>

    Apply (requires a verified full backup):
        python3 scripts/migrate-schema.py --apply --database-url <url> \
            --require-backup ../agentbrujula-backups/marketplace-2026-10-21.sql

WHY THIS EXISTS INSTEAD OF "JUST START THE API ONCE"
-----------------------------------------------------
Starting the API does add the columns — `TaskStore.__init__` runs the ALTER
TABLE. But it does five other things, and two of them delete rows. Audited in
`api/server.py` on 2026-10-21:

  at import time (before any endpoint, before the lifespan):
    lines 14-21   reads .env with os.environ.setdefault
    line 360      _store = TaskStore()        <- the ALTER TABLE we want

  every 60 seconds, from the lifespan's `_refund_sweeper`:
    line 92       sweep_expired_refunds(...)  only `if orch_kp`
    line 124      _store.cleanup(86400)       ALWAYS. DELETEs tasks by age
    line 137      _store.cleanup_nonces(300)  ALWAYS. DELETEs nonces by age

`cleanup` is outside the `if orch_kp:` block, so removing the orchestrator key
does not stop it. Measured against a throwaway copy of the real database, with
every row aged past the 24h threshold:

    cleanup: {'deleted': 32, 'preserved_open': 0}
    the row holding 0.675 USDC: gone

It is not preserved by `_PRESERVE_SQL` precisely because its data is false:
`refunded = 1` makes it look resolved. So the row can only be protected once
`refund_hold` exists — and adding that column by starting the API is what
would destroy it. This script breaks that circle.

And `env -u ORCHESTRATOR_SECRET_KEY` does not do what it looks like: the import
reads .env with `setdefault`, which puts the key back when the variable is
absent from the environment. It happens to be harmless today only because that
line in .env is empty. Once the key is filled in — the next step of this
project — that flag stops protecting anything.

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

#: Must match research_agent.task_store._REQUIRED_TASK_COLUMNS_PG. Read from
#: there rather than copied, so the two cannot drift.
def wanted_columns() -> dict[str, str]:
    sys.path.insert(
        0, str(Path(__file__).resolve().parent.parent / "agent" / "src")
    )
    from research_agent.task_store import _REQUIRED_TASK_COLUMNS_PG

    return dict(_REQUIRED_TASK_COLUMNS_PG)


def fail(msg: str) -> None:
    print(f"ABORT: {msg}", file=sys.stderr)
    sys.exit(2)


def verify_backup(path: Path, expect_tables: set[str]) -> None:
    """A backup is a precondition, not a formality: check it covers the tables
    whose loss would be unrecoverable before writing anything."""
    if not path.exists():
        fail(f"no backup at {path}. Take one first:\n"
             f"  docker compose exec -T postgres pg_dump -U agent_web3 "
             f"--no-owner --no-acl -d marketplace > {path}")
    size = path.stat().st_size
    if size == 0:
        fail(f"{path} is empty")
    text = path.read_text(encoding="utf8", errors="replace")

    missing = {t for t in expect_tables
               if f"CREATE TABLE public.{t}" not in text}
    if missing:
        fail(f"{path} has no CREATE TABLE for: {sorted(missing)}. "
             f"A table-scoped dump is not enough here — losing `nonces` "
             f"removes replay protection, and losing `tasks` removes the only "
             f"local record of a live escrow.")
    print(f"  ok   {path.name}: {size} bytes")
    print(f"  ok   covers: {', '.join(sorted(expect_tables))}")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--apply", action="store_true")
    ap.add_argument("--database-url", default="")
    ap.add_argument("--expect-database", default="marketplace")
    ap.add_argument("--require-backup", type=Path,
                    help="a full-database dump that must exist and cover the "
                         "critical tables before anything is written")
    ap.add_argument("--critical-tables", default="tasks,nonces",
                    help="tables the backup must contain (comma separated)")
    args = ap.parse_args()

    url = args.database_url or os.environ.get("DATABASE_URL", "")
    if not url.startswith("postgres"):
        fail("pass --database-url for the PostgreSQL database")
    name = dbsafe.db_name(url)
    print(f"database: {name}")
    if args.apply and name != args.expect_database:
        fail(f"database is {name!r} but --expect-database says "
             f"{args.expect_database!r}. Name the target explicitly.")

    import psycopg2

    conn = psycopg2.connect(url)
    conn.autocommit = False
    try:
        wanted = wanted_columns()
        with conn.cursor() as cur:
            cur.execute(
                "SELECT column_name FROM information_schema.columns "
                "WHERE table_schema = 'public' AND table_name = 'tasks'"
            )
            have = {r[0] for r in cur.fetchall()}
            cur.execute("SELECT count(*) FROM tasks")
            rows_before = cur.fetchone()[0]
            cur.execute("SELECT count(*) FROM nonces")
            nonces_before = cur.fetchone()[0]

        print(f"\n== current state ==")
        print(f"  tasks: {rows_before} rows, {len(have)} columns")
        print(f"  nonces: {nonces_before} rows")

        print(f"\n== columns this migration adds ==")
        missing = {c: d for c, d in wanted.items() if c not in have}
        for c, ddl in wanted.items():
            print(f"  {'ADD ' if c in missing else 'have'} {c} {ddl}")
        if not missing:
            print("\n  Nothing to do: every column is already present.")
            return 0

        print(f"\n== the statements ==")
        for c, ddl in missing.items():
            print(f"  ALTER TABLE tasks ADD COLUMN IF NOT EXISTS {c} {ddl};")
        print(f"\n  Nothing else. No DELETE, no UPDATE, no background task,")
        print(f"  no .env read, no orchestrator key, no RPC call.")
        print(f"  Verified on a throwaway copy of the real data: all 32 rows")
        print(f"  stay byte-identical, refund_hold = 0 with no NULLs, and")
        print(f"  running it twice is a no-op.")

        if not args.apply:
            print(f"\nDRY RUN — nothing was written.")
            print(f"To apply:  --apply --require-backup <full dump>")
            return 0

        print(f"\n== precondition: a verified full backup ==")
        if not args.require_backup:
            fail("--apply needs --require-backup <full dump>. The backup comes "
                 "first, before the first write of the sequence.")
        verify_backup(args.require_backup,
                      {t.strip() for t in args.critical_tables.split(",") if t.strip()})

        print(f"\n== writing ==")
        with conn.cursor() as cur:
            for c, ddl in missing.items():
                cur.execute(f"ALTER TABLE tasks ADD COLUMN IF NOT EXISTS {c} {ddl}")
                print(f"  added {c}")
            cur.execute("SELECT count(*) FROM tasks")
            rows_after = cur.fetchone()[0]
            cur.execute("SELECT count(*) FROM nonces")
            nonces_after = cur.fetchone()[0]
            if rows_after != rows_before or nonces_after != nonces_before:
                conn.rollback()
                fail(f"row counts changed (tasks {rows_before}->{rows_after}, "
                     f"nonces {nonces_before}->{nonces_after}) — rolled back")
        conn.commit()

        print(f"\n== verification ==")
        with conn.cursor() as cur:
            cur.execute(
                "SELECT column_name, column_default FROM information_schema.columns "
                "WHERE table_schema = 'public' AND table_name = 'tasks' "
                "AND column_name = ANY(%s) ORDER BY column_name",
                (sorted(wanted),),
            )
            found = cur.fetchall()
            cur.execute("SELECT count(*) FROM tasks WHERE refund_hold IS NULL")
            nulls = cur.fetchone()[0]
            cur.execute("SELECT count(*) FROM tasks")
            rows_now = cur.fetchone()[0]
            cur.execute("SELECT count(*) FROM nonces")
            nonces_now = cur.fetchone()[0]

        checks = [
            (f"all {len(wanted)} columns present", len(found) == len(wanted)),
            ("no NULLs in refund_hold", nulls == 0),
            (f"tasks still {rows_before} rows", rows_now == rows_before),
            (f"nonces still {nonces_before} rows", nonces_now == nonces_before),
        ]
        for c, default in found:
            print(f"       {c}: default={default!r}")
        for n, ok in checks:
            print(f"  {'ok  ' if ok else 'FAIL'} {n}")

        if not all(ok for _, ok in checks):
            print(f"\nVERIFICATION FAILED. See --rollback guidance below.")
            return 1

        print(f"\nDone. To reverse:")
        for c in missing:
            print(f"  ALTER TABLE tasks DROP COLUMN IF EXISTS {c};")
        print(f"\n  Dropping them is safe while nothing has been held yet: the")
        print(f"  application ran without these columns until now, and")
        print(f"  TaskStore re-adds them on its next connection. Once a hold")
        print(f"  has been set, dropping the column DELETES THE PROTECTION —")
        print(f"  roll back the hold first, or restore from the backup.")
        return 0
    finally:
        conn.close()


if __name__ == "__main__":
    sys.exit(main())
