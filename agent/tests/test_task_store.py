import os
import tempfile
import threading
import time

import pytest

from research_agent.task_store import TaskStore


@pytest.fixture
def store(tmp_path):
    db_path = str(tmp_path / "test.db")
    s = TaskStore(db_path)
    yield s
    s.close()


@pytest.fixture
def store_path(tmp_path):
    """Path-only fixture for tests that need to open + close + reopen."""
    return str(tmp_path / "test.db")


class TestCreateAndGetTask:

    def test_create_and_get(self, store):
        store.create_task("t1", wallet_address="GABC", task_text="hello", status="pending")
        task = store.get_task("t1")
        assert task is not None
        assert task["task_id"] == "t1"
        assert task["wallet_address"] == "GABC"
        assert task["task_text"] == "hello"
        assert task["status"] == "pending"

    def test_get_nonexistent_returns_none(self, store):
        assert store.get_task("nope") is None

    def test_create_with_all_fields(self, store):
        store.create_task(
            "t2",
            wallet_address="GXYZ",
            agent_handle="agent-1",
            task_text="research AI",
            escrow_tx="tx_abc",
            deadline_unix=9999999999,
            status="running",
        )
        task = store.get_task("t2")
        assert task["agent_handle"] == "agent-1"
        assert task["escrow_tx"] == "tx_abc"
        assert task["deadline_unix"] == 9999999999

    def test_duplicate_task_id_raises(self, store):
        store.create_task("t1", wallet_address="GA")
        with pytest.raises(Exception):
            store.create_task("t1", wallet_address="GB")


class TestGetTaskWithWallet:

    def test_correct_wallet_returns_task(self, store):
        store.create_task("t1", wallet_address="GABC")
        assert store.get_task("t1", wallet_address="GABC") is not None

    def test_wrong_wallet_returns_none(self, store):
        store.create_task("t1", wallet_address="GABC")
        assert store.get_task("t1", wallet_address="GXYZ") is None

    def test_no_wallet_filter_returns_task(self, store):
        store.create_task("t1", wallet_address="GABC")
        assert store.get_task("t1") is not None


class TestUpdateTask:

    def test_update_status(self, store):
        store.create_task("t1", wallet_address="GA")
        store.update_task("t1", status="complete")
        assert store.get_task("t1")["status"] == "complete"

    def test_update_result_json(self, store):
        store.create_task("t1", wallet_address="GA")
        store.update_task("t1", result_json='{"output": "hello"}')
        assert store.get_task("t1")["result_json"] == '{"output": "hello"}'

    def test_update_settled(self, store):
        store.create_task("t1", wallet_address="GA")
        store.update_task("t1", settled=True)
        assert store.get_task("t1")["settled"] == 1

    def test_update_refunded(self, store):
        store.create_task("t1", wallet_address="GA")
        store.update_task("t1", refunded=True)
        assert store.get_task("t1")["refunded"] == 1

    def test_update_nonexistent_is_noop(self, store):
        store.update_task("nope", status="complete")


class TestListExpired:

    def test_lists_expired_unsettled(self, store):
        now = time.time()
        store.create_task("t1", wallet_address="GA", deadline_unix=int(now - 100), status="pending")
        store.create_task("t2", wallet_address="GA", deadline_unix=int(now + 1000), status="pending")

        expired = store.list_expired()
        ids = [t["task_id"] for t in expired]
        assert "t1" in ids
        assert "t2" not in ids

    def test_does_not_list_settled(self, store):
        now = time.time()
        store.create_task("t1", wallet_address="GA", deadline_unix=int(now - 100), status="pending")
        store.update_task("t1", settled=True)

        expired = store.list_expired()
        assert len(expired) == 0

    def test_does_not_list_refunded(self, store):
        now = time.time()
        store.create_task("t1", wallet_address="GA", deadline_unix=int(now - 100), status="pending")
        store.update_task("t1", refunded=True)

        expired = store.list_expired()
        assert len(expired) == 0

    def test_does_not_list_complete(self, store):
        now = time.time()
        store.create_task("t1", wallet_address="GA", deadline_unix=int(now - 100), status="complete")

        expired = store.list_expired()
        assert len(expired) == 0


class TestCleanup:

    def test_cleanup_old_tasks(self, store):
        store.create_task("t1", wallet_address="GA")
        store._conn.execute("UPDATE tasks SET created_at = ?", (time.time() - 100000,))
        store._conn.commit()

        store.create_task("t2", wallet_address="GA")

        store.cleanup(max_age_seconds=1000)

        assert store.get_task("t1") is None
        assert store.get_task("t2") is not None


class TestNonces:

    def test_fresh_nonce_returns_true(self, store):
        assert store.check_nonce("nonce1") is True

    def test_replay_nonce_returns_false(self, store):
        store.record_nonce("nonce1")
        assert store.check_nonce("nonce1") is False

    def test_different_nonces_are_independent(self, store):
        store.record_nonce("nonce1")
        assert store.check_nonce("nonce2") is True

    def test_cleanup_nonces(self, store):
        store.record_nonce("old_nonce")
        store._conn.execute("UPDATE nonces SET seen_at = ?", (time.time() - 1000,))
        store._conn.commit()

        store.record_nonce("new_nonce")
        store.cleanup_nonces(ttl=500)

        assert store.check_nonce("old_nonce") is True  # past TTL, reusable
        assert store.check_nonce("new_nonce") is False  # still within TTL


class TestNonceSurvivesReconnect:

    def test_nonce_persists_across_connections(self, store_path):
        s1 = TaskStore(store_path)
        s1.record_nonce("replay_me")
        s1.close()

        s2 = TaskStore(store_path)
        assert s2.check_nonce("replay_me") is False
        s2.close()


class TestTaskSurvivesReconnect:

    def test_task_persists_across_connections(self, store_path):
        s1 = TaskStore(store_path)
        s1.create_task("t1", wallet_address="GA", task_text="survive")
        s1.close()

        s2 = TaskStore(store_path)
        task = s2.get_task("t1")
        assert task is not None
        assert task["task_text"] == "survive"
        s2.close()


class TestConcurrentWrites:

    def test_threaded_writes_dont_corrupt(self, store):
        errors = []

        def writer(task_id):
            try:
                store.create_task(task_id, wallet_address="GA", task_text=f"task-{task_id}")
            except Exception as e:
                errors.append(e)

        threads = [threading.Thread(target=writer, args=(f"t{i}",)) for i in range(20)]
        for t in threads:
            t.start()
        for t in threads:
            t.join()

        assert len(errors) == 0
        for i in range(20):
            assert store.get_task(f"t{i}") is not None
