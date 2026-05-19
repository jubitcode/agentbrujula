"""_load_task must not fall through to allow authenticated callers to read empty-wallet tasks.

The unauth path (wallet_address=None) still serves empty-wallet tasks
— that's dev mode's intended behavior, and these tests guard both
sides of that contract."""

import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))


@pytest.fixture
def store(tmp_path):
    from research_agent.task_store import TaskStore
    return TaskStore(db_path=str(tmp_path / "test.db"))


@pytest.fixture
def patched_server(monkeypatch, store):
    """Swap server.py's _store for our isolated test store."""
    import server as srv
    monkeypatch.setattr(srv, "_store", store)
    return srv


class TestEmptyWalletNoLongerBypassable:
    def test_authed_caller_cannot_access_empty_wallet_task(
        self, patched_server, store,
    ):
        """Unbound tasks (wallet_address='') must not be accessible to any authenticated wallet."""
        store.create_task(
            task_id="task-empty",
            wallet_address="",  # unbound
            agent_handle="research",
            task_text="x",
            escrow_tx="",
            deadline_unix=0,
        )
        result = patched_server._load_task(
            "task-empty", wallet_address="GATTACKER000",
        )
        assert result is None, (
            "Empty-wallet task must NOT be accessible to any authenticated caller. "
            "Pre-fix this returned the row."
        )

    def test_unauth_caller_can_still_access_empty_wallet_task(
        self, patched_server, store,
    ):
        """Dev-mode compat: unauth path (wallet_address=None) still serves empty-bound tasks."""
        store.create_task(
            task_id="task-empty-2",
            wallet_address="",
            agent_handle="research",
            task_text="x",
            escrow_tx="",
            deadline_unix=0,
        )
        result = patched_server._load_task("task-empty-2", wallet_address=None)
        assert result is not None
        assert result["task_id"] == "task-empty-2"


class TestBoundTaskExactMatch:
    def test_bound_task_returned_for_owner(self, patched_server, store):
        store.create_task(
            task_id="task-bound",
            wallet_address="GOWNER000",
            agent_handle="research",
            task_text="x",
            escrow_tx="",
            deadline_unix=0,
        )
        result = patched_server._load_task("task-bound", wallet_address="GOWNER000")
        assert result is not None

    def test_bound_task_rejected_for_different_wallet(self, patched_server, store):
        store.create_task(
            task_id="task-bound-2",
            wallet_address="GOWNER000",
            agent_handle="research",
            task_text="x",
            escrow_tx="",
            deadline_unix=0,
        )
        result = patched_server._load_task(
            "task-bound-2", wallet_address="GATTACKER000",
        )
        assert result is None


class TestMissing:
    def test_nonexistent_task_returns_none(self, patched_server):
        assert patched_server._load_task("ghost") is None
        assert patched_server._load_task("ghost", wallet_address="G") is None
