"""Atomic nonce claim — closes the check-then-record TOCTOU race.

claim_nonce is a single atomic INSERT that returns True only if the row was actually
inserted (cur.rowcount > 0); UNIQUE constraint admits exactly one writer."""

import sys
import threading
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))

from research_agent.task_store import TaskStore


@pytest.fixture
def store(tmp_path):
    return TaskStore(db_path=str(tmp_path / "test.db"))


class TestClaimNonceBasic:
    def test_first_claim_succeeds(self, store):
        assert store.claim_nonce("nonce-a") is True

    def test_second_claim_same_nonce_fails(self, store):
        assert store.claim_nonce("nonce-a") is True
        assert store.claim_nonce("nonce-a") is False

    def test_different_nonces_independent(self, store):
        assert store.claim_nonce("nonce-a") is True
        assert store.claim_nonce("nonce-b") is True
        assert store.claim_nonce("nonce-a") is False
        assert store.claim_nonce("nonce-b") is False


class TestClaimNonceConcurrent:
    def test_concurrent_claims_exactly_one_wins(self, store):
        """50 threads racing for the same nonce — exactly 1 must win, all-claims-pass means TOCTOU regression."""
        nonce = "race-nonce"
        results: list[bool] = []
        lock = threading.Lock()
        start = threading.Event()

        def worker():
            start.wait()  # release all threads simultaneously
            ok = store.claim_nonce(nonce)
            with lock:
                results.append(ok)

        threads = [threading.Thread(target=worker) for _ in range(50)]
        for t in threads:
            t.start()
        start.set()
        for t in threads:
            t.join()

        winners = sum(1 for r in results if r)
        losers = sum(1 for r in results if not r)
        assert winners == 1, (
            f"Exactly one claim must win the race. Got {winners} winners. "
            f"All-claims-pass = TOCTOU race is back."
        )
        assert losers == 49

    def test_concurrent_claims_different_nonces_all_pass(self, store):
        results: list[bool] = []
        lock = threading.Lock()
        start = threading.Event()

        def worker(i):
            start.wait()
            ok = store.claim_nonce(f"nonce-{i}")
            with lock:
                results.append(ok)

        threads = [threading.Thread(target=worker, args=(i,)) for i in range(50)]
        for t in threads:
            t.start()
        start.set()
        for t in threads:
            t.join()

        assert all(results), "All distinct-nonce claims should succeed"
        assert len(results) == 50


class TestBackCompat:
    """Deprecated check_nonce / record_nonce kept as shims for un-migrated callers."""

    def test_check_nonce_still_works(self, store):
        assert store.check_nonce("x") is True
        store.record_nonce("x")
        assert store.check_nonce("x") is False

    def test_record_nonce_still_works(self, store):
        store.record_nonce("y")
        assert store.check_nonce("y") is False
