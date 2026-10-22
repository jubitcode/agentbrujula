"""Backup and rollback primitives for operations on the `tasks` table.

Why this exists: `pg_dump --data-only --table=tasks` — what the repair and
cleanup scripts used to take as their backup — cannot restore anything on its
own. Measured on 2026-10-20 against a throwaway copy of the real database:

    into an empty database   ERROR: relation "public.tasks" does not exist
    over the existing rows   ERROR: duplicate key value violates "tasks_pkey"
    onto a different schema  ERROR: column "agent_handle" does not exist

It fails closed every time, so nothing was ever corrupted — but a backup that
cannot be restored is not a backup, and the rollback path in those scripts
ended in a paragraph telling the operator to figure it out. That is a manual,
incomplete restore, which is precisely what must not be relied on when the
thing at stake is the only local record of a live escrow.

What replaces it, in two layers:

1. **An in-database snapshot**, written in the same transaction as the change.
   `snapshot_rows` copies the affected rows into `tasks_rollback` tagged with
   an id; `restore_rows` puts them back with a single UPDATE joined on that id.
   Atomic, needs no file, no configuration change, no psql, and no decisions
   from whoever runs it. This is the rollback.

2. **A file backup that includes the schema**, for the case the database
   itself is gone. `file_backup` drops `--data-only`, so the dump can recreate
   the table. It is a disaster copy, not the rollback path.
"""

from __future__ import annotations

import subprocess
import sys
import time
from pathlib import Path

ROLLBACK_TABLE = "tasks_rollback"

#: Bookkeeping columns `tasks_rollback` carries in addition to a copy of
#: `tasks`. Kept out of any restore, since they describe the snapshot itself.
META_COLUMNS = ("rollback_id", "taken_at", "rollback_reason")


class DbSafeError(RuntimeError):
    pass


def db_name(url: str) -> str:
    return url.rstrip("/").rsplit("/", 1)[-1].split("?")[0]


def task_columns(conn) -> list[str]:
    """The live column list, read from the catalogue rather than hardcoded, so
    a schema that has gained a column still round-trips completely."""
    with conn.cursor() as cur:
        cur.execute(
            "SELECT column_name FROM information_schema.columns "
            "WHERE table_schema = 'public' AND table_name = 'tasks' "
            "ORDER BY ordinal_position"
        )
        cols = [r[0] for r in cur.fetchall()]
    if not cols:
        raise DbSafeError("table `tasks` has no columns, or does not exist")
    if "task_id" not in cols:
        raise DbSafeError("table `tasks` has no task_id column")
    return cols


def _ensure_rollback_table(conn, cols: list[str]) -> None:
    """Create `tasks_rollback`, or widen it if `tasks` has gained columns.

    No primary key: the same row may be snapshotted by several operations, and
    the whole point is to keep every one of those snapshots.
    """
    with conn.cursor() as cur:
        cur.execute(
            "SELECT 1 FROM information_schema.tables "
            "WHERE table_schema = 'public' AND table_name = %s",
            (ROLLBACK_TABLE,),
        )
        if cur.fetchone() is None:
            cur.execute(
                f"CREATE TABLE {ROLLBACK_TABLE} AS "
                f"SELECT NULL::text AS rollback_id, "
                f"       NULL::timestamptz AS taken_at, "
                f"       NULL::text AS rollback_reason, "
                f"       t.* FROM tasks t WHERE false"
            )
            cur.execute(
                f"CREATE INDEX IF NOT EXISTS {ROLLBACK_TABLE}_id_idx "
                f"ON {ROLLBACK_TABLE} (rollback_id)"
            )
            return

        cur.execute(
            "SELECT column_name FROM information_schema.columns "
            "WHERE table_schema = 'public' AND table_name = %s",
            (ROLLBACK_TABLE,),
        )
        have = {r[0] for r in cur.fetchall()}
        for col in cols:
            if col not in have:
                cur.execute(
                    "SELECT data_type FROM information_schema.columns "
                    "WHERE table_schema = 'public' AND table_name = 'tasks' "
                    "AND column_name = %s",
                    (col,),
                )
                dtype = cur.fetchone()[0]
                cur.execute(
                    f'ALTER TABLE {ROLLBACK_TABLE} ADD COLUMN "{col}" {dtype}'
                )


def new_rollback_id(prefix: str) -> str:
    return f"{prefix}-{time.strftime('%Y%m%dT%H%M%SZ', time.gmtime())}"


def snapshot_rows(conn, task_ids: list[str], rollback_id: str, reason: str) -> int:
    """Copy the named rows into `tasks_rollback`. Call inside the transaction
    that is about to change them.

    Returns the number of rows snapshotted. Raises if it is not exactly
    `len(task_ids)`: a snapshot that silently covers fewer rows than the change
    is worse than none, because it looks like a rollback is available.
    """
    if not task_ids:
        raise DbSafeError("nothing to snapshot")
    cols = task_columns(conn)
    _ensure_rollback_table(conn, cols)

    with conn.cursor() as cur:
        cur.execute(
            f"SELECT count(*) FROM {ROLLBACK_TABLE} WHERE rollback_id = %s",
            (rollback_id,),
        )
        if cur.fetchone()[0]:
            raise DbSafeError(f"rollback id {rollback_id!r} already exists")

        quoted = ", ".join(f't."{c}"' for c in cols)
        target = ", ".join(f'"{c}"' for c in cols)
        cur.execute(
            f"INSERT INTO {ROLLBACK_TABLE} "
            f"  (rollback_id, taken_at, rollback_reason, {target}) "
            f"SELECT %s, now(), %s, {quoted} "
            f"  FROM tasks t WHERE t.task_id = ANY(%s)",
            (rollback_id, reason, task_ids),
        )
        n = cur.rowcount
    if n != len(task_ids):
        raise DbSafeError(
            f"snapshotted {n} rows but {len(task_ids)} were named; "
            f"the missing ones do not exist in `tasks`"
        )
    return n


def list_snapshots(conn) -> list[dict]:
    with conn.cursor() as cur:
        cur.execute(
            "SELECT 1 FROM information_schema.tables "
            "WHERE table_schema = 'public' AND table_name = %s",
            (ROLLBACK_TABLE,),
        )
        if cur.fetchone() is None:
            return []
        cur.execute(
            f"SELECT rollback_id, min(taken_at) AS taken_at, "
            f"       min(rollback_reason) AS reason, count(*) AS rows "
            f"FROM {ROLLBACK_TABLE} GROUP BY rollback_id ORDER BY 2 DESC"
        )
        return [
            {"rollback_id": r[0], "taken_at": r[1], "reason": r[2], "rows": r[3]}
            for r in cur.fetchall()
        ]


def restore_rows(conn, rollback_id: str) -> int:
    """Put every snapshotted row back, in one statement.

    Restores by UPDATE rather than DELETE + INSERT: the row is still there, and
    a DELETE on `tasks` is the operation that must never be part of a recovery
    path near a live escrow.

    A row that the snapshot has but `tasks` no longer does is reported, not
    silently skipped — it means the row was deleted after the snapshot, and
    reinserting it is a different decision.
    """
    cols = [c for c in task_columns(conn) if c != "task_id"]
    with conn.cursor() as cur:
        cur.execute(
            f"SELECT count(*) FROM {ROLLBACK_TABLE} WHERE rollback_id = %s",
            (rollback_id,),
        )
        expected = cur.fetchone()[0]
        if not expected:
            raise DbSafeError(f"no snapshot with id {rollback_id!r}")

        cur.execute(
            f"SELECT r.task_id FROM {ROLLBACK_TABLE} r "
            f"LEFT JOIN tasks t ON t.task_id = r.task_id "
            f"WHERE r.rollback_id = %s AND t.task_id IS NULL",
            (rollback_id,),
        )
        gone = [r[0] for r in cur.fetchall()]
        if gone:
            raise DbSafeError(
                f"{len(gone)} snapshotted rows are no longer in `tasks` "
                f"({gone[:5]}). They were deleted after the snapshot; "
                f"reinserting them is a separate decision, not a rollback."
            )

        sets = ", ".join(f'"{c}" = r."{c}"' for c in cols)
        cur.execute(
            f"UPDATE tasks t SET {sets} "
            f"  FROM {ROLLBACK_TABLE} r "
            f" WHERE r.rollback_id = %s AND t.task_id = r.task_id",
            (rollback_id,),
        )
        n = cur.rowcount
    if n != expected:
        raise DbSafeError(f"restored {n} of {expected} rows")
    return n


def verify_restored(conn, rollback_id: str) -> list[str]:
    """Compare every column of every restored row against its snapshot.

    Returns a list of mismatches, empty when the restore is exact. Checked in
    the database rather than in Python so that type round-tripping is part of
    what is verified.
    """
    cols = task_columns(conn)
    with conn.cursor() as cur:
        conds = " OR ".join(
            f't."{c}" IS DISTINCT FROM r."{c}"' for c in cols
        )
        cur.execute(
            f"SELECT r.task_id FROM {ROLLBACK_TABLE} r "
            f"  JOIN tasks t ON t.task_id = r.task_id "
            f" WHERE r.rollback_id = %s AND ({conds})",
            (rollback_id,),
        )
        bad = [r[0] for r in cur.fetchall()]
    return bad


def file_backup(url: str, path: Path, docker_container: str = "") -> int:
    """Dump `tasks` WITH its schema, so the file can stand on its own.

    `--data-only` is deliberately absent. With it, the dump restores into
    nothing: there is no CREATE TABLE, so an empty database rejects it, a
    populated one rejects it on the primary key, and a different schema rejects
    it on a missing column. All three were measured.

    Written to a temporary file and renamed only once it is complete and has
    been checked. A half-written dump must never carry the final name: the
    first version of this function left a zero-byte file behind when pg_dump
    failed, and its own "refusing to overwrite" guard then blocked the retry.
    """
    if path.exists():
        raise DbSafeError(f"backup file already exists, refusing: {path}")
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(path.suffix + ".partial")
    if tmp.exists():
        tmp.unlink()

    args = ["pg_dump", "--table=tasks", "--no-owner", "--no-acl",
            "--clean", "--if-exists"]
    try:
        if docker_container:
            # Inside the container pg_dump would otherwise authenticate as the
            # container's OS user, which is not the database role. The role
            # comes from the URL so that this cannot drift from the connection
            # the rest of the script uses.
            user = _url_user(url)
            cmd = ["docker", "exec", docker_container, *args,
                   "-U", user, "-d", db_name(url)]
            with tmp.open("wb") as fh:
                r = _run(cmd, stdout=fh)
        else:
            cmd = [*args, "-f", str(tmp), url]
            r = _run(cmd, capture=True)

        if r.returncode != 0:
            raise DbSafeError(
                f"pg_dump failed: "
                f"{r.stderr.decode('utf8', 'replace').strip()[:400]}"
            )
        if not tmp.exists() or tmp.stat().st_size == 0:
            raise DbSafeError("pg_dump produced an empty file")

        text = tmp.read_text(encoding="utf8", errors="replace")
        if "CREATE TABLE" not in text:
            raise DbSafeError(
                "the dump has no CREATE TABLE: it cannot restore on its own"
            )
        if "COPY public.tasks" not in text and "INSERT INTO public.tasks" not in text:
            raise DbSafeError("the dump has no row data for `tasks`")
    except BaseException:
        tmp.unlink(missing_ok=True)
        raise

    size = tmp.stat().st_size
    tmp.rename(path)
    return size


def _run(cmd: list[str], stdout=None, capture: bool = False):
    """Run pg_dump, turning a missing binary into a useful message.

    `pg_dump` is often absent on a host whose PostgreSQL runs in a container.
    A bare FileNotFoundError hides both the cause and the fix.
    """
    try:
        if capture:
            return subprocess.run(cmd, capture_output=True)
        return subprocess.run(cmd, stdout=stdout, stderr=subprocess.PIPE)
    except FileNotFoundError:
        raise DbSafeError(
            f"{cmd[0]!r} is not installed on this machine. Either install the "
            f"PostgreSQL client tools, or pass --docker-container <name> to "
            f"run pg_dump inside the container that hosts the database."
        ) from None


def _url_user(url: str) -> str:
    """The role from a postgres URL, so pg_dump inside a container
    authenticates as the same role the script connects with."""
    from urllib.parse import unquote, urlsplit

    user = urlsplit(url).username
    if not user:
        raise DbSafeError(
            f"no role in the database URL, and pg_dump inside a container "
            f"cannot guess one"
        )
    return unquote(user)
