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

    def test_cleanup(self, store):
        store.create_task(task_id="old")
        store._execute(
            "UPDATE tasks SET created_at = ? WHERE task_id = ?",
            (time.time() - 200000, "old"),
        )
        store._commit()
        deleted = store.cleanup(max_age_seconds=100000)
        assert deleted == 1
        assert store.get_task("old") is None


def _pg_available() -> bool:
    try:
        import psycopg2
        conn = psycopg2.connect("postgresql://agent_web3:dev-password@localhost:5432/marketplace")
        conn.close()
        return True
    except Exception:
        return False


PG_AVAILABLE = _pg_available()
requires_pg = pytest.mark.skipif(not PG_AVAILABLE, reason="PostgreSQL not available at localhost:5432")


@requires_pg
class TestTaskStorePostgreSQL:
    @pytest.fixture
    def store(self):
        url = "postgresql://agent_web3:dev-password@localhost:5432/marketplace"
        s = TaskStore(database_url=url)
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
