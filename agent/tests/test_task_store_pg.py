import os
import sys
import time
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))

from research_agent.task_store import TaskStore


class TestTaskStoreSQLite:
    @pytest.fixture
    def store(self, tmp_path):
        s = TaskStore(db_path=str(tmp_path / "test.db"))
        yield s
        s.close()

    def test_create_and_get_task(self, store):
        store.create_task(task_id="t1", wallet_address="GTEST", task_text="hello")
        task = store.get_task("t1")
        assert task is not None
        assert task["task_id"] == "t1"
        assert task["wallet_address"] == "GTEST"
        assert task["status"] == "pending"

    def test_get_task_with_wallet_filter(self, store):
        store.create_task(task_id="t2", wallet_address="GOWNER")
        assert store.get_task("t2", wallet_address="GOWNER") is not None
        assert store.get_task("t2", wallet_address="GOTHER") is None

    def test_update_task(self, store):
        store.create_task(task_id="t3")
        store.update_task("t3", status="complete", result_json='{"output":"done"}')
        task = store.get_task("t3")
        assert task["status"] == "complete"
        assert task["result_json"] == '{"output":"done"}'

    def test_nonce_check_and_record(self, store):
        assert store.check_nonce("n1") is True
        store.record_nonce("n1")
        assert store.check_nonce("n1") is False

    def test_nonce_idempotent_record(self, store):
        store.record_nonce("n2")
        store.record_nonce("n2")  # double-record must not raise
        assert store.check_nonce("n2") is False

    def test_list_expired(self, store):
        store.create_task(task_id="active", deadline_unix=int(time.time()) + 9999)
        store.create_task(task_id="expired", deadline_unix=int(time.time()) - 100)
        expired = store.list_expired()
        ids = [t["task_id"] for t in expired]
        assert "expired" in ids
        assert "active" not in ids

    def test_an_explicit_db_path_is_never_overridden_by_the_environment(
        self, tmp_path, monkeypatch
    ):
        """Asking for a file must give a file, whatever DATABASE_URL says.

        It used to give PostgreSQL: the environment was consulted first and
        `db_path` was ignored. Every test in this class passes `db_path`, so
        with DATABASE_URL set they were all running against the shared
        database while reading as if they were isolated — which is how a test
        fixture came to write `refund_tx_hash='h'` onto a row holding a live
        escrow.
        """
        monkeypatch.setenv(
            "DATABASE_URL", "postgresql://nobody@127.0.0.1:1/would_be_wrong"
        )
        path = tmp_path / "explicit.db"

        s = TaskStore(db_path=str(path))
        try:
            assert s._use_pg is False, "db_path was ignored for the environment"
            assert path.exists(), "no SQLite file was created"
            s.create_task(task_id="iso", wallet_address="GLOCAL")
            assert s.get_task("iso")["wallet_address"] == "GLOCAL"
        finally:
            s.close()

    def test_cleanup(self, store):
        store.create_task(task_id="old")
        store._execute(
            "UPDATE tasks SET created_at = ? WHERE task_id = ?",
            (time.time() - 200000, "old"),
        )
        store._commit()
        deleted = store.cleanup(max_age_seconds=100000)
        assert deleted["deleted"] == 1
        assert store.get_task("old") is None


# Never the application's own database. These tests create, mutate and delete
# rows; pointing them at `marketplace` is how a test run ends up writing over
# real records — which is exactly what happened on 2026-10-20, when fixture
# values like refund_tx_hash="h" were written onto a row holding a live escrow.
#
# Set TEST_DATABASE_URL to a throwaway database to run them. Create one with:
#   docker compose exec postgres psql -U agent_web3 -d postgres \
#       -c 'CREATE DATABASE brujula_test'
#: A real Stellar transaction hash: 64 hex characters. `_FINANCIALLY_OPEN_SQL`
#: and `refund_authorisation` both require that shape.
_REAL_TX = "e6d6a1a06f1fd7b57d57aa8f1c96a63dae167f256ee2ecba343f2f20b08a7966"

TEST_DATABASE_URL = os.environ.get("TEST_DATABASE_URL", "")


def _pg_available() -> bool:
    if not TEST_DATABASE_URL:
        return False
    if TEST_DATABASE_URL.rstrip("/").endswith("/marketplace"):
        raise RuntimeError(
            "TEST_DATABASE_URL points at the application database 'marketplace'. "
            "These tests write and delete rows. Use a throwaway database."
        )
    try:
        import psycopg2

        conn = psycopg2.connect(TEST_DATABASE_URL)
        conn.close()
        return True
    except Exception:
        return False


PG_AVAILABLE = _pg_available()
requires_pg = pytest.mark.skipif(
    not PG_AVAILABLE,
    reason="set TEST_DATABASE_URL to a throwaway PostgreSQL database to run these",
)


@requires_pg
class TestTaskStorePostgreSQL:
    @pytest.fixture
    def store(self):
        # Truncates both tables, so the URL must never be the application's.
        # `_pg_available` refuses a TEST_DATABASE_URL ending in /marketplace,
        # and the tests skip unless one is set — the two together are what keep
        # `DELETE FROM tasks` away from live escrow records.
        s = TaskStore(database_url=TEST_DATABASE_URL)
        s._execute("DELETE FROM tasks")
        s._execute("DELETE FROM nonces")
        s._commit()
        yield s
        s.close()

    def test_create_and_get_task(self, store):
        store.create_task(task_id="pg-t1", wallet_address="GTEST", task_text="hello pg")
        task = store.get_task("pg-t1")
        assert task is not None
        assert task["task_id"] == "pg-t1"
        assert task["wallet_address"] == "GTEST"

    def test_wallet_filter(self, store):
        store.create_task(task_id="pg-t2", wallet_address="GOWNER")
        assert store.get_task("pg-t2", wallet_address="GOWNER") is not None
        assert store.get_task("pg-t2", wallet_address="GOTHER") is None

    def test_update_task(self, store):
        store.create_task(task_id="pg-t3")
        store.update_task("pg-t3", status="complete")
        task = store.get_task("pg-t3")
        assert task["status"] == "complete"

    def test_nonce_replay_protection(self, store):
        assert store.check_nonce("pg-n1") is True
        store.record_nonce("pg-n1")
        assert store.check_nonce("pg-n1") is False

    def test_list_expired(self, store):
        store.create_task(task_id="pg-active", deadline_unix=int(time.time()) + 9999)
        store.create_task(task_id="pg-expired", deadline_unix=int(time.time()) - 100)
        expired = store.list_expired()
        ids = [t["task_id"] for t in expired]
        assert "pg-expired" in ids
        assert "pg-active" not in ids

    def test_refund_hold_survives_in_postgres(self, store):
        """The hold is new SQL on a different engine: `COALESCE`, the boolean
        comparison and `length()` all have to behave the same here as they do
        in SQLite, or the protection exists only in tests."""
        store.create_task(task_id="pg-hold", wallet_address="GOWNER",
                          deadline_unix=int(time.time()) - 100)
        store.update_task("pg-hold", escrow_tx=_REAL_TX)
        assert "pg-hold" in [t["task_id"] for t in store.list_expired()]

        assert store.hold_refund("pg-hold", "investigation open") is True

        assert store.is_refund_held("pg-hold") is True
        assert "pg-hold" not in [t["task_id"] for t in store.list_expired()]
        assert [h["task_id"] for h in store.list_refund_holds()] == ["pg-hold"]

        assert store.release_refund_hold("pg-hold", "approved") is True
        assert "pg-hold" in [t["task_id"] for t in store.list_expired()]

    def test_a_held_row_is_preserved_by_cleanup_in_postgres(self, store):
        # The real row's shape: contaminated with refunded = 1, so only the
        # hold protects it. This is the case `_PRESERVE_SQL` has to get right
        # on both engines.
        store.create_task(task_id="pg-held-old", wallet_address="GOWNER")
        store.update_task("pg-held-old", escrow_tx=_REAL_TX, refunded=1,
                          status="refunded")
        # `_execute` rewrites ? to %s for PostgreSQL.
        store._execute("UPDATE tasks SET created_at = ? WHERE task_id = ?",
                       (time.time() - 5 * 86400, "pg-held-old"))
        store._commit()
        store.hold_refund("pg-held-old", "live escrow, false refunded flag")

        result = store.cleanup(max_age_seconds=86400)

        assert store.get_task("pg-held-old") is not None
        assert result["deleted"] == 0
        assert result["preserved_open"] == 1

    def test_a_null_hold_does_not_hide_a_row_in_postgres(self, store):
        """The migration path: `refund_hold` added to a table that already has
        rows. A NULL there must read as "not held" — with a plain
        `refund_hold = 0` it would read as neither, and the row would vanish
        from the expired queue and from cleanup's delete at once."""
        store.create_task(task_id="pg-null-hold", wallet_address="GOWNER",
                          deadline_unix=int(time.time()) - 100)
        store.update_task("pg-null-hold", escrow_tx=_REAL_TX)
        store._execute("UPDATE tasks SET refund_hold = NULL WHERE task_id = ?",
                       ("pg-null-hold",))
        store._commit()

        assert store.get_task("pg-null-hold")["refund_hold"] is None
        assert "pg-null-hold" in [t["task_id"] for t in store.list_expired()]
        assert store.is_refund_held("pg-null-hold") is False
