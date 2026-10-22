"""Escrow refund sweeper contract:
  - DB only updates when on-chain refund is confirmed SUCCESS (not just submitted)
  - Failures bump retry counter + surface error
  - Bounded retries → dead-letter state
  - Idempotent if on-chain escrow already gone
  - Already-handled tasks skipped
  - Same guarantee applies to settle path

process_one_refund is pure — network calls are injected via refund_fn/status_fn."""

import os
import sqlite3
import tempfile
import time
import pytest

from research_agent.task_store import TaskStore


#: A real Stellar transaction hash: 64 hex characters.
REAL_TX = "e6d6a1a06f1fd7b57d57aa8f1c96a63dae167f256ee2ecba343f2f20b08a7966"


class FakeKeypair:
    """Stand-in for a stellar Keypair — only forwarded to refund_fn which ignores it."""
    public_key = "GORCH00000000000000000000000000000000000000000000000000000"


def _make_expired_task(store: TaskStore, task_id: str = "task-001") -> dict:
    """Task the sweeper picks up: expired deadline, unsettled, unrefunded, non-terminal."""
    # A real 64-character transaction hash. `refund_authorisation` refuses a
    # task whose escrow_tx is not one, because a row carrying "abc" never
    # locked funds — so a fixture using a placeholder would be testing the
    # refund path against a task that can never reach it in production.
    store.create_task(
        task_id=task_id,
        wallet_address="GUSER000000000000000000000000000000000000000000000000000000",
        agent_handle="research-agent",
        task_text="anything",
        escrow_tx=REAL_TX,
        deadline_unix=int(time.time()) - 60,
        status="pending",
    )
    return store.get_task(task_id)


@pytest.fixture
def store(tmp_path):
    db = tmp_path / "marketplace.db"
    s = TaskStore(db_path=str(db))
    yield s
    s.close()


@pytest.fixture
def process_one_refund():
    from research_agent.refund_sweeper import process_one_refund
    return process_one_refund


def test_old_db_gets_new_columns_on_init(tmp_path):
    """Old-schema DB must auto-acquire refund_attempts / refund_tx_hash / refund_last_error."""
    db_path = tmp_path / "old.db"
    conn = sqlite3.connect(str(db_path))
    conn.executescript(
        """
        CREATE TABLE tasks (
            task_id TEXT PRIMARY KEY,
            wallet_address TEXT NOT NULL DEFAULT '',
            agent_handle TEXT DEFAULT '',
            task_text TEXT DEFAULT '',
            escrow_tx TEXT DEFAULT '',
            deadline_unix INTEGER DEFAULT 0,
            status TEXT DEFAULT 'pending',
            result_json TEXT DEFAULT '',
            created_at REAL NOT NULL,
            settled INTEGER DEFAULT 0,
            refunded INTEGER DEFAULT 0
        );
        CREATE TABLE nonces (nonce TEXT PRIMARY KEY, seen_at REAL NOT NULL);
        """
    )
    conn.commit()
    conn.close()

    store = TaskStore(db_path=str(db_path))  # migration runs on open
    try:
        conn = sqlite3.connect(str(db_path))
        cols = {row[1] for row in conn.execute("PRAGMA table_info(tasks)")}
        conn.close()
        assert "refund_attempts" in cols
        assert "refund_tx_hash" in cols
        assert "refund_last_error" in cols
    finally:
        store.close()


def test_marks_refunded_only_on_chain_success(store, process_one_refund):
    task = _make_expired_task(store)
    refund_fn = lambda kp, tid: {"hash": "deadbeef", "status": "SUCCESS", "error": None}
    status_fn = lambda tid: {"status": "found"}

    process_one_refund(FakeKeypair(), task, store, refund_fn=refund_fn, status_fn=status_fn)

    fresh = store.get_task(task["task_id"])
    assert fresh["refunded"] == 1
    assert fresh["status"] == "refunded"


def test_records_tx_hash_on_success(store, process_one_refund):
    task = _make_expired_task(store)
    refund_fn = lambda kp, tid: {"hash": "abc123hashvalue", "status": "SUCCESS", "error": None}
    status_fn = lambda tid: {"status": "found"}

    process_one_refund(FakeKeypair(), task, store, refund_fn=refund_fn, status_fn=status_fn)

    fresh = store.get_task(task["task_id"])
    assert fresh["refund_tx_hash"] == "abc123hashvalue"


def test_does_not_mark_refunded_on_chain_failure(store, process_one_refund):
    """Tx submitted but contract reverted on-chain — DB must NOT report a refund that didn't happen."""
    task = _make_expired_task(store)
    refund_fn = lambda kp, tid: {
        "hash": "f00ba4",
        "status": "FAILED",
        "error": "HostError: Contract #4 (deadline not yet expired)",
    }
    status_fn = lambda tid: {"status": "found"}

    process_one_refund(FakeKeypair(), task, store, refund_fn=refund_fn, status_fn=status_fn)

    fresh = store.get_task(task["task_id"])
    assert fresh["refunded"] == 0
    assert fresh["status"] != "refunded"
    assert fresh["refund_attempts"] == 1


def test_records_last_error_on_failure(store, process_one_refund):
    task = _make_expired_task(store)
    refund_fn = lambda kp, tid: {"hash": "x", "status": "FAILED", "error": "specific reason"}
    status_fn = lambda tid: {"status": "found"}

    process_one_refund(FakeKeypair(), task, store, refund_fn=refund_fn, status_fn=status_fn)

    fresh = store.get_task(task["task_id"])
    assert "specific reason" in (fresh["refund_last_error"] or "")


def test_handles_network_exception_without_marking(store, process_one_refund):
    """Transient network error: log + increment attempts; do NOT mark refunded."""
    task = _make_expired_task(store)

    def boom(kp, tid):
        raise RuntimeError("connection reset")

    process_one_refund(FakeKeypair(), task, store,
                       refund_fn=boom, status_fn=lambda t: {"status": "found"})

    fresh = store.get_task(task["task_id"])
    assert fresh["refunded"] == 0
    assert fresh["refund_attempts"] == 1
    assert "connection reset" in (fresh["refund_last_error"] or "")


def test_marks_dead_letter_after_max_attempts(store, process_one_refund):
    task = _make_expired_task(store)
    refund_fn = lambda kp, tid: {"hash": "x", "status": "FAILED", "error": "still failing"}
    status_fn = lambda tid: {"status": "found"}

    # max_attempts = 5; refresh between calls so attempt count carries through
    for _ in range(5):
        current = store.get_task(task["task_id"])
        process_one_refund(FakeKeypair(), current, store,
                           refund_fn=refund_fn, status_fn=status_fn)

    fresh = store.get_task(task["task_id"])
    assert fresh["refund_attempts"] == 5
    assert fresh["status"] == "refund_failed"  # dead-letter, no further retry


def test_dead_letter_task_not_returned_by_list_expired(store, process_one_refund):
    task = _make_expired_task(store)
    store.update_task(task["task_id"], status="refund_failed", refund_attempts=5)

    expired = store.list_expired()
    assert all(t["task_id"] != task["task_id"] for t in expired)


def test_unreadable_escrow_stops_retrying_without_calling_refund(store, process_one_refund):
    """An entry the contract cannot read is terminal — but refund is not retried.

    Calling `refund_to_client` on an entry `get_escrow` traps on cannot
    succeed, so there is nothing to gain from trying.
    """
    task = _make_expired_task(store)

    refund_calls = {"n": 0}

    def refund_fn(kp, tid):
        refund_calls["n"] += 1
        return {"hash": "should-not-happen", "status": "SUCCESS", "error": None}

    status_fn = lambda tid: {"status": "not_found"}

    process_one_refund(FakeKeypair(), task, store, refund_fn=refund_fn, status_fn=status_fn)

    fresh = store.get_task(task["task_id"])
    assert fresh["status"] == "escrow_entry_unreadable"
    assert refund_calls["n"] == 0


def test_unreadable_escrow_is_never_recorded_as_refunded(store, process_one_refund):
    """The money claim this code must not make.

    `get_escrow` traps identically whether the entry was resolved and removed
    or **evicted because its TTL expired** — and an evicted entry can still
    have funds in the contract. Measured on 2026-10-20: escrows created in May
    read as `not_found` while `usdc-escrow` still held 0.745 USDC.

    Marking those `refunded=1` tells a client their money came back when
    nobody can show that it did. The row has to say "we cannot account for
    this", which is true, instead of "refunded", which is not.
    """
    task = _make_expired_task(store)
    status_fn = lambda tid: {"status": "not_found"}
    refund_fn = lambda kp, tid: {"hash": "h", "status": "SUCCESS", "error": None}

    process_one_refund(FakeKeypair(), task, store, refund_fn=refund_fn, status_fn=status_fn)

    fresh = store.get_task(task["task_id"])
    assert fresh["refunded"] == 0, "claimed a refund that cannot be demonstrated"
    assert fresh["status"] != "refunded"
    # And it must leave a trail a person can act on.
    assert "manual reconciliation" in fresh["refund_last_error"]


def test_unreadable_is_counted_apart_from_refunded(store, monkeypatch):
    """A sweep must not fold unaccounted escrows into its refund total."""
    from research_agent import refund_sweeper as rs

    task = _make_expired_task(store)
    result = rs.sweep_expired_refunds(
        store,
        FakeKeypair(),
        refund_fn=lambda kp, tid: {"hash": "h", "status": "SUCCESS", "error": None},
        status_fn=lambda tid: {"status": "not_found"},
    )

    assert result["unreadable"] == 1
    assert result["refunded"] == 0
    assert result["failed"] == 0
    assert store.get_task(task["task_id"])["refunded"] == 0


def test_list_expired_skips_already_refunded(store):
    task = _make_expired_task(store)
    store.update_task(task["task_id"], refunded=1, status="refunded")
    assert all(t["task_id"] != task["task_id"] for t in store.list_expired())


def test_list_expired_skips_complete_tasks(store):
    task = _make_expired_task(store)
    store.update_task(task["task_id"], status="complete")
    assert all(t["task_id"] != task["task_id"] for t in store.list_expired())


def test_list_expired_skips_settled_tasks(store):
    task = _make_expired_task(store)
    store.update_task(task["task_id"], settled=1, status="settled")
    assert all(t["task_id"] != task["task_id"] for t in store.list_expired())


def test_processes_all_when_one_fails(store, process_one_refund):
    """Middle task fails → first and last must still be handled correctly."""
    a = _make_expired_task(store, "task-a")
    b = _make_expired_task(store, "task-b")
    c = _make_expired_task(store, "task-c")

    def refund_fn(kp, tid):
        if tid == "task-b":
            return {"hash": "x", "status": "FAILED", "error": "b unlucky"}
        return {"hash": tid + "-hash", "status": "SUCCESS", "error": None}

    status_fn = lambda tid: {"status": "found"}

    for t in [a, b, c]:
        process_one_refund(FakeKeypair(), t, store, refund_fn=refund_fn, status_fn=status_fn)

    fa = store.get_task("task-a")
    fb = store.get_task("task-b")
    fc = store.get_task("task-c")

    assert fa["refunded"] == 1 and fa["status"] == "refunded"
    assert fb["refunded"] == 0 and fb["status"] != "refunded"
    assert fc["refunded"] == 1 and fc["status"] == "refunded"


def test_escrow_helpers_return_status_dict():
    """refund_escrow and settle_escrow must return {hash, status, error} dicts so callers can distinguish
    submitted-vs-confirmed; a bare hash string is the bug being fixed."""
    from research_agent import escrow as e
    import inspect

    refund_src = inspect.getsource(e.refund_escrow)
    settle_src = inspect.getsource(e.settle_escrow)
    assert "status" in refund_src.lower(), "refund_escrow does not return status"
    assert "status" in settle_src.lower(), "settle_escrow does not return status"


def test_wait_for_tx_confirmation_exists():
    from research_agent.escrow import _wait_for_tx_confirmation
    assert callable(_wait_for_tx_confirmation)


def test_refund_reason_recorded_on_success(store, process_one_refund):
    """Default reason is deadline_expired — distinguishes sweeper refunds from manual/reconciled ones."""
    task = _make_expired_task(store)
    refund_fn = lambda kp, tid: {"hash": "h", "status": "SUCCESS", "error": None}
    status_fn = lambda tid: {"status": "found"}

    process_one_refund(FakeKeypair(), task, store, refund_fn=refund_fn, status_fn=status_fn)

    fresh = store.get_task(task["task_id"])
    assert fresh["refund_reason"] == "deadline_expired"


def test_refund_reason_records_reconciled_when_chain_gone(store, process_one_refund):
    """Escrow already gone on-chain → reason must be `reconciled_chain_gone`, not the misleading `deadline_expired`."""
    task = _make_expired_task(store)
    refund_fn = lambda kp, tid: {"hash": "x", "status": "SUCCESS", "error": None}
    status_fn = lambda tid: {"status": "not_found"}

    process_one_refund(FakeKeypair(), task, store, refund_fn=refund_fn, status_fn=status_fn)

    fresh = store.get_task(task["task_id"])
    assert fresh["refund_reason"] == "reconciled_chain_gone"


def test_refund_reason_can_be_overridden_by_caller(store, process_one_refund):
    """Manual refunds (admin tool, ops script) must be able to label themselves to disambiguate from sweeper refunds."""
    task = _make_expired_task(store)
    refund_fn = lambda kp, tid: {"hash": "h", "status": "SUCCESS", "error": None}
    status_fn = lambda tid: {"status": "found"}

    process_one_refund(
        FakeKeypair(), task, store,
        refund_fn=refund_fn, status_fn=status_fn,
        reason="manual_ops_intervention",
    )

    fresh = store.get_task(task["task_id"])
    assert fresh["refund_reason"] == "manual_ops_intervention"


def test_report_dict_includes_reason(store, process_one_refund):
    """Return dict must include reason so the sweeper can log it without re-reading the DB."""
    task = _make_expired_task(store)
    refund_fn = lambda kp, tid: {"hash": "h", "status": "SUCCESS", "error": None}
    status_fn = lambda tid: {"status": "found"}

    report = process_one_refund(FakeKeypair(), task, store, refund_fn=refund_fn, status_fn=status_fn)
    assert report["reason"] == "deadline_expired"


def test_timeout_status_does_not_mark_refunded(store, process_one_refund):
    """TIMEOUT (no terminal status in poll window) = failure: increment attempts, do NOT mark refunded."""
    task = _make_expired_task(store)
    refund_fn = lambda kp, tid: {"hash": "h", "status": "TIMEOUT", "error": "no terminal status in 30s"}
    status_fn = lambda tid: {"status": "found"}

    process_one_refund(FakeKeypair(), task, store, refund_fn=refund_fn, status_fn=status_fn)

    fresh = store.get_task(task["task_id"])
    assert fresh["refunded"] == 0
    assert fresh["status"] != "refunded"
    assert fresh["refund_attempts"] == 1
    assert "TIMEOUT" in (fresh["refund_last_error"] or "")


def test_submitted_status_does_not_mark_refunded(store, process_one_refund):
    """SUBMITTED (not confirmed) must not count as success — guards against the original bug regressing."""
    task = _make_expired_task(store)
    refund_fn = lambda kp, tid: {"hash": "h", "status": "SUBMITTED", "error": None}
    status_fn = lambda tid: {"status": "found"}

    process_one_refund(FakeKeypair(), task, store, refund_fn=refund_fn, status_fn=status_fn)

    fresh = store.get_task(task["task_id"])
    assert fresh["refunded"] == 0
    assert fresh["status"] != "refunded"
    assert fresh["refund_attempts"] == 1


def test_status_fn_exception_does_not_block_refund(store, process_one_refund):
    """If status_fn throws (RPC down), sweeper must still attempt the refund — better a redundant tx than never retry."""
    task = _make_expired_task(store)

    def status_fn(tid):
        raise RuntimeError("rpc unreachable")

    refund_calls = {"n": 0}
    def refund_fn(kp, tid):
        refund_calls["n"] += 1
        return {"hash": "ok", "status": "SUCCESS", "error": None}

    process_one_refund(FakeKeypair(), task, store, refund_fn=refund_fn, status_fn=status_fn)

    fresh = store.get_task(task["task_id"])
    assert refund_calls["n"] == 1
    assert fresh["refunded"] == 1
    assert fresh["status"] == "refunded"


def test_outcome_string_matches_each_branch(store, process_one_refund):
    """outcome key must come from a closed set so metrics/log code can't silently break when branches are added."""
    task_a = _make_expired_task(store, "outcome-a")
    task_b = _make_expired_task(store, "outcome-b")
    task_c = _make_expired_task(store, "outcome-c")
    task_d = _make_expired_task(store, "outcome-d")

    success = process_one_refund(FakeKeypair(), task_a, store,
        refund_fn=lambda kp, tid: {"hash": "h", "status": "SUCCESS", "error": None},
        status_fn=lambda tid: {"status": "found"})
    reconciled = process_one_refund(FakeKeypair(), task_b, store,
        refund_fn=lambda kp, tid: {"hash": "h", "status": "SUCCESS", "error": None},
        status_fn=lambda tid: {"status": "not_found"})
    retry = process_one_refund(FakeKeypair(), task_c, store,
        refund_fn=lambda kp, tid: {"hash": "h", "status": "FAILED", "error": "x"},
        status_fn=lambda tid: {"status": "found"})
    # Burn task_d's attempt budget first so the next failure hits dead-letter
    store.update_task("outcome-d", refund_attempts=4)
    refreshed = store.get_task("outcome-d")
    dead = process_one_refund(FakeKeypair(), refreshed, store,
        refund_fn=lambda kp, tid: {"hash": "h", "status": "FAILED", "error": "x"},
        status_fn=lambda tid: {"status": "found"})

    assert success["outcome"] == "refunded"
    # Renamed from "reconciled": an unreadable entry is not reconciled with
    # anything, it is unaccounted for. See STATUS_ENTRY_UNREADABLE.
    assert reconciled["outcome"] == "unreadable"
    assert retry["outcome"] == "retry_scheduled"
    assert dead["outcome"] == "dead_letter"
