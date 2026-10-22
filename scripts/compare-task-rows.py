#!/usr/bin/env python3
"""Compare two databases row by row and field by field, and say what differs.

    python3 scripts/compare-task-rows.py --source <url> --target <url>

Written to answer one question about a backup: if it were ever restored, would
the money still be described the same way? Counting rows does not answer that.
A row whose `escrow_tx` survived but whose `refunded` flipped would pass a row
count and would still be a lie about a payment.

So every column of every row is compared, and six are called out by name
because they are the ones that assert something about money:

    task_id         which escrow a row is about
    escrow_tx       the only local link to the transaction on the chain
    refund_tx_hash  the claim that a refund happened
    settled         the claim that the agent was paid
    refunded        the claim that the client got the money back
    refund_hold     the protection against an automated refund

Other tables are compared by row count and by an ordered digest of their
contents, which catches a reordered or partial restore without needing to know
each table's shape.

REFUSES to run against a database named `marketplace`, as either side. This
tool opens read-only transactions, but a comparison is not a reason to point
anything at the application's database, and the name check costs nothing.
"""

from __future__ import annotations

import argparse
import hashlib
import sys

#: Columns whose value is a statement about money. A difference in any of them
#: is reported as a financial discrepancy, not as a difference.
FINANCIAL = ("task_id", "escrow_tx", "refund_tx_hash", "settled", "refunded",
             "refund_hold")

FORBIDDEN_DB = "marketplace"


def fail(msg: str) -> None:
    print(f"ABORT: {msg}", file=sys.stderr)
    sys.exit(2)


def db_name(url: str) -> str:
    return url.rstrip("/").rsplit("/", 1)[-1].split("?")[0]


def connect(url: str, label: str):
    name = db_name(url)
    if name == FORBIDDEN_DB:
        fail(f"{label} is {name!r}. This tool does not open the application "
             f"database, not even to read it.")
    import psycopg2
    import psycopg2.extras

    conn = psycopg2.connect(url)
    conn.set_session(readonly=True, autocommit=True)
    return conn, name


def columns(conn, table: str) -> list[tuple[str, str]]:
    with conn.cursor() as cur:
        cur.execute(
            "SELECT column_name, data_type FROM information_schema.columns "
            "WHERE table_name = %s ORDER BY column_name",
            (table,),
        )
        return [(r[0], r[1]) for r in cur.fetchall()]


def tables(conn) -> list[str]:
    with conn.cursor() as cur:
        cur.execute(
            "SELECT table_name FROM information_schema.tables "
            "WHERE table_schema = 'public' AND table_type = 'BASE TABLE' "
            "ORDER BY table_name"
        )
        return [r[0] for r in cur.fetchall()]


def tasks_by_id(conn, cols: list[str]) -> dict[str, dict]:
    import psycopg2.extras

    sel = ", ".join(f'"{c}"' for c in cols)
    with conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor) as cur:
        cur.execute(f"SELECT {sel} FROM tasks ORDER BY task_id")
        return {r["task_id"]: dict(r) for r in cur.fetchall()}


def digest(conn, table: str) -> tuple[int, str]:
    """Row count and an ordered digest of the table's contents.

    `t.*::text` renders the whole row, so a change in any column of any row
    changes the digest. Ordering by that text makes it independent of physical
    row order, which a restore does not preserve.
    """
    with conn.cursor() as cur:
        cur.execute(f'SELECT count(*) FROM "{table}"')
        n = cur.fetchone()[0]
        cur.execute(f'SELECT t.*::text FROM "{table}" t ORDER BY 1')
        h = hashlib.sha256()
        for (row_text,) in cur.fetchall():
            h.update((row_text or "").encode("utf8"))
            h.update(b"\x00")
        return n, h.hexdigest()[:16]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--source", required=True, help="the database backed up")
    ap.add_argument("--target", required=True, help="the database restored into")
    ap.add_argument("--quiet-ok", action="store_true",
                    help="print only differences and the verdict")
    args = ap.parse_args()

    src, src_name = connect(args.source, "--source")
    dst, dst_name = connect(args.target, "--target")
    print(f"source: {src_name}")
    print(f"target: {dst_name}")
    if src_name == dst_name:
        fail("source and target are the same database name")

    problems: list[str] = []
    financial_problems: list[str] = []

    # -- schema --------------------------------------------------------------
    print("\n== schema of `tasks` ==")
    s_cols, d_cols = columns(src, "tasks"), columns(dst, "tasks")
    s_names = {c for c, _ in s_cols}
    d_names = {c for c, _ in d_cols}
    if s_cols == d_cols:
        print(f"  identical: {len(s_cols)} columns, same names and types")
    else:
        for c in sorted(s_names - d_names):
            problems.append(f"column {c!r} in source but not in target")
            print(f"  MISSING IN TARGET: {c}")
        for c in sorted(d_names - s_names):
            problems.append(f"column {c!r} in target but not in source")
            print(f"  EXTRA IN TARGET:   {c}")
        s_types = dict(s_cols)
        for c, t in d_cols:
            if c in s_types and s_types[c] != t:
                problems.append(f"column {c!r}: {s_types[c]} vs {t}")
                print(f"  TYPE DIFFERS: {c}: source {s_types[c]}, target {t}")

    shared = sorted(s_names & d_names)
    for c in FINANCIAL:
        if c not in shared:
            where = "neither" if c not in s_names | d_names else (
                "source only" if c in s_names else "target only")
            print(f"  note: financial column {c!r} not comparable ({where})")

    # -- tasks, row by row ---------------------------------------------------
    print("\n== `tasks`, field by field ==")
    s_rows = tasks_by_id(src, shared)
    d_rows = tasks_by_id(dst, shared)
    print(f"  source: {len(s_rows)} rows    target: {len(d_rows)} rows")

    only_src = sorted(set(s_rows) - set(d_rows))
    only_dst = sorted(set(d_rows) - set(s_rows))
    for t in only_src:
        msg = f"task {t} is in source but NOT in target"
        problems.append(msg)
        financial_problems.append(msg)
        print(f"  LOST:  {t}")
    for t in only_dst:
        problems.append(f"task {t} is in target but not in source")
        print(f"  EXTRA: {t}")

    compared_rows = 0
    compared_fields = 0
    differing_rows = 0
    for task_id in sorted(set(s_rows) & set(d_rows)):
        a, b = s_rows[task_id], d_rows[task_id]
        diffs = [(c, a[c], b[c]) for c in shared if a[c] != b[c]]
        compared_rows += 1
        compared_fields += len(shared)
        if not diffs:
            continue
        differing_rows += 1
        print(f"  DIFFERS: {task_id}")
        for c, av, bv in diffs:
            tag = "  <-- FINANCIAL" if c in FINANCIAL else ""
            print(f"      {c}: source={av!r} target={bv!r}{tag}")
            msg = f"{task_id}.{c}: {av!r} != {bv!r}"
            problems.append(msg)
            if c in FINANCIAL:
                financial_problems.append(msg)

    if not only_src and not only_dst and differing_rows == 0:
        print(f"  identical: {compared_rows} rows x {len(shared)} columns "
              f"= {compared_fields} field comparisons, all equal")

    # -- the other tables ----------------------------------------------------
    print("\n== other tables (count + ordered digest) ==")
    s_tables, d_tables = set(tables(src)), set(tables(dst))
    for t in sorted(s_tables | d_tables):
        if t == "tasks":
            continue
        if t not in d_tables:
            problems.append(f"table {t!r} missing in target")
            print(f"  MISSING IN TARGET: {t}")
            continue
        if t not in s_tables:
            print(f"  extra in target (ignored): {t}")
            continue
        sn, sh = digest(src, t)
        dn, dh = digest(dst, t)
        if (sn, sh) == (dn, dh):
            if not args.quiet_ok:
                print(f"  ok    {t}: {sn} rows, digest {sh}")
        else:
            problems.append(f"table {t!r}: {sn}/{sh} vs {dn}/{dh}")
            print(f"  DIFFERS {t}: source {sn} rows {sh}, target {dn} rows {dh}")

    # -- verdict -------------------------------------------------------------
    print("\n== verdict ==")
    if financial_problems:
        print(f"  {len(financial_problems)} FINANCIAL discrepancies:")
        for m in financial_problems[:20]:
            print(f"    {m}")
        print("\n  The restore does not describe the money the same way. "
              "Do not rely on it.")
        return 1
    if problems:
        print(f"  {len(problems)} non-financial differences:")
        for m in problems[:20]:
            print(f"    {m}")
        print("\n  Every financial field matches, but the databases are not "
              "identical. Read the list before relying on the restore.")
        return 1
    print(f"  No differences. {compared_rows} task rows compared across "
          f"{len(shared)} columns ({compared_fields} field comparisons), "
          f"plus every other table by count and digest.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
