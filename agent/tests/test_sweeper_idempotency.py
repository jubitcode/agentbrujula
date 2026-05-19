"""sweep_expired_refunds OUTER cycle:
  - Attempt marker written BEFORE refund call → crash mid-refund doesn't cause double-refund
  - list_expired() filters out recently-attempted tasks (retry-interval back-off)
  - per-sweep rate cap

Per-task on-chain correctness lives in test_refund_sweeper.py against process_one_refund."""

from __future__ import annotations

import tempfile
import time

import pytest

from research_agent.task_store import TaskStore
from research_agent.refund_sweeper import sweep_expired_refunds


def _make_store() -> TaskStore:
    f = tempfile.NamedTemporaryFile(delete=False, suffix=".db")
    f.close()
    return TaskStore(db_path=f.name)


def _create_expired_task(store: TaskStore, task_id: str, seconds_ago: int = 60) -> None:
    store.create_task(
        task_id=task_id,
        wallet_address="GTEST",
        deadline_unix=int(time.time()) - seconds_ago,
        status="pending",
    )


_FAKE_KP = object()  # outer cycle just passes this through to process_one_refund


def _ok_status_fn(task_id: str) -> dict:
    """Default: escrow still on-chain → not reconciled."""
    return {"task_id": task_id, "status": "found"}


def _make_refund_fn(calls: list, *, success: bool = True, raise_exc: Exception | None = None):
    def refund_fn(orchestrator_kp, task_id: str) -> dict:
        calls.append(task_id)
        if raise_exc is not None:
            raise raise_exc
        if success:
            return {"hash": f"hash-{task_id}", "status": "SUCCESS", "error": None}
        return {"hash": "", "status": "FAILED", "error": "synthetic test failure"}

    return refund_fn


def test_sweeper_marks_attempt_before_calling_refund():
    """Attempt marker must be written BEFORE the refund call returns — a crash between the two cannot allow retry."""
    store = _make_store()
    _create_expired_task(store, "task-1")

    seen_attempt_at: list[float | None] = []

    def refund_fn(orchestrator_kp, task_id: str) -> dict:
        # Marker must already exist by the time refund_fn runs
        row = store.get_task(task_id)
        seen_attempt_at.append(row.get("refund_attempted_at"))
        return {"hash": "h", "status": "SUCCESS", "error": None}

    result = sweep_expired_refunds(
        store, _FAKE_KP, refund_fn=refund_fn, status_fn=_ok_status_fn,
    )

    assert seen_attempt_at == [pytest.approx(time.time(), abs=2)]
    assert result["refunded"] == 1
    assert result["failed"] == 0


def test_double_invocation_does_not_double_refund():
    store = _make_store()
    _create_expired_task(store, "task-2")

    calls: list[str] = []
    refund_fn = _make_refund_fn(calls, success=True)

    sweep_expired_refunds(store, _FAKE_KP, refund_fn=refund_fn, status_fn=_ok_status_fn)
    sweep_expired_refunds(store, _FAKE_KP, refund_fn=refund_fn, status_fn=_ok_status_fn)

    # First sweep marks status="refunded" → list_expired excludes it; retry-interval would also block
    assert calls == ["task-2"]


def test_crash_between_refund_and_db_update_blocks_retry():
    """refund_fn raises mid-call; the attempt marker must still block the next loop within the retry window."""
    store = _make_store()
    _create_expired_task(store, "task-3")

    calls: list[str] = []
    crashing = _make_refund_fn(calls, raise_exc=RuntimeError("crashed mid-refund"))

    sweep_expired_refunds(store, _FAKE_KP, refund_fn=crashing, status_fn=_ok_status_fn)

    succeeding = _make_refund_fn(calls, success=True)
    sweep_expired_refunds(
        store,
        _FAKE_KP,
        refund_fn=succeeding,
        status_fn=_ok_status_fn,
        retry_interval_seconds=600,  # much longer than test runtime
    )

    assert calls == ["task-3"], (
        f"Expected exactly one refund call (the first), got: {calls}"
    )


def test_retry_after_interval_elapsed():
    store = _make_store()
    _create_expired_task(store, "task-4")

    calls: list[str] = []
    failing = _make_refund_fn(calls, success=False)

    sweep_expired_refunds(
        store, _FAKE_KP,
        refund_fn=failing, status_fn=_ok_status_fn,
        retry_interval_seconds=0,
    )
    sweep_expired_refunds(
        store, _FAKE_KP,
        refund_fn=failing, status_fn=_ok_status_fn,
        retry_interval_seconds=0,
    )

    assert calls == ["task-4", "task-4"]


def test_per_sweep_rate_limit_caps_refunds():
    """20 expired tasks but max_per_sweep=5 → only 5 attempted in one cycle."""
    store = _make_store()
    for i in range(20):
        _create_expired_task(store, f"task-batch-{i}")

    calls: list[str] = []
    refund_fn = _make_refund_fn(calls, success=True)

    result = sweep_expired_refunds(
        store, _FAKE_KP,
        refund_fn=refund_fn, status_fn=_ok_status_fn,
        max_per_sweep=5,
    )

    assert len(calls) == 5
    assert result["refunded"] == 5
    assert result["skipped"] == 15
