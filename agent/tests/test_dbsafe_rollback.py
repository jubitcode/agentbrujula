"""A backup is a claim that the state can be restored. These check the claim.

Why they exist: the backup the repair scripts used to take —
`pg_dump --data-only --table=tasks` — could not restore anything. Measured on
2026-10-20 against a throwaway copy of the real database:

    into an empty database   ERROR: relation "public.tasks" does not exist
    over the existing rows   ERROR: duplicate key value violates "tasks_pkey"
    onto a different schema  ERROR: column "agent_handle" does not exist

It failed closed every time, so nothing was corrupted — but the rollback path
in those scripts ended in a paragraph telling the operator to work it out, and
the thing at stake is the only local record of a live escrow.

PostgreSQL only: `dbsafe` is for the application's database, and the behaviour
being checked (snapshot inside the writing transaction, restore in one
statement, type round-tripping) is specific to it. Set TEST_DATABASE_URL to a
throwaway database to run them.
"""

import os
import sys
import time
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent.parent / "scripts"))

TEST_DATABASE_URL = os.environ.get("TEST_DATABASE_URL", "")

REAL_TX = "e6d6a1a06f1fd7b57d57aa8f1c96a63dae167f256ee2ecba343f2f20b08a7966"


def _pg_available() -> bool:
    if not TEST_DATABASE_URL:
        return False
    if TEST_DATABASE_URL.rstrip("/").endswith("/marketplace"):
        raise RuntimeError(
            "TEST_DATABASE_URL points at the application database. These tests "
            "write and delete rows. Use a throwaway database."
        )
    try:
        import psycopg2

        psycopg2.connect(TEST_DATABASE_URL).close()
        return True
    except Exception:
        return False


requires_pg = pytest.mark.skipif(
    not _pg_available(),
    reason="set TEST_DATABASE_URL to a throwaway PostgreSQL database",
)

#: `pg_dump` is usually absent on a host whose PostgreSQL runs in a container.
#: Set TEST_PG_DOCKER_CONTAINER to that container's name and the file-backup
#: tests run through it; otherwise they skip rather than pretend to pass.
PG_DOCKER = os.environ.get("TEST_PG_DOCKER_CONTAINER", "")


def _pg_dump_reachable() -> bool:
    import shutil
    import subprocess

    if shutil.which("pg_dump"):
        return True
    if not PG_DOCKER:
        return False
    try:
        r = subprocess.run(
            ["docker", "exec", PG_DOCKER, "pg_dump", "--version"],
            capture_output=True, timeout=20,
        )
        return r.returncode == 0
    except Exception:
        return False


requires_pg_dump = pytest.mark.skipif(
    not _pg_dump_reachable(),
    reason="pg_dump not available: install the client tools or set "
           "TEST_PG_DOCKER_CONTAINER to the database's container",
)


@pytest.fixture
def conn():
    """A connection on a table with the current schema and a few rows.

    Truncates `tasks`, `tasks_rollback` and `tasks_test_archive`, so the URL
    must never be the application's — `_pg_available` refuses one ending in
    /marketplace and the tests skip without a URL.
    """
    import psycopg2

    from research_agent.task_store import TaskStore

    store = TaskStore(database_url=TEST_DATABASE_URL)  # creates/migrates schema
    store.close()

    c = psycopg2.connect(TEST_DATABASE_URL)
    c.autocommit = False
    with c.cursor() as cur:
        cur.execute("DELETE FROM tasks")
        for t in ("tasks_rollback", "tasks_test_archive"):
            cur.execute(
                "SELECT 1 FROM information_schema.tables "
                "WHERE table_schema='public' AND table_name=%s", (t,))
            if cur.fetchone():
                cur.execute(f"DELETE FROM {t}")
    c.commit()
    yield c
    c.close()


def add(conn, task_id, **fields):
    row = {
        "wallet_address": "GCKRS77C3OEUFPE4UEDYPPVS2I7357QNCBUMYTJDLR5PNQNBU2S26RNL",
        "escrow_tx": REAL_TX,
        "status": "pending",
        "settled": 0,
        "refunded": 0,
        "refund_tx_hash": "",
        "deadline_unix": int(time.time()) - 60,
        "created_at": time.time(),
        "refund_hold": 0,
        "refund_hold_reason": "",
    }
    row.update(fields)
    cols = ", ".join(f'"{c}"' for c in ("task_id", *row))
    ph = ", ".join(["%s"] * (len(row) + 1))
    with conn.cursor() as cur:
        cur.execute(f"INSERT INTO tasks ({cols}) VALUES ({ph})",
                    (task_id, *row.values()))
    conn.commit()


def read(conn, task_id) -> dict:
    import psycopg2.extras

    with conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor) as cur:
        cur.execute("SELECT * FROM tasks WHERE task_id = %s", (task_id,))
        return dict(cur.fetchone())


# ---------------------------------------------------------------------------
# The snapshot
# ---------------------------------------------------------------------------

@requires_pg
class TestSnapshotAndRestore:
    def test_a_change_is_reversed_exactly(self, conn):
        import dbsafe

        add(conn, "t1", status="refunded", refunded=1, refund_tx_hash="h")
        before = read(conn, "t1")

        rid = dbsafe.new_rollback_id("test")
        dbsafe.snapshot_rows(conn, ["t1"], rid, "before the change")
        with conn.cursor() as cur:
            cur.execute(
                "UPDATE tasks SET refunded = 0, status = %s, "
                "refund_tx_hash = '' WHERE task_id = 't1'",
                ("awaiting_manual_reconciliation",),
            )
        conn.commit()
        assert read(conn, "t1")["refunded"] == 0

        n = dbsafe.restore_rows(conn, rid)
        conn.commit()

        assert n == 1
        assert read(conn, "t1") == before, "the restore is not byte-exact"
        assert dbsafe.verify_restored(conn, rid) == []

    def test_every_column_round_trips(self, conn):
        """Not just the ones a test thought to check: the column list comes
        from the catalogue, so a schema that gains a column still restores
        completely."""
        import dbsafe

        add(conn, "t1", agent_handle="competitive-intel",
            result_json='{"a": 1}', refund_attempts=3,
            refund_last_error="something", refund_reason="deadline_expired",
            refund_hold=1, refund_hold_reason="investigation")
        before = read(conn, "t1")
        cols = dbsafe.task_columns(conn)

        # Every column is moved off its value: the nullable ones to NULL, the
        # NOT NULL ones to a different value of their own type. Nulling
        # everything would just hit the first NOT NULL constraint and stop.
        with conn.cursor() as cur:
            cur.execute(
                "SELECT column_name, is_nullable, data_type "
                "FROM information_schema.columns "
                "WHERE table_schema='public' AND table_name='tasks'"
            )
            meta = {r[0]: (r[1] == "YES", r[2]) for r in cur.fetchall()}

        pieces = []
        for c in cols:
            if c in ("task_id", "created_at"):
                continue
            nullable, dtype = meta[c]
            if nullable:
                pieces.append(f'"{c}" = NULL')
            elif "char" in dtype or dtype == "text":
                pieces.append(f'"{c}" = \'CLOBBERED\'')
            elif "int" in dtype or "double" in dtype or "numeric" in dtype:
                pieces.append(f'"{c}" = -1')
            else:
                pieces.append(f'"{c}" = NULL')
        assert pieces, "no columns to clobber"

        rid = dbsafe.new_rollback_id("test")
        dbsafe.snapshot_rows(conn, ["t1"], rid, "r")
        with conn.cursor() as cur:
            cur.execute(
                f"UPDATE tasks SET {', '.join(pieces)} WHERE task_id = 't1'"
            )
        conn.commit()
        assert read(conn, "t1") != before, "the clobber did nothing"

        dbsafe.restore_rows(conn, rid)
        conn.commit()

        after = read(conn, "t1")
        assert after == before
        assert len(cols) >= 18, "the schema lost columns"

    def test_restoring_does_not_touch_other_rows(self, conn):
        import dbsafe

        add(conn, "t1")
        add(conn, "t2", status="complete")
        rid = dbsafe.new_rollback_id("test")
        dbsafe.snapshot_rows(conn, ["t1"], rid, "r")

        with conn.cursor() as cur:
            cur.execute("UPDATE tasks SET status = 'changed'")
        conn.commit()

        dbsafe.restore_rows(conn, rid)
        conn.commit()

        assert read(conn, "t1")["status"] == "pending"
        assert read(conn, "t2")["status"] == "changed", (
            "restored a row that was not in the snapshot"
        )

    def test_two_snapshots_are_independent(self, conn):
        """The hold and the correction are separate operations with separate
        rollbacks: undoing one must not undo the other."""
        import dbsafe

        add(conn, "t1", status="refunded", refunded=1, refund_tx_hash="h")

        hold_id = dbsafe.new_rollback_id("hold")
        dbsafe.snapshot_rows(conn, ["t1"], hold_id, "before hold")
        with conn.cursor() as cur:
            cur.execute("UPDATE tasks SET refund_hold = 1, "
                        "refund_hold_reason = 'investigation' "
                        "WHERE task_id = 't1'")
        conn.commit()

        fix_id = dbsafe.new_rollback_id("fix")
        dbsafe.snapshot_rows(conn, ["t1"], fix_id, "before fix")
        with conn.cursor() as cur:
            cur.execute("UPDATE tasks SET refunded = 0, status = 'corrected', "
                        "refund_tx_hash = '' WHERE task_id = 't1'")
        conn.commit()

        # Undo only the correction.
        dbsafe.restore_rows(conn, fix_id)
        conn.commit()
        row = read(conn, "t1")
        assert row["refunded"] == 1 and row["refund_tx_hash"] == "h"
        assert row["refund_hold"] == 1, "undoing the fix lifted the hold"

        # Then undo only the hold.
        dbsafe.restore_rows(conn, hold_id)
        conn.commit()
        row = read(conn, "t1")
        assert row["refund_hold"] == 0
        assert row["refunded"] == 1, "undoing the hold changed the fields"


@requires_pg
class TestSnapshotRefusals:
    def test_a_partial_snapshot_is_refused(self, conn):
        """A snapshot covering fewer rows than the change is worse than none:
        it looks like a rollback is available."""
        import dbsafe

        add(conn, "t1")
        with pytest.raises(dbsafe.DbSafeError, match="snapshotted 1 rows but 2"):
            dbsafe.snapshot_rows(conn, ["t1", "missing"],
                                 dbsafe.new_rollback_id("test"), "r")
        conn.rollback()

    def test_a_reused_id_is_refused(self, conn):
        import dbsafe

        add(conn, "t1")
        rid = dbsafe.new_rollback_id("test")
        dbsafe.snapshot_rows(conn, ["t1"], rid, "r")
        conn.commit()
        with pytest.raises(dbsafe.DbSafeError, match="already exists"):
            dbsafe.snapshot_rows(conn, ["t1"], rid, "r")
        conn.rollback()

    def test_an_unknown_id_is_refused(self, conn):
        import dbsafe

        add(conn, "t1")
        dbsafe.snapshot_rows(conn, ["t1"], dbsafe.new_rollback_id("x"), "r")
        conn.commit()
        with pytest.raises(dbsafe.DbSafeError, match="no snapshot"):
            dbsafe.restore_rows(conn, "does-not-exist")

    def test_a_deleted_row_is_reported_not_silently_reinserted(self, conn):
        """A row the snapshot has but `tasks` does not was deleted after the
        snapshot. Putting it back is a different decision from a rollback."""
        import dbsafe

        add(conn, "t1")
        rid = dbsafe.new_rollback_id("test")
        dbsafe.snapshot_rows(conn, ["t1"], rid, "r")
        conn.commit()
        with conn.cursor() as cur:
            cur.execute("DELETE FROM tasks WHERE task_id = 't1'")
        conn.commit()

        with pytest.raises(dbsafe.DbSafeError, match="no longer in"):
            dbsafe.restore_rows(conn, rid)
        conn.rollback()

    def test_verify_catches_a_restore_that_did_not_take(self, conn):
        import dbsafe

        add(conn, "t1")
        rid = dbsafe.new_rollback_id("test")
        dbsafe.snapshot_rows(conn, ["t1"], rid, "r")
        with conn.cursor() as cur:
            cur.execute("UPDATE tasks SET status = 'moved' WHERE task_id = 't1'")
        conn.commit()

        assert dbsafe.verify_restored(conn, rid) == ["t1"]


# ---------------------------------------------------------------------------
# The file backup
# ---------------------------------------------------------------------------

@requires_pg
@requires_pg_dump
class TestFileBackup:
    def test_it_includes_the_schema(self, tmp_path, conn):
        """Without CREATE TABLE the dump restores into nothing. This is the
        exact defect the old `--data-only` backup had."""
        import dbsafe

        add(conn, "t1")
        path = tmp_path / "b.sql"

        size = dbsafe.file_backup(TEST_DATABASE_URL, path, PG_DOCKER)

        text = path.read_text(encoding="utf8")
        assert size > 0
        assert "CREATE TABLE" in text
        assert "COPY public.tasks" in text or "INSERT INTO public.tasks" in text
        assert "t1" in text

    def test_it_refuses_to_overwrite(self, tmp_path, conn):
        import dbsafe

        add(conn, "t1")
        path = tmp_path / "b.sql"
        dbsafe.file_backup(TEST_DATABASE_URL, path, PG_DOCKER)
        with pytest.raises(dbsafe.DbSafeError, match="already exists"):
            dbsafe.file_backup(TEST_DATABASE_URL, path, PG_DOCKER)

@requires_pg
class TestBackupFailureIsClean:
    def test_a_failure_leaves_no_file_to_block_the_retry(self, tmp_path):
        """The first version left a zero-byte file behind when pg_dump failed,
        and its own overwrite guard then blocked the retry."""
        import dbsafe

        path = tmp_path / "b.sql"
        bad = "postgresql://nobody:nobody@127.0.0.1:1/does_not_exist"
        with pytest.raises(dbsafe.DbSafeError):
            dbsafe.file_backup(bad, path, PG_DOCKER)

        assert not path.exists(), "a failed backup left its filename taken"
        assert not path.with_suffix(".sql.partial").exists()
