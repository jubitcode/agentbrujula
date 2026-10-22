import os
import sys
import tempfile

import pytest

from research_agent.task_store import TaskStore


@pytest.fixture
def store_path(tmp_path):
    return str(tmp_path / "multiuser.db")


class TestUserIsolation:

    def test_user_a_cannot_read_user_b_task(self, store_path):
        store = TaskStore(store_path)
        store.create_task("t1", wallet_address="GALICE", task_text="alice task")
        store.create_task("t2", wallet_address="GBOB", task_text="bob task")

        assert store.get_task("t1", wallet_address="GALICE") is not None
        assert store.get_task("t2", wallet_address="GALICE") is None
        assert store.get_task("t2", wallet_address="GBOB") is not None
        assert store.get_task("t1", wallet_address="GBOB") is None
        store.close()

    def test_update_does_not_leak_across_users(self, store_path):
        store = TaskStore(store_path)
        store.create_task("t1", wallet_address="GALICE", status="pending")
        store.update_task("t1", status="complete")

        assert store.get_task("t1", wallet_address="GALICE")["status"] == "complete"
        assert store.get_task("t1", wallet_address="GBOB") is None
        store.close()


class TestPersistenceAcrossRestarts:

    def test_task_survives_restart(self, store_path):
        s1 = TaskStore(store_path)
        s1.create_task("t1", wallet_address="GA", task_text="persistent", status="running")
        s1.update_task("t1", result_json='{"output": "hello world"}')
        s1.close()

        s2 = TaskStore(store_path)
        task = s2.get_task("t1")
        assert task is not None
        assert task["task_text"] == "persistent"
        assert task["status"] == "running"
        assert task["result_json"] == '{"output": "hello world"}'
        s2.close()

    def test_settled_flag_persists(self, store_path):
        s1 = TaskStore(store_path)
        s1.create_task("t1", wallet_address="GA")
        s1.update_task("t1", settled=True)
        s1.close()

        s2 = TaskStore(store_path)
        assert s2.get_task("t1")["settled"] == 1
        s2.close()


class TestNonceReplayAfterRestart:

    def test_replay_rejected_after_restart(self, store_path):
        s1 = TaskStore(store_path)
        assert s1.check_nonce("tx-hash-abc") is True
        s1.record_nonce("tx-hash-abc")
        s1.close()

        s2 = TaskStore(store_path)  # simulate restart
        assert s2.check_nonce("tx-hash-abc") is False
        s2.close()

    def test_fresh_nonce_works_after_restart(self, store_path):
        s1 = TaskStore(store_path)
        s1.record_nonce("old-nonce")
        s1.close()

        s2 = TaskStore(store_path)
        assert s2.check_nonce("new-nonce") is True
        s2.close()


class TestExpiredTaskCleanup:

    def test_expired_tasks_listed(self, store_path):
        import time
        store = TaskStore(store_path)
        now = time.time()
        store.create_task("expired", wallet_address="GA", deadline_unix=int(now - 100), status="pending")
        store.create_task("active", wallet_address="GA", deadline_unix=int(now + 1000), status="pending")
        store.create_task("no_deadline", wallet_address="GA", status="pending")

        expired = store.list_expired()
        ids = [t["task_id"] for t in expired]
        assert "expired" in ids
        assert "active" not in ids
        assert "no_deadline" not in ids
        store.close()

    def test_cleanup_removes_old_tasks(self, store_path):
        import time
        store = TaskStore(store_path)
        store.create_task("old", wallet_address="GA")
        store._conn.execute("UPDATE tasks SET created_at = ?", (time.time() - 200000,))
        store._conn.commit()
        store.create_task("new", wallet_address="GA")

        removed = store.cleanup(max_age_seconds=1000)
        assert removed["deleted"] == 1
        assert store.get_task("old") is None
        assert store.get_task("new") is not None
        store.close()
